package chatcompletions

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

func chunkRole(id, model string) *Chunk {
	return &Chunk{ID: id, Object: "chat.completion.chunk", Model: model, Choices: []ChoiceDelta{{Delta: DeltaPayload{Role: "assistant"}}}}
}

func chunkContent(s string) *Chunk {
	return &Chunk{Object: "chat.completion.chunk", Choices: []ChoiceDelta{{Delta: DeltaPayload{Content: &s}}}}
}

func chunkFinish(reason string) *Chunk {
	return &Chunk{Object: "chat.completion.chunk", Choices: []ChoiceDelta{{Delta: DeltaPayload{}, FinishReason: &reason}}}
}

func chunkUsage() *Chunk {
	return &Chunk{Object: "chat.completion.chunk", Choices: nil, Usage: &UsageWire{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8}}
}

func TestStreamDecoderHappyPath(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	feed := func(c *Chunk) {
		t.Helper()
		events, err := d.Feed(c)
		if err != nil {
			t.Fatalf("Feed(%+v): %v", c, err)
		}
		got = append(got, events...)
	}
	feed(chunkRole("chatcmpl-1", "gpt-4o"))
	feed(chunkContent("Hel"))
	feed(chunkContent("lo"))
	feed(chunkFinish("stop"))
	feed(chunkUsage())
	flush, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got = append(got, flush...)
	want := []ir.Event{
		ir.MessageStart{ID: "chatcmpl-1", Model: "gpt-4o"},
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

func TestStreamDecoderContentOnlyStart(t *testing.T) {
	d := NewStreamDecoder()
	events, err := d.Feed(chunkContent("hi"))
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	want := []ir.Event{
		ir.MessageStart{Model: "gpt-4o"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "hi"}},
	}
	want[0] = ir.MessageStart{} // chunk carried no model; identity mapping of ""
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events\n got %#v\nwant %#v", events, want)
	}
	if _, err := d.Feed(chunkFinish("stop")); err != nil {
		t.Fatalf("Feed finish: %v", err)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestStreamDecoderMissingFinishReason(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(chunkRole("id", "m")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if _, err := d.Feed(chunkContent("x")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if _, err := d.Flush(); err == nil {
		t.Fatal("Flush without finish_reason: want error")
	}
}

func TestStreamDecoderFlushTwiceAndFeedAfterFlush(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(chunkFinish("stop")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if _, err := d.Flush(); err == nil {
		t.Fatal("second Flush: want error")
	}
	if _, err := d.Feed(chunkContent("late")); err == nil {
		t.Fatal("Feed after flush: want error")
	}
}

func TestToolCallDeltaJSONPresence(t *testing.T) {
	var chunk Chunk
	if err := json.Unmarshal([]byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","type":"function","function":{"name":"","arguments":""}}]},"finish_reason":null}]}`), &chunk); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	call := chunk.Choices[0].Delta.ToolCalls[0]
	if call.Index != 0 || call.ID == nil || *call.ID != "" || call.Type == nil || *call.Type != "function" || call.Function == nil || call.Function.Name == nil || *call.Function.Name != "" || call.Function.Arguments == nil || *call.Function.Arguments != "" {
		t.Fatalf("decoded tool call = %#v", call)
	}
	wire, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatalf("Unmarshal marshaled tool call: %v", err)
	}
	if _, ok := fields["index"]; !ok {
		t.Fatalf("index zero omitted from %s", wire)
	}
}

func TestStreamDecoderFinishReasonMapping(t *testing.T) {
	for wire, want := range map[string]ir.StopReason{
		"length":         ir.StopMaxTokens,
		"content_filter": ir.StopRefusal,
	} {
		d := NewStreamDecoder()
		if _, err := d.Feed(chunkFinish(wire)); err != nil {
			t.Fatalf("Feed(%q): %v", wire, err)
		}
		events, err := d.Flush()
		if err != nil {
			t.Fatalf("Flush: %v", err)
		}
		delta, ok := events[len(events)-2].(ir.MessageDelta)
		if !ok || delta.StopReason != want {
			t.Fatalf("finish %q: events = %#v", wire, events)
		}
	}
}

func TestStreamDecoderModelMap(t *testing.T) {
	d := NewStreamDecoder(WithModelMap(modelmap.Table{"gpt-4o": "mapped-4o"}))
	events, err := d.Feed(chunkRole("id", "gpt-4o"))
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if start := events[0].(ir.MessageStart); start.Model != "mapped-4o" {
		t.Fatalf("model = %q", start.Model)
	}
}

func TestStreamDecoderRoleRestartMidStream(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(chunkRole("id", "m")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if _, err := d.Feed(chunkContent("hi")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if _, err := d.Feed(chunkRole("id", "m")); err == nil {
		t.Fatal("role chunk mid-stream (before finish): want error")
	}
	// After finish_reason, a role-bearing chunk is likewise a restart.
	d2 := NewStreamDecoder()
	_, _ = d2.Feed(chunkRole("id", "m"))
	_, _ = d2.Feed(chunkFinish("stop"))
	if _, err := d2.Feed(chunkRole("id", "m")); err == nil {
		t.Fatal("role chunk after finish_reason: want error")
	}
}

func TestStreamDecoderUsageBeforeFinish(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(chunkRole("id", "m")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	// Wire-tolerated: usage chunk arrives before the finish_reason chunk.
	if events, err := d.Feed(chunkUsage()); err != nil || len(events) != 0 {
		t.Fatalf("usage-before-finish feed: events=%#v err=%v", events, err)
	}
	if _, err := d.Feed(chunkFinish("stop")); err != nil {
		t.Fatalf("Feed finish: %v", err)
	}
	events, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	delta, ok := events[len(events)-2].(ir.MessageDelta)
	if !ok || delta.Usage != (ir.Usage{InputTokens: 3, OutputTokens: 5}) {
		t.Fatalf("events = %#v", events)
	}
}

func TestStreamDecoderMergedUsageOnFinishChunk(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(chunkRole("id", "m")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	finish := chunkFinish("stop")
	finish.Usage = &UsageWire{PromptTokens: 7, CompletionTokens: 9, TotalTokens: 16}
	if _, err := d.Feed(finish); err != nil {
		t.Fatalf("Feed finish: %v", err)
	}
	events, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	delta, ok := events[len(events)-2].(ir.MessageDelta)
	if !ok || delta.Usage != (ir.Usage{InputTokens: 7, OutputTokens: 9}) {
		t.Fatalf("events = %#v", events)
	}
}

func TestStreamEncoderHappyPath(t *testing.T) {
	e := NewStreamEncoder()
	var chunks []*Chunk
	apply := func(ev ir.Event) {
		t.Helper()
		cs, _, err := e.Apply(ev)
		if err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
		chunks = append(chunks, cs...)
	}
	apply(ir.MessageStart{ID: "resp-1", Model: "claude-3"})
	apply(ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}})
	apply(ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "Hel"}})
	apply(ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "lo"}})
	apply(ir.ContentBlockStop{Index: 0})
	apply(ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 3, OutputTokens: 5}})
	apply(ir.MessageDone{})

	if len(chunks) != 4 {
		t.Fatalf("chunks = %#v", chunks)
	}
	wantFirst := &Chunk{
		ID: "resp-1", Object: "chat.completion.chunk", Created: 0, Model: "claude-3",
		Choices: []ChoiceDelta{{Index: 0, Delta: DeltaPayload{Role: "assistant"}}},
	}
	if !reflect.DeepEqual(chunks[0], wantFirst) {
		t.Fatalf("first chunk\n got %#v\nwant %#v", chunks[0], wantFirst)
	}
	if got := *chunks[1].Choices[0].Delta.Content; got != "Hel" {
		t.Fatalf("content chunk = %q", got)
	}
	if got := *chunks[2].Choices[0].Delta.Content; got != "lo" {
		t.Fatalf("content chunk = %q", got)
	}
	last := chunks[3]
	if got := *last.Choices[0].FinishReason; got != "stop" {
		t.Fatalf("finish_reason = %q", got)
	}
	if !reflect.DeepEqual(last.Usage, &UsageWire{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8}) {
		t.Fatalf("usage = %#v", last.Usage)
	}
}

func TestStreamEncoderStopReasonMapping(t *testing.T) {
	for stop, want := range map[ir.StopReason]string{
		ir.StopEndTurn:   "stop",
		ir.StopMaxTokens: "length",
		ir.StopRefusal:   "content_filter",
		ir.StopToolUse:   "tool_calls",
	} {
		e := NewStreamEncoder()
		if _, _, err := e.Apply(ir.MessageStart{ID: "id"}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if _, _, err := e.Apply(ir.ContentBlockStop{Index: 0}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		cs, _, err := e.Apply(ir.MessageDelta{StopReason: stop})
		if err != nil {
			t.Fatalf("Apply(%q): %v", stop, err)
		}
		if got := *cs[0].Choices[0].FinishReason; got != want {
			t.Fatalf("stop %q: finish_reason = %q", stop, got)
		}
	}
}

func TestStreamEncoderStopSequenceLoss(t *testing.T) {
	e := NewStreamEncoder()
	for _, ev := range []ir.Event{
		ir.MessageStart{ID: "id"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockStop{Index: 0},
	} {
		if _, _, err := e.Apply(ev); err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
	}
	cs, ls, err := e.Apply(ir.MessageDelta{StopReason: ir.StopSequence})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if *cs[0].Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q", *cs[0].Choices[0].FinishReason)
	}
	if len(ls) != 1 || ls[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses = %#v", ls)
	}
}

func TestStreamEncoderGrammarErrors(t *testing.T) {
	// MessageDelta before ContentBlockStop.
	e := NewStreamEncoder()
	if _, _, err := e.Apply(ir.MessageStart{ID: "id"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, _, err := e.Apply(ir.MessageDelta{StopReason: ir.StopEndTurn}); err == nil {
		t.Fatal("MessageDelta with open block: want error")
	}

	// ContentBlockStart with unexpected index.
	e = NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{}}); err == nil {
		t.Fatal("ContentBlockStart index 1: want error")
	}

	// InputJSONDelta on a text block.
	e = NewStreamEncoder()
	_, _, _ = e.Apply(ir.MessageStart{ID: "id"})
	_, _, _ = e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}})
	if _, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: []byte(`{`)}}); err == nil {
		t.Fatal("InputJSONDelta: want error")
	}

	// Event after MessageDone.
	e = NewStreamEncoder()
	for _, ev := range []ir.Event{
		ir.MessageStart{ID: "id"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockStop{Index: 0},
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

// Round trip: decode a chunk stream produced by the encoder back to events.
func TestStreamingRoundTrip(t *testing.T) {
	e := NewStreamEncoder()
	events := []ir.Event{
		ir.MessageStart{ID: "chatcmpl-9", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "a"}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "b"}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopMaxTokens, Usage: ir.Usage{InputTokens: 1, OutputTokens: 2}},
		ir.MessageDone{},
	}
	var chunks []*Chunk
	for _, ev := range events {
		cs, _, err := e.Apply(ev)
		if err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
		chunks = append(chunks, cs...)
	}
	d := NewStreamDecoder()
	var got []ir.Event
	for _, c := range chunks {
		es, err := d.Feed(c)
		if err != nil {
			t.Fatalf("Feed: %v", err)
		}
		got = append(got, es...)
	}
	flush, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got = append(got, flush...)
	if !reflect.DeepEqual(got, events) {
		t.Fatalf("round trip\n got %#v\nwant %#v", got, events)
	}
	if ls := d.Losses(); len(ls) != 0 {
		t.Fatalf("losses = %#v", ls)
	}
}

