package chatcompletions

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/elkpi/oxa/go/ir"
)

func loss(path, field string, reason ir.LossReason, detail string) ir.Loss {
	return ir.Loss{Path: path, Field: field, Reason: reason, Detail: detail}
}

// N-CC-1 normalizes Chat Completions string and parts-array content into IR
// blocks, dropping unknown parts as semantic losses.
func decodeContent(content any, path string) ([]ir.Block, []ir.Loss, error) {
	switch v := content.(type) {
	case nil:
		return []ir.Block{ir.TextBlock{Text: ""}}, nil, nil
	case string:
		return []ir.Block{ir.TextBlock{Text: v}}, nil, nil
	case []any:
		blocks := make([]ir.Block, 0, len(v))
		var losses []ir.Loss
		for i, raw := range v {
			part, ok := raw.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("chatcompletions: %s[%d]: part is not an object", path, i)
			}
			kind, ok := part["type"].(string)
			if !ok {
				return nil, nil, fmt.Errorf("chatcompletions: %s[%d].type: part type is not a string", path, i)
			}
			text, err := optionalString(part["text"], path+fmt.Sprintf("[%d].text", i))
			if err != nil {
				return nil, nil, err
			}
			imageURL, err := imageURLFromMap(part["image_url"], path+fmt.Sprintf("[%d].image_url", i))
			if err != nil {
				return nil, nil, err
			}
			decoded, partLosses := decodeContentPart(kind, text, imageURL, path, i)
			blocks = append(blocks, decoded...)
			losses = append(losses, partLosses...)
		}
		return blocks, losses, nil
	case []ContentPart:
		blocks := make([]ir.Block, 0, len(v))
		var losses []ir.Loss
		for i, part := range v {
			imageURL := ""
			if part.ImageURL != nil {
				imageURL = part.ImageURL.URL
			}
			decoded, partLosses := decodeContentPart(part.Type, part.Text, imageURL, path, i)
			blocks = append(blocks, decoded...)
			losses = append(losses, partLosses...)
		}
		return blocks, losses, nil
	default:
		return nil, nil, fmt.Errorf("chatcompletions: %s: content is neither string nor parts array", path)
	}
}

func optionalString(value any, path string) (string, error) {
	if value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("chatcompletions: %s: value is not a string", path)
	}
	return text, nil
}

func imageURLFromMap(value any, path string) (string, error) {
	if value == nil {
		return "", nil
	}
	image, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("chatcompletions: %s: image_url is not an object", path)
	}
	return optionalString(image["url"], path+".url")
}

func decodeContentPart(kind, text, imageURL, path string, index int) ([]ir.Block, []ir.Loss) {
	switch kind {
	case "text":
		return []ir.Block{ir.TextBlock{Text: text}}, nil
	case "image_url":
		image, imageLoss := decodeImageURL(imageURL, fmt.Sprintf("%s[%d].image_url", path, index))
		if imageLoss != nil {
			return nil, []ir.Loss{*imageLoss}
		}
		return []ir.Block{image}, nil
	default:
		return nil, []ir.Loss{loss(
			fmt.Sprintf("%s[%d]", path, index), "type", ir.LossUnsupportedSemantic,
			fmt.Sprintf("Chat Completions content part type %q has no IR equivalent", kind),
		)}
	}
}

// N-CC-2 normalizes supported https and data image URLs into ImageBlocks.
func decodeImageURL(raw, path string) (ir.ImageBlock, *ir.Loss) {
	if strings.HasPrefix(raw, "https:") {
		u, err := url.ParseRequestURI(raw)
		if err == nil && u.Scheme == "https" && u.Host != "" {
			return ir.ImageBlock{URL: raw}, nil
		}
		return ir.ImageBlock{}, ptrLoss(loss(path, "image_url", ir.LossUnsupportedSemantic,
			"malformed https image URL has no IR equivalent"))
	}
	if !strings.HasPrefix(raw, "data:") {
		return ir.ImageBlock{}, ptrLoss(loss(path, "image_url", ir.LossUnsupportedSemantic,
			"only https and base64 data image URLs are supported"))
	}
	metadata, data, found := strings.Cut(strings.TrimPrefix(raw, "data:"), ",")
	if !found || !strings.HasSuffix(metadata, ";base64") {
		return ir.ImageBlock{}, ptrLoss(loss(path, "image_url", ir.LossUnsupportedSemantic,
			"malformed data image URL has no IR equivalent"))
	}
	mediaType := strings.TrimSuffix(metadata, ";base64")
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") || mediaType == "image/" {
		return ir.ImageBlock{}, ptrLoss(loss(path, "image_url", ir.LossUnsupportedSemantic,
			"non-image data URL has no IR equivalent"))
	}
	return ir.ImageBlock{MediaType: mediaType, Data: data}, nil
}

