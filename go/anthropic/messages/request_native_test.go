package messages

import (
	"encoding/json"
	"fmt"
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

func TestNativeFormatAndEffort(t *testing.T) {
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
	if req.Params.ResponseFormat == nil || req.Params.ResponseFormat.Type != ir.ResponseFormatJSONSchema || len(losses) != 0 {
		t.Fatalf("expected lossless JSON Schema format, got %+v, losses %+v", req.Params.ResponseFormat, losses)
	}
}

func TestExplicitEffortOnly(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"output_config": {"effort": "low"}
	}`)
	req, dl, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(dl) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", dl)
	}
	wire, el, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(el) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", el)
	}
	if wire.Thinking != nil {
		t.Fatalf("expected nil thinking for explicit effort only, got %+v", wire.Thinking)
	}
	if wire.OutputConfig == nil || wire.OutputConfig.Effort != "low" {
		t.Fatalf("expected output_config.effort low, got %+v", wire.OutputConfig)
	}
}

func TestContextManagementRecursiveAudit(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"context_management": {
			"edits": [
				{
					"type": "clear_tool_uses_20250919",
					"future_policy": true,
					"trigger": {
						"type": "input_tokens",
						"value": 30000,
						"future_nested": false
					}
				}
			]
		}
	}`)
	req, dl, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	var foundPolicy, foundNested bool
	for _, l := range dl {
		if l.Field == "future_policy" && l.Reason == ir.LossUnmappedField {
			foundPolicy = true
		}
		if l.Field == "future_nested" && l.Reason == ir.LossUnmappedField {
			foundNested = true
		}
	}
	if !foundPolicy || !foundNested {
		t.Fatalf("expected unmapped-field losses on unknown keys, got %+v", dl)
	}
	wire, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if wire.ContextManagement == nil || len(wire.ContextManagement.Edits) != 1 {
		t.Fatalf("expected 1 valid edit in encoded wire")
	}
	var editObj map[string]any
	_ = json.Unmarshal(wire.ContextManagement.Edits[0], &editObj)
	if _, ok := editObj["future_policy"]; ok {
		t.Fatalf("unknown field future_policy must be stripped from wire output: %+v", editObj)
	}
	if trig, ok := editObj["trigger"].(map[string]any); ok {
		if _, ok := trig["future_nested"]; ok {
			t.Fatalf("unknown field future_nested must be stripped from wire output: %+v", trig)
		}
	}
}

