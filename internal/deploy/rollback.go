package deploy

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"Draft/internal/store"

	"github.com/docker/docker/client"
)

// RollbackEligibility describes whether a single historical deployment can be
// rolled back to and why. The UI uses this to enable/disable per-row "Redeploy"
// buttons without probing Docker itself.
type RollbackEligibility struct {
	DeploymentID uint   `json:"deploymentId"`
	Eligible     bool   `json:"eligible"`
	Method       string `json:"method"` // "run-image" | "re-pull" | "none"
	Reason       string `json:"reason"`
}

// RollbackEligibility computes per-deployment rollback eligibility for a node.
// Image-mode deployments are always re-pullable; build/git deployments are
// eligible only while their built image is still retained locally (governed by
// keep_images). Git-sourced rebuilds from a SHA are not supported yet — the
// honest reason is surfaced rather than pretending disk is infinite.
func (e *Engine) RollbackEligibility(ctx context.Context, nodeID string) ([]RollbackEligibility, error) {
	deployments, err := e.store.ListDeployments(nodeID)
	if err != nil {
		return nil, err
	}
	settings, _ := e.store.GetNodeSettings(nodeID)
	isImageMode := strings.TrimSpace(settings["image"]) != "" && strings.TrimSpace(settings["dockerfile"]) == ""

	cli, cliErr := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if cliErr == nil {
		defer cli.Close()
	}

	out := make([]RollbackEligibility, 0, len(deployments))
	for _, d := range deployments {
		out = append(out, e.rollbackEligibilityForDeployment(ctx, cli, d, isImageMode))
	}
	return out, nil
}

func (e *Engine) rollbackEligibilityForDeployment(ctx context.Context, cli *client.Client, d store.Deployment, isImageMode bool) RollbackEligibility {
	res := RollbackEligibility{DeploymentID: d.ID}
	if d.ImageTag == "" {
		res.Reason = "no image recorded for this deployment"
		return res
	}
	if isImageMode {
		// ImageTag is a registry pull ref — always recoverable.
		res.Eligible = true
		res.Method = "re-pull"
		return res
	}
	if cli == nil {
		res.Reason = "Docker not reachable"
		return res
	}
	if e.imageExistsLocally(ctx, cli, d.ImageTag) {
		res.Eligible = true
		res.Method = "run-image"
		return res
	}
	if d.SourceSHA != "" {
		res.Reason = "image was garbage-collected; rebuild-from-sha is not supported yet — redeploy from the pinned branch instead"
	} else {
		res.Reason = "working-tree build, image no longer retained (increase keep_images to keep more)"
	}
	return res
}

// RollbackDeployment re-runs a historical deployment's image. It creates a new
// deployment row (so rollback is visible in history) and reuses the shared
// startContainerAndRegister tail so routing, blue-green cutover, healthchecks,
// and env/volume resolution behave exactly like a fresh deploy. Env references
// are re-resolved against current node settings so @{Service.ATTR} links stay
// live. Image-mode images are re-pulled if missing; build/git images must be
// retained locally (keep_images) or the call returns an error.
func (e *Engine) RollbackDeployment(ctx context.Context, deploymentID uint) error {
	historical, err := e.store.GetDeployment(deploymentID)
	if err != nil {
		return fmt.Errorf("deployment not found: %w", err)
	}
	if historical.ImageTag == "" {
		return fmt.Errorf("deployment has no image to roll back to")
	}
	node, err := e.store.GetNode(historical.NodeID)
	if err != nil {
		return err
	}
	settings, err := e.store.EffectiveNodeSettings(historical.NodeID)
	if err != nil {
		return err
	}
	isImageMode := strings.TrimSpace(settings["image"]) != "" && strings.TrimSpace(settings["dockerfile"]) == ""

	if !isImageMode {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("cannot connect to Docker: %w", err)
		}
		present := e.imageExistsLocally(ctx, cli, historical.ImageTag)
		cli.Close()
		if !present {
			if historical.SourceSHA != "" {
				return fmt.Errorf("image no longer retained and rebuild-from-sha is not supported yet; redeploy from the pinned branch instead")
			}
			return fmt.Errorf("working-tree build image is no longer retained; cannot roll back (increase keep_images to keep more)")
		}
	}

	// Run asynchronously like Deploy does; status flows through SSE.
	go e.runRollbackDeploy(context.Background(), historical, node, settings, isImageMode)
	return nil
}

