// Package responses implements the OpenAI Responses face of the oxa
// conversion library: the non-streaming TEXT/SYSTEM/MULTI-TURN/PARAMS/
// TOOLS/IMAGE subset of this milestone, converted to and from the IR
// (spec/01). Wire types are strictly separate from IR types; this package
// imports ir and the standard library only, never another face.
package responses

import (
	"encoding/json"
	"fmt"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleDeveloper = "developer"

	ToolTypeFunction                      = "function"
	ToolTypeCustom                        = "custom"
	ToolTypeApplyPatch                    = "apply_patch"
	ToolTypeNamespace                     = "namespace"
	ToolTypeToolbox                       = "toolbox"
	ItemTypeCustomToolCall                = "custom_tool_call"
	ItemTypeCustomToolCallOutput          = "custom_tool_call_output"
	ItemTypeApplyPatchCall                = "apply_patch_call"
	ItemTypeApplyPatchCallOutput          = "apply_patch_call_output"
	EventTypeResponseCustomToolInputDelta = "response.custom_tool_call_input.delta"
	EventTypeResponseCustomToolInputDone  = "response.custom_tool_call_input.done"

	ToolChoiceAuto     = "auto"
	ToolChoiceNone     = "none"
	ToolChoiceRequired = "required"

	ItemTypeMessage            = "message"
	ItemTypeFunctionCall       = "function_call"
	ItemTypeFunctionCallOutput = "function_call_output"
	ItemTypeReasoning          = "reasoning"
	ItemTypeAdditionalTools    = "additional_tools"
	ItemTypeWebSearchCall      = "web_search_call"

	PartTypeInputText   = "input_text"
	PartTypeOutputText  = "output_text"
	PartTypeSummaryText = "summary_text"
	PartTypeInputImage  = "input_image"
	PartTypeInputFile   = "input_file"

	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusIncomplete = "incomplete"
	StatusFailed     = "failed"

	IncompleteReasonMaxOutputTokens = "max_output_tokens"
	IncompleteReasonContentFilter   = "content_filter"

	ErrorCodeRefusal = "refusal"

	ObjectResponse = "response"

	EventTypeResponseCreated                   = "response.created"
	EventTypeResponseOutputItemAdded           = "response.output_item.added"
	EventTypeResponseOutputItemDone            = "response.output_item.done"
	EventTypeResponseContentPartAdded          = "response.content_part.added"
	EventTypeResponseContentPartDone           = "response.content_part.done"
	EventTypeResponseOutputTextDelta           = "response.output_text.delta"
	EventTypeResponseOutputTextDone            = "response.output_text.done"
	EventTypeResponseReasoningSummaryPartAdded = "response.reasoning_summary_part.added"
	EventTypeResponseReasoningSummaryPartDone  = "response.reasoning_summary_part.done"
	EventTypeResponseReasoningSummaryTextDelta = "response.reasoning_summary_text.delta"
	EventTypeResponseReasoningSummaryTextDone  = "response.reasoning_summary_text.done"
	EventTypeResponseFunctionCallArgsDelta     = "response.function_call_arguments.delta"
	EventTypeResponseFunctionCallArgsDone      = "response.function_call_arguments.done"
	EventTypeResponseCompleted                 = "response.completed"
	EventTypeResponseIncomplete                = "response.incomplete"
	EventTypeResponseFailed                    = "response.failed"

	EventTypeResponseContentPartPrefix          = "response.content_part."
	EventTypeResponseOutputTextPrefix           = "response.output_text."
	EventTypeResponseReasoningSummaryPartPrefix = "response.reasoning_summary_part."
	EventTypeResponseReasoningSummaryTextPrefix = "response.reasoning_summary_text."
)

