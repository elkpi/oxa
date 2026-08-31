package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

// StreamEncoder incrementally converts an IR event stream into Anthropic
// Messages wire events (IR -> face), enforcing the INV-5 grammar. The mapping
// is near-identity: Anthropic is the block model, so each IR event maps to
// the correspondingly named wire event. Envelope fields absent from the IR
// render with the documented defaults and record no loss.
type StreamEncoder struct {
	models    modelmap.Table
	id        string
	model     string
	started   bool
	nextIndex int
	openIndex int
	blockOpen bool
	openTool  bool
	toolInput string
	toolParts []string
	deltaSeen bool
	done      bool
}

// NewStreamEncoder returns an event-stream encoder. The variadic Options match
// the package conversion functions (WithModelMap applies to the message
// envelope Model).
func NewStreamEncoder(opts ...Option) *StreamEncoder {
	o := newOptions(opts...)
	return &StreamEncoder{models: o.models}
}

// MarshalJSON keeps message_delta's envelope form free of a zero delta type and
// keeps empty input_json_delta fragments present on the wire.
func (d StreamDelta) MarshalJSON() ([]byte, error) {
	type deltaWire struct {
		Type         string  `json:"type,omitempty"`
		Text         string  `json:"text,omitempty"`
		PartialJSON  *string `json:"partial_json,omitempty"`
		StopReason   string  `json:"stop_reason,omitempty"`
		StopSequence string  `json:"stop_sequence,omitempty"`
	}
	wire := deltaWire{
		Type:         d.Type,
		Text:         d.Text,
		StopReason:   d.StopReason,
		StopSequence: d.StopSequence,
	}
	if d.Type == "input_json_delta" || d.PartialJSON != "" {
		partial := d.PartialJSON
		wire.PartialJSON = &partial
	}
	return json.Marshal(wire)
}