// runRollbackDeploy mirrors runImageDeploy but runs a historical ImageTag
// instead of settings["image"], and skips the build phase entirely. For
// build/git images (local tags) it never pulls; for image-mode it pulls if the
// image is missing locally.
func (e *Engine) runRollbackDeploy(ctx context.Context, historical *store.Deployment, node *store.CanvasNode, settings map[string]string, isImageMode bool) {
	nodeID := node.ID
	imageRef := historical.ImageTag
	portStr := settings["service_port"]
	if portStr == "" {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "service_port is required"})
		return
	}

	e.emitBuildLog(nodeID, fmt.Sprintf("==> Rolling back to deployment #%d (%s)...", historical.Sequence, imageRef))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Image: %s", imageRef))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Service port: %s", portStr))

	dep := &store.Deployment{
		NodeID:     nodeID,
		ProjectID:  node.ProjectID,
		Status:     "building",
		JobID:      fmt.Sprintf("%d-%s-rollback", time.Now().UnixNano(), nodeID),
		ImageTag:   imageRef,
		SourceSHA:  historical.SourceSHA,
		StartedAt:  ptrTime(time.Now()),
		LastSeenAt: ptrTime(time.Now()),
	}
	dep, err := e.store.CreateDeployment(dep)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to create deployment record"})
		return
	}

	addr, err := e.computeNodeAddress(node)
	if err != nil {
		e.failDeployment(dep, nodeID, "failed to resolve node identity: "+err.Error())
		return
	}
	uid, err := e.store.EnsureNodeUID(nodeID)
	if err != nil {
		e.failDeployment(dep, nodeID, "failed to resolve node identity: "+err.Error())
		return
	}

	deployEnv, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:           nodeID,
		ProjectID:        node.ProjectID,
		ServiceName:      addr.ServiceName,
		ProjectName:      addr.ProjectName,
		Environment:      addr.Environment,
		ServicePort:      addr.ServicePort,
		InternalHostname: addr.InternalHostname,
		InternalURL:      addr.InternalURL,
		PublicHostname:   addr.PublicHostname,
		PublicURL:        addr.PublicURL,
	})
	if err != nil {
		e.failDeployment(dep, nodeID, "failed to resolve environment: "+err.Error())
		return
	}

	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "building"})
	e.emitBuildLog(nodeID, "==> Connecting to Docker daemon...")
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		e.failDeployment(dep, nodeID, "cannot connect to Docker: "+err.Error())
		return
	}
	defer cli.Close()

	ping, pingErr := cli.Ping(ctx)
	if pingErr != nil {
		e.failDeployment(dep, nodeID, "Docker daemon not reachable: "+pingErr.Error())
		return
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("    Connected (API v%s)", ping.APIVersion))

	logFile, err := os.Create(e.logPath(dep.ID))
	if err != nil {
		e.failDeployment(dep, nodeID, "cannot create build log: "+err.Error())
		return
	}
	defer logFile.Close()

	hooks := parseLifecycleHooks(settings)
	overrides := parseContainerOverrides(settings)
	if overrides.Labels == nil {
		overrides.Labels = map[string]string{}
	}
	overrides.Labels["draft.rollback"] = "true"

	project, perr := e.store.GetProject(node.ProjectID)
	if perr != nil {
		e.failDeployment(dep, nodeID, "project not found: "+perr.Error())
		return
	}
	hooksWorkDir := project.Path

	// Image-mode: pull if missing (the historical ref is a registry image).
	// Build/git: the image is a local tag — never pull, require local presence.
	if isImageMode {
		if !e.imageExistsLocally(ctx, cli, imageRef) {
			e.emitBuildLog(nodeID, "==> Pulling image...")
			if err := e.pullImage(ctx, cli, nodeID, imageRef, logFile); err != nil {
				if ctx.Err() != nil {
					e.failDeployment(dep, nodeID, "pull cancelled")
					return
				}
				e.failDeployment(dep, nodeID, "image pull failed: "+err.Error())
				return
			}
		} else {
			e.emitBuildLog(nodeID, fmt.Sprintf("    Image %q present locally", imageRef))
		}
	} else if !e.imageExistsLocally(ctx, cli, imageRef) {
		e.failDeployment(dep, nodeID, fmt.Sprintf("image %q is no longer retained locally", imageRef))
		return
	} else {
		e.emitBuildLog(nodeID, fmt.Sprintf("    Reusing retained image %q", imageRef))
	}

	now := time.Now()
	dep.Status = "built"
	dep.BuildFinishedAt = &now
	dep.LastSeenAt = &now
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "built"})

	if hooks.PreDeploy != "" && hooksWorkDir != "" {
		if err := runLifecycleHook(ctx, "pre-deploy", hooks.PreDeploy, hooksWorkDir, func(line string) {
			e.emitBuildLog(nodeID, line)
		}); err != nil {
			e.failDeployment(dep, nodeID, err.Error())
			return
		}
	}

	if ctx.Err() != nil {
		e.failDeployment(dep, nodeID, "cancelled")
		return
	}

	e.startContainerAndRegister(ctx, cli, dep, node, settings, deployEnv, overrides, hooks, hooksWorkDir, imageRef, portStr, addr, uid)
}