func chunkToolCalls(t *testing.T, toolCalls string) *Chunk {
	t.Helper()
	var chunk Chunk
	if err := json.Unmarshal([]byte(`{"choices":[{"index":0,"delta":{"tool_calls":`+toolCalls+`},"finish_reason":null}]}`), &chunk); err != nil {
		t.Fatalf("unmarshal tool chunk: %v", err)
	}
	return &chunk
}

func irStringToken(t *testing.T, value string) json.RawMessage {
	t.Helper()
	token, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal IR string token: %v", err)
	}
	return token
}

func TestStreamDecoderToolCallsInterleavedAggregate(t *testing.T) {
	d := NewStreamDecoder()
	var beforeFlush []ir.Event
	feed := func(c *Chunk) {
		t.Helper()
		events, err := d.Feed(c)
		if err != nil {
			t.Fatalf("Feed(%+v): %v", c, err)
		}
		beforeFlush = append(beforeFlush, events...)
	}

	feed(chunkRole("chatcmpl-tools", "m"))
	feed(chunkToolCalls(t, `[{"index":0,"id":"call_0","type":"function","function":{"name":"sea","arguments":"{\"a\":"}}]`))
	feed(chunkToolCalls(t, `[{"index":1,"id":"call_1","type":"function","function":{"name":"clo","arguments":""}}]`))
	feed(chunkToolCalls(t, `[{"index":0,"function":{"name":"rch","arguments":"1}"}}]`))
	feed(chunkToolCalls(t, `[{"index":1,"function":{"name":"ck","arguments":"{\"b\":2}"}}]`))
	feed(chunkFinish("tool_calls"))

	wantBeforeFlush := []ir.Event{ir.MessageStart{ID: "chatcmpl-tools", Model: "m"}}
	if !reflect.DeepEqual(beforeFlush, wantBeforeFlush) {
		t.Fatalf("events before Flush\n got %#v\nwant %#v", beforeFlush, wantBeforeFlush)
	}

	flush, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_0", Name: "search", Input: irStringToken(t, `{"a":1}`)}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, `{"a":`)}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, `1}`)}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.ToolUseBlock{ID: "call_1", Name: "clock", Input: irStringToken(t, `{"b":2}`)}},
		ir.ContentBlockDelta{Index: 1, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, "")}},
		ir.ContentBlockDelta{Index: 1, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, `{"b":2}`)}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopToolUse},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(flush, want) {
		t.Fatalf("Flush events\n got %#v\nwant %#v", flush, want)
	}
	if losses := d.Losses(); len(losses) != 0 {
		t.Fatalf("losses = %#v", losses)
	}
}

func TestStreamDecoderToolCallLifecycleErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func(*StreamDecoder) error
	}{
		{
			name: "first index is not zero",
			run: func(d *StreamDecoder) error {
				_, err := d.Feed(chunkToolCalls(t, `[{"index":1,"id":"call_1","type":"function","function":{"name":"x","arguments":""}}]`))
				return err
			},
		},
		{
			name: "new index skips an existing index",
			run: func(d *StreamDecoder) error {
				if _, err := d.Feed(chunkToolCalls(t, `[{"index":0,"id":"call_0","type":"function","function":{"name":"x","arguments":""}}]`)); err != nil {
					return err
				}
				_, err := d.Feed(chunkToolCalls(t, `[{"index":2,"id":"call_2","type":"function","function":{"name":"x","arguments":""}}]`))
				return err
			},
		},
		{
			name: "repeated nonempty IDs conflict",
			run: func(d *StreamDecoder) error {
				if _, err := d.Feed(chunkToolCalls(t, `[{"index":0,"id":"call_0","type":"function","function":{"name":"x","arguments":""}}]`)); err != nil {
					return err
				}
				_, err := d.Feed(chunkToolCalls(t, `[{"index":0,"id":"call_other","function":{"arguments":""}}]`))
				return err
			},
		},
		{
			name: "completed call is missing final ID",
			run: func(d *StreamDecoder) error {
				if _, err := d.Feed(chunkToolCalls(t, `[{"index":0,"type":"function","function":{"name":"x","arguments":"{}"}}]`)); err != nil {
					return err
				}
				if _, err := d.Feed(chunkFinish("tool_calls")); err != nil {
					return err
				}
				_, err := d.Flush()
				return err
			},
		},
		{
			name: "completed call is missing final name",
			run: func(d *StreamDecoder) error {
				if _, err := d.Feed(chunkToolCalls(t, `[{"index":0,"id":"call_0","type":"function","function":{"arguments":"{}"}}]`)); err != nil {
					return err
				}
				if _, err := d.Feed(chunkFinish("tool_calls")); err != nil {
					return err
				}
				_, err := d.Flush()
				return err
			},
		},
		{
			name: "finish reason is not repeated",
			run: func(d *StreamDecoder) error {
				if _, err := d.Feed(chunkFinish("tool_calls")); err != nil {
					return err
				}
				_, err := d.Feed(chunkFinish("tool_calls"))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(NewStreamDecoder()); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestStreamDecoderToolCallUnsupportedUnitAbsorbsDescendants(t *testing.T) {
	d := NewStreamDecoder()
	start, err := d.Feed(chunkToolCalls(t, `[{"index":0,"id":"custom_0","type":"custom","function":{"name":"x","arguments":"first"}}]`))
	if err != nil {
		t.Fatalf("Feed unsupported start: %v", err)
	}
	if !reflect.DeepEqual(start, []ir.Event{ir.MessageStart{}}) {
		t.Fatalf("unsupported start events = %#v", start)
	}
	if _, err := d.Feed(chunkToolCalls(t, `[{"index":0,"function":{"arguments":"second"}}]`)); err != nil {
		t.Fatalf("Feed unsupported descendant: %v", err)
	}
	if _, err := d.Feed(chunkFinish("tool_calls")); err != nil {
		t.Fatalf("Feed finish: %v", err)
	}
	flush, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.MessageDelta{StopReason: ir.StopToolUse},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(flush, want) {
		t.Fatalf("Flush events\n got %#v\nwant %#v", flush, want)
	}
	losses := d.Losses()
	if len(losses) != 1 || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("losses = %#v", losses)
	}
}

func TestStreamDecoderToolCallAllowsLateInitialMetadata(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(chunkRole("chatcmpl-late-metadata", "m")); err != nil {
		t.Fatalf("message start: %v", err)
	}
	if _, err := d.Feed(chunkToolCalls(t, `[{
		"index":0,"function":{"arguments":"{"}
	}]`)); err != nil {
		t.Fatalf("metadata-omitted first delta: %v", err)
	}
	if _, err := d.Feed(chunkToolCalls(t, `[{
		"index":0,"id":"call_0","type":"function","function":{"name":"search","arguments":"}"}
	}]`)); err != nil {
		t.Fatalf("metadata completion: %v", err)
	}
	if _, err := d.Feed(chunkFinish("tool_calls")); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_0", Name: "search", Input: irStringToken(t, "{}")}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, "{")}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, "}")}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopToolUse},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Flush events\n got %#v\nwant %#v", got, want)
	}
	if losses := d.Losses(); len(losses) != 0 {
		t.Fatalf("losses = %#v", losses)
	}
}

