// Package draftpack implements Draft-native portable packs for sharing
// projects, environments, and services across machines.
//
// A pack is configuration only: no Docker runtime state, no deployment
// history, no routes or port leases. Absolute machine paths are stripped or
// remapped; secrets default to keys without values.
package draftpack

// Note kinds mirror cloudconfig so the UI can reuse the same fidelity grouping.
const (
	KindIgnored     = "ignored"
	KindTransformed = "transformed"
	KindManual      = "manual"
	KindInfo        = "info"
)

// Note is one line of the fidelity / remap report.
type Note struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Report collects conversion notes.
type Report struct {
	Notes []Note `json:"notes"`
}

// Add appends a note.
func (r *Report) Add(kind, code, field, message string) {
	r.Notes = append(r.Notes, Note{Kind: kind, Code: code, Field: field, Message: message})
}

// Merge folds another report's notes into this one.
func (r *Report) Merge(other Report) {
	r.Notes = append(r.Notes, other.Notes...)
}

// Empty reports whether there are no notes.
func (r Report) Empty() bool { return len(r.Notes) == 0 }
