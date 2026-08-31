package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

const streamMessageItemID = "msg_abc123"

type streamOutputItemKind uint8

const (
	streamMessageOutputItem streamOutputItemKind = iota
	streamFunctionCallOutputItem
)

// streamOutputItem retains one native Responses output-item lifecycle. A message
// item can receive consecutive text parts; a function-call item owns exactly one
// ToolUse block and is completed at that block's stop.
type streamOutputItem struct {
	kind             streamOutputItemKind
	id               string
	outputIndex      int
	content          []OutputContent
	nextContentIndex int
	callID           string
	name             string
}

type streamEncodeBlock struct {
	index        int
	kind         streamOutputItemKind
	contentIndex int
	text         string
	toolInput    string
	fragments    []string
}

// StreamEncoder incrementally converts an IR event stream into OpenAI
// Responses streaming events. It maintains one serial native output-item
// lifecycle: consecutive text parts share a message item, while every ToolUse
// block creates and completes its own function_call item.
type StreamEncoder struct {
	models modelmap.Table

	id      string
	model   string
	started bool
	delta   bool
	done    bool

	nextBlockIndex   int
	nextOutputIndex  int
	nextMessageItem  int
	nextFunctionItem int
	activeItem       *streamOutputItem
	activeBlock      *streamEncodeBlock
	completed        []OutputItem
}

// NewStreamEncoder returns a Responses event-stream encoder. The variadic
// Options match the package conversion functions; WithModelMap applies to
// response.created and the terminal response envelope.
func NewStreamEncoder(opts ...Option) *StreamEncoder {
	o := newOptions(opts...)
	return &StreamEncoder{models: o.models}
}

// Apply pushes one IR event and emits its corresponding Responses event or
// events. The terminal response is emitted by MessageDelta, while MessageDone
// confirms the already-emitted terminal state.
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
		if !e.started || e.activeBlock != nil || e.delta {
			return nil, nil, fmt.Errorf("responses: ContentBlockStart out of grammar order")
		}
		if event.Index != e.nextBlockIndex {
			return nil, nil, fmt.Errorf("responses: ContentBlockStart index %d, want %d", event.Index, e.nextBlockIndex)
		}
		var (
			out    []*StreamEvent
			losses []ir.Loss
			err    error
		)
		switch block := event.Block.(type) {
		case ir.TextBlock:
			out, losses, err = e.startTextBlock(event.Index, block)
		case ir.ToolUseBlock:
			out, losses, err = e.startFunctionCallBlock(event.Index, block)
		default:
			return nil, nil, fmt.Errorf("responses: ContentBlockStart carries unsupported block %T", block)
		}
		if err != nil {
			return nil, nil, err
		}
		e.nextBlockIndex++
		return out, losses, nil

	case ir.ContentBlockDelta:
		if e.activeBlock == nil || event.Index != e.activeBlock.index {
			return nil, nil, fmt.Errorf("responses: ContentBlockDelta out of grammar order")
		}
		switch e.activeBlock.kind {
		case streamMessageOutputItem:
			delta, ok := event.Delta.(ir.TextDelta)
			if !ok {
				return nil, nil, fmt.Errorf("responses: TextBlock received non-text delta %T", event.Delta)
			}
			e.activeBlock.text += delta.Text
			return []*StreamEvent{{
				Type:         "response.output_text.delta",
				ItemID:       e.activeItem.id,
				OutputIndex:  e.activeItem.outputIndex,
				ContentIndex: e.activeBlock.contentIndex,
				Delta:        delta.Text,
			}}, nil, nil
		case streamFunctionCallOutputItem:
			delta, ok := event.Delta.(ir.InputJSONDelta)
			if !ok {
				return nil, nil, fmt.Errorf("responses: ToolUseBlock received non-input-json delta %T", event.Delta)
			}
			fragment, err := unwrapStreamIRString(delta.PartialJSON)
			if err != nil {
				return nil, nil, fmt.Errorf("responses: InputJSONDelta partial_json: %w", err)
			}
			e.activeBlock.fragments = append(e.activeBlock.fragments, fragment)
			return []*StreamEvent{{
				Type:        "response.function_call_arguments.delta",
				ItemID:      e.activeItem.id,
				OutputIndex: e.activeItem.outputIndex,
				Delta:       fragment,
			}}, nil, nil
		default:
			return nil, nil, fmt.Errorf("responses: unknown active block kind")
		}

	case ir.ContentBlockStop:
		if e.activeBlock == nil || event.Index != e.activeBlock.index {
			return nil, nil, fmt.Errorf("responses: ContentBlockStop out of grammar order")
		}
		switch e.activeBlock.kind {
		case streamMessageOutputItem:
			return e.stopTextBlock()
		case streamFunctionCallOutputItem:
			return e.stopFunctionCallBlock()
		default:
			return nil, nil, fmt.Errorf("responses: unknown active block kind")
		}

	case ir.MessageDelta:
		if !e.started || e.activeBlock != nil || e.delta {
			return nil, nil, fmt.Errorf("responses: MessageDelta out of grammar order")
		}
		var out []*StreamEvent
		if e.activeItem != nil {
			if e.activeItem.kind != streamMessageOutputItem {
				return nil, nil, fmt.Errorf("responses: MessageDelta with an uncompleted function_call item")
			}
			out = append(out, e.closeMessageItem())
		}
		terminal, losses, err := e.terminal(event)
		if err != nil {
			return nil, nil, err
		}
		terminal.Response.Output = make([]OutputItem, len(e.completed))
		copy(terminal.Response.Output, e.completed)
		e.delta = true
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

