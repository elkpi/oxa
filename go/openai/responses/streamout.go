package responses

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

const streamMessageItemID = "msg_abc123"

// StreamEncoder incrementally converts an IR event stream into OpenAI
// Responses streaming events. The Responses wire separates output-item and
// content-part lifecycles, so a ContentBlockStart opens the synthesized
// assistant item on its first use and then opens one content part per IR block.
type StreamEncoder struct {
	models modelmap.Table

	id      string
	model   string
	started bool
	delta   bool
	done    bool

	nextBlockIndex int
	blockOpen      bool
	blockIndex     int
	blockText      string

	itemOpened bool
	itemParts  []OutputContent
}

// NewStreamEncoder returns a Responses event-stream encoder. The variadic
// Options match the package conversion functions; WithModelMap applies to
// response.created and the terminal response envelope.
func NewStreamEncoder(opts ...Option) *StreamEncoder {
	o := newOptions(opts...)
	return &StreamEncoder{models: o.models}
}

// Apply pushes one IR event and emits its corresponding Responses event or
// events. ContentBlockStart emits the item-add event only for the first text
// block; all following blocks share the same assistant output item. The
// terminal response is emitted by MessageDelta, while MessageDone confirms the
// already-emitted terminal state.
func (e *StreamEncoder) Apply(ev ir.Event) ([]*StreamEvent, []ir.Loss, error) {
	if ev == nil {
		return nil, nil, fmt.Errorf("responses: nil event")
	}
	if e.done || (e.delta && !isMessageDone(ev)) {
		return nil, nil, fmt.Errorf("responses: event applied after stream termination (%T)", ev)
	}

	switch event := ev.(type) {
	case ir.MessageStart:
		if e.started {
			return nil, nil, fmt.Errorf("responses: duplicate MessageStart")
		}
		e.started = true
		e.id = event.ID
		e.model = e.models.Map(event.Model)
		return []*StreamEvent{{Type: "response.created", Response: &Response{
			ID: e.id, Object: "response", Status: "in_progress", Model: e.model, Output: []OutputItem{},
		}}}, nil, nil
	case ir.ContentBlockStart:
		if !e.started || e.blockOpen || e.delta {
			return nil, nil, fmt.Errorf("responses: ContentBlockStart out of grammar order")
		}
		block, ok := event.Block.(ir.TextBlock)
		if !ok {
			return nil, nil, fmt.Errorf("responses: ContentBlockStart carries a non-text block (unreachable in M6)")
		}
		if event.Index != e.nextBlockIndex {
			return nil, nil, fmt.Errorf("responses: ContentBlockStart index %d, want %d", event.Index, e.nextBlockIndex)
		}
		e.nextBlockIndex++
		e.blockOpen = true
		e.blockIndex = event.Index
		e.blockText = block.Text
		part := OutputContent{Type: "output_text", Text: block.Text, Annotations: []json.RawMessage{}}
		e.itemParts = append(e.itemParts, part)
		partEvent := &StreamEvent{
			Type: "response.content_part.added", ItemID: streamMessageItemID,
			OutputIndex: 0, ContentIndex: event.Index, Part: &part,
		}
		if e.itemOpened {
			return []*StreamEvent{partEvent}, nil, nil
		}
		e.itemOpened = true
		return []*StreamEvent{
			{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{
				ID: streamMessageItemID, Type: "message", Status: "in_progress", Role: "assistant", Content: []OutputContent{},
			}},
			partEvent,
		}, nil, nil
	case ir.ContentBlockDelta:
		if !e.blockOpen || event.Index != e.blockIndex {
			return nil, nil, fmt.Errorf("responses: ContentBlockDelta out of grammar order")
		}
		delta, ok := event.Delta.(ir.TextDelta)
		if !ok {
			return nil, nil, fmt.Errorf("responses: ContentBlockDelta carries a non-text delta (input_json_delta is encoded in M7)")
		}
		e.blockText += delta.Text
		return []*StreamEvent{{
			Type: "response.output_text.delta", ItemID: streamMessageItemID,
			OutputIndex: 0, ContentIndex: event.Index, Delta: delta.Text,
		}}, nil, nil
	case ir.ContentBlockStop:
		if !e.blockOpen || event.Index != e.blockIndex {
			return nil, nil, fmt.Errorf("responses: ContentBlockStop out of grammar order")
		}
		e.blockOpen = false
		e.itemParts[len(e.itemParts)-1].Text = e.blockText
		part := e.itemParts[len(e.itemParts)-1]
		return []*StreamEvent{
			{
				Type: "response.output_text.done", ItemID: streamMessageItemID,
				OutputIndex: 0, ContentIndex: event.Index, Text: e.blockText,
			},
			{
				Type: "response.content_part.done", ItemID: streamMessageItemID,
				OutputIndex: 0, ContentIndex: event.Index, Part: &part,
			},
		}, nil, nil
	case ir.MessageDelta:
		if !e.started || e.blockOpen || e.delta {
			return nil, nil, fmt.Errorf("responses: MessageDelta out of grammar order")
		}
		terminal, losses, err := e.terminal(event)
		if err != nil {
			return nil, nil, err
		}
		e.delta = true
		var out []*StreamEvent
		if e.itemOpened {
			out = append(out, &StreamEvent{Type: "response.output_item.done", OutputIndex: 0, Item: e.completedItem()})
			terminal.Response.Output = []OutputItem{*e.completedItem()}
		}
		out = append(out, terminal)
		return out, losses, nil
	case ir.MessageDone:
		if !e.delta {
			return nil, nil, fmt.Errorf("responses: MessageDone out of grammar order")
		}
		e.done = true
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("responses: unknown event %T", ev)
	}
}

func (e *StreamEncoder) completedItem() *OutputItem {
	content := make([]OutputContent, len(e.itemParts))
	copy(content, e.itemParts)
	return &OutputItem{
		ID: streamMessageItemID, Type: "message", Status: "completed", Role: "assistant", Content: content,
	}
}

func (e *StreamEncoder) terminal(delta ir.MessageDelta) (*StreamEvent, []ir.Loss, error) {
	response := &Response{
		ID: e.id, Object: "response", Model: e.model, Output: []OutputItem{},
		Usage: &UsageWire{
			InputTokens: delta.Usage.InputTokens, OutputTokens: delta.Usage.OutputTokens,
			TotalTokens: delta.Usage.InputTokens + delta.Usage.OutputTokens,
		},
	}
	switch delta.StopReason {
	case ir.StopEndTurn, ir.StopToolUse:
		response.Status = "completed"
		return &StreamEvent{Type: "response.completed", Response: response}, nil, nil
	case ir.StopMaxTokens:
		response.Status = "incomplete"
		response.IncompleteDetails = &IncompleteWire{Reason: "max_output_tokens"}
		return &StreamEvent{Type: "response.incomplete", Response: response}, nil, nil
	case ir.StopRefusal:
		response.Status = "failed"
		response.Error = &ErrorWire{Code: "refusal"}
		return &StreamEvent{Type: "response.failed", Response: response}, nil, nil
	case ir.StopSequence:
		response.Status = "completed"
		return &StreamEvent{Type: "response.completed", Response: response}, []ir.Loss{{
			Field:  "stop_sequence",
			Reason: ir.LossUnmappedValue,
			Detail: "Responses status carries no stop-sequence identity; the matched IR stop sequence is lost",
		}}, nil
	default:
		return nil, nil, fmt.Errorf("responses: stop reason %q has no Responses equivalent", delta.StopReason)
	}
}

func isMessageDone(ev ir.Event) bool {
	_, ok := ev.(ir.MessageDone)
	return ok
}
