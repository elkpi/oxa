package messages

import (
	"fmt"

	"github.com/oxa-protocol/oxa/go/ir"
	"github.com/oxa-protocol/oxa/go/modelmap"
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

// Apply pushes one IR event and returns the wire events it produces (exactly
// one per IR event; the mapping is near-identity). Out-of-grammar orderings
// are structural errors.
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
		block, ok := event.Block.(ir.TextBlock)
		if !ok {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockStart carries a non-text block (unreachable in M6)")
		}
		if event.Index != e.nextIndex {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockStart index %d, want %d", event.Index, e.nextIndex)
		}
		e.nextIndex++
		e.blockOpen = true
		e.openIndex = event.Index
		return []*StreamEvent{{Type: "content_block_start", Index: event.Index, ContentBlock: &BlockWire{Type: "text", Text: block.Text}}}, nil, nil
	case ir.ContentBlockDelta:
		if !e.blockOpen || event.Index != e.openIndex {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockDelta out of grammar order")
		}
		delta, ok := event.Delta.(ir.TextDelta)
		if !ok {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockDelta carries a non-text delta (input_json_delta is encoded in M7)")
		}
		return []*StreamEvent{{Type: "content_block_delta", Index: event.Index, Delta: &StreamDelta{Type: "text_delta", Text: delta.Text}}}, nil, nil
	case ir.ContentBlockStop:
		if !e.blockOpen || event.Index != e.openIndex {
			return nil, nil, fmt.Errorf("anthropic: ContentBlockStop out of grammar order")
		}
		e.blockOpen = false
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
