package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// cloneHelperImage is a small image used to tar-copy between volumes.
// Alpine is widely cached on developer machines.
const cloneHelperImage = "alpine:3.20"

// CloneVolumeResult is returned after a successful late or create-time clone.
type CloneVolumeResult struct {
	NewVolumeName      string `json:"newVolumeName"`
	OrphanedVolumeName string `json:"orphanedVolumeName,omitempty"`
}

// CloneVolumePreview describes what CloneVolumeData will do.
type CloneVolumePreview struct {
	SourceVolume     string `json:"sourceVolume"`
	TargetVolume     string `json:"targetVolume"`
	SourceSize       int64  `json:"sourceSize"`
	TargetSize       int64  `json:"targetSize"`
	SourceRunning    bool   `json:"sourceRunning"`
	TargetRunning    bool   `json:"targetRunning"`
	WillOrphanVolume bool   `json:"willOrphanVolume"`
	ContainerPath    string `json:"containerPath"`
	Warning          string `json:"warning"`
}

var (
	cloneMu    sync.Mutex
	cloneLocks = map[string]struct{}{}
)

func tryLockClone(nodeID string) bool {
	cloneMu.Lock()
	defer cloneMu.Unlock()
	if _, busy := cloneLocks[nodeID]; busy {
		return false
	}
	cloneLocks[nodeID] = struct{}{}
	return true
}

func unlockClone(nodeID string) {
	cloneMu.Lock()
	delete(cloneLocks, nodeID)
	cloneMu.Unlock()
}

// resolveVolumeNameForPath returns the Docker volume name for a node's mount path.
func (e *Engine) resolveVolumeNameForPath(node *store.CanvasNode, settings map[string]string, containerPath string) (string, error) {
	containerPath = strings.TrimSpace(containerPath)
	for _, spec := range ParseVolumeSpecs(settings["volume_mounts"]) {
		if strings.TrimSpace(spec.ContainerPath) != containerPath {
			continue
		}
		t := spec.Type
		if t == "" {
			t = VolumeTypeBind
		}
		if t != VolumeTypeVolume {
			return "", fmt.Errorf("path %q is not a named volume", containerPath)
		}
		if name := strings.TrimSpace(spec.Source); name != "" {
			return name, nil
		}
		// Auto-named: derive from node identity.
		project, err := e.store.GetProject(node.ProjectID)
		if err != nil {
			return "", err
		}
		envSlug := "default"
		sandbox := false
		if env, err := e.store.GetEnvironment(node.EnvironmentID); err == nil {
			envSlug = env.Slug
			sandbox, err = e.isSandboxEnvironment(env.ID)
			if err != nil {
				return "", err
			}
		}
		uid, err := e.store.EnsureNodeUID(node.ID)
		if err != nil {
			return "", err
		}
		return DraftVolumeName(node.ProjectID, project.Name, networking.DockerEnvironment(envSlug, sandbox), uid, containerPath), nil
	}
	return "", fmt.Errorf("no volume mount at path %q on service %q", containerPath, node.Label)
}

// PreviewCloneVolume returns sizes and warnings before a destructive late clone.
func (e *Engine) PreviewCloneVolume(ctx context.Context, targetNodeID, sourceNodeID, containerPath string) (*CloneVolumePreview, error) {
	target, source, targetSettings, sourceSettings, err := e.cloneEndpoints(targetNodeID, sourceNodeID)
	if err != nil {
		return nil, err
	}
	srcVol, err := e.resolveVolumeNameForPath(source, sourceSettings, containerPath)
	if err != nil {
		return nil, err
	}
	tgtVol, err := e.resolveVolumeNameForPath(target, targetSettings, containerPath)
	if err != nil {
		// Target may not have the path yet (promote empty); still preview source.
		tgtVol = ""
	}

	preview := &CloneVolumePreview{
		SourceVolume:     srcVol,
		TargetVolume:     tgtVol,
		ContainerPath:    containerPath,
		WillOrphanVolume: tgtVol != "",
		Warning:          "Replace all data at this path with a copy from the source. The previous volume is kept as an orphan until you delete it.",
	}

	if dep, _ := e.store.ActiveDeployment(sourceNodeID); dep != nil && (dep.Status == "running" || dep.Status == "starting") {
		preview.SourceRunning = true
	}
	if dep, _ := e.store.ActiveDeployment(targetNodeID); dep != nil && (dep.Status == "running" || dep.Status == "starting") {
		preview.TargetRunning = true
	}

	// Best-effort sizes via ListManagedVolumes / DiskUsage.
	pid := target.ProjectID
	vols, err := e.ListManagedVolumes(ctx, &pid, "")
	if err == nil {
		for _, v := range vols {
			if v.Name == srcVol {
				preview.SourceSize = v.Size
			}
			if v.Name == tgtVol {
				preview.TargetSize = v.Size
			}
		}
	}
	return preview, nil
}

