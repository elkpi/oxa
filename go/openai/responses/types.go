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

// Request is the Responses wire request for the supported non-streaming
// subset.
type Request struct {
	Model           string            `json:"model"`
	Input           Input             `json:"input"`
	Instructions    *string           `json:"instructions,omitempty"`
	Temperature     *float64          `json:"temperature,omitempty"`
	TopP            *float64          `json:"top_p,omitempty"`
	MaxOutputTokens *int64            `json:"max_output_tokens,omitempty"`
	Tools           []ToolDef         `json:"tools,omitempty"`
	ToolChoice      any               `json:"tool_choice,omitempty"` // auto | none | required | ToolChoiceWire
	Metadata        map[string]string `json:"metadata,omitempty"`

	// Text carries output-shaping parameters. Verbosity and Format have no IR
	// equivalent in v1 and are dropped with unmapped-field losses (N-R-8).
	Text *TextParams `json:"text,omitempty"`

	// Reasoning effort configuration has no IR equivalent in v1 and is dropped
	// with an unmapped-field loss (N-R-8).
	Reasoning any `json:"reasoning,omitempty"`

	// ParallelToolCalls has no IR equivalent in v1 and is dropped with an
	// unmapped-field loss (N-R-8).
	ParallelToolCalls *bool `json:"parallel_tool_calls,omitempty"`
}

// TextParams is the Responses text output-shaping object.
type TextParams struct {
	Verbosity *string `json:"verbosity,omitempty"`
	Format    any     `json:"format,omitempty"`
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
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"` // string or []ContentPart
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// ContentPart is one element of a parts-array item content: input_text,
// output_text, or input_image.
type ContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// ToolDef is one element of a wire tools array. The supported Responses tool
// variant is type:function with a flat name/description/parameters shape.
// Strict has no IR equivalent in v1 and is dropped with an unmapped-field
// loss (N-R-8).
type ToolDef struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// ToolChoiceWire forces one named function (Responses named form is flat:
// {type: "function", name}).
type ToolChoiceWire struct {
	Type string `json:"type"`
	Name string `json:"name"`
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
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Status    string          `json:"status,omitempty"`
	Role      string          `json:"role,omitempty"`
	Content   []OutputContent `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
}

// OutputContent is one element of a message output item's content array.
type OutputContent struct {
	Type        string            `json:"type"`
	Text        string            `json:"text"`
	Annotations []json.RawMessage `json:"annotations"`
}

// UsageWire is the wire usage object. total_tokens is derived (input +
// output) and recomputed on encode, so its absence on the IR side carries no
// loss (vectors/README.md loss conventions, DERIVED fields).
type UsageWire struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
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
	case "response.created", "response.completed", "response.incomplete", "response.failed":
		if e.Response == nil {
			return nil, fmt.Errorf("responses: %s without response", e.Type)
		}
		return json.Marshal(struct {
			Type     string    `json:"type"`
			Response *Response `json:"response"`
			sequence
		}{Type: e.Type, Response: e.Response, sequence: sequence{e.SequenceNumber}})
	case "response.output_item.added", "response.output_item.done":
		if e.Item == nil {
			return nil, fmt.Errorf("responses: %s without item", e.Type)
		}
		return json.Marshal(struct {
			Type        string      `json:"type"`
			OutputIndex int         `json:"output_index"`
			Item        *OutputItem `json:"item"`
			sequence
		}{Type: e.Type, OutputIndex: e.OutputIndex, Item: e.Item, sequence: sequence{e.SequenceNumber}})
	case "response.content_part.added", "response.content_part.done":
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
	case "response.function_call_arguments.delta":
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
	case "response.function_call_arguments.done":
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
	case "response.output_text.delta":
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
	case "response.output_text.done":
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
