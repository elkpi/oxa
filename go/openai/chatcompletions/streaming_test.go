package chatcompletions

import (
	"reflect"
	"testing"

	"github.com/oxa-protocol/oxa/go/ir"
	"github.com/oxa-protocol/oxa/go/modelmap"
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

func TestStreamDecoderToolCallsDeltaLoss(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(chunkRole("id", "m")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	toolChunk := &Chunk{Choices: []ChoiceDelta{{Delta: DeltaPayload{ToolCalls: []byte(`[{"index":0}]`)}}}}
	events, err := d.Feed(toolChunk)
	if err != nil {
		t.Fatalf("Feed tool chunk: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("tool chunk produced events: %#v", events)
	}
	if _, err := d.Feed(chunkContent("ok")); err != nil {
		t.Fatalf("Feed after tool chunk: %v", err)
	}
	if _, err := d.Feed(chunkFinish("stop")); err != nil {
		t.Fatalf("Feed finish: %v", err)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	ls := d.Losses()
	if len(ls) != 1 || ls[0].Field != "tool_calls" || ls[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("losses = %#v", ls)
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

	// InputJSONDelta is not encodable in M6.
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
