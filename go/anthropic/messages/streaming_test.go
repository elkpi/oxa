package messages

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/oxa-protocol/oxa/go/ir"
	"github.com/oxa-protocol/oxa/go/modelmap"
)

func eventMessageStart(id, model string) *StreamEvent {
	return &StreamEvent{Type: "message_start", Message: &Response{
		ID: id, Type: "message", Role: "assistant", Model: model,
		Content: []BlockWire{}, Usage: &UsageWire{},
	}}
}

func eventBlockStart(index int, blockType string) *StreamEvent {
	return &StreamEvent{Type: "content_block_start", Index: index, ContentBlock: &BlockWire{Type: blockType}}
}

func eventTextDelta(index int, text string) *StreamEvent {
	return &StreamEvent{Type: "content_block_delta", Index: index, Delta: &StreamDelta{Type: "text_delta", Text: text}}
}

func eventBlockStop(index int) *StreamEvent {
	return &StreamEvent{Type: "content_block_stop", Index: index}
}

func eventMessageDelta(stop, seq string, in, out int64) *StreamEvent {
	return &StreamEvent{
		Type:  "message_delta",
		Delta: &StreamDelta{StopReason: stop, StopSequence: seq},
		Usage: &UsageWire{InputTokens: in, OutputTokens: out},
	}
}

var wireTextStream = []*StreamEvent{
	eventMessageStart("msg_1", "claude-3"),
	eventBlockStart(0, "text"),
	eventTextDelta(0, "Hel"),
	eventTextDelta(0, "lo"),
	eventBlockStop(0),
	eventMessageDelta("end_turn", "", 3, 5),
	{Type: "message_stop"},
}

func TestStreamEventMarshalWireShape(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event *StreamEvent
		want  string
	}{
		{
			name:  "message start uses null stop reason",
			event: eventMessageStart("msg_0", "claude-3"),
			want:  `{"type":"message_start","message":{"id":"msg_0","type":"message","role":"assistant","model":"claude-3","content":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
		},
		{
			name:  "first content block retains index zero",
			event: eventBlockStart(0, "text"),
			want:  `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.event)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("JSON\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestStreamDecoderHappyPath(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, ev := range wireTextStream {
		events, err := d.Feed(ev)
		if err != nil {
			t.Fatalf("Feed(%s): %v", ev.Type, err)
		}
		got = append(got, events...)
	}
	flush, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(flush) != 0 {
		t.Fatalf("flush events = %#v", flush)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "msg_1", Model: "claude-3"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "Hel"}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "lo"}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 3, OutputTokens: 5}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
	if ls := d.Losses(); len(ls) != 0 {
		t.Fatalf("losses = %#v", ls)
	}
}

func TestStreamDecoderMultiBlock(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, ev := range []*StreamEvent{
		eventMessageStart("msg_2", "claude-3"),
		eventBlockStart(0, "text"),
		eventTextDelta(0, "a"),
		eventBlockStop(0),
		eventBlockStart(1, "text"),
		eventTextDelta(1, "b"),
		eventBlockStop(1),
		eventMessageDelta("end_turn", "", 1, 2),
		{Type: "message_stop"},
	} {
		events, err := d.Feed(ev)
		if err != nil {
			t.Fatalf("Feed(%s): %v", ev.Type, err)
		}
		got = append(got, events...)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "msg_2", Model: "claude-3"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "a"}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 1, Delta: ir.TextDelta{Text: "b"}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 1, OutputTokens: 2}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
}

func TestStreamDecoderUnknownBlockSkipped(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, ev := range []*StreamEvent{
		eventMessageStart("msg_3", "claude-3"),
		eventBlockStart(0, "redacted_thinking"),
		eventTextDelta(0, "secret"),
		eventBlockStop(0),
		eventBlockStart(1, "text"),
		eventTextDelta(1, "kept"),
		eventBlockStop(1),
		eventBlockStart(2, "text"),
		eventTextDelta(2, "also kept"),
		eventBlockStop(2),
		eventMessageDelta("end_turn", "", 1, 1),
		{Type: "message_stop"},
	} {
		events, err := d.Feed(ev)
		if err != nil {
			t.Fatalf("Feed(%s): %v", ev.Type, err)
		}
		got = append(got, events...)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "msg_3", Model: "claude-3"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "kept"}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 1, Delta: ir.TextDelta{Text: "also kept"}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 1, OutputTokens: 1}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
	ls := d.Losses()
	if len(ls) != 1 || ls[0].Field != "content_block.type" || ls[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("losses = %#v", ls)
	}
}

