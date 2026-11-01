package ir

import (
	"encoding/json"
	"testing"
)

func TestNativePortableRoundTrip(t *testing.T) {
	req := &Request{
		Model: "test-model",
		Native: []NativeAnnotation{
			{
				Source: "anthropic",
				Kind:   "thinking_mode",
				Value:  `"adaptive"`,
				Omission: &NativeOmission{
					Field:  "type",
					Reason: LossUnmappedField,
				},
			},
			{
				Source: "anthropic",
				Kind:   "context_management",
				Value:  `{"edits":[{"keep":"all","type":"clear_thinking_20251015"}]}`,
				Omission: &NativeOmission{
					Field:  "context_management",
					Reason: LossUnsupportedSemantic,
				},
			},
		},
		System: []SystemBlock{
			{
				Text: "System prompt",
				Native: []NativeAnnotation{
					{
						Source: "anthropic",
						Kind:   "cache_control",
						Value:  `{"type":"ephemeral"}`,
						Omission: &NativeOmission{
							Field:  "cache_control",
							Reason: LossUnmappedField,
						},
					},
				},
			},
		},
		Messages: []Message{
			{
				Role: RoleUser,
				Content: []Block{
					TextBlock{
						Text: "Hello",
						Native: []NativeAnnotation{
							{
								Source: "anthropic",
								Kind:   "cache_control",
								Value:  `{"ttl":"1h","type":"ephemeral"}`,
								Omission: &NativeOmission{
									Field:  "cache_control",
									Reason: LossUnmappedField,
								},
							},
						},
					},
				},
			},
		},
		Tools: []Tool{
			{
				Name:        "lookup",
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Native: []NativeAnnotation{
					{
						Source: "anthropic",
						Kind:   "cache_control",
						Value:  `{"type":"ephemeral"}`,
						Omission: &NativeOmission{
							Field:  "cache_control",
							Reason: LossUnmappedField,
						},
					},
				},
			},
		},
		Params: Params{
			ReasoningEffort: ReasoningEffortXHigh,
		},
	}

	bytes, err := MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest failed: %v", err)
	}

	readBack, err := UnmarshalRequest(bytes)
	if err != nil {
		t.Fatalf("UnmarshalRequest failed: %v", err)
	}

	if len(readBack.Native) != 2 || readBack.Native[0].Value != `"adaptive"` {
		t.Fatalf("request native[0].value mismatch: got %v", readBack.Native)
	}
	if len(readBack.System) != 1 || len(readBack.System[0].Native) != 1 || readBack.System[0].Native[0].Kind != "cache_control" {
		t.Fatalf("system native mismatch: got %v", readBack.System)
	}
	contentBlock, ok := readBack.Messages[0].Content[0].(TextBlock)
	if !ok || len(contentBlock.Native) != 1 || contentBlock.Native[0].Kind != "cache_control" {
		t.Fatalf("content block native mismatch: got %v", readBack.Messages[0].Content)
	}
	if len(readBack.Tools) != 1 || len(readBack.Tools[0].Native) != 1 || readBack.Tools[0].Native[0].Kind != "cache_control" {
		t.Fatalf("tools native mismatch: got %v", readBack.Tools)
	}
	if readBack.Params.ReasoningEffort != ReasoningEffortXHigh {
		t.Fatalf("reasoning effort mismatch: got %s", readBack.Params.ReasoningEffort)
	}
}

func TestNativeIRValidation(t *testing.T) {
	// Duplicate known kind on same host -> error
	dupReq := &Request{
		Model: "m",
		Native: []NativeAnnotation{
			{Source: "anthropic", Kind: "thinking_mode", Value: `"adaptive"`},
			{Source: "anthropic", Kind: "thinking_mode", Value: `"enabled"`},
		},
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}},
	}
	if err := ValidateNativeRequest(dupReq); err == nil {
		t.Fatalf("ValidateNativeRequest(duplicate_known_kind) expected error, got nil")
	}

	// Malformed JSON value -> error
	badJSONReq := &Request{
		Model: "m",
		Native: []NativeAnnotation{
			{Source: "anthropic", Kind: "cache_control", Value: `{not valid json}`},
		},
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}},
	}
	if err := ValidateNativeRequest(badJSONReq); err == nil {
		t.Fatalf("ValidateNativeRequest(bad_json) expected error, got nil")
	}

	// Known kind on unauthorized host (e.g. thinking_mode on SystemBlock) -> error
	badHostReq := &Request{
		Model: "m",
		System: []SystemBlock{
			{
				Text: "sys",
				Native: []NativeAnnotation{
					{Source: "anthropic", Kind: "thinking_mode", Value: `"adaptive"`},
				},
			},
		},
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}},
	}
	if err := ValidateNativeRequest(badHostReq); err == nil {
		t.Fatalf("ValidateNativeRequest(bad_host) expected error, got nil")
	}
}

