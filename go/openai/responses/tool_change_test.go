package responses

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa/go/v3/ir"
)

func TestCompactionProducesOneExplicitLoss(t *testing.T) {
	req := &ir.Request{Model: "m", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.CompactionBlock{NullContent: true}, ir.TextBlock{Text: "hi"}}}}}
	_, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Path != "messages[0].content[0]" || losses[0].Field != "type" || losses[0].Reason != ir.LossUnsupportedSemantic || losses[0].Detail != "Anthropic compaction block cannot be represented on the target face" {
		t.Fatalf("losses=%+v", losses)
	}
}

func TestEmptyAdditionalToolsRoundTrips(t *testing.T) {
	input := []byte(`{"model":"m","input":[{"type":"additional_tools","role":"developer","id":"empty","tools":[]},{"role":"user","content":"hi"}]}`)
	req, losses, err := DecodeRequestJSON(input)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%+v", err, losses)
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Input []map[string]json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Input) != 2 || string(doc.Input[0]["tools"]) != "[]" || string(doc.Input[0]["id"]) != `"empty"` {
		t.Fatalf("empty item not preserved: %s", raw)
	}
}

func TestEncodeRejectsOutOfRangeToolChangePosition(t *testing.T) {
	req := &ir.Request{Model: "m", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}}, ToolChangeGroups: []ir.ToolChangeGroup{{MessageIndex: 9, ContentIndex: -1, Parts: []ir.ToolChangePart{}}}}
	if _, _, err := EncodeRequest(req); err == nil {
		t.Fatal("expected structural error for out-of-range position")
	}
}

func TestAdditionalToolsBetweenCallsKeepsPositionAndID(t *testing.T) {
	input := []byte(`{"model":"m","input":[{"role":"user","content":"go"},{"type":"function_call","name":"a","call_id":"a","arguments":"{}"},{"type":"additional_tools","role":"developer","id":"tools_1","future":true,"tools":[{"type":"function","name":"b","parameters":{"type":"object"}}]},{"type":"function_call","name":"b","call_id":"b","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":"a"},{"type":"function_call_output","call_id":"b","output":"b"}]}`)
	req, losses, err := DecodeRequestJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 0 || len(req.Tools) != 0 || len(req.ToolChangeGroups) != 1 {
		t.Fatalf("groups=%+v tools=%+v losses=%+v", req.ToolChangeGroups, req.Tools, losses)
	}
	g := req.ToolChangeGroups[0]
	if g.MessageIndex != 1 || g.ContentIndex != 1 || g.ID != "tools_1" || string(g.Extra["future"]) != "true" {
		t.Fatalf("unexpected group: %+v", g)
	}
	raw, err := ir.MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	output, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode err=%v losses=%+v", err, losses)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Input []map[string]json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Input) < 4 || string(wire.Input[2]["type"]) != `"additional_tools"` || string(wire.Input[2]["id"]) != `"tools_1"` || string(wire.Input[2]["future"]) != "true" {
		t.Fatalf("additional_tools not preserved: %s", encoded)
	}
}
