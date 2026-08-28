// Package messages implements the Anthropic Messages face of the oxa
// conversion library: the non-streaming TEXT/SYSTEM/MULTI-TURN/PARAMS/TOOLS/
// IMAGE subset of this milestone, converted to and from the IR (spec/01).
// Wire types are strictly separate from IR types; this package imports ir and
// the standard library only, never another face.
package messages

import (
	"encoding/json"
	"fmt"
)

// Request is the Anthropic Messages wire request. max_tokens is required by
// the API and is therefore not omitempty.
type Request struct {
	Model         string          `json:"model"`
	System        any             `json:"system,omitempty"` // string or []SystemBlockWire
	Messages      []Message       `json:"messages"`
	MaxTokens     int64           `json:"max_tokens"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Metadata      any             `json:"metadata,omitempty"`
	Tools         []ToolWire      `json:"tools,omitempty"`
	ToolChoice    *ToolChoiceWire `json:"tool_choice,omitempty"`
}

// ToolWire is one element of the wire tools array. input_schema is a
// JSON-Schema-shaped object carried verbatim (spec/01 s3.5): json.RawMessage
// preserves the exact source bytes across decode and encode.
type ToolWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolChoiceWire is the wire tool_choice object. disable_parallel_tool_use
// has no IR equivalent and is dropped with an unmapped-field loss (N-AN-6).
type ToolChoiceWire struct {
	Type                   string `json:"type"` // auto | any | tool
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
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
// blocks. A decoded tool_use input is kept in BlockWire.Input as raw JSON.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// BlockWire is one content block: text, image, tool_use, or tool_result.
// Input is a raw JSON object. Decoders and encoders MUST copy it without
// parsing or reserializing it (spec/01 INV-1).
type BlockWire struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// image
	Source *SourceWire `json:"source,omitempty"`
}

// SourceWire is the source object of an image block: a base64 payload
// (media_type + data) or a URL.
type SourceWire struct {
	Type      string `json:"type"` // base64 | url
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
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

// inputToIRString converts a wire tool_use.input raw JSON object into the IR
// string form (spec/01 INV-1): the exact source bytes become the string
// payload. The object bytes are never parsed or re-serialized as an object.
func inputToIRString(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("anthropic: tool_use input is required")
	}
	return json.Marshal(string(raw))
}

// inputFromIRString converts an IR tool_use input string token back into the
// raw JSON object bytes it carries, verbatim.
func inputFromIRString(tok json.RawMessage) (json.RawMessage, error) {
	if len(tok) == 0 {
		return nil, fmt.Errorf("anthropic: tool_use input is required")
	}
	var s string
	if err := json.Unmarshal(tok, &s); err != nil {
		return nil, fmt.Errorf("anthropic: tool_use input is not a string: %w", err)
	}
	return json.RawMessage(s), nil
}