func (e *StreamEncoder) startTextBlock(index int, block ir.TextBlock) ([]*StreamEvent, []ir.Loss, error) {
	var out []*StreamEvent
	if e.activeItem == nil {
		item, added := e.openMessageItem()
		e.activeItem = item
		out = append(out, added)
	}
	if e.activeItem.kind != streamMessageOutputItem {
		return nil, nil, fmt.Errorf("responses: TextBlock cannot open before the active function_call item completes")
	}
	contentIndex := e.activeItem.nextContentIndex
	e.activeItem.nextContentIndex++
	part := OutputContent{Type: "output_text", Text: block.Text, Annotations: []json.RawMessage{}}
	e.activeItem.content = append(e.activeItem.content, part)
	e.activeBlock = &streamEncodeBlock{
		index: index, kind: streamMessageOutputItem, contentIndex: contentIndex, text: block.Text,
	}
	out = append(out, &StreamEvent{
		Type:         "response.content_part.added",
		ItemID:       e.activeItem.id,
		OutputIndex:  e.activeItem.outputIndex,
		ContentIndex: contentIndex,
		Part:         &part,
	})
	return out, nil, nil
}

func (e *StreamEncoder) startFunctionCallBlock(index int, block ir.ToolUseBlock) ([]*StreamEvent, []ir.Loss, error) {
	if block.ID == "" || block.Name == "" {
		return nil, nil, fmt.Errorf("responses: ToolUseBlock requires nonempty ID and name")
	}
	input, err := unwrapStreamIRString(block.Input)
	if err != nil {
		return nil, nil, fmt.Errorf("responses: ToolUseBlock input: %w", err)
	}
	var out []*StreamEvent
	if e.activeItem != nil {
		if e.activeItem.kind != streamMessageOutputItem {
			return nil, nil, fmt.Errorf("responses: ToolUseBlock cannot open before the active function_call item completes")
		}
		out = append(out, e.closeMessageItem())
	}
	item, added := e.openFunctionCallItem(block.ID, block.Name)
	e.activeItem = item
	e.activeBlock = &streamEncodeBlock{
		index: index, kind: streamFunctionCallOutputItem, toolInput: input,
	}
	return append(out, added), nil, nil
}

func (e *StreamEncoder) stopTextBlock() ([]*StreamEvent, []ir.Loss, error) {
	if e.activeItem == nil || e.activeItem.kind != streamMessageOutputItem {
		return nil, nil, fmt.Errorf("responses: text block without an active message item")
	}
	block := e.activeBlock
	e.activeItem.content[block.contentIndex].Text = block.text
	part := e.activeItem.content[block.contentIndex]
	e.activeBlock = nil
	return []*StreamEvent{
		{
			Type:         "response.output_text.done",
			ItemID:       e.activeItem.id,
			OutputIndex:  e.activeItem.outputIndex,
			ContentIndex: block.contentIndex,
			Text:         block.text,
		},
		{
			Type:         "response.content_part.done",
			ItemID:       e.activeItem.id,
			OutputIndex:  e.activeItem.outputIndex,
			ContentIndex: block.contentIndex,
			Part:         &part,
		},
	}, nil, nil
}

