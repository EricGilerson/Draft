package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// startContainerAndRegister is the shared "create container, start it, verify
// readiness, register the route, retire the previous deployment" tail used by
// both the build path and the image-pull path. imageRef is the image to run:
// the locally-built tag for build mode, or settings["image"] for image mode.
// hooksWorkDir is the directory lifecycle hooks run in (service root for build
// mode, project root for image mode).
func (e *Engine) startContainerAndRegister(
	ctx context.Context,
	cli *client.Client,
	dep *store.Deployment,
	node *store.CanvasNode,
	settings map[string]string,
	deployEnv deploymentEnv,
	overrides containerOverrides,
	hooks lifecycleHooks,
	hooksWorkDir string,
	imageRef string,
	portStr string,
	addr NodeAddress,
	uid string,
) {
	nodeID := node.ID
	serviceName := addr.ServiceName
	projectName := addr.ProjectName
	environment := addr.Environment
	hostname := addr.InternalHostname

	// Route protocol: "http" (default, proxied, ephemeral host port) or "tcp"
	// (stable host port bound directly — used by datastores that need a
	// predictable localhost:5432). For TCP the port must be leased and the
	// route created BEFORE ContainerCreate so the container can bind to that
	// specific host port; for HTTP the port is ephemeral and the route is
	// registered after readiness below.
	protocol := strings.TrimSpace(settings["route_protocol"])
	if protocol == "" {
		protocol = "http"
	}
	isTCP := protocol == "tcp"
	preferPort, _ := strconv.Atoi(strings.TrimSpace(settings["host_port"]))

	e.emitBuildLog(nodeID, "==> Creating container...")
	dep.Status = "starting"
	dep.LastSeenAt = ptrTime(time.Now())
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "starting"})

	containerPort := nat.Port(portStr + "/tcp")
	containerName := fmt.Sprintf("draft-%s-%s-%d", projectName, serviceName, dep.Sequence)
	networkName := draftNetworkName(node.ProjectID, projectName, environment)

	// For TCP, lease the host port and create the route up front so we can bind
	// the container to it. On any failure after this point, the deferred
	// cleanup unregisters the pre-registered route and frees the lease.
	preRegisteredHostname := ""
	preRegisteredHostPort := 0
	succeeded := false
	if isTCP {
		reg, err := e.router.Register(networking.RegisterRequest{
			Service:     serviceName,
			Project:     projectName,
			ProjectID:   node.ProjectID,
			NodeID:      nodeID,
			UID:         uid,
			Environment: environment,
			Protocol:    "tcp",
			TargetHost:  "127.0.0.1",
			TargetPort:  atoiPort(portStr),
			PreferPort:  preferPort,
		})
		if err != nil {
			e.failDeployment(dep, nodeID, "tcp port lease failed: "+err.Error())
			return
		}
		preRegisteredHostname = reg.Hostname
		preRegisteredHostPort = reg.HostPort
		e.emitBuildLog(nodeID, fmt.Sprintf("    TCP route: %s → 127.0.0.1:%d", reg.Hostname, reg.HostPort))
	}
	defer func() {
		if preRegisteredHostname != "" && !succeeded {
			_ = e.router.Unregister(preRegisteredHostname)
		}
	}()

	if err := ensureDraftNetwork(ctx, cli, networkName, node.ProjectID, projectName, environment); err != nil {
		e.failDeployment(dep, nodeID, "docker network setup failed: "+err.Error())
		return
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("    Network: %s", networkName))

	// Resolve auto-named volumes against this node's identity and create them
	// in Docker (idempotent). Bind mounts and volumes with an explicit name
	// pass through unchanged. Done here — right before ContainerCreate — so
	// both the build and image deploy paths get it for free, and so the Docker
	// client we already opened is reused.
	resolvedMounts, err := e.ensureNamedVolumes(ctx, cli, node, addr, uid, ParseVolumeSpecs(settings["volume_mounts"]))
	if err != nil {
		e.failDeployment(dep, nodeID, "volume setup failed: "+err.Error())
		return
	}
	overrides.Mounts = resolvedMounts
	if len(resolvedMounts) > 0 {
		e.emitBuildLog(nodeID, fmt.Sprintf("    Volumes: %d", len(resolvedMounts)))
	}

	labels := map[string]string{
		"draft.project":    fmt.Sprintf("%d", node.ProjectID),
		"draft.node":       nodeID,
		"draft.deployment": fmt.Sprintf("%d", dep.ID),
	}
	for k, v := range overrides.Labels {
		labels[k] = v
	}

	containerCfg := &container.Config{
		Image:        imageRef,
		Env:          deployEnv.RuntimeEnv,
		Labels:       labels,
		ExposedPorts: nat.PortSet{containerPort: struct{}{}},
	}
	if len(overrides.Cmd) > 0 {
		containerCfg.Cmd = overrides.Cmd
	}
	if len(overrides.Entrypoint) > 0 {
		containerCfg.Entrypoint = overrides.Entrypoint
	}
	if overrides.WorkingDir != "" {
		containerCfg.WorkingDir = overrides.WorkingDir
	}
	if overrides.User != "" {
		containerCfg.User = overrides.User
	}
	if overrides.StopSignal != "" {
		containerCfg.StopSignal = overrides.StopSignal
	}
	if overrides.Healthcheck != nil {
		containerCfg.Healthcheck = overrides.Healthcheck
	}
	if overrides.StopTimeout != nil {
		containerCfg.StopTimeout = overrides.StopTimeout
	}

	// For TCP the container binds directly to the leased host port; for HTTP
	// the proxy fronts an ephemeral host port (Docker picks one).
	hostPortBinding := "0"
	if isTCP && preRegisteredHostPort > 0 {
		hostPortBinding = strconv.Itoa(preRegisteredHostPort)
	}

	hostCfg := &container.HostConfig{
		PortBindings: nat.PortMap{
			containerPort: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: hostPortBinding}},
		},
		RestartPolicy:  overrides.RestartPolicy,
		Resources:      overrides.Resources,
		Mounts:         overrides.Mounts,
		Privileged:     overrides.Privileged,
		ReadonlyRootfs: overrides.ReadonlyRootfs,
		CapAdd:         overrides.CapAdd,
		CapDrop:        overrides.CapDrop,
		Init:           overrides.Init,
	}

	createResp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, &dockernetwork.NetworkingConfig{
		EndpointsConfig: map[string]*dockernetwork.EndpointSettings{
			networkName: {
				Aliases: internalNetworkAliases(serviceName, hostname),
			},
		},
	}, nil, containerName)
	if err != nil {
		e.failDeployment(dep, nodeID, "container create failed: "+err.Error())
		return
	}

	dep.ContainerID = createResp.ID
	dep.LastSeenAt = ptrTime(time.Now())
	e.store.UpdateDeployment(dep)
	e.emitBuildLog(nodeID, fmt.Sprintf("    Container: %s (%s)", containerName, createResp.ID[:12]))

	e.emitBuildLog(nodeID, "==> Starting container...")
	if err := cli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		e.failDeployment(dep, nodeID, "container start failed: "+err.Error())
		return
	}

	inspect, err := cli.ContainerInspect(ctx, createResp.ID)
	if err != nil {
		e.failDeployment(dep, nodeID, "container inspect failed: "+err.Error())
		return
	}

	var hostPort int
	bindings := inspect.NetworkSettings.Ports[containerPort]
	if len(bindings) > 0 {
		fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("    Listening on 127.0.0.1:%d (container port %s)", hostPort, portStr))

	// Verify the new container is actually serving before switching traffic to
	// it. Until Register() runs below, the route still points at the previous
	// deployment, so this wait causes no downtime for existing traffic.
	e.emitBuildLog(nodeID, "==> Waiting for new container to become ready...")
	if err := e.waitForReady(ctx, cli, createResp.ID, hostPort); err != nil {
		// The new container never became ready: tear it down and leave the
		// previous deployment serving untouched (automatic rollback).
		e.emitBuildLog(nodeID, fmt.Sprintf("    New container not ready: %v", err))
		e.emitBuildLog(nodeID, "    Keeping the previous deployment; no traffic was switched.")
		stopTO := stopTimeoutForSettings(settings)
		cli.ContainerStop(context.Background(), createResp.ID, container.StopOptions{Timeout: &stopTO})
		_ = removeContainerAndWait(context.Background(), cli, createResp.ID)
		if dep.ImageTag != "" {
			_ = removeImageAndWait(context.Background(), cli, dep.ImageTag)
		}
		e.failDeployment(dep, nodeID, "new container did not become ready: "+err.Error()+" (previous deployment left running)")
		return
	}
	e.emitBuildLog(nodeID, "    Ready")

	// HTTP routes are registered now (after readiness) with the ephemeral host
	// port the proxy forwards to. TCP routes were pre-registered before
	// ContainerCreate so the container could bind to the leased port.
	routeHostname := preRegisteredHostname
	if !isTCP {
		e.emitBuildLog(nodeID, "==> Registering route...")
		regResult, err := e.router.Register(networking.RegisterRequest{
			Service:     serviceName,
			Project:     projectName,
			ProjectID:   node.ProjectID,
			NodeID:      nodeID,
			UID:         uid,
			Environment: environment,
			Protocol:    "http",
			TargetHost:  "127.0.0.1",
			TargetPort:  hostPort,
		})
		if err != nil {
			log.Printf("[deploy] route registration failed (non-fatal): %v", err)
			e.emitBuildLog(nodeID, fmt.Sprintf("    Warning: %v", err))
		} else if regResult != nil {
			routeHostname = regResult.Hostname
			e.emitBuildLog(nodeID, fmt.Sprintf("    Route: %s → 127.0.0.1:%d", regResult.Hostname, hostPort))
		}
	}

	dep.Status = "running"
	dep.HostPort = hostPort
	dep.ContainerStartedAt = ptrTime(time.Now())
	dep.ContainerStoppedAt = nil
	dep.FinishedAt = nil
	dep.LastSeenAt = ptrTime(time.Now())
	if routeHostname != "" {
		dep.Hostname = routeHostname
	}
	e.store.UpdateDeployment(dep)

	// Traffic now flows to the new container; retire the previous deployment(s).
	e.emitBuildLog(nodeID, "==> Retiring previous deployment...")
	if err := e.stopPrevious(ctx, cli, nodeID, dep.ID, dep.Hostname); err != nil {
		log.Printf("[deploy] warning: retire previous: %v", err)
		e.emitBuildLog(nodeID, fmt.Sprintf("    Warning: %v", err))
	} else {
		e.emitBuildLog(nodeID, "    Done")
	}

	if hooks.PostDeploy != "" {
		if err := runLifecycleHook(ctx, "post-deploy", hooks.PostDeploy, hooksWorkDir, func(line string) {
			e.emitBuildLog(nodeID, line)
		}); err != nil {
			log.Printf("[deploy] post-deploy hook failed (non-fatal): %v", err)
		}
	}

	e.emitBuildLog(nodeID, "==> Deployed successfully!")

	e.promoteStagedAfterSuccessfulDeploy(ctx, nodeID, node.ProjectID)

	succeeded = true

	e.emitStatus(nodeID, StatusEvent{
		DeploymentID: dep.ID,
		Status:       "running",
		Hostname:     dep.Hostname,
		HostPort:     hostPort,
	})

	go e.watchContainer(context.Background(), dep, nodeID)
}

