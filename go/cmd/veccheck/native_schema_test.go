package main

import (
	"encoding/json"
	"testing"
)

func TestNativeRequestSchemaAcceptance(t *testing.T) {
	schs := testSchemas(t)

	validate := func(raw string) bool {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		return schs.ir.Validate(v) == nil
	}

	// 1. request_with_native_cache == valid
	reqNativeCache := `{"specVersion":"0.2.0","model":"m","native":[{"source":"anthropic","kind":"cache_control","value":"{\"type\":\"ephemeral\"}"}],"messages":[{"role":"user","content":[{"type":"text","text":"hi","native":[{"source":"anthropic","kind":"cache_control","value":"{\"type\":\"ephemeral\"}"}]}]}]}`
	if !validate(reqNativeCache) {
		t.Errorf("validate(request_with_native_cache) expected valid, got invalid")
	}

	// 2. request_with_native_source_future == valid
	reqNativeFuture := `{"specVersion":"0.2.0","model":"m","native":[{"source":"future_source","kind":"future_kind","value":"{}"}],"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`
	if !validate(reqNativeFuture) {
		t.Errorf("validate(request_with_native_source_future) expected valid, got invalid")
	}

	// 3. response_with_native_text_block == invalid
	respNativeBlock := `{"specVersion":"0.2.0","id":"msg_1","model":"m","content":[{"type":"text","text":"hi","native":[{"source":"anthropic","kind":"cache_control","value":"{}"}]}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	if validate(respNativeBlock) {
		t.Errorf("validate(response_with_native_text_block) expected invalid, got valid")
	}

	// 4. event_with_native_text_block == invalid
	eventNativeBlock := `{"specVersion":"0.2.0","events":[{"type":"message_start","message":{"id":"m1","model":"m","usage":{"input_tokens":1}}},{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"","native":[{"source":"anthropic","kind":"cache_control","value":"{}"}]}},{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}},{"type":"message_done"}]}`
	if validate(eventNativeBlock) {
		t.Errorf("validate(event_with_native_text_block) expected invalid, got valid")
	}
}
