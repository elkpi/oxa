package chatcompletions

import (
	"fmt"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

// StreamDecoder incrementally converts a Chat Completions chunk stream into IR
// events (face -> IR), enforcing the INV-5 grammar. Chunks are pushed with
// Feed; the terminal events are emitted by Flush because the wire may deliver
// a usage-only chunk after the finish_reason chunk. Envelope fields of chunks
// (object, created, choices[].index, repeated role) are exempt from losses, as
// in the non-streaming decode; chunk logprobs are likewise ignored silently.
type StreamDecoder struct {
	models     modelmap.Table
	losses     []ir.Loss
	started    bool
	blockOpen  bool
	id         string
	model      string
	finishSeen bool
	stop       ir.StopReason
	usage      *ir.Usage
	flushed    bool
}

// NewStreamDecoder returns a chunk-stream decoder. The variadic Options match
// the package conversion functions (WithModelMap applies to
// MessageStart.Model).
func NewStreamDecoder(opts ...Option) *StreamDecoder {
	o := newOptions(opts...)
	return &StreamDecoder{models: o.models}
}

// Feed pushes one wire chunk and returns any IR events it completes. Only
// choices[0] participates; additional choices are ignored (envelope). A
// tool_calls delta is not decodable in M6 and records one
// unsupported-semantic loss without disturbing the decoder state. A
// role-annotated delta after the stream has started is a single-stream
// violation; content deltas arriving after the finish_reason chunk are
// tolerated (the wire may deliver further chunks before the usage-only one),
// though they no longer affect the IR text emitted at Flush.
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
	if len(choice.Delta.ToolCalls) > 0 {
		// Provisional id: reserved tool-delta loss (formalized as a spec/20
		// N-CC id in M7 when streaming tool calls land).
		d.losses = append(d.losses, loss(
			"choices[0].delta.tool_calls", "tool_calls", ir.LossUnsupportedSemantic,
			"streaming tool calls arrive in milestone M7",
		))
		return nil, nil
	}
	if d.started && choice.Delta.Role != "" {
		if d.finishSeen {
			return nil, fmt.Errorf("chatcompletions: chunk stream restarted after finish_reason")
		}
		return nil, fmt.Errorf("chatcompletions: chunk stream already started")
	}
	var events []ir.Event
	if !d.started {
		d.started = true
		d.blockOpen = true
		d.id = chunk.ID
		d.model = d.models.Map(chunk.Model)
		events = append(events,
			ir.MessageStart{ID: d.id, Model: d.model},
			ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		)
	}
	if choice.Delta.Content != nil {
		events = append(events, ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: *choice.Delta.Content}})
	}
	if choice.FinishReason != nil {
		stop, finishLoss, err := decodeFinishReason(*choice.FinishReason)
		if err != nil {
			return nil, err
		}
		if finishLoss != nil {
			d.losses = append(d.losses, *finishLoss)
		}
		d.stop = stop
		d.finishSeen = true
		// No MessageDelta yet: a usage-only chunk may follow (INV-5 close
		// happens in Flush).
	}
	return events, nil
}

// Flush closes the stream and returns the terminal events
// (ContentBlockStop if still open, MessageDelta, MessageDone). A stream that
// never carried a finish_reason is a structural error, not a default.
func (d *StreamDecoder) Flush() ([]ir.Event, error) {
	if d.flushed {
		return nil, fmt.Errorf("chatcompletions: stream flushed twice")
	}
	if !d.finishSeen {
		return nil, fmt.Errorf("chatcompletions: stream ended without finish_reason")
	}
	d.flushed = true
	var events []ir.Event
	if d.blockOpen {
		events = append(events, ir.ContentBlockStop{Index: 0})
		d.blockOpen = false
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
