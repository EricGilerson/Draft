package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"Draft/internal/store"
)

// DuplicateEnvironmentResult is returned by DuplicateEnvironmentWithChoices so
// the UI can open the new environment and surface optional stack-start outcomes
// without a second round-trip (same shape as SandboxCreateResult).
type DuplicateEnvironmentResult struct {
	Environment *store.Environment      `json:"environment"`
	Stack       *EnvironmentStackResult `json:"stack,omitempty"`
	Started     bool                    `json:"started"`
	// StartError is set when duplication succeeded but stack start failed
	// (hard error or one or more services failed to kick off).
	StartError string `json:"startError,omitempty"`
}

// DuplicateEnvironment clones every node in sourceEnvironmentID into a brand
// new environment named newName: same labels/positions/TemplateID, a fresh
// UID per node, and every NodeSetting + EnvVar copied verbatim. {{draft.*}}
// expressions in copied values are re-resolved afterward since they depend on
// the node's UID/environment, which just changed. @{Label.ATTR} tokens are
// left untouched — they resolve dynamically at read time and will find the
// duplicated sibling by label within the new environment, since reference
// resolution is environment-scoped (see refs.go).
//
// choices control per-stateful-service data handling (fresh / share / clone).
// Nodes without a choice default to fresh. Deployment history, routes, and
// port leases are never copied. Does not start services (see
// DuplicateEnvironmentWithChoices with startAfter).
//
// If duplication fails partway, the newly created environment (and whatever
// nodes were created under it) is deleted so no half-duplicated environment
// is left behind.
func (e *Engine) DuplicateEnvironment(sourceEnvironmentID uint, newName string, choices ...ServiceDataChoice) (*store.Environment, error) {
	res, err := e.DuplicateEnvironmentWithChoices(context.Background(), sourceEnvironmentID, newName, choices, false, nil)
	if err != nil {
		return nil, err
	}
	return res.Environment, nil
}

// DuplicateEnvironmentWithChoices is the full API with context for clone I/O.
// When startAfter is true, copied services are deployed after materialize
// (async kickoff per node). Duplication still succeeds when start fails —
// see DuplicateEnvironmentResult.StartError.
//
// repositories, when non-empty, resolves each ref and stamps git_branch on
// independent copies that match those repo roots (same pin plumbing as
// sandboxes). Shared/linked aliases are skipped. Empty/nil keeps whatever
// git_branch was copied from the source.
func (e *Engine) DuplicateEnvironmentWithChoices(ctx context.Context, sourceEnvironmentID uint, newName string, choices []ServiceDataChoice, startAfter bool, repositories []SandboxRepositoryRef) (*DuplicateEnvironmentResult, error) {
	sourceEnv, err := e.store.GetEnvironment(sourceEnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("source environment not found: %w", err)
	}
	sourceNodes, err := e.store.ListNodesByEnvironment(sourceEnvironmentID)
	if err != nil {
		return nil, err
	}

	choiceBySource := map[string]ServiceDataChoice{}
	for _, c := range choices {
		if c.SourceNodeID == "" {
			continue
		}
		if c.Mode == "" {
			c.Mode = ServiceDataFresh
		}
		if c.Consistency == "" {
			c.Consistency = CloneConsistent
		}
		choiceBySource[c.SourceNodeID] = c
	}

	pins, err := e.resolveExplicitRepositoryPins(ctx, repositories)
	if err != nil {
		return nil, err
	}

	newEnv, err := e.store.CreateEnvironment(sourceEnv.ProjectID, newName)
	if err != nil {
		return nil, err
	}

	if err := e.duplicateNodesInto(ctx, sourceNodes, newEnv, choiceBySource); err != nil {
		_ = e.store.DeleteEnvironment(newEnv.ID)
		return nil, err
	}

	if err := e.pinCopiedNodesToRepositories(ctx, sourceNodes, newEnv.ID, pins); err != nil {
		_ = e.store.DeleteEnvironment(newEnv.ID)
		return nil, err
	}

	out := &DuplicateEnvironmentResult{Environment: newEnv}
	if startAfter {
		stack, startErr := e.RunEnvironmentStack(ctx, newEnv.ID, StackStart)
		applyStackStartResult(out, stack, startErr)
	}
	return out, nil
}

