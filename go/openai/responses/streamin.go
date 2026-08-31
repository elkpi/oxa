package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

// streamFunctionCall retains a native function_call output item until its
// item-completion event makes its complete opaque argument text available.
type streamFunctionCall struct {
	itemID        string
	outputIndex   int
	callID        string
	name          string
	fragments     []string
	argumentsDone bool
}

// StreamDecoder incrementally converts an OpenAI Responses event stream into
// IR events. Assistant message items emit output_text blocks as they arrive;
// function_call items retain opaque arguments until item completion. Unsupported
// items and parts are skipped as native lifecycle units so their follow-up events
// do not duplicate losses.
type StreamDecoder struct {
	models modelmap.Table
	losses []ir.Loss

	started    bool
	terminated bool
	flushed    bool

	nextOutputIndex  int
	nextBlockIndex   int
	itemOpen         bool
	skippedItem      bool
	itemType         string
	itemID           string
	outputIndex      int
	nextContentIndex int
	functionCall     *streamFunctionCall
	toolUseSeen      bool

	blockOpen    bool
	skippedPart  bool
	blockIndex   int
	contentIndex int
	textDone     bool
}

// NewStreamDecoder returns a Responses event-stream decoder. The variadic
// Options match the package conversion functions; WithModelMap applies to the
// response model in response.created.
func NewStreamDecoder(opts ...Option) *StreamDecoder {
	o := newOptions(opts...)
	return &StreamDecoder{models: o.models}
}