func TestFilteredCacheAttachment(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "mystery_block"},
					{"type": "text", "text": "keep", "cache_control": {"type": "ephemeral", "ttl": "1h"}}
				]
			}
		]
	}`)
	req, _, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(req.Messages[0].Content) != 1 {
		t.Fatalf("expected 1 surviving block, got %d", len(req.Messages[0].Content))
	}
	tb, ok := req.Messages[0].Content[0].(ir.TextBlock)
	if !ok || len(tb.Native) != 1 || tb.Native[0].Kind != ir.NativeKindCacheControl {
		t.Fatalf("expected cache_control native on surviving text block, got %+v", req.Messages[0].Content[0])
	}
	wire, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	blocks, ok := wire.Messages[0].Content.([]BlockWire)
	if !ok || len(blocks) != 1 || len(blocks[0].CacheControl) == 0 {
		t.Fatalf("expected cache_control preserved on wire block: %+v", wire.Messages[0].Content)
	}
}

func TestFilteredToolCacheAttachment(t *testing.T) {
	max := int64(16000)
	req := &ir.Request{
		Model: "claude-opus-5-5",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}},
		},
		Params: ir.Params{MaxTokens: &max},
		Tools: []ir.Tool{
			{Type: ir.ToolTypeApplyPatch},
			{
				Name:        "search",
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Native: []ir.NativeAnnotation{
					{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindCacheControl,
						Value:    `{"type":"ephemeral"}`,
						Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField},
					},
				},
			},
		},
	}
	wire, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(wire.Tools) != 1 || wire.Tools[0].Name != "search" {
		t.Fatalf("expected 1 surviving tool 'search', got %+v", wire.Tools)
	}
	if len(wire.Tools[0].CacheControl) == 0 {
		t.Fatalf("expected cache_control preserved on tool 'search'")
	}
}

func TestBudgetClearedLoss(t *testing.T) {
	raw := []byte(`{
		"model": "claude-3-7-sonnet-20250219",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"thinking": {"type": "enabled", "budget_tokens": 6000}
	}`)
	req, _, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	req.Params.ReasoningEffort = ""
	wire, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if wire.Thinking != nil {
		t.Fatalf("expected nil thinking when effort cleared, got %+v", wire.Thinking)
	}
	var foundUnsupported bool
	for _, l := range losses {
		if l.Field == "thinking" && l.Reason == ir.LossUnsupportedSemantic {
			foundUnsupported = true
		}
	}
	if !foundUnsupported {
		t.Fatalf("expected thinking unsupported-semantic loss when effort cleared, got %+v", losses)
	}
}

func TestNonPositiveBudgetRejection(t *testing.T) {
	for _, budget := range []string{`0`, `-1`, `-100`} {
		raw := []byte(fmt.Sprintf(`{
			"model": "claude-3-7-sonnet-20250219",
			"max_tokens": 16000,
			"messages": [{"role": "user", "content": "hi"}],
			"thinking": {"type": "enabled", "budget_tokens": %s}
		}`, budget))
		_, _, err := DecodeRequestJSON(raw)
		if err == nil {
			t.Fatalf("expected error for budget_tokens %s, got nil", budget)
		}
	}
	missingRaw := []byte(`{
		"model": "claude-3-7-sonnet-20250219",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"thinking": {"type": "enabled"}
	}`)
	_, _, err := DecodeRequestJSON(missingRaw)
	if err == nil {
		t.Fatalf("expected error for missing budget_tokens in enabled thinking, got nil")
	}
}

func TestResponseNativeIsolation(t *testing.T) {
	raw := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-opus-5-5",
		"content": [
			{"type": "text", "text": "hello", "cache_control": {"type": "ephemeral"}}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 10}
	}`)
	resp, losses, err := DecodeResponseJSON(raw)
	if err != nil {
		t.Fatalf("DecodeResponseJSON failed: %v", err)
	}
	tb, ok := resp.Content[0].(ir.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", resp.Content[0])
	}
	if len(tb.Native) != 0 {
		t.Fatalf("response block must not have native annotations, got %+v", tb.Native)
	}
	// Verify it passes MarshalResponse cleanly
	if _, err := ir.MarshalResponse(resp); err != nil {
		t.Fatalf("MarshalResponse failed: %v", err)
	}
	var foundUnmapped bool
	for _, l := range losses {
		if l.Field == "cache_control" && l.Reason == ir.LossUnmappedField {
			foundUnmapped = true
		}
	}
	if !foundUnmapped {
		t.Fatalf("expected unmapped-field on response cache_control, got %+v", losses)
	}
}

