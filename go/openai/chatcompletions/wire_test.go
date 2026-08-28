package chatcompletions

import (
	"testing"

	"github.com/oxa-protocol/oxa/go/ir"
	"github.com/oxa-protocol/oxa/go/modelmap"
)

func ptr[T any](v T) *T { return &v }

func TestDecodeRequestSystemAndParams(t *testing.T) {
	wire := &Request{
		Model: "gpt-4o-mini",
		Messages: []Message{
			{Role: "system", Content: "You are terse."},
			{Role: "user", Content: "Hi"},
		},
		Temperature: ptr(0.5),
		TopP:        ptr(0.9),
		MaxTokens:   ptr(int64(256)),
		Stop:        []string{"\n\n", "END"},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if len(req.System) != 1 || req.System[0].Text != "You are terse." {
		t.Fatalf("system not mapped: %+v", req.System)
	}
	if req.Params.Temperature == nil || *req.Params.Temperature != 0.5 {
		t.Fatalf("temperature not mapped: %+v", req.Params)
	}
	if len(req.Params.StopSequences) != 2 {
		t.Fatalf("stop not mapped: %+v", req.Params)
	}
}

func TestDecodeRequestLogprobsLosses(t *testing.T) {
	wire := &Request{
		Model:       "gpt-4o-mini",
		Messages:    []Message{{Role: "user", Content: "Hello"}},
		Logprobs:    true,
		TopLogprobs: 5,
	}
	_, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(losses) != 2 {
		t.Fatalf("expected 2 losses, got %+v", losses)
	}
	for _, l := range losses {
		if l.Reason != ir.LossUnmappedField {
			t.Fatalf("wrong reason: %+v", l)
		}
	}
}

func TestDecodeRequestPartsArray(t *testing.T) {
	wire := &Request{Model: "m", Messages: []Message{{
		Role:    "user",
		Content: []any{map[string]any{"type": "text", "text": "a"}, map[string]any{"type": "text", "text": "b"}},
	}}}
	req, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := req.Messages[0].Content
	if len(got) != 2 || got[0].(ir.TextBlock).Text != "a" || got[1].(ir.TextBlock).Text != "b" {
		t.Fatalf("parts not mapped: %+v", got)
	}
}

func TestDecodeResponseUsageAndFinish(t *testing.T) {
	wire := &Response{
		ID: "chatcmpl-abc123", Object: "chat.completion", Created: 1720000000,
		Model: "gpt-4o-mini-2024-07-18",
		Choices: []Choice{{
			Index: 0, Message: Message{Role: "assistant", Content: "Hello there."},
			FinishReason: "stop",
		}},
		Usage: &UsageWire{PromptTokens: 9, CompletionTokens: 3, TotalTokens: 12},
	}
	resp, losses, err := DecodeResponse(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if resp.StopReason != ir.StopEndTurn {
		t.Fatalf("finish_reason: %v", resp.StopReason)
	}
	if resp.Usage.InputTokens != 9 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("usage: %+v", resp.Usage)
	}
}

func TestDecodeResponseUnknownFinishReason(t *testing.T) {
	wire := &Response{
		ID: "id", Model: "m",
		Choices: []Choice{{Message: Message{Role: "assistant", Content: "x"}, FinishReason: "cosmic_ray"}},
	}
	resp, losses, err := DecodeResponse(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StopReason != ir.StopOther {
		t.Fatalf("want other, got %v", resp.StopReason)
	}
	if len(losses) != 1 || losses[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses: %+v", losses)
	}
}

func TestEncodeRequest(t *testing.T) {
	req := &ir.Request{
		Model:  "gpt-4o-mini",
		System: []ir.SystemBlock{{Text: "You are a terse assistant."}},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}},
		},
		Params: ir.Params{MaxTokens: ptr(int64(100))},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if len(wire.Messages) != 2 || wire.Messages[0].Role != "system" ||
		wire.Messages[0].Content != "You are a terse assistant." {
		t.Fatalf("system not rendered: %+v", wire.Messages)
	}
	if wire.MaxTokens == nil || *wire.MaxTokens != 100 {
		t.Fatalf("max_tokens not rendered: %+v", wire)
	}
}