// Request is the Responses wire request for the supported non-streaming
// subset.
type Request struct {
	Model           string    `json:"model"`
	Input           Input     `json:"input"`
	Instructions    *string   `json:"instructions,omitempty"`
	Temperature     *float64  `json:"temperature,omitempty"`
	TopP            *float64  `json:"top_p,omitempty"`
	MaxOutputTokens *int64    `json:"max_output_tokens,omitempty"`
	Tools           []ToolDef `json:"tools,omitempty"`
	ToolChoice      any       `json:"tool_choice,omitempty"` // auto | none | required | ToolChoiceWire
	Metadata        any       `json:"metadata,omitempty"`

	// Text carries output-shaping parameters. Verbosity and Format have no IR
	// equivalent in v1 and are dropped with unmapped-field losses (N-R-8).
	Text *TextParams `json:"text,omitempty"`

	// Reasoning effort configuration has no IR equivalent in v1 and is dropped
	// with an unmapped-field loss (N-R-8).
	Reasoning any `json:"reasoning,omitempty"`

	// ParallelToolCalls has no IR equivalent in v1 and is dropped with an
	// unmapped-field loss (N-R-8).
	ParallelToolCalls *bool `json:"parallel_tool_calls,omitempty"`
	Store             *bool `json:"store,omitempty"`
}

// TextParams is the Responses text output-shaping object.
type TextParams struct {
	Verbosity *string `json:"verbosity,omitempty"`
	Format    any     `json:"format,omitempty"` // TextFormatWire or map[string]any
}

// TextFormatWire is the wire shape for Responses text.format (spec/11 s3).
type TextFormatWire struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// Input is the Responses request input: either a plain string (the string
// shorthand) or an array of input items. The zero value carries neither and
// is a structural error on decode.
type Input struct {
	Text  *string
	Items []InputItem
}

// UnmarshalJSON accepts the string shorthand or an input-item array.
func (in *Input) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		in.Text = &text
		return nil
	}
	var items []InputItem
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("responses: input is neither string nor item array: %w", err)
	}
	in.Items = items
	return nil
}

// MarshalJSON renders the string shorthand when only text is present and the
// item array otherwise.
func (in Input) MarshalJSON() ([]byte, error) {
	if in.Text != nil && len(in.Items) == 0 {
		return json.Marshal(*in.Text)
	}
	if len(in.Items) == 0 {
		return []byte("null"), nil
	}
	return json.Marshal(in.Items)
}

// InputItem is one element of a Responses input array: a message item
// (type absent or "message", role user/assistant/system), a function_call
// item, a function_call_output item, or an unknown type dropped with a loss.
type InputItem struct {
	ID        string                     `json:"id,omitempty"`
	Type      string                     `json:"type,omitempty"`
	Role      string                     `json:"role,omitempty"`
	Content   any                        `json:"content,omitempty"` // string or []ContentPart
	CallID    string                     `json:"call_id,omitempty"`
	Name      string                     `json:"name,omitempty"`
	Namespace string                     `json:"namespace,omitempty"`
	Arguments string                     `json:"arguments,omitempty"`
	Output    any                        `json:"output,omitempty"`
	Input     *string                    `json:"input,omitempty"`
	Status    string                     `json:"status,omitempty"`
	Operation *ApplyPatchOperationWire   `json:"operation,omitempty"`
	Summary   []OutputContent            `json:"summary,omitempty"`
	Tools     []json.RawMessage          `json:"tools,omitempty"`
	Action    json.RawMessage            `json:"action,omitempty"`
	Extra     map[string]json.RawMessage `json:"-"`
}

// ContentPart is one element of a parts-array item content: input_text,
// output_text, input_image, or input_file.
type ContentPart struct {
	Type     string  `json:"type"`
	Text     string  `json:"text,omitempty"`
	Refusal  string  `json:"refusal,omitempty"`
	ImageURL string  `json:"image_url,omitempty"`
	FileData *string `json:"file_data,omitempty"`
	FileURL  *string `json:"file_url,omitempty"`
	FileID   *string `json:"file_id,omitempty"`
	Filename *string `json:"filename,omitempty"`
	Detail   *string `json:"detail,omitempty"`
}