func TestStreamDecoderToolCallSkipsNonfunctionAndCompactsIRIndex(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(chunkRole("chatcmpl-compact", "m")); err != nil {
		t.Fatalf("message start: %v", err)
	}
	if _, err := d.Feed(chunkToolCalls(t, `[{
		"index":0,"id":"custom_0","type":"custom","function":{"name":"ignored","arguments":"{}"}
	},{
		"index":1,"id":"call_1","type":"function","function":{"name":"search","arguments":"{}"}
	}]`)); err != nil {
		t.Fatalf("tool calls: %v", err)
	}
	if _, err := d.Feed(chunkFinish("tool_calls")); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_1", Name: "search", Input: irStringToken(t, "{}")}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, "{}")}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopToolUse},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Flush events\n got %#v\nwant %#v", got, want)
	}
	losses := d.Losses()
	if len(losses) != 1 || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("losses = %#v", losses)
	}
}

func TestStreamEncoderToolCallDeltas(t *testing.T) {
	e := NewStreamEncoder()
	var chunks []*Chunk
	apply := func(ev ir.Event) []ir.Loss {
		t.Helper()
		got, losses, err := e.Apply(ev)
		if err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
		chunks = append(chunks, got...)
		return losses
	}

	input := `{"a":1}`
	apply(ir.MessageStart{ID: "chatcmpl-tools", Model: "m"})
	apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_0", Name: "search", Input: irStringToken(t, input)}})
	apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, `{"a":`)}})
	apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, "")}})
	apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, `1}`)}})
	apply(ir.ContentBlockStop{Index: 0})
	if losses := apply(ir.MessageDelta{StopReason: ir.StopToolUse}); len(losses) != 0 {
		t.Fatalf("terminal losses = %#v", losses)
	}
	apply(ir.MessageDone{})

	if len(chunks) != 5 {
		t.Fatalf("chunks = %#v", chunks)
	}
	for i, want := range []string{`{"a":`, "", `1}`} {
		call := chunks[i+1].Choices[0].Delta.ToolCalls
		if len(call) != 1 || call[0].Function == nil || call[0].Function.Arguments == nil || *call[0].Function.Arguments != want {
			t.Fatalf("tool chunk %d = %#v, want arguments %q", i, call, want)
		}
	}
	first := chunks[1].Choices[0].Delta.ToolCalls[0]
	if first.Index != 0 || first.ID == nil || *first.ID != "call_0" || first.Type == nil || *first.Type != "function" || first.Function.Name == nil || *first.Function.Name != "search" {
		t.Fatalf("first tool chunk = %#v", first)
	}
	for i := 2; i <= 3; i++ {
		call := chunks[i].Choices[0].Delta.ToolCalls[0]
		if call.ID != nil || call.Type != nil || call.Function.Name != nil {
			t.Fatalf("continued tool chunk %d repeated start fields: %#v", i, call)
		}
	}
}

