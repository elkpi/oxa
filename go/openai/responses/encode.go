package responses

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/ir"
)

// EncodeRequest converts an IR request to a Responses wire request (IR ->
// face). System content renders as the instructions string; a conversation
// of exactly one user text message and no system content renders as the
// input string shorthand (N-R-2).
func EncodeRequest(req *ir.Request, opts ...Option) (*Request, []ir.Loss, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("responses: nil request")
	}
	var losses []ir.Loss
	if len(req.Metadata) > 0 {
		losses = append(losses, loss(
			"metadata", "metadata", ir.LossUnmappedField,
			"Responses requests have a string-valued metadata field with no IR equivalent; the IR metadata map is dropped.",
		))
	}
	o := newOptions(opts...)
	out := &Request{Model: o.models.Map(req.Model)}
	if len(req.System) > 0 {
		text := ""
		for _, system := range req.System {
			text += system.Text
		}
		out.Instructions = &text
	}
	if len(req.Tools) > 0 {
		out.Tools = make([]ToolDef, 0, len(req.Tools))
		for _, tool := range req.Tools {
			out.Tools = append(out.Tools, ToolDef{
				Type:        ToolTypeFunction,
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			})
		}
	}
	choice, choiceLoss := encodeToolChoice(req.ToolChoice)
	out.ToolChoice = choice
	if choiceLoss != nil {
		losses = append(losses, *choiceLoss)
	}

	items := make([]InputItem, 0)
	for i, message := range req.Messages {
		switch message.Role {
		case ir.RoleAssistant:
			messageLosses, err := encodeAssistantMessage(&items, message.Content, fmt.Sprintf("messages[%d].content", i))
			if err != nil {
				return nil, nil, err
			}
			losses = append(losses, messageLosses...)
		case ir.RoleUser:
			messageLosses, err := encodeUserMessage(&items, message, fmt.Sprintf("messages[%d]", i))
			if err != nil {
				return nil, nil, err
			}
			losses = append(losses, messageLosses...)
		default:
			return nil, nil, fmt.Errorf("responses: messages[%d]: unknown role %q", i, message.Role)
		}
	}

	// N-R-2: the input string shorthand renders exactly the case of no system
	// content and one user message whose content is exactly one text block.
	if len(req.System) == 0 && len(items) == 1 && items[0].Role == RoleUser {
		if text, ok := items[0].Content.(string); ok {
			out.Input = Input{Text: &text}
		}
	}
	if out.Input.Text == nil {
		out.Input = Input{Items: items}
	}

	// N-R-7: Responses has no stop-sequences parameter, so IR stop sequences
	// cannot round-trip; presence records exactly one loss.
	if len(req.Params.StopSequences) > 0 {
		losses = append(losses, loss(
			"params.stop_sequences", "stop_sequences", ir.LossUnmappedField,
			"Responses requests have no stop-sequences parameter; the IR stop sequences are dropped.",
		))
	}
	out.Temperature = req.Params.Temperature
	out.TopP = req.Params.TopP
	out.MaxOutputTokens = req.Params.MaxTokens
	return out, losses, nil
}

// encodeUserMessage renders one IR user message as input items: tool results
// first (as function_call_output items, N-R-6), then the ordinary content as
// a message item (N-R-3). When ordinary content precedes a tool result in
// the source turn, that hoisting does not preserve the source order and
// records a degraded loss (N-R-10).
func encodeUserMessage(items *[]InputItem, message ir.Message, path string) ([]ir.Loss, error) {
	var losses []ir.Loss
	var normal []ir.Block
	var results []ir.ToolResultBlock
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
			path, "ordering", ir.LossDegraded,
			"N-R-10: function_call_output items are hoisted ahead of the trailing user content; source order is not preserved",
		))
	}
	for j, result := range results {
		output := ""
		for k, block := range result.Content {
			if text, ok := block.(ir.TextBlock); ok {
				output += text.Text
				continue
			}
			losses = append(losses, loss(
				fmt.Sprintf("%s.content[%d].content[%d]", path, j, k), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Responses function_call_output item", block),
			))
		}
		if result.IsError {
			losses = append(losses, loss(
				fmt.Sprintf("%s.content[%d].is_error", path, j), "is_error", ir.LossUnmappedField,
				"Responses function_call_output items have no is_error field",
			))
		}
		*items = append(*items, InputItem{Type: ItemTypeFunctionCallOutput, CallID: result.ToolUseID, Output: output})
	}
	if len(normal) > 0 || len(results) == 0 {
		content, contentLosses := encodeUserContent(normal, path+".content")
		*items = append(*items, InputItem{Role: RoleUser, Content: content})
		losses = append(losses, contentLosses...)
	}
	return losses, nil
}

