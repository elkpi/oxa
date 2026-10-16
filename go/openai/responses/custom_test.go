package responses

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func TestCustomUnsupportedOutputDoesNotInspectFields(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"role":"user","content":"go"},{"type":"custom_tool_call","call_id":"c","name":"run","input":"code"},{"type":"custom_tool_call_output","call_id":"c","output":[{"type":"input_file","file_id":"f","text":{"opaque":true}}]}]}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Path != "input[2].output[0]" || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("%+v", losses)
	}
	result := req.Messages[2].Content[0].(ir.CustomToolResultBlock)
	if result.Output.Text != nil || len(result.Output.Content) != 0 {
		t.Fatalf("%+v", result.Output)
	}
	var wire Request
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeRequest(&wire); err != nil {
		t.Fatalf("typed unsupported output must not inspect unrelated fields: %v", err)
	}
}

func TestCustomOutputAuditsVariantSpecificFields(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"role":"user","content":"go"},{"type":"custom_tool_call","call_id":"c","name":"run","input":"code"},{"type":"custom_tool_call_output","call_id":"c","output":[{"type":"input_text","text":"kept","image_url":{"opaque":true}}]}]}`)
	_, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Path != "input[2].output[0].image_url" || losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("%+v", losses)
	}
}

func customDecoder(t *testing.T) *StreamDecoder {
	t.Helper()
	d := NewStreamDecoder()
	for _, raw := range []string{`{"type":"response.created","response":{"id":"r","model":"m","status":"in_progress"}}`, `{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc","call_id":"c","name":"run","input":""}}`} {
		if _, err := d.FeedJSON([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestCustomDeltaRequiresStringField(t *testing.T) {
	for _, raw := range []string{`{"type":"response.custom_tool_call_input.delta","item_id":"ctc","output_index":0}`, `{"type":"response.custom_tool_call_input.delta","item_id":"ctc","output_index":0,"delta":null}`} {
		if _, err := customDecoder(t).FeedJSON([]byte(raw)); err == nil {
			t.Fatalf("missing or null delta accepted: %s", raw)
		}
	}
	if _, err := customDecoder(t).FeedJSON([]byte(`{"type":"response.custom_tool_call_input.delta","item_id":"ctc","output_index":0,"delta":""}`)); err != nil {
		t.Fatal(err)
	}
}

func TestCustomStreamRejectsConflictingSnapshotsAndIdentity(t *testing.T) {
	for _, raw := range []string{
		`{"type":"response.custom_tool_call_input.done","item_id":"ctc","output_index":0,"input":"changed"}`,
		`{"type":"response.custom_tool_call_input.delta","item_id":"wrong","output_index":0,"delta":"code"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"ctc","output_index":0,"delta":"code"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc","call_id":"wrong","name":"run","input":"code"}}`,
	} {
		d := customDecoder(t)
		if _, err := d.FeedJSON([]byte(`{"type":"response.custom_tool_call_input.delta","item_id":"ctc","output_index":0,"delta":"code"}`)); err != nil {
			t.Fatal(err)
		}
		if _, err := d.FeedJSON([]byte(raw)); err == nil {
			t.Fatalf("invalid event accepted: %s", raw)
		}
	}
	d := customDecoder(t)
	if _, err := d.FeedJSON([]byte(`{"type":"response.custom_tool_call_input.done","item_id":"ctc","output_index":0,"input":"code"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.FeedJSON([]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc","call_id":"c","name":"run","input":"changed"}}`)); err == nil {
		t.Fatal("conflicting full snapshots accepted")
	}
}

func TestCustomTerminalSnapshotAllowsOptionalItemID(t *testing.T) {
	d := customDecoder(t)
	if _, err := d.FeedJSON([]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc","call_id":"c","name":"run","input":"code"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.FeedJSON([]byte(`{"type":"response.completed","response":{"id":"r","model":"m","status":"completed","output":[{"type":"custom_tool_call","call_id":"c","name":"run","input":"code"}]}}`)); err != nil {
		t.Fatalf("optional terminal item id was required: %v", err)
	}
	if _, err := d.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestCustomRawAuditRetainsSourceOrder(t *testing.T) {
	raw := []byte(`{"model":"m","input":"hi","tools":[{"type":"custom","name":"run","async":false,"format":{"type":"grammar","syntax":"regex","definition":".*","extra":null}}],"tool_choice":{"type":"custom","name":"run","extra":false}}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tools[0].async", "tools[0].format.extra", "tool_choice.extra"}
	if len(losses) != len(want) {
		t.Fatalf("%+v", losses)
	}
	for i, path := range want {
		if losses[i].Path != path || losses[i].Reason != ir.LossUnmappedField {
			t.Fatalf("%+v", losses)
		}
	}
	if req.Tools[0].Format.Definition != ".*" || req.ToolChoice.ToolType != ir.ToolTypeCustom {
		t.Fatalf("%+v", req)
	}
}

func TestCustomEncoderRejectsJSONDeltaAndFragmentMismatch(t *testing.T) {
	for _, delta := range []ir.Delta{ir.InputJSONDelta{PartialJSON: json.RawMessage(`"code"`)}, ir.CustomToolInputDelta{Text: json.RawMessage(`"changed"`)}} {
		e := NewStreamEncoder()
		if _, _, err := e.Apply(ir.MessageStart{ID: "r", Model: "m"}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.Apply(ir.ContentBlockStart{Index: 0, Block: ir.CustomToolUseBlock{ID: "c", Name: "run", Input: json.RawMessage(`"code"`)}}); err != nil {
			t.Fatal(err)
		}
		_, _, deltaErr := e.Apply(ir.ContentBlockDelta{Index: 0, Delta: delta})
		if deltaErr != nil {
			continue
		}
		if _, _, err := e.Apply(ir.ContentBlockStop{Index: 0}); err == nil {
			t.Fatal("mismatched custom input was accepted")
		}
	}
}