// Feed pushes one Responses streaming event and returns any completed IR
// events. response.completed, response.incomplete, and response.failed are
// terminal events: each emits the final MessageDelta and MessageDone. Callers
// must still call Flush to confirm the completed wire stream.
func (d *StreamDecoder) Feed(ev *StreamEvent) ([]ir.Event, error) {
	if ev == nil {
		return nil, fmt.Errorf("responses: nil stream event")
	}
	if d.flushed {
		return nil, fmt.Errorf("responses: event fed after stream flush")
	}
	if d.terminated {
		return nil, fmt.Errorf("responses: event fed after terminal response")
	}

	switch ev.Type {
	case "response.created":
		if d.started {
			return nil, fmt.Errorf("responses: duplicate response.created")
		}
		if ev.Response == nil {
			return nil, fmt.Errorf("responses: response.created without response")
		}
		d.started = true
		return []ir.Event{ir.MessageStart{
			ID: ev.Response.ID, Model: d.models.Map(ev.Response.Model),
		}}, nil
	case "response.output_item.added":
		if err := d.requireStarted("response.output_item.added"); err != nil {
			return nil, err
		}
		if d.itemOpen {
			return nil, fmt.Errorf("responses: response.output_item.added with an item still open")
		}
		if ev.OutputIndex != d.nextOutputIndex {
			return nil, fmt.Errorf("responses: output_item.added output_index %d, want %d", ev.OutputIndex, d.nextOutputIndex)
		}
		if ev.Item == nil {
			return nil, fmt.Errorf("responses: response.output_item.added without item")
		}
		d.nextOutputIndex++
		d.itemOpen = true
		d.itemType = ev.Item.Type
		d.outputIndex = ev.OutputIndex
		d.itemID = ev.Item.ID
		d.nextContentIndex = 0
		d.functionCall = nil
		switch {
		case ev.Item.Type == "message" && ev.Item.Role == "assistant":
			return nil, nil
		case ev.Item.Type == "function_call":
			if ev.Item.ID == "" || ev.Item.CallID == "" || ev.Item.Name == "" {
				return nil, fmt.Errorf("responses: function_call item requires id, call_id, and name")
			}
			d.functionCall = &streamFunctionCall{
				itemID:      ev.Item.ID,
				outputIndex: ev.OutputIndex,
				callID:      ev.Item.CallID,
				name:        ev.Item.Name,
				fragments:   []string{ev.Item.Arguments},
			}
			return nil, nil
		default:
			d.skippedItem = true
			d.losses = append(d.losses, d.unsupportedItemLoss(ev.OutputIndex, ev.Item.Type))
			return nil, nil
		}
	case "response.content_part.added":
		if err := d.requireActiveItem(ev, "response.content_part.added"); err != nil {
			return nil, err
		}
		if d.functionCall != nil {
			return nil, fmt.Errorf("responses: response.content_part.added on function_call item")
		}
		if d.blockOpen || d.skippedPart {
			return nil, fmt.Errorf("responses: response.content_part.added with a part still open")
		}
		if ev.ContentIndex != d.nextContentIndex {
			return nil, fmt.Errorf("responses: content_part.added content_index %d, want %d", ev.ContentIndex, d.nextContentIndex)
		}
		d.nextContentIndex++
		d.contentIndex = ev.ContentIndex
		if ev.Part == nil {
			return nil, fmt.Errorf("responses: response.content_part.added without part")
		}
		if d.skippedItem {
			d.skippedPart = true
			return nil, nil
		}
		if ev.Part.Type != "output_text" {
			d.skippedPart = true
			d.losses = append(d.losses, ir.Loss{
				Path:   fmt.Sprintf("output[%d].content[%d]", ev.OutputIndex, ev.ContentIndex),
				Field:  "type",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("Responses streaming content type %q is not decoded in the Responses stream profile", ev.Part.Type),
			})
			return nil, nil
		}
		d.blockOpen = true
		d.blockIndex = d.nextBlockIndex
		d.nextBlockIndex++
		d.textDone = false
		return []ir.Event{ir.ContentBlockStart{
			Index: d.blockIndex, Block: ir.TextBlock{Text: ev.Part.Text},
		}}, nil
	case "response.function_call_arguments.delta":
		if d.skippedItem {
			if err := d.requireActiveItem(ev, "response.function_call_arguments.delta"); err != nil {
				return nil, err
			}
			return nil, nil
		}
		if err := d.requireFunctionCall(ev, "response.function_call_arguments.delta"); err != nil {
			return nil, err
		}
		if d.functionCall.argumentsDone {
			return nil, fmt.Errorf("responses: response.function_call_arguments.delta after arguments.done")
		}
		d.functionCall.fragments = append(d.functionCall.fragments, ev.Delta)
		return nil, nil
	case "response.function_call_arguments.done":
		if d.skippedItem {
			if err := d.requireActiveItem(ev, "response.function_call_arguments.done"); err != nil {
				return nil, err
			}
			return nil, nil
		}
		if err := d.requireFunctionCall(ev, "response.function_call_arguments.done"); err != nil {
			return nil, err
		}
		if d.functionCall.argumentsDone {
			return nil, fmt.Errorf("responses: duplicate response.function_call_arguments.done")
		}
		if ev.CallID != d.functionCall.callID || ev.Name != d.functionCall.name || ev.Arguments != strings.Join(d.functionCall.fragments, "") {
			return nil, fmt.Errorf("responses: response.function_call_arguments.done does not match the active function call")
		}
		d.functionCall.argumentsDone = true
		return nil, nil
	case "response.output_text.delta":
		if err := d.requireActiveItem(ev, "response.output_text.delta"); err != nil {
			return nil, err
		}
		if d.functionCall != nil {
			return nil, fmt.Errorf("responses: response.output_text.delta on function_call item")
		}
		if d.skippedItem || d.skippedPart {
			if ev.ContentIndex != d.contentIndex {
				return nil, fmt.Errorf("responses: output_text.delta content_index %d does not match the skipped part", ev.ContentIndex)
			}
			return nil, nil
		}
		if !d.blockOpen || ev.ContentIndex != d.contentIndex {
			return nil, fmt.Errorf("responses: output_text.delta does not match the open content part")
		}
		if d.textDone {
			return nil, fmt.Errorf("responses: output_text.delta after output_text.done")
		}
		return []ir.Event{ir.ContentBlockDelta{
			Index: d.blockIndex, Delta: ir.TextDelta{Text: ev.Delta},
		}}, nil
	case "response.output_text.done":
		if err := d.requireActiveItem(ev, "response.output_text.done"); err != nil {
			return nil, err
		}
		if d.functionCall != nil {
			return nil, fmt.Errorf("responses: response.output_text.done on function_call item")
		}
		if d.skippedItem || d.skippedPart {
			if ev.ContentIndex != d.contentIndex {
				return nil, fmt.Errorf("responses: output_text.done content_index %d does not match the skipped part", ev.ContentIndex)
			}
			return nil, nil
		}
		if !d.blockOpen || ev.ContentIndex != d.contentIndex {
			return nil, fmt.Errorf("responses: output_text.done does not match the open content part")
		}
		if d.textDone {
			return nil, fmt.Errorf("responses: duplicate output_text.done")
		}
		d.textDone = true
		return nil, nil
	case "response.content_part.done":
		if err := d.requireActiveItem(ev, "response.content_part.done"); err != nil {
			return nil, err
		}
		if d.functionCall != nil {
			return nil, fmt.Errorf("responses: response.content_part.done on function_call item")
		}
		if ev.Part == nil {
			return nil, fmt.Errorf("responses: response.content_part.done without part")
		}
		if d.skippedItem || d.skippedPart {
			if ev.ContentIndex != d.contentIndex {
				return nil, fmt.Errorf("responses: content_part.done content_index %d does not match the skipped part", ev.ContentIndex)
			}
			d.skippedPart = false
			return nil, nil
		}
		if !d.blockOpen || ev.ContentIndex != d.contentIndex {
			return nil, fmt.Errorf("responses: content_part.done does not match the open content part")
		}
		if !d.textDone {
			return nil, fmt.Errorf("responses: content_part.done before output_text.done")
		}
		d.blockOpen = false
		return []ir.Event{ir.ContentBlockStop{Index: d.blockIndex}}, nil
	case "response.output_item.done":
		if err := d.requireStarted("response.output_item.done"); err != nil {
			return nil, err
		}
		if !d.itemOpen || ev.OutputIndex != d.outputIndex {
			return nil, fmt.Errorf("responses: response.output_item.done does not match the open item")
		}
		if ev.Item == nil || ev.Item.ID != d.itemID || ev.Item.Type != d.itemType {
			return nil, fmt.Errorf("responses: response.output_item.done does not match the open item")
		}
		if d.blockOpen || d.skippedPart {
			return nil, fmt.Errorf("responses: response.output_item.done with a content part still open")
		}
		var events []ir.Event
		if d.functionCall != nil {
			joined := strings.Join(d.functionCall.fragments, "")
			if ev.Item.CallID != d.functionCall.callID || ev.Item.Name != d.functionCall.name || ev.Item.Arguments != joined {
				return nil, fmt.Errorf("responses: response.output_item.done does not match the active function call")
			}
			var err error
			events, err = d.replayFunctionCall(d.functionCall)
			if err != nil {
				return nil, err
			}
			d.toolUseSeen = true
		}
		d.itemOpen = false
		d.itemType = ""
		d.skippedItem = false
		d.functionCall = nil
		return events, nil
	case "response.completed", "response.incomplete", "response.failed":
		if err := d.requireStarted(ev.Type); err != nil {
			return nil, err
		}
		if d.itemOpen || d.blockOpen || d.skippedPart {
			return nil, fmt.Errorf("responses: %s before output lifecycle completed", ev.Type)
		}
		if ev.Response == nil {
			return nil, fmt.Errorf("responses: %s without response", ev.Type)
		}
		stop, losses, err := decodeStatus(ev.Response, d.toolUseSeen)
		if err != nil {
			return nil, err
		}
		d.losses = append(d.losses, losses...)
		d.terminated = true
		var usage ir.Usage
		if ev.Response.Usage != nil {
			usage = ir.Usage{InputTokens: ev.Response.Usage.InputTokens, OutputTokens: ev.Response.Usage.OutputTokens}
		}
		return []ir.Event{ir.MessageDelta{StopReason: stop, Usage: usage}, ir.MessageDone{}}, nil
	default:
		if d.isSkippedItemDescendant(ev) || d.isSkippedPartDescendant(ev) {
			return nil, nil
		}
		if err := d.validateUnknownDescendant(ev); err != nil {
			return nil, err
		}
		d.losses = append(d.losses, ir.Loss{
			Path:   "type",
			Field:  "type",
			Reason: ir.LossUnsupportedSemantic,
			Detail: fmt.Sprintf("Responses stream event type %q is not decoded in the Responses stream profile", ev.Type),
		})
		return nil, nil
	}
}

