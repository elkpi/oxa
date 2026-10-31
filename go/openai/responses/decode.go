package responses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

func isValidReasoningEffort(effort string) bool {
	switch effort {
	case ir.ReasoningEffortMinimal, ir.ReasoningEffortLow, ir.ReasoningEffortMedium, ir.ReasoningEffortHigh, ir.ReasoningEffortXHigh, ir.ReasoningEffortMax:
		return true
	}
	return false
}

// DecodeRequest converts a Responses wire request to the IR (face -> IR).
// Semantic unmappables are losses, never errors; errors are reserved for
// structural type violations of known fields (spec/02 s4).
func DecodeRequest(wire *Request, opts ...Option) (*ir.Request, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("responses: nil request")
	}
	var losses []ir.Loss
	for _, field := range []struct {
		path    string
		field   string
		present bool
		detail  string
	}{
		{"parallel_tool_calls", "parallel_tool_calls", wire.ParallelToolCalls != nil,
			"Responses parallel tool calls have no IR equivalent in v1."},
	} {
		if field.present {
			losses = append(losses, loss(field.path, field.field, ir.LossUnmappedField, field.detail))
		}
	}

	o := newOptions(opts...)
	req := &ir.Request{Model: o.models.Map(wire.Model)}
	if wire.Text != nil && wire.Text.Verbosity != nil {
		switch *wire.Text.Verbosity {
		case "low", "medium", "high":
			req.Params.Verbosity = *wire.Text.Verbosity
		default:
			losses = append(losses, loss("text.verbosity", "verbosity", ir.LossUnmappedValue, "unknown output verbosity"))
		}
	}
	metadata, err := responsesMetadata(wire.Metadata)
	if err != nil {
		return nil, nil, err
	}
	req.Metadata = metadata

	if wire.Reasoning != nil {
		if m, ok := wire.Reasoning.(map[string]any); ok {
			if effort, ok := m["effort"].(string); ok {
				if isValidReasoningEffort(effort) {
					req.Params.ReasoningEffort = effort
				} else {
					losses = append(losses, loss("reasoning.effort", "effort", ir.LossUnmappedValue,
						fmt.Sprintf("unknown reasoning effort value %q", effort)))
				}
			}
			if _, ok := m["summary"]; ok {
				losses = append(losses, loss("reasoning.summary", "summary", ir.LossUnmappedField,
					"Responses reasoning summary configuration has no IR equivalent."))
			}
		} else if str, ok := wire.Reasoning.(string); ok {
			if isValidReasoningEffort(str) {
				req.Params.ReasoningEffort = str
			} else {
				losses = append(losses, loss("reasoning.effort", "effort", ir.LossUnmappedValue,
					fmt.Sprintf("unknown reasoning effort value %q", str)))
			}
		}
	}

	// N-R-1：instructions 在前，system/developer 条目共享输入顺序。
	if wire.Instructions != nil {
		req.System = append(req.System, ir.SystemBlock{Text: *wire.Instructions})
	}

	seenNames := make(map[string]bool)
	for i, tool := range wire.Tools {
		raw := []byte(tool.RawDefinition)
		if len(raw) == 0 {
			var err error
			raw, err = json.Marshal(tool)
			if err != nil {
				return nil, nil, err
			}
		}
		decodedTools, toolLosses, err := decodeToolItem(raw, nil, fmt.Sprintf("tools[%d]", i), seenNames)
		if err != nil {
			return nil, nil, err
		}
		losses = append(losses, toolLosses...)
		req.Tools = append(req.Tools, decodedTools...)
	}
	choice, choiceLosses := decodeToolChoice(wire.ToolChoice)
	req.ToolChoice = choice
	losses = append(losses, choiceLosses...)

	// N-R-2: the string shorthand is one user message with one text block.
	if wire.Input.Text != nil {
		req.Messages = append(req.Messages, ir.Message{
			Role:    ir.RoleUser,
			Content: []ir.Block{ir.TextBlock{Text: *wire.Input.Text}},
		})
	}

	decodedCalls, decodedResults, droppedItems, pairLosses, err := precheckApplyPatchPairs(wire.Input.Items)
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, pairLosses...)

	i := 0
	for i < len(wire.Input.Items) {
		item := wire.Input.Items[i]
		switch item.Type {
		case "", ItemTypeMessage:
			switch item.Role {
			case RoleSystem, RoleDeveloper:
				content, contentLosses, err := decodeItemContent(item.Content, fmt.Sprintf("input[%d].content", i))
				if err != nil {
					return nil, nil, err
				}
				role := ""
				if item.Role == RoleDeveloper {
					role = ir.SystemRoleDeveloper
				}
				for j, block := range content {
					if text, ok := block.(ir.TextBlock); ok {
						req.System = append(req.System, ir.SystemBlock{Text: text.Text, Role: role, Native: text.Native})
						continue
					}
					losses = append(losses, loss(
						fmt.Sprintf("input[%d].content[%d]", i, j), "content", ir.LossUnsupportedSemantic,
						fmt.Sprintf("IR %T cannot be rendered in the IR system field", block),
					))
				}
				losses = append(losses, contentLosses...)
				i++
			case RoleUser:
				content, contentLosses, err := decodeItemContent(item.Content, fmt.Sprintf("input[%d].content", i))
				if err != nil {
					return nil, nil, err
				}
				losses = append(losses, contentLosses...)
				req.Messages = append(req.Messages, ir.Message{Role: ir.RoleUser, Content: content})
				i++
			case RoleAssistant:
				merged, next, runLosses, err := decodeAssistantRun(wire.Input.Items, i, decodedCalls, droppedItems)
				if err != nil {
					return nil, nil, err
				}
				if merged != nil {
					req.Messages = append(req.Messages, *merged)
				}
				losses = append(losses, runLosses...)
				i = next
			default:
				return nil, nil, fmt.Errorf("responses: input[%d]: unknown role %q", i, item.Role)
			}
		case ItemTypeFunctionCall, ItemTypeCustomToolCall, ItemTypeApplyPatchCall, ItemTypeReasoning, ItemTypeWebSearchCall:
			merged, next, runLosses, err := decodeAssistantRun(wire.Input.Items, i, decodedCalls, droppedItems)
			if err != nil {
				return nil, nil, err
			}
			if merged != nil {
				req.Messages = append(req.Messages, *merged)
			}
			losses = append(losses, runLosses...)
			i = next
		case ItemTypeFunctionCallOutput, ItemTypeCustomToolCallOutput, ItemTypeApplyPatchCallOutput:
			merged, next, runLosses, err := decodeOutputRun(wire.Input.Items, i, decodedResults, droppedItems)
			if err != nil {
				return nil, nil, err
			}
			if merged != nil {
				req.Messages = append(req.Messages, *merged)
			}
			losses = append(losses, runLosses...)
			i = next
		case ItemTypeAdditionalTools:
			if len(item.Tools) == 0 {
				return nil, nil, fmt.Errorf("responses: input[%d]: additional_tools requires non-empty tools array", i)
			}
			for idx, rawTool := range item.Tools {
				toolPath := fmt.Sprintf("input[%d].tools[%d]", i, idx)
				decodedTools, toolLosses, err := decodeToolItem(rawTool, nil, toolPath, seenNames)
				if err != nil {
					return nil, nil, err
				}
				losses = append(losses, toolLosses...)
				req.Tools = append(req.Tools, decodedTools...)
			}
			i++
		default:
			losses = append(losses, loss(
				fmt.Sprintf("input[%d]", i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses input item type %q has no IR equivalent", item.Type),
			))
			i++
		}
	}
	if len(req.Messages) == 0 {
		if len(req.System) == 0 {
			return nil, nil, fmt.Errorf("responses: request carries no conversation input")
		}
		req.Messages = append(req.Messages, ir.Message{
			Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: ""}},
		})
		losses = append(losses, loss("input", "input", ir.LossDegraded,
			"An empty user message was added because the IR requires at least one conversation message."))
	}
	req.Params.Temperature = wire.Temperature
	req.Params.TopP = wire.TopP
	req.Params.MaxTokens = wire.MaxOutputTokens
	if wire.Text != nil && wire.Text.Format != nil {
		rf, rfLosses, err := decodeTextFormat(wire.Text.Format)
		if err != nil {
			return nil, nil, err
		}
		losses = append(losses, rfLosses...)
		req.Params.ResponseFormat = rf
	}
	if err := ir.ValidateCustomConversation(req); err != nil {
		return nil, nil, err
	}
	return req, losses, nil
}

