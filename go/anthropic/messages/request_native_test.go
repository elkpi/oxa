package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func TestNativeThinkingCombinations(t *testing.T) {
	// 1. adaptive + effort=xhigh
	raw1 := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hello"}],
		"thinking": {"type": "adaptive"},
		"output_config": {"effort": "xhigh"}
	}`)
	req1, losses1, err := DecodeRequestJSON(raw1)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(losses1) != 0 {
		t.Fatalf("expected 0 losses, got %+v", losses1)
	}
	if req1.Params.ReasoningEffort != ir.ReasoningEffortXHigh {
		t.Fatalf("expected effort xhigh, got %q", req1.Params.ReasoningEffort)
	}
	encoded1, el1, err := EncodeRequest(req1)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if len(el1) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", el1)
	}
	if encoded1.Thinking == nil || encoded1.Thinking.Type != "adaptive" {
		t.Fatalf("expected thinking type adaptive, got %+v", encoded1.Thinking)
	}

	// 2. enabled, budget=6000
	raw2 := []byte(`{
		"model": "claude-3-7-sonnet-20250219",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hello"}],
		"thinking": {"type": "enabled", "budget_tokens": 6000}
	}`)
	req2, losses2, err := DecodeRequestJSON(raw2)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(losses2) != 0 {
		t.Fatalf("expected 0 decode losses for preserved budget, got %+v", losses2)
	}
	if req2.Params.ReasoningEffort != ir.ReasoningEffortMedium {
		t.Fatalf("expected effort medium, got %q", req2.Params.ReasoningEffort)
	}
	encoded2, el2, err := EncodeRequest(req2)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if len(el2) != 0 {
		t.Fatalf("expected 0 encode losses for exact budget restoration, got %+v", el2)
	}
	if encoded2.Thinking == nil || encoded2.Thinking.BudgetTokens != 6000 {
		t.Fatalf("expected budget 6000, got %+v", encoded2.Thinking)
	}

	// 3. disabled
	raw3 := []byte(`{
		"model": "claude-sonnet-5",
		"max_tokens": 4096,
		"messages": [{"role": "user", "content": "hello"}],
		"thinking": {"type": "disabled"}
	}`)
	req3, losses3, err := DecodeRequestJSON(raw3)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(losses3) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", losses3)
	}
	encoded3, el3, err := EncodeRequest(req3)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if len(el3) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", el3)
	}
	if encoded3.Thinking == nil || encoded3.Thinking.Type != "disabled" {
		t.Fatalf("expected thinking type disabled, got %+v", encoded3.Thinking)
	}

	// 4. adaptive, display=summarized
	raw4 := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hello"}],
		"thinking": {"type": "adaptive", "display": "summarized"}
	}`)
	req4, losses4, err := DecodeRequestJSON(raw4)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(losses4) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", losses4)
	}
	encoded4, _, err := EncodeRequest(req4)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if encoded4.Thinking == nil || encoded4.Thinking.Display != "summarized" {
		t.Fatalf("expected display summarized, got %+v", encoded4.Thinking)
	}
}

