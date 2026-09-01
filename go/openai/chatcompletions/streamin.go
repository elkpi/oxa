package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

// streamToolCall is decoder-private aggregation state for a single native
// tool_calls[index]. Native indexes validate the CC stream only; IR indexes
// are allocated independently when retained calls are replayed at Flush.
type streamToolCall struct {
	index     int
	id        string
	name      string
	fragments []string
	skipped   bool
}

// StreamDecoder incrementally converts a Chat Completions chunk stream into IR
// events (face -> IR), enforcing the INV-5 grammar. Chunks are pushed with
// Feed; the terminal events are emitted by Flush because the wire may deliver
// a usage-only chunk after the finish_reason chunk. Function tool calls buffer
// until Flush so their complete opaque input can be present in ToolUseBlock.
type StreamDecoder struct {
	models      modelmap.Table
	losses      []ir.Loss
	started     bool
	textOpen    bool
	textIndex   int
	nextIRIndex int
	id          string
	model       string
	finishSeen  bool
	stop        ir.StopReason
	usage       *ir.Usage
	flushed     bool
	toolCalls   []*streamToolCall
}

// NewStreamDecoder returns a chunk-stream decoder. The variadic Options match
// the package conversion functions (WithModelMap applies to
// MessageStart.Model).
func NewStreamDecoder(opts ...Option) *StreamDecoder {
	o := newOptions(opts...)
	return &StreamDecoder{models: o.models}
}

// Feed pushes one wire chunk and returns any IR events it completes. Only
// choices[0] participates; additional choices are ignored (envelope). Text is
// emitted live, while function tool calls are retained until Flush so each
// ToolUseBlock carries its complete opaque input.
func (d *StreamDecoder) Feed(chunk *Chunk) ([]ir.Event, error) {
	if chunk == nil {
		return nil, fmt.Errorf("chatcompletions: nil chunk")
	}
	if d.flushed {
		return nil, fmt.Errorf("chatcompletions: chunk fed after stream flush")
	}
	if chunk.Usage != nil {
		// Usually the trailing usage-only chunk (stream_options.include_usage),
		// sometimes merged into the finish chunk. Arriving before the
		// finish_reason chunk is wire-tolerated; the last value wins.
		d.usage = &ir.Usage{
			InputTokens:  chunk.Usage.PromptTokens,
			OutputTokens: chunk.Usage.CompletionTokens,
		}
	}
	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	choice := chunk.Choices[0]
	if d.started && choice.Delta.Role != "" {
		if d.finishSeen {
			return nil, fmt.Errorf("chatcompletions: chunk stream restarted after finish_reason")
		}
		return nil, fmt.Errorf("chatcompletions: chunk stream already started")
	}

	var events []ir.Event
	if !d.started {
		d.started = true
		d.id = chunk.ID
		d.model = d.models.Map(chunk.Model)
		events = append(events, ir.MessageStart{ID: d.id, Model: d.model})
	}

	if err := d.recordToolCalls(choice.Delta.ToolCalls); err != nil {
		return nil, err
	}
	if choice.Delta.Content != nil {
		if !d.textOpen {
			d.textOpen = true
			d.textIndex = d.nextIRIndex
			d.nextIRIndex++
			events = append(events, ir.ContentBlockStart{Index: d.textIndex, Block: ir.TextBlock{Text: ""}})
		}
		events = append(events, ir.ContentBlockDelta{Index: d.textIndex, Delta: ir.TextDelta{Text: *choice.Delta.Content}})
	}
	if choice.FinishReason != nil {
		if d.finishSeen {
			return nil, fmt.Errorf("chatcompletions: duplicate finish_reason")
		}
		stop, finishLoss, err := decodeFinishReason(*choice.FinishReason)
		if err != nil {
			return nil, err
		}
		if finishLoss != nil {
			d.losses = append(d.losses, *finishLoss)
		}
		d.stop = stop
		d.finishSeen = true
		// No MessageDelta yet: a usage-only chunk may follow. Retained tool
		// calls close at Flush, after the native terminal state is known.
	}
	return events, nil
}

