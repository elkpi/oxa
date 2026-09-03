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

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	BlockTypeText       = "text"
	BlockTypeImage      = "image"
	BlockTypeToolUse    = "tool_use"
	BlockTypeToolResult = "tool_result"

	SourceTypeBase64 = "base64"
	SourceTypeURL    = "url"

	ToolChoiceTypeAuto = "auto"
	ToolChoiceTypeAny  = "any"
	ToolChoiceTypeNone = "none"
	ToolChoiceTypeTool = "tool"

	StopReasonEndTurn      = "end_turn"
	StopReasonMaxTokens    = "max_tokens"
	StopReasonStopSequence = "stop_sequence"
	StopReasonToolUse      = "tool_use"
	StopReasonRefusal      = "refusal"

	EventTypeMessageStart      = "message_start"
	EventTypeContentBlockStart = "content_block_start"
	EventTypeContentBlockDelta = "content_block_delta"
	EventTypeContentBlockStop  = "content_block_stop"
	EventTypeMessageDelta      = "message_delta"
	EventTypeMessageStop       = "message_stop"

	DeltaTypeTextDelta      = "text_delta"
	DeltaTypeInputJSONDelta = "input_json_delta"

	TypeMessage = "message"
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

// StreamEvent is one Anthropic Messages streaming SSE event (spec/01 s5).
// Type is the discriminating head; the per-type payloads reuse the wire types
// where the shapes coincide: message_start carries its near-identical message
// envelope as a *Response (MarshalJSON renders its wire null stop_reason),
// content_block_start carries the opening block, content_block_delta carries
// a *StreamDelta, and message_delta carries a *StreamDelta (stop_reason /
// stop_sequence form) plus the cumulative usage. Nil payloads mean "not part
// of this event type".
type StreamEvent struct {
	Type         string       `json:"type"`
	Message      *Response    `json:"message,omitempty"`
	Index        int          `json:"index,omitempty"`
	ContentBlock *BlockWire   `json:"content_block,omitempty"`
	Delta        *StreamDelta `json:"delta,omitempty"`
	Usage        *UsageWire   `json:"usage,omitempty"`
}

// StreamDelta is the delta payload of content_block_delta and message_delta.
// TextDelta is the text form {type:"text_delta", text}; PartialJSON is
// declared for input_json_delta detection only (streaming tool calls arrive
// in M7); the stop_reason/stop_sequence form populates the last two fields.
type StreamDelta struct {
	Type         string `json:"type"` // text_delta | input_json_delta | (empty on message_delta)
	Text         string `json:"text,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

// MarshalJSON renders type-specific stream envelopes. In particular, index 0
// is significant for content block events and message_start carries the
// Anthropic null stop_reason rather than the empty-string convention of the
// completed-response Response type.
func (e StreamEvent) MarshalJSON() ([]byte, error) {
	switch e.Type {
	case EventTypeMessageStart:
		if e.Message == nil {
			return nil, fmt.Errorf("anthropic: message_start without message")
		}
		type messageStartWire struct {
			ID         string      `json:"id"`
			Type       string      `json:"type"`
			Role       string      `json:"role"`
			Model      string      `json:"model"`
			Content    []BlockWire `json:"content"`
			StopReason any         `json:"stop_reason"`
			Usage      *UsageWire  `json:"usage"`
		}
		return json.Marshal(struct {
			Type    string           `json:"type"`
			Message messageStartWire `json:"message"`
		}{
			Type: e.Type,
			Message: messageStartWire{
				ID:         e.Message.ID,
				Type:       e.Message.Type,
				Role:       e.Message.Role,
				Model:      e.Message.Model,
				Content:    e.Message.Content,
				StopReason: nil,
				Usage:      e.Message.Usage,
			},
		})
	case EventTypeContentBlockStart:
		if e.ContentBlock == nil {
			return nil, fmt.Errorf("anthropic: content_block_start without content_block")
		}
		return json.Marshal(struct {
			Type         string     `json:"type"`
			Index        int        `json:"index"`
			ContentBlock *BlockWire `json:"content_block"`
		}{Type: e.Type, Index: e.Index, ContentBlock: e.ContentBlock})
	case EventTypeContentBlockDelta:
		if e.Delta == nil {
			return nil, fmt.Errorf("anthropic: content_block_delta without delta")
		}
		return json.Marshal(struct {
			Type  string       `json:"type"`
			Index int          `json:"index"`
			Delta *StreamDelta `json:"delta"`
		}{Type: e.Type, Index: e.Index, Delta: e.Delta})
	case EventTypeContentBlockStop:
		return json.Marshal(struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}{Type: e.Type, Index: e.Index})
	case EventTypeMessageDelta:
		if e.Delta == nil {
			return nil, fmt.Errorf("anthropic: message_delta without delta")
		}
		return json.Marshal(struct {
			Type  string       `json:"type"`
			Delta *StreamDelta `json:"delta"`
			Usage *UsageWire   `json:"usage"`
		}{Type: e.Type, Delta: e.Delta, Usage: e.Usage})
	case EventTypeMessageStop:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{Type: e.Type})
	default:
		type plainStreamEvent StreamEvent
		return json.Marshal(plainStreamEvent(e))
	}
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
