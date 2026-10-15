package responses

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func TestDecodeRequestDeveloperTyped(t *testing.T) {
	wire := &Request{
		Model:        "m",
		Instructions: ptr("First.\n"),
		Input: Input{Items: []InputItem{
			{Type: ItemTypeMessage, Role: RoleDeveloper, Content: "Second.\n"},
			{Role: RoleUser, Content: "Hi"},
			{Role: RoleSystem, Content: "Third.\n"},
			{Role: RoleDeveloper, Content: []ContentPart{
				{Type: PartTypeInputText, Text: "Fourth.\n"},
				{Type: PartTypeInputText, Text: "Fifth.\n"},
			}},
		}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatal(err)
	}
	wantSystem := []ir.SystemBlock{{Text: "First.\n"}, {Text: "Second.\n"}, {Text: "Third.\n"}, {Text: "Fourth.\n"}, {Text: "Fifth.\n"}}
	if !reflect.DeepEqual(req.System, wantSystem) {
		t.Fatalf("system = %#v, want %#v", req.System, wantSystem)
	}
	wantMessages := []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}}
	if !reflect.DeepEqual(req.Messages, wantMessages) {
		t.Fatalf("messages = %#v, want %#v", req.Messages, wantMessages)
	}
	if len(losses) != 2 {
		t.Fatalf("losses = %#v, want two developer role degradations", losses)
	}
	for i, path := range []string{"input[0].role", "input[3].role"} {
		if losses[i].Path != path || losses[i].Field != "role" || losses[i].Reason != ir.LossDegraded || !strings.Contains(losses[i].Detail, "developer") {
			t.Fatalf("loss[%d] = %#v, want developer degradation at %s", i, losses[i], path)
		}
	}
	encoded, encodeLosses, err := EncodeRequest(req)
	if err != nil || len(encodeLosses) != 0 {
		t.Fatalf("encode: err=%v losses=%#v", err, encodeLosses)
	}
	if encoded.Instructions == nil || *encoded.Instructions != "First.\nSecond.\nThird.\nFourth.\nFifth.\n" {
		t.Fatalf("encoded instructions = %#v", encoded.Instructions)
	}
}

func TestDecodeRequestDeveloperJSONLossOrder(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"content":[{"type":"input_text","text":"Keep.","extra":false}],"role":"developer","extra":null},{"role":"user","content":"Hi"}],"store":false}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.System) != 1 || req.System[0].Text != "Keep." {
		t.Fatalf("system = %#v", req.System)
	}
	wantPaths := []string{"input[0].content[0].extra", "input[0].role", "input[0].extra", "store"}
	if len(losses) != len(wantPaths) {
		t.Fatalf("losses = %#v, want paths %v", losses, wantPaths)
	}
	for i, path := range wantPaths {
		wantReason := ir.LossUnmappedField
		if i == 1 {
			wantReason = ir.LossDegraded
		}
		if losses[i].Path != path || losses[i].Reason != wantReason {
			t.Fatalf("loss[%d] = %#v, want %s at %s", i, losses[i], wantReason, path)
		}
	}
}

func TestDecodeRequestDeveloperRejectsStructuralErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
	}{
		{"unknown role", `{"model":"m","input":[{"role":"moderator","content":"No"},{"role":"user","content":"Hi"}]}`},
		{"non-string content", `{"model":"m","input":[{"role":"developer","content":42},{"role":"user","content":"Hi"}]}`},
		{"non-string part text", `{"model":"m","input":[{"role":"developer","content":[{"type":"input_text","text":42}]},{"role":"user","content":"Hi"}]}`},
		{"no input or instructions", `{"model":"m","input":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, losses, err := DecodeRequestJSON([]byte(test.raw))
			if err == nil || req != nil || len(losses) != 0 {
				t.Fatalf("request=%#v losses=%#v err=%v, want structural error without partial output", req, losses, err)
			}
		})
	}
}

func TestDecodeRequestDeveloperOnlyTyped(t *testing.T) {
	var wire Request
	if err := json.Unmarshal([]byte(`{"model":"m","input":[{"role":"developer","content":"Answer briefly."}]}`), &wire); err != nil {
		t.Fatal(err)
	}
	req, losses, err := DecodeRequest(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req.System, []ir.SystemBlock{{Text: "Answer briefly."}}) {
		t.Fatalf("system = %#v", req.System)
	}
	wantMessages := []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: ""}}}}
	if !reflect.DeepEqual(req.Messages, wantMessages) {
		t.Fatalf("messages = %#v, want %#v", req.Messages, wantMessages)
	}
	if len(losses) != 2 || losses[0].Reason != ir.LossDegraded || losses[0].Path != "input[0].role" || losses[1].Reason != ir.LossDegraded || losses[1].Path != "input" {
		t.Fatalf("losses = %#v, want role degradation and empty user normalization", losses)
	}
}
