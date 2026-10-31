package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
	"github.com/elkpi/oxa/go/v2/openai/responses"
)

func TestFidelityV2411_RedactedThinking_RoundTrip(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 4096,
		"messages": [
			{"role": "user", "content": "Prior prompt."},
			{
				"role": "assistant",
				"content": [
					{"type": "redacted_thinking", "data": "OPAQUE_ENCRYPTED_DATA"},
					{"type": "text", "text": "Follow up response."}
				]
			},
			{"role": "user", "content": "Continue."}
		]
	}`)

	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON unexpected error: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses, got: %+v", losses)
	}

	asstContent := req.Messages[1].Content
	if len(asstContent) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(asstContent))
	}
	redacted, ok := asstContent[0].(ir.RedactedThinkingBlock)
	if !ok {
		t.Fatalf("expected ir.RedactedThinkingBlock, got %T", asstContent[0])
	}
	if redacted.Data != "OPAQUE_ENCRYPTED_DATA" {
		t.Fatalf("expected data 'OPAQUE_ENCRYPTED_DATA', got %q", redacted.Data)
	}

	encoded, encLosses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest unexpected error: %v", err)
	}
	if len(encLosses) != 0 {
		t.Fatalf("expected 0 encode losses, got: %+v", encLosses)
	}
	encodedJSON, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(encodedJSON), `"type":"redacted_thinking"`) || !strings.Contains(string(encodedJSON), `"data":"OPAQUE_ENCRYPTED_DATA"`) {
		t.Fatalf("encoded JSON missing redacted_thinking: %s", string(encodedJSON))
	}
}

func TestFidelityV2411_BetweenTools_RoundTrip(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-5-5",
		"max_tokens": 4096,
		"thinking": {"type": "between_tools"},
		"messages": [{"role": "user", "content": "Hello"}]
	}`)

	req, losses, err := DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON error: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses, got: %+v", losses)
	}
	var modeAnn *ir.NativeAnnotation
	for _, ann := range req.Native {
		if ann.Kind == ir.NativeKindThinkingMode {
			modeAnn = &ann
			break
		}
	}
	if modeAnn == nil || modeAnn.Value != `"between_tools"` {
		t.Fatalf("expected between_tools native annotation, got %+v", modeAnn)
	}

	encoded, encLosses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	if len(encLosses) != 0 {
		t.Fatalf("expected 0 encode losses, got: %+v", encLosses)
	}
	if encoded.Thinking == nil || encoded.Thinking.Type != "between_tools" {
		t.Fatalf("expected thinking.type between_tools, got %+v", encoded.Thinking)
	}
}

func TestFidelityV2411_BetweenTools_Conflict(t *testing.T) {
	rawBudget := []byte(`{
		"model": "claude-sonnet-5-5",
		"max_tokens": 4096,
		"thinking": {"type": "between_tools", "budget_tokens": 2048},
		"messages": [{"role": "user", "content": "Hello"}]
	}`)
	_, _, err := DecodeRequestJSON(rawBudget)
	if err == nil || !strings.Contains(err.Error(), "between_tools") {
		t.Fatalf("expected conflict error with between_tools and budget_tokens, got: %v", err)
	}

	rawDisplay := []byte(`{
		"model": "claude-sonnet-5-5",
		"max_tokens": 4096,
		"thinking": {"type": "between_tools", "display": "summarized"},
		"messages": [{"role": "user", "content": "Hello"}]
	}`)
	_, _, err = DecodeRequestJSON(rawDisplay)
	if err == nil || !strings.Contains(err.Error(), "between_tools") {
		t.Fatalf("expected conflict error with between_tools and display, got: %v", err)
	}
}