// Apply pushes one IR event and returns the wire events it produces. The
// mapping is near-identity except that a tool block stop may synthesize one
// full input_json_delta when no argument deltas were supplied. Out-of-grammar
// orderings are structural errors.
func (e *StreamEncoder) Apply(ev ir.Event) ([]*StreamEvent, []ir.Loss, error) {
	if ev == nil {
		return nil, nil, fmt.Errorf("anthropic: nil event")
	}
	if e.done {
		return nil, nil, fmt.Errorf("anthropic: event applied after MessageDone (%T)", ev)
	}
	switch event := ev.(type) {
	case ir.MessageStart:
		if e.started {
			return nil, nil, fmt.Errorf("anthropic: duplicate MessageStart")
		}
		e.started = true
		e.id = event.ID
		e.model = e.models.Map(event.Model)
		// Wire envelope defaults: type "message", role "assistant", empty
		// content, null stop_reason (the Go empty string), zero usage.
		return []*StreamEvent{{Type: "message_start", Message: &Response{
			ID:      e.id,
			Type:    "message",
			Role:    "assistant",
			Model:   e.model,
			Content: []BlockWire{},
			Usage:   &UsageWire{},
		}}}, nil, nil
	case ir.ContentBlockStart:
		if !e.started || e.blockOpen {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockStart out of grammar order")
		}
		if event.Index != e.nextIndex {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockStart index %d, want %d", event.Index, e.nextIndex)
		}
		switch block := event.Block.(type) {
		case ir.TextBlock:
			e.nextIndex++
			e.blockOpen = true
			e.openTool = false
			e.openIndex = event.Index
			return []*StreamEvent{{Type: "content_block_start", Index: event.Index, ContentBlock: &BlockWire{Type: "text", Text: block.Text}}}, nil, nil
		case ir.ToolUseBlock:
			if block.ID == "" {
				return nil, nil, fmt.Errorf("anthropic: ContentBlockStart tool_use id is required")
			}
			if block.Name == "" {
				return nil, nil, fmt.Errorf("anthropic: ContentBlockStart tool_use name is required")
			}
			input, err := inputFromIRString(block.Input)
			if err != nil {
				return nil, nil, fmt.Errorf("anthropic: ContentBlockStart tool_use input: %w", err)
			}
			e.nextIndex++
			e.blockOpen = true
			e.openTool = true
			e.openIndex = event.Index
			e.toolInput = string(input)
			e.toolParts = nil
			return []*StreamEvent{{Type: "content_block_start", Index: event.Index, ContentBlock: &BlockWire{
				Type: "tool_use", ID: block.ID, Name: block.Name, Input: json.RawMessage("{}"),
			}}}, nil, nil
		default:
			return nil, nil, fmt.Errorf("anthropic: ContentBlockStart carries an unsupported block %T", event.Block)
		}
	case ir.ContentBlockDelta:
		if !e.blockOpen || event.Index != e.openIndex {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockDelta out of grammar order")
		}
		switch delta := event.Delta.(type) {
		case ir.TextDelta:
			if e.openTool {
				return nil, nil, fmt.Errorf("anthropic: TextDelta on tool_use block")
			}
			return []*StreamEvent{{Type: "content_block_delta", Index: event.Index, Delta: &StreamDelta{Type: "text_delta", Text: delta.Text}}}, nil, nil
		case ir.InputJSONDelta:
			if !e.openTool {
				return nil, nil, fmt.Errorf("anthropic: InputJSONDelta on text block")
			}
			fragment, err := streamInputFragment(delta.PartialJSON)
			if err != nil {
				return nil, nil, fmt.Errorf("anthropic: InputJSONDelta: %w", err)
			}
			e.toolParts = append(e.toolParts, fragment)
			return []*StreamEvent{{Type: "content_block_delta", Index: event.Index, Delta: &StreamDelta{
				Type: "input_json_delta", PartialJSON: fragment,
			}}}, nil, nil
		default:
			return nil, nil, fmt.Errorf("anthropic: ContentBlockDelta carries an unsupported delta %T", event.Delta)
		}
	case ir.ContentBlockStop:
		if !e.blockOpen || event.Index != e.openIndex {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockStop out of grammar order")
		}
		if e.openTool {
			if len(e.toolParts) == 0 {
				return e.toolStopWithSynthesizedDelta(event.Index), nil, nil
			}
			if strings.Join(e.toolParts, "") != e.toolInput {
				return nil, nil, fmt.Errorf("anthropic: tool input fragments do not match ToolUseBlock input")
			}
		}
		e.blockOpen = false
		e.openTool = false
		e.toolInput = ""
		e.toolParts = nil
		return []*StreamEvent{{Type: "content_block_stop", Index: event.Index}}, nil, nil
	case ir.MessageDelta:
		if !e.started || e.blockOpen || e.deltaSeen {
			return nil, nil, fmt.Errorf("anthropic: MessageDelta out of grammar order")
		}
		reason, seq, err := encodeStopReason(event.StopReason, event.StopSequence)
		if err != nil {
			return nil, nil, err
		}
		e.deltaSeen = true
		return []*StreamEvent{{Type: "message_delta", Delta: &StreamDelta{StopReason: reason, StopSequence: seq}, Usage: &UsageWire{
			InputTokens:  event.Usage.InputTokens,
			OutputTokens: event.Usage.OutputTokens,
		}}}, nil, nil
	case ir.MessageDone:
		if !e.deltaSeen {
			return nil, nil, fmt.Errorf("anthropic: MessageDone out of grammar order")
		}
		e.done = true
		return []*StreamEvent{{Type: "message_stop"}}, nil, nil
	default:
		return nil, nil, fmt.Errorf("anthropic: unknown event %T", ev)
	}
}

func (e *StreamEncoder) toolStopWithSynthesizedDelta(index int) []*StreamEvent {
	wire := []*StreamEvent{
		{Type: "content_block_delta", Index: index, Delta: &StreamDelta{
			Type: "input_json_delta", PartialJSON: e.toolInput,
		}},
		{Type: "content_block_stop", Index: index},
	}
	e.blockOpen = false
	e.openTool = false
	e.toolInput = ""
	e.toolParts = nil
	return wire
}

func streamInputFragment(token json.RawMessage) (string, error) {
	if len(token) == 0 {
		return "", fmt.Errorf("partial_json is required")
	}
	var fragment string
	if err := json.Unmarshal(token, &fragment); err != nil {
		return "", fmt.Errorf("partial_json must be an IR JSON string token: %w", err)
	}
	return fragment, nil
}

// encodeStopReason maps an IR stop reason (with its conditional stop
// sequence) to the wire stop_reason/stop_sequence pair; it is the inverse of
// the non-streaming decodeStopReason (spec/01 s4).
func encodeStopReason(stop ir.StopReason, stopSequence string) (string, string, error) {
	switch stop {
	case ir.StopEndTurn:
		return "end_turn", "", nil
	case ir.StopMaxTokens:
		return "max_tokens", "", nil
	case ir.StopSequence:
		return "stop_sequence", stopSequence, nil
	case ir.StopToolUse:
		return "tool_use", "", nil
	case ir.StopRefusal:
		return "refusal", "", nil
	default:
		return "", "", fmt.Errorf("anthropic: stop reason %q has no Anthropic equivalent", stop)
	}
}