func ptrLoss(value ir.Loss) *ir.Loss { return &value }

// N-CC-3 appends function tool calls after the assistant's normal content and
// wraps their opaque arguments text in the IR's raw JSON string token.
func decodeToolCalls(calls []ToolCall, path string) ([]ir.Block, []ir.Loss) {
	blocks := make([]ir.Block, 0, len(calls))
	var losses []ir.Loss
	for i, call := range calls {
		if call.Type != "function" {
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Chat Completions tool call type %q has no IR equivalent", call.Type),
			))
			continue
		}
		input, err := json.Marshal(call.Function.Arguments)
		if err != nil {
			// json.Marshal on a Go string cannot fail, but keep conversion total if
			// that implementation detail ever changes.
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d].function.arguments", path, i), "arguments", ir.LossUnsupportedSemantic,
				"tool arguments could not be represented as raw IR text",
			))
			continue
		}
		blocks = append(blocks, ir.ToolUseBlock{
			ID: call.ID, Name: call.Function.Name, Input: json.RawMessage(input),
		})
	}
	return blocks, losses
}

// N-CC-4 merges consecutive role:tool messages into one user message of
// ToolResultBlocks, as required by INV-4.
func decodeToolResultRun(messages []Message, start int) (ir.Message, int, []ir.Loss, error) {
	content := make([]ir.Block, 0)
	var losses []ir.Loss
	i := start
	for ; i < len(messages) && messages[i].Role == "tool"; i++ {
		blocks, blockLosses, err := decodeContent(messages[i].Content, fmt.Sprintf("messages[%d].content", i))
		if err != nil {
			return ir.Message{}, 0, nil, err
		}
		content = append(content, ir.ToolResultBlock{ToolUseID: messages[i].ToolCallID, Content: blocks})
		losses = append(losses, blockLosses...)
		if messages[i].FunctionCall != nil {
			losses = append(losses, loss(
				fmt.Sprintf("messages[%d].function_call", i), "function_call", ir.LossUnmappedField,
				"legacy Chat Completions function_call has no IR equivalent",
			))
		}
	}
	return ir.Message{Role: ir.RoleUser, Content: content}, i, losses, nil
}

// N-CC-5 maps the four Chat Completions tool_choice forms to the IR modes.
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
				fmt.Sprintf("Chat Completions tool_choice %q has no IR equivalent", choice))}
		}
	}
	var kind, name string
	switch choice := value.(type) {
	case ToolChoiceWire:
		kind, name = choice.Type, choice.Function.Name
	case *ToolChoiceWire:
		if choice == nil {
			return nil, nil
		}
		kind, name = choice.Type, choice.Function.Name
	case map[string]any:
		kind, _ = choice["type"].(string)
		function, _ := choice["function"].(map[string]any)
		if function != nil {
			name, _ = function["name"].(string)
		}
	default:
		return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
			"Chat Completions tool_choice has no IR equivalent")}
	}
	if kind == "function" && name != "" {
		return &ir.ToolChoice{Mode: "tool", Name: name}, nil
	}
	return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
		"only named function Chat Completions tool_choice values are supported")}
}

// N-CC-6 renders the IR's four tool-choice modes in Chat Completions form.
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
		out := ToolChoiceWire{Type: "function"}
		out.Function.Name = choice.Name
		return out, nil
	default:
		return nil, ptrLoss(loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
			fmt.Sprintf("IR tool_choice mode %q has no Chat Completions equivalent", choice.Mode)))
	}
}