func (c ContentPart) MarshalJSON() ([]byte, error) {
	if c.Type == "refusal" {
		return json.Marshal(struct {
			Type    string `json:"type"`
			Refusal string `json:"refusal"`
		}{
			Type:    c.Type,
			Refusal: c.Refusal,
		})
	}
	type shadow ContentPart
	return json.Marshal(shadow(c))
}

func (c *ContentPart) UnmarshalJSON(data []byte) error {
	type shadow ContentPart
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s.Type == "refusal" {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		ref, present := raw["refusal"]
		if !present {
			return fmt.Errorf("responses: refusal content part requires refusal string")
		}
		var str string
		if err := json.Unmarshal(ref, &str); err != nil {
			return fmt.Errorf("responses: refusal content part requires string refusal: %w", err)
		}
		s.Refusal = str
	}
	*c = ContentPart(s)
	return nil
}

// ToolDef is one element of a wire tools array.
type ToolDef struct {
	Type          string                `json:"type"`
	Name          string                `json:"name,omitempty"`
	Description   string                `json:"description,omitempty"`
	Parameters    json.RawMessage       `json:"parameters,omitempty"`
	Strict        *bool                 `json:"strict,omitempty"`
	Format        *CustomToolFormatWire `json:"format,omitempty"`
	DeferLoading  *bool                 `json:"defer_loading,omitempty"`
	Tools         []json.RawMessage     `json:"tools,omitempty"`
	RawDefinition string                `json:"-"`
}

func (td ToolDef) MarshalJSON() ([]byte, error) {
	if td.RawDefinition != "" {
		return []byte(td.RawDefinition), nil
	}
	type Alias ToolDef
	return json.Marshal((Alias)(td))
}

func (td *ToolDef) UnmarshalJSON(data []byte) error {
	td.RawDefinition = string(data)
	type Alias ToolDef
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*td = ToolDef(a)
	td.RawDefinition = string(data)
	return nil
}

type ApplyPatchOperationWire struct {
	Type string `json:"type"`
	Path any    `json:"path,omitempty"`
	Diff any    `json:"diff,omitempty"`
}

type CustomToolFormatWire struct {
	Type       string  `json:"type"`
	Syntax     string  `json:"syntax,omitempty"`
	Definition *string `json:"definition,omitempty"`
}

