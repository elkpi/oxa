package responses

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

func streamCreated(id, model string) *StreamEvent {
	return &StreamEvent{Type: "response.created", Response: &Response{
		ID: id, Object: "response", Status: "in_progress", Model: model, Output: []OutputItem{},
	}}
}

func streamItemAdded(outputIndex int, itemID string) *StreamEvent {
	return &StreamEvent{Type: "response.output_item.added", OutputIndex: outputIndex, Item: &OutputItem{
		ID: itemID, Type: "message", Status: "in_progress", Role: "assistant", Content: []OutputContent{},
	}}
}

func streamPartAdded(outputIndex, contentIndex int, itemID string) *StreamEvent {
	return &StreamEvent{Type: "response.content_part.added", ItemID: itemID, OutputIndex: outputIndex, ContentIndex: contentIndex,
		Part: &OutputContent{Type: "output_text", Annotations: []json.RawMessage{}}}
}

func streamTextDelta(outputIndex, contentIndex int, itemID, delta string) *StreamEvent {
	return &StreamEvent{Type: "response.output_text.delta", ItemID: itemID, OutputIndex: outputIndex, ContentIndex: contentIndex, Delta: delta}
}

func streamTextDone(outputIndex, contentIndex int, itemID, text string) *StreamEvent {
	return &StreamEvent{Type: "response.output_text.done", ItemID: itemID, OutputIndex: outputIndex, ContentIndex: contentIndex, Text: text}
}

func streamPartDone(outputIndex, contentIndex int, itemID, text string) *StreamEvent {
	return &StreamEvent{Type: "response.content_part.done", ItemID: itemID, OutputIndex: outputIndex, ContentIndex: contentIndex,
		Part: &OutputContent{Type: "output_text", Text: text, Annotations: []json.RawMessage{}}}
}

func streamItemDone(outputIndex int, itemID string) *StreamEvent {
	return &StreamEvent{Type: "response.output_item.done", OutputIndex: outputIndex, Item: &OutputItem{
		ID: itemID, Type: "message", Status: "completed", Role: "assistant", Content: []OutputContent{},
	}}
}

func streamCompleted(id, model string, in, out int64) *StreamEvent {
	return &StreamEvent{Type: "response.completed", Response: &Response{
		ID: id, Object: "response", Status: "completed", Model: model,
		Usage: &UsageWire{InputTokens: in, OutputTokens: out, TotalTokens: in + out},
	}}
}

var wireTextStream = []*StreamEvent{
	streamCreated("resp_1", "gpt-4o-mini"),
	streamItemAdded(0, "msg_1"),
	streamPartAdded(0, 0, "msg_1"),
	streamTextDelta(0, 0, "msg_1", "Hel"),
	streamTextDelta(0, 0, "msg_1", "lo"),
	streamTextDone(0, 0, "msg_1", "Hello"),
	streamPartDone(0, 0, "msg_1", "Hello"),
	streamItemDone(0, "msg_1"),
	streamCompleted("resp_1", "gpt-4o-mini", 3, 5),
}

func TestStreamDecoderHappyPath(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, event := range wireTextStream {
		events, err := d.Feed(event)
		if err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
		got = append(got, events...)
	}
	flush, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(flush) != 0 {
		t.Fatalf("Flush events = %#v", flush)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "resp_1", Model: "gpt-4o-mini"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "Hel"}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "lo"}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 3, OutputTokens: 5}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
	if losses := d.Losses(); len(losses) != 0 {
		t.Fatalf("losses = %#v", losses)
	}
}

func TestStreamEventMarshalWireShape(t *testing.T) {
	payload, err := json.Marshal(streamPartAdded(0, 0, "msg_0"))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	const want = `{"type":"response.content_part.added","item_id":"msg_0","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}`
	if string(payload) != want {
		t.Fatalf("JSON\n got %s\nwant %s", payload, want)
	}
}

