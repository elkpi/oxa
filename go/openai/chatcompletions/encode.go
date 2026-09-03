package chatcompletions

import (
	"fmt"

	"github.com/elkpi/oxa/go/ir"
)

// EncodeRequest converts an IR request to a Chat Completions wire request
// (IR -> face). System content renders as one leading system message; text
// content remains a string while image input renders as content parts.
func EncodeRequest(req *ir.Request, opts ...Option) (*Request, []ir.Loss, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil request")
	}
	var losses []ir.Loss
	if len(req.Metadata) > 0 {
		losses = append(losses, loss(
			"metadata", "metadata", ir.LossUnmappedField,
			"Chat Completions requests have no metadata field; the IR metadata map is dropped.",
		))
	}
	o := newOptions(opts...)
	out := &Request{Model: o.models.Map(req.Model)}
	if len(req.Tools) > 0 {
		out.Tools = make([]ToolWire, 0, len(req.Tools))
		for _, tool := range req.Tools {
			out.Tools = append(out.Tools, ToolWire{
				Type: ToolTypeFunction,
				Function: FunctionWire{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}
	choice, choiceLoss := encodeToolChoice(req.ToolChoice)
	out.ToolChoice = choice
	if choiceLoss != nil {
		losses = append(losses, *choiceLoss)
	}
	if len(req.System) > 0 {
		text := ""
		for _, system := range req.System {
			text += system.Text
		}
		out.Messages = append(out.Messages, Message{Role: RoleSystem, Content: text})
	}
	for i, message := range req.Messages {
		switch message.Role {
		case ir.RoleAssistant:
			assistant, messageLosses, err := encodeAssistantMessage(message.Content, fmt.Sprintf("messages[%d].content", i))
			if err != nil {
				return nil, nil, err
			}
			out.Messages = append(out.Messages, assistant)
			losses = append(losses, messageLosses...)
		case ir.RoleUser:
			var normal []ir.Block
			var results []ir.ToolResultBlock
			// N-CC-9: tool messages are hoisted ahead of the trailing user
			// content. When ordinary content precedes a tool result in the
			// source turn, that hoisting does not preserve the source order.
			lastResult, firstNormal := -1, -1
			for k, block := range message.Content {
				if result, ok := block.(ir.ToolResultBlock); ok {
					results = append(results, result)
					lastResult = k
					continue
				}
				if firstNormal < 0 {
					firstNormal = k
				}
				normal = append(normal, block)
			}
			if firstNormal >= 0 && firstNormal < lastResult {
				losses = append(losses, loss(
					fmt.Sprintf("messages[%d]", i), "ordering", ir.LossDegraded,
					"N-CC-9: tool messages are hoisted ahead of the trailing user content; source order is not preserved",
				))
			}
			// Tool messages must follow the assistant invocation immediately. If
			// the IR user turn also carries normal content, render it after the
			// tool-result run as a separate user message.
			for j, result := range results {
				toolMessage, resultLosses := encodeToolResult(result, fmt.Sprintf("messages[%d].content[%d]", i, j))
				out.Messages = append(out.Messages, toolMessage)
				losses = append(losses, resultLosses...)
			}
			if len(normal) > 0 || len(results) == 0 {
				content, contentLosses := encodeUserContent(normal, fmt.Sprintf("messages[%d].content", i))
				out.Messages = append(out.Messages, Message{Role: RoleUser, Content: content})
				losses = append(losses, contentLosses...)
			}
		default:
			return nil, nil, fmt.Errorf("chatcompletions: messages[%d]: unknown role %q", i, message.Role)
		}
	}
	if len(req.Params.StopSequences) > 0 {
		out.Stop = req.Params.StopSequences
	}
	out.Temperature = req.Params.Temperature
	out.TopP = req.Params.TopP
	out.MaxTokens = req.Params.MaxTokens
	return out, losses, nil
}

// EncodeResponse converts an IR response to a Chat Completions wire response
// (IR -> face). Envelope fields absent from the IR are rendered with the
// documented defaults (object "chat.completion", created 0, single choice
// index 0, message role assistant) and record no loss. usage.total_tokens is
// derived and recomputed.
func EncodeResponse(resp *ir.Response, opts ...Option) (*Response, []ir.Loss, error) {
	if resp == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil response")
	}
	message, losses, err := encodeAssistantMessage(resp.Content, "content")
	if err != nil {
		return nil, nil, err
	}
	finish := ""
	switch resp.StopReason {
	case ir.StopEndTurn:
		finish = FinishReasonStop
	case ir.StopMaxTokens:
		finish = FinishReasonLength
	case ir.StopRefusal:
		finish = FinishReasonContentFilter
	case ir.StopToolUse:
		finish = FinishReasonToolCalls
	case ir.StopSequence:
		// Chat Completions reports only finish_reason "stop" without
		// identifying which stop sequence matched, so the sequence value is
		// lost (spec/01 s4.1 note).
		finish = FinishReasonStop
		losses = append(losses, loss(
			"", "stop_sequence", ir.LossUnmappedValue,
			"Chat Completions finish_reason \"stop\" does not identify the matched stop sequence",
		))
	default:
		return nil, nil, fmt.Errorf("chatcompletions: stop reason %q has no Chat Completions equivalent", resp.StopReason)
	}
	o := newOptions(opts...)
	return &Response{
		ID:      resp.ID,
		Object:  ObjectChatCompletion,
		Created: 0,
		Model:   o.models.Map(resp.Model),
		Choices: []Choice{{
			Index:        0,
			Message:      message,
			FinishReason: finish,
		}},
		Usage: &UsageWire{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}, losses, nil
}
