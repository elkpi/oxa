package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func documentRequest(content any) *Request {
	return &Request{
		Model: "claude-opus-5-5", MaxTokens: 512,
		Messages: []Message{{Role: "user", Content: content}},
	}
}

func TestDocumentSourceValidation(t *testing.T) {
	t.Run("missing source discriminator", func(t *testing.T) {
		raw := []byte(`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document","source":{}}]}]}`)
		_, _, err := DecodeRequestJSON(raw)
		if err == nil || !strings.Contains(err.Error(), "source.type is required") {
			t.Fatalf("want source type required error, got %v", err)
		}
	})

	t.Run("explicit empty conflicting field", func(t *testing.T) {
		raw := []byte(`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"YQ==","url":""}}]}]}`)
		_, _, err := DecodeRequestJSON(raw)
		if err == nil || !strings.Contains(err.Error(), "conflicting source fields") {
			t.Fatalf("want conflicting source field error, got %v", err)
		}
	})

	t.Run("null citations is a structural error", func(t *testing.T) {
		_, _, err := DecodeRequest(documentRequest([]BlockWire{{
			Type:      BlockTypeDocument,
			Source:    &SourceWire{Type: SourceTypeFile, FileID: "file_1"},
			Citations: json.RawMessage(`null`),
		}}))
		if err == nil || !strings.Contains(err.Error(), "citations must be a JSON object") {
			t.Fatalf("want citations object error, got %v", err)
		}
	})
}

func TestDocumentSourceConflictLoss(t *testing.T) {
	req := &ir.Request{
		Model: "m", Params: ir.Params{MaxTokens: int64p(1)},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{
			ir.TextBlock{Text: "keep"},
			ir.DocumentBlock{URL: "https://example.com/a.pdf", FileID: "f"},
		}}},
	}
	_, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(losses) != 1 || losses[0].Field != "document" || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("want one whole-block document loss, got %+v", losses)
	}
}

func TestNativeDocumentValueTypeValidation(t *testing.T) {
	for _, tc := range []struct {
		kind  string
		value string
	}{
		{"document_title", `null`},
		{"document_title", `123`},
		{"document_context", `null`},
		{"document_citations", `null`},
		{"document_citations", `[]`},
	} {
		req := &ir.Request{
			Model: "m", Params: ir.Params{MaxTokens: int64p(1)},
			Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{
				ir.DocumentBlock{
					FileID: "f",
					Native: []ir.NativeAnnotation{{Source: "anthropic", Kind: tc.kind, Value: tc.value}},
				},
			}}},
		}
		_, _, err := EncodeRequest(req)
		if err == nil {
			t.Fatalf("%s value %s: want validation error", tc.kind, tc.value)
		}
	}
}

func int64p(v int64) *int64 { return &v }

func TestEmptyDocumentMetadataRoundTrip(t *testing.T) {
	raw := []byte(`{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"file","file_id":"f"},"title":"","context":""}]}]}`)
	req, _, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	wire, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"title":""`) || !strings.Contains(string(encoded), `"context":""`) {
		t.Fatalf("empty document metadata was lost: %s", encoded)
	}

	decoded, _, err := DecodeRequestJSON(encoded)
	if err != nil {
		t.Fatalf("decode round trip: %v", err)
	}
	block, ok := decoded.Messages[0].Content[0].(ir.DocumentBlock)
	if !ok || len(block.Native) != 2 {
		t.Fatalf("empty document annotations were lost: %+v", decoded.Messages[0].Content)
	}
}