func TestStreamEncoderToolCallWithoutDeltasSynthesizesInput(t *testing.T) {
	e := NewStreamEncoder()
	input := `{"count":1e+01}`
	for _, ev := range []ir.Event{
		ir.MessageStart{ID: "chatcmpl-tools", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_0", Name: "count", Input: irStringToken(t, input)}},
		ir.ContentBlockStop{Index: 0},
	} {
		if chunks, _, err := e.Apply(ev); err != nil || len(chunks) != 0 && !isMessageStart(ev) {
			t.Fatalf("Apply(%T): chunks=%#v err=%v", ev, chunks, err)
		}
	}
	chunks, losses, err := e.Apply(ir.MessageDelta{StopReason: ir.StopToolUse})
	if err != nil || len(losses) != 0 {
		t.Fatalf("MessageDelta: chunks=%#v losses=%#v err=%v", chunks, losses, err)
	}
	if len(chunks) != 2 {
		t.Fatalf("terminal chunks = %#v", chunks)
	}
	call := chunks[0].Choices[0].Delta.ToolCalls
	if len(call) != 1 || call[0].Function == nil || call[0].Function.Arguments == nil || *call[0].Function.Arguments != input {
		t.Fatalf("synthesized tool call = %#v", call)
	}
}

func TestStreamEncoderToolCallRejectsMismatchedInput(t *testing.T) {
	e := NewStreamEncoder()
	for _, ev := range []ir.Event{
		ir.MessageStart{ID: "chatcmpl-tools", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_0", Name: "search", Input: irStringToken(t, `{"a":1}`)}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: irStringToken(t, `{"a":2}`)}},
	} {
		if _, _, err := e.Apply(ev); err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
	}
	if _, _, err := e.Apply(ir.ContentBlockStop{Index: 0}); err == nil {
		t.Fatal("ContentBlockStop with mismatched aggregate: want error")
	}
}

func TestStreamEncoderToolCallRejectsNonStringTokens(t *testing.T) {
	for _, token := range []string{"null", "true", "1", "{}", "[]"} {
		raw := json.RawMessage(token)
		t.Run("tool input "+string(raw), func(t *testing.T) {
			e := NewStreamEncoder()
			if _, _, err := e.Apply(ir.MessageStart{ID: "chatcmpl-tools", Model: "m"}); err != nil {
				t.Fatalf("MessageStart: %v", err)
			}
			chunks, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
				ID: "call_0", Name: "search", Input: raw,
			}})
			if err == nil {
				t.Fatalf("ToolUseBlock input %s: want error", raw)
			}
			if len(chunks) != 0 {
				t.Fatalf("ToolUseBlock input %s: got chunks %#v", raw, chunks)
			}
		})
		t.Run("partial json "+string(raw), func(t *testing.T) {
			e := NewStreamEncoder()
			if _, _, err := e.Apply(ir.MessageStart{ID: "chatcmpl-tools", Model: "m"}); err != nil {
				t.Fatalf("MessageStart: %v", err)
			}
			if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{
				ID: "call_0", Name: "search", Input: irStringToken(t, `{}`),
			}}); err != nil {
				t.Fatalf("ToolUseBlock start: %v", err)
			}
			chunks, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: raw}})
			if err == nil {
				t.Fatalf("InputJSONDelta %s: want error", raw)
			}
			if len(chunks) != 0 {
				t.Fatalf("InputJSONDelta %s: got chunks %#v", raw, chunks)
			}
		})
	}
}

