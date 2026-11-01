package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func TestPromptCacheOptionsExplicitNullSurvives(t *testing.T) {
	var wire Request
	if err := json.Unmarshal([]byte(`{"model":"m","input":"hi","prompt_cache_options":null}`), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	req, _, err := DecodeRequest(&wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Native) != 1 || req.Native[0].Kind != ir.NativeKindPromptCacheOptions || req.Native[0].Value != "null" {
		t.Fatalf("expected prompt_cache_options null annotation, got %+v", req.Native)
	}
	out, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"prompt_cache_options":null`) {
		t.Fatalf("expected explicit null preserved, got %s", b)
	}
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
			var wire Request
			if err := json.Unmarshal([]byte(`{"model":"m","input":"hi","prompt_cache_options":`+tc.value+`}`), &wire); err != nil {
				return
			}
			if _, _, err := DecodeRequest(&wire); err == nil {
				t.Fatalf("expected decode error for prompt_cache_options %s", tc.value)
			}
		})
	}
}

func TestPromptCacheBreakpointValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"unknown mode", `{"mode":"bad"}`, true},
		{"unknown ttl", `{"ttl":"wrong"}`, true},
		{"non-string mode", `{"mode":1}`, true},
		{"empty object allowed", `{}`, false},
		{"ttl without mode allowed", `{"ttl":"30m"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"hi","prompt_cache_breakpoint":` + tc.value + `}]}]}`
			var wire Request
			if err := json.Unmarshal([]byte(raw), &wire); err != nil {
				if !tc.wantErr {
					t.Fatalf("unexpected unmarshal error: %v", err)
				}
				return
			}
			_, _, err := DecodeRequest(&wire)
			if tc.wantErr && err == nil {
				t.Fatalf("expected decode error for prompt_cache_breakpoint %s", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected decode error: %v", err)
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
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"prompt_cache_breakpoint"`) {
		t.Fatalf("expected prompt_cache_breakpoint restored on image part, got %s", b)
	}
}

func TestSystemBreakpointEmitsItemsNotInstructions(t *testing.T) {
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
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, `"instructions"`) {
		t.Fatalf("system breakpoint must not collapse into instructions, got %s", s)
	}
	if !strings.Contains(s, `"prompt_cache_breakpoint"`) {
		t.Fatalf("expected breakpoint on system input item, got %s", s)
	}
}
