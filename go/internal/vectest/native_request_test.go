package vectest

import (
	"testing"

	"github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
	"github.com/elkpi/oxa/go/v2/openai/responses"
)

func TestNativeRequestThreeRoutes(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 64000,
		"cache_control": {"type": "ephemeral"},
		"messages": [{"role": "user", "content": "Analyze complex codebase."}],
		"thinking": {"type": "adaptive"},
		"output_config": {"effort": "xhigh"},
		"context_management": {
			"edits": [{"type": "clear_thinking_20251015", "keep": "all"}]
		}
	}`)

	req, decodeLosses, err := messages.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(decodeLosses) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", decodeLosses)
	}

	// 1. Encode to ChatCompletions
	ccOut, ccLosses, err := chatcompletions.EncodeRequest(req)
	if err != nil {
		t.Fatalf("chatcompletions.EncodeRequest failed: %v", err)
	}
	if ccOut.ReasoningEffort != "xhigh" {
		t.Fatalf("expected cc ReasoningEffort xhigh, got %q", ccOut.ReasoningEffort)
	}
	if len(ccLosses) != 3 {
		t.Fatalf("expected 3 cc omission losses, got %+v", ccLosses)
	}

	// 2. Encode to Responses
	respOut, respLosses, err := responses.EncodeRequest(req)
	if err != nil {
		t.Fatalf("responses.EncodeRequest failed: %v", err)
	}
	rMap, ok := respOut.Reasoning.(map[string]any)
	if !ok || rMap["effort"] != "xhigh" {
		t.Fatalf("expected responses reasoning effort xhigh, got %+v", respOut.Reasoning)
	}
	if len(respLosses) != 3 {
		t.Fatalf("expected 3 responses omission losses, got %+v", respLosses)
	}

	// 3. Encode to Anthropic (verifying native options were preserved and not mutated by prior encodes!)
	anOut, anLosses, err := messages.EncodeRequest(req)
	if err != nil {
		t.Fatalf("messages.EncodeRequest failed: %v", err)
	}
	if len(anLosses) != 0 {
		t.Fatalf("expected 0 anthropic encode losses, got %+v", anLosses)
	}
	if anOut.Thinking == nil || anOut.Thinking.Type != "adaptive" {
		t.Fatalf("expected thinking adaptive, got %+v", anOut.Thinking)
	}
	if anOut.OutputConfig == nil || anOut.OutputConfig.Effort != "xhigh" {
		t.Fatalf("expected output_config effort xhigh, got %+v", anOut.OutputConfig)
	}
	if anOut.CacheControl == nil {
		t.Fatalf("expected cache_control preserved")
	}
	if anOut.ContextManagement == nil {
		t.Fatalf("expected context_management preserved")
	}
}

func TestNativeDecodeEncodeLossStages(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 64000,
		"metadata": {"user_id": "u1"},
		"messages": [{"role": "user", "content": "hi"}],
		"cache_control": {"type": "ephemeral"}
	}`)

	req, decLosses, err := messages.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decLosses) != 1 || decLosses[0].Field != "metadata" {
		t.Fatalf("expected decode loss on metadata only, got %+v", decLosses)
	}

	_, encLosses, err := chatcompletions.EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(encLosses) != 1 || encLosses[0].Field != "cache_control" || encLosses[0].Path != "native[0]" {
		t.Fatalf("expected encode loss on native[0] cache_control, got %+v", encLosses)
	}
}

func TestNativeEffortNewValues(t *testing.T) {
	ccRaw := []byte(`{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "hi"}],
		"reasoning_effort": "max"
	}`)
	ccReq, ccLosses, err := chatcompletions.DecodeRequestJSON(ccRaw)
	if err != nil {
		t.Fatalf("cc decode failed: %v", err)
	}
	if len(ccLosses) != 0 {
		t.Fatalf("expected 0 cc losses for max effort, got %+v", ccLosses)
	}
	if ccReq.Params.ReasoningEffort != "max" {
		t.Fatalf("expected cc effort max, got %q", ccReq.Params.ReasoningEffort)
	}

	respRaw := []byte(`{
		"model": "gpt-4o",
		"input": "hi",
		"reasoning": {"effort": "xhigh"}
	}`)
	respReq, respLosses, err := responses.DecodeRequestJSON(respRaw)
	if err != nil {
		t.Fatalf("responses decode failed: %v", err)
	}
	if len(respLosses) != 0 {
		t.Fatalf("expected 0 responses losses for xhigh effort, got %+v", respLosses)
	}
	if respReq.Params.ReasoningEffort != "xhigh" {
		t.Fatalf("expected responses effort xhigh, got %q", respReq.Params.ReasoningEffort)
	}
}