func TestNativeOmissionDoesNotMutateRequest(t *testing.T) {
	req := &Request{
		Model: "m",
		Native: []NativeAnnotation{
			{
				Source: "future_provider",
				Kind:   "future_feature",
				Value:  `{"foo":"bar"}`,
			},
		},
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}},
	}

	losses, err := OmittedNativeRequestLosses(req, "responses")
	if err != nil {
		t.Fatalf("OmittedNativeRequestLosses failed: %v", err)
	}
	if len(losses) != 1 || losses[0].Reason != LossUnsupportedSemantic || losses[0].Field != "source" {
		t.Fatalf("expected one unsupported-semantic loss for unknown source, got %+v", losses)
	}

	// Verify request was not mutated
	if len(req.Native) != 1 || req.Native[0].Source != "future_provider" {
		t.Fatalf("request was mutated by OmittedNativeRequestLosses")
	}
}

func TestNativeProjectionOmissions(t *testing.T) {
	// Case 1: valid unedited budget projection -> exactly one degraded budget_tokens loss, no duplicate type loss
	req1 := &Request{
		Model: "m",
		Params: Params{
			ReasoningEffort: ReasoningEffortMedium,
		},
		Native: []NativeAnnotation{
			{
				Source:   "anthropic",
				Kind:     "thinking_mode",
				Value:    `"enabled"`,
				Omission: &NativeOmission{Field: "type", Reason: LossUnmappedField},
			},
			{
				Source:     "anthropic",
				Kind:       "thinking_budget",
				Value:      `8192`,
				Omission:   &NativeOmission{Field: "budget_tokens", Reason: LossDegraded, Detail: "budget approximated"},
				Projection: &NativeProjection{Origin: "budget", Effort: ReasoningEffortMedium},
			},
		},
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}},
	}

	losses1, err := OmittedNativeRequestLosses(req1, "responses")
	if err != nil {
		t.Fatalf("OmittedNativeRequestLosses failed: %v", err)
	}
	if len(losses1) != 1 || losses1[0].Field != "budget_tokens" || losses1[0].Reason != LossDegraded {
		t.Fatalf("expected one degraded budget_tokens loss, got %+v", losses1)
	}

	// Case 2: edited budget projection (caller changed ReasoningEffort to Low) -> stale budget degraded suppressed, mode type unmapped-field reported
	req2 := &Request{
		Model: "m",
		Params: Params{
			ReasoningEffort: ReasoningEffortLow,
		},
		Native: []NativeAnnotation{
			{
				Source:   "anthropic",
				Kind:     "thinking_mode",
				Value:    `"enabled"`,
				Omission: &NativeOmission{Field: "type", Reason: LossUnmappedField},
			},
			{
				Source:     "anthropic",
				Kind:       "thinking_budget",
				Value:      `8192`,
				Omission:   &NativeOmission{Field: "budget_tokens", Reason: LossDegraded, Detail: "budget approximated"},
				Projection: &NativeProjection{Origin: "budget", Effort: ReasoningEffortMedium},
			},
		},
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}},
	}

	losses2, err := OmittedNativeRequestLosses(req2, "responses")
	if err != nil {
		t.Fatalf("OmittedNativeRequestLosses failed: %v", err)
	}
	// Stale budget projection does not report budget_tokens degraded loss
	for _, l := range losses2 {
		if l.Field == "budget_tokens" && l.Reason == LossDegraded {
			t.Fatalf("stale budget degraded loss should not be emitted, got %+v", losses2)
		}
	}
	// Thinking mode unmapped-field is reported
	foundType := false
	for _, l := range losses2 {
		if l.Field == "type" && l.Reason == LossUnmappedField {
			foundType = true
		}
	}
	if !foundType {
		t.Fatalf("expected type unmapped-field loss when budget projection is invalidated, got %+v", losses2)
	}
}

func TestNativeRequestOnly(t *testing.T) {
	// MarshalResponse with a native-annotated block MUST fail
	resp := &Response{
		ID:    "resp_1",
		Model: "m",
		Content: []Block{
			TextBlock{
				Text: "hi",
				Native: []NativeAnnotation{
					{Source: "anthropic", Kind: "cache_control", Value: "{}"},
				},
			},
		},
		StopReason: StopEndTurn,
		Usage:      Usage{InputTokens: 1, OutputTokens: 1},
	}

	if _, err := MarshalResponse(resp); err == nil {
		t.Fatalf("MarshalResponse(response_with_native_block) expected error, got nil")
	}

	// Event with native-annotated block MUST fail
	event := ContentBlockStart{
		Index: 0,
		Block: TextBlock{
			Text: "hi",
			Native: []NativeAnnotation{
				{Source: "anthropic", Kind: "cache_control", Value: "{}"},
			},
		},
	}
	if _, err := MarshalEvent(event); err == nil {
		t.Fatalf("MarshalEvent(event_with_native_block) expected error, got nil")
	}
}

