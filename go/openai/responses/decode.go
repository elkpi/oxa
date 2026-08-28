package responses

import (
	"fmt"

	"github.com/oxa-protocol/oxa/go/ir"
)

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
		{"metadata", "metadata", len(wire.Metadata) > 0,
			"Responses request metadata has no IR equivalent in v1."},
		{"text.verbosity", "verbosity", wire.Text != nil && wire.Text.Verbosity != nil,
			"Responses output verbosity has no IR equivalent in v1."},
		{"text.format", "format", wire.Text != nil && wire.Text.Format != nil,
			"Responses text output format has no IR equivalent in v1."},
		{"reasoning", "reasoning", wire.Reasoning != nil,
			"Responses reasoning effort configuration has no IR equivalent in v1."},
		{"parallel_tool_calls", "parallel_tool_calls", wire.ParallelToolCalls != nil,
			"Responses parallel tool calls have no IR equivalent in v1."},
	} {
		if field.present {
			losses = append(losses, loss(field.path, field.field, ir.LossUnmappedField, field.detail))
		}
	}

	o := newOptions(opts...)
	req := &ir.Request{Model: o.models.Map(wire.Model)}

	// N-R-1: instructions render as the first system block; system items in
	// the input array follow in document order.
	if wire.Instructions != nil {
		req.System = append(req.System, ir.SystemBlock{Text: *wire.Instructions})
	}

	for i, tool := range wire.Tools {
		if tool.Type != "function" {
			losses = append(losses, loss(
				fmt.Sprintf("tools[%d]", i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses tool type %q has no IR equivalent", tool.Type),
			))
			continue
		}
		req.Tools = append(req.Tools, ir.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.Parameters,
		})
		if tool.Strict != nil {
			losses = append(losses, loss(
				fmt.Sprintf("tools[%d].strict", i), "strict", ir.LossUnmappedField,
				"Responses function tool strict mode has no IR equivalent in v1.",
			))
		}
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

	i := 0
	for i < len(wire.Input.Items) {
		item := wire.Input.Items[i]
		switch {
		case item.Type == "" || item.Type == "message":
			switch item.Role {
			case "system":
				content, contentLosses, err := decodeItemContent(item.Content, fmt.Sprintf("input[%d].content", i))
				if err != nil {
					return nil, nil, err
				}
				for j, block := range content {
					if text, ok := block.(ir.TextBlock); ok {
						req.System = append(req.System, ir.SystemBlock{Text: text.Text})
						continue
					}
					losses = append(losses, loss(
						fmt.Sprintf("input[%d].content[%d]", i, j), "content", ir.LossUnsupportedSemantic,
						fmt.Sprintf("IR %T cannot be rendered in the IR system field", block),
					))
				}
				losses = append(losses, contentLosses...)
				i++
			case "user":
				content, contentLosses, err := decodeItemContent(item.Content, fmt.Sprintf("input[%d].content", i))
				if err != nil {
					return nil, nil, err
				}
				losses = append(losses, contentLosses...)
				req.Messages = append(req.Messages, ir.Message{Role: ir.RoleUser, Content: content})
				i++
			case "assistant":
				merged, next, runLosses, err := decodeAssistantRun(wire.Input.Items, i)
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
		case item.Type == "function_call":
			merged, next, runLosses, err := decodeAssistantRun(wire.Input.Items, i)
			if err != nil {
				return nil, nil, err
			}
			if merged != nil {
				req.Messages = append(req.Messages, *merged)
			}
			losses = append(losses, runLosses...)
			i = next
		case item.Type == "function_call_output":
			merged, next, runLosses, err := decodeOutputRun(wire.Input.Items, i)
			if err != nil {
				return nil, nil, err
			}
			req.Messages = append(req.Messages, *merged)
			losses = append(losses, runLosses...)
			i = next
		default:
			losses = append(losses, loss(
				fmt.Sprintf("input[%d]", i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses input item type %q has no IR equivalent", item.Type),
			))
			i++
		}
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("responses: request carries no conversation input")
	}
	req.Params = ir.Params{
		Temperature: wire.Temperature,
		TopP:        wire.TopP,
		MaxTokens:   wire.MaxOutputTokens,
	}
	return req, losses, nil
}

// decodeAssistantRun converts a maximal run of assistant message items and
// function_call items into one IR assistant message (N-R-5): text blocks
// first (in document order), then ToolUseBlocks.
func decodeAssistantRun(items []InputItem, start int) (*ir.Message, int, []ir.Loss, error) {
	var blocks []ir.Block
	var losses []ir.Loss
	i := start
	for ; i < len(items); i++ {
		item := items[i]
		if item.Type == "function_call" {
			input, err := wrapToolArguments(item.Arguments)
			if err != nil {
				return nil, 0, nil, fmt.Errorf("responses: input[%d].arguments: %w", i, err)
			}
			blocks = append(blocks, ir.ToolUseBlock{ID: item.CallID, Name: item.Name, Input: input})
			continue
		}
		if item.Type != "" && item.Type != "message" {
			break
		}
		if item.Role != "assistant" {
			break
		}
		content, contentLosses, err := decodeItemContent(item.Content, fmt.Sprintf("input[%d].content", i))
		if err != nil {
			return nil, 0, nil, err
		}
		losses = append(losses, contentLosses...)
		blocks = append(blocks, content...)
	}
	if len(blocks) == 0 {
		return nil, i, losses, nil
	}
	return &ir.Message{Role: ir.RoleAssistant, Content: blocks}, i, losses, nil
}

// decodeOutputRun converts a maximal run of consecutive function_call_output
// items into one IR user message of ToolResultBlocks (N-R-6, INV-4).
func decodeOutputRun(items []InputItem, start int) (*ir.Message, int, []ir.Loss, error) {
	content := make([]ir.Block, 0)
	i := start
	for ; i < len(items) && items[i].Type == "function_call_output"; i++ {
		content = append(content, ir.ToolResultBlock{
			ToolUseID: items[i].CallID,
			Content:   []ir.Block{ir.TextBlock{Text: items[i].Output}},
		})
	}
	return &ir.Message{Role: ir.RoleUser, Content: content}, i, nil, nil
}

// decodeToolChoice maps the Responses tool_choice forms to the IR modes
// (N-R-9): required and IR mode any are equivalent, loss-free both ways.
func decodeToolChoice(value any) (*ir.ToolChoice, []ir.Loss) {
	if value == nil {
		return nil, nil
	}
	if choice, ok := value.(string); ok {
		switch choice {
		case "auto", "none":
			return &ir.ToolChoice{Mode: choice}, nil
		case "required":
			return &ir.ToolChoice{Mode: "any"}, nil
		default:
			return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses tool_choice %q has no IR equivalent", choice))}
		}
	}
	var kind, name string
	switch choice := value.(type) {
	case ToolChoiceWire:
		kind, name = choice.Type, choice.Name
	case *ToolChoiceWire:
		if choice == nil {
			return nil, nil
		}
		kind, name = choice.Type, choice.Name
	case map[string]any:
		kind, _ = choice["type"].(string)
		name, _ = choice["name"].(string)
	default:
		return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
			"Responses tool_choice has no IR equivalent")}
	}
	if kind == "function" && name != "" {
		return &ir.ToolChoice{Mode: "tool", Name: name}, nil
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
	case "auto", "none":
		return choice.Mode, nil
	case "any":
		return "required", nil
	case "tool":
		if choice.Name == "" {
			return nil, ptrLoss(loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
				"IR named tool choice has no function name"))
		}
		return ToolChoiceWire{Type: "function", Name: choice.Name}, nil
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
	var blocks []ir.Block
	hasToolUse := false
	for i, item := range wire.Output {
		switch item.Type {
		case "message":
			for j, part := range item.Content {
				if part.Type != "output_text" {
					losses = append(losses, loss(
						fmt.Sprintf("output[%d].content[%d]", i, j), "type", ir.LossUnsupportedSemantic,
						fmt.Sprintf("Responses output content type %q has no IR equivalent", part.Type),
					))
					continue
				}
				if len(part.Annotations) > 0 {
					losses = append(losses, loss(
						fmt.Sprintf("output[%d].content[%d].annotations", i, j), "annotations", ir.LossUnmappedField,
						"Responses output annotations have no IR equivalent in v1.",
					))
				}
				blocks = append(blocks, ir.TextBlock{Text: part.Text})
			}
		case "function_call":
			input, err := wrapToolArguments(item.Arguments)
			if err != nil {
				return nil, nil, fmt.Errorf("responses: output[%d].arguments: %w", i, err)
			}
			blocks = append(blocks, ir.ToolUseBlock{ID: item.CallID, Name: item.Name, Input: input})
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
	resp := &ir.Response{
		ID:         wire.ID,
		Model:      o.models.Map(wire.Model),
		Content:    blocks,
		StopReason: stop,
	}
	if wire.Usage != nil {
		resp.Usage = ir.Usage{
			InputTokens:  wire.Usage.InputTokens,
			OutputTokens: wire.Usage.OutputTokens,
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
	case "completed":
		if hasToolUse {
			return ir.StopToolUse, nil, nil
		}
		return ir.StopEndTurn, nil, nil
	case "incomplete":
		reason := ""
		if wire.IncompleteDetails != nil {
			reason = wire.IncompleteDetails.Reason
		}
		if reason == "max_output_tokens" {
			return ir.StopMaxTokens, nil, nil
		}
		return ir.StopOther, []ir.Loss{loss(
			"incomplete_details.reason", "reason", ir.LossUnmappedValue,
			fmt.Sprintf("Responses incomplete_details reason %q has no IR equivalent", reason),
		)}, nil
	case "failed":
		return ir.StopOther, []ir.Loss{loss(
			"error", "error", ir.LossUnsupportedSemantic,
			"failed Responses response carries no error object",
		)}, nil
	default:
		return "", nil, fmt.Errorf("responses: status %q has no IR equivalent", wire.Status)
	}
}
