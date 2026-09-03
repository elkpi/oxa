package ir

import (
	"encoding/json"
	"fmt"
)

// SpecVersion is the spec version stamped on every IR document on marshal and
// const-checked on unmarshal. It mirrors the specVersion const pinned by
// spec/schema/ir.schema.json; the schema-alignment test in json_test.go
// verifies the agreement.
const SpecVersion = "0.1.0"

// ---- blocks ---------------------------------------------------------------

type wireTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireImageBlock struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type wireToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type wireToolResultBlock struct {
	Type      string            `json:"type"`
	ToolUseID string            `json:"tool_use_id"`
	Content   []json.RawMessage `json:"content"`
	IsError   bool              `json:"is_error,omitempty"`
}

// MarshalBlock renders one block in its canonical type-discriminated form.
func MarshalBlock(b Block) (json.RawMessage, error) {
	switch v := b.(type) {
	case TextBlock:
		return json.Marshal(wireTextBlock{Type: BlockTypeText, Text: v.Text})
	case ImageBlock:
		return json.Marshal(wireImageBlock{
			Type: BlockTypeImage, MediaType: v.MediaType, Data: v.Data, URL: v.URL,
		})
	case ToolUseBlock:
		if len(v.Input) == 0 {
			return nil, fmt.Errorf("ir: tool_use input is required (INV-1)")
		}
		return json.Marshal(wireToolUseBlock{
			Type: BlockTypeToolUse, ID: v.ID, Name: v.Name, Input: v.Input,
		})
	case ToolResultBlock:
		content, err := marshalBlocks(v.Content)
		if err != nil {
			return nil, err
		}
		return json.Marshal(wireToolResultBlock{
			Type: BlockTypeToolResult, ToolUseID: v.ToolUseID, Content: content, IsError: v.IsError,
		})
	default:
		return nil, fmt.Errorf("ir: unknown block type %T", b)
	}
}

func marshalBlocks(blocks []Block) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(blocks))
	for i, b := range blocks {
		raw, err := MarshalBlock(b)
		if err != nil {
			return nil, fmt.Errorf("block[%d]: %w", i, err)
		}
		out = append(out, raw)
	}
	return out, nil
}

// UnmarshalBlock parses one type-discriminated block. The input of a
// tool_use block and the partial_json of an input_json_delta are kept as the
// exact source JSON string token, so decode/encode round-trips are
// byte-faithful (INV-1).
func UnmarshalBlock(data []byte) (Block, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case BlockTypeText:
		var w wireTextBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return TextBlock{Text: w.Text}, nil
	case BlockTypeImage:
		var w wireImageBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return ImageBlock{MediaType: w.MediaType, Data: w.Data, URL: w.URL}, nil
	case BlockTypeToolUse:
		var w wireToolUseBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if len(w.Input) == 0 {
			return nil, fmt.Errorf("ir: tool_use input is required (INV-1)")
		}
		return ToolUseBlock{ID: w.ID, Name: w.Name, Input: w.Input}, nil
	case BlockTypeToolResult:
		var w wireToolResultBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		content := make([]Block, 0, len(w.Content))
		for i, raw := range w.Content {
			b, err := UnmarshalBlock(raw)
			if err != nil {
				return nil, fmt.Errorf("content[%d]: %w", i, err)
			}
			content = append(content, b)
		}
		return ToolResultBlock{
			ToolUseID: w.ToolUseID, Content: content, IsError: w.IsError,
		}, nil
	default:
		return nil, fmt.Errorf("ir: unknown block type %q", head.Type)
	}
}

