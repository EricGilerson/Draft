package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"Draft/internal/store"
)

// Sync scopes and apply modes.
const (
	SyncScopeService     = "service"
	SyncScopeEnvironment = "environment"

	SyncModeStage            = "stage"
	SyncModeStageAndRedeploy = "stageAndRedeploy"

	// Diff actions
	SyncActionEqual   = "equal"
	SyncActionSet     = "set"
	SyncActionRestamp = "restamp"
	SyncActionSkip    = "skip"
)

// Settings never copied between environments.
var syncNeverCopySettings = map[string]bool{
	SettingServiceLink: true,
	"git_repo_root":    true,
}

// Git automation / path keys excluded from default settings sync (often env-specific).
// service_root and env_file are included when they differ (immediate apply).
var syncExcludeSettings = map[string]bool{
	"git_branch":       true,
	"deploy_trigger":   true,
	"redeploy_on_pull": true,
	"git_stream":       true,
}

// SyncRequest describes a config sync from source → target.
type SyncRequest struct {
	Scope               string `json:"scope"` // service | environment
	SourceEnvironmentID uint   `json:"sourceEnvironmentId"`
	TargetEnvironmentID uint   `json:"targetEnvironmentId"`
	SourceNodeID        string `json:"sourceNodeId,omitempty"`
	TargetNodeID        string `json:"targetNodeId,omitempty"`
	IncludeSettings     bool   `json:"includeSettings"`
	IncludeEnv          bool   `json:"includeEnv"`
}

