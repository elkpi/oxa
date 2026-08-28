package ir

// LossReason is the reason code of a loss record (spec/02 s3). Closed set.
type LossReason string

const (
	// LossUnmappedField: the source field has no representation in the target
	// dialect; the field is dropped.
	LossUnmappedField LossReason = "unmapped-field"
	// LossUnmappedValue: the field exists on both sides, but this specific
	// value has no mapping.
	LossUnmappedValue LossReason = "unmapped-value"
	// LossUnsupportedSemantic: a whole construct or combination the target
	// dialect cannot express.
	LossUnsupportedSemantic LossReason = "unsupported-semantic"
	// LossDegraded: best-effort carry with known distortion; reserved for
	// carry, never for drops.
	LossDegraded LossReason = "degraded"
)

// Loss reports a fidelity cost of a conversion (spec/02 s2). Losses are
// first-class output, never silently dropped, and never turned into errors.
// Path is root-relative: object keys joined by '.', array elements addressed
// by zero-based index in brackets.
type Loss struct {
	Path   string     `json:"path"`
	Field  string     `json:"field"`
	Reason LossReason `json:"reason"`
	Detail string     `json:"detail,omitempty"`
}