func TestStreamEncoderToolCallRejectsTextDelta(t *testing.T) {
	e := NewStreamEncoder()
	for _, ev := range []ir.Event{
		ir.MessageStart{ID: "chatcmpl-tools", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_0", Name: "search", Input: irStringToken(t, `{}`)}},
	} {
		if _, _, err := e.Apply(ev); err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
	}
	if _, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "not tool input"}}); err == nil {
		t.Fatal("TextDelta on tool block: want error")
	}
}

func TestStreamEncoderTextAfterToolNormalizesWithOneLoss(t *testing.T) {
	e := NewStreamEncoder()
	var chunks []*Chunk
	var losses []ir.Loss
	apply := func(ev ir.Event) {
		t.Helper()
		got, gotLosses, err := e.Apply(ev)
		if err != nil {
			t.Fatalf("Apply(%T): %v", ev, err)
		}
		chunks = append(chunks, got...)
		losses = append(losses, gotLosses...)
	}

	apply(ir.MessageStart{ID: "chatcmpl-tools", Model: "m"})
	apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_0", Name: "search", Input: irStringToken(t, `{}`)}})
	apply(ir.ContentBlockStop{Index: 0})
	apply(ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{}})
	apply(ir.ContentBlockDelta{Index: 1, Delta: ir.TextDelta{Text: "after tool"}})
	apply(ir.ContentBlockStop{Index: 1})
	apply(ir.MessageDelta{StopReason: ir.StopToolUse})
	apply(ir.MessageDone{})

	if len(losses) != 1 || losses[0].Reason != ir.LossDegraded {
		t.Fatalf("losses = %#v", losses)
	}
	if len(chunks) != 4 || chunks[1].Choices[0].Delta.Content == nil || *chunks[1].Choices[0].Delta.Content != "after tool" || len(chunks[2].Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("normalized chunks = %#v", chunks)
	}
}

