// Package messages implements the Anthropic Messages face of the oxa
// conversion library: the non-streaming TEXT/SYSTEM/MULTI-TURN/PARAMS/TOOLS/
// IMAGE subset of this milestone, converted to and from the IR (spec/01).
// Wire types are strictly separate from IR types; this package imports ir and
// the standard library only, never another face.
package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"

	BlockTypeText       = "text"
	BlockTypeImage      = "image"
	BlockTypeToolUse    = "tool_use"
	BlockTypeToolResult = "tool_result"
	BlockTypeThinking   = "thinking"
	BlockTypeDocument   = "document"

	SourceTypeBase64 = "base64"
	SourceTypeURL    = "url"
	SourceTypeFile   = "file"

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
	DeltaTypeThinking       = "thinking_delta"
	DeltaTypeSignature      = "signature_delta"

	TypeMessage = "message"
)

// Request is the Anthropic Messages wire request. max_tokens is required by
// the API and is therefore not omitempty.
type Request struct {
	Model             string                 `json:"model"`
	System            any                    `json:"system,omitempty"` // string or []SystemBlockWire
	Messages          []Message              `json:"messages"`
	MaxTokens         int64                  `json:"max_tokens"`
	Temperature       *float64               `json:"temperature,omitempty"`
	TopP              *float64               `json:"top_p,omitempty"`
	StopSequences     []string               `json:"stop_sequences,omitempty"`
	Metadata          any                    `json:"metadata,omitempty"`
	Tools             []ToolWire             `json:"tools,omitempty"`
	ToolChoice        *ToolChoiceWire        `json:"tool_choice,omitempty"`
	Thinking          *ThinkingWire          `json:"thinking,omitempty"`
	CacheControl      json.RawMessage        `json:"cache_control,omitempty"`
	OutputConfig      *OutputConfigWire      `json:"output_config,omitempty"`
	ContextManagement *ContextManagementWire `json:"context_management,omitempty"`
}

// OutputConfigWire is the Anthropic request output_config configuration.
type OutputConfigWire struct {
	Effort string          `json:"effort,omitempty"`
	Format json.RawMessage `json:"format,omitempty"`
}

// ContextManagementWire is the Anthropic request context_management configuration.
type ContextManagementWire struct {
	Edits []json.RawMessage `json:"edits,omitempty"`
}

