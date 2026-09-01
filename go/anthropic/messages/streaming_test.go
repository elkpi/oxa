package messages

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
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

func TestStreamEventMarshalEmptyInputJSONDelta(t *testing.T) {
	got, err := json.Marshal(eventInputJSONDelta(0, ""))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`
	if string(got) != want {
		t.Fatalf("JSON\n got %s\nwant %s", got, want)
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
	if _, err := d.Feed(&StreamEvent{Type: "content_block_delta", Index: 0, Delta: &StreamDelta{Type: "input_json_delta", PartialJSON: `{"a"`}}); err == nil {
		t.Fatal("input_json_delta on text: want error")
	}
	events, err := d.Feed(&StreamEvent{Type: "content_block_delta", Index: 0, Delta: &StreamDelta{Type: "citations_delta"}})
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
	if ls := d.Losses(); len(ls) != 2 {
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
	// Tool block without the required input token.
	e := NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "t", Name: "n"}}); err == nil {
		t.Fatal("tool block without input: want error")
	}
	// Unsupported block type.
	e = NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ImageBlock{}}); err == nil {
		t.Fatal("unsupported block: want error")
	}
	// Unexpected index.
	e = NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{}}); err == nil {
		t.Fatal("index 1: want error")
	}
	// InputJSONDelta is only encodable for tool blocks in M7.
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

func eventToolBlockStart(index int, id, name, input string) *StreamEvent {
	return &StreamEvent{Type: "content_block_start", Index: index, ContentBlock: &BlockWire{
		Type: "tool_use", ID: id, Name: name, Input: json.RawMessage(input),
	}}
}

func eventInputJSONDelta(index int, partial string) *StreamEvent {
	return &StreamEvent{Type: "content_block_delta", Index: index, Delta: &StreamDelta{
		Type: "input_json_delta", PartialJSON: partial,
	}}
}

func irStringToken(t *testing.T, value string) json.RawMessage {
	t.Helper()
	token, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(%q): %v", value, err)
	}
	return token
}

func TestStreamDecoderToolUseBuffersFragmentsUntilStop(t *testing.T) {
	d := NewStreamDecoder()
	start := eventToolBlockStart(0, "toolu_1", "weather", `{}`)
	escapedUnicode := string(rune(92)) + "u0041"
	fragments := []string{`{"city":`, "", ` "P` + escapedUnicode + `ris", "days": 1e+01}`}

	if events, err := d.Feed(eventMessageStart("msg_tool", "claude-3")); err != nil || len(events) != 1 {
		t.Fatalf("message_start: events=%#v err=%v", events, err)
	}
	if events, err := d.Feed(start); err != nil || len(events) != 0 {
		t.Fatalf("tool start: events=%#v err=%v", events, err)
	}
	for _, fragment := range fragments {
		events, err := d.Feed(eventInputJSONDelta(0, fragment))
		if err != nil || len(events) != 0 {
			t.Fatalf("input_json_delta %q: events=%#v err=%v", fragment, events, err)
		}
	}

	events, err := d.Feed(eventBlockStop(0))
	if err != nil {
		t.Fatalf("tool stop: %v", err)
	}
	want := []ir.Event{
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
			ID: "toolu_1", Name: "weather", Input: irStringToken(t, strings.Join(fragments, "")),
		}},
	}
	for _, fragment := range fragments {
		want = append(want, ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{
			PartialJSON: irStringToken(t, fragment),
		}})
	}
	want = append(want, ir.ContentBlockStop{Index: 0})
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("tool events\n got %#v\nwant %#v", events, want)
	}

	feedAll(t, d, []*StreamEvent{
		eventMessageDelta("tool_use", "", 8, 3),
		{Type: "message_stop"},
	})
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if losses := d.Losses(); len(losses) != 0 {
		t.Fatalf("losses = %#v", losses)
	}
}

func TestStreamDecoderToolUseStartInputFallback(t *testing.T) {
	d := NewStreamDecoder()
	startInput := `{ "tz": "Asia` + string(rune(92)) + `/Shanghai", "hour": 1e+01 }`
	feedAll(t, d, []*StreamEvent{
		eventMessageStart("msg_fallback", "claude-3"),
		eventToolBlockStart(0, "toolu_2", "clock", startInput),
	})
	events, err := d.Feed(eventBlockStop(0))
	if err != nil {
		t.Fatalf("tool stop: %v", err)
	}
	want := []ir.Event{
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
			ID: "toolu_2", Name: "clock", Input: irStringToken(t, startInput),
		}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{
			PartialJSON: irStringToken(t, startInput),
		}},
		ir.ContentBlockStop{Index: 0},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("fallback events\n got %#v\nwant %#v", events, want)
	}
}

func TestStreamDecoderToolUseValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start *StreamEvent
	}{
		{name: "missing id", start: &StreamEvent{Type: "content_block_start", Index: 0, ContentBlock: &BlockWire{Type: "tool_use", Name: "weather", Input: json.RawMessage(`{}`)}}},
		{name: "missing name", start: &StreamEvent{Type: "content_block_start", Index: 0, ContentBlock: &BlockWire{Type: "tool_use", ID: "toolu", Input: json.RawMessage(`{}`)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewStreamDecoder()
			feedAll(t, d, []*StreamEvent{eventMessageStart("msg", "claude-3")})
			if _, err := d.Feed(tc.start); err == nil {
				t.Fatal("tool start: want error")
			}
		})
	}

	d := NewStreamDecoder()
	feedAll(t, d, []*StreamEvent{
		eventMessageStart("msg", "claude-3"),
		eventToolBlockStart(0, "toolu", "weather", `{}`),
	})
	if _, err := d.Feed(eventInputJSONDelta(1, `{`)); err == nil {
		t.Fatal("wrong native index: want error")
	}

	d = NewStreamDecoder()
	feedAll(t, d, []*StreamEvent{
		eventMessageStart("msg", "claude-3"),
		eventBlockStart(0, "text"),
	})
	if _, err := d.Feed(eventInputJSONDelta(0, `{`)); err == nil {
		t.Fatal("input_json_delta on text: want error")
	}
	if _, err := d.Feed(eventBlockStop(0)); err != nil {
		t.Fatalf("text stop after rejected delta: %v", err)
	}

	d = NewStreamDecoder()
	feedAll(t, d, []*StreamEvent{
		eventMessageStart("msg", "claude-3"),
		eventToolBlockStart(0, "toolu", "weather", `{}`),
	})
	if _, err := d.Feed(&StreamEvent{Type: "message_stop"}); err == nil {
		t.Fatal("message_stop with open tool: want error")
	}
}

func TestStreamDecoderUnsupportedServerToolUseContained(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, ev := range []*StreamEvent{
		eventMessageStart("msg_server_tool", "claude-3"),
		{Type: "content_block_start", Index: 0, ContentBlock: &BlockWire{
			Type: "server_tool_use", ID: "server-1", Name: "web_search", Input: json.RawMessage(`{"query":"x"}`),
		}},
		eventInputJSONDelta(0, `{"ignored":`),
		eventBlockStop(0),
		eventBlockStart(1, "text"),
		eventTextDelta(1, "kept"),
		eventBlockStop(1),
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
		ir.MessageStart{ID: "msg_server_tool", Model: "claude-3"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "kept"}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 1, OutputTokens: 1}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
	losses := d.Losses()
	if len(losses) != 1 || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("losses = %#v", losses)
	}
}

func TestStreamEncoderToolUseMapsFragments(t *testing.T) {
	e := NewStreamEncoder()
	escapedUnicode := string(rune(92)) + "u0041"
	input := `{"city": "P` + escapedUnicode + `ris", "weight": 1.0}`
	fragments := []string{`{"city":`, ` "P` + escapedUnicode + `ris",`, ` "weight": 1.0}`}
	if _, _, err := e.Apply(ir.MessageStart{ID: "msg", Model: "claude-3"}); err != nil {
		t.Fatalf("MessageStart: %v", err)
	}
	ws, losses, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
		ID: "toolu", Name: "weather", Input: irStringToken(t, input),
	}})
	if err != nil || len(losses) != 0 || len(ws) != 1 {
		t.Fatalf("tool start: events=%#v losses=%#v err=%v", ws, losses, err)
	}
	if ws[0].ContentBlock.Input == nil || string(ws[0].ContentBlock.Input) != `{}` {
		t.Fatalf("tool start input = %s, want {}", ws[0].ContentBlock.Input)
	}
	for _, fragment := range fragments {
		ws, losses, err = e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{
			PartialJSON: irStringToken(t, fragment),
		}})
		if err != nil || len(losses) != 0 || len(ws) != 1 {
			t.Fatalf("fragment %q: events=%#v losses=%#v err=%v", fragment, ws, losses, err)
		}
		if ws[0].Delta.Type != "input_json_delta" || ws[0].Delta.PartialJSON != fragment {
			t.Fatalf("fragment %q: wire delta=%#v", fragment, ws[0].Delta)
		}
	}
	ws, losses, err = e.Apply(ir.ContentBlockStop{Index: 0})
	if err != nil || len(losses) != 0 || len(ws) != 1 || ws[0].Type != "content_block_stop" {
		t.Fatalf("tool stop: events=%#v losses=%#v err=%v", ws, losses, err)
	}
}

func TestStreamEncoderToolUseSynthesizesZeroDelta(t *testing.T) {
	e := NewStreamEncoder()
	input := `{ "tz": "Asia` + string(rune(92)) + `/Shanghai", "hour": 1e+01 }`
	if _, _, err := e.Apply(ir.MessageStart{ID: "msg", Model: "claude-3"}); err != nil {
		t.Fatalf("MessageStart: %v", err)
	}
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
		ID: "toolu", Name: "clock", Input: irStringToken(t, input),
	}}); err != nil {
		t.Fatalf("tool start: %v", err)
	}
	ws, losses, err := e.Apply(ir.ContentBlockStop{Index: 0})
	if err != nil || len(losses) != 0 || len(ws) != 2 {
		t.Fatalf("tool stop: events=%#v losses=%#v err=%v", ws, losses, err)
	}
	if ws[0].Type != "content_block_delta" || ws[0].Delta.Type != "input_json_delta" || ws[0].Delta.PartialJSON != input {
		t.Fatalf("synthesized delta = %#v", ws[0])
	}
	if ws[1].Type != "content_block_stop" || ws[1].Index != 0 {
		t.Fatalf("stop = %#v", ws[1])
	}
}

func TestStreamEncoderToolUseValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block ir.ToolUseBlock
	}{
		{name: "missing id", block: ir.ToolUseBlock{Name: "weather", Input: irStringToken(t, `{}`)}},
		{name: "missing name", block: ir.ToolUseBlock{ID: "toolu", Input: irStringToken(t, `{}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewStreamEncoder()
			if _, _, err := e.Apply(ir.MessageStart{ID: "msg"}); err != nil {
				t.Fatalf("MessageStart: %v", err)
			}
			if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: tc.block}); err == nil {
				t.Fatal("tool start: want error")
			}
		})
	}

	e := NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "msg"})
	_, _, _ = e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}})
	if _, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{
		PartialJSON: irStringToken(t, `{}`),
	}}); err == nil {
		t.Fatal("InputJSONDelta on text: want error")
	}

	e = NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "msg"})
	_, _, _ = e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
		ID: "toolu", Name: "weather", Input: irStringToken(t, `{"expected":1}`),
	}})
	if _, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "no"}}); err == nil {
		t.Fatal("TextDelta on tool: want error")
	}
	_, _, _ = e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{
		PartialJSON: irStringToken(t, `{"actual":1}`),
	}})
	if _, _, err := e.Apply(ir.ContentBlockStop{Index: 0}); err == nil {
		t.Fatal("mismatched tool input: want error")
	}
}

