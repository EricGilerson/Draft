package draftpack

import (
	"encoding/json"
	"strings"
)

// volumeSpec mirrors deploy.VolumeSpec JSON tags without importing deploy.
type volumeSpec struct {
	Type          string            `json:"type,omitempty"`
	Source        string            `json:"source,omitempty"`
	HostPath      string            `json:"hostPath,omitempty"`
	ContainerPath string            `json:"containerPath"`
	ReadOnly      bool              `json:"readOnly,omitempty"`
	SizeHint      string            `json:"sizeHint,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

func parseVolumeSpecs(raw string) []volumeSpec {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var specs []volumeSpec
	if json.Unmarshal([]byte(raw), &specs) != nil {
		return nil
	}
	return specs
}

// sanitizeVolumes rewrites volume_mounts for portability.
// Draft-managed volumes keep container path only (auto-name on import).
// Bind mounts: strip host path unless includeHostPaths; always record remaps.
func sanitizeVolumes(raw string, includeHostPaths bool, rep *Report, serviceLabel string) (sanitized string, remaps []BindRemap) {
	specs := parseVolumeSpecs(raw)
	if len(specs) == 0 {
		return "", nil
	}
	out := make([]volumeSpec, 0, len(specs))
	for _, s := range specs {
		cp := strings.TrimSpace(s.ContainerPath)
		if cp == "" {
			continue
		}
		switch strings.TrimSpace(s.Type) {
		case "volume":
			// Portable intent: Draft-managed named volume at container path.
			// Drop explicit Docker volume names (they encode project/env/uid).
			out = append(out, volumeSpec{
				Type:          "volume",
				ContainerPath: cp,
				ReadOnly:      s.ReadOnly,
				SizeHint:      s.SizeHint,
			})
		default: // bind or empty
			host := strings.TrimSpace(s.Source)
			if host == "" {
				host = strings.TrimSpace(s.HostPath)
			}
			remap := BindRemap{
				ContainerPath: cp,
				OriginalHost:  host,
				ReadOnly:      s.ReadOnly,
			}
			remaps = append(remaps, remap)
			if includeHostPaths && host != "" {
				out = append(out, volumeSpec{
					Type:          "bind",
					Source:        host,
					ContainerPath: cp,
					ReadOnly:      s.ReadOnly,
				})
				rep.Add(KindInfo, "bind_path_included", serviceLabel,
					"Bind mount host path for "+cp+" was included; it may not exist on another machine.")
			} else {
				// Keep a bind entry without host so the mount target is visible;
				// import will apply overrides or leave unset.
				out = append(out, volumeSpec{
					Type:          "bind",
					ContainerPath: cp,
					ReadOnly:      s.ReadOnly,
				})
				if host != "" {
					rep.Add(KindManual, "bind_path_omitted", serviceLabel,
						"Bind mount "+cp+" needs a host path on import (was "+host+").")
				} else {
					rep.Add(KindManual, "bind_path_missing", serviceLabel,
						"Bind mount "+cp+" has no host path; set one after import.")
				}
			}
		}
	}
	if len(out) == 0 {
		return "", remaps
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", remaps
	}
	return string(b), remaps
}

// applyBindOverrides fills bind host paths from overrides and drops binds still empty.
func applyBindOverrides(raw string, serviceKey string, overrides map[string]string, rep *Report, label string) string {
	specs := parseVolumeSpecs(raw)
	if len(specs) == 0 {
		return raw
	}
	out := make([]volumeSpec, 0, len(specs))
	for _, s := range specs {
		cp := strings.TrimSpace(s.ContainerPath)
		if strings.TrimSpace(s.Type) == "volume" {
			// Always re-auto-name Draft volumes.
			out = append(out, volumeSpec{
				Type:          "volume",
				ContainerPath: cp,
				ReadOnly:      s.ReadOnly,
				SizeHint:      s.SizeHint,
			})
			continue
		}
		host := strings.TrimSpace(s.Source)
		if host == "" {
			host = strings.TrimSpace(s.HostPath)
		}
		if key := BindOverrideKey(serviceKey, cp); overrides != nil {
			if v, ok := overrides[key]; ok && strings.TrimSpace(v) != "" {
				host = strings.TrimSpace(v)
			}
		}
		if host == "" {
			rep.Add(KindManual, "bind_skipped", label,
				"Bind mount "+cp+" has no host path and was not mounted.")
			continue
		}
		out = append(out, volumeSpec{
			Type:          "bind",
			Source:        host,
			ContainerPath: cp,
			ReadOnly:      s.ReadOnly,
		})
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}
