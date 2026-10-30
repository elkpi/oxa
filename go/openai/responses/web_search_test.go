package responses

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func TestWebSearchHistoryOfficialActions(t *testing.T) {
	valid := []string{
		`{"type":"search","query":"q","sources":[{"type":"url","url":"https://example.com"}]}`,
		`{"type":"open_page","url":null}`,
		`{"type":"find_in_page","url":"https://example.com","pattern":"q"}`,
	}
	for _, action := range valid {
		data := []byte(`{"model":"m","input":[{"type":"web_search_call","id":"ws","status":"completed","action":` + action + `},{"role":"assistant","content":"answer"},{"role":"user","content":"continue"}]}`)
		req, losses, err := DecodeRequestJSON(data)
		if err != nil || len(losses) != 0 {
			t.Fatalf("action %s: %v %+v", action, err, losses)
		}
		if req.Messages[0].Role != ir.RoleAssistant {
			t.Fatalf("expected initial assistant history: %+v", req.Messages)
		}
		if _, _, err := EncodeRequest(req); err != nil {
			t.Fatalf("encode %s: %v", action, err)
		}
	}
}

func TestWebSearchRetainsUnknownTopLevelFields(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"role":"user","content":"hi"},{"type":"web_search_call","id":"ws","status":"completed","action":{"type":"search"},"role":true}]}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	block, ok := req.Messages[1].Content[0].(ir.WebSearchCallBlock)
	if !ok || string(block.Extra["role"]) != `true` {
		t.Fatalf("block: %+v", req.Messages[1].Content[0])
	}
	if len(losses) != 1 || losses[0].Path != "input[1].role" {
		t.Fatalf("losses: %+v", losses)
	}
}

func TestWebSearchStreamDropContinues(t *testing.T) {
	e := NewStreamEncoder()
	events := []ir.Event{
		ir.MessageStart{ID: "resp", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.WebSearchCallBlock{ID: "ws", Status: "completed", Action: json.RawMessage(`{"type":"search"}`)}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{Text: "ok"}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopEndTurn},
		ir.MessageDone{},
	}
	var losses []ir.Loss
	for _, event := range events {
		_, emitted, err := e.Apply(event)
		if err != nil {
			t.Fatalf("Apply(%T): %v", event, err)
		}
		losses = append(losses, emitted...)
	}
	if len(losses) != 1 || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("losses: %+v", losses)
	}
}

func TestWebSearchHistoryRejectsInvalidActions(t *testing.T) {
	for _, action := range []string{
		`null`, `{}`, `{"type":"future"}`,
		`{"type":"find_in_page","url":"https://example.com"}`,
		`{"type":"find_in_page","url":null,"pattern":"q"}`,
		`{"type":"search","query":null}`,
		`{"type":"search","queries":null}`,
		`{"type":"search","sources":[{"type":"url"}]}`,
	} {
		_, _, err := ir.InspectWebSearchCall("ws", "completed", json.RawMessage(action), "input[0].action")
		if err == nil {
			t.Errorf("action %s: expected structural error", action)
		}
	}
}