func TestStreamDecoderRequiresSkippedBlockStop(t *testing.T) {
	d := NewStreamDecoder()
	for _, ev := range []*StreamEvent{
		eventMessageStart("msg_open_skip", "claude-3"),
		eventBlockStart(0, "redacted_thinking"),
	} {
		if _, err := d.Feed(ev); err != nil {
			t.Fatalf("Feed(%s): %v", ev.Type, err)
		}
	}
	if _, err := d.Feed(eventBlockStart(1, "text")); err == nil {
		t.Fatal("content_block_start before skipped block stop: want error")
	}
}

func TestStreamDecoderUnknownEventsAndDeltas(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(&StreamEvent{Type: "ping"}); err != nil {
		t.Fatalf("Feed ping: %v", err)
	}
	if _, err := d.Feed(eventMessageStart("msg_4", "claude-3")); err != nil {
		t.Fatalf("Feed message_start: %v", err)
	}
	if _, err := d.Feed(eventBlockStart(0, "text")); err != nil {
		t.Fatalf("Feed block start: %v", err)
	}
	events, err := d.Feed(&StreamEvent{Type: "content_block_delta", Index: 0, Delta: &StreamDelta{Type: "input_json_delta", PartialJSON: `{"a"`}})
	if err != nil || len(events) != 0 {
		t.Fatalf("input_json_delta: events=%#v err=%v", events, err)
	}
	events, err = d.Feed(&StreamEvent{Type: "content_block_delta", Index: 0, Delta: &StreamDelta{Type: "citations_delta"}})
	if err != nil || len(events) != 0 {
		t.Fatalf("citations_delta: events=%#v err=%v", events, err)
	}
	if _, err := d.Feed(eventBlockStop(0)); err != nil {
		t.Fatalf("Feed block stop: %v", err)
	}
	if _, err := d.Feed(eventMessageDelta("end_turn", "", 0, 0)); err != nil {
		t.Fatalf("Feed message_delta: %v", err)
	}
	if _, err := d.Feed(&StreamEvent{Type: "message_stop"}); err != nil {
		t.Fatalf("Feed message_stop: %v", err)
	}
	if ls := d.Losses(); len(ls) != 3 {
		t.Fatalf("losses = %#v", ls)
	}
}

func TestStreamDecoderStopSequenceConditional(t *testing.T) {
	run := func(delta *StreamDelta) ir.MessageDelta {
		t.Helper()
		d := NewStreamDecoder()
		var got []ir.Event
		for _, ev := range []*StreamEvent{
			eventMessageStart("msg_5", "claude-3"),
			{Type: "message_delta", Delta: delta, Usage: &UsageWire{InputTokens: 1, OutputTokens: 1}},
			{Type: "message_stop"},
		} {
			events, err := d.Feed(ev)
			if err != nil {
				t.Fatalf("Feed(%s): %v", ev.Type, err)
			}
			got = append(got, events...)
		}
		out, ok := got[len(got)-2].(ir.MessageDelta)
		if !ok {
			t.Fatalf("events = %#v", got)
		}
		return out
	}
	if delta := run(&StreamDelta{StopReason: "stop_sequence", StopSequence: "END"}); delta.StopReason != ir.StopSequence || delta.StopSequence != "END" {
		t.Fatalf("stop_sequence delta = %#v", delta)
	}
	// A stray stop_sequence on any other reason is dropped (IR schema rule).
	if delta := run(&StreamDelta{StopReason: "end_turn", StopSequence: "END"}); delta.StopSequence != "" {
		t.Fatalf("end_turn delta = %#v", delta)
	}
}

func TestStreamDecoderUnknownStopReason(t *testing.T) {
	d := NewStreamDecoder()
	for _, ev := range []*StreamEvent{
		eventMessageStart("msg_7", "claude-3"),
		eventMessageDelta("pause_turn", "", 1, 1),
		{Type: "message_stop"},
	} {
		if _, err := d.Feed(ev); err != nil {
			t.Fatalf("Feed(%s): %v", ev.Type, err)
		}
	}
	// Flush is still required and clean; the loss was recorded.
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	ls := d.Losses()
	if len(ls) != 1 || ls[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses = %#v", ls)
	}
}

func TestStreamDecoderModelMap(t *testing.T) {
	d := NewStreamDecoder(WithModelMap(modelmap.Table{"claude-3": "mapped-3"}))
	events, err := d.Feed(eventMessageStart("msg_8", "claude-3"))
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if start := events[0].(ir.MessageStart); start.Model != "mapped-3" {
		t.Fatalf("model = %q", start.Model)
	}
}