type toolItemRef struct {
	callID string
	kind   string
	index  int
}

func precheckApplyPatchPairs(items []InputItem) (map[int]ir.Block, map[int]ir.Block, map[int]bool, []ir.Loss, error) {
	decodedCalls := make(map[int]ir.Block)
	decodedResults := make(map[int]ir.Block)
	dropped := make(map[int]bool)
	var losses []ir.Loss

	i := 0
	for i < len(items) {
		item := items[i]
		if item.Type == ItemTypeApplyPatchCallOutput {
			return nil, nil, nil, nil, fmt.Errorf("responses: input[%d]: apply_patch tool result without preceding tool calls (INV-3)", i)
		}
		isAssistantItem := (item.Type == "" && item.Role == RoleAssistant) ||
			(item.Type == ItemTypeMessage && item.Role == RoleAssistant) ||
			item.Type == ItemTypeFunctionCall || item.Type == ItemTypeCustomToolCall ||
			item.Type == ItemTypeApplyPatchCall || item.Type == ItemTypeReasoning || item.Type == ItemTypeWebSearchCall

		if !isAssistantItem {
			i++
			continue
		}

		var calls []toolItemRef
		seenCallIDs := make(map[string]bool)
		aStart := i
		for aStart < len(items) {
			it := items[aStart]
			kind := ""
			switch it.Type {
			case ItemTypeFunctionCall:
				kind = "function"
			case ItemTypeCustomToolCall:
				kind = "custom"
			case ItemTypeApplyPatchCall:
				kind = "apply_patch"
			case ItemTypeWebSearchCall:
			case ItemTypeReasoning:
			case "", ItemTypeMessage:
				if it.Role != RoleAssistant {
					kind = "stop"
				}
			default:
				kind = "stop"
			}
			if kind == "stop" {
				break
			}
			if kind != "" {
				if kind == "apply_patch" && it.CallID == "" {
					return nil, nil, nil, nil, fmt.Errorf("responses: input[%d]: apply_patch tool call requires nonempty call_id (INV-3)", aStart)
				}
				if seenCallIDs[it.CallID] {
					return nil, nil, nil, nil, fmt.Errorf("responses: input[%d]: duplicate call_id %q in turn (INV-3)", aStart, it.CallID)
				}
				seenCallIDs[it.CallID] = true
				calls = append(calls, toolItemRef{it.CallID, kind, aStart})
			}
			aStart++
		}

		var results []toolItemRef
		oStart := aStart
		for oStart < len(items) {
			it := items[oStart]
			matchedKind := ""
			switch it.Type {
			case ItemTypeFunctionCallOutput:
				matchedKind = "function"
			case ItemTypeCustomToolCallOutput:
				matchedKind = "custom"
			case ItemTypeApplyPatchCallOutput:
				matchedKind = "apply_patch"
			default:
			}
			if matchedKind == "" {
				break
			}
			if matchedKind == "apply_patch" && it.CallID == "" {
				return nil, nil, nil, nil, fmt.Errorf("responses: input[%d]: apply_patch tool result requires nonempty call_id (INV-3)", oStart)
			}
			results = append(results, toolItemRef{it.CallID, matchedKind, oStart})
			oStart++
		}

		hasApplyPatch := false
		for _, c := range calls {
			if c.kind == "apply_patch" {
				hasApplyPatch = true
				break
			}
		}
		for _, r := range results {
			if r.kind == "apply_patch" {
				hasApplyPatch = true
				break
			}
		}

		if hasApplyPatch {
			if len(calls) != len(results) {
				return nil, nil, nil, nil, fmt.Errorf("responses: tool calls count (%d) and results count (%d) mismatch (INV-3)", len(calls), len(results))
			}
			for k := range calls {
				if calls[k].callID != results[k].callID || calls[k].kind != results[k].kind {
					return nil, nil, nil, nil, fmt.Errorf("responses: tool result identity/type/order mismatch (INV-3)")
				}
			}
			for k := range calls {
				if calls[k].kind != "apply_patch" {
					continue
				}
				cIdx := calls[k].index
				oIdx := results[k].index
				cItem := items[cIdx]
				oItem := items[oIdx]

				op, opLoss, err := decodeApplyPatchOperation(cItem.Operation, fmt.Sprintf("input[%d]", cIdx))
				if err != nil {
					return nil, nil, nil, nil, err
				}
				cStatus, cStatusLoss, err := decodeApplyPatchCallStatus(cItem.Status, fmt.Sprintf("input[%d]", cIdx))
				if err != nil {
					return nil, nil, nil, nil, err
				}
				rStatus, rStatusLoss, err := decodeApplyPatchResultStatus(oItem.Status, fmt.Sprintf("input[%d]", oIdx))
				if err != nil {
					return nil, nil, nil, nil, err
				}
				outputStr, err := decodeApplyPatchResultOutput(oItem.Output, fmt.Sprintf("input[%d]", oIdx))
				if err != nil {
					return nil, nil, nil, nil, err
				}

				hasCallLoss := opLoss != nil || cStatusLoss != nil
				hasResLoss := rStatusLoss != nil

				if hasCallLoss || hasResLoss {
					dropped[cIdx] = true
					dropped[oIdx] = true
					if hasCallLoss {
						if opLoss != nil {
							losses = append(losses, *opLoss)
						}
						if cStatusLoss != nil {
							losses = append(losses, *cStatusLoss)
						}
					} else {
						losses = append(losses, loss(fmt.Sprintf("input[%d]", cIdx), "type", ir.LossUnsupportedSemantic, "paired apply_patch result has unsupported semantic"))
					}
					if hasResLoss {
						losses = append(losses, *rStatusLoss)
					} else {
						losses = append(losses, loss(fmt.Sprintf("input[%d]", oIdx), "type", ir.LossUnsupportedSemantic, "paired apply_patch call has unsupported semantic"))
					}
				} else {
					decodedCalls[cIdx] = ir.ApplyPatchToolUseBlock{
						ID:        cItem.CallID,
						Status:    cStatus,
						Operation: op,
					}
					decodedResults[oIdx] = ir.ApplyPatchToolResultBlock{
						ToolUseID: oItem.CallID,
						Status:    rStatus,
						Output:    outputStr,
					}
				}
			}
		}

		i = oStart
	}

	return decodedCalls, decodedResults, dropped, losses, nil
}

