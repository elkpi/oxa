package chatcompletions

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

func loss(path, field string, reason ir.LossReason, detail string) ir.Loss {
	return ir.Loss{Path: path, Field: field, Reason: reason, Detail: detail}
}

// N-CC-1 normalizes Chat Completions string and parts-array content into IR
// blocks, dropping unknown parts as semantic losses.
func decodeContent(content any, path string, isUser ...bool) ([]ir.Block, []ir.Loss, error) {
	user := false
	if len(isUser) > 0 {
		user = isUser[0]
	}
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
			var pcbAnn *ir.NativeAnnotation
			if rawPCB, exists := part["prompt_cache_breakpoint"]; exists {
				pcbMap, ok := rawPCB.(map[string]any)
				if !ok {
					return nil, nil, fmt.Errorf("chatcompletions: %s[%d].prompt_cache_breakpoint must be an object", path, i)
				}
				if err := ir.ValidatePromptCacheBreakpointObject(pcbMap); err != nil {
					return nil, nil, fmt.Errorf("chatcompletions: %s[%d].%w", path, i, err)
				}
				b, _ := json.Marshal(pcbMap)
				pcbAnn = &ir.NativeAnnotation{
					Source: ir.NativeSourceOpenAI,
					Kind:   ir.NativeKindPromptCacheBreakpoint,
					Value:  string(b),
					Omission: &ir.NativeOmission{
						Field:  "prompt_cache_breakpoint",
						Reason: ir.LossUnmappedField,
						Detail: ir.PromptCacheBreakpointLossDetail,
					},
				}
			}
			if kind == ContentPartTypeFile {
				if !user {
					losses = append(losses, loss(
						fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
						"Chat Completions content part type \"file\" has no IR equivalent",
					))
					continue
				}
				doc, fileLosses, err := decodeFilePart(part["file"], fmt.Sprintf("%s[%d].file", path, i))
				if err != nil {
					return nil, nil, err
				}
				if pcbAnn != nil {
					doc.Native = append(doc.Native, *pcbAnn)
				}
				blocks = append(blocks, doc)
				losses = append(losses, fileLosses...)
				continue
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
			if pcbAnn != nil && len(decoded) > 0 {
				switch db := decoded[0].(type) {
				case ir.TextBlock:
					db.Native = append(db.Native, *pcbAnn)
					decoded[0] = db
				case ir.ImageBlock:
					db.Native = append(db.Native, *pcbAnn)
					decoded[0] = db
				}
			}
			blocks = append(blocks, decoded...)
			losses = append(losses, partLosses...)
		}
		return blocks, losses, nil
	case []ContentPart:
		blocks := make([]ir.Block, 0, len(v))
		var losses []ir.Loss
		for i, part := range v {
			var pcbAnn *ir.NativeAnnotation
			if len(part.PromptCacheBreakpoint) > 0 {
				trimmed := strings.TrimSpace(string(part.PromptCacheBreakpoint))
				if len(trimmed) == 0 || trimmed[0] != '{' {
					return nil, nil, fmt.Errorf("chatcompletions: %s[%d].prompt_cache_breakpoint must be an object", path, i)
				}
				var pcbMap map[string]any
				if err := json.Unmarshal(part.PromptCacheBreakpoint, &pcbMap); err != nil || pcbMap == nil {
					return nil, nil, fmt.Errorf("chatcompletions: %s[%d].prompt_cache_breakpoint must be an object", path, i)
				}
				if err := ir.ValidatePromptCacheBreakpointObject(pcbMap); err != nil {
					return nil, nil, fmt.Errorf("chatcompletions: %s[%d].%w", path, i, err)
				}
				pcbAnn = &ir.NativeAnnotation{
					Source: ir.NativeSourceOpenAI,
					Kind:   ir.NativeKindPromptCacheBreakpoint,
					Value:  trimmed,
					Omission: &ir.NativeOmission{
						Field:  "prompt_cache_breakpoint",
						Reason: ir.LossUnmappedField,
						Detail: ir.PromptCacheBreakpointLossDetail,
					},
				}
			}
			if part.Type == ContentPartTypeFile {
				if !user {
					losses = append(losses, loss(
						fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
						"Chat Completions content part type \"file\" has no IR equivalent",
					))
					continue
				}
				doc, fileLosses, err := decodeFilePart(part.File, fmt.Sprintf("%s[%d].file", path, i))
				if err != nil {
					return nil, nil, err
				}
				if pcbAnn != nil {
					doc.Native = append(doc.Native, *pcbAnn)
				}
				blocks = append(blocks, doc)
				losses = append(losses, fileLosses...)
				continue
			}
			imageURL := ""
			if part.ImageURL != nil {
				imageURL = part.ImageURL.URL
			}
			decoded, partLosses := decodeContentPart(part.Type, part.Text, imageURL, path, i)
			if pcbAnn != nil && len(decoded) > 0 {
				switch db := decoded[0].(type) {
				case ir.TextBlock:
					db.Native = append(db.Native, *pcbAnn)
					decoded[0] = db
				case ir.ImageBlock:
					db.Native = append(db.Native, *pcbAnn)
					decoded[0] = db
				}
			}
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

func decodeFileData(raw, path string) (string, string, error) {
	if strings.HasPrefix(raw, "data:") {
		rest := strings.TrimPrefix(raw, "data:")
		idx := strings.Index(rest, ";base64,")
		if idx == -1 {
			return "", "", fmt.Errorf("chatcompletions: %s: malformed data file URL", path)
		}
		mt := rest[:idx]
		if mt == "" {
			mt = "application/pdf"
		}
		payload := rest[idx+len(";base64,"):]
		return payload, mt, nil
	}
	return raw, "application/pdf", nil
}

func decodeFilePart(fileVal any, path string) (ir.DocumentBlock, []ir.Loss, error) {
	if fileVal == nil {
		return ir.DocumentBlock{}, nil, fmt.Errorf("chatcompletions: %s: file part missing file object", path)
	}
	var fMap map[string]any
	switch f := fileVal.(type) {
	case *FileWire:
		if f == nil {
			return ir.DocumentBlock{}, nil, fmt.Errorf("chatcompletions: %s: file part missing file object", path)
		}
		fMap = map[string]any{}
		if f.FileID != nil {
			fMap["file_id"] = *f.FileID
		}
		if f.FileData != nil {
			fMap["file_data"] = *f.FileData
		}
		if f.Filename != nil {
			fMap["filename"] = *f.Filename
		}
	case FileWire:
		fMap = map[string]any{}
		if f.FileID != nil {
			fMap["file_id"] = *f.FileID
		}
		if f.FileData != nil {
			fMap["file_data"] = *f.FileData
		}
		if f.Filename != nil {
			fMap["filename"] = *f.Filename
		}
	case map[string]any:
		fMap = f
	default:
		return ir.DocumentBlock{}, nil, fmt.Errorf("chatcompletions: %s: file must be an object", path)
	}

	var fileID, fileData string
	var count int
	if rawID, ok := fMap["file_id"]; ok && rawID != nil {
		str, ok := rawID.(string)
		if !ok || str == "" {
			return ir.DocumentBlock{}, nil, fmt.Errorf("chatcompletions: %s.file_id must be a non-empty string", path)
		}
		fileID = str
		count++
	}
	if rawData, ok := fMap["file_data"]; ok && rawData != nil {
		str, ok := rawData.(string)
		if !ok || str == "" {
			return ir.DocumentBlock{}, nil, fmt.Errorf("chatcompletions: %s.file_data must be a non-empty string", path)
		}
		fileData = str
		count++
	}
	if count != 1 {
		return ir.DocumentBlock{}, nil, fmt.Errorf("chatcompletions: %s must specify exactly one of file_id or file_data", path)
	}

	var losses []ir.Loss
	if rawFilename, ok := fMap["filename"]; ok && rawFilename != nil {
		if _, ok := rawFilename.(string); !ok {
			return ir.DocumentBlock{}, nil, fmt.Errorf("chatcompletions: %s.filename must be a string", path)
		}
		losses = append(losses, loss(
			fmt.Sprintf("%s.filename", path), "filename", ir.LossUnmappedField,
			"Chat Completions file filename has no IR equivalent",
		))
	}

	if fileID != "" {
		return ir.DocumentBlock{FileID: fileID}, losses, nil
	}
	data, mt, err := decodeFileData(fileData, path+".file_data")
	if err != nil {
		return ir.DocumentBlock{}, nil, err
	}
	return ir.DocumentBlock{Data: data, MediaType: mt}, losses, nil
}

func decodeContentPart(kind, text, imageURL, path string, index int) ([]ir.Block, []ir.Loss) {
	switch kind {
	case ContentPartTypeText:
		return []ir.Block{ir.TextBlock{Text: text}}, nil
	case ContentPartTypeImageURL:
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

// N-CC-3 appends function and custom tool calls after the assistant's normal content and
// wraps their opaque arguments text in the IR's raw JSON string token.
func decodeToolCalls(calls []ToolCall, path string) ([]ir.Block, []ir.Loss) {
	blocks := make([]ir.Block, 0, len(calls))
	var losses []ir.Loss
	for i, call := range calls {
		if call.Type == ir.ToolTypeCustom {
			if call.Custom == nil {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d]", path, i), "custom", ir.LossUnsupportedSemantic,
					"custom tool call missing custom definition",
				))
				continue
			}
			input, err := json.Marshal(call.Custom.Input)
			if err != nil {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d].custom.input", path, i), "input", ir.LossUnsupportedSemantic,
					"custom input could not be represented as raw IR text",
				))
				continue
			}
			blocks = append(blocks, ir.CustomToolUseBlock{
				ID: call.ID, Name: call.Custom.Name, Input: json.RawMessage(input),
			})
			continue
		}
		if call.Type != ToolTypeFunction {
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Chat Completions tool call type %q has no IR equivalent", call.Type),
			))
			continue
		}
		if call.Function.Name == "" {
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "function", ir.LossUnsupportedSemantic,
				"function tool call missing function definition",
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
// ToolResultBlocks or CustomToolResultBlocks, as required by INV-4.
func decodeToolResultRun(messages []Message, start int, customCallIDs map[string]bool) (ir.Message, int, []ir.Loss, error) {
	content := make([]ir.Block, 0)
	var losses []ir.Loss
	i := start
	for ; i < len(messages) && messages[i].Role == RoleTool; i++ {
		toolCallID := messages[i].ToolCallID
		if customCallIDs != nil && customCallIDs[toolCallID] {
			if str, ok := messages[i].Content.(string); ok {
				content = append(content, ir.CustomToolResultBlock{
					ToolUseID: toolCallID,
					Output:    ir.CustomToolOutput{Text: &str},
				})
			} else {
				blocks, blockLosses, err := decodeContent(messages[i].Content, fmt.Sprintf("messages[%d].content", i))
				if err != nil {
					return ir.Message{}, 0, nil, err
				}
				content = append(content, ir.CustomToolResultBlock{
					ToolUseID: toolCallID,
					Output:    ir.CustomToolOutput{Content: blocks},
				})
				losses = append(losses, blockLosses...)
			}
		} else {
			blocks, blockLosses, err := decodeContent(messages[i].Content, fmt.Sprintf("messages[%d].content", i))
			if err != nil {
				return ir.Message{}, 0, nil, err
			}
			content = append(content, ir.ToolResultBlock{ToolUseID: toolCallID, Content: blocks})
			losses = append(losses, blockLosses...)
		}
		if messages[i].FunctionCall != nil {
			losses = append(losses, loss(
				fmt.Sprintf("messages[%d].function_call", i), "function_call", ir.LossUnmappedField,
				"legacy Chat Completions function_call has no IR equivalent",
			))
		}
	}
	return ir.Message{Role: ir.RoleUser, Content: content}, i, losses, nil
}

// N-CC-5 maps the Chat Completions tool_choice forms to the IR modes.
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
				fmt.Sprintf("Chat Completions tool_choice %q has no IR equivalent", choice))}
		}
	}
	var kind, name string
	switch choice := value.(type) {
	case ToolChoiceWire:
		kind = choice.Type
		if choice.Type == ir.ToolTypeCustom && choice.Custom != nil {
			name = choice.Custom.Name
		} else {
			name = choice.Function.Name
		}
	case *ToolChoiceWire:
		if choice == nil {
			return nil, nil
		}
		kind = choice.Type
		if choice.Type == ir.ToolTypeCustom && choice.Custom != nil {
			name = choice.Custom.Name
		} else {
			name = choice.Function.Name
		}
	case map[string]any:
		kind, _ = choice["type"].(string)
		if kind == ir.ToolTypeCustom {
			if cust, ok := choice["custom"].(map[string]any); ok {
				name, _ = cust["name"].(string)
			}
		} else if function, ok := choice["function"].(map[string]any); ok {
			name, _ = function["name"].(string)
		}
	default:
		return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
			"Chat Completions tool_choice has no IR equivalent")}
	}
	if kind == ir.ToolTypeCustom && name != "" {
		return &ir.ToolChoice{Mode: ir.ToolChoiceTool, ToolType: ir.ToolTypeCustom, Name: name}, nil
	}
	if kind == ToolTypeFunction && name != "" {
		return &ir.ToolChoice{Mode: ir.ToolChoiceTool, Name: name}, nil
	}
	return nil, []ir.Loss{loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
		"only named function or custom Chat Completions tool_choice values are supported")}
}

