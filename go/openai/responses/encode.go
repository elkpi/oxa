package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

// EncodeRequest converts an IR request to a Responses wire request (IR ->
// face). System content renders as the instructions string; a conversation
// of exactly one user text message and no system content renders as the
// input string shorthand (N-R-2).
func EncodeRequest(req *ir.Request, opts ...Option) (*Request, []ir.Loss, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("responses: nil request")
	}
	if err := ir.ValidateToolStrict(req); err != nil {
		return nil, nil, err
	}
	if err := ir.ValidateCustomConversation(req); err != nil {
		return nil, nil, err
	}
	var losses []ir.Loss
	o := newOptions(opts...)
	out := &Request{Model: o.models.Map(req.Model)}
	if len(req.Metadata) > 0 {
		for key := range req.Metadata {
			if key == "" {
				return nil, nil, fmt.Errorf("responses: metadata key cannot be empty")
			}
		}
		md := make(map[string]string, len(req.Metadata))
		for key, value := range req.Metadata {
			md[key] = value
		}
		out.Metadata = md
	}
	var leadingSystemItems []InputItem
	if len(req.System) > 0 {
		instructions, items := encodeSystemInstructions(req.System)
		out.Instructions = instructions
		leadingSystemItems = items
	}
	if len(req.Tools) > 0 {
		groupedTools, toolLosses, err := encodeGroupedTools(req.Tools)
		if err != nil {
			return nil, nil, err
		}
		out.Tools = groupedTools
		losses = append(losses, toolLosses...)
	}
	if req.ToolChoice != nil && req.ToolChoice.Mode == ir.ToolChoiceTool && ir.IsNativeToolChoice(req) {
		nativeFoundAndRepresented := false
		for _, tool := range req.Tools {
			if tool.Type == ir.ToolTypeNative && tool.Name == req.ToolChoice.Name && tool.Source == ir.NativeSourceOpenAI {
				nativeFoundAndRepresented = true
				break
			}
		}
		if !nativeFoundAndRepresented {
			out.ToolChoice = nil
			losses = append(losses, ir.NativeToolChoiceLoss(req.ToolChoice))
		} else {
			choice, choiceLoss := encodeToolChoice(req.ToolChoice)
			out.ToolChoice = choice
			if choiceLoss != nil {
				losses = append(losses, *choiceLoss)
			}
		}
	} else {
		choice, choiceLoss := encodeToolChoice(req.ToolChoice)
		out.ToolChoice = choice
		if choiceLoss != nil {
			losses = append(losses, *choiceLoss)
		}
	}
	if req.ToolChoice != nil && req.ToolChoice.Mode == ir.ToolChoiceAny && len(req.Tools) > 0 && len(out.Tools) == 0 {
		out.ToolChoice = nil
		losses = append(losses, loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic, "all tool declarations were dropped"))
	}
	if len(out.Tools) == 0 {
		out.Tools = nil
	}

	items := append([]InputItem{}, leadingSystemItems...)
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
	if req.Params.ReasoningEffort != "" {
		out.Reasoning = map[string]any{"effort": req.Params.ReasoningEffort}
	}
	if req.Params.Verbosity != "" {
		v := req.Params.Verbosity
		out.Text = &TextParams{Verbosity: &v}
	}
	if rf := req.Params.ResponseFormat; rf != nil {
		if rf.Type == ir.ResponseFormatJSONSchema && rf.Name == "" {
			losses = append(losses, ir.Loss{Path: "params.response_format", Field: "name", Reason: ir.LossUnsupportedSemantic, Detail: "Responses requires a non-empty schema name"})
		} else {
			if out.Text == nil {
				out.Text = &TextParams{}
			}
			out.Text.Format = encodeTextFormat(rf)
		}
	}
	for _, n := range req.Native {
		if n.Source == ir.NativeSourceOpenAI && n.Kind == ir.NativeKindStore {
			var b bool
			if err := json.Unmarshal([]byte(n.Value), &b); err == nil {
				out.Store = &b
			}
		}
	}
	omissionLosses, err := ir.OmittedNativeRequestLosses(req, "responses")
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, omissionLosses...)
	return out, losses, nil
}

func encodeTextFormat(rf *ir.ResponseFormat) *TextFormatWire {
	if rf == nil {
		return nil
	}
	switch rf.Type {
	case ir.ResponseFormatText:
		return &TextFormatWire{Type: "text"}
	case ir.ResponseFormatJSONObject:
		return &TextFormatWire{Type: "json_object"}
	case ir.ResponseFormatJSONSchema:
		return &TextFormatWire{
			Type:        "json_schema",
			Name:        rf.Name,
			Description: rf.Description,
			Schema:      rf.Schema,
			Strict:      rf.Strict,
		}
	default:
		return &TextFormatWire{Type: rf.Type}
	}
}

