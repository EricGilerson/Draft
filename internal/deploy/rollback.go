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
	Method       string `json:"method"` // "run-image" | "re-pull" | "rebuild-sha" | "none"
	Reason       string `json:"reason"`
}

// RollbackEligibility computes per-deployment rollback eligibility for a node.
// Image-mode deployments are always re-pullable. Build deployments are eligible
// while their image is retained locally, or via rebuild-from-SHA when
// SourceSHA is recorded (keeps keep_images lean without losing rollback).
func (e *Engine) RollbackEligibility(ctx context.Context, nodeID string) ([]RollbackEligibility, error) {
	deployments, err := e.store.ListDeployments(nodeID)
	if err != nil {
		return nil, err
	}
	settings, _ := e.store.GetNodeSettings(nodeID)
	isImageMode := strings.TrimSpace(settings["image"]) != "" && strings.TrimSpace(settings["dockerfile"]) == ""
	canRebuildSHA := strings.TrimSpace(settings["dockerfile"]) != ""

	cli, cliErr := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if cliErr == nil {
		defer cli.Close()
	}

	out := make([]RollbackEligibility, 0, len(deployments))
	for _, d := range deployments {
		out = append(out, e.rollbackEligibilityForDeployment(ctx, cli, d, isImageMode, canRebuildSHA))
	}
	return out, nil
}

func (e *Engine) rollbackEligibilityForDeployment(ctx context.Context, cli *client.Client, d store.Deployment, isImageMode, canRebuildSHA bool) RollbackEligibility {
	res := RollbackEligibility{DeploymentID: d.ID}
	if d.ImageTag == "" && d.SourceSHA == "" {
		res.Reason = "no image or source commit recorded for this deployment"
		return res
	}
	if isImageMode {
		if d.ImageTag == "" {
			res.Reason = "no image recorded for this deployment"
			return res
		}
		res.Eligible = true
		res.Method = "re-pull"
		return res
	}
	if d.ImageTag != "" && cli != nil {
		if _, ok := e.resolveLocalDraftImageRef(ctx, cli, d.ImageTag); ok {
			res.Eligible = true
			res.Method = "run-image"
			return res
		}
	}
	if d.SourceSHA != "" && canRebuildSHA {
		res.Eligible = true
		res.Method = "rebuild-sha"
		res.Reason = fmt.Sprintf("image not retained; will rebuild from commit %s (current settings apply)", shortSHA(d.SourceSHA))
		return res
	}
	if d.SourceSHA != "" && !canRebuildSHA {
		res.Reason = "image was garbage-collected; service is no longer in build mode (no dockerfile)"
		return res
	}
	if cli == nil {
		res.Reason = "Docker not reachable"
		return res
	}
	res.Reason = "working-tree build, image no longer retained (no source commit to rebuild from)"
	return res
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// RollbackDeployment re-runs a historical deployment. Prefer the retained
// image when present; otherwise rebuild from SourceSHA when recorded. Creates
// a new deployment row and shares Deploy's cancel map so concurrent
// Deploy/Stop cancel an in-flight rollback.
func (e *Engine) RollbackDeployment(ctx context.Context, deploymentID uint) error {
	historical, err := e.store.GetDeployment(deploymentID)
	if err != nil {
		return fmt.Errorf("deployment not found: %w", err)
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
	canRebuildSHA := historical.SourceSHA != "" && strings.TrimSpace(settings["dockerfile"]) != ""

	rebuildFromSHA := false
	if !isImageMode {
		if historical.ImageTag == "" && !canRebuildSHA {
			return fmt.Errorf("deployment has no image or source commit to roll back to")
		}
		if historical.ImageTag != "" {
			cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
			if err != nil {
				return fmt.Errorf("cannot connect to Docker: %w", err)
			}
			_, present := e.resolveLocalDraftImageRef(ctx, cli, historical.ImageTag)
			cli.Close()
			if !present {
				if canRebuildSHA {
					rebuildFromSHA = true
				} else {
					return fmt.Errorf("working-tree build image is no longer retained; cannot roll back")
				}
			}
		} else {
			rebuildFromSHA = true
		}
	} else if historical.ImageTag == "" {
		return fmt.Errorf("deployment has no image to roll back to")
	}

	nodeID := historical.NodeID
	e.mu.Lock()
	if cancel, ok := e.active[nodeID]; ok {
		cancel()
	}
	buildCtx, cancel := context.WithCancel(ctx)
	e.active[nodeID] = cancel
	e.mu.Unlock()

	go func() {
		defer func() {
			e.mu.Lock()
			delete(e.active, nodeID)
			e.mu.Unlock()
			cancel()
		}()
		if rebuildFromSHA {
			e.runRollbackRebuildFromSHA(buildCtx, historical, node)
			return
		}
		e.runRollbackDeploy(buildCtx, historical, node, settings, isImageMode)
	}()
	return nil
}

// runRollbackRebuildFromSHA rebuilds from the historical commit using current
// effective settings (dockerfile/service_root/env may have changed since then —
// intentional trade-off vs retaining every image).
func (e *Engine) runRollbackRebuildFromSHA(ctx context.Context, historical *store.Deployment, node *store.CanvasNode) {
	nodeID := node.ID
	sha := strings.TrimSpace(historical.SourceSHA)
	e.emitBuildLog(nodeID, fmt.Sprintf("==> Rolling back by rebuilding commit %s (deployment #%d)...", shortSHA(sha), historical.Sequence))
	e.emitBuildLog(nodeID, "    Note: uses current service settings; only the source commit is restored from history")
	e.runDeployWith(ctx, nodeID, map[string]string{"git_branch": sha})
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
		EnvironmentID:    node.EnvironmentID,
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
	// Build/git: the image is a local tag (live or -previous) — never pull.
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
	} else {
		resolved, ok := e.resolveLocalDraftImageRef(ctx, cli, imageRef)
		if !ok {
			e.failDeployment(dep, nodeID, fmt.Sprintf("image %q is no longer retained locally", imageRef))
			return
		}
		if resolved != imageRef {
			e.emitBuildLog(nodeID, fmt.Sprintf("    Reusing retained previous image %q (stored as %q)", resolved, imageRef))
			imageRef = resolved
			dep.ImageTag = resolved
			e.store.UpdateDeployment(dep)
		} else {
			e.emitBuildLog(nodeID, fmt.Sprintf("    Reusing retained image %q", imageRef))
		}
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