func TestNativeCacheControlOmissionLossDetails(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 64000,
		"system": [{"type": "text", "text": "S", "cache_control": {"type": "ephemeral"}}],
		"tools": [{"name": "f", "description": "d", "input_schema": {"type": "object"}, "cache_control": {"type": "ephemeral"}}],
		"messages": [{"role": "user", "content": [{"type": "text", "text": "go", "cache_control": {"type": "ephemeral"}}]}]
	}`)
	req, decodeLosses, err := messages.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(decodeLosses) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", decodeLosses)
	}

	_, ccLosses, err := chatcompletions.EncodeRequest(req)
	if err != nil {
		t.Fatalf("chatcompletions.EncodeRequest failed: %v", err)
	}
	if len(ccLosses) != 3 {
		t.Fatalf("expected 3 cc omission losses, got %+v", ccLosses)
	}
	for i, l := range ccLosses {
		if l.Field != "cache_control" || l.Reason != ir.LossUnmappedField {
			t.Errorf("ccLosses[%d] = (field=%q, reason=%q), want (cache_control, unmapped-field)", i, l.Field, l.Reason)
		}
		if l.Detail != ir.CacheControlLossDetail {
			t.Errorf("ccLosses[%d].Detail = %q, want %q", i, l.Detail, ir.CacheControlLossDetail)
		}
	}

	_, respLosses, err := responses.EncodeRequest(req)
	if err != nil {
		t.Fatalf("responses.EncodeRequest failed: %v", err)
	}
	if len(respLosses) != 3 {
		t.Fatalf("expected 3 responses omission losses, got %+v", respLosses)
	}
	for i, l := range respLosses {
		if l.Detail != ir.CacheControlLossDetail {
			t.Errorf("respLosses[%d].Detail = %q, want %q", i, l.Detail, ir.CacheControlLossDetail)
		}
	}

	reqNoOmission := &ir.Request{
		Model: "claude-opus-5-5",
		Native: []ir.NativeAnnotation{
			{
				Source: ir.NativeSourceAnthropic,
				Kind:   ir.NativeKindCacheControl,
				Value:  `{"type":"ephemeral"}`,
			},
		},
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.Block{ir.TextBlock{Text: "hi"}},
			},
		},
	}
	_, ccLossesNoOmission, err := chatcompletions.EncodeRequest(reqNoOmission)
	if err != nil {
		t.Fatalf("chatcompletions.EncodeRequest failed: %v", err)
	}
	if len(ccLossesNoOmission) != 1 {
		t.Fatalf("expected 1 cc loss for cache_control without omission, got %+v", ccLossesNoOmission)
	}
	if ccLossesNoOmission[0].Field != "cache_control" || ccLossesNoOmission[0].Reason != ir.LossUnmappedField || ccLossesNoOmission[0].Detail != ir.CacheControlLossDetail {
		t.Errorf("got cc loss %+v, want detail %q", ccLossesNoOmission[0], ir.CacheControlLossDetail)
	}

	reqEmptyDetail := &ir.Request{
		Model: "claude-opus-5-5",
		Native: []ir.NativeAnnotation{
			{
				Source:   ir.NativeSourceAnthropic,
				Kind:     ir.NativeKindCacheControl,
				Value:    `{"type":"ephemeral"}`,
				Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: ""},
			},
		},
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.Block{ir.TextBlock{Text: "hi"}},
			},
		},
	}
	_, ccLossesEmptyDetail, err := chatcompletions.EncodeRequest(reqEmptyDetail)
	if err != nil {
		t.Fatalf("chatcompletions.EncodeRequest failed: %v", err)
	}
	if len(ccLossesEmptyDetail) != 1 {
		t.Fatalf("expected 1 cc loss, got %+v", ccLossesEmptyDetail)
	}
	if ccLossesEmptyDetail[0].Detail != ir.CacheControlLossDetail {
		t.Errorf("got cc loss %+v, want detail %q", ccLossesEmptyDetail[0], ir.CacheControlLossDetail)
	}
}