// encodeUserMessage renders one IR user message as input items: tool results
// first (as function_call_output items, N-R-6), then the ordinary content as
// a message item (N-R-3). When ordinary content precedes a tool result in
// the source turn, that hoisting does not preserve the source order and
// records a degraded loss (N-R-10).
func encodeUserMessage(items *[]InputItem, message ir.Message, path string) ([]ir.Loss, error) {
	var losses []ir.Loss
	var normal []ir.Block
	var results []ir.Block
	var resultIndexes []int
	lastResult, firstNormal := -1, -1
	for k, block := range message.Content {
		switch block.(type) {
		case ir.ToolResultBlock, ir.CustomToolResultBlock, ir.ApplyPatchToolResultBlock:
			results = append(results, block)
			resultIndexes = append(resultIndexes, k)
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
	for j, resultBlock := range results {
		j = resultIndexes[j]
		if patch, ok := resultBlock.(ir.ApplyPatchToolResultBlock); ok {
			item := InputItem{
				Type:   ItemTypeApplyPatchCallOutput,
				CallID: patch.ToolUseID,
				Status: string(patch.Status),
			}
			if patch.Output != nil {
				item.Output = *patch.Output
			}
			*items = append(*items, item)
			continue
		}
		if custom, ok := resultBlock.(ir.CustomToolResultBlock); ok {
			output, outputLosses, err := encodeCustomOutput(custom.Output, fmt.Sprintf("%s.content[%d].output", path, j))
			if err != nil {
				return nil, err
			}
			losses = append(losses, outputLosses...)
			*items = append(*items, InputItem{Type: ItemTypeCustomToolCallOutput, CallID: custom.ToolUseID, Output: output})
			continue
		}
		result := resultBlock.(ir.ToolResultBlock)
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
			if len(value.Citations) > 0 {
				hasAnthropic := false
				hasOpenAI := false
				for _, c := range value.Citations {
					if c.Source == ir.CitationSourceOpenAI {
						hasOpenAI = true
					} else {
						hasAnthropic = true
					}
				}
				if hasAnthropic {
					losses = append(losses, loss(
						fmt.Sprintf("%s[%d].citations", path, i), "citations", ir.LossUnsupportedSemantic,
						"anthropic text citations; the target face cannot preserve source locations and replay metadata",
					))
				}
				if hasOpenAI {
					losses = append(losses, loss(
						fmt.Sprintf("%s[%d].citations", path, i), "citations", ir.LossUnsupportedSemantic,
						"openai text annotations; the target face cannot preserve source locations and replay metadata",
					))
				}
			}
		case ir.RedactedThinkingBlock:
			otherBlocks++
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
				"anthropic encrypted thinking replay state; the target face cannot preserve this block",
			))
		case ir.ImageBlock:
			part, imageLoss := encodeImagePart(value, fmt.Sprintf("%s[%d]", path, i))
			if imageLoss != nil {
				losses = append(losses, *imageLoss)
				continue
			}
			otherBlocks++
			parts = append(parts, part)
		case ir.DocumentBlock:
			otherBlocks++
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Responses user message item", block),
			))
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
	var thinkings []OutputContent
	var calls []InputItem
	text := ""
	hasText := false
	for i, block := range blocks {
		switch value := block.(type) {
		case ir.TextBlock:
			text += value.Text
			hasText = true
			if len(value.Citations) > 0 {
				hasAnthropic := false
				hasOpenAI := false
				for _, c := range value.Citations {
					if c.Source == ir.CitationSourceOpenAI {
						hasOpenAI = true
					} else {
						hasAnthropic = true
					}
				}
				if hasAnthropic {
					losses = append(losses, loss(
						fmt.Sprintf("%s[%d].citations", path, i), "citations", ir.LossUnsupportedSemantic,
						"anthropic text citations; the target face cannot preserve source locations and replay metadata",
					))
				}
				if hasOpenAI {
					losses = append(losses, loss(
						fmt.Sprintf("%s[%d].citations", path, i), "citations", ir.LossUnsupportedSemantic,
						"openai text annotations; the target face cannot preserve source locations and replay metadata",
					))
				}
			}
		case ir.RedactedThinkingBlock:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
				"anthropic encrypted thinking replay state; the target face cannot preserve this block",
			))
		case ir.ThinkingBlock:
			thinkings = append(thinkings, OutputContent{
				Type: PartTypeOutputText, Text: value.Thinking, Annotations: []json.RawMessage{},
			})
			if value.Signature != "" {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d].signature", path, i), "signature", ir.LossUnmappedField,
					"Responses request reasoning items carry no provider signature",
				))
			}
		case ir.ApplyPatchToolUseBlock:
			opWire, err := encodeApplyPatchOperation(value.Operation)
			if err != nil {
				return nil, err
			}
			calls = append(calls, InputItem{
				Type:      ItemTypeApplyPatchCall,
				CallID:    value.ID,
				Status:    string(value.Status),
				Operation: opWire,
			})
		case ir.CustomToolUseBlock:
			input, err := unwrapToolArguments(value.Input)
			if err != nil {
				return nil, err
			}
			ns, nm := "", value.Name
			if dotIdx := strings.Index(value.Name, "."); dotIdx > 0 {
				ns = value.Name[:dotIdx]
				nm = value.Name[dotIdx+1:]
			}
			calls = append(calls, InputItem{Type: ItemTypeCustomToolCall, CallID: value.ID, Name: nm, Namespace: ns, Input: &input})
		case ir.ToolUseBlock:
			arguments, err := unwrapToolArguments(value.Input)
			if err != nil {
				return nil, fmt.Errorf("responses: %s[%d].input: %w", path, i, err)
			}
			ns, nm := "", value.Name
			if dotIdx := strings.Index(value.Name, "."); dotIdx > 0 {
				ns = value.Name[:dotIdx]
				nm = value.Name[dotIdx+1:]
			}
			calls = append(calls, InputItem{
				Type: ItemTypeFunctionCall, CallID: value.ID, Name: nm, Namespace: ns, Arguments: arguments,
			})
		case ir.WebSearchCallBlock:
			item, err := encodeWebSearchCall(value, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			calls = append(calls, item)
		case ir.ImageBlock, ir.ToolResultBlock, ir.DocumentBlock:
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
	// Reasoning, text, and tool calls preserve the normalized IR block order.
	if len(thinkings) > 0 {
		*items = append(*items, InputItem{Type: ItemTypeReasoning, Summary: thinkings})
	}
	if hasText || (len(blocks) == 0 && len(thinkings) == 0) {
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
	var thinkings []OutputContent
	var textParts []OutputContent
	apcIndex := 0
	apcoIndex := 0
	for i, block := range resp.Content {
		switch value := block.(type) {
		case ir.TextBlock:
			var partAnnotations []json.RawMessage
			hasAnthropic := false
			for _, c := range value.Citations {
				if err := ir.ValidateCitation(c.Source, c.Value); err != nil {
					return nil, nil, fmt.Errorf("responses: content[%d].citations: %w", i, err)
				}
				switch c.Source {
				case ir.CitationSourceOpenAI:
					partAnnotations = append(partAnnotations, json.RawMessage(c.Value))
				case ir.CitationSourceAnthropic:
					hasAnthropic = true
				default:
					return nil, nil, fmt.Errorf("responses: content[%d].citations: unknown citation source %q", i, c.Source)
				}
			}
			if hasAnthropic {
				losses = append(losses, loss(
					fmt.Sprintf("content[%d].citations", i), "citations", ir.LossUnsupportedSemantic,
					"anthropic text citations; the target face cannot preserve source locations and replay metadata",
				))
			}
			if partAnnotations == nil {
				partAnnotations = []json.RawMessage{}
			}
			textParts = append(textParts, OutputContent{
				Type: PartTypeOutputText, Text: value.Text, Annotations: partAnnotations,
			})
		case ir.RedactedThinkingBlock:
			losses = append(losses, loss(
				fmt.Sprintf("content[%d]", i), "type", ir.LossUnsupportedSemantic,
				"anthropic encrypted thinking replay state; the target face cannot preserve this block",
			))
		case ir.ThinkingBlock:
			thinkings = append(thinkings, OutputContent{
				Type: PartTypeOutputText, Text: value.Thinking, Annotations: []json.RawMessage{},
			})
			if value.Signature != "" {
				losses = append(losses, loss(
					fmt.Sprintf("content[%d].signature", i), "signature", ir.LossUnmappedField,
					"Responses reasoning items carry no provider signature",
				))
			}
		case ir.ApplyPatchToolUseBlock:
			opWire, err := encodeApplyPatchOperation(value.Operation)
			if err != nil {
				return nil, nil, err
			}
			output = append(output, OutputItem{
				Type:      ItemTypeApplyPatchCall,
				ID:        fmt.Sprintf("apc_%d", apcIndex),
				Status:    string(value.Status),
				CallID:    value.ID,
				Operation: opWire,
			})
			apcIndex++
		case ir.ApplyPatchToolResultBlock:
			item := OutputItem{
				Type:   ItemTypeApplyPatchCallOutput,
				ID:     fmt.Sprintf("apco_%d", apcoIndex),
				Status: string(value.Status),
				CallID: value.ToolUseID,
			}
			if value.Output != nil {
				item.Output = value.Output
			}
			output = append(output, item)
			apcoIndex++
		case ir.CustomToolUseBlock:
			input, err := unwrapToolArguments(value.Input)
			if err != nil {
				return nil, nil, err
			}
			ns, nm := "", value.Name
			if dotIdx := strings.Index(value.Name, "."); dotIdx > 0 {
				ns = value.Name[:dotIdx]
				nm = value.Name[dotIdx+1:]
			}
			output = append(output, OutputItem{Type: ItemTypeCustomToolCall, ID: "ctc_abc123", CallID: value.ID, Name: nm, Namespace: ns, Input: &input})
		case ir.ToolUseBlock:
			arguments, err := unwrapToolArguments(value.Input)
			if err != nil {
				return nil, nil, fmt.Errorf("responses: content[%d].input: %w", i, err)
			}
			ns, nm := "", value.Name
			if dotIdx := strings.Index(value.Name, "."); dotIdx > 0 {
				ns = value.Name[:dotIdx]
				nm = value.Name[dotIdx+1:]
			}
			output = append(output, OutputItem{
				Type: ItemTypeFunctionCall, ID: "fc_abc123", Status: StatusCompleted,
				CallID: value.ID, Name: nm, Namespace: ns, Arguments: arguments,
			})
		case ir.WebSearchCallBlock:
			item, err := encodeWebSearchCall(value, fmt.Sprintf("content[%d]", i))
			if err != nil {
				return nil, nil, err
			}
			output = append(output, OutputItem{Type: item.Type, ID: item.ID, Status: item.Status, Action: item.Action, Extra: item.Extra})
		default:
			losses = append(losses, loss(
				fmt.Sprintf("content[%d]", i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Responses output item", block),
			))
		}
	}
	if len(textParts) > 0 || (len(resp.Content) == 0 && len(thinkings) == 0) {
		if len(textParts) == 0 {
			textParts = []OutputContent{{
				Type: PartTypeOutputText, Text: "", Annotations: []json.RawMessage{},
			}}
		}
		output = append([]OutputItem{{
			Type: ItemTypeMessage, ID: "msg_abc123", Status: StatusCompleted, Role: RoleAssistant,
			Content: textParts,
		}}, output...)
	}
	if len(thinkings) > 0 {
		output = append([]OutputItem{{
			Type: ItemTypeReasoning, ID: "rs_abc123",
			Summary: thinkings,
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

	usage := &UsageWire{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	if resp.Usage.InputTokensDetails != nil && resp.Usage.InputTokensDetails.CachedTokens != nil {
		usage.InputTokenDetails = &InputTokenDetailsWire{
			CachedTokens: *resp.Usage.InputTokensDetails.CachedTokens,
		}
	}
	if resp.Usage.OutputTokensDetails != nil && resp.Usage.OutputTokensDetails.ReasoningTokens != nil {
		usage.OutputTokenDetails = &OutputTokenDetailsWire{
			ReasoningTokens: *resp.Usage.OutputTokensDetails.ReasoningTokens,
		}
	}

	o := newOptions(opts...)
	out := &Response{
		ID:                resp.ID,
		Object:            ObjectResponse,
		Status:            StatusCompleted,
		Model:             o.models.Map(resp.Model),
		Output:            output,
		Usage:             usage,
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

func encodeSystemInstructions(blocks []ir.SystemBlock) (*string, []InputItem) {
	hasDeveloper := false
	for _, block := range blocks {
		if block.Role == ir.SystemRoleDeveloper {
			hasDeveloper = true
			break
		}
	}
	if !hasDeveloper {
		text := ""
		for _, block := range blocks {
			text += block.Text
		}
		return &text, nil
	}
	var instructions *string
	i := 0
	if blocks[0].Role != ir.SystemRoleDeveloper {
		text := ""
		for i < len(blocks) && blocks[i].Role != ir.SystemRoleDeveloper {
			text += blocks[i].Text
			i++
		}
		instructions = &text
	}
	items := make([]InputItem, 0)
	for ; i < len(blocks); i++ {
		role := RoleSystem
		if blocks[i].Role == ir.SystemRoleDeveloper {
			role = RoleDeveloper
		}
		items = append(items, InputItem{Role: role, Content: blocks[i].Text})
	}
	return instructions, items
}
