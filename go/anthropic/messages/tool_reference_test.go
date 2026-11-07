package messages_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa-protocol/go/v3/anthropic/messages"
	"github.com/elkpi/oxa-protocol/go/v3/ir"
)

func TestDecodeTypedToolReference(t *testing.T) {
	wire := &messages.Request{
		Model: "m", MaxTokens: 32,
		Messages: []messages.Message{{Role: messages.RoleUser, Content: []messages.BlockWire{{Type: messages.BlockTypeToolReference, ToolName: "Read"}}}},
	}
	request, losses, err := messages.DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("DecodeRequest: losses=%+v, err=%v", losses, err)
	}
	ref, ok := request.Messages[0].Content[0].(ir.ToolReferenceBlock)
	if !ok || ref.ToolName != "Read" {
		t.Fatalf("unexpected reference: %+v", request.Messages[0].Content[0])
	}
}

func TestDecodeToolReferenceBlock(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"tools": [
			{"name": "Read", "input_schema": {"type": "object"}}
		],
		"messages": [
			{
				"role": "assistant",
				"content": [
					{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": {}}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"content": [
							{"type": "tool_reference", "tool_name": "Read"},
							{"type": "text", "text": "ok"}
						]
					}
				]
			}
		]
	}`)

	req, losses, err := messages.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON error: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses, got %+v", losses)
	}

	toolResult, ok := req.Messages[1].Content[0].(ir.ToolResultBlock)
	if !ok {
		t.Fatalf("expected ToolResultBlock, got %T", req.Messages[1].Content[0])
	}
	if len(toolResult.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(toolResult.Content))
	}

	toolRef, ok := toolResult.Content[0].(ir.ToolReferenceBlock)
	if !ok {
		t.Fatalf("expected ToolReferenceBlock, got %T", toolResult.Content[0])
	}
	if toolRef.ToolName != "Read" {
		t.Errorf("expected tool_name 'Read', got %q", toolRef.ToolName)
	}

	// Round-trip encode
	encoded, encodeLosses, err := messages.EncodeRequestJSON(req)
	if err != nil {
		t.Fatalf("EncodeRequestJSON error: %v", err)
	}
	if len(encodeLosses) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", encodeLosses)
	}

	var wire struct {
		Messages []struct {
			Content []struct {
				Content []struct {
					Type     string `json:"type"`
					ToolName string `json:"tool_name"`
					Text     string `json:"text"`
				} `json:"content"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal encoded JSON: %v", err)
	}

	refBlock := wire.Messages[1].Content[0].Content[0]
	if refBlock.Type != "tool_reference" || refBlock.ToolName != "Read" {
		t.Errorf("round-trip tool_reference mismatch: %+v", refBlock)
	}
}

func TestDecodeToolReferenceNameFallback(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"content": [
							{"type": "tool_reference", "name": "FallbackTool"}
						]
					}
				]
			}
		]
	}`)

	req, losses, err := messages.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON error: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses, got %+v", losses)
	}

	toolResult := req.Messages[0].Content[0].(ir.ToolResultBlock)
	toolRef := toolResult.Content[0].(ir.ToolReferenceBlock)
	if toolRef.ToolName != "FallbackTool" {
		t.Errorf("expected 'FallbackTool', got %q", toolRef.ToolName)
	}
}

func TestDecodeToolReferenceMissingToolName(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"content": [
							{"type": "tool_reference"}
						]
					}
				]
			}
		]
	}`)

	_, _, err := messages.DecodeRequestJSON(raw)
	if err == nil {
		t.Fatalf("expected error for missing tool_name, got nil")
	}
}

