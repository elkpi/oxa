package chatcompletions

import (
	"fmt"

	"github.com/oxa-protocol/oxa/go/ir"
)

// DecodeRequest converts a Chat Completions wire request to the IR (face ->
// IR). Semantic unmappables are losses, never errors; errors are reserved for
// structural type violations of known fields (spec/02 s4).
func DecodeRequest(wire *Request, opts ...Option) (*ir.Request, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil request")
	}
	var losses []ir.Loss
	for _, field := range []struct {
		name    string
		present bool
		detail  string
	}{
		{"parallel_tool_calls", wire.ParallelToolCalls != nil, "Chat Completions parallel tool calls have no IR equivalent in v1."},
		{"functions", wire.Functions != nil, "legacy Chat Completions functions have no IR equivalent in v1."},
		{"function_call", wire.FunctionCall != nil, "legacy Chat Completions function_call has no IR equivalent in v1."},
		{"response_format", wire.ResponseFormat != nil, "Chat Completions response_format has no IR equivalent in v1."},
		{"logprobs", wire.Logprobs != nil, "Chat Completions log-probability sampling has no IR equivalent in v1."},
		{"top_logprobs", wire.TopLogprobs != nil, "Chat Completions log-probability sampling has no IR equivalent in v1."},
		{"metadata", wire.Metadata != nil, "Chat Completions request metadata has no IR equivalent in v1."},
	} {
		if field.present {
			losses = append(losses, loss(field.name, field.name, ir.LossUnmappedField, field.detail))
		}
	}

	o := newOptions(opts...)
	req := &ir.Request{Model: o.models.Map(wire.Model)}
	for i, tool := range wire.Tools {
		if tool.Type != "function" {
			losses = append(losses, loss(
				fmt.Sprintf("tools[%d]", i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Chat Completions tool type %q has no IR equivalent", tool.Type),
			))
			continue
		}
		req.Tools = append(req.Tools, ir.Tool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
	}
	choice, choiceLosses := decodeToolChoice(wire.ToolChoice)
	req.ToolChoice = choice
	losses = append(losses, choiceLosses...)

	for i := 0; i < len(wire.Messages); {
		message := wire.Messages[i]
		if message.Role == "tool" {
			merged, next, resultLosses, err := decodeToolResultRun(wire.Messages, i)
			if err != nil {
				return nil, nil, err
			}
			req.Messages = append(req.Messages, merged)
			losses = append(losses, resultLosses...)
			i = next
			continue
		}

		content, contentLosses, err := decodeContent(message.Content, fmt.Sprintf("messages[%d].content", i))
		if err != nil {
			return nil, nil, err
		}
		losses = append(losses, contentLosses...)
		if message.Role != "system" && len(content) == 0 {
			// IR conversation messages cannot have empty content (spec/01 s3.3).
			content = []ir.Block{ir.TextBlock{Text: ""}}
		}
		switch message.Role {
		case "system":
			system, systemLosses := contentSystem(content, fmt.Sprintf("messages[%d].content", i))
			req.System = append(req.System, system...)
			losses = append(losses, systemLosses...)
		case "user":
			req.Messages = append(req.Messages, ir.Message{Role: ir.RoleUser, Content: content})
		case "assistant":
			toolCalls, toolLosses := decodeToolCalls(message.ToolCalls, fmt.Sprintf("messages[%d].tool_calls", i))
			if message.Content == nil && len(toolCalls) > 0 {
				// A tool-only assistant message has no normal content to prepend.
				content = nil
			}
			content = append(content, toolCalls...)
			req.Messages = append(req.Messages, ir.Message{Role: ir.RoleAssistant, Content: content})
			losses = append(losses, toolLosses...)
		default:
			return nil, nil, fmt.Errorf("chatcompletions: messages[%d]: unknown role %q", i, message.Role)
		}
		if message.FunctionCall != nil {
			losses = append(losses, loss(
				fmt.Sprintf("messages[%d].function_call", i), "function_call", ir.LossUnmappedField,
				"legacy Chat Completions function_call has no IR equivalent",
			))
		}
		i++
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

// DecodeResponse converts a Chat Completions wire response to the IR (face ->
// IR). Envelope fields (object, created, choices[].index, message.role) are
// exempt from losses (vectors/README.md loss conventions); usage.total_tokens
// is derived. Unknown finish_reason values map to other plus an unmapped-value
// loss (spec/01 s4.1).
func DecodeResponse(wire *Response, opts ...Option) (*ir.Response, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil response")
	}
	if len(wire.Choices) == 0 {
		return nil, nil, fmt.Errorf("chatcompletions: response carries no choices")
	}
	var losses []ir.Loss
	choice := wire.Choices[0]
	blocks, contentLosses, err := decodeContent(choice.Message.Content, "choices[0].message.content")
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, contentLosses...)
	toolCalls, toolLosses := decodeToolCalls(choice.Message.ToolCalls, "choices[0].message.tool_calls")
	if choice.Message.Content == nil && len(toolCalls) > 0 {
		// A tool-only assistant response has no normal content to prepend.
		blocks = nil
	}
	blocks = append(blocks, toolCalls...)
	losses = append(losses, toolLosses...)
	if choice.Message.FunctionCall != nil {
		losses = append(losses, loss(
			"choices[0].message.function_call", "function_call", ir.LossUnmappedField,
			"legacy Chat Completions function_call has no IR equivalent",
		))
	}
	stop, finishLoss, err := decodeFinishReason(choice.FinishReason)
	if err != nil {
		return nil, nil, err
	}
	if finishLoss != nil {
		losses = append(losses, *finishLoss)
	}
	o := newOptions(opts...)
	resp := &ir.Response{
		ID:         wire.ID,
		Model:      o.models.Map(wire.Model),
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