func isMessageStart(ev ir.Event) bool {
	_, ok := ev.(ir.MessageStart)
	return ok
}

func FuzzStreamToolArguments(f *testing.F) {
	for _, seed := range []string{
		"",
		"{}",
		`{"quote":"say \\"hi\\" and \\\\path"}`,
		`{"city":"東京"}`,
		` { "key" : "value" } `,
		"1e+01",
		`{"nested":{"text":"hello"}}`,
		`{"emoji":"🙂"}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if !utf8.ValidString(input) {
			t.Skip()
		}
		boundaries := runeBoundaries(input)
		split := boundaries[len(boundaries)/2]
		fragments := []string{"", input[:split], "", input[split:]}

		d := NewStreamDecoder()
		feed := func(chunk *Chunk) {
			t.Helper()
			if _, err := d.Feed(chunk); err != nil {
				t.Fatalf("Feed(%#v): %v", chunk, err)
			}
		}
		feed(chunkRole("chatcmpl-fuzz", "m"))
		feed(&Chunk{Choices: []ChoiceDelta{{Delta: DeltaPayload{ToolCalls: []ToolCallDelta{{
			Index: 0,
			ID:    stringPointer("call_fuzz"),
			Type:  stringPointer("function"),
			Function: &FunctionDelta{
				Name:      stringPointer("fuzz"),
				Arguments: stringPointer(fragments[0]),
			},
		}}}}}})
		for _, fragment := range fragments[1:] {
			feed(&Chunk{Choices: []ChoiceDelta{{Delta: DeltaPayload{ToolCalls: []ToolCallDelta{{
				Index:    0,
				Function: &FunctionDelta{Arguments: stringPointer(fragment)},
			}}}}}})
		}
		feed(chunkFinish("tool_calls"))
		events, err := d.Flush()
		if err != nil {
			t.Fatalf("Flush: %v", err)
		}

		var toolInput string
		var decodedFragments []string
		for _, event := range events {
			switch event := event.(type) {
			case ir.ContentBlockStart:
				block, ok := event.Block.(ir.ToolUseBlock)
				if !ok {
					continue
				}
				if err := json.Unmarshal(block.Input, &toolInput); err != nil {
					t.Fatalf("unwrap tool input: %v", err)
				}
			case ir.ContentBlockDelta:
				delta, ok := event.Delta.(ir.InputJSONDelta)
				if !ok {
					continue
				}
				var fragment string
				if err := json.Unmarshal(delta.PartialJSON, &fragment); err != nil {
					t.Fatalf("unwrap input fragment: %v", err)
				}
				decodedFragments = append(decodedFragments, fragment)
			}
		}
		if got := strings.Join(decodedFragments, ""); got != toolInput {
			t.Fatalf("joined fragments = %q, tool input = %q", got, toolInput)
		}
		if toolInput != input {
			t.Fatalf("tool input = %q, want %q", toolInput, input)
		}
	})
}

func runeBoundaries(value string) []int {
	boundaries := []int{0}
	for offset := range value {
		if offset != 0 {
			boundaries = append(boundaries, offset)
		}
	}
	return append(boundaries, len(value))
}

func stringPointer(value string) *string {
	return &value
}