func TestFidelityV2411_TopK_RoundTrip(t *testing.T) {
	for _, kVal := range []int64{0, 40, 100} {
		raw := []byte(`{
			"model": "claude-opus-4-6",
			"max_tokens": 1024,
			"top_k": ` + jsonNumber(kVal) + `,
			"messages": [{"role": "user", "content": "Hello"}]
		}`)
		req, losses, err := DecodeRequestJSON(raw)
		if err != nil {
			t.Fatalf("k=%d: DecodeRequestJSON error: %v", kVal, err)
		}
		if len(losses) != 0 {
			t.Fatalf("k=%d: expected 0 losses, got: %+v", kVal, losses)
		}

		encoded, encLosses, err := EncodeRequest(req)
		if err != nil {
			t.Fatalf("k=%d: EncodeRequest error: %v", kVal, err)
		}
		if len(encLosses) != 0 {
			t.Fatalf("k=%d: expected 0 encode losses, got: %+v", kVal, encLosses)
		}
		if encoded.TopK == nil || *encoded.TopK != kVal {
			t.Fatalf("k=%d: expected encoded TopK=%d, got %v", kVal, kVal, encoded.TopK)
		}
	}
}

func TestFidelityV2411_DisableParallelToolUse_RoundTrip(t *testing.T) {
	for _, flag := range []bool{true, false} {
		flagStr := "true"
		if !flag {
			flagStr = "false"
		}
		raw := []byte(`{
			"model": "claude-sonnet-4-5",
			"max_tokens": 1024,
			"messages": [{"role": "user", "content": "Hello"}],
			"tools": [{"name": "tool1", "input_schema": {"type": "object"}}],
			"tool_choice": {"type": "auto", "disable_parallel_tool_use": ` + flagStr + `}
		}`)
		req, losses, err := DecodeRequestJSON(raw)
		if err != nil {
			t.Fatalf("flag=%v: DecodeRequestJSON error: %v", flag, err)
		}
		if len(losses) != 0 {
			t.Fatalf("flag=%v: expected 0 losses, got: %+v", flag, losses)
		}

		encoded, encLosses, err := EncodeRequest(req)
		if err != nil {
			t.Fatalf("flag=%v: EncodeRequest error: %v", flag, err)
		}
		if len(encLosses) != 0 {
			t.Fatalf("flag=%v: expected 0 encode losses, got: %+v", flag, encLosses)
		}
		if encoded.ToolChoice == nil || encoded.ToolChoice.DisableParallelToolUse != flag {
			t.Fatalf("flag=%v: expected encoded DisableParallelToolUse=%v, got %+v", flag, flag, encoded.ToolChoice)
		}
	}
}

func TestFidelityV2411_DisableParallelToolUse_NoneConflict(t *testing.T) {
	raw := []byte(`{
		"model": "claude-sonnet-4-5",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "Hello"}],
		"tools": [{"name": "tool1", "input_schema": {"type": "object"}}],
		"tool_choice": {"type": "none", "disable_parallel_tool_use": true}
	}`)
	_, _, err := DecodeRequestJSON(raw)
	if err == nil || !strings.Contains(err.Error(), "disable_parallel_tool_use") {
		t.Fatalf("expected error on disable_parallel_tool_use with none choice, got: %v", err)
	}
}

func TestFidelityV2411_TextCitations_RoundTrip(t *testing.T) {
	respRaw := []byte(`{
		"id": "msg_resp_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-6",
		"content": [
			{
				"type": "text",
				"text": "Factual statement.",
				"citations": [
					{
						"type": "char_location",
						"cited_text": "cited",
						"document_index": 0,
						"document_title": "Doc",
						"start_char_index": 0,
						"end_char_index": 5,
						"file_id": "file_1"
					}
				]
			}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 10}
	}`)
	resp, losses, err := DecodeResponseJSON(respRaw)
	if err != nil {
		t.Fatalf("DecodeResponseJSON error: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses, got: %+v", losses)
	}
	txtBlock, ok := resp.Content[0].(ir.TextBlock)
	if !ok {
		t.Fatalf("expected ir.TextBlock, got %T", resp.Content[0])
	}
	if len(txtBlock.Citations) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(txtBlock.Citations))
	}

	encResp, encLosses, err := EncodeResponse(resp)
	if err != nil {
		t.Fatalf("EncodeResponse error: %v", err)
	}
	if len(encLosses) != 0 {
		t.Fatalf("expected 0 encode losses, got: %+v", encLosses)
	}
	encJSON, _ := json.Marshal(encResp)
	if !strings.Contains(string(encJSON), `"char_location"`) || !strings.Contains(string(encJSON), `"file_doc_1"`) && !strings.Contains(string(encJSON), `"file_1"`) {
		t.Fatalf("citations not preserved in encoded response: %s", string(encJSON))
	}
}