// SyncSettingDiff is one settings key difference.
type SyncSettingDiff struct {
	Key             string   `json:"key"`
	SourceValue     string   `json:"sourceValue"`
	TargetApplied   string   `json:"targetApplied"`
	TargetStaged    string   `json:"targetStaged,omitempty"`
	TargetEffective string   `json:"targetEffective"`
	Action          string   `json:"action"` // equal | set | skip
	Reason          string   `json:"reason,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// SyncEnvDiff is one env-var difference.
type SyncEnvDiff struct {
	Key            string `json:"key"`
	SourceValue    string `json:"sourceValue"`
	SourceScope    string `json:"sourceScope"`
	SourceSource   string `json:"sourceSource"`
	SourceSecret   bool   `json:"sourceSecret"`
	TargetValue    string `json:"targetValue"`
	TargetScope    string `json:"targetScope"`
	TargetSource   string `json:"targetSource"`
	TargetSecret   bool   `json:"targetSecret"`
	ProposedValue  string `json:"proposedValue,omitempty"`
	ProposedScope  string `json:"proposedScope,omitempty"`
	Action         string `json:"action"` // equal | set | restamp | skip
	Reason         string `json:"reason,omitempty"`
}

// SyncServicePreview is the per-service diff for one matched pair.
type SyncServicePreview struct {
	Label           string            `json:"label"`
	SourceNodeID    string            `json:"sourceNodeId"`
	TargetNodeID    string            `json:"targetNodeId"`
	Skipped         bool              `json:"skipped"`
	SkipReason      string            `json:"skipReason,omitempty"`
	Settings        []SyncSettingDiff `json:"settings"`
	Env             []SyncEnvDiff     `json:"env"`
	Warnings        []SettingsWarning `json:"warnings"`
	ActionableCount int               `json:"actionableCount"`
}

// SyncPreview is the full read-only plan before apply.
type SyncPreview struct {
	Scope               string               `json:"scope"`
	SourceEnvironmentID uint                 `json:"sourceEnvironmentId"`
	TargetEnvironmentID uint                 `json:"targetEnvironmentId"`
	SourceEnvName       string               `json:"sourceEnvName"`
	TargetEnvName       string               `json:"targetEnvName"`
	IncludeSettings     bool                 `json:"includeSettings"`
	IncludeEnv          bool                 `json:"includeEnv"`
	Services            []SyncServicePreview `json:"services"`
	UnmatchedSource     []string             `json:"unmatchedSource"`
	UnmatchedTarget     []string             `json:"unmatchedTarget"`
	ActionableCount     int                  `json:"actionableCount"`
}

// SyncApplyNodeResult is the per-node outcome of ApplySync.
type SyncApplyNodeResult struct {
	NodeID    string `json:"nodeId"`
	Label     string `json:"label"`
	Staged    bool   `json:"staged"`
	Redeployed bool  `json:"redeployed"`
	Error     string `json:"error,omitempty"`
}

// SyncApplyResult summarizes ApplySync.
type SyncApplyResult struct {
	Mode            string                `json:"mode"`
	ActionableCount int                   `json:"actionableCount"`
	Results         []SyncApplyNodeResult `json:"results"`
}

// PreviewSync builds a read-only diff for the given request. No writes.
func (e *Engine) PreviewSync(req SyncRequest) (*SyncPreview, error) {
	return e.buildSyncPreview(req)
}

// ApplySync stages (and optionally redeploys) based on the same plan as PreviewSync.
func (e *Engine) ApplySync(ctx context.Context, req SyncRequest, mode string) (*SyncApplyResult, error) {
	switch mode {
	case SyncModeStage, SyncModeStageAndRedeploy:
	default:
		return nil, fmt.Errorf("unknown sync mode %q", mode)
	}
	if !req.IncludeSettings && !req.IncludeEnv {
		return nil, fmt.Errorf("enable settings sync and/or env sync")
	}

	preview, err := e.buildSyncPreview(req)
	if err != nil {
		return nil, err
	}
	out := &SyncApplyResult{
		Mode:            mode,
		ActionableCount: preview.ActionableCount,
		Results:         []SyncApplyNodeResult{},
	}
	if preview.ActionableCount == 0 {
		return out, nil
	}

	for _, svc := range preview.Services {
		if svc.Skipped || svc.ActionableCount == 0 {
			continue
		}
		res := SyncApplyNodeResult{NodeID: svc.TargetNodeID, Label: svc.Label}
		if err := e.applyServiceSync(svc, req); err != nil {
			res.Error = err.Error()
			out.Results = append(out.Results, res)
			continue
		}
		res.Staged = true
		if mode == SyncModeStageAndRedeploy {
			if err := e.Deploy(ctx, svc.TargetNodeID); err != nil {
				res.Error = "staged, but redeploy failed: " + err.Error()
			} else {
				res.Redeployed = true
			}
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

func (e *Engine) buildSyncPreview(req SyncRequest) (*SyncPreview, error) {
	if e.store == nil {
		return nil, fmt.Errorf("store is not available")
	}
	if !req.IncludeSettings && !req.IncludeEnv {
		return nil, fmt.Errorf("enable settings sync and/or env sync")
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = SyncScopeService
	}
	if scope != SyncScopeService && scope != SyncScopeEnvironment {
		return nil, fmt.Errorf("unknown sync scope %q", scope)
	}
	if req.SourceEnvironmentID == 0 || req.TargetEnvironmentID == 0 {
		return nil, fmt.Errorf("source and target environments are required")
	}
	if req.SourceEnvironmentID == req.TargetEnvironmentID && scope == SyncScopeEnvironment {
		return nil, fmt.Errorf("source and target environments must differ")
	}

	srcEnv, err := e.store.GetEnvironment(req.SourceEnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("source environment: %w", err)
	}
	tgtEnv, err := e.store.GetEnvironment(req.TargetEnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("target environment: %w", err)
	}
	if srcEnv.ProjectID != tgtEnv.ProjectID {
		return nil, fmt.Errorf("environments must belong to the same project")
	}

	pairs, unmatchedSrc, unmatchedTgt, err := e.resolveSyncPairs(req, srcEnv, tgtEnv)
	if err != nil {
		return nil, err
	}

	out := &SyncPreview{
		Scope:               scope,
		SourceEnvironmentID: srcEnv.ID,
		TargetEnvironmentID: tgtEnv.ID,
		SourceEnvName:       srcEnv.Name,
		TargetEnvName:       tgtEnv.Name,
		IncludeSettings:     req.IncludeSettings,
		IncludeEnv:          req.IncludeEnv,
		Services:            make([]SyncServicePreview, 0, len(pairs)),
		UnmatchedSource:     unmatchedSrc,
		UnmatchedTarget:     unmatchedTgt,
	}

	for _, pair := range pairs {
		svc, err := e.diffSyncPair(pair.source, pair.target, req)
		if err != nil {
			return nil, err
		}
		out.Services = append(out.Services, *svc)
		out.ActionableCount += svc.ActionableCount
	}

	sort.Slice(out.Services, func(i, j int) bool {
		return strings.ToLower(out.Services[i].Label) < strings.ToLower(out.Services[j].Label)
	})
	return out, nil
}

type syncPair struct {
	source store.CanvasNode
	target store.CanvasNode
}

func (e *Engine) resolveSyncPairs(req SyncRequest, srcEnv, tgtEnv *store.Environment) (pairs []syncPair, unmatchedSrc, unmatchedTgt []string, err error) {
	srcNodes, err := e.store.ListNodesByEnvironment(srcEnv.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	tgtNodes, err := e.store.ListNodesByEnvironment(tgtEnv.ID)
	if err != nil {
		return nil, nil, nil, err
	}

	if strings.TrimSpace(req.Scope) == SyncScopeService || req.Scope == "" {
		srcID := strings.TrimSpace(req.SourceNodeID)
		tgtID := strings.TrimSpace(req.TargetNodeID)
		if srcID == "" || tgtID == "" {
			return nil, nil, nil, fmt.Errorf("sourceNodeId and targetNodeId are required for service sync")
		}
		var src, tgt *store.CanvasNode
		for i := range srcNodes {
			if srcNodes[i].ID == srcID {
				src = &srcNodes[i]
				break
			}
		}
		for i := range tgtNodes {
			if tgtNodes[i].ID == tgtID {
				tgt = &tgtNodes[i]
				break
			}
		}
		// Allow same-env service sync only when different nodes (unusual but ok).
		if src == nil {
			// Source might be in source env only — also try get by id
			n, gerr := e.store.GetNode(srcID)
			if gerr != nil {
				return nil, nil, nil, fmt.Errorf("source service not found")
			}
			if n.EnvironmentID != srcEnv.ID {
				return nil, nil, nil, fmt.Errorf("source service is not in the source environment")
			}
			src = n
		}
		if tgt == nil {
			n, gerr := e.store.GetNode(tgtID)
			if gerr != nil {
				return nil, nil, nil, fmt.Errorf("target service not found")
			}
			if n.EnvironmentID != tgtEnv.ID {
				return nil, nil, nil, fmt.Errorf("target service is not in the target environment")
			}
			tgt = n
		}
		if src.ID == tgt.ID {
			return nil, nil, nil, fmt.Errorf("source and target services must differ")
		}
		return []syncPair{{source: *src, target: *tgt}}, nil, nil, nil
	}

	// Environment scope: match by normalized label.
	tgtByLabel := map[string]store.CanvasNode{}
	for _, n := range tgtNodes {
		key := syncLabelKey(n.Label)
		if key == "" {
			continue
		}
		tgtByLabel[key] = n
	}
	srcByLabel := map[string]store.CanvasNode{}
	for _, n := range srcNodes {
		key := syncLabelKey(n.Label)
		if key == "" {
			continue
		}
		srcByLabel[key] = n
	}
	for key, src := range srcByLabel {
		if tgt, ok := tgtByLabel[key]; ok {
			pairs = append(pairs, syncPair{source: src, target: tgt})
			delete(tgtByLabel, key)
		} else {
			unmatchedSrc = append(unmatchedSrc, src.Label)
		}
	}
	for _, tgt := range tgtByLabel {
		unmatchedTgt = append(unmatchedTgt, tgt.Label)
	}
	sort.Strings(unmatchedSrc)
	sort.Strings(unmatchedTgt)
	return pairs, unmatchedSrc, unmatchedTgt, nil
}

func syncLabelKey(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	return strings.Trim(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s), "-")
}

func (e *Engine) diffSyncPair(source, target store.CanvasNode, req SyncRequest) (*SyncServicePreview, error) {
	out := &SyncServicePreview{
		Label:        target.Label,
		SourceNodeID: source.ID,
		TargetNodeID: target.ID,
		Settings:     []SyncSettingDiff{},
		Env:          []SyncEnvDiff{},
		Warnings:     []SettingsWarning{},
	}

	srcSettings, err := e.store.EffectiveNodeSettings(source.ID)
	if err != nil {
		return nil, err
	}
	if ParseServiceLink(srcSettings[SettingServiceLink]) != nil {
		// Diffing from an alias: use root's settings/env for content, but keep
		// source node id for pairing. Prefer root for settings/env read.
		link := ParseServiceLink(srcSettings[SettingServiceLink])
		rootSettings, rerr := e.store.EffectiveNodeSettings(link.RootNodeID)
		if rerr == nil {
			srcSettings = rootSettings
		}
	}

	tgtApplied, err := e.store.GetNodeSettings(target.ID)
	if err != nil {
		return nil, err
	}
	if tgtApplied == nil {
		tgtApplied = map[string]string{}
	}
	tgtStaged, err := e.store.GetStagedNodeSettings(target.ID)
	if err != nil {
		return nil, err
	}
	if tgtStaged == nil {
		tgtStaged = map[string]string{}
	}
	tgtEffective := store.MergeNodeSettings(tgtApplied, tgtStaged)
	for key := range store.ImmediateSettingKeys {
		if v, ok := tgtApplied[key]; ok {
			tgtEffective[key] = v
		} else {
			delete(tgtEffective, key)
		}
	}

	if ParseServiceLink(tgtEffective[SettingServiceLink]) != nil {
		out.Skipped = true
		out.SkipReason = "Target is a linked (shared) service — edit the root or unlink first"
		return out, nil
	}

	// Source effective may still be alias after root hop for settings.
	srcEffective := srcSettings
	if srcEffective == nil {
		srcEffective = map[string]string{}
	}

	if req.IncludeSettings {
		diffs, warns, err := e.diffSettingsForSync(source.ID, target.ID, srcEffective, tgtApplied, tgtStaged, tgtEffective)
		if err != nil {
			return nil, err
		}
		out.Settings = diffs
		out.Warnings = append(out.Warnings, warns...)
	}

	if req.IncludeEnv {
		envDiffs, err := e.diffEnvForSync(source, target)
		if err != nil {
			return nil, err
		}
		out.Env = envDiffs
	}

	for _, d := range out.Settings {
		if d.Action == SyncActionSet {
			out.ActionableCount++
		}
	}
	for _, d := range out.Env {
		if d.Action == SyncActionSet || d.Action == SyncActionRestamp {
			out.ActionableCount++
		}
	}
	return out, nil
}

func (e *Engine) diffSettingsForSync(
	sourceNodeID, targetNodeID string,
	srcEffective, tgtApplied, tgtStaged, tgtEffective map[string]string,
) ([]SyncSettingDiff, []SettingsWarning, error) {
	keys := map[string]struct{}{}
	for k := range srcEffective {
		keys[k] = struct{}{}
	}
	for k := range tgtEffective {
		keys[k] = struct{}{}
	}

	var diffs []SyncSettingDiff
	var warnings []SettingsWarning
	proposedForPreview := map[string]string{}

	for key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		srcVal := srcEffective[key]
		// Normalize volume_mounts for both sides before compare/copy.
		if key == "volume_mounts" {
			srcVal = normalizeVolumeMountsForSync(srcVal)
		}
		tgtEff := tgtEffective[key]
		if key == "volume_mounts" {
			tgtEff = normalizeVolumeMountsForSync(tgtEff)
		}
		tgtApp := tgtApplied[key]
		tgtStg := tgtStaged[key]

		diff := SyncSettingDiff{
			Key:             key,
			SourceValue:     srcVal,
			TargetApplied:   tgtApp,
			TargetStaged:    tgtStg,
			TargetEffective: tgtEff,
			Action:          SyncActionEqual,
		}

		if syncNeverCopySettings[key] {
			if !store.SettingValuesEqual(srcVal, tgtEff) {
				diff.Action = SyncActionSkip
				diff.Reason = "identity / cross-env link — not synced"
				diffs = append(diffs, diff)
			}
			continue
		}
		if syncExcludeSettings[key] {
			if !store.SettingValuesEqual(srcVal, tgtEff) {
				diff.Action = SyncActionSkip
				diff.Reason = "git/deploy automation is excluded from sync by default"
				diffs = append(diffs, diff)
			}
			continue
		}

		// Target-only keys: leave them (no delete).
		if strings.TrimSpace(srcVal) == "" && strings.TrimSpace(tgtEff) != "" {
			// Source missing: not an add from source. Skip silently unless values
			// differ with empty source meaning "clear" — we do not clear.
			continue
		}

		if store.SettingValuesEqual(srcVal, tgtEff) {
			continue // equal — omit from preview noise
		}

		// Source has a value different from target effective → set.
		if strings.TrimSpace(srcVal) == "" {
			continue
		}

		diff.Action = SyncActionSet
		if key == "volume_mounts" {
			diff.SourceValue = srcVal
			diff.Warnings = append(diff.Warnings, "Managed volumes are re-auto-named for the target environment")
		}
		if key == "service_port" {
			diff.Warnings = append(diff.Warnings, "Port change applies on next deploy; routes and references may need updating")
		}
		if key == "host_port" {
			diff.Warnings = append(diff.Warnings, "Host port may collide with another service on this machine")
		}
		if tgtStg != "" && !store.SettingValuesEqual(tgtStg, srcVal) {
			diff.Warnings = append(diff.Warnings, "Overwrites an existing staged value on the target")
		}
		proposedForPreview[key] = srcVal
		diffs = append(diffs, diff)
	}

	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Key < diffs[j].Key })

	if len(proposedForPreview) > 0 {
		preview, err := PreviewStagedChangesFromStore(e.store, targetNodeID, proposedForPreview)
		if err == nil && preview != nil {
			warnings = append(warnings, preview.Warnings...)
			for _, er := range preview.Errors {
				warnings = append(warnings, SettingsWarning{
					Code:    er.Code,
					Message: "Error: " + er.Message,
					Field:   er.Field,
				})
			}
		}
	}
	_ = sourceNodeID
	return diffs, warnings, nil
}

func (e *Engine) diffEnvForSync(source, target store.CanvasNode) ([]SyncEnvDiff, error) {
	srcNodeID := source.ID
	srcSettings, _ := e.store.GetNodeSettings(source.ID)
	if link := ParseServiceLink(srcSettings[SettingServiceLink]); link != nil {
		srcNodeID = link.RootNodeID
	}

	srcVars, err := e.store.EffectiveEnvVars(srcNodeID)
	if err != nil {
		return nil, err
	}
	tgtVars, err := e.store.EffectiveEnvVars(target.ID)
	if err != nil {
		return nil, err
	}

	srcBy := map[string]store.EnvVar{}
	for _, v := range srcVars {
		if isReservedDraftEnvKey(v.Key) {
			continue
		}
		srcBy[v.Key] = v
	}
	tgtBy := map[string]store.EnvVar{}
	for _, v := range tgtVars {
		if isReservedDraftEnvKey(v.Key) {
			continue
		}
		tgtBy[v.Key] = v
	}

	// Template entries for restamp preview on target.
	templateByKey := map[string]templateEnvEntry{}
	if target.TemplateID != 0 {
		if tpl, err := e.store.GetTemplate(target.TemplateID); err == nil {
			if entries, err := parseTemplateEnvVars(tpl.EnvVars); err == nil {
				for _, entry := range entries {
					k := strings.TrimSpace(entry.Key)
					if k != "" {
						templateByKey[k] = entry
					}
				}
			}
		}
	}

	keys := map[string]struct{}{}
	for k := range srcBy {
		keys[k] = struct{}{}
	}
	for k := range tgtBy {
		keys[k] = struct{}{}
	}

	var diffs []SyncEnvDiff
	for key := range keys {
		src, srcOK := srcBy[key]
		tgt, tgtOK := tgtBy[key]

		diff := SyncEnvDiff{Key: key, Action: SyncActionEqual}
		if srcOK {
			diff.SourceValue = src.Value
			diff.SourceScope = src.Scope
			diff.SourceSource = src.Source
			diff.SourceSecret = src.Secret
		}
		if tgtOK {
			diff.TargetValue = tgt.Value
			diff.TargetScope = tgt.Scope
			diff.TargetSource = tgt.Source
			diff.TargetSecret = tgt.Secret
		}

		// Target-only: leave alone.
		if !srcOK {
			continue
		}

		// Generated: restamp against target identity when template owns the key.
		if src.Source == store.EnvSourceGenerated {
			entry, inTemplate := templateByKey[key]
			if !inTemplate && target.TemplateID == 0 {
				// Try source template if target has none.
				if source.TemplateID != 0 {
					if tpl, err := e.store.GetTemplate(source.TemplateID); err == nil {
						if entries, err := parseTemplateEnvVars(tpl.EnvVars); err == nil {
							for _, en := range entries {
								if strings.TrimSpace(en.Key) == key {
									entry = en
									inTemplate = true
									break
								}
							}
						}
					}
				}
			}
			if !inTemplate {
				if !tgtOK || src.Value != tgt.Value || src.Scope != tgt.Scope {
					diff.Action = SyncActionSkip
					diff.Reason = "generated value with no template expression — not copied"
					diffs = append(diffs, diff)
				}
				continue
			}
			// Only restamp if target still treats it as generated (or missing).
			if tgtOK && tgt.Source != store.EnvSourceGenerated {
				if src.Value != tgt.Value {
					diff.Action = SyncActionSkip
					diff.Reason = "target value is user-owned; generated source not applied"
					diffs = append(diffs, diff)
				}
				continue
			}
			resolved, rerr := e.ResolveNodeTemplateExprs(target.ID, entry.Value)
			if rerr != nil {
				diff.Action = SyncActionSkip
				diff.Reason = "could not restamp: " + rerr.Error()
				diffs = append(diffs, diff)
				continue
			}
			scope := entry.Scope
			if scope == "" {
				scope = store.EnvScopeRuntime
			}
			if tgtOK && tgt.Value == resolved && (tgt.Scope == scope || tgt.Scope == "") {
				continue // already correct for target identity
			}
			diff.Action = SyncActionRestamp
			diff.ProposedValue = resolved
			diff.ProposedScope = scope
			diff.Reason = "template-generated — restamp for target identity (not copied from source)"
			diffs = append(diffs, diff)
			continue
		}

		// Manual / imported secrets: skip overwrite of existing secrets.
		if (src.Secret || (tgtOK && tgt.Secret)) && tgtOK && src.Value != tgt.Value {
			diff.Action = SyncActionSkip
			diff.Reason = "secret — not overwritten by default"
			diffs = append(diffs, diff)
			continue
		}

		if tgtOK && src.Value == tgt.Value && normalizeEnvScope(src.Scope) == normalizeEnvScope(tgt.Scope) {
			continue
		}

		diff.Action = SyncActionSet
		diff.ProposedValue = src.Value
		diff.ProposedScope = normalizeEnvScope(src.Scope)
		if !tgtOK {
			diff.Reason = "add from source"
		} else {
			diff.Reason = "update from source"
		}
		diffs = append(diffs, diff)
	}

	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Key < diffs[j].Key })
	return diffs, nil
}

func normalizeEnvScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return store.EnvScopeRuntime
	}
	return scope
}

func isReservedDraftEnvKey(key string) bool {
	_, ok := generatedEnvKeys[key]
	return ok
}

func normalizeVolumeMountsForSync(raw string) string {
	specs := ParseVolumeSpecs(raw)
	if len(specs) == 0 {
		if strings.TrimSpace(raw) == "" {
			return ""
		}
		// Keep raw if unparseable so we don't pretend equality.
		return strings.TrimSpace(raw)
	}
	out := make([]VolumeSpec, 0, len(specs))
	for _, s := range specs {
		t := s.Type
		if t == "" {
			t = VolumeTypeBind
		}
		if t == VolumeTypeVolume {
			out = append(out, VolumeSpec{
				Type:          VolumeTypeVolume,
				ContainerPath: s.ContainerPath,
				ReadOnly:      s.ReadOnly,
			})
			continue
		}
		out = append(out, s)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return string(b)
}

func (e *Engine) applyServiceSync(svc SyncServicePreview, req SyncRequest) error {
	if svc.Skipped {
		return nil
	}
	targetID := svc.TargetNodeID

	if req.IncludeSettings {
		toStage := map[string]string{}
		for _, d := range svc.Settings {
			if d.Action != SyncActionSet {
				continue
			}
			val := d.SourceValue
			if d.Key == "volume_mounts" {
				val = normalizeVolumeMountsForSync(val)
			}
			if store.IsImmediateSetting(d.Key) {
				if err := e.store.SetNodeSetting(targetID, d.Key, val); err != nil {
					return fmt.Errorf("apply immediate %s: %w", d.Key, err)
				}
				continue
			}
			toStage[d.Key] = val
		}
		if len(toStage) > 0 {
			if err := e.store.StageNodeSettings(targetID, toStage); err != nil {
				return fmt.Errorf("stage settings: %w", err)
			}
		}
	}

	if req.IncludeEnv {
		var upserts []store.EnvVarStageUpsert
		needRestamp := false
		for _, d := range svc.Env {
			switch d.Action {
			case SyncActionSet:
				upserts = append(upserts, store.EnvVarStageUpsert{
					Key:    d.Key,
					Value:  d.ProposedValue,
					Scope:  d.ProposedScope,
					Source: store.EnvSourceManual,
				})
			case SyncActionRestamp:
				needRestamp = true
			}
		}
		if len(upserts) > 0 {
			if err := e.store.StageEnvVarChanges(targetID, upserts, nil); err != nil {
				return fmt.Errorf("stage env: %w", err)
			}
		}
		if needRestamp {
			// Restamp all template-owned generated keys for target identity.
			// sourceNodeID is used only for conservative settings refresh; pass target.
			if err := e.restampTemplateOwnedGeneratedValues(targetID, svc.SourceNodeID); err != nil {
				return fmt.Errorf("restamp generated env: %w", err)
			}
		}
	}
	return nil
}
