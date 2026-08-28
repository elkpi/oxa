package responses

import (
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

// StreamDecoder incrementally converts an OpenAI Responses event stream into
// IR events. In M6 it supports assistant message items with output_text parts;
// Responses item events describe item lifecycle only, while content-part events
// alone define IR block boundaries. Unsupported items and parts are skipped as
// native lifecycle units so their follow-up events do not duplicate losses.
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
	itemID           string
	outputIndex      int
	nextContentIndex int

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
		d.outputIndex = ev.OutputIndex
		d.itemID = ev.Item.ID
		d.nextContentIndex = 0
		if ev.Item.Type != "message" || ev.Item.Role != "assistant" {
			d.skippedItem = true
			d.losses = append(d.losses, ir.Loss{
				Path:   fmt.Sprintf("output[%d]", ev.OutputIndex),
				Field:  "type",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("Responses streaming output item type %q is not decoded in M6", ev.Item.Type),
			})
			return nil, nil
		}
		return nil, nil
	case "response.content_part.added":
		if err := d.requireActiveItem(ev, "response.content_part.added"); err != nil {
			return nil, err
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
				Detail: fmt.Sprintf("Responses streaming content type %q is not decoded in M6", ev.Part.Type),
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
	case "response.output_text.delta":
		if err := d.requireActiveItem(ev, "response.output_text.delta"); err != nil {
			return nil, err
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
		if ev.Item == nil || ev.Item.ID != d.itemID {
			return nil, fmt.Errorf("responses: response.output_item.done does not match the open item id")
		}
		if d.blockOpen || d.skippedPart {
			return nil, fmt.Errorf("responses: response.output_item.done with a content part still open")
		}
		d.itemOpen = false
		d.skippedItem = false
		return nil, nil
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
		stop, losses, err := decodeStatus(ev.Response, false)
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
			Detail: fmt.Sprintf("Responses stream event type %q is not decoded in M6", ev.Type),
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

func (d *StreamDecoder) isSkippedItemDescendant(ev *StreamEvent) bool {
	return d.itemOpen && d.skippedItem && ev.OutputIndex == d.outputIndex &&
		ev.ItemID != "" && ev.ItemID == d.itemID
}

func (d *StreamDecoder) isSkippedPartDescendant(ev *StreamEvent) bool {
	return d.itemOpen && d.skippedPart && ev.OutputIndex == d.outputIndex &&
		ev.ItemID != "" && ev.ItemID == d.itemID && ev.ContentIndex == d.contentIndex
}

func (d *StreamDecoder) validateUnknownDescendant(ev *StreamEvent) error {
	if !d.itemOpen || d.skippedItem || ev.ItemID == "" {
		return nil
	}
	if ev.OutputIndex != d.outputIndex || ev.ItemID != d.itemID {
		return fmt.Errorf("responses: unknown event %s does not match the open output item", ev.Type)
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