// encodeUserContent renders ordinary user content as a string when it is
// exactly one text block and as a parts array otherwise (N-R-3, N-R-4).
func encodeUserContent(blocks []ir.Block, path string) (any, []ir.Loss) {
	parts := make([]ContentPart, 0, len(blocks))
	text := ""
	textBlocks := 0
	otherBlocks := 0
	var losses []ir.Loss
	for i, block := range blocks {
		switch value := block.(type) {
		case ir.TextBlock:
			parts = append(parts, ContentPart{Type: PartTypeInputText, Text: value.Text})
			text += value.Text
			textBlocks++
		case ir.ImageBlock:
			part, imageLoss := encodeImagePart(value, fmt.Sprintf("%s[%d]", path, i))
			if imageLoss != nil {
				losses = append(losses, *imageLoss)
				continue
			}
			otherBlocks++
			parts = append(parts, part)
		default:
			otherBlocks++
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Responses user message item", block),
			))
		}
	}
	// String content is reserved for the single-text-block case; any other
	// content (multiple text blocks, images) renders as a parts array.
	if textBlocks <= 1 && otherBlocks == 0 {
		return text, losses
	}
	return parts, losses
}

// encodeAssistantMessage renders one IR assistant message as input items:
// the text (if any) as a message item with string content, then each
// ToolUseBlock as a function_call item carrying the raw argument text
// verbatim (N-R-5, INV-1).
func encodeAssistantMessage(items *[]InputItem, blocks []ir.Block, path string) ([]ir.Loss, error) {
	var losses []ir.Loss
	var calls []InputItem
	text := ""
	hasText := false
	for i, block := range blocks {
		switch value := block.(type) {
		case ir.TextBlock:
			text += value.Text
			hasText = true
		case ir.ToolUseBlock:
			arguments, err := unwrapToolArguments(value.Input)
			if err != nil {
				return nil, fmt.Errorf("responses: %s[%d].input: %w", path, i, err)
			}
			calls = append(calls, InputItem{
				Type: ItemTypeFunctionCall, CallID: value.ID, Name: value.Name, Arguments: arguments,
			})
		case ir.ImageBlock, ir.ToolResultBlock:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Responses assistant input item", block),
			))
		default:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("unknown IR block %T cannot be rendered", block),
			))
		}
	}
	// The message item precedes its function_call items, mirroring the decode
	// order of text blocks before tool-use blocks (N-R-5).
	if hasText || len(blocks) == 0 {
		*items = append(*items, InputItem{Role: RoleAssistant, Content: text})
	}
	*items = append(*items, calls...)
	return losses, nil
}

// EncodeResponse converts an IR response to a Responses wire response (IR ->
// face). Envelope fields absent from the IR are rendered with the documented
// defaults (N-R-12): object "response", status, item ids (msg_abc123 /
// fc_abc123), item status "completed", and empty annotations; no loss is
// recorded. usage.total_tokens is derived and recomputed.
func EncodeResponse(resp *ir.Response, opts ...Option) (*Response, []ir.Loss, error) {
	if resp == nil {
		return nil, nil, fmt.Errorf("responses: nil response")
	}
	var losses []ir.Loss
	var output []OutputItem
	text := ""
	hasText := false
	for i, block := range resp.Content {
		switch value := block.(type) {
		case ir.TextBlock:
			text += value.Text
			hasText = true
		case ir.ToolUseBlock:
			arguments, err := unwrapToolArguments(value.Input)
			if err != nil {
				return nil, nil, fmt.Errorf("responses: content[%d].input: %w", i, err)
			}
			output = append(output, OutputItem{
				Type: ItemTypeFunctionCall, ID: "fc_abc123", Status: StatusCompleted,
				CallID: value.ID, Name: value.Name, Arguments: arguments,
			})
		default:
			losses = append(losses, loss(
				fmt.Sprintf("content[%d]", i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Responses output item", block),
			))
		}
	}
	if hasText || len(resp.Content) == 0 {
		output = append([]OutputItem{{
			Type: ItemTypeMessage, ID: "msg_abc123", Status: StatusCompleted, Role: RoleAssistant,
			Content: []OutputContent{{
				Type: PartTypeOutputText, Text: text, Annotations: []json.RawMessage{},
			}},
		}}, output...)
	}

	var incomplete *IncompleteWire
	var failed *ErrorWire
	switch resp.StopReason {
	case ir.StopEndTurn, ir.StopToolUse:
		// status completed below
	case ir.StopMaxTokens:
		incomplete = &IncompleteWire{Reason: IncompleteReasonMaxOutputTokens}
	case ir.StopSequence:
		losses = append(losses, loss(
			"", "stop_sequence", ir.LossUnmappedValue,
			"Responses status carries no stop-sequence identity; the matched IR stop sequence is lost",
		))
	case ir.StopRefusal:
		failed = &ErrorWire{Code: ErrorCodeRefusal}
	default:
		return nil, nil, fmt.Errorf("responses: stop reason %q has no Responses equivalent", resp.StopReason)
	}

	o := newOptions(opts...)
	out := &Response{
		ID:     resp.ID,
		Object: ObjectResponse,
		Status: StatusCompleted,
		Model:  o.models.Map(resp.Model),
		Output: output,
		Usage: &UsageWire{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
		IncompleteDetails: incomplete,
	}
	if failed != nil {
		out.Status = StatusFailed
		out.Error = failed
	} else if incomplete != nil {
		out.Status = StatusIncomplete
	}
	return out, losses, nil
}
