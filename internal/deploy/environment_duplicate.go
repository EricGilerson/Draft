package deploy

import (
	"fmt"
	"strings"

	"Draft/internal/store"
)

// DuplicateEnvironment clones every node in sourceEnvironmentID into a brand
// new environment named newName: same labels/positions/TemplateID, a fresh
// UID per node, and every NodeSetting + EnvVar copied verbatim. {{draft.*}}
// expressions in copied values are re-resolved afterward since they depend on
// the node's UID/environment, which just changed. @{Label.ATTR} tokens are
// left untouched — they resolve dynamically at read time and will find the
// duplicated sibling by label within the new environment, since reference
// resolution is environment-scoped (see refs.go).
//
// Deployment history, routes, port leases, and Docker volumes are never
// copied — they start fresh for the new environment on first deploy.
//
// If duplication fails partway, the newly created environment (and whatever
// nodes were created under it) is deleted so no half-duplicated environment
// is left behind.
func (e *Engine) DuplicateEnvironment(sourceEnvironmentID uint, newName string) (*store.Environment, error) {
	sourceEnv, err := e.store.GetEnvironment(sourceEnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("source environment not found: %w", err)
	}
	sourceNodes, err := e.store.ListNodesByEnvironment(sourceEnvironmentID)
	if err != nil {
		return nil, err
	}

	newEnv, err := e.store.CreateEnvironment(sourceEnv.ProjectID, newName)
	if err != nil {
		return nil, err
	}

	if err := e.duplicateNodesInto(sourceNodes, newEnv); err != nil {
		_ = e.store.DeleteEnvironment(newEnv.ID)
		return nil, err
	}
	return newEnv, nil
}

// duplicateNodesInto clones sourceNodes into newEnv, then re-resolves every
// copied {{draft.*}} expression against each new node's fresh UID/environment.
func (e *Engine) duplicateNodesInto(sourceNodes []store.CanvasNode, newEnv *store.Environment) error {
	newNodeIDs := make([]string, 0, len(sourceNodes))
	for _, src := range sourceNodes {
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

		settings, err := e.store.GetNodeSettings(src.ID)
		if err != nil {
			return fmt.Errorf("read settings for %q: %w", src.Label, err)
		}
		for key, value := range settings {
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
	}

	// Second pass: every new node/label now exists, so {{draft.*}} expressions
	// (which depend on the node's own fresh UID) can be safely re-resolved.
	// @{Label.ATTR} tokens are left as-is.
	for _, nodeID := range newNodeIDs {
		if err := e.reresolveDraftExprsForNode(nodeID); err != nil {
			return err
		}
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