func TestStreamEncoderHappyPath(t *testing.T) {
	e := NewStreamEncoder()
	input := []ir.Event{
		ir.MessageStart{ID: "resp_2", Model: "gpt-4o-mini"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "Hel"}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "lo"}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 3, OutputTokens: 5}},
		ir.MessageDone{},
	}
	var wire []*StreamEvent
	for _, event := range input {
		out, losses, err := e.Apply(event)
		if err != nil {
			t.Fatalf("Apply(%T): %v", event, err)
		}
		if len(losses) != 0 {
			t.Fatalf("Apply(%T) losses = %#v", event, losses)
		}
		wire = append(wire, out...)
	}
	if len(wire) != 9 {
		t.Fatalf("wire event count = %d", len(wire))
	}
	if !reflect.DeepEqual(wire[0], streamCreated("resp_2", "gpt-4o-mini")) {
		t.Fatalf("created = %#v", wire[0])
	}
	if !reflect.DeepEqual(wire[1], streamItemAdded(0, "msg_abc123")) ||
		!reflect.DeepEqual(wire[2], streamPartAdded(0, 0, "msg_abc123")) {
		t.Fatalf("opening events = %#v", wire[1:3])
	}
	if !reflect.DeepEqual(wire[3], streamTextDelta(0, 0, "msg_abc123", "Hel")) ||
		!reflect.DeepEqual(wire[4], streamTextDelta(0, 0, "msg_abc123", "lo")) {
		t.Fatalf("delta events = %#v", wire[3:5])
	}
	if !reflect.DeepEqual(wire[5], streamTextDone(0, 0, "msg_abc123", "Hello")) ||
		!reflect.DeepEqual(wire[6], streamPartDone(0, 0, "msg_abc123", "Hello")) {
		t.Fatalf("block completion events = %#v", wire[5:7])
	}
	if wire[7].Type != "response.output_item.done" || wire[7].Item == nil ||
		wire[7].Item.ID != "msg_abc123" || len(wire[7].Item.Content) != 1 || wire[7].Item.Content[0].Text != "Hello" {
		t.Fatalf("item completion event = %#v", wire[7])
	}
	if wire[8].Type != "response.completed" || wire[8].Response == nil ||
		wire[8].Response.Status != "completed" || len(wire[8].Response.Output) != 1 ||
		wire[8].Response.Output[0].Content[0].Text != "Hello" {
		t.Fatalf("terminal event = %#v", wire[8])
	}
}

func TestStreamDecoderSkipsFunctionCallOutputItem(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, event := range []*StreamEvent{
		streamCreated("resp_skip", "gpt-4o-mini"),
		{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{ID: "fco_1", Type: "function_call_output"}},
		{Type: "response.output_item.done", OutputIndex: 0, Item: &OutputItem{ID: "fco_1", Type: "function_call_output"}},
		streamItemAdded(1, "msg_1"),
		streamPartAdded(1, 0, "msg_1"),
		streamTextDone(1, 0, "msg_1", ""),
		streamPartDone(1, 0, "msg_1", ""),
		streamItemDone(1, "msg_1"),
		streamCompleted("resp_skip", "gpt-4o-mini", 1, 1),
	} {
		events, err := d.Feed(event)
		if err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
		got = append(got, events...)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "resp_skip", Model: "gpt-4o-mini"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 1, OutputTokens: 1}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
	losses := d.Losses()
	if len(losses) != 1 || losses[0].Reason != ir.LossUnsupportedSemantic || losses[0].Field != "type" {
		t.Fatalf("losses = %#v", losses)
	}
}

func TestStreamDecoderSkipsUnsupportedPart(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, event := range []*StreamEvent{
		streamCreated("resp_part", "gpt-4o-mini"),
		streamItemAdded(0, "msg_1"),
		{Type: "response.content_part.added", ItemID: "msg_1", OutputIndex: 0, ContentIndex: 0, Part: &OutputContent{Type: "refusal"}},
		{Type: "response.refusal.delta", ItemID: "msg_1", OutputIndex: 0, ContentIndex: 0, Delta: "no"},
		{Type: "response.content_part.done", ItemID: "msg_1", OutputIndex: 0, ContentIndex: 0, Part: &OutputContent{Type: "refusal"}},
		streamPartAdded(0, 1, "msg_1"),
		streamTextDone(0, 1, "msg_1", ""),
		streamPartDone(0, 1, "msg_1", ""),
		streamItemDone(0, "msg_1"),
		streamCompleted("resp_part", "gpt-4o-mini", 0, 0),
	} {
		events, err := d.Feed(event)
		if err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
		got = append(got, events...)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "resp_part", Model: "gpt-4o-mini"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopEndTurn},
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

func TestStreamDecoderSequentialTextBlocks(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, event := range []*StreamEvent{
		streamCreated("resp_blocks", "gpt-4o-mini"),
		streamItemAdded(0, "msg_1"),
		streamPartAdded(0, 0, "msg_1"),
		streamTextDelta(0, 0, "msg_1", "a"),
		streamTextDone(0, 0, "msg_1", "a"),
		streamPartDone(0, 0, "msg_1", "a"),
		streamPartAdded(0, 1, "msg_1"),
		streamTextDelta(0, 1, "msg_1", "b"),
		streamTextDone(0, 1, "msg_1", "b"),
		streamPartDone(0, 1, "msg_1", "b"),
		streamItemDone(0, "msg_1"),
		streamCompleted("resp_blocks", "gpt-4o-mini", 1, 2),
	} {
		events, err := d.Feed(event)
		if err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
		got = append(got, events...)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "resp_blocks", Model: "gpt-4o-mini"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "a"}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{}},
		ir.ContentBlockDelta{Index: 1, Delta: ir.TextDelta{Text: "b"}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 1, OutputTokens: 2}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
}

