package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oxa-protocol/oxa/go/ir"
)

func ptr[T any](v T) *T { return &v }

func TestDecodeRequestSystemStringAndBlocks(t *testing.T) {
	wire := &Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 512,
		System:    "You are terse.",
		Messages:  []Message{{Role: "user", Content: "Hi"}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if len(req.System) != 1 || req.System[0].Text != "You are terse." {
		t.Fatalf("system string not mapped: %+v", req.System)
	}
	if req.Params.MaxTokens == nil || *req.Params.MaxTokens != 512 {
		t.Fatalf("max_tokens not mapped: %+v", req.Params)
	}
}

func TestDecodeRequestCacheControlLoss(t *testing.T) {
	wire := &Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 512,
		System: []any{
			map[string]any{
				"type":          "text",
				"text":          "You are a terse assistant.",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
		Messages: []Message{{Role: "user", Content: "Hi"}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.System) != 1 || req.System[0].Text != "You are a terse assistant." {
		t.Fatalf("system not mapped: %+v", req.System)
	}
	if len(losses) != 1 || losses[0].Path != "system[0].cache_control" ||
		losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("losses: %+v", losses)
	}
}

func TestDecodeRequestParams(t *testing.T) {
	wire := &Request{
		Model:         "claude-sonnet-4-5",
		MaxTokens:     300,
		Temperature:   ptr(0.5),
		TopP:          ptr(0.9),
		StopSequences: []string{"\n\n", "END"},
		Messages:      []Message{{Role: "user", Content: "Hello"}},
	}
	req, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Params.Temperature == nil || *req.Params.Temperature != 0.5 ||
		req.Params.TopP == nil || *req.Params.TopP != 0.9 ||
		len(req.Params.StopSequences) != 2 {
		t.Fatalf("params not mapped: %+v", req.Params)
	}
}

func TestEncodeRequestMaxTokensDefault(t *testing.T) {
	req := &ir.Request{
		Model:    "claude-sonnet-4-5",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hello"}}}},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if wire.MaxTokens != defaultMaxTokens {
		t.Fatalf("max_tokens default: %d", wire.MaxTokens)
	}
	if len(losses) != 1 || losses[0].Reason != ir.LossDegraded ||
		losses[0].Field != "max_tokens" {
		t.Fatalf("degraded loss missing: %+v", losses)
	}
	// Single message, single text block, no system: string shorthand.
	b, _ := json.Marshal(wire)
	if want := `"content":"Hello"`; !strings.Contains(string(b), want) {
		t.Fatalf("string shorthand not used: %s", b)
	}
}

func TestEncodeRequestBlockArrayRendering(t *testing.T) {
	// System present: block-array rendering even for a single text block.
	req := &ir.Request{
		Model:  "claude-sonnet-4-5",
		System: []ir.SystemBlock{{Text: "You are a terse assistant."}},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}},
		},
		Params: ir.Params{MaxTokens: ptr(int64(512))},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	b, _ := json.Marshal(wire)
	if strings.Contains(string(b), `"content":"Hi"`) {
		t.Fatalf("string shorthand must not be used with a system prompt: %s", b)
	}
	if !strings.Contains(string(b), `"content":[{"type":"text","text":"Hi"}]`) {
		t.Fatalf("block array not rendered: %s", b)
	}
	// Multiple messages also render block arrays.
	req2 := &ir.Request{
		Model: "claude-sonnet-4-5",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "a"}}},
			{Role: ir.RoleAssistant, Content: []ir.Block{ir.TextBlock{Text: "b"}}},
		},
		Params: ir.Params{MaxTokens: ptr(int64(512))},
	}
	wire2, _, err := EncodeRequest(req2)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b2, _ := json.Marshal(wire2)
	if strings.Contains(string(b2), `"content":"a"`) {
		t.Fatalf("string shorthand must not be used with multiple messages: %s", b2)
	}
}

func TestDecodeResponse(t *testing.T) {
	wire := &Response{
		ID: "msg_01ABCdefGHI", Type: "message", Role: "assistant",
		Model:      "claude-sonnet-4-5",
		Content:    []BlockWire{{Type: "text", Text: "Hello there."}},
		StopReason: "end_turn",
		Usage:      &UsageWire{InputTokens: 9, OutputTokens: 3},
	}
	resp, losses, err := DecodeResponse(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if resp.StopReason != ir.StopEndTurn || resp.ID != wire.ID ||
		resp.Usage.InputTokens != 9 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("response not mapped: %+v", resp)
	}
}

func TestEncodeResponseEnvelopeDefaults(t *testing.T) {
	resp := &ir.Response{
		ID: "msg_01ABCdefGHI", Model: "claude-sonnet-4-5",
		Content:    []ir.Block{ir.TextBlock{Text: "Hello there."}},
		StopReason: ir.StopEndTurn,
		Usage:      ir.Usage{InputTokens: 9, OutputTokens: 3},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if wire.Type != "message" || wire.Role != "assistant" || wire.StopReason != "end_turn" {
		t.Fatalf("envelope wrong: %+v", wire)
	}
	if wire.Usage == nil || wire.Usage.InputTokens != 9 {
		t.Fatalf("usage wrong: %+v", wire.Usage)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	wire := &Request{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "What is 2+2?"},
			}},
			{Role: "assistant", Content: "4"},
			{Role: "user", Content: "And 3+3?"},
		},
		MaxTokens: 1024,
	}
	req, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	back, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if back.MaxTokens != 1024 || len(back.Messages) != 3 {
		t.Fatalf("round trip drifted: %+v", back)
	}
	b, _ := json.Marshal(back)
	if !strings.Contains(string(b), `"content":"And 3+3?"`) && !strings.Contains(string(b), `"And 3+3?"`) {
		t.Fatalf("third message drifted: %s", b)
	}
}