// applyStackStartResult fills Started / StartError / Stack on a duplicate
// result from RunEnvironmentStack. Materialize already succeeded.
func applyStackStartResult(out *DuplicateEnvironmentResult, stack *EnvironmentStackResult, startErr error) {
	out.Stack = stack
	if startErr != nil {
		out.StartError = startErr.Error()
		return
	}
	if stack != nil && stack.Failed > 0 {
		out.StartError = formatStackStartError(stack)
		out.Started = stack.Succeeded > 0
		return
	}
	out.Started = true
}

func formatStackStartError(stack *EnvironmentStackResult) string {
	if stack == nil {
		return "start failed"
	}
	lines := make([]string, 0, 4)
	for _, r := range stack.Results {
		if r.Error == "" {
			continue
		}
		label := r.Label
		if label == "" {
			label = r.NodeID
		}
		lines = append(lines, label+": "+r.Error)
		if len(lines) >= 4 {
			break
		}
	}
	head := fmt.Sprintf("%d of %d services failed to start", stack.Failed, stack.Total)
	if len(lines) == 0 {
		return head
	}
	return head + " — " + strings.Join(lines, "; ")
}

// duplicateNodesInto clones sourceNodes into newEnv, applying share/clone/fresh,
// then re-resolves every copied {{draft.*}} expression against each new node's
// fresh UID/environment.
func (e *Engine) duplicateNodesInto(ctx context.Context, sourceNodes []store.CanvasNode, newEnv *store.Environment, choiceBySource map[string]ServiceDataChoice) error {
	// sourceNodeID → newNodeID for clone volume wiring after all nodes exist.
	type pendingClone struct {
		newNodeID    string
		sourceNodeID string
		paths        []string
		consistency  CloneConsistency
	}
	type pendingRestamp struct {
		newNodeID    string
		sourceNodeID string
	}
	var clones []pendingClone
	var restamps []pendingRestamp
	newNodeIDs := make([]string, 0, len(sourceNodes))

	for _, src := range sourceNodes {
		choice := choiceBySource[src.ID]
		if choice.Mode == "" {
			choice.Mode = ServiceDataFresh
		}

		// If the source itself is an alias, always re-link to the same root
		// (never chain through the source alias).
		srcSettings, err := e.store.GetNodeSettings(src.ID)
		if err != nil {
			return fmt.Errorf("read settings for %q: %w", src.Label, err)
		}
		srcLink := ParseServiceLink(srcSettings[SettingServiceLink])

		newID := genNodeID()
		newNode, err := e.store.CreateNode(&store.CanvasNode{
			ID:            newID,
			Label:         src.Label,
			ProjectID:     newEnv.ProjectID,
			EnvironmentID: newEnv.ID,
			X:             src.X,
			Y:             src.Y,
			TemplateID:    src.TemplateID,
		})
		if err != nil {
			return fmt.Errorf("duplicate service %q: %w", src.Label, err)
		}
		newNodeIDs = append(newNodeIDs, newID)

		if _, err := e.store.EnsureNodeUID(newNode.ID); err != nil {
			return fmt.Errorf("assign uid for %q: %w", src.Label, err)
		}

		// Copy settings first, then adjust for mode.
		for key, value := range srcSettings {
			if err := e.store.SetNodeSetting(newNode.ID, key, value); err != nil {
				return fmt.Errorf("copy setting %q for %q: %w", key, src.Label, err)
			}
		}

		vars, err := e.store.ListEnvVars(src.ID)
		if err != nil {
			return fmt.Errorf("read env vars for %q: %w", src.Label, err)
		}
		for _, v := range vars {
			v.NodeID = newNode.ID
			if err := e.store.UpsertEnvVar(v); err != nil {
				return fmt.Errorf("copy env var %q for %q: %w", v.Key, src.Label, err)
			}
		}

		switch {
		case srcLink != nil:
			// Preserve link to the original root (not the source alias).
			if err := e.SetServiceLink(newNode.ID, srcLink.RootNodeID); err != nil {
				return fmt.Errorf("relink %q: %w", src.Label, err)
			}
		case choice.Mode == ServiceDataShare:
			if err := e.SetServiceLink(newNode.ID, src.ID); err != nil {
				return fmt.Errorf("share %q: %w", src.Label, err)
			}
		case choice.Mode == ServiceDataClone:
			// Keep copied volume_mounts but force auto-name (clear explicit sources)
			// then clone data into new volumes after UID is assigned.
			paths := managedVolumePaths(srcSettings)
			if len(paths) > 0 {
				specs := make([]VolumeSpec, 0, len(paths))
				for _, p := range paths {
					specs = append(specs, VolumeSpec{Type: VolumeTypeVolume, ContainerPath: p})
				}
				raw, _ := json.Marshal(specs)
				_ = e.store.SetNodeSetting(newNode.ID, "volume_mounts", string(raw))
				_ = e.store.SetNodeSetting(newNode.ID, SettingServiceLink, "")
				clones = append(clones, pendingClone{
					newNodeID:    newNode.ID,
					sourceNodeID: src.ID,
					paths:        paths,
					consistency:  choice.Consistency,
				})
			}
			restamps = append(restamps, pendingRestamp{newNodeID: newNode.ID, sourceNodeID: src.ID})
		default: // fresh
			// Clear any accidental service_link copy; keep volume_mounts as
			// auto-named (strip explicit sources so new env gets its own volumes).
			_ = e.store.SetNodeSetting(newNode.ID, SettingServiceLink, "")
			if paths := managedVolumePaths(srcSettings); len(paths) > 0 {
				specs := make([]VolumeSpec, 0, len(paths))
				for _, p := range paths {
					specs = append(specs, VolumeSpec{Type: VolumeTypeVolume, ContainerPath: p})
				}
				// Preserve non-volume mounts from original.
				for _, spec := range ParseVolumeSpecs(srcSettings["volume_mounts"]) {
					t := spec.Type
					if t == "" {
						t = VolumeTypeBind
					}
					if t == VolumeTypeVolume {
						continue
					}
					specs = append(specs, spec)
				}
				raw, _ := json.Marshal(specs)
				_ = e.store.SetNodeSetting(newNode.ID, "volume_mounts", string(raw))
			}
			restamps = append(restamps, pendingRestamp{newNodeID: newNode.ID, sourceNodeID: src.ID})
		}
	}

	// Second pass: re-resolve {{draft.*}} against fresh UIDs.
	for _, nodeID := range newNodeIDs {
		if err := e.reresolveDraftExprsForNode(nodeID); err != nil {
			return err
		}
	}

	// Third pass: refresh concrete template-owned generated values copied from
	// the source node so independent services get their own credentials/URLs.
	for _, item := range restamps {
		if err := e.restampTemplateOwnedGeneratedValues(item.newNodeID, item.sourceNodeID); err != nil {
			return err
		}
	}

	// Fourth pass: volume clones (need Docker + resolved settings).
	for _, c := range clones {
		for _, path := range c.paths {
			if _, err := e.CloneVolumeData(ctx, c.newNodeID, c.sourceNodeID, path, c.consistency); err != nil {
				return fmt.Errorf("clone volume %s for new node: %w", path, err)
			}
		}
	}

	// Final pass: attach shared roots to the new environment network when roots
	// are running.
	for _, nodeID := range newNodeIDs {
		link, err := e.GetServiceLink(nodeID)
		if err != nil || link == nil {
			continue
		}
		_ = e.EnsureServiceLinkNetworks(ctx, link.RootNodeID)
	}

	return nil
}

// reresolveDraftExprsForNode re-expands {{draft.*}} expressions in every
// NodeSetting and EnvVar value that still contains one, writing the result
// back. Used after duplication, where copied values were resolved against
// the source node's (now-stale) UID/environment.
func (e *Engine) reresolveDraftExprsForNode(nodeID string) error {
	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		return err
	}
	for key, value := range settings {
		if !strings.Contains(value, "{{draft.") {
			continue
		}
		resolved, err := e.ResolveNodeTemplateExprs(nodeID, value)
		if err != nil {
			return fmt.Errorf("re-resolve setting %q: %w", key, err)
		}
		if err := e.store.SetNodeSetting(nodeID, key, resolved); err != nil {
			return err
		}
	}

	vars, err := e.store.ListEnvVars(nodeID)
	if err != nil {
		return err
	}
	for _, v := range vars {
		if !strings.Contains(v.Value, "{{draft.") {
			continue
		}
		resolved, err := e.ResolveNodeTemplateExprs(nodeID, v.Value)
		if err != nil {
			return fmt.Errorf("re-resolve env %q: %w", v.Key, err)
		}
		v.Value = resolved
		if err := e.store.UpsertEnvVar(v); err != nil {
			return err
		}
	}
	return nil
}