func TestStreamDecoderUsesStreamWideBlockIndexesAcrossItems(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, event := range []*StreamEvent{
		streamCreated("resp_items", "gpt-4o-mini"),
		streamItemAdded(0, "msg_1"),
		streamPartAdded(0, 0, "msg_1"),
		streamTextDone(0, 0, "msg_1", ""),
		streamPartDone(0, 0, "msg_1", ""),
		streamItemDone(0, "msg_1"),
		streamItemAdded(1, "msg_2"),
		streamPartAdded(1, 0, "msg_2"),
		streamTextDone(1, 0, "msg_2", ""),
		streamPartDone(1, 0, "msg_2", ""),
		streamItemDone(1, "msg_2"),
		streamCompleted("resp_items", "gpt-4o-mini", 0, 0),
	} {
		events, err := d.Feed(event)
		if err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
		got = append(got, events...)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "resp_items", Model: "gpt-4o-mini"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopEndTurn},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
}

func TestStreamDecoderRejectsMismatchedUnknownPartDescendant(t *testing.T) {
	d := NewStreamDecoder()
	for _, event := range []*StreamEvent{
		streamCreated("resp_unknown", "gpt-4o-mini"),
		streamItemAdded(0, "msg_1"),
		streamPartAdded(0, 0, "msg_1"),
	} {
		if _, err := d.Feed(event); err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
	}
	if _, err := d.Feed(&StreamEvent{
		Type: "response.output_text.annotation.added", ItemID: "msg_1", OutputIndex: 0, ContentIndex: 1,
	}); err == nil {
		t.Fatal("unknown part descendant with mismatched content_index: want error")
	}
}

func TestStreamDecoderRejectsMismatchedSkippedItemDescendant(t *testing.T) {
	d := NewStreamDecoder()
	for _, event := range []*StreamEvent{
		streamCreated("resp_skip_mismatch", "m"),
		{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{ID: "fco_1", Type: "function_call_output"}},
	} {
		if _, err := d.Feed(event); err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
	}
	if _, err := d.Feed(&StreamEvent{Type: "response.unknown", ItemID: "other", OutputIndex: 0}); err == nil {
		t.Fatal("mismatched skipped-item descendant: want error")
	}
}

func TestStreamDecoderRejectsMissingPartPayloadOnSkippedItem(t *testing.T) {
	d := NewStreamDecoder()
	for _, event := range []*StreamEvent{
		streamCreated("resp_nil_part", "gpt-4o-mini"),
		{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{ID: "fco_1", Type: "function_call_output"}},
	} {
		if _, err := d.Feed(event); err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
	}
	if _, err := d.Feed(&StreamEvent{
		Type: "response.content_part.added", ItemID: "fco_1", OutputIndex: 0, ContentIndex: 0,
	}); err == nil {
		t.Fatal("content_part.added without part payload: want error")
	}
}

func TestStreamDecoderRejectsMissingPartPayloadOnCompletion(t *testing.T) {
	d := NewStreamDecoder()
	for _, event := range []*StreamEvent{
		streamCreated("resp_nil_done", "gpt-4o-mini"),
		streamItemAdded(0, "msg_1"),
		streamPartAdded(0, 0, "msg_1"),
		streamTextDone(0, 0, "msg_1", ""),
	} {
		if _, err := d.Feed(event); err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
	}
	if _, err := d.Feed(&StreamEvent{
		Type: "response.content_part.done", ItemID: "msg_1", OutputIndex: 0, ContentIndex: 0,
	}); err == nil {
		t.Fatal("content_part.done without part payload: want error")
	}
}