func unmarshalBlocks(raws []json.RawMessage) ([]Block, error) {
	out := make([]Block, 0, len(raws))
	for i, raw := range raws {
		b, err := UnmarshalBlock(raw)
		if err != nil {
			return nil, fmt.Errorf("content[%d]: %w", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

// ---- request document ------------------------------------------------------

type wireRequest struct {
	SpecVersion string            `json:"specVersion"`
	Model       string            `json:"model"`
	System      []wireTextBlock   `json:"system,omitempty"`
	Messages    []wireMessage     `json:"messages"`
	Tools       []wireTool        `json:"tools,omitempty"`
	ToolChoice  *wireToolChoice   `json:"tool_choice,omitempty"`
	Params      *wireParams       `json:"params,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type wireMessage struct {
	Role    Role              `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type wireToolChoice struct {
	Mode string `json:"mode"`
	Name string `json:"name,omitempty"`
}

type wireParams struct {
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty"`
	MaxTokens     *int64   `json:"max_tokens,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

// MarshalRequest renders a Request as a canonical IR document, stamping
// specVersion.
func MarshalRequest(req *Request) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("ir: nil request")
	}
	system := make([]wireTextBlock, 0, len(req.System))
	for _, s := range req.System {
		system = append(system, wireTextBlock{Type: BlockTypeText, Text: s.Text})
	}
	messages := make([]wireMessage, 0, len(req.Messages))
	for i, m := range req.Messages {
		content, err := marshalBlocks(m.Content)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		messages = append(messages, wireMessage{Role: m.Role, Content: content})
	}
	tools := make([]wireTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, wireTool(t))
	}
	var tc *wireToolChoice
	if req.ToolChoice != nil {
		tc = &wireToolChoice{Mode: req.ToolChoice.Mode, Name: req.ToolChoice.Name}
	}
	var params *wireParams
	if req.Params.set() {
		params = &wireParams{
			Temperature:   req.Params.Temperature,
			TopP:          req.Params.TopP,
			MaxTokens:     req.Params.MaxTokens,
			StopSequences: req.Params.StopSequences,
		}
	}
	return json.Marshal(wireRequest{
		SpecVersion: SpecVersion,
		Model:       req.Model,
		System:      system,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  tc,
		Params:      params,
		Metadata:    req.Metadata,
	})
}

// UnmarshalRequest parses a canonical IR request document, const-checking
// specVersion.
func UnmarshalRequest(data []byte) (*Request, error) {
	var w wireRequest
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	if err := checkSpecVersion(w.SpecVersion); err != nil {
		return nil, err
	}
	req := &Request{Model: w.Model, Metadata: w.Metadata}
	for _, s := range w.System {
		req.System = append(req.System, SystemBlock{Text: s.Text})
	}
	for i, m := range w.Messages {
		content, err := unmarshalBlocks(m.Content)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		req.Messages = append(req.Messages, Message{Role: m.Role, Content: content})
	}
	for _, t := range w.Tools {
		req.Tools = append(req.Tools, Tool(t))
	}
	if w.ToolChoice != nil {
		req.ToolChoice = &ToolChoice{Mode: w.ToolChoice.Mode, Name: w.ToolChoice.Name}
	}
	if w.Params != nil {
		req.Params = Params{
			Temperature:   w.Params.Temperature,
			TopP:          w.Params.TopP,
			MaxTokens:     w.Params.MaxTokens,
			StopSequences: w.Params.StopSequences,
		}
	}
	return req, nil
}

// ---- response document -----------------------------------------------------

type wireResponse struct {
	SpecVersion  string            `json:"specVersion"`
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	Content      []json.RawMessage `json:"content"`
	StopReason   StopReason        `json:"stop_reason"`
	StopSequence string            `json:"stop_sequence,omitempty"`
	Usage        wireUsage         `json:"usage"`
}

type wireUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// MarshalResponse renders a Response as a canonical IR document, stamping
// specVersion.
func MarshalResponse(resp *Response) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("ir: nil response")
	}
	content, err := marshalBlocks(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	return json.Marshal(wireResponse{
		SpecVersion:  SpecVersion,
		ID:           resp.ID,
		Model:        resp.Model,
		Content:      content,
		StopReason:   resp.StopReason,
		StopSequence: resp.StopSequence,
		Usage: wireUsage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	})
}

// UnmarshalResponse parses a canonical IR response document, const-checking
// specVersion.
func UnmarshalResponse(data []byte) (*Response, error) {
	var w wireResponse
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	if err := checkSpecVersion(w.SpecVersion); err != nil {
		return nil, err
	}
	content, err := unmarshalBlocks(w.Content)
	if err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	return &Response{
		ID:           w.ID,
		Model:        w.Model,
		Content:      content,
		StopReason:   w.StopReason,
		StopSequence: w.StopSequence,
		Usage:        Usage{InputTokens: w.Usage.InputTokens, OutputTokens: w.Usage.OutputTokens},
	}, nil
}

// ---- deltas and events -----------------------------------------------------

type wireTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireInputJSONDelta struct {
	Type        string          `json:"type"`
	PartialJSON json.RawMessage `json:"partial_json"`
}

// MarshalDelta renders one delta in its canonical type-discriminated form.
func MarshalDelta(d Delta) (json.RawMessage, error) {
	switch v := d.(type) {
	case TextDelta:
		return json.Marshal(wireTextDelta{Type: DeltaTypeTextDelta, Text: v.Text})
	case InputJSONDelta:
		return json.Marshal(wireInputJSONDelta{
			Type: DeltaTypeInputJSONDelta, PartialJSON: v.PartialJSON,
		})
	default:
		return nil, fmt.Errorf("ir: unknown delta type %T", d)
	}
}

// UnmarshalDelta parses one type-discriminated delta.
func UnmarshalDelta(data []byte) (Delta, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case DeltaTypeTextDelta:
		var w wireTextDelta
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return TextDelta{Text: w.Text}, nil
	case DeltaTypeInputJSONDelta:
		var w wireInputJSONDelta
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return InputJSONDelta{PartialJSON: w.PartialJSON}, nil
	default:
		return nil, fmt.Errorf("ir: unknown delta type %q", head.Type)
	}
}

// MarshalEvent renders one event in its canonical type-discriminated form.
func MarshalEvent(e Event) (json.RawMessage, error) {
	switch v := e.(type) {
	case MessageStart:
		return json.Marshal(struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			Model string `json:"model"`
		}{Type: EventTypeMessageStart, ID: v.ID, Model: v.Model})
	case ContentBlockStart:
		block, err := MarshalBlock(v.Block)
		if err != nil {
			return nil, fmt.Errorf("block: %w", err)
		}
		return json.Marshal(struct {
			Type  string          `json:"type"`
			Index int             `json:"index"`
			Block json.RawMessage `json:"block"`
		}{Type: EventTypeContentBlockStart, Index: v.Index, Block: block})
	case ContentBlockDelta:
		delta, err := MarshalDelta(v.Delta)
		if err != nil {
			return nil, fmt.Errorf("delta: %w", err)
		}
		return json.Marshal(struct {
			Type  string          `json:"type"`
			Index int             `json:"index"`
			Delta json.RawMessage `json:"delta"`
		}{Type: EventTypeContentBlockDelta, Index: v.Index, Delta: delta})
	case ContentBlockStop:
		return json.Marshal(struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}{Type: EventTypeContentBlockStop, Index: v.Index})
	case MessageDelta:
		return json.Marshal(struct {
			Type         string     `json:"type"`
			StopReason   StopReason `json:"stop_reason"`
			StopSequence string     `json:"stop_sequence,omitempty"`
			Usage        wireUsage  `json:"usage"`
		}{Type: EventTypeMessageDelta, StopReason: v.StopReason, StopSequence: v.StopSequence,
			Usage: wireUsage{InputTokens: v.Usage.InputTokens, OutputTokens: v.Usage.OutputTokens}})
	case MessageDone:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{Type: EventTypeMessageDone})
	default:
		return nil, fmt.Errorf("ir: unknown event type %T", e)
	}
}

// UnmarshalEvent parses one type-discriminated event.
func UnmarshalEvent(data []byte) (Event, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case EventTypeMessageStart:
		var w struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return MessageStart{ID: w.ID, Model: w.Model}, nil
	case EventTypeContentBlockStart:
		var w struct {
			Index int             `json:"index"`
			Block json.RawMessage `json:"block"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		block, err := UnmarshalBlock(w.Block)
		if err != nil {
			return nil, fmt.Errorf("block: %w", err)
		}
		return ContentBlockStart{Index: w.Index, Block: block}, nil
	case EventTypeContentBlockDelta:
		var w struct {
			Index int             `json:"index"`
			Delta json.RawMessage `json:"delta"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		delta, err := UnmarshalDelta(w.Delta)
		if err != nil {
			return nil, fmt.Errorf("delta: %w", err)
		}
		return ContentBlockDelta{Index: w.Index, Delta: delta}, nil
	case EventTypeContentBlockStop:
		var w struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return ContentBlockStop{Index: w.Index}, nil
	case EventTypeMessageDelta:
		var w struct {
			StopReason   StopReason `json:"stop_reason"`
			StopSequence string     `json:"stop_sequence"`
			Usage        wireUsage  `json:"usage"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return MessageDelta{
			StopReason:   w.StopReason,
			StopSequence: w.StopSequence,
			Usage:        Usage{InputTokens: w.Usage.InputTokens, OutputTokens: w.Usage.OutputTokens},
		}, nil
	case EventTypeMessageDone:
		return MessageDone{}, nil
	default:
		return nil, fmt.Errorf("ir: unknown event type %q", head.Type)
	}
}

// ---- event stream document ---------------------------------------------------

type wireEventStream struct {
	SpecVersion string            `json:"specVersion"`
	Events      []json.RawMessage `json:"events"`
}

// MarshalEventStream renders an EventStream as a canonical IR document,
// stamping specVersion.
func MarshalEventStream(es *EventStream) ([]byte, error) {
	events := make([]json.RawMessage, 0, len(es.Events))
	for i, e := range es.Events {
		raw, err := MarshalEvent(e)
		if err != nil {
			return nil, fmt.Errorf("events[%d]: %w", i, err)
		}
		events = append(events, raw)
	}
	return json.Marshal(wireEventStream{SpecVersion: SpecVersion, Events: events})
}

// UnmarshalEventStream parses a canonical IR event-stream document,
// const-checking specVersion.
func UnmarshalEventStream(data []byte) (*EventStream, error) {
	var w wireEventStream
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	if err := checkSpecVersion(w.SpecVersion); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(w.Events))
	for i, raw := range w.Events {
		e, err := UnmarshalEvent(raw)
		if err != nil {
			return nil, fmt.Errorf("events[%d]: %w", i, err)
		}
		events = append(events, e)
	}
	return &EventStream{Events: events}, nil
}

func checkSpecVersion(v string) error {
	if v != SpecVersion {
		return fmt.Errorf("ir: unsupported specVersion %q (want %q)", v, SpecVersion)
	}
	return nil
}