func TestAnthropicStream_OpenAICitationAbsorbed(t *testing.T) {
	enc := NewStreamEncoder()
	_, _, err := enc.Apply(ir.MessageStart{ID: "msg_1", Model: "claude-3-7-sonnet-20250219"})
	if err != nil {
		t.Fatal(err)
	}
	cite := ir.TextCitation{
		Source: ir.CitationSourceOpenAI,
		Value:  `{"type":"url_citation","url":"https://example.test"}`,
	}
	events, losses, err := enc.Apply(ir.ContentBlockStart{
		Index: 0,
		Block: ir.TextBlock{Text: "Docs", Citations: []ir.TextCitation{cite}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Field != "citations" || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("expected 1 unsupported-semantic loss, got: %+v", losses)
	}
	if len(events) != 1 || events[0].ContentBlock.Citations != nil {
		t.Fatalf("expected block start without citations, got: %+v", events)
	}

	events, losses, err = enc.Apply(ir.ContentBlockDelta{
		Index: 0,
		Delta: ir.CitationDelta{Citation: cite},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 wire events, got: %+v", events)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses (already reported on block), got: %+v", losses)
	}
}

func TestFidelityV2411_CrossFace_Losses(t *testing.T) {
	redactedReq := &ir.Request{
		Model:  "claude-opus-5-5",
		Params: ir.Params{MaxTokens: int64p(1024)},
		Messages: []ir.Message{
			{
				Role: ir.RoleAssistant,
				Content: []ir.Block{
					ir.RedactedThinkingBlock{Data: "TEST_REDACTED"},
					ir.TextBlock{Text: "Visible text."},
				},
			},
		},
	}

	// Chat Completions encode
	_, ccLosses, err := chatcompletions.EncodeRequest(redactedReq)
	if err != nil {
		t.Fatalf("CC EncodeRequest error: %v", err)
	}
	var foundRedactedCC bool
	for _, l := range ccLosses {
		if l.Field == "type" && l.Reason == ir.LossUnsupportedSemantic && strings.Contains(l.Detail, "encrypted thinking") {
			foundRedactedCC = true
			break
		}
	}
	if !foundRedactedCC {
		t.Fatalf("expected unsupported-semantic loss for redacted_thinking in CC, got: %+v", ccLosses)
	}

	// Responses encode
	_, reLosses, err := responses.EncodeRequest(redactedReq)
	if err != nil {
		t.Fatalf("Responses EncodeRequest error: %v", err)
	}
	var foundRedactedRE bool
	for _, l := range reLosses {
		if l.Field == "type" && l.Reason == ir.LossUnsupportedSemantic && strings.Contains(l.Detail, "encrypted thinking") {
			foundRedactedRE = true
			break
		}
	}
	if !foundRedactedRE {
		t.Fatalf("expected unsupported-semantic loss for redacted_thinking in Responses, got: %+v", reLosses)
	}
}

func TestFidelityV2411_RedactedThinking_DataValidation(t *testing.T) {
	// 1. data: "" empty string is preserved
	rawEmpty := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hi"},
			{"role": "assistant", "content": [{"type": "redacted_thinking", "data": ""}, {"type": "text", "text": "Ok"}]}
		]
	}`)
	req, _, err := DecodeRequestJSON(rawEmpty)
	if err != nil {
		t.Fatalf("unexpected error on empty data string: %v", err)
	}
	encoded, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("unexpected encode error on empty data: %v", err)
	}
	encJSON, _ := json.Marshal(encoded)
	if !strings.Contains(string(encJSON), `"data":""`) {
		t.Fatalf("expected \"data\":\"\" in encoded json, got: %s", string(encJSON))
	}

	// 2. data: null is rejected
	rawNull := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hi"},
			{"role": "assistant", "content": [{"type": "redacted_thinking", "data": null}, {"type": "text", "text": "Ok"}]}
		]
	}`)
	_, _, err = DecodeRequestJSON(rawNull)
	if err == nil {
		t.Fatalf("expected error on data: null, got nil")
	}
}