func TestEncodeResponseDefaultsAndDerivedUsage(t *testing.T) {
	resp := &ir.Response{
		ID: "chatcmpl-abc123", Model: "gpt-4o-mini-2024-07-18",
		Content:    []ir.Block{ir.TextBlock{Text: "Hello there."}},
		StopReason: ir.StopEndTurn,
		Usage:      ir.Usage{InputTokens: 9, OutputTokens: 3},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if wire.Object != "chat.completion" || wire.Created != 0 || len(wire.Choices) != 1 {
		t.Fatalf("envelope defaults wrong: %+v", wire)
	}
	if wire.Usage == nil || wire.Usage.TotalTokens != 12 {
		t.Fatalf("total_tokens not derived: %+v", wire.Usage)
	}
}

func TestEncodeResponseStopSequenceLoss(t *testing.T) {
	resp := &ir.Response{
		ID: "id", Model: "m", StopReason: ir.StopSequence, StopSequence: "END",
		Usage: ir.Usage{InputTokens: 1, OutputTokens: 1},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if wire.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason: %q", wire.Choices[0].FinishReason)
	}
	if len(losses) != 1 || losses[0].Field != "stop_sequence" || losses[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses: %+v", losses)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	orig := &Request{
		Model: "gpt-4o-mini",
		Messages: []Message{
			{Role: "system", Content: "s"},
			{Role: "user", Content: "u"},
			{Role: "assistant", Content: "a"},
			{Role: "user", Content: "u2"},
		},
		Temperature: ptr(0.1),
	}
	req, _, err := DecodeRequest(orig)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	back, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if back.Model != orig.Model || back.Temperature == nil || *back.Temperature != 0.1 {
		t.Fatalf("round trip drifted: %+v", back)
	}
	if len(back.Messages) != 4 || back.Messages[0].Role != "system" || back.Messages[3].Content != "u2" {
		t.Fatalf("messages drifted: %+v", back.Messages)
	}
}

func TestMetadataLossesBothDirections(t *testing.T) {
	// face -> IR: wire metadata present is dropped with one unmapped-field loss.
	wire := &Request{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "Hi"}},
		Metadata: map[string]any{"user_id": "u1"},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Metadata != nil {
		t.Fatalf("wire metadata must be dropped: %+v", req.Metadata)
	}
	if len(losses) != 1 || losses[0].Path != "metadata" || losses[0].Field != "metadata" ||
		losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("decode metadata loss wrong: %+v", losses)
	}
	// IR -> face: IR metadata map is dropped with one unmapped-field loss.
	irReq := &ir.Request{
		Model:    "gpt-4o-mini",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}},
		Metadata: map[string]string{"k": "v"},
	}
	out, losses, err := EncodeRequest(irReq)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Metadata != nil {
		t.Fatalf("IR metadata must not be rendered: %+v", out.Metadata)
	}
	if len(losses) != 1 || losses[0].Path != "metadata" || losses[0].Field != "metadata" ||
		losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("encode metadata loss wrong: %+v", losses)
	}
}

func TestWithModelMap(t *testing.T) {
	wire := &Request{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	}
	// face -> IR: model name mapped through the table.
	req, _, err := DecodeRequest(wire, WithModelMap(modelmap.Table{"gpt-4o-mini": "gpt-4o"}))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Model != "gpt-4o" {
		t.Fatalf("decode model not mapped: %q", req.Model)
	}
	// No options: identity passthrough (spec/03 s2).
	reqDefault, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode default: %v", err)
	}
	if reqDefault.Model != "gpt-4o-mini" {
		t.Fatalf("default must be identity: %q", reqDefault.Model)
	}
	// IR -> face: model name mapped back through the table.
	irReq := &ir.Request{
		Model:    "gpt-4o",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}},
	}
	out, _, err := EncodeRequest(irReq, WithModelMap(modelmap.Table{"gpt-4o": "gpt-4o-mini"}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Model != "gpt-4o-mini" {
		t.Fatalf("encode model not mapped: %q", out.Model)
	}
	// The table applies to both response directions too.
	irResp := &ir.Response{ID: "i", Model: "gpt-4o", StopReason: ir.StopEndTurn}
	respOut, _, err := EncodeResponse(irResp, WithModelMap(modelmap.Table{"gpt-4o": "gpt-4o-mini"}))
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	if respOut.Model != "gpt-4o-mini" {
		t.Fatalf("encode response model not mapped: %q", respOut.Model)
	}
	wireResp := &Response{
		ID: "i", Model: "gpt-4o-mini",
		Choices: []Choice{{Message: Message{Role: "assistant", Content: "x"}, FinishReason: "stop"}},
	}
	respIn, _, err := DecodeResponse(wireResp, WithModelMap(modelmap.Table{"gpt-4o-mini": "gpt-4o"}))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if respIn.Model != "gpt-4o" {
		t.Fatalf("decode response model not mapped: %q", respIn.Model)
	}
}