func decodeTextFormat(raw any) (*ir.ResponseFormat, []ir.Loss, error) {
	if raw == nil {
		return nil, nil, nil
	}
	var wire TextFormatWire
	switch v := raw.(type) {
	case *TextFormatWire:
		if v == nil {
			return nil, nil, nil
		}
		wire = *v
	case TextFormatWire:
		wire = v
	case json.RawMessage:
		if err := json.Unmarshal(v, &wire); err != nil {
			return nil, nil, fmt.Errorf("responses: text.format: %w", err)
		}
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("responses: text.format: %w", err)
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, nil, fmt.Errorf("responses: text.format: %w", err)
		}
	}

	switch wire.Type {
	case ir.ResponseFormatText:
		return &ir.ResponseFormat{Type: ir.ResponseFormatText}, nil, nil
	case ir.ResponseFormatJSONObject:
		return &ir.ResponseFormat{Type: ir.ResponseFormatJSONObject}, nil, nil
	case ir.ResponseFormatJSONSchema:
		if wire.Name == "" {
			return nil, nil, fmt.Errorf("responses: text.format of type json_schema requires non-empty name")
		}
		if len(wire.Schema) == 0 {
			return nil, nil, fmt.Errorf("responses: text.format of type json_schema requires schema")
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(wire.Schema, &obj); err != nil || obj == nil {
			return nil, nil, fmt.Errorf("responses: text.format schema must be a JSON object")
		}
		return &ir.ResponseFormat{
			Type:        ir.ResponseFormatJSONSchema,
			Name:        wire.Name,
			Description: wire.Description,
			Schema:      wire.Schema,
			Strict:      wire.Strict,
		}, nil, nil
	case "":
		return nil, nil, fmt.Errorf("responses: text.format requires non-empty type")
	default:
		return nil, []ir.Loss{
			loss("text.format.type", "type", ir.LossUnmappedValue,
				fmt.Sprintf("unknown text.format type %q", wire.Type)),
		}, nil
	}
}