// runImageDeploy is the image-pull deploy path for templates whose Mode is
// "image" (Postgres, Redis, MySQL, Mongo, or any custom image template). It
// skips the build entirely: pull the image, then reuse the shared
// startContainerAndRegister tail so routing, blue-green cutover, healthchecks,
// and metrics behave exactly like a built service.
func (e *Engine) runImageDeploy(ctx context.Context, nodeID string, settings map[string]string, node *store.CanvasNode, project *store.Project) {
	imageRef := strings.TrimSpace(settings["image"])
	portStr := settings["service_port"]
	if imageRef == "" {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "image and port are required settings for image-mode services"})
		return
	}
	if portStr == "" {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "service_port is required for image-mode services"})
		return
	}

	e.emitBuildLog(nodeID, "==> Initializing deployment (image mode)...")
	e.emitBuildLog(nodeID, fmt.Sprintf("    Image: %s", imageRef))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Service port: %s", portStr))

	dep := &store.Deployment{
		NodeID:         nodeID,
		ProjectID:      node.ProjectID,
		Status:         "building", // "building" covers the pull phase for UI continuity
		JobID:          fmt.Sprintf("%d-%s", time.Now().UnixNano(), nodeID),
		ImageTag:       imageRef,
		WorkerPID:      0,
		StartedAt:      ptrTime(time.Now()),
		BuildStartedAt: ptrTime(time.Now()),
		LastSeenAt:     ptrTime(time.Now()),
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

	if hooks.PreBuild != "" {
		if err := runLifecycleHook(ctx, "pre-build", hooks.PreBuild, project.Path, func(line string) {
			e.emitBuildLog(nodeID, line)
		}); err != nil {
			e.failDeployment(dep, nodeID, err.Error())
			return
		}
	}

	e.emitBuildLog(nodeID, "==> Resolving image...")
	pullPolicy := normalizePullPolicy(settings["pull_policy"])
	skipPull := false
	switch pullPolicy {
	case pullPolicyNever:
		skipPull = true
	case pullPolicyMissing:
		if e.imageExistsLocally(ctx, cli, imageRef) {
			skipPull = true
		}
	}
	if skipPull {
		if !e.imageExistsLocally(ctx, cli, imageRef) {
			e.failDeployment(dep, nodeID, fmt.Sprintf("pull policy is %q but image %q is not present locally; set pull policy to \"always\" or pull the image manually", pullPolicy, imageRef))
			return
		}
		e.emitBuildLog(nodeID, fmt.Sprintf("    Image %q already present locally — skipping pull", imageRef))
	} else {
		e.emitBuildLog(nodeID, "==> Pulling image...")
		if err := e.pullImage(ctx, cli, nodeID, imageRef, logFile); err != nil {
			if ctx.Err() != nil {
				e.failDeployment(dep, nodeID, "pull cancelled")
				return
			}
			e.failDeployment(dep, nodeID, "image pull failed: "+err.Error())
			return
		}
	}

	now := time.Now()
	dep.Status = "built"
	dep.BuildFinishedAt = &now
	dep.LastSeenAt = &now
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "built"})

	if hooks.PreDeploy != "" {
		if err := runLifecycleHook(ctx, "pre-deploy", hooks.PreDeploy, project.Path, func(line string) {
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

	e.startContainerAndRegister(ctx, cli, dep, node, settings, deployEnv, overrides, hooks, project.Path, imageRef, portStr, addr, uid)
}

// pullImage pulls imageRef, streaming progress lines to the build log. The
// Docker pull response is a stream of JSON status objects; each is rendered as
// a human-readable line (layer pull progress / status messages).
func (e *Engine) pullImage(ctx context.Context, cli *client.Client, nodeID, imageRef string, logFile *os.File) error {
	resp, err := cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return err
	}
	defer resp.Close()

	dec := json.NewDecoder(resp)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var msg struct {
			Status   string `json:"status"`
			ID       string `json:"id"`
			Progress string `json:"progress"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		line := renderPullLine(msg.Status, msg.ID, msg.Progress)
		if line == "" {
			continue
		}
		e.emitBuildLog(nodeID, line)
		if logFile != nil {
			_, _ = logFile.WriteString(line + "\n")
		}
	}
	return nil
}

// renderPullLine formats a Docker pull status message into a single log line.
func renderPullLine(status, id, progress string) string {
	if id == "" {
		return strings.TrimSpace(status)
	}
	if progress == "" {
		return fmt.Sprintf("    %s: %s", id, strings.TrimSpace(status))
	}
	return fmt.Sprintf("    %s: %s %s", id, strings.TrimSpace(status), strings.TrimSpace(progress))
}

// Pull policy values for image-mode services. Stored on node_settings under the
// "pull_policy" key. The default (empty/unset) is pullPolicyMissing, which
// matches Docker's intuitive behavior: use the image if it's already local,
// otherwise pull. pullPolicyAlways forces a fresh pull; pullPolicyNever
// requires the image to already be present locally (air-gapped / strictly-local
// workflows) and fails the deploy if it isn't.
const (
	pullPolicyAlways   = "always"
	pullPolicyMissing  = "missing"
	pullPolicyNever    = "never"
	pullPolicyDefault  = pullPolicyMissing
)

// normalizePullPolicy coerces an arbitrary setting value to one of the
// recognized policies, defaulting to pullPolicyMissing for empty/unknown input.
func normalizePullPolicy(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case pullPolicyAlways:
		return pullPolicyAlways
	case pullPolicyNever:
		return pullPolicyNever
	default:
		return pullPolicyMissing
	}
}

// imageExistsLocally reports whether imageRef is present in the local Docker
// image store. It uses the daemon's `reference` filter, which matches both
// locally-tagged images (e.g. "myapp:latest") and registry-prefixed refs. A
// listing error is treated as "not found" so the caller falls through to a
// real pull attempt, which will surface the underlying error if any.
func (e *Engine) imageExistsLocally(ctx context.Context, cli *client.Client, imageRef string) bool {
	args := filters.NewArgs()
	args.Add("reference", imageRef)
	summary, err := cli.ImageList(ctx, image.ListOptions{Filters: args})
	if err != nil {
		return false
	}
	return len(summary) > 0
}

// atoiPort parses a port string to an int, returning 0 for empty/invalid input.
func atoiPort(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
