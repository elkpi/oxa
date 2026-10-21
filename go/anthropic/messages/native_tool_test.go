package messages

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
	"github.com/elkpi/oxa/go/v2/openai/responses"
)

func TestNativeToolDeclarationFidelity(t *testing.T) {
	definition := `{ "type" : "advisor_20260301", "name":"advisor", "model":"opus", "max_uses":2, "vendor":{"n":1e+03}, "cache_control":{"type":"ephemeral"} }`
	raw := []byte(`{"model":"m","max_tokens":32,"tools":[` + definition + `],"messages":[{"role":"user","content":"hi"}]}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: losses=%+v err=%v", losses, err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Type != "native" {
		t.Fatalf("tools: %+v", req.Tools)
	}
	encoded, err := ir.MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Tools []struct {
			Source     string `json:"source"`
			Definition string `json:"definition"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document.Tools[0].Source != "anthropic" || document.Tools[0].Definition != definition {
		t.Fatalf("native payload changed: %s", encoded)
	}
	restored, err := ir.UnmarshalRequest(encoded)
	if err != nil {
		t.Fatal(err)
	}
	wire, losses, err := EncodeRequest(restored)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: losses=%+v err=%v", losses, err)
	}
	toolJSON, err := json.Marshal(wire.Tools[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"model":"opus"`, `"n":1e+03`, `"max_uses":2`, `"cache_control":{"type":"ephemeral"}`} {
		if !bytes.Contains(toolJSON, []byte(fragment)) {
			t.Fatalf("missing original configuration %s in %s", fragment, toolJSON)
		}
	}
}

func TestNativeToolMalformedDeclarations(t *testing.T) {
	for _, tool := range []string{
		`{"name":"function"}`,
		`{"name":"function","input_schema":[]}`,
		`{"type":null,"name":"function","input_schema":{}}`,
		`{"type":7,"name":"function","input_schema":{}}`,
		`{"type":"advisor_20260301","name":""}`,
		`{"type":"advisor_20260301","name":7}`,
	} {
		t.Run(tool, func(t *testing.T) {
			_, _, err := DecodeRequestJSON([]byte(`{"model":"m","max_tokens":32,"tools":[` + tool + `],"messages":[{"role":"user","content":"hi"}]}`))
			if err == nil {
				t.Fatalf("invalid tool accepted: %s", tool)
			}
		})
	}
}

func TestNativeToolChoiceWithoutExplicitToolType(t *testing.T) {
	raw := []byte(`{"model":"m","max_tokens":32,"tools":[{"type":"advisor_20260301","name":"advisor","model":"opus"}],"tool_choice":{"type":"tool","name":"advisor"},"messages":[{"role":"user","content":"hi"}]}`)
	req, _, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	req.ToolChoice.ToolType = ""
	cc, ccLosses, err := chatcompletions.EncodeRequest(req)
	if err != nil || cc.ToolChoice != nil || len(cc.Tools) != 0 || len(ccLosses) != 2 {
		t.Fatalf("Chat Completions dangling choice: wire=%+v losses=%+v err=%v", cc, ccLosses, err)
	}
	re, reLosses, err := responses.EncodeRequest(req)
	if err != nil || re.ToolChoice != nil || len(re.Tools) != 0 || len(reLosses) != 2 {
		t.Fatalf("Responses dangling choice: wire=%+v losses=%+v err=%v", re, reLosses, err)
	}
}

func TestNativeToolInvalidIR(t *testing.T) {
	valid := `{"type":"native","name":"advisor","source":"anthropic","definition":"{\"type\":\"advisor_20260301\",\"name\":\"advisor\"}"}`
	for _, tool := range []string{
		strings.Replace(valid, `"source":"anthropic"`, `"source":""`, 1),
		strings.Replace(valid, `\"name\":\"advisor\"`, `\"name\":\"other\"`, 1),
		strings.Replace(valid, `"definition":"{\"type\":\"advisor_20260301\",\"name\":\"advisor\"}"`, `"definition":"[]"`, 1),
		strings.Replace(valid, `"source":"anthropic"`, `"input_schema":{},"source":"anthropic"`, 1),
	} {
		doc := []byte(`{"specVersion":"0.2.0","model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"tools":[` + tool + `]}`)
		if _, err := ir.UnmarshalRequest(doc); err == nil {
			t.Fatalf("invalid native IR accepted: %s", tool)
		}
	}
}