func (e *Engine) cloneEndpoints(targetNodeID, sourceNodeID string) (
	target, source *store.CanvasNode,
	targetSettings, sourceSettings map[string]string,
	err error,
) {
	target, err = e.store.GetNode(targetNodeID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("target not found: %w", err)
	}
	source, err = e.store.GetNode(sourceNodeID)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("source not found: %w", err)
	}
	if target.ProjectID != source.ProjectID {
		return nil, nil, nil, nil, fmt.Errorf("source and target must be in the same project")
	}
	targetSettings, err = e.store.GetNodeSettings(targetNodeID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sourceSettings, err = e.store.GetNodeSettings(sourceNodeID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if ParseServiceLink(targetSettings[SettingServiceLink]) != nil {
		return nil, nil, nil, nil, fmt.Errorf("target is a linked service; promote it before cloning into local volumes")
	}
	if ParseServiceLink(sourceSettings[SettingServiceLink]) != nil {
		return nil, nil, nil, nil, fmt.Errorf("source is a linked service; only root volumes can be cloned")
	}
	return target, source, targetSettings, sourceSettings, nil
}

// CloneVolumeData copies source's volume at containerPath into the target node
// using the safe staging + promote pattern for late re-seed, or create-into-new
// when the target has no prior volume at that path.
func (e *Engine) CloneVolumeData(ctx context.Context, targetNodeID, sourceNodeID, containerPath string, consistency CloneConsistency) (_ *CloneVolumeResult, retErr error) {
	if !tryLockClone(targetNodeID) {
		return nil, fmt.Errorf("a volume clone is already in progress for this service")
	}
	defer unlockClone(targetNodeID)

	if consistency == "" {
		consistency = CloneConsistent
	}
	containerPath = strings.TrimSpace(containerPath)
	if containerPath == "" {
		return nil, fmt.Errorf("container path is required")
	}

	target, source, targetSettings, sourceSettings, err := e.cloneEndpoints(targetNodeID, sourceNodeID)
	if err != nil {
		return nil, err
	}

	srcVol, err := e.resolveVolumeNameForPath(source, sourceSettings, containerPath)
	if err != nil {
		return nil, err
	}

	// Always stop target before writing.
	if err := e.stopNodeContainers(ctx, targetNodeID); err != nil {
		return nil, fmt.Errorf("stop target: %w", err)
	}

	var restartSource bool
	if consistency == CloneConsistent {
		if dep, _ := e.store.ActiveDeployment(sourceNodeID); dep != nil && dep.ContainerID != "" &&
			(dep.Status == "running" || dep.Status == "starting") {
			if err := e.stopNodeContainers(ctx, sourceNodeID); err != nil {
				return nil, fmt.Errorf("stop source for consistent clone: %w", err)
			}
			restartSource = true
		}
	}
	defer func() {
		if !restartSource {
			return
		}
		e.emitBuildLog(sourceNodeID, "==> Restarting source service after consistent clone...")
		if err := e.Deploy(context.Background(), sourceNodeID); err != nil {
			if retErr != nil {
				retErr = fmt.Errorf("%w; restart source after consistent clone: %v", retErr, err)
			} else {
				retErr = fmt.Errorf("restart source after consistent clone: %w", err)
			}
		}
	}()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	// Ensure source volume exists.
	if _, err := cli.VolumeInspect(ctx, srcVol); err != nil {
		return nil, fmt.Errorf("source volume %q not found (deploy the source service first): %w", srcVol, err)
	}

	project, err := e.store.GetProject(target.ProjectID)
	if err != nil {
		return nil, err
	}
	envSlug := "default"
	sandbox := false
	if env, err := e.store.GetEnvironment(target.EnvironmentID); err == nil {
		envSlug = env.Slug
		sandbox, err = e.isSandboxEnvironment(env.ID)
		if err != nil {
			return nil, err
		}
	}
	dockerEnv := networking.DockerEnvironment(envSlug, sandbox)

	stagingName := fmt.Sprintf("draft-clone-%s-%s", sanitize(targetNodeID), shortHash(containerPath + targetNodeID)[:8])
	// Create staging with ownership labels for the target (will become the live volume).
	if _, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   stagingName,
		Driver: "local",
		Labels: map[string]string{
			"draft.managed":       "true",
			"draft.project":       fmt.Sprintf("%d", target.ProjectID),
			"draft.projectName":   project.Name,
			"draft.node":          target.ID,
			"draft.environment":   dockerEnv,
			"draft.target":        containerPath,
			"draft.clone_staging": "true",
		},
	}); err != nil {
		return nil, fmt.Errorf("create staging volume: %w", err)
	}

	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = cli.VolumeRemove(ctx, stagingName, true)
		}
	}()

	e.emitBuildLog(targetNodeID, fmt.Sprintf("==> Cloning volume %s → staging", srcVol))
	if err := copyVolumeContents(ctx, cli, srcVol, stagingName); err != nil {
		return nil, fmt.Errorf("copy volume data: %w", err)
	}

	// Previous volume name (if any) — will be orphaned, not deleted.
	var orphaned string
	if oldName, err := e.resolveVolumeNameForPath(target, targetSettings, containerPath); err == nil && oldName != "" && oldName != stagingName {
		// Only orphan if it exists in Docker.
		if _, err := cli.VolumeInspect(ctx, oldName); err == nil {
			orphaned = oldName
		}
	}

	// Promote: point volume_mounts at staging as explicit source.
	if err := e.setVolumeMountSource(targetNodeID, targetSettings, containerPath, stagingName); err != nil {
		return nil, err
	}
	cleanupStaging = false // owned by target settings now

	// Clear clone_staging marker is best-effort (Docker volumes can't update labels);
	// the mount source is the source of truth.

	e.emitBuildLog(targetNodeID, fmt.Sprintf("    Volume ready: %s", stagingName))
	if orphaned != "" {
		e.emitBuildLog(targetNodeID, fmt.Sprintf("    Previous volume kept as orphan: %s", orphaned))
	}

	return &CloneVolumeResult{
		NewVolumeName:      stagingName,
		OrphanedVolumeName: orphaned,
	}, nil
}