func TestStreamDecoderGrammarErrors(t *testing.T) {
	// Event before message_start.
	d := NewStreamDecoder()
	if _, err := d.Feed(eventBlockStart(0, "text")); err == nil {
		t.Fatal("block start before message_start: want error")
	}
	// Event after message_stop.
	d = NewStreamDecoder()
	feedAll(t, d, wireTextStream)
	if _, err := d.Feed(eventBlockStart(0, "text")); err == nil {
		t.Fatal("event after message_stop: want error")
	}
	// Out-of-sequence index.
	d = NewStreamDecoder()
	feedAll(t, d, wireTextStream[:1])
	if _, err := d.Feed(eventBlockStart(1, "text")); err == nil {
		t.Fatal("content_block_start index 1: want error")
	}
	// Delta with no open block.
	d = NewStreamDecoder()
	feedAll(t, d, wireTextStream[:5])
	if _, err := d.Feed(eventTextDelta(0, "late")); err == nil {
		t.Fatal("delta after block stop: want error")
	}
	// Block start with a block still open.
	d = NewStreamDecoder()
	feedAll(t, d, wireTextStream[:2])
	if _, err := d.Feed(eventBlockStart(1, "text")); err == nil {
		t.Fatal("nested content_block_start: want error")
	}
	// message_stop with a block still open.
	d = NewStreamDecoder()
	feedAll(t, d, wireTextStream[:2])
	if _, err := d.Feed(&StreamEvent{Type: "message_stop"}); err == nil {
		t.Fatal("message_stop with open block: want error")
	}
	// Missing message_delta before message_stop.
	d = NewStreamDecoder()
	feedAll(t, d, wireTextStream[:5])
	if _, err := d.Feed(&StreamEvent{Type: "message_stop"}); err == nil {
		t.Fatal("message_stop without message_delta: want error")
	}
}

func TestStreamDecoderFlushSemantics(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Flush(); err == nil {
		t.Fatal("Flush without message_stop: want error")
	}
	feedAll(t, d, wireTextStream)
	events, err := d.Flush()
	if err != nil || len(events) != 0 {
		t.Fatalf("Flush: events=%#v err=%v", events, err)
	}
	if _, err := d.Flush(); err == nil {
		t.Fatal("second Flush: want error")
	}
	if _, err := d.Feed(&StreamEvent{Type: "ping"}); err == nil {
		t.Fatal("Feed after flush: want error")
	}
}

func TestStreamEncoderHappyPath(t *testing.T) {
	e := NewStreamEncoder()
	events := []ir.Event{
		ir.MessageStart{ID: "msg_9", Model: "claude-3"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "Hel"}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "lo"}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 3, OutputTokens: 5}},
		ir.MessageDone{},
	}
	var wire []*StreamEvent
	for _, ev := range events {
		ws, _, err := e.Apply(ev)
		if err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
		wire = append(wire, ws...)
	}
	if len(wire) != 7 {
		t.Fatalf("wire event count = %d", len(wire))
	}
	first := wire[0]
	if !reflect.DeepEqual(first, eventMessageStart("msg_9", "claude-3")) {
		t.Fatalf("first event\n got %#v\nwant %#v", first, eventMessageStart("msg_9", "claude-3"))
	}
	if !reflect.DeepEqual(wire[1], eventBlockStart(0, "text")) {
		t.Fatalf("block start = %#v", wire[1])
	}
	if !reflect.DeepEqual(wire[2], eventTextDelta(0, "Hel")) || !reflect.DeepEqual(wire[3], eventTextDelta(0, "lo")) {
		t.Fatalf("deltas = %#v", wire[2:4])
	}
	if !reflect.DeepEqual(wire[4], eventBlockStop(0)) {
		t.Fatalf("block stop = %#v", wire[4])
	}
	// message_delta + message_stop: near-identity, one wire event each.
	if !reflect.DeepEqual(wire[5], eventMessageDelta("end_turn", "", 3, 5)) {
		t.Fatalf("message_delta = %#v", wire[5])
	}
	if wire[6].Type != "message_stop" {
		t.Fatalf("message_stop = %#v", wire[6])
	}
}