func decodeReasoningSummary(parts []OutputContent, path string) ([]ir.Block, []ir.Loss) {
	var blocks []ir.Block
	var losses []ir.Loss
	for i, part := range parts {
		partPath := fmt.Sprintf("%s[%d]", path, i)
		if part.Type != PartTypeOutputText && part.Type != PartTypeSummaryText {
			losses = append(losses, loss(partPath, "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses reasoning summary type %q has no IR equivalent", part.Type)))
			continue
		}
		if len(part.Annotations) > 0 {
			losses = append(losses, loss(partPath+".annotations", "annotations", ir.LossUnmappedField,
				"Responses reasoning summary annotations have no IR equivalent."))
		}
		blocks = append(blocks, ir.ThinkingBlock{Thinking: part.Text})
	}
	return blocks, losses
}

// decodeAssistantRun converts a maximal run of assistant message items and
// function_call items into one IR assistant message (N-R-5): the text blocks
// first (in document order), then the ToolUseBlocks, regardless of how the
// wire interleaved message items and function_call items.
func decodeAssistantRun(items []InputItem, start int, decodedCalls map[int]ir.Block, dropped map[int]bool) (*ir.Message, int, []ir.Loss, error) {
	var thinkings []ir.Block
	var texts []ir.Block
	var calls []ir.Block
	var losses []ir.Loss
	i := start
	for ; i < len(items); i++ {
		item := items[i]
		if item.Type == ItemTypeApplyPatchCall {
			if dropped[i] {
				continue
			}
			if block, ok := decodedCalls[i]; ok {
				calls = append(calls, block)
				continue
			}
			return nil, 0, nil, fmt.Errorf("responses: input[%d]: unpaired apply_patch_call (INV-3)", i)
		}
		if item.Type == ItemTypeCustomToolCall {
			finalName := item.Name
			if item.Namespace != "" && !strings.HasPrefix(finalName, item.Namespace+".") {
				finalName = item.Namespace + "." + finalName
			}
			call, err := decodeCustomCall(item.CallID, finalName, item.Input)
			if err != nil {
				return nil, 0, nil, err
			}
			calls = append(calls, call)
			continue
		}
		if item.Type == ItemTypeFunctionCall {
			input, err := wrapToolArguments(item.Arguments)
			if err != nil {
				return nil, 0, nil, fmt.Errorf("responses: input[%d].arguments: %w", i, err)
			}
			finalName := item.Name
			if item.Namespace != "" && !strings.HasPrefix(finalName, item.Namespace+".") {
				finalName = item.Namespace + "." + finalName
			}
			calls = append(calls, ir.ToolUseBlock{ID: item.CallID, Name: finalName, Input: input})
			continue
		}
		if item.Type == ItemTypeWebSearchCall {
			block, callLosses, err := decodeWebSearchCall(item.ID, item.Status, item.Action, item.Extra, fmt.Sprintf("input[%d]", i))
			if err != nil {
				return nil, 0, nil, err
			}
			losses = append(losses, callLosses...)
			calls = append(calls, block)
			continue
		}
		if item.Type == ItemTypeReasoning {
			blocks, summaryLosses := decodeReasoningSummary(item.Summary, fmt.Sprintf("input[%d].summary", i))
			thinkings = append(thinkings, blocks...)
			losses = append(losses, summaryLosses...)
			continue
		}
		if item.Type != "" && item.Type != ItemTypeMessage {
			break
		}
		if item.Role != RoleAssistant {
			break
		}
		content, contentLosses, err := decodeItemContent(item.Content, fmt.Sprintf("input[%d].content", i))
		if err != nil {
			return nil, 0, nil, err
		}
		losses = append(losses, contentLosses...)
		texts = append(texts, content...)
	}
	blocks := make([]ir.Block, 0, len(thinkings)+len(texts)+len(calls))
	blocks = append(blocks, thinkings...)
	blocks = append(blocks, texts...)
	blocks = append(blocks, calls...)
	if len(blocks) == 0 {
		return nil, i, losses, nil
	}
	return &ir.Message{Role: ir.RoleAssistant, Content: blocks}, i, losses, nil
}

