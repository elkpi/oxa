package responses

import (
	"encoding/json"
	"fmt"
	"testing"

	ant "github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/ir"
	cc "github.com/elkpi/oxa/go/v2/openai/chatcompletions"
)

func TestApplyPatchNonstreamValidation(t *testing.T) {
	prefix := `{"id":"r","object":"response","status":"completed","model":"m","output":[`
	cases := []struct {
		name string
		item string
	}{
		{"missing-path", `{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file"}}`},
		{"null-path", `{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file","path":null}}`},
		{"object-path-known-op", `{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file","path":{"invalid":true}}}`},
		{"missing-diff-create", `{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"create_file","path":"a"}}`},
		{"missing-call-id", `{"id":"apc","type":"apply_patch_call","status":"completed","operation":{"type":"delete_file","path":"a"}}`},
		{"missing-item-id", `{"type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file","path":"a"}}`},
		{"missing-operation", `{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed"}`},
		{"missing-status", `{"id":"apc","type":"apply_patch_call","call_id":"c","operation":{"type":"delete_file","path":"a"}}`},
	}
	for _, c := range cases {
		_, _, err := DecodeResponseJSON([]byte(prefix + c.item + `]}`))
		if err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestApplyPatchRequestAssociation(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			"duplicate-filtered-id",
			`{"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call","call_id":"same","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call","call_id":"same","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call_output","call_id":"same","status":"completed"},{"type":"apply_patch_call_output","call_id":"same","status":"completed"}]}`,
		},
		{
			"mismatch-filtered-id",
			`{"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call","call_id":"a","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call_output","call_id":"b","status":"future"}]}`,
		},
		{
			"orphaned-apply-patch-result",
			`{"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call_output","call_id":"c","status":"completed"}]}`,
		},
	}
	for _, c := range cases {
		_, _, err := DecodeRequestJSON([]byte(c.raw))
		if err == nil {
			t.Errorf("%s: expected association error, got nil", c.name)
		}
	}
}

func TestApplyPatchStreamingUnknownSemantics(t *testing.T) {
	cases := []struct {
		name       string
		op         string
		status     string
		wantLosses int
	}{
		{"unknown-operation", `{"type":"future","extra":true}`, "completed", 1},
		{"unknown-status", `{"type":"delete_file","path":"a"}`, "future", 1},
		{"delete-extra-diff-number", `{"type":"delete_file","path":"a","diff":42}`, "completed", 1},
	}
	for _, c := range cases {
		d := NewStreamDecoder()
		_, err := d.FeedJSON([]byte(`{"type":"response.created","response":{"id":"r","model":"m","status":"in_progress","output":[]}}`))
		if err != nil {
			t.Fatalf("%s: response.created error: %v", c.name, err)
		}
		_, err = d.FeedJSON([]byte(fmt.Sprintf(`{"type":"response.output_item.added","output_index":0,"item":{"id":"apc","type":"apply_patch_call","call_id":"c","status":"in_progress","operation":%s}}`, c.op)))
		if err != nil {
			t.Fatalf("%s: output_item.added error: %v", c.name, err)
		}
		events, err := d.FeedJSON([]byte(fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"id":"apc","type":"apply_patch_call","call_id":"c","status":%q,"operation":%s}}`, c.status, c.op)))
		if err != nil {
			t.Fatalf("%s: output_item.done error: %v", c.name, err)
		}
		if len(events) != 0 && (c.name == "unknown-operation" || c.name == "unknown-status") {
			t.Errorf("%s: expected skipped block, got %d events", c.name, len(events))
		}
		_, err = d.FeedJSON([]byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":"r","model":"m","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":%q,"operation":%s}]}}`, c.status, c.op)))
		if err != nil {
			t.Fatalf("%s: response.completed error: %v", c.name, err)
		}
		if _, err := d.Flush(); err != nil {
			t.Fatalf("%s: Flush error: %v", c.name, err)
		}
		if len(d.Losses()) < c.wantLosses {
			t.Errorf("%s: expected at least %d losses, got %d (%+v)", c.name, c.wantLosses, len(d.Losses()), d.Losses())
		}
	}
}

func TestApplyPatchStreamDegradation(t *testing.T) {
	events := []ir.Event{
		ir.MessageStart{ID: "r", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.ApplyPatchToolResultBlock{ToolUseID: "c", Status: ir.ApplyPatchResultCompleted}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopEndTurn},
		ir.MessageDone{},
	}

	ce := cc.NewStreamEncoder()
	for _, ev := range events {
		_, _, err := ce.Apply(ev)
		if err != nil {
			t.Fatalf("cc result degradation failed: %v", err)
		}
	}

	ae := ant.NewStreamEncoder()
	for _, ev := range events {
		_, _, err := ae.Apply(ev)
		if err != nil {
			t.Fatalf("anthropic result degradation failed: %v", err)
		}
	}

	// Any delta on atomic block must fail
	deltaEvents := []ir.Event{
		ir.MessageStart{ID: "r", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.ApplyPatchToolUseBlock{ID: "c", Status: ir.ApplyPatchCallCompleted, Operation: ir.ApplyPatchDeleteFileOperation{Path: "a"}}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.CustomToolInputDelta{Text: json.RawMessage(`""`)}},
	}
	ce2 := cc.NewStreamEncoder()
	var deltaErr error
	for _, ev := range deltaEvents {
		_, _, err := ce2.Apply(ev)
		if err != nil {
			deltaErr = err
			break
		}
	}
	if deltaErr == nil {
		t.Error("expected error on atomic block delta in cc stream, got nil")
	}

	ae2 := ant.NewStreamEncoder()
	deltaErr = nil
	for _, ev := range deltaEvents {
		_, _, err := ae2.Apply(ev)
		if err != nil {
			deltaErr = err
			break
		}
	}
	if deltaErr == nil {
		t.Error("expected error on atomic block delta in anthropic stream, got nil")
	}
}
