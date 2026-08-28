// Package messages implements the Anthropic Messages face of the oxa
// conversion library: the non-streaming TEXT/SYSTEM/MULTI-TURN/PARAMS subset
// of this milestone, converted to and from the IR (spec/01). Wire types are
// strictly separate from IR types; this package imports ir and the standard
// library only, never another face.
package messages

import "encoding/json"

// Request is the Anthropic Messages wire request (the non-tool subset of the
// fields). max_tokens is required by the API and is therefore not omitempty.
type Request struct {
	Model         string    `json:"model"`
	System        any       `json:"system,omitempty"` // string or []SystemBlockWire
	Messages      []Message `json:"messages"`
	MaxTokens     int64     `json:"max_tokens"`
	Temperature   *float64  `json:"temperature,omitempty"`
	TopP          *float64  `json:"top_p,omitempty"`
	StopSequences []string  `json:"stop_sequences,omitempty"`
	Metadata      any       `json:"metadata,omitempty"`
}

// SystemBlockWire is one element of the system block array. cache_control is
// an Anthropic prompt-caching annotation with no IR equivalent in v1; it is
// dropped with an unmapped-field loss.
type SystemBlockWire struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

// Message is a wire message. Content is either a string or an array of
// blocks; decode handles both, encode uses the string shorthand only where
// the seed vectors' rendering defaults dictate (see encode.go).
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// BlockWire is one content block of the text subset. Tool-use and image block
// fields arrive in later milestones and are not declared here.
type BlockWire struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

// Response is the Anthropic Messages wire response object.
type Response struct {
	ID           string      `json:"id"`
	Type         string      `json:"type"`
	Role         string      `json:"role"`
	Model        string      `json:"model"`
	Content      []BlockWire `json:"content"`
	StopReason   string      `json:"stop_reason"`
	StopSequence string      `json:"stop_sequence,omitempty"`
	Usage        *UsageWire  `json:"usage"`
}

// UsageWire is the wire usage object; input/output tokens map 1:1 to IR
// usage.
type UsageWire struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