func (e *StreamEncoder) stopFunctionCallBlock() ([]*StreamEvent, []ir.Loss, error) {
	if e.activeItem == nil || e.activeItem.kind != streamFunctionCallOutputItem {
		return nil, nil, fmt.Errorf("responses: tool block without an active function_call item")
	}
	block := e.activeBlock
	var out []*StreamEvent
	if len(block.fragments) == 0 {
		block.fragments = append(block.fragments, block.toolInput)
		out = append(out, &StreamEvent{
			Type:        "response.function_call_arguments.delta",
			ItemID:      e.activeItem.id,
			OutputIndex: e.activeItem.outputIndex,
			Delta:       block.toolInput,
		})
	}
	arguments := strings.Join(block.fragments, "")
	if arguments != block.toolInput {
		return nil, nil, fmt.Errorf("responses: ToolUseBlock input does not equal concatenated InputJSONDelta fragments")
	}
	completed := OutputItem{
		ID: e.activeItem.id, Type: "function_call", Status: "completed",
		CallID: e.activeItem.callID, Name: e.activeItem.name, Arguments: arguments,
	}
	out = append(out,
		&StreamEvent{
			Type:        "response.function_call_arguments.done",
			ItemID:      e.activeItem.id,
			OutputIndex: e.activeItem.outputIndex,
			CallID:      e.activeItem.callID,
			Name:        e.activeItem.name,
			Arguments:   arguments,
		},
		&StreamEvent{Type: "response.output_item.done", OutputIndex: e.activeItem.outputIndex, Item: &completed},
	)
	e.completed = append(e.completed, completed)
	e.activeBlock = nil
	e.activeItem = nil
	return out, nil, nil
}

func (e *StreamEncoder) openMessageItem() (*streamOutputItem, *StreamEvent) {
	id := streamGeneratedItemID("msg", e.nextMessageItem)
	e.nextMessageItem++
	item := &streamOutputItem{
		kind: streamMessageOutputItem, id: id, outputIndex: e.nextOutputIndex,
	}
	e.nextOutputIndex++
	return item, &StreamEvent{Type: "response.output_item.added", OutputIndex: item.outputIndex, Item: &OutputItem{
		ID: item.id, Type: "message", Status: "in_progress", Role: "assistant", Content: []OutputContent{},
	}}
}

func (e *StreamEncoder) openFunctionCallItem(callID, name string) (*streamOutputItem, *StreamEvent) {
	id := streamGeneratedItemID("fc", e.nextFunctionItem)
	e.nextFunctionItem++
	item := &streamOutputItem{
		kind: streamFunctionCallOutputItem, id: id, outputIndex: e.nextOutputIndex, callID: callID, name: name,
	}
	e.nextOutputIndex++
	return item, &StreamEvent{Type: "response.output_item.added", OutputIndex: item.outputIndex, Item: &OutputItem{
		ID: item.id, Type: "function_call", Status: "in_progress", CallID: callID, Name: name, Arguments: "",
	}}
}

func (e *StreamEncoder) closeMessageItem() *StreamEvent {
	outputIndex := e.activeItem.outputIndex
	completed := OutputItem{
		ID: e.activeItem.id, Type: "message", Status: "completed", Role: "assistant",
		Content: append([]OutputContent(nil), e.activeItem.content...),
	}
	e.completed = append(e.completed, completed)
	e.activeItem = nil
	return &StreamEvent{Type: "response.output_item.done", OutputIndex: outputIndex, Item: &completed}
}

func streamGeneratedItemID(prefix string, ordinal int) string {
	return fmt.Sprintf("%s_abc%03d", prefix, 123+333*ordinal)
}

// unwrapStreamIRString validates and unwraps only the outer IR raw JSON string
// token. The returned payload remains opaque function argument text.
func unwrapStreamIRString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
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