// decodeOutputRun converts a maximal run of consecutive function_call_output
// items into one IR user message of ToolResultBlocks (N-R-6, INV-4).
func decodeOutputRun(items []InputItem, start int, decodedResults map[int]ir.Block, dropped map[int]bool) (*ir.Message, int, []ir.Loss, error) {
	content := make([]ir.Block, 0)
	var losses []ir.Loss
	i := start
	for ; i < len(items); i++ {
		item := items[i]
		if item.Type == ItemTypeApplyPatchCallOutput {
			if dropped[i] {
				continue
			}
			if block, ok := decodedResults[i]; ok {
				content = append(content, block)
				continue
			}
			return nil, 0, nil, fmt.Errorf("responses: input[%d]: unpaired apply_patch_call_output (INV-3)", i)
		}
		if item.Type == ItemTypeCustomToolCallOutput {
			if item.CallID == "" {
				return nil, 0, nil, fmt.Errorf("responses: custom result requires call_id")
			}
			output, partLosses, err := decodeCustomOutput(item.Output, fmt.Sprintf("input[%d].output", i))
			if err != nil {
				return nil, 0, nil, err
			}
			losses = append(losses, partLosses...)
			content = append(content, ir.CustomToolResultBlock{ToolUseID: item.CallID, Output: output})
			continue
		}
		if item.Type != ItemTypeFunctionCallOutput {
			break
		}
		output, ok := item.Output.(string)
		if !ok {
			return nil, 0, nil, fmt.Errorf("responses: function output must be a string")
		}
		content = append(content, ir.ToolResultBlock{ToolUseID: item.CallID, Content: []ir.Block{ir.TextBlock{Text: output}}})
	}
	if len(content) == 0 {
		return nil, i, losses, nil
	}
	return &ir.Message{Role: ir.RoleUser, Content: content}, i, losses, nil
}

