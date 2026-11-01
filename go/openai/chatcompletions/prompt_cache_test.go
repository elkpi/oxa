package chatcompletions

import (
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func unmarshalRequest(t *testing.T, raw string) (*Request, error) {
	t.Helper()
	var wire Request
	err := wire.UnmarshalJSON([]byte(raw))
	if err != nil {
		return nil, err
	}
	return &wire, nil
}

func TestPromptCacheOptionsRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"unknown mode", `{"mode":"bad"}`},
		{"unknown ttl", `{"ttl":"1h"}`},
		{"non-string mode", `{"mode":123}`},
		{"non-string ttl", `{"ttl":30}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"model":"m","messages":[{"role":"user","content":"hi"}],"prompt_cache_options":` + tc.value + `}`
			wire, err := unmarshalRequest(t, raw)
			if err != nil {
				return
			}
			if _, _, err := DecodeRequest(wire); err == nil {
				t.Fatalf("expected decode error for prompt_cache_options %s", tc.value)
			}
		})
	}
}

func TestPromptCacheOptionsTypedFieldFallback(t *testing.T) {
	wire := &Request{
		Model:              "m",
		PromptCacheOptions: map[string]any{"mode": "explicit"},
	}
	wire.Messages = []Message{{Role: RoleUser, Content: "hi"}}
	req, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Native) != 1 || req.Native[0].Kind != ir.NativeKindPromptCacheOptions {
		t.Fatalf("expected prompt_cache_options annotation from typed field, got %+v", req.Native)
	}
}

func TestPromptCacheBreakpointValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"explicit only", `{"mode":"bad"}`, true},
		{"unknown ttl", `{"ttl":"wrong"}`, true},
		{"non-string mode", `{"mode":1}`, true},
		{"empty object allowed", `{}`, false},
		{"ttl without mode allowed", `{"ttl":"30m"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi","prompt_cache_breakpoint":` + tc.value + `}]}]}`
			_, err := unmarshalRequest(t, raw)
			if tc.wantErr {
				// shape-level errors surface at unmarshal for non-objects; value
				// errors surface at decode.
				if err == nil {
					wire, uerr := unmarshalRequest(t, raw)
					if uerr != nil {
						t.Fatalf("unmarshal: %v", uerr)
					}
					if _, _, derr := DecodeRequest(wire); derr == nil {
						t.Fatalf("expected decode error for prompt_cache_breakpoint %s", tc.value)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}
		})
	}
}

func TestEncodeRestoresImageBreakpoint(t *testing.T) {
	req := &ir.Request{
		Model: "gpt-4o",
		Messages: []ir.Message{{
			Role: ir.RoleUser,
			Content: []ir.Block{ir.ImageBlock{
				URL: "https://example.com/cat.png",
				Native: []ir.NativeAnnotation{{
					Source: ir.NativeSourceOpenAI,
					Kind:   ir.NativeKindPromptCacheBreakpoint,
					Value:  `{"mode":"explicit"}`,
				}},
			}},
		}},
	}
	out, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	content := out.Messages[0].Content
	parts, ok := content.([]ContentPart)
	if !ok || len(parts) != 1 {
		t.Fatalf("expected parts array, got %#v", content)
	}
	if len(parts[0].PromptCacheBreakpoint) == 0 {
		t.Fatalf("expected prompt_cache_breakpoint restored on image part")
	}
}

func TestEncodeSystemBreakpointEmitsParts(t *testing.T) {
	req := &ir.Request{
		Model: "gpt-4o",
		System: []ir.SystemBlock{{Text: "policy", Native: []ir.NativeAnnotation{{
			Source: ir.NativeSourceOpenAI,
			Kind:   ir.NativeKindPromptCacheBreakpoint,
			Value:  `{"mode":"explicit"}`,
		}}}},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}},
	}
	out, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected no losses, got %+v", losses)
	}
	if content, ok := out.Messages[0].Content.([]ContentPart); !ok || len(content) != 1 || len(content[0].PromptCacheBreakpoint) == 0 {
		t.Fatalf("expected system content parts with breakpoint, got %#v", out.Messages[0].Content)
	}
}
