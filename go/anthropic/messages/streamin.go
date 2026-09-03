package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

// StreamDecoder incrementally converts an Anthropic Messages event stream
// into IR events (face -> IR), enforcing the INV-5 grammar. Events are pushed
// with Feed; unlike Chat Completions, the terminal events are emitted by the
// message_stop event itself (the wire's own terminator), and the required
// Flush afterwards only confirms the stream end. Unknown event types and
// unknown delta types record one unsupported-semantic loss each without
// disturbing decoder state. Unsupported content block types additionally mark
// their native index as skipped: later deltas and the stop for that index are
// absorbed without events or further losses, while later emitted IR blocks
// retain contiguous indexes.
type StreamDecoder struct {
	models      modelmap.Table
	losses      []ir.Loss
	started     bool
	nextIndex   int // expected native index of the next content_block_start
	nextIRIndex int // index allocated to the next emitted IR content block
	openIndex   int // native index, valid while blockOpen
	openIRIndex int // emitted IR index, valid while blockOpen
	blockOpen   bool
	openTool    bool
	toolID      string
	toolName    string
	toolInput   json.RawMessage
	toolParts   []string
	skippedOpen bool
	skipped     map[int]bool // indexes absorbed as unknown block types
	deltaSeen   bool
	stop        ir.StopReason
	stopSeq     string
	usage       ir.Usage
	stopped     bool // message_stop fed
	flushed     bool
}

// NewStreamDecoder returns an event-stream decoder. The variadic Options match
// the package conversion functions (WithModelMap applies to
// MessageStart.Model).
func NewStreamDecoder(opts ...Option) *StreamDecoder {
	o := newOptions(opts...)
	return &StreamDecoder{models: o.models, skipped: map[int]bool{}}
}