func TestDecodeToolReferenceEmptyToolNameIsStructuralEvenWithAlias(t *testing.T) {
	for _, ref := range []string{
		`{"type": "tool_reference", "tool_name": "", "name": "Read"}`,
		`{"type": "tool_reference", "tool_name": null, "name": "Read"}`,
	} {
		raw := []byte(`{
			"model": "claude-opus-5-5",
			"max_tokens": 1024,
			"messages": [
				{
					"role": "user",
					"content": [
						{
							"type": "tool_result",
							"tool_use_id": "toolu_1",
							"content": [` + ref + `]
						}
					]
				}
			]
		}`)

		if _, _, err := messages.DecodeRequestJSON(raw); err == nil {
			t.Errorf("expected structural error for %s, got nil", ref)
		}
	}
}

func TestDecodeToolReferenceUnknownFieldIsAudited(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"content": [
							{"type": "tool_reference", "tool_name": "Read", "extra": false}
						]
					}
				]
			}
		]
	}`)

	_, losses, err := messages.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON error: %v", err)
	}
	found := false
	for _, l := range losses {
		if l.Path == "messages[0].content[0].content[0].extra" && l.Field == "extra" && l.Reason == ir.LossUnmappedField {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unmapped-field loss for extra, got %+v", losses)
	}
}

// N-AN-21: cache_control attached to a tool_reference must round-trip on
// Anthropic (decode attaches the annotation; encode re-emits it).
func TestToolReferenceCacheControlRoundTrip(t *testing.T) {
	raw := []byte(`{
		"model": "claude-opus-5-5",
		"max_tokens": 1024,
		"tools": [
			{"name": "Read", "input_schema": {"type": "object"}}
		],
		"messages": [
			{
				"role": "assistant",
				"content": [
					{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": {}}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"content": [
							{
								"type": "tool_reference",
								"tool_name": "Read",
								"cache_control": {"type": "ephemeral", "ttl": "1h"}
							}
						]
					}
				]
			}
		]
	}`)

	req, losses, err := messages.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON error: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 decode losses, got %+v", losses)
	}

	toolResult := req.Messages[1].Content[0].(ir.ToolResultBlock)
	toolRef := toolResult.Content[0].(ir.ToolReferenceBlock)
	found := false
	for _, ann := range toolRef.Native {
		if ann.Source == ir.NativeSourceAnthropic && ann.Kind == ir.NativeKindCacheControl {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cache_control native annotation on decode, got %+v", toolRef.Native)
	}

	encoded, encodeLosses, err := messages.EncodeRequestJSON(req)
	if err != nil {
		t.Fatalf("EncodeRequestJSON error: %v", err)
	}
	if len(encodeLosses) != 0 {
		t.Fatalf("expected 0 encode losses, got %+v", encodeLosses)
	}
	if !strings.Contains(string(encoded), `"cache_control"`) {
		t.Fatalf("encoded request dropped cache_control: %s", encoded)
	}
}

// N-AN-21: a dropped tool_reference carries its annotations with it; the
// cross-face omission collector must not report them a second time.
func TestToolReferenceCrossFaceOmissionReportsWholeBlockOnly(t *testing.T) {
	req := &ir.Request{
		Model: "m",
		Messages: []ir.Message{{
			Role: ir.RoleUser,
			Content: []ir.Block{ir.ToolResultBlock{ToolUseID: "c", Content: []ir.Block{
				ir.ToolReferenceBlock{ToolName: "Read", Native: []ir.NativeAnnotation{{
					Source: ir.NativeSourceAnthropic,
					Kind:   ir.NativeKindCacheControl,
					Value:  `{"type":"ephemeral"}`,
				}}},
			}}},
		}},
	}
	losses, err := ir.OmittedNativeRequestLosses(req, "chatcompletions")
	if err != nil {
		t.Fatalf("OmittedNativeRequestLosses error: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 omission losses (whole-block drop), got %+v", losses)
	}
}

func TestEncodeToolReferenceRejectsEmptyToolName(t *testing.T) {
	req := &ir.Request{
		Model: "m",
		Messages: []ir.Message{{
			Role:    ir.RoleUser,
			Content: []ir.Block{ir.ToolReferenceBlock{}},
		}},
	}
	if _, _, err := messages.EncodeRequestJSON(req); err == nil {
		t.Fatalf("expected error for empty tool_name, got nil")
	}
}
