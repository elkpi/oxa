package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func TestMidConversationSystem_ValidStringAndBlocks(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"system": [{"type": "text", "text": "Base system."}],
		"messages": [
			{"role": "user", "content": "Hello."},
			{"role": "assistant", "content": "Hi there."},
			{"role": "user", "content": "Summarize."},
			{"role": "system", "content": "Terse mode enabled."},
			{"role": "system", "content": [{"type": "text", "text": "Second instruction.", "cache_control": {"type": "ephemeral"}}]}
		]
	}`)

	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 conversation messages, got %d", len(req.Messages))
	}
	if len(req.System) != 3 {
		t.Fatalf("expected 3 system blocks, got %d", len(req.System))
	}
	if req.System[0].Text != "Base system." {
		t.Errorf("expected System[0] 'Base system.', got %q", req.System[0].Text)
	}
	if req.System[1].Text != "Terse mode enabled." {
		t.Errorf("expected System[1] 'Terse mode enabled.', got %q", req.System[1].Text)
	}
	if req.System[2].Text != "Second instruction." {
		t.Errorf("expected System[2] 'Second instruction.', got %q", req.System[2].Text)
	}
	if len(req.System[2].Native) != 1 || req.System[2].Native[0].Kind != ir.NativeKindCacheControl {
		t.Errorf("expected cache_control native annotation on System[2], got %+v", req.System[2].Native)
	}

	if len(losses) != 2 {
		t.Fatalf("expected 2 degraded losses, got %d (%+v)", len(losses), losses)
	}
	expectedLosses := []struct {
		path   string
		field  string
		reason ir.LossReason
	}{
		{"messages[3].role", "role", ir.LossDegraded},
		{"messages[4].role", "role", ir.LossDegraded},
	}
	for i, exp := range expectedLosses {
		if losses[i].Path != exp.path || losses[i].Field != exp.field || losses[i].Reason != exp.reason {
			t.Errorf("loss[%d] mismatch: got %+v, expected %+v", i, losses[i], exp)
		}
		if !strings.Contains(losses[i].Detail, "carried as request-level system content") {
			t.Errorf("loss[%d] unexpected detail: %q", i, losses[i].Detail)
		}
	}
}

func TestMidConversationSystem_PlacementErrors(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		errContains string
	}{
		{
			name: "system at index 0",
			raw: `{
				"model": "claude-opus-5-5",
				"max_tokens": 1024,
				"messages": [
					{"role": "system", "content": "Initial system."},
					{"role": "user", "content": "Hi."}
				]
			}`,
			errContains: "messages[0]: system message cannot appear at messages[0]",
		},
		{
			name: "system followed by user",
			raw: `{
				"model": "claude-opus-5-5",
				"max_tokens": 1024,
				"messages": [
					{"role": "user", "content": "Turn 1."},
					{"role": "system", "content": "Mid system."},
					{"role": "user", "content": "Turn 2."}
				]
			}`,
			errContains: "messages[1]: system message must be followed by an assistant turn or be the last message",
		},
		{
			name: "system preceded by plain assistant without server tool",
			raw: `{
				"model": "claude-opus-5-5",
				"max_tokens": 1024,
				"messages": [
					{"role": "user", "content": "Turn 1."},
					{"role": "assistant", "content": "Assistant answer."},
					{"role": "system", "content": "Mid system."}
				]
			}`,
			errContains: "messages[2]: system message must follow a user turn",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := DecodeRequestJSON([]byte(tc.raw))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errContains)
			}
			if !strings.Contains(err.Error(), tc.errContains) {
				t.Fatalf("expected error containing %q, got %q", tc.errContains, err.Error())
			}
		})
	}
}

func TestMidConversationSystem_LossOrderRoleBeforeContent(t *testing.T) {
	wire := &Request{
		Model:     "claude-opus-5-5",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: RoleUser, Content: "Hi"},
			{Role: RoleSystem, Content: []BlockWire{{
				Type:         BlockTypeText,
				Text:         "x",
				CacheControl: json.RawMessage(`{"type":"ephemeral","ttl":"10m"}`),
			}}},
		},
	}
	_, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("DecodeRequest failed: %v", err)
	}
	if len(losses) != 2 {
		t.Fatalf("expected 2 losses, got %d (%+v)", len(losses), losses)
	}
	if losses[0].Path != "messages[1].role" || losses[0].Reason != ir.LossDegraded {
		t.Errorf("expected role loss first, got %+v", losses[0])
	}
	if losses[1].Path != "messages[1].content[0].cache_control.ttl" || losses[1].Reason != ir.LossUnmappedValue {
		t.Errorf("expected ttl loss second, got %+v", losses[1])
	}
}

func TestMidConversationSystem_ContentErrors(t *testing.T) {
	raw := `{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello."},
			{"role": "system", "content": [
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "abc"}}
			]}
		]
	}`
	_, _, err := DecodeRequestJSON([]byte(raw))
	if err == nil {
		t.Fatalf("expected error for non-text block in system message, got nil")
	}
	if !strings.Contains(err.Error(), "messages[1]: system content must be text") {
		t.Fatalf("expected error containing 'system content must be text', got %q", err.Error())
	}
}