// decodeToolChoice maps the Responses tool_choice forms to the IR modes
// (N-R-9): required and IR mode any are equivalent, loss-free both ways.
func decodeToolChoice(value any) (*ir.ToolChoice, []ir.Loss) {
	if value == nil {
		return nil, nil
	}
	if choice, ok := value.(string); ok {
		switch choice {
		case ToolChoiceAuto, ToolChoiceNone:
			return &ir.ToolChoice{Mode: choice}, nil
		case ToolChoiceRequired:
			return &ir.ToolChoice{Mode: ir.ToolChoiceAny}, nil
		default:
			return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses tool_choice %q has no IR equivalent", choice))}
		}
	}
	var kind, name, namespace string
	switch choice := value.(type) {
	case ToolChoiceWire:
		kind, name, namespace = choice.Type, choice.Name, choice.Namespace
	case *ToolChoiceWire:
		if choice == nil {
			return nil, nil
		}
		kind, name, namespace = choice.Type, choice.Name, choice.Namespace
	case map[string]any:
		kind, _ = choice["type"].(string)
		name, _ = choice["name"].(string)
		namespace, _ = choice["namespace"].(string)
	default:
		return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
			"Responses tool_choice has no IR equivalent")}
	}
	if namespace != "" && !strings.HasPrefix(name, namespace+".") {
		name = namespace + "." + name
	}
	if kind == ToolTypeApplyPatch {
		return &ir.ToolChoice{Mode: ir.ToolChoiceTool, ToolType: ir.ToolTypeApplyPatch}, nil
	}
	if kind == ToolTypeCustom && name != "" {
		return &ir.ToolChoice{Mode: ir.ToolChoiceTool, Name: name, ToolType: ir.ToolTypeCustom}, nil
	}
	if kind == ToolTypeFunction && name != "" {
		return &ir.ToolChoice{Mode: ir.ToolChoiceTool, Name: name}, nil
	}
	return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
		"only named function Responses tool_choice values are supported")}
}

// encodeToolChoice renders the IR tool-choice modes in Responses form
// (N-R-9).
func encodeToolChoice(choice *ir.ToolChoice) (any, *ir.Loss) {
	if choice == nil {
		return nil, nil
	}
	switch choice.Mode {
	case ir.ToolChoiceAuto, ir.ToolChoiceNone:
		return choice.Mode, nil
	case ir.ToolChoiceAny:
		return ToolChoiceRequired, nil
	case ir.ToolChoiceTool:
		if choice.ToolType == ir.ToolTypeApplyPatch {
			return ToolChoiceWire{Type: ToolTypeApplyPatch}, nil
		}
		if choice.Name == "" {
			return nil, ptrLoss(loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
				"IR named tool choice has no function name"))
		}
		kind := ToolTypeFunction
		if choice.ToolType == ir.ToolTypeCustom {
			kind = ToolTypeCustom
		}
		dotIdx := strings.Index(choice.Name, ".")
		if dotIdx > 0 {
			return ToolChoiceWire{
				Type:      kind,
				Namespace: choice.Name[:dotIdx],
				Name:      choice.Name[dotIdx+1:],
			}, nil
		}
		return ToolChoiceWire{Type: kind, Name: choice.Name}, nil
	default:
		return nil, ptrLoss(loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
			fmt.Sprintf("IR tool_choice mode %q has no Responses equivalent", choice.Mode)))
	}
}

