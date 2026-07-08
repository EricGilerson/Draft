package cloudconfig

// Note kinds. The UI groups notes by kind so a user can see, at a glance, what
// mapped cleanly, what Draft ignored, what it transformed, and what needs a
// human decision.
const (
	// KindIgnored: a field with no local run/build meaning was dropped
	// (autoscaling, IAM, CI steps). Preserved for round-trip when possible.
	KindIgnored = "ignored"
	// KindTransformed: a value was rewritten to keep intent (HTTP probe → shell
	// healthcheck, cross-service URL → @{Service.ATTR}, secret ref → placeholder).
	KindTransformed = "transformed"
	// KindManual: the user must supply something Draft cannot (a secret value,
	// a production endpoint).
	KindManual = "manual"
	// KindInfo: neutral informational note.
	KindInfo = "info"
)

// Note is a single line of the fidelity report. It intentionally shares the
// shape of deploy.SettingsWarning so the frontend can render both with one
// component.
type Note struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Report is the fidelity report for one conversion. It is honest by design:
// every lossy or approximating step appends a note rather than failing silently.
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
