package chatcompletions

import (
	"fmt"

	"github.com/oxa-protocol/oxa/go/ir"
	"github.com/oxa-protocol/oxa/go/modelmap"
)

// StreamEncoder incrementally converts an IR event stream into Chat
// Completions chunks (IR -> face), enforcing the INV-5 grammar. Envelope
// fields absent from the IR render with the documented defaults (object
// "chat.completion.chunk", created 0, single choice index 0) and record no
// loss.
type StreamEncoder struct {
	models    modelmap.Table
	id        string
	model     string
	started   bool
	blockOpen bool
	finished  bool // MessageDelta applied
	done      bool // MessageDone applied
}

// NewStreamEncoder returns an event-stream encoder. The variadic Options match
// the package conversion functions (WithModelMap applies to the chunk Model).
func NewStreamEncoder(opts ...Option) *StreamEncoder {
	o := newOptions(opts...)
	return &StreamEncoder{models: o.models}
}

// Apply pushes one IR event and returns the chunks it produces (possibly none:
// content-block lifecycle events are absorbed into the chunk deltas).
// Out-of-grammar orderings are structural errors.
func (e *StreamEncoder) Apply(ev ir.Event) ([]*Chunk, []ir.Loss, error) {
	if ev == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil event")
	}
	if e.done || (e.finished && !isMessageDone(ev)) {
		return nil, nil, fmt.Errorf("chatcompletions: event applied after stream termination (%T)", ev)
	}
	switch event := ev.(type) {
	case ir.MessageStart:
		if e.started {
			return nil, nil, fmt.Errorf("chatcompletions: duplicate MessageStart")
		}
		e.started = true
		e.id = event.ID
		e.model = e.models.Map(event.Model)
		return []*Chunk{{
			ID:      e.id,
			Object:  "chat.completion.chunk",
			Created: 0,
			Model:   e.model,
			Choices: []ChoiceDelta{{Index: 0, Delta: DeltaPayload{Role: "assistant"}}},
		}}, nil, nil
	case ir.ContentBlockStart:
		if !e.started || e.blockOpen {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockStart out of grammar order")
		}
		if _, ok := event.Block.(ir.TextBlock); !ok {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockStart carries a non-text block (unreachable in M6)")
		}
		if event.Index != 0 {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockStart index %d, want 0", event.Index)
		}
		e.blockOpen = true
		return nil, nil, nil
	case ir.ContentBlockDelta:
		if !e.blockOpen || event.Index != 0 {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockDelta out of grammar order")
		}
		delta, ok := event.Delta.(ir.TextDelta)
		if !ok {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockDelta carries a non-text delta (unreachable in M6)")
		}
		text := delta.Text
		return []*Chunk{{
			ID:      e.id,
			Object:  "chat.completion.chunk",
			Created: 0,
			Model:   e.model,
			Choices: []ChoiceDelta{{Index: 0, Delta: DeltaPayload{Content: &text}}},
		}}, nil, nil
	case ir.ContentBlockStop:
		if !e.blockOpen || event.Index != 0 {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockStop out of grammar order")
		}
		e.blockOpen = false
		return nil, nil, nil
	case ir.MessageDelta:
		if !e.started || e.blockOpen {
			return nil, nil, fmt.Errorf("chatcompletions: MessageDelta out of grammar order")
		}
		finish, finishLoss, err := encodeFinishReason(event.StopReason)
		if err != nil {
			return nil, nil, err
		}
		var losses []ir.Loss
		if finishLoss != nil {
			losses = append(losses, *finishLoss)
		}
		e.finished = true
		chunk := &Chunk{
			ID:      e.id,
			Object:  "chat.completion.chunk",
			Created: 0,
			Model:   e.model,
			Choices: []ChoiceDelta{{Index: 0, Delta: DeltaPayload{}, FinishReason: &finish}},
			Usage: &UsageWire{
				PromptTokens:     event.Usage.InputTokens,
				CompletionTokens: event.Usage.OutputTokens,
				TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
			},
		}
		return []*Chunk{chunk}, losses, nil
	case ir.MessageDone:
		if !e.finished {
			return nil, nil, fmt.Errorf("chatcompletions: MessageDone out of grammar order")
		}
		e.done = true
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("chatcompletions: unknown event %T", ev)
	}
}

func isMessageDone(ev ir.Event) bool {
	_, ok := ev.(ir.MessageDone)
	return ok
}

// encodeFinishReason maps an IR stop reason to a finish_reason value; it is
// the inverse of the non-streaming response encode (spec/01 s4.1).
func encodeFinishReason(stop ir.StopReason) (string, *ir.Loss, error) {
	switch stop {
	case ir.StopEndTurn:
		return "stop", nil, nil
	case ir.StopMaxTokens:
		return "length", nil, nil
	case ir.StopRefusal:
		return "content_filter", nil, nil
	case ir.StopToolUse:
		return "tool_calls", nil, nil
	case ir.StopSequence:
		return "stop", &ir.Loss{
			Field:  "stop_sequence",
			Reason: ir.LossUnmappedValue,
			Detail: "Chat Completions finish_reason \"stop\" does not identify the matched stop sequence",
		}, nil
	default:
		return "", nil, fmt.Errorf("chatcompletions: stop reason %q has no Chat Completions equivalent", stop)
	}
}