// Non-string IR tokens (json.RawMessage("null") in particular, which
// json.Unmarshal into a string would silently accept as "") are structural
// errors at the streaming boundary, for both tool inputs and partial
// fragments.
func TestStreamEncoderRejectsNonStringToolTokens(t *testing.T) {
	e := NewStreamEncoder()
	if _, _, err := e.Apply(ir.MessageStart{ID: "msg"}); err != nil {
		t.Fatalf("MessageStart: %v", err)
	}
	ws, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
		ID: "toolu", Name: "weather", Input: json.RawMessage("null"),
	}})
	if err == nil {
		t.Fatal("null tool input: want error")
	}
	if len(ws) != 0 {
		t.Fatalf("null tool input: got %d wire events", len(ws))
	}

	e = NewStreamEncoder()
	if _, _, err := e.Apply(ir.MessageStart{ID: "msg"}); err != nil {
		t.Fatalf("MessageStart: %v", err)
	}
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
		ID: "toolu", Name: "weather", Input: irStringToken(t, `{}`),
	}}); err != nil {
		t.Fatalf("valid tool start: %v", err)
	}
	ws, _, err = e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{
		PartialJSON: json.RawMessage("null"),
	}})
	if err == nil {
		t.Fatal("null partial_json: want error")
	}
	if len(ws) != 0 {
		t.Fatalf("null partial_json: got %d wire events", len(ws))
	}
}