func TestStreamEncoderStopReasonMapping(t *testing.T) {
	for stop, want := range map[ir.StopReason]string{
		ir.StopEndTurn:   "end_turn",
		ir.StopMaxTokens: "max_tokens",
		ir.StopToolUse:   "tool_use",
		ir.StopRefusal:   "refusal",
	} {
		e := NewStreamEncoder()
		if _, _, err := e.Apply(ir.MessageStart{ID: "id"}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		ws, _, err := e.Apply(ir.MessageDelta{StopReason: stop})
		if err != nil {
			t.Fatalf("Apply(%q): %v", stop, err)
		}
		if ws[0].Delta.StopReason != want {
			t.Fatalf("stop %q: wire = %q", stop, ws[0].Delta.StopReason)
		}
		if ws[0].Delta.StopSequence != "" {
			t.Fatalf("stop %q: stray stop_sequence %q", stop, ws[0].Delta.StopSequence)
		}
	}
}

func TestStreamEncoderStopSequenceConditional(t *testing.T) {
	e := NewStreamEncoder()
	if _, _, err := e.Apply(ir.MessageStart{ID: "id"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	ws, _, err := e.Apply(ir.MessageDelta{StopReason: ir.StopSequence, StopSequence: "END"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ws[0].Delta.StopReason != "stop_sequence" || ws[0].Delta.StopSequence != "END" {
		t.Fatalf("wire = %#v", ws[0].Delta)
	}
}

func TestStreamEncoderModelMap(t *testing.T) {
	e := NewStreamEncoder(WithModelMap(modelmap.Table{"claude-3": "mapped-3"}))
	ws, _, err := e.Apply(ir.MessageStart{ID: "id", Model: "claude-3"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ws[0].Message.Model != "mapped-3" {
		t.Fatalf("model = %q", ws[0].Message.Model)
	}
}

func TestStreamEncoderGrammarErrors(t *testing.T) {
	// Non-text block.
	e := NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "t", Name: "n"}}); err == nil {
		t.Fatal("non-text block: want error")
	}
	// Unexpected index.
	e = NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{}}); err == nil {
		t.Fatal("index 1: want error")
	}
	// InputJSONDelta is not encodable in M6.
	e = NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	_, _, _ = e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}})
	if _, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: []byte(`{`)}}); err == nil {
		t.Fatal("InputJSONDelta: want error")
	}
	// MessageDelta before MessageStart.
	e = NewStreamEncoder()
	if _, _, err := e.Apply(ir.MessageDelta{StopReason: ir.StopEndTurn}); err == nil {
		t.Fatal("MessageDelta before MessageStart: want error")
	}
	// MessageDone without MessageDelta.
	e = NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	if _, _, err := e.Apply(ir.MessageDone{}); err == nil {
		t.Fatal("MessageDone without MessageDelta: want error")
	}
	// Event after MessageDone.
	e = NewStreamEncoder()
	for _, ev := range []ir.Event{
		ir.MessageStart{ID: "id"},
		ir.MessageDelta{StopReason: ir.StopEndTurn},
		ir.MessageDone{},
	} {
		if _, _, err := e.Apply(ev); err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
	}
	if _, _, err := e.Apply(ir.MessageDone{}); err == nil {
		t.Fatal("event after MessageDone: want error")
	}
}

func feedAll(t *testing.T, d *StreamDecoder, events []*StreamEvent) {
	t.Helper()
	for _, ev := range events {
		if _, err := d.Feed(ev); err != nil {
			t.Fatalf("Feed(%s): %v", ev.Type, err)
		}
	}
}

// Round trip: encode IR events to wire events and decode them back.
func TestStreamingRoundTrip(t *testing.T) {
	events := []ir.Event{
		ir.MessageStart{ID: "msg_10", Model: "claude-3"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "a"}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "b"}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 1, Delta: ir.TextDelta{Text: "c"}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopMaxTokens, Usage: ir.Usage{InputTokens: 1, OutputTokens: 2}},
		ir.MessageDone{},
	}
	e := NewStreamEncoder()
	d := NewStreamDecoder()
	var got []ir.Event
	for _, ev := range events {
		ws, _, err := e.Apply(ev)
		if err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
		for _, w := range ws {
			decoded, err := d.Feed(w)
			if err != nil {
				t.Fatalf("Feed(%s): %v", w.Type, err)
			}
			got = append(got, decoded...)
		}
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !reflect.DeepEqual(got, events) {
		t.Fatalf("round trip\n got %#v\nwant %#v", got, events)
	}
	if ls := d.Losses(); len(ls) != 0 {
		t.Fatalf("losses = %#v", ls)
	}
}