func TestStreamDecoderTerminalStatusAndModelMap(t *testing.T) {
	d := NewStreamDecoder(WithModelMap(modelmap.Table{"gpt-4o-mini": "mapped-4o-mini"}))
	start, err := d.Feed(streamCreated("resp_incomplete", "gpt-4o-mini"))
	if err != nil {
		t.Fatalf("Feed response.created: %v", err)
	}
	if start[0].(ir.MessageStart).Model != "mapped-4o-mini" {
		t.Fatalf("mapped model = %#v", start[0])
	}
	events, err := d.Feed(&StreamEvent{Type: "response.incomplete", Response: &Response{
		ID: "resp_incomplete", Object: "response", Status: "incomplete", Model: "gpt-4o-mini",
		IncompleteDetails: &IncompleteWire{Reason: "max_output_tokens"},
		Usage:             &UsageWire{InputTokens: 7, OutputTokens: 9, TotalTokens: 16},
	}})
	if err != nil {
		t.Fatalf("Feed response.incomplete: %v", err)
	}
	want := []ir.Event{
		ir.MessageDelta{StopReason: ir.StopMaxTokens, Usage: ir.Usage{InputTokens: 7, OutputTokens: 9}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("terminal events\n got %#v\nwant %#v", events, want)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestStreamDecoderGrammarAndFlushErrors(t *testing.T) {
	d := NewStreamDecoder()
	if _, err := d.Feed(streamItemAdded(0, "msg_1")); err == nil {
		t.Fatal("output_item before response.created: want error")
	}
	d = NewStreamDecoder()
	if _, err := d.Feed(streamCreated("resp_open", "m")); err != nil {
		t.Fatalf("Feed response.created: %v", err)
	}
	if _, err := d.Feed(streamItemAdded(0, "msg_1")); err != nil {
		t.Fatalf("Feed output_item: %v", err)
	}
	if _, err := d.Feed(streamCompleted("resp_open", "m", 0, 0)); err == nil {
		t.Fatal("terminal response with open item: want error")
	}
	d = NewStreamDecoder()
	if _, err := d.Flush(); err == nil {
		t.Fatal("Flush without terminal response: want error")
	}
	d = NewStreamDecoder()
	for _, event := range wireTextStream {
		if _, err := d.Feed(event); err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if _, err := d.Flush(); err == nil {
		t.Fatal("second Flush: want error")
	}
	if _, err := d.Feed(streamCreated("late", "m")); err == nil {
		t.Fatal("Feed after Flush: want error")
	}
}

func TestStreamEncoderStatusModelMapAndGrammar(t *testing.T) {
	e := NewStreamEncoder(WithModelMap(modelmap.Table{"gpt-4o-mini": "gpt-4o"}))
	created, _, err := e.Apply(ir.MessageStart{ID: "resp_status", Model: "gpt-4o-mini"})
	if err != nil || created[0].Response.Model != "gpt-4o" {
		t.Fatalf("created = %#v err=%v", created, err)
	}
	terminal, losses, err := e.Apply(ir.MessageDelta{StopReason: ir.StopSequence, StopSequence: "END"})
	if err != nil {
		t.Fatalf("Apply StopSequence: %v", err)
	}
	if terminal[0].Type != "response.completed" || terminal[0].Response == nil || terminal[0].Response.Model != "gpt-4o" ||
		len(losses) != 1 || losses[0].Field != "stop_sequence" {
		t.Fatalf("terminal/losses = %#v %#v", terminal, losses)
	}
	if _, _, err := e.Apply(ir.MessageDone{}); err != nil {
		t.Fatalf("Apply MessageDone: %v", err)
	}

	e = NewStreamEncoder()
	if _, _, err := e.Apply(ir.MessageStart{ID: "resp_grammar"}); err != nil {
		t.Fatalf("Apply MessageStart: %v", err)
	}
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{}}); err == nil {
		t.Fatal("ContentBlockStart index 1: want error")
	}
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call", Name: "tool"}}); err == nil {
		t.Fatal("non-text ContentBlockStart: want error")
	}
	if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}}); err != nil {
		t.Fatalf("Apply ContentBlockStart: %v", err)
	}
	if _, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: []byte(`{`)}}); err == nil {
		t.Fatal("InputJSONDelta: want error")
	}
	if _, _, err := e.Apply(ir.MessageDelta{StopReason: ir.StopEndTurn}); err == nil {
		t.Fatal("MessageDelta with open block: want error")
	}
}

func TestStreamEncoderUsesFailedTerminalForRefusal(t *testing.T) {
	e := NewStreamEncoder()
	if _, _, err := e.Apply(ir.MessageStart{ID: "resp_refusal", Model: "gpt-4o-mini"}); err != nil {
		t.Fatalf("Apply MessageStart: %v", err)
	}
	wire, losses, err := e.Apply(ir.MessageDelta{
		StopReason: ir.StopRefusal,
		Usage:      ir.Usage{InputTokens: 2, OutputTokens: 3},
	})
	if err != nil {
		t.Fatalf("Apply MessageDelta: %v", err)
	}
	if len(losses) != 0 || len(wire) != 1 || wire[0].Type != "response.failed" || wire[0].Response == nil ||
		wire[0].Response.Status != "failed" || wire[0].Response.Error == nil || wire[0].Response.Error.Code != "refusal" ||
		!reflect.DeepEqual(wire[0].Response.Usage, &UsageWire{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}) {
		t.Fatalf("terminal/losses = %#v %#v", wire, losses)
	}
}

func TestStreamingRoundTrip(t *testing.T) {
	want := []ir.Event{
		ir.MessageStart{ID: "resp_roundtrip", Model: "gpt-4o-mini"},
		ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "a"}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{}},
		ir.ContentBlockDelta{Index: 1, Delta: ir.TextDelta{Text: "b"}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopMaxTokens, Usage: ir.Usage{InputTokens: 1, OutputTokens: 2}},
		ir.MessageDone{},
	}
	e := NewStreamEncoder()
	d := NewStreamDecoder()
	var got []ir.Event
	for _, event := range want {
		wire, losses, err := e.Apply(event)
		if err != nil {
			t.Fatalf("Apply(%T): %v", event, err)
		}
		if len(losses) != 0 {
			t.Fatalf("Apply(%T) losses = %#v", event, losses)
		}
		for _, streamEvent := range wire {
			decoded, err := d.Feed(streamEvent)
			if err != nil {
				t.Fatalf("Feed(%s): %v", streamEvent.Type, err)
			}
			got = append(got, decoded...)
		}
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip\n got %#v\nwant %#v", got, want)
	}
}