// DecodeResponse converts a Responses wire response to the IR (face -> IR).
// Envelope fields (object, item ids/status) are exempt from losses
// (vectors/README.md loss conventions); usage.total_tokens is derived.
func DecodeResponse(wire *Response, opts ...Option) (*ir.Response, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("responses: nil response")
	}
	var losses []ir.Loss
	var thinkings []ir.Block
	var texts []ir.Block
	var calls []ir.Block
	hasToolUse := false
	for i, item := range wire.Output {
		switch item.Type {
		case ItemTypeApplyPatchCall:
			if item.ID == "" {
				return nil, nil, fmt.Errorf("responses: output[%d].id is required", i)
			}
			if item.CallID == "" {
				return nil, nil, fmt.Errorf("responses: output[%d].call_id is required", i)
			}
			status, statusLoss, err := decodeApplyPatchCallStatus(item.Status, fmt.Sprintf("output[%d]", i))
			if err != nil {
				return nil, nil, err
			}
			if statusLoss != nil {
				losses = append(losses, *statusLoss)
				continue
			}
			op, opLoss, err := decodeApplyPatchOperation(item.Operation, fmt.Sprintf("output[%d]", i))
			if err != nil {
				return nil, nil, err
			}
			if opLoss != nil {
				losses = append(losses, *opLoss)
				continue
			}
			calls = append(calls, ir.ApplyPatchToolUseBlock{
				ID:        item.CallID,
				Status:    status,
				Operation: op,
			})
			hasToolUse = true
		case ItemTypeApplyPatchCallOutput:
			if item.ID == "" {
				return nil, nil, fmt.Errorf("responses: output[%d].id is required", i)
			}
			if item.CallID == "" {
				return nil, nil, fmt.Errorf("responses: output[%d].call_id is required", i)
			}
			status, statusLoss, err := decodeApplyPatchResultStatus(item.Status, fmt.Sprintf("output[%d]", i))
			if err != nil {
				return nil, nil, err
			}
			if statusLoss != nil {
				losses = append(losses, *statusLoss)
				continue
			}
			outputStr, err := decodeApplyPatchResultOutput(item.Output, fmt.Sprintf("output[%d]", i))
			if err != nil {
				return nil, nil, err
			}
			calls = append(calls, ir.ApplyPatchToolResultBlock{
				ToolUseID: item.CallID,
				Status:    status,
				Output:    outputStr,
			})
		case ItemTypeCustomToolCall:
			finalName := item.Name
			if item.Namespace != "" && !strings.HasPrefix(finalName, item.Namespace+".") {
				finalName = item.Namespace + "." + finalName
			}
			call, err := decodeCustomCall(item.CallID, finalName, item.Input)
			if err != nil {
				return nil, nil, err
			}
			calls = append(calls, call)
			hasToolUse = true
		case ItemTypeReasoning:
			if len(item.Summary) == 0 {
				losses = append(losses, loss(
					fmt.Sprintf("output[%d]", i), "type", ir.LossUnsupportedSemantic,
					"Responses reasoning output item with empty summary carries no convertible content",
				))
			} else {
				blocks, summaryLosses := decodeReasoningSummary(item.Summary, fmt.Sprintf("output[%d].summary", i))
				thinkings = append(thinkings, blocks...)
				losses = append(losses, summaryLosses...)
			}
			if item.EncryptedContent != "" {
				losses = append(losses, loss(
					fmt.Sprintf("output[%d].encrypted_content", i), "encrypted_content", ir.LossUnmappedField,
					"Responses reasoning encrypted_content has no IR equivalent in v2.",
				))
			}
		case ItemTypeMessage:
			for j, part := range item.Content {
				if part.Type != PartTypeOutputText {
					losses = append(losses, loss(
						fmt.Sprintf("output[%d].content[%d]", i, j), "type", ir.LossUnsupportedSemantic,
						fmt.Sprintf("Responses output content type %q has no IR equivalent", part.Type),
					))
					continue
				}
				cites, err := decodeResponsesAnnotations(part.Annotations, fmt.Sprintf("output[%d].content[%d].annotations", i, j))
				if err != nil {
					return nil, nil, err
				}
				texts = append(texts, ir.TextBlock{Text: part.Text, Citations: cites})
			}
		case ItemTypeWebSearchCall:
			block, callLosses, err := decodeWebSearchCall(item.ID, item.Status, item.Action, item.Extra, fmt.Sprintf("output[%d]", i))
			{
				if err != nil {
					return nil, nil, err
				}
				losses = append(losses, callLosses...)
				calls = append(calls, block)
			}
		case ItemTypeFunctionCall:
			input, err := wrapToolArguments(item.Arguments)
			if err != nil {
				return nil, nil, fmt.Errorf("responses: output[%d].arguments: %w", i, err)
			}
			finalName := item.Name
			if item.Namespace != "" && !strings.HasPrefix(finalName, item.Namespace+".") {
				finalName = item.Namespace + "." + finalName
			}
			calls = append(calls, ir.ToolUseBlock{ID: item.CallID, Name: finalName, Input: input})
			hasToolUse = true
		default:
			losses = append(losses, loss(
				fmt.Sprintf("output[%d]", i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses output item type %q has no IR equivalent", item.Type),
			))
		}
	}
	stop, stopLosses, err := decodeStatus(wire, hasToolUse)
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, stopLosses...)
	o := newOptions(opts...)
	// N-R-5: the IR content is ordered thinking blocks first, then text blocks,
	// then tool uses, regardless of how the wire interleaved the output items.
	content := make([]ir.Block, 0, len(thinkings)+len(texts)+len(calls))
	content = append(content, thinkings...)
	content = append(content, texts...)
	content = append(content, calls...)
	resp := &ir.Response{
		ID:         wire.ID,
		Model:      o.models.Map(wire.Model),
		Content:    content,
		StopReason: stop,
	}
	if wire.Usage != nil {
		resp.Usage = ir.Usage{
			InputTokens:  wire.Usage.InputTokens,
			OutputTokens: wire.Usage.OutputTokens,
		}
		if wire.Usage.InputTokenDetails != nil {
			cachedTokens := wire.Usage.InputTokenDetails.CachedTokens
			resp.Usage.InputTokensDetails = &ir.InputTokensDetails{
				CachedTokens: &cachedTokens,
			}
		}
		if wire.Usage.OutputTokenDetails != nil {
			reasoningTokens := wire.Usage.OutputTokenDetails.ReasoningTokens
			resp.Usage.OutputTokensDetails = &ir.OutputTokensDetails{
				ReasoningTokens: &reasoningTokens,
			}
		}
	}
	return resp, losses, nil
}