// N-CC-7 renders ImageBlocks as the supported image_url content parts.
func encodeImageBlock(image ir.ImageBlock, path string) (ContentPart, *ir.Loss) {
	if image.Data != "" && image.URL != "" {
		return ContentPart{}, ptrLoss(loss(path, "image", ir.LossUnsupportedSemantic,
			"IR image contains both data and URL"))
	}
	if image.Data != "" {
		if !strings.HasPrefix(strings.ToLower(image.MediaType), "image/") || image.MediaType == "image/" {
			return ContentPart{}, ptrLoss(loss(path, "media_type", ir.LossUnsupportedSemantic,
				"IR image media type has no Chat Completions image_url equivalent"))
		}
		return ContentPart{Type: "image_url", ImageURL: &ImageURLWire{
			URL: "data:" + image.MediaType + ";base64," + image.Data,
		}}, nil
	}
	if image.URL != "" {
		u, err := url.ParseRequestURI(image.URL)
		if err == nil && u.Scheme == "https" && u.Host != "" {
			return ContentPart{Type: "image_url", ImageURL: &ImageURLWire{URL: image.URL}}, nil
		}
	}
	return ContentPart{}, ptrLoss(loss(path, "image", ir.LossUnsupportedSemantic,
		"IR image has no supported Chat Completions image_url equivalent"))
}

// N-CC-8 renders normal user content as a string when text-only and as a
// parts array when it contains images.
func encodeUserContent(blocks []ir.Block, path string) (any, []ir.Loss) {
	parts := make([]ContentPart, 0, len(blocks))
	text := ""
	hasImage := false
	var losses []ir.Loss
	for i, block := range blocks {
		switch value := block.(type) {
		case ir.TextBlock:
			parts = append(parts, ContentPart{Type: "text", Text: value.Text})
			text += value.Text
		case ir.ImageBlock:
			part, imageLoss := encodeImageBlock(value, fmt.Sprintf("%s[%d]", path, i))
			if imageLoss != nil {
				losses = append(losses, *imageLoss)
				continue
			}
			hasImage = true
			parts = append(parts, part)
		default:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Chat Completions user message", block),
			))
		}
	}
	if !hasImage {
		return text, losses
	}
	return parts, losses
}

// N-CC-9 renders assistant text and ToolUseBlocks, preserving the opaque
// function.arguments text while unwrapping only the IR JSON string envelope.
func encodeAssistantMessage(blocks []ir.Block, path string) (Message, []ir.Loss, error) {
	out := Message{Role: "assistant"}
	text := ""
	var losses []ir.Loss
	for i, block := range blocks {
		switch value := block.(type) {
		case ir.TextBlock:
			text += value.Text
		case ir.ToolUseBlock:
			var arguments string
			if err := json.Unmarshal(value.Input, &arguments); err != nil {
				return Message{}, nil, fmt.Errorf("chatcompletions: %s[%d].input: tool input is not a JSON string: %w", path, i, err)
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID: value.ID, Type: "function",
				Function: FunctionWire{Name: value.Name, Arguments: arguments},
			})
		case ir.ImageBlock, ir.ToolResultBlock:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Chat Completions assistant message", block),
			))
		default:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("unknown IR block %T cannot be rendered", block),
			))
		}
	}
	out.Content = text
	return out, losses, nil
}

// N-CC-10 renders ToolResultBlocks as role:tool messages and reports result
// blocks that Chat Completions cannot represent instead of panicking.
func encodeToolResult(result ir.ToolResultBlock, path string) (Message, []ir.Loss) {
	text := ""
	var losses []ir.Loss
	for i, block := range result.Content {
		switch value := block.(type) {
		case ir.TextBlock:
			text += value.Text
		default:
			losses = append(losses, loss(
				fmt.Sprintf("%s.content[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Chat Completions tool result", block),
			))
		}
	}
	if result.IsError {
		losses = append(losses, loss(path+".is_error", "is_error", ir.LossUnmappedField,
			"Chat Completions tool messages have no is_error field"))
	}
	return Message{Role: "tool", Content: text, ToolCallID: result.ToolUseID}, losses
}

func contentSystem(blocks []ir.Block, path string) ([]ir.SystemBlock, []ir.Loss) {
	out := make([]ir.SystemBlock, 0, len(blocks))
	var losses []ir.Loss
	for i, block := range blocks {
		if text, ok := block.(ir.TextBlock); ok {
			out = append(out, ir.SystemBlock(text))
			continue
		}
		losses = append(losses, loss(
			fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
			fmt.Sprintf("IR %T cannot be rendered in the IR system field", block),
		))
	}
	return out, losses
}