// N-CC-6 renders the IR's tool-choice modes in Chat Completions form.
func encodeToolChoice(choice *ir.ToolChoice) (any, *ir.Loss) {
	if choice == nil {
		return nil, nil
	}
	if choice.ToolType == ir.ToolTypeApplyPatch {
		return nil, ptrLoss(loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic, "tool choice is not implemented by this profile"))
	}
	if choice.ToolType == ir.ToolTypeCustom {
		if choice.Name == "" {
			return nil, ptrLoss(loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic, "IR named custom tool choice has no tool name"))
		}
		out := ToolChoiceWire{Type: ir.ToolTypeCustom}
		out.Custom = &struct {
			Name string `json:"name"`
		}{Name: choice.Name}
		return out, nil
	}
	switch choice.Mode {
	case ir.ToolChoiceAuto, ir.ToolChoiceNone:
		return choice.Mode, nil
	case ir.ToolChoiceAny:
		return ToolChoiceRequired, nil
	case ir.ToolChoiceTool:
		if choice.Name == "" {
			return nil, ptrLoss(loss("tool_choice", "tool_choice", ir.LossUnsupportedSemantic,
				"IR named tool choice has no function name"))
		}
		out := ToolChoiceWire{Type: ToolTypeFunction}
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
		return ContentPart{Type: ContentPartTypeImageURL, ImageURL: &ImageURLWire{
			URL: "data:" + image.MediaType + ";base64," + image.Data,
		}}, nil
	}
	if image.URL != "" {
		u, err := url.ParseRequestURI(image.URL)
		if err == nil && u.Scheme == "https" && u.Host != "" {
			return ContentPart{Type: ContentPartTypeImageURL, ImageURL: &ImageURLWire{URL: image.URL}}, nil
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
			part := ContentPart{Type: ContentPartTypeText, Text: value.Text}
			for _, ann := range value.Native {
				if ann.Source == ir.NativeSourceOpenAI && ann.Kind == ir.NativeKindPromptCacheBreakpoint {
					part.PromptCacheBreakpoint = json.RawMessage(ann.Value)
					hasImage = true
					break
				}
			}
			parts = append(parts, part)
			text += value.Text
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
		case ir.ImageBlock:
			part, imageLoss := encodeImageBlock(value, fmt.Sprintf("%s[%d]", path, i))
			if imageLoss != nil {
				losses = append(losses, *imageLoss)
				continue
			}
			hasImage = true
			for _, ann := range value.Native {
				if ann.Source == ir.NativeSourceOpenAI && ann.Kind == ir.NativeKindPromptCacheBreakpoint {
					part.PromptCacheBreakpoint = json.RawMessage(ann.Value)
					break
				}
			}
			parts = append(parts, part)
		case ir.DocumentBlock:
			var sCount int
			if value.FileID != "" {
				sCount++
			}
			if value.URL != "" {
				sCount++
			}
			if value.Data != "" {
				sCount++
			}
			if sCount == 0 {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
					"IR DocumentBlock carries no supported file source",
				))
				continue
			}
			if sCount > 1 {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
					"IR DocumentBlock carries conflicting file sources",
				))
				continue
			}
			if value.URL != "" {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
					"Chat Completions file content parts do not support url sources; file dropped",
				))
				continue
			}
			if value.Data != "" && value.MediaType != "" && value.MediaType != "application/pdf" {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
					"Chat Completions file content parts do not support non-PDF media types; file dropped",
				))
				continue
			}
			hasImage = true
			var pcb json.RawMessage
			for _, ann := range value.Native {
				if ann.Source == ir.NativeSourceOpenAI && ann.Kind == ir.NativeKindPromptCacheBreakpoint {
					pcb = json.RawMessage(ann.Value)
					break
				}
			}
			if value.FileID != "" {
				parts = append(parts, ContentPart{
					Type:                  ContentPartTypeFile,
					File:                  &FileWire{FileID: &value.FileID},
					PromptCacheBreakpoint: pcb,
				})
			} else {
				parts = append(parts, ContentPart{
					Type:                  ContentPartTypeFile,
					File:                  &FileWire{FileData: &value.Data},
					PromptCacheBreakpoint: pcb,
				})
			}
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
	out := Message{Role: RoleAssistant}
	text := ""
	hasText := false
	var losses []ir.Loss
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
		case ir.ThinkingBlock:
			out.ReasoningContent += value.Thinking
			if value.Signature != "" {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d].signature", path, i), "signature", ir.LossUnmappedField,
					"Chat Completions carries no signature field on reasoning_content",
				))
			}
		case ir.RedactedThinkingBlock:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
				"anthropic encrypted thinking replay state; the target face cannot preserve this block",
			))
		case ir.ToolUseBlock:
			var arguments string
			if err := json.Unmarshal(value.Input, &arguments); err != nil {
				return Message{}, nil, fmt.Errorf("chatcompletions: %s[%d].input: tool input is not a JSON string: %w", path, i, err)
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID: value.ID, Type: ToolTypeFunction,
				Function: FunctionWire{Name: value.Name, Arguments: arguments},
			})
		case ir.CustomToolUseBlock:
			var inputStr string
			if err := json.Unmarshal(value.Input, &inputStr); err != nil {
				inputStr = string(value.Input)
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:   value.ID,
				Type: ir.ToolTypeCustom,
				Custom: &CustomCallWire{
					Name:  value.Name,
					Input: inputStr,
				},
			})
		case ir.WebSearchCallBlock:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
				"Responses web_search_call has no Chat Completions equivalent",
			))
		case ir.ApplyPatchToolUseBlock:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
				fmt.Sprintf("IR %T cannot be rendered in a Chat Completions assistant message", block),
			))
		case ir.RefusalBlock:
			if out.Refusal == "" {
				out.Refusal = value.Refusal
				if value.Category != "" {
					losses = append(losses, loss(
						fmt.Sprintf("%s[%d].category", path, i), "category", ir.LossUnsupportedSemantic,
						"Anthropic refusal category has no Chat Completions equivalent",
					))
				}
			} else {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d]", path, i), "refusal", ir.LossUnsupportedSemantic,
					"Chat Completions supports only a single refusal; subsequent refusal block dropped",
				))
			}
		case ir.ImageBlock, ir.ToolResultBlock, ir.DocumentBlock:
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
	if !hasText && out.Refusal != "" {
		out.Content = nil
	} else {
		out.Content = text
	}
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
	return Message{Role: RoleTool, Content: text, ToolCallID: result.ToolUseID}, losses
}

func contentSystem(blocks []ir.Block, path string) ([]ir.SystemBlock, []ir.Loss) {
	out := make([]ir.SystemBlock, 0, len(blocks))
	var losses []ir.Loss
	for i, block := range blocks {
		if text, ok := block.(ir.TextBlock); ok {
			out = append(out, ir.SystemBlock{Text: text.Text, Native: text.Native})
			continue
		}
		losses = append(losses, loss(
			fmt.Sprintf("%s[%d]", path, i), "content", ir.LossUnsupportedSemantic,
			fmt.Sprintf("IR %T cannot be rendered in the IR system field", block),
		))
	}
	return out, losses
}