// Feed pushes one wire event and returns any IR events it completes.
// message_stop is the terminal wire event: feeding it emits the buffered
// MessageDelta (from the message_delta event) followed by MessageDone.
// Events before message_start or after message_stop are structural errors.
func (d *StreamDecoder) Feed(ev *StreamEvent) ([]ir.Event, error) {
	if ev == nil {
		return nil, fmt.Errorf("anthropic: nil stream event")
	}
	if d.flushed {
		return nil, fmt.Errorf("anthropic: event fed after stream flush")
	}
	if d.stopped {
		return nil, fmt.Errorf("anthropic: event fed after message_stop")
	}
	switch ev.Type {
	case EventTypeMessageStart:
		if d.started {
			return nil, fmt.Errorf("anthropic: duplicate message_start")
		}
		if ev.Message == nil {
			return nil, fmt.Errorf("anthropic: message_start without message")
		}
		d.started = true
		return []ir.Event{ir.MessageStart{
			ID:    ev.Message.ID,
			Model: d.models.Map(ev.Message.Model),
		}}, nil
	case EventTypeContentBlockStart:
		if !d.started {
			return nil, fmt.Errorf("anthropic: content_block_start before message_start")
		}
		if d.blockOpen || d.skippedOpen {
			return nil, fmt.Errorf("anthropic: content_block_start with a block still open")
		}
		if ev.Index != d.nextIndex {
			return nil, fmt.Errorf("anthropic: content_block_start index %d, want %d", ev.Index, d.nextIndex)
		}
		if ev.ContentBlock == nil {
			d.nextIndex++
			d.skipped[ev.Index] = true
			d.skippedOpen = true
			d.losses = append(d.losses, ir.Loss{
				Path:   fmt.Sprintf("content_block_start[%d].content_block.type", ev.Index),
				Field:  "content_block.type",
				Reason: ir.LossUnsupportedSemantic,
				Detail: "Anthropic streaming block has no content_block payload; the index is skipped",
			})
			return nil, nil
		}
		if ev.ContentBlock.Type == BlockTypeToolUse {
			if ev.ContentBlock.ID == "" {
				return nil, fmt.Errorf("anthropic: content_block_start[%d].content_block.id is required", ev.Index)
			}
			if ev.ContentBlock.Name == "" {
				return nil, fmt.Errorf("anthropic: content_block_start[%d].content_block.name is required", ev.Index)
			}
			d.nextIndex++
			d.blockOpen = true
			d.openTool = true
			d.openIndex = ev.Index
			d.openIRIndex = d.nextIRIndex
			d.nextIRIndex++
			d.toolID = ev.ContentBlock.ID
			d.toolName = ev.ContentBlock.Name
			d.toolInput = append(d.toolInput[:0], ev.ContentBlock.Input...)
			d.toolParts = nil
			return nil, nil
		}
		if ev.ContentBlock.Type != BlockTypeText {
			d.nextIndex++
			d.skipped[ev.Index] = true
			d.skippedOpen = true
			d.losses = append(d.losses, ir.Loss{
				Path:   fmt.Sprintf("content_block_start[%d].content_block.type", ev.Index),
				Field:  "content_block.type",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("Anthropic streaming block type %q is not decodable in M7; the index is skipped", ev.ContentBlock.Type),
			})
			return nil, nil
		}
		d.nextIndex++
		d.blockOpen = true
		d.openTool = false
		d.openIndex = ev.Index
		d.openIRIndex = d.nextIRIndex
		d.nextIRIndex++
		return []ir.Event{ir.ContentBlockStart{Index: d.openIRIndex, Block: ir.TextBlock{Text: ev.ContentBlock.Text}}}, nil
	case EventTypeContentBlockDelta:
		if !d.started {
			return nil, fmt.Errorf("anthropic: content_block_delta before message_start")
		}
		if ev.Delta == nil {
			return nil, fmt.Errorf("anthropic: content_block_delta without delta")
		}
		if d.skipped[ev.Index] {
			// The block at this index was an unknown type; its deltas are
			// absorbed silently (the one loss was recorded at block start).
			return nil, nil
		}
		if !d.blockOpen || ev.Index != d.openIndex {
			return nil, fmt.Errorf("anthropic: content_block_delta index %d does not match the open block", ev.Index)
		}
		if d.openTool {
			switch ev.Delta.Type {
			case DeltaTypeTextDelta:
				return nil, fmt.Errorf("anthropic: text_delta on tool_use block")
			case DeltaTypeInputJSONDelta:
				d.toolParts = append(d.toolParts, ev.Delta.PartialJSON)
				return nil, nil
			}
		}
		switch ev.Delta.Type {
		case DeltaTypeTextDelta:
			return []ir.Event{ir.ContentBlockDelta{Index: d.openIRIndex, Delta: ir.TextDelta{Text: ev.Delta.Text}}}, nil
		case DeltaTypeInputJSONDelta:
			return nil, fmt.Errorf("anthropic: input_json_delta on non-tool block")
		default:
			d.losses = append(d.losses, ir.Loss{
				Path:   fmt.Sprintf("content_block_delta[%d].delta.type", ev.Index),
				Field:  "delta.type",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("Anthropic delta type %q has no IR equivalent", ev.Delta.Type),
			})
			return nil, nil
		}
	case EventTypeContentBlockStop:
		if !d.started {
			return nil, fmt.Errorf("anthropic: content_block_stop before message_start")
		}
		if d.skipped[ev.Index] {
			delete(d.skipped, ev.Index)
			d.skippedOpen = false
			return nil, nil
		}
		if !d.blockOpen || ev.Index != d.openIndex {
			return nil, fmt.Errorf("anthropic: content_block_stop index %d does not match the open block", ev.Index)
		}
		if d.openTool {
			var input json.RawMessage
			var deltas []ir.Event
			if len(d.toolParts) > 0 {
				joined, err := json.Marshal(strings.Join(d.toolParts, ""))
				if err != nil {
					return nil, fmt.Errorf("anthropic: tool_use input: %w", err)
				}
				input = joined
				for _, part := range d.toolParts {
					partial, err := json.Marshal(part)
					if err != nil {
						return nil, fmt.Errorf("anthropic: tool_use input fragment: %w", err)
					}
					deltas = append(deltas, ir.ContentBlockDelta{
						Index: d.openIRIndex,
						Delta: ir.InputJSONDelta{PartialJSON: partial},
					})
				}
			} else {
				var err error
				input, err = inputToIRString(d.toolInput)
				if err != nil {
					return nil, fmt.Errorf("anthropic: tool_use input: %w", err)
				}
				deltas = append(deltas, ir.ContentBlockDelta{
					Index: d.openIRIndex,
					Delta: ir.InputJSONDelta{PartialJSON: input},
				})
			}
			events := []ir.Event{
				ir.ContentBlockStart{Index: d.openIRIndex, Block: ir.ToolUseBlock{
					ID: d.toolID, Name: d.toolName, Input: input,
				}},
			}
			events = append(events, deltas...)
			events = append(events, ir.ContentBlockStop{Index: d.openIRIndex})
			d.blockOpen = false
			d.openTool = false
			d.toolID = ""
			d.toolName = ""
			d.toolInput = nil
			d.toolParts = nil
			return events, nil
		}
		d.blockOpen = false
		return []ir.Event{ir.ContentBlockStop{Index: d.openIRIndex}}, nil
	case EventTypeMessageDelta:
		if !d.started {
			return nil, fmt.Errorf("anthropic: message_delta before message_start")
		}
		if d.blockOpen || d.skippedOpen {
			return nil, fmt.Errorf("anthropic: message_delta with a block still open")
		}
		if ev.Delta == nil {
			return nil, fmt.Errorf("anthropic: message_delta without delta")
		}
		stop, loss, err := decodeStopReason(ev.Delta.StopReason, ev.Delta.StopSequence)
		if err != nil {
			return nil, err
		}
		if loss != nil {
			d.losses = append(d.losses, *loss)
		}
		d.stop = stop
		d.stopSeq = ev.Delta.StopSequence
		if ev.Usage != nil {
			d.usage = ir.Usage{InputTokens: ev.Usage.InputTokens, OutputTokens: ev.Usage.OutputTokens}
		}
		d.deltaSeen = true
		return nil, nil
	case EventTypeMessageStop:
		if !d.started {
			return nil, fmt.Errorf("anthropic: message_stop before message_start")
		}
		if d.blockOpen || d.skippedOpen {
			return nil, fmt.Errorf("anthropic: message_stop with a block still open")
		}
		if !d.deltaSeen {
			return nil, fmt.Errorf("anthropic: message_stop without a preceding message_delta")
		}
		d.stopped = true
		delta := ir.MessageDelta{StopReason: d.stop, Usage: d.usage}
		if d.stop == ir.StopSequence {
			// IR schema rule: StopSequence only when the reason is
			// stop_sequence (same conditional as Response.StopSequence).
			delta.StopSequence = d.stopSeq
		}
		return []ir.Event{delta, ir.MessageDone{}}, nil
	default:
		// Unknown event type (ping, error, M7+ additions): one reserved loss,
		// no event, state intact.
		d.losses = append(d.losses, ir.Loss{
			Path:   "type",
			Field:  "type",
			Reason: ir.LossUnsupportedSemantic,
			Detail: fmt.Sprintf("Anthropic stream event type %q is not decoded in this milestone", ev.Type),
		})
		return nil, nil
	}
}

// Flush closes the stream. Because message_stop already emitted the terminal
// events, a complete stream flushes with no events; a stream that ended
// without message_stop is a structural error, and a second Flush errors.
func (d *StreamDecoder) Flush() ([]ir.Event, error) {
	if d.flushed {
		return nil, fmt.Errorf("anthropic: stream flushed twice")
	}
	if !d.stopped {
		return nil, fmt.Errorf("anthropic: stream ended without message_stop")
	}
	d.flushed = true
	return nil, nil
}

// Losses returns the losses accumulated across the stream so far.
func (d *StreamDecoder) Losses() []ir.Loss {
	return d.losses
}