// ToolChoiceWire forces one named function or custom or apply_patch tool.
type ToolChoiceWire struct {
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// Response is the Responses wire response object.
type Response struct {
	ID                string          `json:"id"`
	Object            string          `json:"object"`
	Status            string          `json:"status"`
	Model             string          `json:"model"`
	Output            []OutputItem    `json:"output"`
	Usage             *UsageWire      `json:"usage,omitempty"`
	IncompleteDetails *IncompleteWire `json:"incomplete_details,omitempty"`
	Error             *ErrorWire      `json:"error,omitempty"`
}

// IncompleteWire explains why a response stopped early.
type IncompleteWire struct {
	Reason string `json:"reason,omitempty"`
}

// ErrorWire carries a failed response's error.
type ErrorWire struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// OutputItem is one element of a wire response's output array: a message
// item, a function_call item, or a reasoning item (dropped with a loss).
type OutputItem struct {
	Type             string                     `json:"type"`
	ID               string                     `json:"id,omitempty"`
	Status           string                     `json:"status,omitempty"`
	Role             string                     `json:"role,omitempty"`
	Content          []OutputContent            `json:"content,omitempty"`
	CallID           string                     `json:"call_id,omitempty"`
	Name             string                     `json:"name,omitempty"`
	Namespace        string                     `json:"namespace,omitempty"`
	Arguments        string                     `json:"arguments,omitempty"`
	Input            *string                    `json:"input,omitempty"`
	Output           *string                    `json:"output,omitempty"`
	Operation        *ApplyPatchOperationWire   `json:"operation,omitempty"`
	Summary          []OutputContent            `json:"summary,omitempty"`
	EncryptedContent string                     `json:"encrypted_content,omitempty"`
	Action           json.RawMessage            `json:"action,omitempty"`
	Extra            map[string]json.RawMessage `json:"-"`
}

// OutputContent is one element of a message output item's content array.
type OutputContent struct {
	Type        string            `json:"type"`
	Text        string            `json:"text"`
	Refusal     string            `json:"refusal,omitempty"`
	Annotations []json.RawMessage `json:"annotations"`
}

func (c OutputContent) MarshalJSON() ([]byte, error) {
	if c.Type == "refusal" {
		return json.Marshal(struct {
			Type    string `json:"type"`
			Refusal string `json:"refusal"`
		}{
			Type:    c.Type,
			Refusal: c.Refusal,
		})
	}
	type shadow OutputContent
	return json.Marshal(shadow(c))
}

func (c *OutputContent) UnmarshalJSON(data []byte) error {
	type shadow OutputContent
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s.Type == "refusal" {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		ref, present := raw["refusal"]
		if !present {
			return fmt.Errorf("responses: refusal content part requires refusal string")
		}
		var str string
		if err := json.Unmarshal(ref, &str); err != nil {
			return fmt.Errorf("responses: refusal content part requires string refusal: %w", err)
		}
		s.Refusal = str
	}
	*c = OutputContent(s)
	return nil
}

// UsageWire is the wire usage object. total_tokens is derived (input +
// output) and recomputed on encode, so its absence on the IR side carries no
// loss (vectors/README.md loss conventions, DERIVED fields).
type UsageWire struct {
	InputTokens        int64                   `json:"input_tokens"`
	OutputTokens       int64                   `json:"output_tokens"`
	TotalTokens        int64                   `json:"total_tokens"`
	InputTokenDetails  *InputTokenDetailsWire  `json:"input_token_details,omitempty"`
	OutputTokenDetails *OutputTokenDetailsWire `json:"output_token_details,omitempty"`
}

// InputTokenDetailsWire carries fine-grained input token counts.
type InputTokenDetailsWire struct {
	CachedTokens int64 `json:"cached_tokens"`
}

// OutputTokenDetailsWire carries fine-grained output token counts.
type OutputTokenDetailsWire struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

// StreamEvent is one OpenAI Responses streaming SSE event. Type is the
// discriminating head. The payload fields model response and output-item
// lifecycles, text content parts, and function-call argument deltas and
// completion validation. SequenceNumber is an envelope field and has no IR
// equivalent.
type StreamEvent struct {
	Type           string         `json:"type"`
	Response       *Response      `json:"response,omitempty"`
	ItemID         string         `json:"item_id,omitempty"`
	OutputIndex    int            `json:"output_index,omitempty"`
	ContentIndex   int            `json:"content_index,omitempty"`
	Item           *OutputItem    `json:"item,omitempty"`
	Part           *OutputContent `json:"part,omitempty"`
	Delta          string         `json:"delta,omitempty"`
	Text           string         `json:"text,omitempty"`
	CallID         string         `json:"call_id,omitempty"`
	Name           string         `json:"name,omitempty"`
	Arguments      string         `json:"arguments,omitempty"`
	Input          *string        `json:"input,omitempty"`
	SequenceNumber int64          `json:"sequence_number,omitempty"`
}

// MarshalJSON renders each typed event's native wire envelope. Index zero is
// meaningful for output_index and content_index, but must not appear on event
// types which do not use indexes.
func (e StreamEvent) MarshalJSON() ([]byte, error) {
	type sequence struct {
		SequenceNumber int64 `json:"sequence_number,omitempty"`
	}
	switch e.Type {
	case EventTypeResponseCreated, EventTypeResponseCompleted, EventTypeResponseIncomplete, EventTypeResponseFailed:
		if e.Response == nil {
			return nil, fmt.Errorf("responses: %s without response", e.Type)
		}
		return json.Marshal(struct {
			Type     string    `json:"type"`
			Response *Response `json:"response"`
			sequence
		}{Type: e.Type, Response: e.Response, sequence: sequence{e.SequenceNumber}})
	case EventTypeResponseOutputItemAdded, EventTypeResponseOutputItemDone:
		if e.Item == nil {
			return nil, fmt.Errorf("responses: %s without item", e.Type)
		}
		return json.Marshal(struct {
			Type        string      `json:"type"`
			OutputIndex int         `json:"output_index"`
			Item        *OutputItem `json:"item"`
			sequence
		}{Type: e.Type, OutputIndex: e.OutputIndex, Item: e.Item, sequence: sequence{e.SequenceNumber}})
	case EventTypeResponseContentPartAdded, EventTypeResponseContentPartDone,
		EventTypeResponseReasoningSummaryPartAdded, EventTypeResponseReasoningSummaryPartDone:
		if e.Part == nil {
			return nil, fmt.Errorf("responses: %s without part", e.Type)
		}
		return json.Marshal(struct {
			Type         string         `json:"type"`
			ItemID       string         `json:"item_id"`
			OutputIndex  int            `json:"output_index"`
			ContentIndex int            `json:"content_index"`
			Part         *OutputContent `json:"part"`
			sequence
		}{
			Type: e.Type, ItemID: e.ItemID, OutputIndex: e.OutputIndex,
			ContentIndex: e.ContentIndex, Part: e.Part, sequence: sequence{e.SequenceNumber},
		})
	case EventTypeResponseCustomToolInputDone:
		if e.Input == nil {
			return nil, fmt.Errorf("responses: custom input.done requires input")
		}
		return json.Marshal(struct {
			Type        string `json:"type"`
			ItemID      string `json:"item_id"`
			OutputIndex int    `json:"output_index"`
			Input       string `json:"input"`
			sequence
		}{e.Type, e.ItemID, e.OutputIndex, *e.Input, sequence{e.SequenceNumber}})
	case EventTypeResponseFunctionCallArgsDelta, EventTypeResponseCustomToolInputDelta:
		return json.Marshal(struct {
			Type        string `json:"type"`
			ItemID      string `json:"item_id"`
			OutputIndex int    `json:"output_index"`
			Delta       string `json:"delta"`
			sequence
		}{
			Type: e.Type, ItemID: e.ItemID, OutputIndex: e.OutputIndex,
			Delta: e.Delta, sequence: sequence{e.SequenceNumber},
		})
	case EventTypeResponseFunctionCallArgsDone:
		return json.Marshal(struct {
			Type        string `json:"type"`
			ItemID      string `json:"item_id"`
			OutputIndex int    `json:"output_index"`
			CallID      string `json:"call_id"`
			Name        string `json:"name"`
			Arguments   string `json:"arguments"`
			sequence
		}{
			Type: e.Type, ItemID: e.ItemID, OutputIndex: e.OutputIndex,
			CallID: e.CallID, Name: e.Name, Arguments: e.Arguments,
			sequence: sequence{e.SequenceNumber},
		})
	case EventTypeResponseOutputTextDelta, EventTypeResponseReasoningSummaryTextDelta:
		return json.Marshal(struct {
			Type         string `json:"type"`
			ItemID       string `json:"item_id"`
			OutputIndex  int    `json:"output_index"`
			ContentIndex int    `json:"content_index"`
			Delta        string `json:"delta"`
			sequence
		}{
			Type: e.Type, ItemID: e.ItemID, OutputIndex: e.OutputIndex,
			ContentIndex: e.ContentIndex, Delta: e.Delta, sequence: sequence{e.SequenceNumber},
		})
	case EventTypeResponseOutputTextDone, EventTypeResponseReasoningSummaryTextDone:
		return json.Marshal(struct {
			Type         string `json:"type"`
			ItemID       string `json:"item_id"`
			OutputIndex  int    `json:"output_index"`
			ContentIndex int    `json:"content_index"`
			Text         string `json:"text"`
			sequence
		}{
			Type: e.Type, ItemID: e.ItemID, OutputIndex: e.OutputIndex,
			ContentIndex: e.ContentIndex, Text: e.Text, sequence: sequence{e.SequenceNumber},
		})
	default:
		type plainStreamEvent StreamEvent
		return json.Marshal(plainStreamEvent(e))
	}
}