func (d *StreamDecoder) recordToolCalls(calls []ToolCallDelta) error {
	for _, call := range calls {
		if call.Index < 0 || call.Index > len(d.toolCalls) {
			return fmt.Errorf("chatcompletions: tool_calls index %d is not the next consecutive native index", call.Index)
		}
		if call.Index == len(d.toolCalls) {
			d.toolCalls = append(d.toolCalls, &streamToolCall{index: call.Index})
		}
		record := d.toolCalls[call.Index]
		if call.ID != nil && *call.ID != "" {
			if record.id != "" && record.id != *call.ID {
				return fmt.Errorf("chatcompletions: tool_calls[%d] has conflicting IDs %q and %q", call.Index, record.id, *call.ID)
			}
			record.id = *call.ID
		}
		if call.Type != nil && *call.Type != "function" {
			if !record.skipped {
				d.losses = append(d.losses, loss(
					fmt.Sprintf("choices[0].delta.tool_calls[%d]", call.Index), "type", ir.LossUnsupportedSemantic,
					fmt.Sprintf("Chat Completions streamed tool type %q has no IR equivalent", *call.Type),
				))
				record.skipped = true
			}
		}
		if record.skipped || call.Function == nil {
			continue
		}
		if call.Function.Name != nil {
			record.name += *call.Function.Name
		}
		if call.Function.Arguments != nil {
			record.fragments = append(record.fragments, *call.Function.Arguments)
		}
	}
	return nil
}

// Flush closes the stream and returns a stop event for any live text block,
// then complete retained tool blocks in native-index order, followed by the
// required terminal message events. A stream that never carried a finish_reason
// is a structural error, not a default.
func (d *StreamDecoder) Flush() ([]ir.Event, error) {
	if d.flushed {
		return nil, fmt.Errorf("chatcompletions: stream flushed twice")
	}
	if !d.finishSeen {
		return nil, fmt.Errorf("chatcompletions: stream ended without finish_reason")
	}
	d.flushed = true
	var events []ir.Event
	if d.textOpen {
		events = append(events, ir.ContentBlockStop{Index: d.textIndex})
		d.textOpen = false
	}
	for _, call := range d.toolCalls {
		if call.skipped {
			continue
		}
		if call.id == "" {
			return nil, fmt.Errorf("chatcompletions: tool_calls[%d] is missing final ID", call.index)
		}
		if call.name == "" {
			return nil, fmt.Errorf("chatcompletions: tool_calls[%d] is missing final function name", call.index)
		}
		input, err := json.Marshal(strings.Join(call.fragments, ""))
		if err != nil {
			return nil, fmt.Errorf("chatcompletions: wrap tool_calls[%d] input: %w", call.index, err)
		}
		index := d.nextIRIndex
		d.nextIRIndex++
		events = append(events, ir.ContentBlockStart{
			Index: index,
			Block: ir.ToolUseBlock{ID: call.id, Name: call.name, Input: input},
		})
		for _, fragment := range call.fragments {
			partial, err := json.Marshal(fragment)
			if err != nil {
				return nil, fmt.Errorf("chatcompletions: wrap tool_calls[%d] fragment: %w", call.index, err)
			}
			events = append(events, ir.ContentBlockDelta{
				Index: index,
				Delta: ir.InputJSONDelta{PartialJSON: partial},
			})
		}
		events = append(events, ir.ContentBlockStop{Index: index})
	}
	var usage ir.Usage
	if d.usage != nil {
		usage = *d.usage
	}
	// Chat Completions carries no stop sequence; the empty StopSequence is an
	// envelope default and records no loss.
	events = append(events,
		ir.MessageDelta{StopReason: d.stop, StopSequence: "", Usage: usage},
		ir.MessageDone{},
	)
	return events, nil
}

// Losses returns the losses accumulated across the stream so far.
func (d *StreamDecoder) Losses() []ir.Loss {
	return d.losses
}
