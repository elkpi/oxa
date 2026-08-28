package responses

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/oxa-protocol/oxa/go/ir"
	"github.com/oxa-protocol/oxa/go/modelmap"
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

func TestStreamDecoderSkipsUnsupportedItemDescendants(t *testing.T) {
	d := NewStreamDecoder()
	var got []ir.Event
	for _, event := range []*StreamEvent{
		streamCreated("resp_skip", "gpt-4o-mini"),
		{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{ID: "fc_1", Type: "function_call"}},
		{Type: "response.function_call_arguments.delta", ItemID: "fc_1", OutputIndex: 0, Delta: `{"city"`},
		{Type: "response.output_item.done", OutputIndex: 0, Item: &OutputItem{ID: "fc_1", Type: "function_call"}},
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

func TestStreamDecoderRejectsMissingPartPayloadOnSkippedItem(t *testing.T) {
	d := NewStreamDecoder()
	for _, event := range []*StreamEvent{
		streamCreated("resp_nil_part", "gpt-4o-mini"),
		{Type: "response.output_item.added", OutputIndex: 0, Item: &OutputItem{ID: "fc_1", Type: "function_call"}},
	} {
		if _, err := d.Feed(event); err != nil {
			t.Fatalf("Feed(%s): %v", event.Type, err)
		}
	}
	if _, err := d.Feed(&StreamEvent{
		Type: "response.content_part.added", ItemID: "fc_1", OutputIndex: 0, ContentIndex: 0,
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
