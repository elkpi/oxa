package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func TestOutputFormatSchemaRawBytes(t *testing.T) {
	schema := `{ "type" : "object", "properties" : { "n" : {"type":"string"} } }`
	raw := []byte(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":` + schema + `}}}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: %v %+v", err, losses)
	}
	if string(req.Params.ResponseFormat.Schema) != schema {
		t.Fatalf("schema bytes: %q", req.Params.ResponseFormat.Schema)
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: %v %+v", err, losses)
	}
	if !strings.Contains(string(wire.OutputConfig.Format), schema) {
		t.Fatalf("schema bytes not preserved: %s", wire.OutputConfig.Format)
	}
	encoded, encodeLosses, err := EncodeRequestJSON(req)
	if err != nil || len(encodeLosses) != 0 || !strings.Contains(string(encoded), schema) {
		t.Fatalf("serialized schema bytes not preserved: %s, %v, %+v", encoded, err, encodeLosses)
	}
	canonical, err := ir.MarshalRequest(req)
	if err != nil || !strings.Contains(string(canonical), schema) {
		t.Fatalf("canonical schema bytes not preserved: %s, %v", canonical, err)
	}
	back, err := ir.UnmarshalRequest(canonical)
	if err != nil || string(back.Params.ResponseFormat.Schema) != schema {
		t.Fatalf("canonical round-trip schema not preserved: %+v, %v", back, err)
	}
}

func TestOutputFormatUnknownNestedField(t *testing.T) {
	raw := []byte(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","extra":false,"schema":{"extension":true}}}}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil || req.Params.ResponseFormat == nil || len(losses) != 1 || losses[0].Path != "output_config.format.extra" || losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("decode: %v %+v", err, losses)
	}
}

func TestOutputConfigContainerTypes(t *testing.T) {
	prefix := `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":`
	if req, losses, err := DecodeRequestJSON([]byte(prefix + `null}`)); err != nil || len(losses) != 0 || req.Params.ResponseFormat != nil {
		t.Fatalf("null output_config: request=%+v losses=%+v err=%v", req, losses, err)
	}
	for _, value := range []string{`[]`, `42`, `"invalid"`, `false`} {
		if _, _, err := DecodeRequestJSON([]byte(prefix + value + `}`)); err == nil {
			t.Errorf("expected error for output_config %s", value)
		}
	}
}

func TestOutputFormatInvalidSchema(t *testing.T) {
	for _, format := range []string{`null`, `{}`, `{"type":"json_schema"}`, `{"type":"json_schema","schema":null}`, `{"type":"json_schema","schema":[]}`} {
		raw := []byte(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":` + format + `}}`)
		if _, _, err := DecodeRequestJSON(raw); err == nil {
			t.Errorf("expected error for %s", format)
		}
	}
}

func TestOutputFormatForeignFieldsLoss(t *testing.T) {
	strict := false
	maxTokens := int64(64)
	req := &ir.Request{Model: "claude", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}}, Params: ir.Params{MaxTokens: &maxTokens, ResponseFormat: &ir.ResponseFormat{Type: ir.ResponseFormatJSONSchema, Name: "name", Description: "desc", Strict: &strict, Schema: json.RawMessage(`{}`)}}}
	_, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 3 {
		t.Fatalf("encode: %v %+v", err, losses)
	}
}
