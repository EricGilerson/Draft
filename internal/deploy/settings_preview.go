package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"Draft/internal/networking"
	"Draft/internal/store"
)

type SettingsWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type StagedChangePreview struct {
	Warnings []SettingsWarning `json:"warnings"`
	Errors   []SettingsWarning `json:"errors"`
}

func PreviewStagedChangesFromStore(s *store.Store, nodeID string, proposedSettings map[string]string) (*StagedChangePreview, error) {
	if len(proposedSettings) == 0 {
		return &StagedChangePreview{
			Warnings: []SettingsWarning{},
			Errors:   []SettingsWarning{},
		}, nil
	}

	applied, err := s.GetNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	if applied == nil {
		applied = map[string]string{}
	}

	existingStaged, err := s.GetStagedNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	if existingStaged == nil {
		existingStaged = map[string]string{}
	}

	staged := store.MergeNodeSettings(existingStaged, proposedSettings)
	effective := store.MergeNodeSettings(applied, staged)
	return previewEffectiveSettings(s, nodeID, applied, effective, proposedSettings)
}

func (e *Engine) PreviewStagedChanges(ctx context.Context, nodeID string, proposedSettings map[string]string) (*StagedChangePreview, error) {
	return PreviewStagedChangesFromStore(e.store, nodeID, proposedSettings)
}

func previewEffectiveSettings(s *store.Store, nodeID string, applied, effective, proposed map[string]string) (*StagedChangePreview, error) {
	out := &StagedChangePreview{
		Warnings: []SettingsWarning{},
		Errors:   []SettingsWarning{},
	}
	if proposed == nil {
		proposed = map[string]string{}
	}

	oldMounts := applied["volume_mounts"]
	newMounts := effective["volume_mounts"]
	if _, stagingVolumes := proposed["volume_mounts"]; stagingVolumes {
		if err := validateVolumeMountsJSON(newMounts); err != nil {
			out.Errors = append(out.Errors, SettingsWarning{
				Code:    "volume_mounts_invalid",
				Message: err.Error(),
				Field:   "volume_mounts",
			})
		} else if oldMounts != newMounts {
			warnings, err := volumeMountChangeWarnings(s, nodeID, oldMounts, newMounts)
			if err != nil {
				return nil, err
			}
			out.Warnings = append(out.Warnings, warnings...)
		}
	}

	if _, stagingPort := proposed["service_port"]; stagingPort &&
		applied["service_port"] != effective["service_port"] &&
		strings.TrimSpace(effective["service_port"]) != "" {
		out.Warnings = append(out.Warnings, SettingsWarning{
			Code:    "service_port_change",
			Message: "Service port change applies on next deploy; routes and env references may need updating.",
			Field:   "service_port",
		})
	}

	active, _ := s.ActiveDeployment(nodeID)
	if active != nil && active.Status == "running" && len(out.Warnings) > 0 {
		out.Warnings = append([]SettingsWarning{{
			Code:    "pending_redeploy",
			Message: "Service is running; staged changes apply on the next deploy.",
		}}, out.Warnings...)
	}

	return out, nil
}

func validateVolumeMountsJSON(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	specs := ParseVolumeSpecs(raw)
	if len(specs) == 0 {
		return fmt.Errorf("volume_mounts must be a JSON array of mount specs")
	}
	for _, spec := range specs {
		if strings.TrimSpace(spec.ContainerPath) == "" {
			return fmt.Errorf("each volume mount requires a container path")
		}
	}
	return nil
}

func volumeMountChangeWarnings(s *store.Store, nodeID, oldRaw, newRaw string) ([]SettingsWarning, error) {
	node, err := s.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	project, err := s.GetProject(node.ProjectID)
	if err != nil {
		return nil, err
	}
	envSlug := "default"
	sandbox := false
	if env, err := s.GetEnvironment(node.EnvironmentID); err == nil {
		envSlug = env.Slug
		if _, err := s.GetSandboxByEnvironment(env.ID); err == nil {
			sandbox = true
		}
	}
	dockerEnv := networking.DockerEnvironment(envSlug, sandbox)
	uid, _ := s.EnsureNodeUID(nodeID)
	oldSpecs := ParseVolumeSpecs(oldRaw)
	newSpecs := ParseVolumeSpecs(newRaw)
	oldByTarget := map[string]VolumeSpec{}
	for _, spec := range oldSpecs {
		oldByTarget[spec.ContainerPath] = spec
	}
	var warnings []SettingsWarning
	for _, spec := range newSpecs {
		old, had := oldByTarget[spec.ContainerPath]
		if had && volumeSpecEqual(old, spec) {
			continue
		}
		if spec.Type == VolumeTypeVolume || spec.Type == "" && strings.TrimSpace(spec.Source) == "" {
			oldName := ""
			if had && (old.Type == VolumeTypeVolume || old.Type == "") {
				src := strings.TrimSpace(old.Source)
				if src == "" {
					oldName = DraftVolumeName(node.ProjectID, project.Name, dockerEnv, uid, old.ContainerPath)
				} else {
					oldName = src
				}
			}
			newName := strings.TrimSpace(spec.Source)
			if newName == "" {
				newName = DraftVolumeName(node.ProjectID, project.Name, dockerEnv, uid, spec.ContainerPath)
			}
			if oldName != "" && oldName != newName {
				warnings = append(warnings, SettingsWarning{
					Code:    "volume_orphan",
					Message: fmt.Sprintf("Changing mount %q will create volume %q on next deploy; existing data in %q will not be moved automatically.", spec.ContainerPath, newName, oldName),
					Field:   "volume_mounts",
				})
			}
		}
		if had && old.Type != spec.Type {
			warnings = append(warnings, SettingsWarning{
				Code:    "volume_type_change",
				Message: fmt.Sprintf("Switching mount type at %q replaces the storage backend on next deploy.", spec.ContainerPath),
				Field:   "volume_mounts",
			})
		}
	}
	for _, old := range oldSpecs {
		found := false
		for _, spec := range newSpecs {
			if spec.ContainerPath == old.ContainerPath {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, SettingsWarning{
				Code:    "volume_removed",
				Message: fmt.Sprintf("Removing mount %q makes that path ephemeral on next deploy.", old.ContainerPath),
				Field:   "volume_mounts",
			})
		}
	}
	return warnings, nil
}

func volumeSpecEqual(a, b VolumeSpec) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}