func TestNativeEditedEffortWins(t *testing.T) {
	// Case 1: Decode adaptive xhigh -> edit effort to low -> output_config.effort == "low"
	raw1 := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hello"}],
		"thinking": {"type": "adaptive"},
		"output_config": {"effort": "xhigh"}
	}`)
	req1, _, err := DecodeRequestJSON(raw1)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	req1.Params.ReasoningEffort = ir.ReasoningEffortLow
	enc1, _, err := EncodeRequest(req1)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if enc1.OutputConfig == nil || enc1.OutputConfig.Effort != "low" {
		t.Fatalf("expected output_config.effort low, got %+v", enc1.OutputConfig)
	}

	// Case 2: Decode budget 6000 -> edit effort to low -> budget reconstructed as 2048 with degraded loss
	raw2 := []byte(`{
		"model": "claude-3-7-sonnet-20250219",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hello"}],
		"thinking": {"type": "enabled", "budget_tokens": 6000}
	}`)
	req2, _, err := DecodeRequestJSON(raw2)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	req2.Params.ReasoningEffort = ir.ReasoningEffortLow
	enc2, el2, err := EncodeRequest(req2)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if enc2.Thinking == nil || enc2.Thinking.BudgetTokens != 2048 {
		t.Fatalf("expected reconstructed budget 2048, got %+v", enc2.Thinking)
	}
	foundDegraded := false
	for _, l := range el2 {
		if l.Field == "reasoning_effort" && l.Reason == ir.LossDegraded {
			foundDegraded = true
		}
	}
	if !foundDegraded {
		t.Fatalf("expected degraded loss on reconstructed budget, got %+v", el2)
	}
}

func TestNativeUnknownEnumDropsUnit(t *testing.T) {
	// Unknown cache ttl -> breakpoint dropped, unmapped-value loss, not defaulted to 5m
	raw1 := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "hello", "cache_control": {"type": "ephemeral", "ttl": "invalid_ttl"}}
				]
			}
		]
	}`)
	req1, losses1, err := DecodeRequestJSON(raw1)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	foundLoss := false
	for _, l := range losses1 {
		if l.Field == "ttl" && l.Reason == ir.LossUnmappedValue {
			foundLoss = true
		}
	}
	if !foundLoss {
		t.Fatalf("expected unmapped-value loss on ttl, got %+v", losses1)
	}
	enc1, _, err := EncodeRequest(req1)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	msgContent, _ := json.Marshal(enc1.Messages[0].Content)
	if strings.Contains(string(msgContent), "cache_control") {
		t.Fatalf("cache_control should be absent after invalid ttl, got: %s", string(msgContent))
	}

	// Unknown context edit keep -> edit dropped
	raw2 := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hello"}],
		"context_management": {
			"edits": [
				{"type": "clear_thinking_20251015", "keep": "invalid_keep"}
			]
		}
	}`)
	req2, losses2, err := DecodeRequestJSON(raw2)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	foundKeepLoss := false
	for _, l := range losses2 {
		if l.Field == "keep" && l.Reason == ir.LossUnmappedValue {
			foundKeepLoss = true
		}
	}
	if !foundKeepLoss {
		t.Fatalf("expected unmapped-value loss on keep, got %+v", losses2)
	}
	enc2, _, err := EncodeRequest(req2)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if enc2.ContextManagement != nil && len(enc2.ContextManagement.Edits) > 0 {
		t.Fatalf("context_management should have no valid edits, got %+v", enc2.ContextManagement)
	}
}

func TestNativeCachePlacement(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"cache_control": {"type": "ephemeral"},
		"system": [
			{"type": "text", "text": "sys", "cache_control": {"type": "ephemeral"}}
		],
		"tools": [
			{"name": "t1", "input_schema": {"type": "object"}, "cache_control": {"type": "ephemeral"}}
		],
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "single text with cache", "cache_control": {"type": "ephemeral", "ttl": "1h"}}
				]
			}
		]
	}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", losses)
	}
	enc, el, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if len(el) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", el)
	}
	if enc.CacheControl == nil {
		t.Fatalf("expected top-level cache_control preserved")
	}
	// Verify user message content is NOT simplified to a plain string
	if _, isStr := enc.Messages[0].Content.(string); isStr {
		t.Fatalf("content with cache_control must not be simplified to plain string")
	}
}

func TestNativeFormatStillAudited(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hello"}],
		"output_config": {
			"effort": "high",
			"format": {"type": "json_schema", "schema": {"type": "object"}}
		}
	}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if req.Params.ReasoningEffort != ir.ReasoningEffortHigh {
		t.Fatalf("expected effort high, got %q", req.Params.ReasoningEffort)
	}
	foundFormatLoss := false
	for _, l := range losses {
		if l.Field == "format" && l.Reason == ir.LossUnmappedField {
			foundFormatLoss = true
		}
	}
	if !foundFormatLoss {
		t.Fatalf("expected unmapped-field loss on output_config.format, got %+v", losses)
	}
}
