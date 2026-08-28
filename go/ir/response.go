package ir

// StopReason is the terminal reason of a response. Closed set, unioned over
// the three faces; other is the escape hatch for face-native stop reasons
// with no IR equivalent and MUST record a loss (spec/01 s4.1, spec/02).
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopMaxTokens StopReason = "max_tokens"
	StopSequence  StopReason = "stop_sequence"
	StopToolUse   StopReason = "tool_use"
	StopRefusal   StopReason = "refusal"
	StopOther     StopReason = "other"
)

// Response is a completed (aggregated) model response (spec/01 s4.1).
type Response struct {
	ID           string
	Model        string
	Content      []Block
	StopReason   StopReason
	StopSequence string // set only when StopReason == stop_sequence, then non-empty
	Usage        Usage
}

// Usage carries token counts.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}
