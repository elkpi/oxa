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

// An Anthropic explicit type=custom label with a real JSON Schema is an
// ordinary user tool: the schema must survive as a function tool instead of
// being promoted to a free-text custom tool (spec/16 N-NT-2).
func TestExplicitCustomLabelKeepsInputSchema(t *testing.T) {
	raw := []byte(`{"model":"m","max_tokens":32,"tools":[{"type":"custom","name":"lookup","description":"Look up a city.","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"messages":[{"role":"user","content":"hi"}]}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: losses=%+v err=%v", losses, err)
	}
	tool := req.Tools[0]
	if tool.Type == ir.ToolTypeCustom || len(tool.InputSchema) == 0 {
		t.Fatalf("explicit custom label must keep the JSON Schema, got %+v", tool)
	}
	wire, encodeLosses, err := EncodeRequest(req)
	if err != nil || len(encodeLosses) != 0 {
		t.Fatalf("encode: losses=%+v err=%v", encodeLosses, err)
	}
	toolJSON, err := json.Marshal(wire.Tools[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"city"`, `"required":["city"]`} {
		if !bytes.Contains(toolJSON, []byte(fragment)) {
			t.Fatalf("missing schema fragment %s in %s", fragment, toolJSON)
		}
	}
}

// Raw source replay is scoped to native declarations: ordinary ToolWire values
// must re-marshal from their exported fields so caller-side edits stick
// (spec/16 N-NT-2).
func TestOrdinaryToolWireEditsAreHonored(t *testing.T) {
	var tool ToolWire
	if err := json.Unmarshal([]byte(`{"name":"lookup","description":"original","input_schema":{"type":"object"}}`), &tool); err != nil {
		t.Fatal(err)
	}
	tool.Name = "renamed"
	tool.Description = "updated"
	tool.InputSchema = json.RawMessage(`{"type":"object","required":["city"]}`)
	rendered, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rendered, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["name"] != "renamed" || decoded["description"] != "updated" {
		t.Fatalf("edits were overwritten by raw replay: %s", rendered)
	}
	if schema, ok := decoded["input_schema"].(map[string]any); !ok || len(schema) == 0 || schema["required"] == nil {
		t.Fatalf("edited input_schema was overwritten by raw replay: %s", rendered)
	}
}