func TestCustomToolResultNativeNested(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"tools": [
			{
				"name": "exec",
				"input_schema": {
					"type": "object",
					"properties": {"input": {"type": "string"}},
					"required": ["input"]
				}
			}
		],
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "c1", "name": "exec", "input": {"input": "echo"}}]},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "c1",
						"content": [
							{"type": "text", "text": "out", "cache_control": {"type": "ephemeral"}}
						]
					}
				]
			}
		]
	}`)
	req, _, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	marshaled, err := ir.MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest failed on custom tool result with nested native: %v", err)
	}
	unmarshaled, err := ir.UnmarshalRequest(marshaled)
	if err != nil {
		t.Fatalf("UnmarshalRequest failed: %v", err)
	}
	ctr, ok := unmarshaled.Messages[2].Content[0].(ir.CustomToolResultBlock)
	if !ok {
		t.Fatalf("expected CustomToolResultBlock, got %T", unmarshaled.Messages[2].Content[0])
	}
	innerTb, ok := ctr.Output.Content[0].(ir.TextBlock)
	if !ok || len(innerTb.Native) == 0 {
		t.Fatalf("expected native annotation preserved on inner text block in custom tool result")
	}
	wire, _, err := EncodeRequest(unmarshaled)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	// Check wire output has cache_control on the tool result content
	userMsg := wire.Messages[2]
	blocks, ok := userMsg.Content.([]BlockWire)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected BlockWire in message content, got %T", userMsg.Content)
	}
	toolRes := blocks[0]
	innerBlocks, ok := toolRes.Content.([]BlockWire)
	if !ok || len(innerBlocks) != 1 || len(innerBlocks[0].CacheControl) == 0 {
		t.Fatalf("expected cache_control on encoded inner block: %+v", toolRes.Content)
	}
}

func TestSystemBlockCacheControlRoundTrip(t *testing.T) {
	raw := []byte(`{
		"model": "test-model",
		"max_tokens": 64,
		"system": [
			{
				"type": "text",
				"text": "synthetic",
				"cache_control": {
					"type": "ephemeral",
					"ttl": "1h"
				}
			}
		],
		"messages": [
			{"role": "user", "content": "hi"}
		]
	}`)

	req, decodeLosses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decodeLosses) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", decodeLosses)
	}
	if len(req.System) != 1 || len(req.System[0].Native) != 1 {
		t.Fatalf("expected 1 native annotation on system[0], got %+v", req.System)
	}

	wire, encodeLosses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(encodeLosses) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", encodeLosses)
	}
	systemBlocks, ok := wire.System.([]SystemBlockWire)
	if !ok {
		t.Fatalf("expected []SystemBlockWire in wire.System, got %T", wire.System)
	}
	if len(systemBlocks) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(systemBlocks))
	}
	var cc0 map[string]any
	if err := json.Unmarshal(systemBlocks[0].CacheControl, &cc0); err != nil {
		t.Fatalf("failed to unmarshal cache_control: %v", err)
	}
	if cc0["type"] != "ephemeral" || cc0["ttl"] != "1h" {
		t.Fatalf("expected type=ephemeral, ttl=1h, got %+v", cc0)
	}
}

func TestSystemBlockMultipleCacheControl(t *testing.T) {
	raw := []byte(`{
		"model": "test-model",
		"max_tokens": 64,
		"system": [
			{
				"type": "text",
				"text": "block 0 (no cache)"
			},
			{
				"type": "text",
				"text": "block 1 (ephemeral 5m)",
				"cache_control": {
					"type": "ephemeral",
					"ttl": "5m"
				}
			},
			{
				"type": "text",
				"text": "block 2 (ephemeral 1h)",
				"cache_control": {
					"type": "ephemeral",
					"ttl": "1h"
				}
			},
			{
				"type": "text",
				"text": "block 3 (ephemeral no ttl)",
				"cache_control": {
					"type": "ephemeral"
				}
			}
		],
		"messages": [
			{"role": "user", "content": "hi"}
		]
	}`)

	req, decodeLosses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decodeLosses) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", decodeLosses)
	}

	wire, encodeLosses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(encodeLosses) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", encodeLosses)
	}
	systemBlocks, ok := wire.System.([]SystemBlockWire)
	if !ok || len(systemBlocks) != 4 {
		t.Fatalf("expected 4 system blocks, got %+v", wire.System)
	}
	if len(systemBlocks[0].CacheControl) != 0 {
		t.Errorf("block 0: expected empty cache_control, got %s", systemBlocks[0].CacheControl)
	}
	var cc1, cc2, cc3 map[string]any
	if err := json.Unmarshal(systemBlocks[1].CacheControl, &cc1); err != nil {
		t.Errorf("block 1: failed to unmarshal: %v", err)
	} else if cc1["type"] != "ephemeral" || cc1["ttl"] != "5m" {
		t.Errorf("block 1: expected type=ephemeral, ttl=5m, got %+v", cc1)
	}
	if err := json.Unmarshal(systemBlocks[2].CacheControl, &cc2); err != nil {
		t.Errorf("block 2: failed to unmarshal: %v", err)
	} else if cc2["type"] != "ephemeral" || cc2["ttl"] != "1h" {
		t.Errorf("block 2: expected type=ephemeral, ttl=1h, got %+v", cc2)
	}
	if err := json.Unmarshal(systemBlocks[3].CacheControl, &cc3); err != nil {
		t.Errorf("block 3: failed to unmarshal: %v", err)
	} else if cc3["type"] != "ephemeral" || cc3["ttl"] != nil {
		t.Errorf("block 3: expected type=ephemeral without ttl, got %+v", cc3)
	}
}

func TestSystemBlockDirectIRCacheControl(t *testing.T) {
	maxTokens := int64(100)
	req := &ir.Request{
		Model: "claude-sonnet-4-5",
		System: []ir.SystemBlock{
			{
				Text: "system instructions",
				Native: []ir.NativeAnnotation{
					{
						Source: ir.NativeSourceAnthropic,
						Kind:   ir.NativeKindCacheControl,
						Value:  `{"type":"ephemeral"}`,
					},
				},
			},
		},
		Messages: []ir.Message{
			{
				Role: ir.RoleUser,
				Content: []ir.Block{
					ir.TextBlock{Text: "hello"},
				},
			},
		},
		Params: ir.Params{
			MaxTokens: &maxTokens,
		},
	}

	wire, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses, got %+v", losses)
	}
	systemBlocks, ok := wire.System.([]SystemBlockWire)
	if !ok || len(systemBlocks) != 1 {
		t.Fatalf("expected 1 system block, got %+v", wire.System)
	}
	if string(systemBlocks[0].CacheControl) != `{"type":"ephemeral"}` {
		t.Fatalf("expected cache_control %q, got %q", `{"type":"ephemeral"}`, string(systemBlocks[0].CacheControl))
	}
}

func TestNativeToolDeferLoading(t *testing.T) {
	// 1. defer_loading: true round-trip with zero decode losses
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [
			{"name": "t1", "description": "tool 1", "input_schema": {"type": "object"}, "defer_loading": true}
		]
	}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", losses)
	}
	if len(req.Tools) != 1 || len(req.Tools[0].Native) != 1 {
		t.Fatalf("expected tool native annotation, got %+v", req.Tools)
	}
	ann := req.Tools[0].Native[0]
	if ann.Kind != ir.NativeKindDeferLoading || ann.Value != "true" || ann.Omission == nil || ann.Omission.Reason != ir.LossDegraded {
		t.Fatalf("unexpected annotation %+v", ann)
	}
	enc, el, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}
	if len(el) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", el)
	}
	if len(enc.Tools) != 1 || enc.Tools[0].DeferLoading == nil || !*enc.Tools[0].DeferLoading {
		t.Fatalf("expected tool defer_loading: true on wire, got %+v", enc.Tools)
	}

	// 2. defer_loading: false produces no annotation and no losses
	rawFalse := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [
			{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": false}
		]
	}`)
	reqFalse, lossesFalse, err := DecodeRequestJSON(rawFalse)
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed: %v", err)
	}
	if len(lossesFalse) != 0 {
		t.Fatalf("expected 0 losses for defer_loading: false, got %+v", lossesFalse)
	}
	if len(reqFalse.Tools[0].Native) != 0 {
		t.Fatalf("expected 0 native annotations for defer_loading: false, got %+v", reqFalse.Tools[0].Native)
	}

	// 3. conflict: defer_loading: true AND cache_control returns error
	rawConflict := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [
			{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": true, "cache_control": {"type": "ephemeral"}}
		]
	}`)
	_, _, err = DecodeRequestJSON(rawConflict)
	if err == nil || !strings.Contains(err.Error(), "cannot specify both defer_loading and cache_control") {
		t.Fatalf("expected conflict error, got %v", err)
	}

	// 4. non-boolean defer_loading returns error
	rawNonBool := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [
			{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": "true"}
		]
	}`)
	_, _, err = DecodeRequestJSON(rawNonBool)
	if err == nil || !strings.Contains(err.Error(), "tools[0].defer_loading must be a boolean") {
		t.Fatalf("expected boolean type error, got %v", err)
	}

	// 5. whitespace boolean annotation round-trips
	maxTokens := int64(16000)
	reqWhitespace := &ir.Request{
		Model:    "claude-opus-5-5",
		Params:   ir.Params{MaxTokens: &maxTokens},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}},
		Tools: []ir.Tool{
			{
				Name:        "t1",
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Native: []ir.NativeAnnotation{
					{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindDeferLoading, Value: " true \n"},
				},
			},
		},
	}
	if err := ir.ValidateNativeRequest(reqWhitespace); err != nil {
		t.Fatalf("validate whitespace boolean failed: %v", err)
	}
	encWhitespace, elWhitespace, err := EncodeRequest(reqWhitespace)
	if err != nil || len(elWhitespace) != 0 {
		t.Fatalf("encode whitespace boolean failed: %v, losses: %+v", err, elWhitespace)
	}
	if len(encWhitespace.Tools) != 1 || encWhitespace.Tools[0].DeferLoading == nil || !*encWhitespace.Tools[0].DeferLoading {
		t.Fatalf("expected defer_loading: true on wire for whitespace annotation, got %+v", encWhitespace.Tools)
	}

	// 6. direct IR conflict: Tool with both cache_control and defer_loading
	reqDirectConflict := &ir.Request{
		Model:    "claude-opus-5-5",
		Params:   ir.Params{MaxTokens: &maxTokens},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}},
		Tools: []ir.Tool{
			{
				Name:        "t1",
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Native: []ir.NativeAnnotation{
					{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindDeferLoading, Value: "true"},
					{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindCacheControl, Value: `{"type":"ephemeral"}`},
				},
			},
		},
	}
	if err := ir.ValidateNativeRequest(reqDirectConflict); err == nil || !strings.Contains(err.Error(), "cannot specify both defer_loading and cache_control") {
		t.Fatalf("expected direct IR validation conflict error, got %v", err)
	}
	if _, _, err := EncodeRequest(reqDirectConflict); err == nil || !strings.Contains(err.Error(), "cannot specify both defer_loading and cache_control") {
		t.Fatalf("expected direct IR encode conflict error, got %v", err)
	}

	// 7. native tool declaration with non-boolean defer_loading is preserved opaquely
	rawNative := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [
			{"type": "future_20990101", "name": "future", "defer_loading": {"future": true}}
		]
	}`)
	reqNative, lossesNative, err := DecodeRequestJSON(rawNative)
	if err != nil || len(lossesNative) != 0 {
		t.Fatalf("native tool decode with arbitrary defer_loading failed: %v, losses: %+v", err, lossesNative)
	}
	if len(reqNative.Tools) != 1 || reqNative.Tools[0].Type != ir.ToolTypeNative {
		t.Fatalf("expected native tool, got %+v", reqNative.Tools)
	}

	// 8. cache_control: null + defer_loading: true returns conflict error
	rawNullCache := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 16000,
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [
			{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": true, "cache_control": null}
		]
	}`)
	if _, _, err := DecodeRequestJSON(rawNullCache); err == nil || !strings.Contains(err.Error(), "cannot specify both defer_loading and cache_control") {
		t.Fatalf("expected conflict error with null cache_control, got %v", err)
	}
}

