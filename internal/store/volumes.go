package store

import (
	"encoding/json"
	"strings"
)

// TemplateVolume is the JSON shape stored in ServiceTemplate.Volumes. It
// mirrors deploy.VolumeSpec but lives in the store package so template
// normalization doesn't import deploy. The two must stay in sync; the on-disk
// JSON is the contract, not the Go struct.
type TemplateVolume struct {
	Type          string            `json:"type,omitempty"`          // "bind" | "volume"; "" => "bind"
	Source        string            `json:"source,omitempty"`        // volume: "" = auto-name; bind: host path
	HostPath      string            `json:"hostPath,omitempty"`      // legacy bind host path (back-compat)
	ContainerPath string            `json:"containerPath"`           // mount target inside the container
	ReadOnly      bool              `json:"readOnly,omitempty"`
	SizeHint      string            `json:"sizeHint,omitempty"`      // advisory, monitored — NOT enforced by Docker
	Labels        map[string]string `json:"labels,omitempty"`
}

// ParseTemplateVolumes decodes a template's Volumes JSON. Empty => nil; malformed
// => error so a bad template can't ship a broken volume list into the wizard.
func ParseTemplateVolumes(raw string) ([]TemplateVolume, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var vols []TemplateVolume
	if err := json.Unmarshal([]byte(raw), &vols); err != nil {
		return nil, err
	}
	return vols, nil
}

// NormalizeTemplateVolumes validates and cleans a template's Volumes JSON:
// drops entries with no container path, trims string fields, and re-encodes.
// Empty result encodes to "" so the stored column stays compact.
func NormalizeTemplateVolumes(raw string) (string, error) {
	vols, err := ParseTemplateVolumes(raw)
	if err != nil {
		return "", err
	}
	out := make([]TemplateVolume, 0, len(vols))
	for _, v := range vols {
		v.ContainerPath = strings.TrimSpace(v.ContainerPath)
		if v.ContainerPath == "" {
			continue
		}
		v.Source = strings.TrimSpace(v.Source)
		v.HostPath = strings.TrimSpace(v.HostPath)
		v.SizeHint = strings.TrimSpace(v.SizeHint)
		v.Type = strings.TrimSpace(v.Type)
		if v.Type == "" {
			v.Type = "bind"
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return "", nil
	}
	enc, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(enc), nil
}
