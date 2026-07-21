package deploy

import (
	"context"
	"fmt"

	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ReferenceDependent is another service whose env var still references the
// service being deleted. Values are left unchanged; deploy/preview will fail
// until the user fixes them.
type ReferenceDependent struct {
	SourceNodeID string `json:"sourceNodeId"`
	SourceLabel  string `json:"sourceLabel"`
	VarKey       string `json:"varKey"`
	Token        string `json:"token"`
}

// DeleteServicePreview summarizes what deleting a service will affect.
type DeleteServicePreview struct {
	Label              string               `json:"label"`
	IsRunning          bool                 `json:"isRunning"`
	ManagedVolumeCount int                  `json:"managedVolumeCount"`
	Dependents         []ReferenceDependent `json:"dependents"`
	// ActiveAliases lists labels of linked services in other environments that
	// still point at this root. Non-empty means delete will be blocked.
	ActiveAliases []string `json:"activeAliases,omitempty"`
}

// PreviewDeleteServiceFromStore builds a delete preview from SQLite only. It
// does not query Docker for live managed-volume sizes; use the daemon path
// when an accurate count matters.
func PreviewDeleteServiceFromStore(s *store.Store, nodeID string) (*DeleteServicePreview, error) {
	node, err := s.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	dependents, err := ListServiceDependentsFromStore(s, nodeID)
	if err != nil {
		return nil, err
	}
	if dependents == nil {
		dependents = []ReferenceDependent{}
	}
	active, err := s.ActiveDeployment(nodeID)
	if err != nil {
		return nil, err
	}
	settings, _ := s.GetNodeSettings(nodeID)
	managedCount := 0
	if settings != nil {
		managedCount = countConfiguredManagedVolumes(settings["volume_mounts"])
	}
	preview := &DeleteServicePreview{
		Label:              node.Label,
		IsRunning:          active != nil,
		ManagedVolumeCount: managedCount,
		Dependents:         dependents,
	}
	// Note active linkers when this is a root (store-only scan).
	nodes, err := s.ListNodes(node.ProjectID)
	if err == nil {
		for _, n := range nodes {
			if n.ID == nodeID {
				continue
			}
			st, _ := s.GetNodeSettings(n.ID)
			if link := ParseServiceLink(st[SettingServiceLink]); link != nil && link.RootNodeID == nodeID {
				preview.ActiveAliases = append(preview.ActiveAliases, n.Label)
			}
		}
	}
	return preview, nil
}

// PreviewDeleteService returns the label, runtime state, managed-volume count,
// and other services that still reference this one.
func (e *Engine) PreviewDeleteService(ctx context.Context, nodeID string) (*DeleteServicePreview, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	dependents, err := e.ListServiceDependents(nodeID)
	if err != nil {
		return nil, err
	}
	if dependents == nil {
		dependents = []ReferenceDependent{}
	}
	active, err := e.store.ActiveDeployment(nodeID)
	if err != nil {
		return nil, err
	}
	pid := node.ProjectID
	vols, err := e.ListManagedVolumes(ctx, &pid, nodeID)
	if err != nil {
		return nil, err
	}
	preview := &DeleteServicePreview{
		Label:              node.Label,
		IsRunning:          active != nil,
		ManagedVolumeCount: len(vols),
		Dependents:         dependents,
	}
	if linkers, err := e.ListLinkers(nodeID); err == nil {
		for _, l := range linkers {
			preview.ActiveAliases = append(preview.ActiveAliases, l.Label)
		}
	}
	return preview, nil
}

// DeleteService stops containers, removes routes and store rows for nodeID, and
// leaves other services' reference tokens untouched. Draft-managed Docker
// volumes are kept so orphaned data can be surfaced later.
//
// Roots that still have linked aliases in other environments cannot be deleted
// until those aliases are unlinked or promoted. Deleting an alias disconnects
// its shared network attachment first.
func (e *Engine) DeleteService(ctx context.Context, nodeID string) error {
	if _, err := e.store.GetNode(nodeID); err != nil {
		return err
	}
	if err := e.guardRootDelete(nodeID); err != nil {
		return err
	}

	// If this node is an alias, drop multi-network attach before removing rows.
	if link, _ := e.GetServiceLink(nodeID); link != nil {
		_ = e.DisconnectServiceLinkNetwork(ctx, nodeID)
	}

	e.mu.Lock()
	if cancel, ok := e.active[nodeID]; ok {
		cancel()
		delete(e.active, nodeID)
	}
	e.mu.Unlock()

	deployments, err := e.store.ListDeployments(nodeID)
	if err != nil {
		return err
	}

	settings, _ := e.store.GetNodeSettings(nodeID)
	stopTimeout := stopTimeoutForSettings(settings)

	cli, cliErr := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if cliErr == nil {
		defer cli.Close()
	}

	for _, d := range deployments {
		if cliErr == nil && d.ContainerID != "" {
			_ = cli.ContainerStop(ctx, d.ContainerID, container.StopOptions{Timeout: &stopTimeout})
			_ = removeContainerAndWait(ctx, cli, d.ContainerID)
		}
		if cliErr == nil && d.ImageTag != "" {
			_ = removeDraftDeploymentImage(ctx, cli, d.ImageTag)
		}
		if d.Hostname != "" {
			_ = e.router.Unregister(d.Hostname)
		}
	}
	e.removeBuildLogs(deployments)

	if err := e.router.UnregisterNode(nodeID); err != nil {
		return fmt.Errorf("unregister routes: %w", err)
	}
	if err := e.store.DeleteEnvVarsByNode(nodeID); err != nil {
		return err
	}
	if err := e.store.DeleteStagedChanges(nodeID); err != nil {
		return err
	}
	if err := e.store.DeleteDeploymentsByNode(nodeID); err != nil {
		return err
	}
	if err := e.store.DeleteNodeSettings(nodeID); err != nil {
		return err
	}
	return e.store.DeleteNode(nodeID)
}

// DeleteServiceFromStore removes a node's store rows without stopping containers
// or unregistering routes. Used when the daemon is unavailable or stale.
func DeleteServiceFromStore(s *store.Store, nodeID string) error {
	if _, err := s.GetNode(nodeID); err != nil {
		return err
	}
	if err := s.DeleteEnvVarsByNode(nodeID); err != nil {
		return err
	}
	if err := s.DeleteStagedChanges(nodeID); err != nil {
		return err
	}
	if err := s.DeleteDeploymentsByNode(nodeID); err != nil {
		return err
	}
	if err := s.DeleteNodeSettings(nodeID); err != nil {
		return err
	}
	return s.DeleteNode(nodeID)
}

func countConfiguredManagedVolumes(raw string) int {
	count := 0
	for _, spec := range ParseVolumeSpecs(raw) {
		t := spec.Type
		if t == "" {
			t = VolumeTypeBind
		}
		if t == VolumeTypeVolume {
			count++
		}
	}
	return count
}