// decodeStatus maps the Responses status/incomplete_details/error triple to
// the IR stop reason (N-R-11).
func decodeStatus(wire *Response, hasToolUse bool) (ir.StopReason, []ir.Loss, error) {
	if wire.Error != nil {
		return ir.StopOther, []ir.Loss{loss(
			"error", "error", ir.LossUnsupportedSemantic,
			fmt.Sprintf("failed Responses response carries error %q: %s", wire.Error.Code, wire.Error.Message),
		)}, nil
	}
	switch wire.Status {
	case StatusCompleted:
		if hasToolUse {
			return ir.StopToolUse, nil, nil
		}
		return ir.StopEndTurn, nil, nil
	case StatusIncomplete:
		reason := ""
		if wire.IncompleteDetails != nil {
			reason = wire.IncompleteDetails.Reason
		}
		if reason == IncompleteReasonMaxOutputTokens {
			return ir.StopMaxTokens, nil, nil
		}
		return ir.StopOther, []ir.Loss{loss(
			"incomplete_details.reason", "reason", ir.LossUnmappedValue,
			fmt.Sprintf("Responses incomplete_details reason %q has no IR equivalent", reason),
		)}, nil
	case StatusFailed:
		return ir.StopOther, []ir.Loss{loss(
			"error", "error", ir.LossUnsupportedSemantic,
			"failed Responses response carries no error object",
		)}, nil
	default:
		return ir.StopOther, []ir.Loss{loss(
			"status", "status", ir.LossUnmappedValue,
			fmt.Sprintf("Responses status %q has no IR equivalent", wire.Status),
		)}, nil
	}
}

func responsesMetadata(v any) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}
	if metadata, ok := v.(map[string]string); ok {
		if len(metadata) == 0 {
			return nil, nil
		}
		out := make(map[string]string, len(metadata))
		for key, value := range metadata {
			if key == "" {
				return nil, fmt.Errorf("responses: metadata key cannot be empty")
			}
			out[key] = value
		}
		return out, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("responses: metadata must be an object")
	}
	if len(obj) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if key == "" {
			return nil, fmt.Errorf("responses: metadata key cannot be empty")
		}
		value, ok := obj[key].(string)
		if !ok {
			return nil, fmt.Errorf("responses: metadata.%s must be a string", key)
		}
		out[key] = value
	}
	return out, nil
}

func decodeResponsesAnnotations(raw []json.RawMessage, path string) ([]ir.TextCitation, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []ir.TextCitation
	for k, ann := range raw {
		trimmed := bytes.TrimSpace(ann)
		if len(trimmed) == 0 {
			return nil, fmt.Errorf("responses: %s[%d]: annotation cannot be empty", path, k)
		}
		if trimmed[0] != '{' {
			return nil, fmt.Errorf("responses: %s[%d]: annotation must be an object", path, k)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return nil, fmt.Errorf("responses: %s[%d]: annotation must be an object: %w", path, k, err)
		}
		typeVal, ok := obj["type"]
		if !ok {
			return nil, fmt.Errorf("responses: %s[%d]: annotation requires type field", path, k)
		}
		var typeStr string
		if err := json.Unmarshal(typeVal, &typeStr); err != nil || typeStr == "" {
			return nil, fmt.Errorf("responses: %s[%d]: annotation type must be a non-empty string", path, k)
		}
		keys := make([]string, 0, len(obj))
		for key := range obj {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(key)
			buf.Write(kb)
			buf.WriteByte(':')
			buf.Write(bytes.TrimSpace(obj[key]))
		}
		buf.WriteByte('}')

		out = append(out, ir.TextCitation{
			Source: ir.CitationSourceOpenAI,
			Value:  buf.String(),
		})
	}
	return out, nil
}
