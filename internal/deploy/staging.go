package deploy

import (
	"context"
	"log"
	"strings"
	"time"

	"Draft/internal/githooks"
	"Draft/internal/store"
)

// loadEffectiveSettings returns applied settings merged with staged overrides.
// Template-owned cmd/entrypoint/working_dir that still use {{draft.*}} in the
// template body are rehydrated against the current node identity so pack import
// (new UID) cannot leave Redis --requirepass on an old password while
// generated REDIS_URL is re-derived for the new identity.
func (e *Engine) loadEffectiveSettings(nodeID string) (map[string]string, error) {
	settings, err := e.store.EffectiveNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	return e.withRehydratedTemplateSettings(nodeID, settings), nil
}

// withRehydratedTemplateSettings returns a copy of settings with template-
// authored {{draft.*}} command fields resolved for nodeID. Does not write the
// store; restampTemplateOwnedIdentityValues persists the same values after import.
func (e *Engine) withRehydratedTemplateSettings(nodeID string, settings map[string]string) map[string]string {
	if settings == nil {
		return nil
	}
	out := make(map[string]string, len(settings)+3)
	for k, v := range settings {
		out[k] = v
	}
	node, err := e.store.GetNode(nodeID)
	if err != nil || node.TemplateID == 0 {
		return out
	}
	tpl, err := e.store.GetTemplate(node.TemplateID)
	if err != nil {
		return out
	}
	for key, raw := range map[string]string{
		"cmd_override":        tpl.CmdOverride,
		"entrypoint_override": tpl.Entrypoint,
		"working_dir":         tpl.WorkingDir,
	} {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.Contains(raw, "{{draft.") {
			continue
		}
		resolved, err := e.ResolveNodeTemplateExprs(nodeID, raw)
		if err != nil {
			continue
		}
		out[key] = resolved
	}
	return out
}

// loadEffectiveEnvVars returns applied env vars merged with staged changes.
func (e *Engine) loadEffectiveEnvVars(nodeID string) ([]store.EnvVar, error) {
	return e.store.EffectiveEnvVars(nodeID)
}

func (e *Engine) promoteStagedAfterSuccessfulDeploy(ctx context.Context, nodeID string, projectID uint) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
			if lastErr != nil {
				break
			}
		}
		if err := e.store.PromoteStagedToApplied(nodeID); err != nil {
			lastErr = err
			continue
		}
		lastErr = nil
		settings, _ := e.store.GetNodeSettings(nodeID)
		if settings == nil {
			return
		}
		if settings["git_branch"] != "" || settings["deploy_trigger"] != "" || settings["redeploy_on_pull"] != "" {
			trigger := settings["deploy_trigger"]
			if trigger == "" {
				trigger = "manual"
			}
			_ = githooks.SetDeployTrigger(ctx, e.store, nodeID, projectID, trigger)
			redeployOnPull := settings["redeploy_on_pull"] == "true"
			_ = githooks.SetRedeployOnPull(ctx, e.store, nodeID, projectID, redeployOnPull)
		}
		return
	}

	msg := "staged config was NOT promoted after deploy"
	if lastErr != nil {
		msg += ": " + lastErr.Error()
	}
	log.Printf("[deploy] promote failed for %s: %v", nodeID, lastErr)
	e.emitBuildLog(nodeID, "ERROR: "+msg+" — draft bar may still show pending changes; redeploy or discard/re-stage")
	if e.emit != nil {
		e.emit("deploy:promote-failed", map[string]any{
			"nodeId": nodeID,
			"error":  msg,
		})
	}
}

// StagedEnvVarChange describes a pending env var upsert or deletion.
type StagedEnvVarChange struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Scope  string `json:"scope"`
	Delete bool   `json:"delete"`
}

// NodeConfigStatus summarizes applied vs staged configuration for the UI.
type NodeConfigStatus struct {
	AppliedSettings        map[string]string    `json:"appliedSettings"`
	StagedSettings         map[string]string    `json:"stagedSettings"`
	StagedEnvChanges       []StagedEnvVarChange `json:"stagedEnvChanges"`
	HasStagedChanges       bool                 `json:"hasStagedChanges"`
	ActiveDeploymentStatus string               `json:"activeDeploymentStatus"`
}

func NodeConfigStatusFromStore(s *store.Store, nodeID string) (*NodeConfigStatus, error) {
	_ = s.StripImmediateStagedSettings(nodeID)
	_ = s.PruneNoOpStagedSettings(nodeID)
	_ = s.PruneNoOpStagedEnvVars(nodeID)

	applied, err := s.GetNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	if applied == nil {
		applied = map[string]string{}
	}
	staged, err := s.GetStagedNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	if staged == nil {
		staged = map[string]string{}
	}
	for key := range store.ImmediateSettingKeys {
		delete(staged, key)
	}
	staged = store.MeaningfulStagedSettings(applied, staged)
	stagedEnv, err := s.ListStagedEnvVarChanges(nodeID)
	if err != nil {
		return nil, err
	}
	envApplied, err := s.ListEnvVars(nodeID)
	if err != nil {
		return nil, err
	}
	stagedEnv = store.MeaningfulStagedEnvChanges(envApplied, stagedEnv)
	envChanges := make([]StagedEnvVarChange, 0, len(stagedEnv))
	for _, row := range stagedEnv {
		envChanges = append(envChanges, StagedEnvVarChange{
			Key:    row.Key,
			Value:  row.Value,
			Scope:  row.Scope,
			Delete: row.Delete,
		})
	}
	status := &NodeConfigStatus{
		AppliedSettings:  applied,
		StagedSettings:   staged,
		StagedEnvChanges: envChanges,
		HasStagedChanges: len(staged) > 0 || len(envChanges) > 0,
	}
	active, err := s.ActiveDeployment(nodeID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		status.ActiveDeploymentStatus = active.Status
	}
	return status, nil
}