// ThinkingWire is the Anthropic request thinking configuration.
type ThinkingWire struct {
	Type         string `json:"type"` // adaptive | enabled | disabled
	BudgetTokens int64  `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

// ToolWire is one element of the wire tools array. input_schema is a
// JSON-Schema-shaped object carried verbatim (spec/01 s3.5): json.RawMessage
// preserves the exact source bytes across decode and encode. Raw keeps the
// exact declaration source only for native (provider-discriminated) tools so
// their opaque configuration survives round trips; ordinary tools always
// re-marshal from their exported fields so caller-side edits are honored
// (spec/16 N-NT-2).
type ToolWire struct {
	Type                string          `json:"type,omitempty"`
	Name                string          `json:"name"`
	Description         string          `json:"description,omitempty"`
	InputSchema         json.RawMessage `json:"input_schema,omitempty"`
	CacheControl        json.RawMessage `json:"cache_control,omitempty"`
	DeferLoading        *bool           `json:"defer_loading,omitempty"`
	EagerInputStreaming *bool           `json:"eager_input_streaming,omitempty"`
	Raw                 json.RawMessage `json:"-"`
	rawDeferLoading     json.RawMessage `json:"-"`
	rawEager            json.RawMessage `json:"-"`
}

func (t *ToolWire) UnmarshalJSON(data []byte) error {
	type shadow struct {
		Type                json.RawMessage `json:"type"`
		Name                json.RawMessage `json:"name"`
		Description         string          `json:"description,omitempty"`
		InputSchema         json.RawMessage `json:"input_schema,omitempty"`
		CacheControl        json.RawMessage `json:"cache_control,omitempty"`
		DeferLoading        json.RawMessage `json:"defer_loading,omitempty"`
		EagerInputStreaming json.RawMessage `json:"eager_input_streaming,omitempty"`
	}
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if len(s.Type) > 0 {
		if string(s.Type) == "null" {
			return fmt.Errorf("anthropic: tool type must be a string")
		}
		if err := json.Unmarshal(s.Type, &t.Type); err != nil {
			return fmt.Errorf("anthropic: tool type must be a string")
		}
	}
	if len(s.Name) == 0 || string(s.Name) == "null" {
		return fmt.Errorf("anthropic: tool name is required")
	}
	if err := json.Unmarshal(s.Name, &t.Name); err != nil {
		return fmt.Errorf("anthropic: tool name must be a string")
	}
	if t.Name == "" {
		return fmt.Errorf("anthropic: tool name cannot be empty")
	}
	t.Description = s.Description
	t.InputSchema = s.InputSchema
	t.CacheControl = s.CacheControl
	if isNativeToolType(t.Type) {
		t.Raw = append([]byte(nil), data...)
	} else {
		t.Raw = nil
		if len(s.DeferLoading) > 0 {
			t.rawDeferLoading = s.DeferLoading
			trimmed := strings.TrimSpace(string(s.DeferLoading))
			switch trimmed {
			case "true":
				b := true
				t.DeferLoading = &b
			case "false":
				b := false
				t.DeferLoading = &b
			}
		}
		if len(s.EagerInputStreaming) > 0 {
			t.rawEager = s.EagerInputStreaming
			trimmed := strings.TrimSpace(string(s.EagerInputStreaming))
			switch trimmed {
			case "true":
				b := true
				t.EagerInputStreaming = &b
			case "false":
				b := false
				t.EagerInputStreaming = &b
			}
		}
	}
	return nil
}

// isNativeToolType reports whether a wire discriminator selects a
// provider-native declaration (spec/16 N-NT-2): non-empty and neither
// function nor custom.
func isNativeToolType(toolType string) bool {
	return toolType != "" && toolType != ir.ToolTypeFunction && toolType != ir.ToolTypeCustom
}

func (t ToolWire) MarshalJSON() ([]byte, error) {
	if len(t.Raw) > 0 && isNativeToolType(t.Type) {
		return t.Raw, nil
	}
	type shadow struct {
		Type                string          `json:"type,omitempty"`
		Name                string          `json:"name"`
		Description         string          `json:"description,omitempty"`
		InputSchema         json.RawMessage `json:"input_schema,omitempty"`
		CacheControl        json.RawMessage `json:"cache_control,omitempty"`
		DeferLoading        *bool           `json:"defer_loading,omitempty"`
		EagerInputStreaming *bool           `json:"eager_input_streaming,omitempty"`
	}
	s := shadow{
		Type:                t.Type,
		Name:                t.Name,
		Description:         t.Description,
		InputSchema:         t.InputSchema,
		CacheControl:        t.CacheControl,
		DeferLoading:        t.DeferLoading,
		EagerInputStreaming: t.EagerInputStreaming,
	}
	return json.Marshal(s)
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
	// thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
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
	// document
	Title      string          `json:"title,omitempty"`
	Context    string          `json:"context,omitempty"`
	Citations  json.RawMessage `json:"citations,omitempty"`
	hasTitle   bool
	hasContext bool
}

func (w *BlockWire) UnmarshalJSON(data []byte) error {
	type blockAlias BlockWire
	var decoded blockAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if decoded.Type == BlockTypeDocument {
		for _, key := range []string{"title", "context"} {
			if raw, ok := fields[key]; ok {
				if string(raw) == "null" {
					return fmt.Errorf("anthropic: document %s must be a string", key)
				}
				var value string
				if err := json.Unmarshal(raw, &value); err != nil {
					return fmt.Errorf("anthropic: document %s must be a string", key)
				}
			}
		}
	}
	*w = BlockWire(decoded)
	_, w.hasTitle = fields["title"]
	_, w.hasContext = fields["context"]
	return nil
}

func (w BlockWire) MarshalJSON() ([]byte, error) {
	type blockAlias BlockWire
	data, err := json.Marshal(blockAlias(w))
	if err != nil || (!w.hasTitle && !w.hasContext) {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if w.hasTitle {
		if _, ok := fields["title"]; !ok {
			fields["title"] = json.RawMessage(`""`)
		}
	}
	if w.hasContext {
		if _, ok := fields["context"]; !ok {
			fields["context"] = json.RawMessage(`""`)
		}
	}
	return json.Marshal(fields)
}

// SourceWire is the source object of an image block: a base64 payload
// (media_type + data) or a URL.
type SourceWire struct {
	Type      string `json:"type"` // base64 | url | file
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
	hasFields map[string]bool
}

func (s *SourceWire) UnmarshalJSON(data []byte) error {
	type sourceAlias SourceWire
	var decoded sourceAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"type", "media_type", "data", "url", "file_id"} {
		if raw, ok := fields[key]; ok && string(raw) == "null" {
			return fmt.Errorf("anthropic: source.%s must be a string", key)
		}
	}
	*s = SourceWire(decoded)
	s.hasFields = make(map[string]bool, len(fields))
	for key := range fields {
		s.hasFields[key] = true
	}
	return nil
}

func (s SourceWire) has(key string, value string) bool {
	return s.hasFields[key] || value != ""
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
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens,omitempty"`
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
	Type         string `json:"type"` // text_delta | input_json_delta | thinking_delta | signature_delta | (empty on message_delta)
	Text         string `json:"text,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	Signature    string `json:"signature,omitempty"`
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