func TestNativeToolEagerInputStreaming(t *testing.T) {
	// 1. 32 tools carrying eager_input_streaming: true produce NO loss amplification
	var toolsJSON []byte
	toolsJSON = append(toolsJSON, `[`...)
	for i := 0; i < 32; i++ {
		if i > 0 {
			toolsJSON = append(toolsJSON, `,`...)
		}
		toolsJSON = append(toolsJSON, fmt.Sprintf(`{"name":"tool_%d","description":"tool %d","input_schema":{"type":"object"},"eager_input_streaming":true}`, i, i)...)
	}
	toolsJSON = append(toolsJSON, `]`...)

	raw32 := fmt.Sprintf(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "hello"}],
		"tools": %s
	}`, string(toolsJSON))

	req32, losses32, err := DecodeRequestJSON([]byte(raw32))
	if err != nil {
		t.Fatalf("DecodeRequestJSON failed for 32 tools: %v", err)
	}
	if len(losses32) != 0 {
		t.Fatalf("expected 0 losses for 32 tools with eager_input_streaming, got %d losses: %+v", len(losses32), losses32)
	}
	if len(req32.Tools) != 32 {
		t.Fatalf("expected 32 tools, got %d", len(req32.Tools))
	}
	for i, tool := range req32.Tools {
		if len(tool.Native) != 1 || tool.Native[0].Kind != ir.NativeKindEagerInputStreaming || tool.Native[0].Value != "true" {
			t.Fatalf("tools[%d] missing expected eager_input_streaming annotation: %+v", i, tool.Native)
		}
	}

	// 2. Non-boolean eager_input_streaming returns structural error
	for _, badVal := range []string{`"yes"`, `1`, `null`, `{"nested":true}`} {
		rawBad := fmt.Sprintf(`{
			"model": "claude-opus-5-5",
			"max_tokens": 1024,
			"messages": [{"role": "user", "content": "hello"}],
			"tools": [
				{"name": "t0", "input_schema": {"type": "object"}},
				{"name": "t1", "input_schema": {"type": "object"}, "eager_input_streaming": %s}
			]
		}`, badVal)
		if _, _, err := DecodeRequestJSON([]byte(rawBad)); err == nil || !strings.Contains(err.Error(), "tools[1].eager_input_streaming must be a boolean") {
			t.Fatalf("expected structural error with index 1 for eager_input_streaming: %s, got: %v", badVal, err)
		}
	}

	// 3. Coexistence with cache_control and defer_loading (orthogonal)
	rawCoexist := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "hello"}],
		"tools": [
			{"name": "t1", "input_schema": {"type": "object"}, "eager_input_streaming": true, "cache_control": {"type": "ephemeral"}}
		]
	}`)
	reqCoexist, lossesCoexist, err := DecodeRequestJSON(rawCoexist)
	if err != nil || len(lossesCoexist) != 0 {
		t.Fatalf("expected valid coexistence with cache_control, got err=%v, losses=%+v", err, lossesCoexist)
	}
	if len(reqCoexist.Tools[0].Native) != 2 {
		t.Fatalf("expected 2 native annotations on tool, got %d", len(reqCoexist.Tools[0].Native))
	}

	// 4. Foreign OpenAI native options do not leak into Anthropic request
	maxTok := int64(100)
	reqForeign := &ir.Request{
		Model:    "claude-opus-5-5",
		Params:   ir.Params{MaxTokens: &maxTok},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}},
		Native: []ir.NativeAnnotation{
			{Source: "openai", Kind: "thinking_mode", Value: `"disabled"`},
			{Source: "openai", Kind: "context_management", Value: `{"edits":[{"type":"clear_tool_uses_20250919"}]}`},
		},
	}
	encForeign, lossesForeign, err := EncodeRequest(reqForeign)
	if err != nil {
		t.Fatalf("EncodeRequest failed for foreign native: %v", err)
	}
	if encForeign.Thinking != nil {
		t.Fatalf("foreign openai thinking_mode leaked into Anthropic wire: %+v", encForeign.Thinking)
	}
	if encForeign.ContextManagement != nil {
		t.Fatalf("foreign openai context_management leaked into Anthropic wire: %+v", encForeign.ContextManagement)
	}
	if len(lossesForeign) != 2 {
		t.Fatalf("expected 2 losses for 2 unsupported foreign native annotations, got %d: %+v", len(lossesForeign), lossesForeign)
	}
}