func TestFidelityV2411_DisableParallel_NullAndConflict(t *testing.T) {
	// null is rejected
	rawNull := []byte(`{
		"model": "claude-sonnet-4-5",
		"max_tokens": 1024,
		"messages": [{"role": "user", "content": "Hello"}],
		"tools": [{"name": "tool1", "input_schema": {"type": "object"}}],
		"tool_choice": {"type": "auto", "disable_parallel_tool_use": null}
	}`)
	_, _, err := DecodeRequestJSON(rawNull)
	if err == nil {
		t.Fatalf("expected error on disable_parallel_tool_use: null, got nil")
	}

	// encode conflict when tool_choice is none
	reqConflict := &ir.Request{
		Model:      "claude-sonnet-4-5",
		Params:     ir.Params{MaxTokens: int64p(1024)},
		ToolChoice: &ir.ToolChoice{Mode: ir.ToolChoiceNone},
		Messages:   []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}},
		Native: []ir.NativeAnnotation{
			{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindDisableParallelToolUse, Value: "true"},
		},
	}
	_, _, err = EncodeRequest(reqConflict)
	if err == nil {
		t.Fatalf("expected error encoding disable_parallel_tool_use with tool_choice none, got nil")
	}
}

func TestFidelityV2411_BetweenTools_EncodeConflict(t *testing.T) {
	reqConflict := &ir.Request{
		Model:    "claude-sonnet-5-5",
		Params:   ir.Params{MaxTokens: int64p(1024)},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}},
		Native: []ir.NativeAnnotation{
			{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindThinkingMode, Value: `"between_tools"`},
			{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindThinkingBudget, Value: "2048"},
		},
	}
	_, _, err := EncodeRequest(reqConflict)
	if err == nil {
		t.Fatalf("expected error encoding between_tools with budget, got nil")
	}
}

func TestFidelityV2411_OmissionLosses_DefaultFallback(t *testing.T) {
	req := &ir.Request{
		Model:    "claude-opus-4-6",
		Params:   ir.Params{MaxTokens: int64p(1024)},
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}},
		Native: []ir.NativeAnnotation{
			{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindTopK, Value: "40"},
			{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindDisableParallelToolUse, Value: "true"},
		},
		ToolChoice: &ir.ToolChoice{Mode: ir.ToolChoiceAuto},
	}
	losses, err := ir.OmittedNativeRequestLosses(req, "chatcompletions")
	if err != nil {
		t.Fatalf("OmittedNativeRequestLosses error: %v", err)
	}
	var hasTopK, hasDPU bool
	for _, l := range losses {
		if l.Field == "top_k" && l.Reason == ir.LossUnmappedField {
			hasTopK = true
		}
		if l.Field == "disable_parallel_tool_use" && l.Reason == ir.LossUnmappedField {
			hasDPU = true
		}
	}
	if !hasTopK || !hasDPU {
		t.Fatalf("expected top_k and disable_parallel_tool_use losses without omission object, got: %+v", losses)
	}
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