// setVolumeMountSource updates volume_mounts so containerPath uses explicit source.
func (e *Engine) setVolumeMountSource(nodeID string, settings map[string]string, containerPath, volumeName string) error {
	specs := ParseVolumeSpecs(settings["volume_mounts"])
	found := false
	for i := range specs {
		if strings.TrimSpace(specs[i].ContainerPath) != containerPath {
			continue
		}
		specs[i].Type = VolumeTypeVolume
		specs[i].Source = volumeName
		found = true
	}
	if !found {
		specs = append(specs, VolumeSpec{
			Type:          VolumeTypeVolume,
			Source:        volumeName,
			ContainerPath: containerPath,
		})
	}
	raw, err := json.Marshal(specs)
	if err != nil {
		return err
	}
	return e.store.SetNodeSetting(nodeID, "volume_mounts", string(raw))
}

// copyVolumeContents tar-streams from srcVol to dstVol via a one-shot helper container.
func copyVolumeContents(ctx context.Context, cli *client.Client, srcVol, dstVol string) error {
	// Ensure helper image exists.
	if _, err := cli.ImageInspect(ctx, cloneHelperImage); err != nil {
		reader, err := cli.ImagePull(ctx, cloneHelperImage, image.PullOptions{})
		if err != nil {
			return fmt.Errorf("pull %s: %w", cloneHelperImage, err)
		}
		_, _ = io.Copy(io.Discard, reader)
		_ = reader.Close()
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: cloneHelperImage,
		Cmd:   []string{"sh", "-c", "cd /src && tar cf - . | tar xpf - -C /dst"},
	}, &container.HostConfig{
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: srcVol, Target: "/src", ReadOnly: true},
			{Type: mount.TypeVolume, Source: dstVol, Target: "/dst"},
		},
		AutoRemove: false,
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("create clone helper: %w", err)
	}
	defer func() {
		_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start clone helper: %w", err)
	}
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("wait clone helper: %w", err)
		}
	case st := <-statusCh:
		if st.StatusCode != 0 {
			return fmt.Errorf("clone helper exited with code %d", st.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// volumeExists reports whether a Docker volume name is present.
func volumeExists(ctx context.Context, cli *client.Client, name string) bool {
	args := filters.NewArgs()
	args.Add("name", name)
	list, err := cli.VolumeList(ctx, volume.ListOptions{Filters: args})
	if err != nil {
		return false
	}
	for _, v := range list.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}
