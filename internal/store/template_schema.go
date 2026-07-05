package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TemplateSchema is the structured capabilities descriptor a ServiceTemplate
// carries. It drives the create-service wizard (which steps to show, which
// fields to expose as overrides) and the Settings tab (which sections to hide
// for a node created from this template).
//
// ServiceRoot and Dockerfile are mode strings, not booleans, so a future
// template can express finer-grained intent. The only allowed values today are
// "optional" and "hidden". There is deliberately NO "required" value: service
// root must never block service creation, and the build path already falls back
// to the project root when service_root is unset.
type TemplateSchema struct {
	ServiceRoot   string               `json:"serviceRoot,omitempty"`   // "optional" | "hidden"
	Dockerfile    string               `json:"dockerfile,omitempty"`    // "optional" | "hidden"
	WizardSteps   []WizardStep         `json:"wizardSteps,omitempty"`
	Settings      map[string]FieldSpec `json:"settings,omitempty"`      // key = node_settings key
	HideSections  []string             `json:"hideSections,omitempty"`  // SettingsTab section ids to hide
}

// WizardStep is a single step in the create-service wizard. Steps are authored
// declaratively; the frontend renders them in order.
type WizardStep struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// FieldSpec describes a single overridable field surfaced in the wizard. A
// field maps to either a node_settings key or (when Type=="env") an env var
// key. Defaults come from the template itself (Port, EnvVars, etc.); the spec
// only controls how the wizard presents the override.
type FieldSpec struct {
	Default string   `json:"default,omitempty"`
	Hidden  bool     `json:"hidden,omitempty"`
	Label   string   `json:"label,omitempty"`
	Type    string   `json:"type,omitempty"`   // "text" | "number" | "path" | "select" | "env"
	Options []string `json:"options,omitempty"`
}

const (
	SchemaOptional = "optional"
	SchemaHidden   = "hidden"

	ModeBuild = "build"
	ModeImage = "image"
)

// Canonical SettingsTab section ids that HideSections can reference.
const (
	SectionSource           = "source"
	SectionDockerfile       = "dockerfile"
	SectionBuildContext     = "buildContext"
	SectionBuildConfig      = "buildConfiguration"
	SectionRuntimeCommand   = "runtimeCommand"
	SectionRestart          = "restart"
	SectionHealthcheck      = "healthcheck"
	SectionResources        = "resources"
	SectionVolumes          = "volumes"
	SectionLifecycle        = "lifecycle"
	SectionSecurity         = "security"
	SectionLabels           = "labels"
)

// defaultBuildSchema is the schema applied when a template has no Schema set
// (e.g. a brand-new custom template saved before the author touched the
// Capabilities editor). It mirrors the intent of the built-in build templates:
// everything optional, full wizard, no hidden sections.
func defaultBuildSchema() TemplateSchema {
	return TemplateSchema{
		ServiceRoot: SchemaOptional,
		Dockerfile:  SchemaOptional,
		WizardSteps: []WizardStep{
			{ID: "template", Title: "Template"},
			{ID: "identity", Title: "Name & options"},
			{ID: "source", Title: "Service root"},
			{ID: "review", Title: "Review"},
		},
	}
}

// defaultImageSchema is the schema applied to image-mode templates with no
// explicit Schema. Source/dockerfile/build sections are hidden and the wizard
// skips the source step.
func defaultImageSchema() TemplateSchema {
	return TemplateSchema{
		ServiceRoot: SchemaHidden,
		Dockerfile:  SchemaHidden,
		HideSections: []string{
			SectionSource,
			SectionDockerfile,
			SectionBuildContext,
			SectionBuildConfig,
			SectionRuntimeCommand,
			SectionVolumes,
		},
		WizardSteps: []WizardStep{
			{ID: "template", Title: "Template"},
			{ID: "identity", Title: "Name & options"},
			{ID: "review", Title: "Review"},
		},
	}
}

// DefaultSchemaFor returns the schema to apply when a template of the given
// mode is saved without an explicit one.
func DefaultSchemaFor(mode string) TemplateSchema {
	if mode == ModeImage {
		return defaultImageSchema()
	}
	return defaultBuildSchema()
}

// ParseTemplateSchema decodes a template's Schema JSON column. An empty/raw
// value yields a zero schema; the caller is expected to fill in defaults via
// NormalizeSchema when needed. Malformed JSON is an error so a bad template
// can't silently ship a zero schema into the wizard.
func ParseTemplateSchema(raw string) (TemplateSchema, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TemplateSchema{}, nil
	}
	var s TemplateSchema
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return TemplateSchema{}, fmt.Errorf("template schema is not valid JSON: %w", err)
	}
	return s, nil
}

// EncodeTemplateSchema serializes a schema to the JSON string form stored in
// the ServiceTemplate.Schema column. A zero schema serializes to "" so empty
// stays empty.
func EncodeTemplateSchema(s TemplateSchema) (string, error) {
	if isZeroSchema(s) {
		return "", nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// NormalizeSchema fills any unset mode fields with defaults derived from the
// template mode, and coerces invalid mode values to the default. It does NOT
// mutate HideSections/WizardSteps/Settings the author set explicitly — only
// the ServiceRoot/Dockerfile mode strings and an empty WizardSteps list.
func NormalizeSchema(s TemplateSchema, mode string) TemplateSchema {
	defaults := DefaultSchemaFor(mode)
	if s.ServiceRoot != SchemaOptional && s.ServiceRoot != SchemaHidden {
		s.ServiceRoot = defaults.ServiceRoot
	}
	if s.Dockerfile != SchemaOptional && s.Dockerfile != SchemaHidden {
		s.Dockerfile = defaults.Dockerfile
	}
	if len(s.WizardSteps) == 0 {
		s.WizardSteps = defaults.WizardSteps
	}
	return s
}

func isZeroSchema(s TemplateSchema) bool {
	return s.ServiceRoot == "" && s.Dockerfile == "" && len(s.WizardSteps) == 0 &&
		len(s.Settings) == 0 && len(s.HideSections) == 0
}