func responseIRStringToken(t *testing.T, value string) json.RawMessage {
	t.Helper()
	token, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal IR string token: %v", err)
	}
	return token
}

func responseStreamEventFromJSON(t *testing.T, raw string) *StreamEvent {
	t.Helper()
	var event StreamEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatalf("unmarshal stream event %s: %v", raw, err)
	}
	return &event
}

func TestStreamEventFunctionCallArgumentWireRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		wire string
	}{
		{
			name: "delta preserves required identity and empty fragment",
			wire: `{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":""}`,
		},
		{
			name: "done preserves call identity and complete arguments",
			wire: `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"call_id":"call_1","name":"weather","arguments":"{\"city\": 1e+01}"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := responseStreamEventFromJSON(t, test.wire)
			got, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != test.wire {
				t.Fatalf("wire JSON\n got %s\nwant %s", got, test.wire)
			}
		})
	}
}

func TestStreamDecoderFunctionCallArgumentsReplayAtItemDone(t *testing.T) {
	const input = `{"city": "Tokyo", "count": 1e+01}`
	fragments := []string{"", `{"city": `, `"Tokyo", `, `"count": 1e+01}`}
	d := NewStreamDecoder()
	var got []ir.Event
	feed := func(event *StreamEvent) []ir.Event {
		t.Helper()
		events, err := d.Feed(event)
		if err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
		got = append(got, events...)
		return events
	}

	feed(streamCreated("resp_function", "gpt-4o-mini"))
	feed(&StreamEvent{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{
		ID: "fc_1", Type: "function_call", Status: "in_progress", CallID: "call_1", Name: "weather", Arguments: fragments[0],
	}})
	for _, fragment := range fragments[1:] {
		feed(&StreamEvent{Type: "response.function_call_arguments.delta", ItemID: "fc_1", OutputIndex: 0, Delta: fragment})
	}
	if events := feed(responseStreamEventFromJSON(t, `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"call_id":"call_1","name":"weather","arguments":"{\"city\": \"Tokyo\", \"count\": 1e+01}"}`)); len(events) != 0 {
		t.Fatalf("arguments.done emitted IR events = %#v", events)
	}
	itemDoneEvents := feed(&StreamEvent{Type: "response.output_item.done", OutputIndex: 0, Item: &OutputItem{
		ID: "fc_1", Type: "function_call", Status: "completed", CallID: "call_1", Name: "weather", Arguments: input,
	}})
	wantReplay := []ir.Event{
		ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_1", Name: "weather", Input: responseIRStringToken(t, input)}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, fragments[0])}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, fragments[1])}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, fragments[2])}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, fragments[3])}},
		ir.ContentBlockStop{Index: 0},
	}
	if !reflect.DeepEqual(itemDoneEvents, wantReplay) {
		t.Fatalf("item completion replay\n got %#v\nwant %#v", itemDoneEvents, wantReplay)
	}
	feed(&StreamEvent{Type: "response.completed", Response: &Response{
		ID: "resp_function", Object: "response", Status: "completed", Model: "gpt-4o-mini",
		Output: []OutputItem{{ID: "fc_1", Type: "function_call", Status: "completed", CallID: "call_1", Name: "weather", Arguments: input}},
		Usage:  &UsageWire{InputTokens: 3, OutputTokens: 5, TotalTokens: 8},
	}})
	want := append([]ir.Event{ir.MessageStart{ID: "resp_function", Model: "gpt-4o-mini"}}, wantReplay...)
	want = append(want, ir.MessageDelta{StopReason: ir.StopToolUse, Usage: ir.Usage{InputTokens: 3, OutputTokens: 5}}, ir.MessageDone{})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded events\n got %#v\nwant %#v", got, want)
	}
	if losses := d.Losses(); len(losses) != 0 {
		t.Fatalf("losses = %#v", losses)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestStreamDecoderFunctionCallLifecycleErrors(t *testing.T) {
	newActiveCall := func(t *testing.T) *StreamDecoder {
		t.Helper()
		d := NewStreamDecoder()
		for _, event := range []*StreamEvent{
			streamCreated("resp_errors", "m"),
			{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{
				ID: "fc_1", Type: "function_call", Status: "in_progress", CallID: "call_1", Name: "weather", Arguments: `{"x":`,
			}},
		} {
			if _, err := d.Feed(event); err != nil {
				t.Fatalf("Feed(%s): %v", event.Type, err)
			}
		}
		return d
	}
	tests := []struct {
		name string
		run  func(*StreamDecoder) error
	}{
		{
			name: "argument delta item ID differs",
			run: func(d *StreamDecoder) error {
				_, err := d.Feed(&StreamEvent{Type: "response.function_call_arguments.delta", ItemID: "fc_other", OutputIndex: 0, Delta: `1}`})
				return err
			},
		},
		{
			name: "argument delta output index differs",
			run: func(d *StreamDecoder) error {
				_, err := d.Feed(&StreamEvent{Type: "response.function_call_arguments.delta", ItemID: "fc_1", OutputIndex: 1, Delta: `1}`})
				return err
			},
		},
		{
			name: "arguments done call ID differs",
			run: func(d *StreamDecoder) error {
				_, err := d.Feed(responseStreamEventFromJSON(t, `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"call_id":"call_other","name":"weather","arguments":"{\"x\":"}`))
				return err
			},
		},
		{
			name: "arguments done name differs",
			run: func(d *StreamDecoder) error {
				_, err := d.Feed(responseStreamEventFromJSON(t, `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"call_id":"call_1","name":"other","arguments":"{\"x\":"}`))
				return err
			},
		},
		{
			name: "arguments done final text differs",
			run: func(d *StreamDecoder) error {
				_, err := d.Feed(responseStreamEventFromJSON(t, `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"call_id":"call_1","name":"weather","arguments":"{}"}`))
				return err
			},
		},
		{
			name: "argument delta follows arguments done",
			run: func(d *StreamDecoder) error {
				if _, err := d.Feed(responseStreamEventFromJSON(t, `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"call_id":"call_1","name":"weather","arguments":"{\"x\":"}`)); err != nil {
					return err
				}
				_, err := d.Feed(&StreamEvent{Type: "response.function_call_arguments.delta", ItemID: "fc_1", OutputIndex: 0, Delta: `1}`})
				return err
			},
		},
		{
			name: "arguments done is repeated",
			run: func(d *StreamDecoder) error {
				done := responseStreamEventFromJSON(t, `{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"call_id":"call_1","name":"weather","arguments":"{\"x\":"}`)
				if _, err := d.Feed(done); err != nil {
					return err
				}
				_, err := d.Feed(done)
				return err
			},
		},
		{
			name: "terminal response leaves retained call open",
			run: func(d *StreamDecoder) error {
				_, err := d.Feed(streamCompleted("resp_errors", "m", 0, 0))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(newActiveCall(t)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestStreamDecoderFunctionCallOutputIsAbsorbedOnce(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, event := range []*StreamEvent{
		streamCreated("resp_output", "m"),
		{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{ID: "fco_1", Type: "function_call_output", Status: "in_progress", CallID: "call_1"}},
		{Type: "response.output_item.done", OutputIndex: 0, Item: &OutputItem{ID: "fco_1", Type: "function_call_output", Status: "completed", CallID: "call_1"}},
		streamCompleted("resp_output", "m", 1, 2),
	} {
		events, err := d.Feed(event)
		if err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
		got = append(got, events...)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := []ir.Event{
		ir.MessageStart{ID: "resp_output", Model: "m"},
		ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 1, OutputTokens: 2}},
		ir.MessageDone{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events\n got %#v\nwant %#v", got, want)
	}
	wantLoss := ir.Loss{
		Path:   "output[0]",
		Field:  "type",
		Reason: ir.LossUnsupportedSemantic,
		Detail: "N-S-10: Responses function_call_output has no supported IR block mapping; response.output_item.done completes and is absorbed for this item-only lifecycle vector",
	}
	if losses := d.Losses(); !reflect.DeepEqual(losses, []ir.Loss{wantLoss}) {
		t.Fatalf("losses\n got %#v\nwant %#v", losses, []ir.Loss{wantLoss})
	}
}

func TestStreamDecoderFunctionCallOutputRejectsMismatchedCallID(t *testing.T) {
	d := NewStreamDecoder()
	for _, event := range []*StreamEvent{
		streamCreated("resp_output_mismatch", "m"),
		{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{
			ID: "fco_1", Type: "function_call_output", Status: "in_progress", CallID: "call_A",
		}},
	} {
		if _, err := d.Feed(event); err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
	}
	if _, err := d.Feed(&StreamEvent{Type: "response.output_item.done", OutputIndex: 0, Item: &OutputItem{
		ID: "fco_1", Type: "function_call_output", Status: "completed", CallID: "call_B",
	}}); err == nil {
		t.Fatal("mismatched function_call_output call_id: want error")
	}
}

func TestStreamEncoderFunctionCallItemsPreserveSerialOutputOrder(t *testing.T) {
	e := NewStreamEncoder()
	var wire []*StreamEvent
	apply := func(event ir.Event) {
		t.Helper()
		events, losses, err := e.Apply(event)
		if err != nil {
			t.Fatalf("Apply(%T): %v", event, err)
		}
		if len(losses) != 0 {
			t.Fatalf("Apply(%T) losses = %#v", event, losses)
		}
		wire = append(wire, events...)
	}
	input := `{"tz": "Asia\/Shanghai", "hour": 1e+01}`
	apply(ir.MessageStart{ID: "resp_serial", Model: "gpt-4o-mini"})
	apply(ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{}})
	apply(ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "Looking."}})
	apply(ir.ContentBlockStop{Index: 0})
	apply(ir.ContentBlockStart{Index: 1, Block: ir.ToolUseBlock{ID: "call_7", Name: "clock", Input: responseIRStringToken(t, input)}})
	apply(ir.ContentBlockDelta{Index: 1, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, `{"tz":`)}})
	apply(ir.ContentBlockDelta{Index: 1, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, ` "Asia\/Shanghai",`)}})
	apply(ir.ContentBlockDelta{Index: 1, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, ` "hour": 1e+01}`)}})
	apply(ir.ContentBlockStop{Index: 1})
	apply(ir.ContentBlockStart{Index: 2, Block: ir.TextBlock{}})
	apply(ir.ContentBlockDelta{Index: 2, Delta: ir.TextDelta{Text: "Done."}})
	apply(ir.ContentBlockStop{Index: 2})
	apply(ir.MessageDelta{StopReason: ir.StopToolUse, Usage: ir.Usage{InputTokens: 6, OutputTokens: 8}})
	apply(ir.MessageDone{})

	wantTypes := []string{
		"response.created",
		"response.output_item.added", "response.content_part.added", "response.output_text.delta", "response.output_text.done", "response.content_part.done", "response.output_item.done",
		"response.output_item.added", "response.function_call_arguments.delta", "response.function_call_arguments.delta", "response.function_call_arguments.delta", "response.function_call_arguments.done", "response.output_item.done",
		"response.output_item.added", "response.content_part.added", "response.output_text.delta", "response.output_text.done", "response.content_part.done", "response.output_item.done",
		"response.completed",
	}
	if len(wire) != len(wantTypes) {
		t.Fatalf("wire count = %d, want %d: %#v", len(wire), len(wantTypes), wire)
	}
	for index, wantType := range wantTypes {
		if wire[index].Type != wantType {
			t.Fatalf("wire[%d].Type = %q, want %q", index, wire[index].Type, wantType)
		}
	}
	if item := wire[7].Item; item == nil || wire[7].OutputIndex != 1 || item.ID != "fc_abc123" || item.CallID != "call_7" || item.Name != "clock" || item.Arguments != "" {
		t.Fatalf("function_call item start = %#v", wire[7])
	}
	argumentsDone, err := json.Marshal(wire[11])
	if err != nil {
		t.Fatalf("marshal arguments.done: %v", err)
	}
	const wantArgumentsDone = `{"type":"response.function_call_arguments.done","item_id":"fc_abc123","output_index":1,"call_id":"call_7","name":"clock","arguments":"{\"tz\": \"Asia\\/Shanghai\", \"hour\": 1e+01}"}`
	if string(argumentsDone) != wantArgumentsDone {
		t.Fatalf("arguments.done JSON\n got %s\nwant %s", argumentsDone, wantArgumentsDone)
	}
	if item := wire[12].Item; item == nil || item.ID != "fc_abc123" || item.CallID != "call_7" || item.Name != "clock" || item.Arguments != input {
		t.Fatalf("function_call item completion = %#v", wire[12])
	}
	if item := wire[13].Item; item == nil || wire[13].OutputIndex != 2 || item.ID != "msg_abc456" || item.Type != "message" {
		t.Fatalf("second message item start = %#v", wire[13])
	}
	wantOutput := []OutputItem{
		{ID: "msg_abc123", Type: "message", Status: "completed", Role: "assistant", Content: []OutputContent{{Type: "output_text", Text: "Looking.", Annotations: []json.RawMessage{}}}},
		{ID: "fc_abc123", Type: "function_call", Status: "completed", CallID: "call_7", Name: "clock", Arguments: input},
		{ID: "msg_abc456", Type: "message", Status: "completed", Role: "assistant", Content: []OutputContent{{Type: "output_text", Text: "Done.", Annotations: []json.RawMessage{}}}},
	}
	if terminal := wire[len(wire)-1]; terminal.Response == nil || !reflect.DeepEqual(terminal.Response.Output, wantOutput) {
		t.Fatalf("terminal output\n got %#v\nwant %#v", terminal, wantOutput)
	}
}

func TestStreamEncoderFunctionCallWithoutDeltasSynthesizesArguments(t *testing.T) {
	e := NewStreamEncoder()
	input := `{"count": 1e+01}`
	if _, _, err := e.Apply(ir.MessageStart{ID: "resp_synth", Model: "m"}); err != nil {
		t.Fatalf("MessageStart: %v", err)
	}
	start, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_count", Name: "count", Input: responseIRStringToken(t, input)}})
	if err != nil {
		t.Fatalf("ToolUseBlock start: %v", err)
	}
	if len(start) != 1 || start[0].Type != "response.output_item.added" || start[0].Item == nil || start[0].Item.ID != "fc_abc123" || start[0].Item.CallID != "call_count" || start[0].Item.Name != "count" || start[0].Item.Arguments != "" {
		t.Fatalf("function item start = %#v", start)
	}
	stop, _, err := e.Apply(ir.ContentBlockStop{Index: 0})
	if err != nil {
		t.Fatalf("ToolUseBlock stop: %v", err)
	}
	if len(stop) != 3 || stop[0].Type != "response.function_call_arguments.delta" || stop[0].Delta != input || stop[1].Type != "response.function_call_arguments.done" || stop[2].Type != "response.output_item.done" {
		t.Fatalf("synthesized tool completion = %#v", stop)
	}
	argumentsDone, err := json.Marshal(stop[1])
	if err != nil {
		t.Fatalf("marshal arguments.done: %v", err)
	}
	const wantArgumentsDone = `{"type":"response.function_call_arguments.done","item_id":"fc_abc123","output_index":0,"call_id":"call_count","name":"count","arguments":"{\"count\": 1e+01}"}`
	if string(argumentsDone) != wantArgumentsDone {
		t.Fatalf("arguments.done JSON\n got %s\nwant %s", argumentsDone, wantArgumentsDone)
	}
}

func TestStreamEncoderFunctionCallRejectsInvalidDeltasAndAggregate(t *testing.T) {
	newToolEncoder := func(t *testing.T) *StreamEncoder {
		t.Helper()
		e := NewStreamEncoder()
		for _, event := range []ir.Event{
			ir.MessageStart{ID: "resp_errors", Model: "m"},
			ir.ContentBlockStart{Index: 0, Block: ir.ToolUseBlock{ID: "call_1", Name: "weather", Input: responseIRStringToken(t, `{"city":1}`)}},
		} {
			if _, _, err := e.Apply(event); err != nil {
				t.Fatalf("Apply(%T): %v", event, err)
			}
		}
		return e
	}
	tests := []struct {
		name string
		run  func(*StreamEncoder) error
	}{
		{
			name: "text delta on tool block",
			run: func(e *StreamEncoder) error {
				_, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "not arguments"}})
				return err
			},
		},
		{
			name: "input delta index differs",
			run: func(e *StreamEncoder) error {
				_, _, err := e.Apply(ir.ContentBlockDelta{Index: 1, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, `{"city":1}`)}})
				return err
			},
		},
		{
			name: "joined arguments differ from tool input",
			run: func(e *StreamEncoder) error {
				if _, _, err := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: ir.InputJSONDelta{PartialJSON: responseIRStringToken(t, `{"city":2}`)}}); err != nil {
					return err
				}
				_, _, err := e.Apply(ir.ContentBlockStop{Index: 0})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(newToolEncoder(t)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func FuzzStreamFunctionCallArguments(f *testing.F) {
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
		boundaries := responseRuneBoundaries(input)
		split := boundaries[len(boundaries)/2]
		fragments := []string{"", input[:split], "", input[split:]}

		d := NewStreamDecoder()
		feed := func(event *StreamEvent) []ir.Event {
			t.Helper()
			events, err := d.Feed(event)
			if err != nil {
				t.Fatalf("Feed(%s): %v", event.Type, err)
			}
			return events
		}
		feed(streamCreated("resp_fuzz", "m"))
		feed(&StreamEvent{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{
			ID: "fc_fuzz", Type: "function_call", Status: "in_progress", CallID: "call_fuzz", Name: "fuzz", Arguments: fragments[0],
		}})
		for _, fragment := range fragments[1:] {
			feed(&StreamEvent{Type: "response.function_call_arguments.delta", ItemID: "fc_fuzz", OutputIndex: 0, Delta: fragment})
		}
		events := feed(&StreamEvent{Type: "response.output_item.done", OutputIndex: 0, Item: &OutputItem{
			ID: "fc_fuzz", Type: "function_call", Status: "completed", CallID: "call_fuzz", Name: "fuzz", Arguments: input,
		}})
		feed(streamCompleted("resp_fuzz", "m", 0, 0))
		if _, err := d.Flush(); err != nil {
			t.Fatalf("Flush: %v", err)
		}

		var toolInput string
		var decodedFragments []string
		toolFound := false
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
				toolFound = true
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
		if !toolFound {
			t.Fatal("function_call item did not replay a ToolUseBlock")
		}
		if got := strings.Join(decodedFragments, ""); got != toolInput {
			t.Fatalf("joined fragments = %q, tool input = %q", got, toolInput)
		}
		if toolInput != input {
			t.Fatalf("tool input = %q, want %q", toolInput, input)
		}
	})
}

func responseRuneBoundaries(value string) []int {
	boundaries := []int{0}
	for offset := range value {
		if offset != 0 {
			boundaries = append(boundaries, offset)
		}
	}
	return append(boundaries, len(value))
}
