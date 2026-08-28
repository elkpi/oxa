package chatcompletions

import (
	"fmt"

	"github.com/oxa-protocol/oxa/go/ir"
)

// DecodeRequest converts a Chat Completions wire request to the IR (face ->
// IR). Semantic unmappables are losses, never errors; errors are reserved for
// structural type violations of known fields (spec/02 s4).
//
// Mapping: system role messages concatenate, in order, into ir.System;
// user/assistant messages become IR messages with text blocks (string or
// parts-array content); temperature/top_p/max_tokens/stop map to Params;
// logprobs/top_logprobs are dropped with unmapped-field losses.
func DecodeRequest(wire *Request) (*ir.Request, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil request")
	}
	var losses []ir.Loss
	if wire.Logprobs != nil {
		losses = append(losses, ir.Loss{
			Path:   "logprobs",
			Field:  "logprobs",
			Reason: ir.LossUnmappedField,
			Detail: "Chat Completions log-probability sampling has no IR equivalent in v1.",
		})
	}
	if wire.TopLogprobs != nil {
		losses = append(losses, ir.Loss{
			Path:   "top_logprobs",
			Field:  "top_logprobs",
			Reason: ir.LossUnmappedField,
			Detail: "Chat Completions log-probability sampling has no IR equivalent in v1.",
		})
	}

	if wire.Metadata != nil {
		losses = append(losses, ir.Loss{
			Path:   "metadata",
			Field:  "metadata",
			Reason: ir.LossUnmappedField,
			Detail: "Chat Completions request metadata has no IR equivalent in v1.",
		})
	}
	req := &ir.Request{Model: defaultOptions.models.Map(wire.Model)}
	for i, m := range wire.Messages {
		content, err := decodeContent(m.Content, fmt.Sprintf("messages[%d].content", i))
		if err != nil {
			return nil, nil, err
		}
		switch m.Role {
		case "system":
			req.System = append(req.System, contentSystem(content)...)
		case "user":
			req.Messages = append(req.Messages, ir.Message{Role: ir.RoleUser, Content: content})
		case "assistant":
			req.Messages = append(req.Messages, ir.Message{Role: ir.RoleAssistant, Content: content})
		default:
			return nil, nil, fmt.Errorf("chatcompletions: messages[%d]: unknown role %q", i, m.Role)
		}
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("chatcompletions: request carries no conversation messages")
	}
	req.Params = ir.Params{
		Temperature:   wire.Temperature,
		TopP:          wire.TopP,
		MaxTokens:     wire.MaxTokens,
		StopSequences: wire.Stop,
	}
	return req, losses, nil
}

// decodeContent converts string or parts-array wire content into IR text
// blocks.
func decodeContent(content any, path string) ([]ir.Block, error) {
	switch v := content.(type) {
	case nil:
		// An empty message is not representable in IR; use an empty text
		// block (spec/01 s3.3).
		return []ir.Block{ir.TextBlock{Text: ""}}, nil
	case string:
		return []ir.Block{ir.TextBlock{Text: v}}, nil
	case []any:
		blocks := make([]ir.Block, 0, len(v))
		for j, part := range v {
			raw, ok := part.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("chatcompletions: %s[%d]: part is not an object", path, j)
			}
			if t, _ := raw["type"].(string); t != "text" {
				return nil, fmt.Errorf("chatcompletions: %s[%d]: unsupported part type %q", path, j, t)
			}
			text, _ := raw["text"].(string)
			blocks = append(blocks, ir.TextBlock{Text: text})
		}
		return blocks, nil
	default:
		return nil, fmt.Errorf("chatcompletions: %s: content is neither string nor parts array", path)
	}
}

func contentSystem(blocks []ir.Block) []ir.SystemBlock {
	out := make([]ir.SystemBlock, 0, len(blocks))
	for _, b := range blocks {
		if t, ok := b.(ir.TextBlock); ok {
			out = append(out, ir.SystemBlock{Text: t.Text})
		}
	}
	return out
}

// DecodeResponse converts a Chat Completions wire response to the IR (face ->
// IR). Envelope fields (object, created, choices[].index, message.role) are
// exempt from losses (vectors/README.md loss conventions); usage.total_tokens
// is derived. Unknown finish_reason values map to other plus an unmapped-value
// loss (spec/01 s4.1).
func DecodeResponse(wire *Response) (*ir.Response, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil response")
	}
	if len(wire.Choices) == 0 {
		return nil, nil, fmt.Errorf("chatcompletions: response carries no choices")
	}
	var losses []ir.Loss
	choice := wire.Choices[0]
	blocks, err := decodeContent(choice.Message.Content, "choices[0].message.content")
	if err != nil {
		return nil, nil, err
	}
	stop, loss, err := decodeFinishReason(choice.FinishReason)
	if err != nil {
		return nil, nil, err
	}
	if loss != nil {
		losses = append(losses, *loss)
	}
	resp := &ir.Response{
		ID:         wire.ID,
		Model:      defaultOptions.models.Map(wire.Model),
		Content:    blocks,
		StopReason: stop,
	}
	if wire.Usage != nil {
		resp.Usage = ir.Usage{
			InputTokens:  wire.Usage.PromptTokens,
			OutputTokens: wire.Usage.CompletionTokens,
		}
	}
	return resp, losses, nil
}

func decodeFinishReason(finish string) (ir.StopReason, *ir.Loss, error) {
	switch finish {
	case "stop":
		return ir.StopEndTurn, nil, nil
	case "length":
		return ir.StopMaxTokens, nil, nil
	case "content_filter":
		return ir.StopRefusal, nil, nil
	case "tool_calls":
		return ir.StopToolUse, nil, nil
	case "":
		return "", nil, fmt.Errorf("chatcompletions: choices[0].finish_reason is missing")
	default:
		return ir.StopOther, &ir.Loss{
			Path:   "choices[0].finish_reason",
			Field:  "finish_reason",
			Reason: ir.LossUnmappedValue,
			Detail: fmt.Sprintf("Chat Completions finish_reason %q has no IR equivalent", finish),
		}, nil
	}
}