func (d *StreamDecoder) requireStarted(eventType string) error {
	if !d.started {
		return fmt.Errorf("responses: %s before response.created", eventType)
	}
	return nil
}

func (d *StreamDecoder) requireActiveItem(ev *StreamEvent, eventType string) error {
	if err := d.requireStarted(eventType); err != nil {
		return err
	}
	if !d.itemOpen || ev.OutputIndex != d.outputIndex || ev.ItemID != d.itemID {
		return fmt.Errorf("responses: %s does not match the open output item", eventType)
	}
	return nil
}

func (d *StreamDecoder) requireFunctionCall(ev *StreamEvent, eventType string) error {
	if err := d.requireActiveItem(ev, eventType); err != nil {
		return err
	}
	if d.functionCall == nil {
		return fmt.Errorf("responses: %s without an active function_call item", eventType)
	}
	return nil
}

func (d *StreamDecoder) replayFunctionCall(call *streamFunctionCall) ([]ir.Event, error) {
	input, err := json.Marshal(strings.Join(call.fragments, ""))
	if err != nil {
		return nil, fmt.Errorf("responses: wrap function_call arguments: %w", err)
	}
	partials := make([]json.RawMessage, len(call.fragments))
	for index, fragment := range call.fragments {
		partial, err := json.Marshal(fragment)
		if err != nil {
			return nil, fmt.Errorf("responses: wrap function_call argument fragment: %w", err)
		}
		partials[index] = partial
	}
	index := d.nextBlockIndex
	d.nextBlockIndex++
	events := make([]ir.Event, 0, len(partials)+2)
	events = append(events, ir.ContentBlockStart{
		Index: index,
		Block: ir.ToolUseBlock{ID: call.callID, Name: call.name, Input: input},
	})
	for _, partial := range partials {
		events = append(events, ir.ContentBlockDelta{
			Index: index,
			Delta: ir.InputJSONDelta{PartialJSON: partial},
		})
	}
	return append(events, ir.ContentBlockStop{Index: index}), nil
}