func FuzzStreamToolUseArguments(f *testing.F) {
	for _, seed := range []string{
		"",
		`{"quote":"\\\"","slash":"\\/"}`,
		`{"unicode":"世界"}`,
		`  {"number": 1e+01}  `,
		`{"nested":{"values":[1,true,null]}}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			t.Skip()
		}
		parts := splitToolArgumentRunes(input)
		d := NewStreamDecoder()
		feedAll(t, d, []*StreamEvent{
			eventMessageStart("fuzz", "claude-3"),
			eventToolBlockStart(0, "toolu", "fuzz", `{}`),
		})
		var got []ir.Event
		for _, part := range parts {
			events, err := d.Feed(eventInputJSONDelta(0, part))
			if err != nil {
				t.Fatalf("input_json_delta %q: %v", part, err)
			}
			got = append(got, events...)
		}
		stop, err := d.Feed(eventBlockStop(0))
		if err != nil {
			t.Fatalf("tool stop: %v", err)
		}
		got = append(got, stop...)

		if len(got) != len(parts)+2 {
			t.Fatalf("events = %#v", got)
		}
		blockStart, ok := got[0].(ir.ContentBlockStart)
		if !ok {
			t.Fatalf("first event = %#v", got[0])
		}
		tool, ok := blockStart.Block.(ir.ToolUseBlock)
		if !ok {
			t.Fatalf("block = %#v", blockStart.Block)
		}
		var final string
		if err := json.Unmarshal(tool.Input, &final); err != nil {
			t.Fatalf("tool input token %s: %v", tool.Input, err)
		}
		if final != input {
			t.Fatalf("tool input = %q, want %q", final, input)
		}
		var joined strings.Builder
		for _, event := range got[1 : len(got)-1] {
			delta, ok := event.(ir.ContentBlockDelta)
			if !ok {
				t.Fatalf("delta event = %#v", event)
			}
			fragment, ok := delta.Delta.(ir.InputJSONDelta)
			if !ok {
				t.Fatalf("delta = %#v", delta.Delta)
			}
			var text string
			if err := json.Unmarshal(fragment.PartialJSON, &text); err != nil {
				t.Fatalf("fragment token %s: %v", fragment.PartialJSON, err)
			}
			joined.WriteString(text)
		}
		if joined.String() != input {
			t.Fatalf("joined fragments = %q, want %q", joined.String(), input)
		}
	})
}

func splitToolArgumentRunes(input string) []string {
	parts := make([]string, 0, len(input))
	for _, r := range input {
		parts = append(parts, string(r))
	}
	if len(parts) == 0 {
		return []string{""}
	}
	return parts
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
