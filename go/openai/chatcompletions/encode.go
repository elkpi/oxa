package chatcompletions

import (
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
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
		for i, tool := range req.Tools {
			if tool.Type == ir.ToolTypeCustom {
				out.Tools = append(out.Tools, ToolWire{
					Type: ir.ToolTypeCustom,
					Custom: &CustomToolWire{
						Name:        tool.Name,
						Description: tool.Description,
						Format:      tool.Format,
					},
				})
				continue
			}
			if tool.Type == ir.ToolTypeApplyPatch {
				losses = append(losses, loss(fmt.Sprintf("tools[%d]", i), "type", ir.LossUnsupportedSemantic, "apply_patch tool declarations are not implemented by this Chat Completions profile"))
				continue
			}
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
	if req.ToolChoice != nil && req.ToolChoice.Mode == ir.ToolChoiceAny && len(req.Tools) > 0 && len(out.Tools) == 0 {
		out.ToolChoice = nil
		losses = append(losses, loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic, "all tool declarations were dropped"))
	}
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
			var toolMessages []Message
			// N-CC-9: tool messages are hoisted ahead of the trailing user
			// content. When ordinary content precedes a tool result in the
			// source turn, that hoisting does not preserve the source order.
			lastResult, firstNormal := -1, -1
			for k, block := range message.Content {
				if customResult, ok := block.(ir.CustomToolResultBlock); ok {
					var content any
					if customResult.Output.Text != nil {
						content = *customResult.Output.Text
					} else {
						parts := make([]ContentPart, 0, len(customResult.Output.Content))
						for pIdx, b := range customResult.Output.Content {
							switch pb := b.(type) {
							case ir.TextBlock:
								parts = append(parts, ContentPart{Type: ContentPartTypeText, Text: pb.Text})
							case ir.ImageBlock:
								part, l := encodeImageBlock(pb, fmt.Sprintf("messages[%d].content[%d][%d]", i, k, pIdx))
								if l != nil {
									losses = append(losses, *l)
								}
								parts = append(parts, part)
							}
						}
						content = parts
					}
					toolMessages = append(toolMessages, Message{
						Role:       RoleTool,
						ToolCallID: customResult.ToolUseID,
						Content:    content,
					})
					lastResult = k
					continue
				}
				if _, patch := block.(ir.ApplyPatchToolResultBlock); patch {
					losses = append(losses, loss(fmt.Sprintf("messages[%d].content[%d]", i, k), "content", ir.LossUnsupportedSemantic, "apply_patch tool results are not implemented by this profile"))
					continue
				}
				if result, ok := block.(ir.ToolResultBlock); ok {
					toolMessage, resultLosses := encodeToolResult(result, fmt.Sprintf("messages[%d].content[%d]", i, k))
					toolMessages = append(toolMessages, toolMessage)
					losses = append(losses, resultLosses...)
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
			out.Messages = append(out.Messages, toolMessages...)
			if len(normal) > 0 || len(toolMessages) == 0 {
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
	out.ReasoningEffort = req.Params.ReasoningEffort
	if req.Params.ResponseFormat != nil {
		out.ResponseFormat = encodeResponseFormat(req.Params.ResponseFormat)
	}
	return out, losses, nil
}

func encodeResponseFormat(rf *ir.ResponseFormat) *ResponseFormatWire {
	if rf == nil {
		return nil
	}
	switch rf.Type {
	case ir.ResponseFormatText:
		return &ResponseFormatWire{Type: "text"}
	case ir.ResponseFormatJSONObject:
		return &ResponseFormatWire{Type: "json_object"}
	case ir.ResponseFormatJSONSchema:
		return &ResponseFormatWire{
			Type: "json_schema",
			JSONSchema: &JSONSchemaWire{
				Name:        rf.Name,
				Description: rf.Description,
				Schema:      rf.Schema,
				Strict:      rf.Strict,
			},
		}
	default:
		return &ResponseFormatWire{Type: rf.Type}
	}
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
		hasPatch, hasCall := false, false
		for _, b := range resp.Content {
			switch b.(type) {
			case ir.ApplyPatchToolUseBlock:
				hasPatch = true
			case ir.ToolUseBlock, ir.CustomToolUseBlock:
				hasCall = true
			}
		}
		if hasPatch && !hasCall {
			finish = FinishReasonStop
			losses = append(losses, loss("stop_reason", "stop_reason", ir.LossDegraded, "tool calls were dropped; tool_use normalized to end_turn"))
		}
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
	usage := &UsageWire{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
		TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	if resp.Usage.InputTokensDetails != nil && resp.Usage.InputTokensDetails.CachedTokens != nil {
		usage.PromptTokensDetails = &PromptTokensDetailsWire{
			CachedTokens: *resp.Usage.InputTokensDetails.CachedTokens,
		}
	}
	if resp.Usage.OutputTokensDetails != nil && resp.Usage.OutputTokensDetails.ReasoningTokens != nil {
		usage.CompletionTokensDetails = &CompletionTokensDetailsWire{
			ReasoningTokens: *resp.Usage.OutputTokensDetails.ReasoningTokens,
		}
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
		Usage: usage,
	}, losses, nil
}
