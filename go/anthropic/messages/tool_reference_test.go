package messages_test

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/ir"
)

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