func (d *StreamDecoder) unsupportedItemLoss(outputIndex int, itemType string) ir.Loss {
	detail := fmt.Sprintf("Responses streaming output item type %q is not decoded", itemType)
	if itemType == "function_call_output" {
		detail = "N-S-10: Responses function_call_output has no supported IR block mapping; response.output_item.done completes and is absorbed for this item-only lifecycle vector"
	}
	return ir.Loss{
		Path:   fmt.Sprintf("output[%d]", outputIndex),
		Field:  "type",
		Reason: ir.LossUnsupportedSemantic,
		Detail: detail,
	}
}

func (d *StreamDecoder) isSkippedItemDescendant(ev *StreamEvent) bool {
	return d.itemOpen && d.skippedItem && ev.OutputIndex == d.outputIndex &&
		ev.ItemID != "" && ev.ItemID == d.itemID
}

func (d *StreamDecoder) isSkippedPartDescendant(ev *StreamEvent) bool {
	return d.itemOpen && d.skippedPart && ev.OutputIndex == d.outputIndex &&
		ev.ItemID != "" && ev.ItemID == d.itemID && ev.ContentIndex == d.contentIndex
}

func (d *StreamDecoder) validateUnknownDescendant(ev *StreamEvent) error {
	if !d.itemOpen || ev.ItemID == "" {
		return nil
	}
	if ev.OutputIndex != d.outputIndex || ev.ItemID != d.itemID {
		return fmt.Errorf("responses: unknown event %s does not match the open output item", ev.Type)
	}
	if d.skippedItem {
		return nil
	}
	if d.skippedPart && ev.ContentIndex != d.contentIndex {
		return fmt.Errorf("responses: unknown event %s does not match the open content part", ev.Type)
	}
	if (strings.HasPrefix(ev.Type, "response.content_part.") || strings.HasPrefix(ev.Type, "response.output_text.")) &&
		(!d.blockOpen || ev.ContentIndex != d.contentIndex) {
		return fmt.Errorf("responses: unknown event %s does not match the open content part", ev.Type)
	}
	return nil
}

// Flush confirms the terminal Responses event. Terminal IR events are emitted
// by Feed because the terminal response carries final usage and status.
func (d *StreamDecoder) Flush() ([]ir.Event, error) {
	if d.flushed {
		return nil, fmt.Errorf("responses: stream flushed twice")
	}
	if !d.terminated {
		return nil, fmt.Errorf("responses: stream ended without a terminal response event")
	}
	d.flushed = true
	return nil, nil
}

// Losses returns the losses accumulated across the stream so far.
func (d *StreamDecoder) Losses() []ir.Loss {
	return d.losses
}
