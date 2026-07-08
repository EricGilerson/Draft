package deploy

import (
	"context"

	"Draft/internal/githooks"
	"Draft/internal/store"
)

// loadEffectiveSettings returns applied settings merged with staged overrides.
func (e *Engine) loadEffectiveSettings(nodeID string) (map[string]string, error) {
	return e.store.EffectiveNodeSettings(nodeID)
}

// loadEffectiveEnvVars returns applied env vars merged with staged changes.
func (e *Engine) loadEffectiveEnvVars(nodeID string) ([]store.EnvVar, error) {
	return e.store.EffectiveEnvVars(nodeID)
}

func (e *Engine) promoteStagedAfterSuccessfulDeploy(ctx context.Context, nodeID string, projectID uint) {
	if err := e.store.PromoteStagedToApplied(nodeID); err != nil {
		e.emitBuildLog(nodeID, "    Warning: failed to promote staged settings: "+err.Error())
		return
	}
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