func TestFingerprintLosses(t *testing.T) {
	losses := []Loss{
		{Path: "tools[0]", Field: "eager_input_streaming", Reason: LossDegraded, Detail: "sensitive prompt details"},
		{Path: "native[1]", Field: "store", Reason: LossUnsupportedSemantic, Detail: "sensitive user key"},
		{Path: "stream_options", Field: "extra", Reason: LossUnmappedField, Detail: "extra payload"},
	}

	// Limit <= 0 returns all
	fpsAll := FingerprintLosses(losses, 0)
	if len(fpsAll) != 3 {
		t.Fatalf("expected 3 fingerprints, got %d", len(fpsAll))
	}
	if fpsAll[0].Path != "tools[0]" || fpsAll[0].Field != "eager_input_streaming" || fpsAll[0].Reason != LossDegraded {
		t.Fatalf("unexpected fingerprint values: %+v", fpsAll[0])
	}

	// Limit > 0 truncates
	fpsTrunc := FingerprintLosses(losses, 2)
	if len(fpsTrunc) != 2 {
		t.Fatalf("expected 2 fingerprints, got %d", len(fpsTrunc))
	}
	if fpsTrunc[1].Field != "store" {
		t.Fatalf("unexpected truncated second element: %+v", fpsTrunc[1])
	}
}

func TestValidateNativeRequestNewKinds(t *testing.T) {
	// eager_input_streaming on tool
	req := &Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}},
		},
		Tools: []Tool{
			{
				Name:        "t1",
				InputSchema: []byte(`{"type":"object"}`),
				Native: []NativeAnnotation{
					{Source: NativeSourceAnthropic, Kind: NativeKindEagerInputStreaming, Value: "true"},
				},
			},
		},
	}
	if err := ValidateNativeRequest(req); err != nil {
		t.Fatalf("ValidateNativeRequest failed for eager_input_streaming true: %v", err)
	}

	// eager_input_streaming non-boolean fails
	reqInvalid := &Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}},
		},
		Tools: []Tool{
			{
				Name:        "t1",
				InputSchema: []byte(`{"type":"object"}`),
				Native: []NativeAnnotation{
					{Source: NativeSourceAnthropic, Kind: NativeKindEagerInputStreaming, Value: `"not-bool"`},
				},
			},
		},
	}
	if err := ValidateNativeRequest(reqInvalid); err == nil {
		t.Fatalf("expected ValidateNativeRequest to fail for non-boolean eager_input_streaming")
	}

	// OpenAI request controls on Request
	reqOpenAI := &Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}},
		},
		Native: []NativeAnnotation{
			{Source: NativeSourceOpenAI, Kind: NativeKindMaxCompletionTokens, Value: "true"},
			{Source: NativeSourceOpenAI, Kind: NativeKindStore, Value: "false"},
			{Source: NativeSourceOpenAI, Kind: NativeKindStream, Value: "true"},
			{Source: NativeSourceOpenAI, Kind: NativeKindStreamOptions, Value: `{"include_usage":true}`},
			{Source: NativeSourceOpenAI, Kind: NativeKindPromptCacheKey, Value: `"k1"`},
			{Source: NativeSourceOpenAI, Kind: NativeKindPromptCacheOptions, Value: `{"mode":"explicit"}`},
		},
	}
	if err := ValidateNativeRequest(reqOpenAI); err != nil {
		t.Fatalf("ValidateNativeRequest failed for OpenAI request controls: %v", err)
	}

	reqInvalidPCK := &Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}},
		},
		Native: []NativeAnnotation{
			{Source: NativeSourceOpenAI, Kind: NativeKindPromptCacheKey, Value: `123`},
		},
	}
	if err := ValidateNativeRequest(reqInvalidPCK); err == nil {
		t.Fatalf("expected error for non-string prompt_cache_key")
	}

	reqNullPCK := &Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}},
		},
		Native: []NativeAnnotation{
			{Source: NativeSourceOpenAI, Kind: NativeKindPromptCacheKey, Value: `null`},
		},
	}
	if err := ValidateNativeRequest(reqNullPCK); err == nil {
		t.Fatalf("expected error for null prompt_cache_key")
	}

	reqInvalidPCO := &Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}},
		},
		Native: []NativeAnnotation{
			{Source: NativeSourceOpenAI, Kind: NativeKindPromptCacheOptions, Value: `"not-object"`},
		},
	}
	if err := ValidateNativeRequest(reqInvalidPCO); err == nil {
		t.Fatalf("expected error for non-object prompt_cache_options")
	}

	reqValidPCB := &Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{
				Text: "hi",
				Native: []NativeAnnotation{
					{Source: NativeSourceOpenAI, Kind: NativeKindPromptCacheBreakpoint, Value: `{"mode":"explicit"}`},
				},
			}}},
		},
	}
	if err := ValidateNativeRequest(reqValidPCB); err != nil {
		t.Fatalf("unexpected error for valid prompt_cache_breakpoint: %v", err)
	}

	reqInvalidPCB := &Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{
				Text: "hi",
				Native: []NativeAnnotation{
					{Source: NativeSourceOpenAI, Kind: NativeKindPromptCacheBreakpoint, Value: `123`},
				},
			}}},
		},
	}
	if err := ValidateNativeRequest(reqInvalidPCB); err == nil {
		t.Fatalf("expected error for non-object prompt_cache_breakpoint")
	}
}
