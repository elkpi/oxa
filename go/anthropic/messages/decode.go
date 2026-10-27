package messages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

// DecodeRequest converts an Anthropic Messages wire request to the IR (face
// -> IR). The mapping is near-identity: system (string or block array) becomes
// ir.System, required max_tokens becomes Params.MaxTokens, and
// temperature/top_p/stop_sequences map 1:1. cache_control annotations have no
// IR equivalent in v1 and are dropped with unmapped-field losses.
func DecodeRequest(wire *Request, opts ...Option) (*ir.Request, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("anthropic: nil request")
	}
	var losses []ir.Loss
	if wire.MaxTokens <= 0 {
		return nil, nil, fmt.Errorf("anthropic: max_tokens is required and must be positive")
	}
	if wire.Metadata != nil {
		// Anthropic's wire metadata is the specific {user_id} semantic, not
		// an arbitrary string map, so it is dropped symmetrically with a
		// single unmapped-field loss (spec/12 will pin this in the loss
		// catalog).
		losses = append(losses, ir.Loss{
			Path:   "metadata",
			Field:  "metadata",
			Reason: ir.LossUnmappedField,
			Detail: "Anthropic request metadata (user_id) has no IR equivalent in v1.",
		})
	}
	o := newOptions(opts...)
	maxTokens := wire.MaxTokens
	req := &ir.Request{
		Model:  o.models.Map(wire.Model),
		Params: ir.Params{MaxTokens: &maxTokens},
	}
	declaredTools := make(map[string]bool)
	customTools := make(map[string]bool)
	nativeTools := make(map[string]bool)
	for i, tool := range wire.Tools {
		declaredTools[tool.Name] = true
		if tool.Type != "" && tool.Type != ir.ToolTypeFunction && tool.Type != ir.ToolTypeCustom {
			rawDef := tool.Raw
			if len(rawDef) == 0 {
				var err error
				rawDef, err = json.Marshal(tool)
				if err != nil {
					return nil, nil, fmt.Errorf("anthropic: tools[%d]: %w", i, err)
				}
			}
			nativeTools[tool.Name] = true
			req.Tools = append(req.Tools, ir.Tool{
				Type:       ir.ToolTypeNative,
				Name:       tool.Name,
				Source:     "anthropic",
				Definition: string(rawDef),
			})
			continue
		}
		if err := requireJSONObject(tool.InputSchema, fmt.Sprintf("tools[%d].input_schema", i)); err != nil {
			return nil, nil, err
		}
		if len(tool.rawDeferLoading) > 0 {
			trimmed := strings.TrimSpace(string(tool.rawDeferLoading))
			if trimmed != "true" && trimmed != "false" {
				return nil, nil, fmt.Errorf("anthropic: tools[%d].defer_loading must be a boolean", i)
			}
		}
		if len(tool.rawEager) > 0 {
			trimmed := strings.TrimSpace(string(tool.rawEager))
			if trimmed != "true" && trimmed != "false" {
				return nil, nil, fmt.Errorf("anthropic: tools[%d].eager_input_streaming must be a boolean", i)
			}
		}
		var nat []ir.NativeAnnotation
		if tool.DeferLoading != nil && *tool.DeferLoading {
			if len(tool.CacheControl) > 0 {
				return nil, nil, fmt.Errorf("anthropic: tools[%d]: cannot specify both defer_loading and cache_control", i)
			}
			nat = append(nat, ir.NativeAnnotation{
				Source:   ir.NativeSourceAnthropic,
				Kind:     ir.NativeKindDeferLoading,
				Value:    "true",
				Omission: &ir.NativeOmission{Field: "defer_loading", Reason: ir.LossDegraded, Detail: ir.DeferLoadingLossDetail},
			})
		}
		if len(tool.CacheControl) > 0 {
			if tool.DeferLoading != nil && *tool.DeferLoading {
				return nil, nil, fmt.Errorf("anthropic: tools[%d]: cannot specify both defer_loading and cache_control", i)
			}
			canon, l, ok := parseCacheControl(tool.CacheControl, fmt.Sprintf("tools[%d].cache_control", i))
			if l != nil {
				losses = append(losses, *l)
			}
			if ok {
				nat = append(nat, ir.NativeAnnotation{
					Source:   ir.NativeSourceAnthropic,
					Kind:     ir.NativeKindCacheControl,
					Value:    canon,
					Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
				})
			}
		}
		if tool.EagerInputStreaming != nil {
			val := "false"
			var om *ir.NativeOmission
			if *tool.EagerInputStreaming {
				val = "true"
				om = &ir.NativeOmission{
					Field:  "eager_input_streaming",
					Reason: ir.LossDegraded,
					Detail: ir.EagerInputStreamingLossDetail,
				}
			}
			nat = append(nat, ir.NativeAnnotation{
				Source:   ir.NativeSourceAnthropic,
				Kind:     ir.NativeKindEagerInputStreaming,
				Value:    val,
				Omission: om,
			})
		}
		if tool.Strict == nil && isCustomToolInputSchema(tool.InputSchema) {
			customTools[tool.Name] = true
			req.Tools = append(req.Tools, ir.Tool{
				Type:        ir.ToolTypeCustom,
				Name:        tool.Name,
				Description: tool.Description,
				Native:      nat,
			})
			continue
		}
		req.Tools = append(req.Tools, ir.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Strict:      tool.Strict,
			Native:      nat,
		})
	}
	choice, choiceLosses, err := decodeToolChoice(wire.ToolChoice, customTools, nativeTools)
	if err != nil {
		return nil, nil, err
	}
	req.ToolChoice = choice
	losses = append(losses, choiceLosses...)
	if wire.ToolChoice != nil && (wire.ToolChoice.HasDisableParallelToolUse || wire.ToolChoice.DisableParallelToolUse) {
		val := "false"
		if wire.ToolChoice.DisableParallelToolUse {
			val = "true"
		}
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source:   ir.NativeSourceAnthropic,
			Kind:     ir.NativeKindDisableParallelToolUse,
			Value:    val,
			Omission: &ir.NativeOmission{Field: "disable_parallel_tool_use", Reason: ir.LossUnmappedField},
		})
	}

	system, sysLosses, err := decodeSystem(wire.System)
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, sysLosses...)
	req.System = system

	customCallIDs := make(map[string]bool)
	for _, m := range wire.Messages {
		if m.Role == RoleAssistant {
			if blocks, ok := m.Content.([]BlockWire); ok {
				for _, b := range blocks {
					if b.Type == BlockTypeToolUse {
						if customTools[b.Name] {
							customCallIDs[b.ID] = true
						} else if declaredTools[b.Name] {
							// Declared as function tool; do not promote to custom
						} else if _, ok := extractCustomInput(b.Input); ok {
							customCallIDs[b.ID] = true
						}
					}
				}
			} else if rawList, ok := m.Content.([]any); ok {
				for _, item := range rawList {
					if mp, ok := item.(map[string]any); ok && mp["type"] == BlockTypeToolUse {
						name, _ := mp["name"].(string)
						id, _ := mp["id"].(string)
						if customTools[name] {
							customCallIDs[id] = true
						} else if declaredTools[name] {
							// Declared as function tool; do not promote to custom
						} else if in, ok := mp["input"].(map[string]any); ok && len(in) == 1 {
							if _, ok := in["input"].(string); ok {
								customCallIDs[id] = true
							}
						}
					}
				}
			}
		}
	}

	if err := validateSystemMessagePlacement(wire.Messages); err != nil {
		return nil, nil, err
	}

	for i, m := range wire.Messages {
		if m.Role == RoleSystem {
			losses = append(losses, ir.Loss{
				Path:   fmt.Sprintf("messages[%d].role", i),
				Field:  "role",
				Reason: ir.LossDegraded,
				Detail: "Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved.",
			})
			sysBlocks, sysBlockLosses, err := decodeSystemMessageContent(m.Content, i)
			if err != nil {
				return nil, nil, err
			}
			req.System = append(req.System, sysBlocks...)
			losses = append(losses, sysBlockLosses...)
			continue
		}

		var role ir.Role
		switch m.Role {
		case RoleUser:
			role = ir.RoleUser
		case RoleAssistant:
			role = ir.RoleAssistant
		default:
			return nil, nil, fmt.Errorf("anthropic: messages[%d]: unknown role %q", i, m.Role)
		}
		blocks, blockLosses, err := decodeBlocks(m.Content, fmt.Sprintf("messages[%d].content", i), customCallIDs, declaredTools, customTools, true)
		if err != nil {
			return nil, nil, err
		}
		if role == ir.RoleUser {
			for j, b := range blocks {
				switch blk := b.(type) {
				case ir.RedactedThinkingBlock:
					return nil, nil, fmt.Errorf("anthropic: messages[%d].content[%d]: redacted_thinking is not permitted in user messages", i, j)
				case ir.TextBlock:
					if len(blk.Citations) > 0 {
						return nil, nil, fmt.Errorf("anthropic: messages[%d].content[%d].citations: citations are not permitted on user text blocks", i, j)
					}
				}
			}
		}
		losses = append(losses, blockLosses...)
		req.Messages = append(req.Messages, ir.Message{Role: role, Content: blocks})
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("anthropic: request carries no messages")
	}

	natLosses, err := decodeRequestNative(wire, req)
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, natLosses...)

	if wire.Temperature != nil {
		req.Params.Temperature = wire.Temperature
	}
	if wire.TopP != nil {
		req.Params.TopP = wire.TopP
	}
	if len(wire.StopSequences) > 0 {
		req.Params.StopSequences = wire.StopSequences
	}
	return req, losses, nil
}

func decodeSystem(system any) ([]ir.SystemBlock, []ir.Loss, error) {
	if system == nil {
		return nil, nil, nil
	}
	switch v := system.(type) {
	case string:
		return []ir.SystemBlock{{Text: v}}, nil, nil
	case []any:
		blocks := make([]ir.SystemBlock, 0, len(v))
		var losses []ir.Loss
		for i, part := range v {
			raw, err := json.Marshal(part)
			if err != nil {
				return nil, nil, err
			}
			var w SystemBlockWire
			if err := json.Unmarshal(raw, &w); err != nil {
				return nil, nil, fmt.Errorf("anthropic: system[%d]: %w", i, err)
			}
			if w.Type != BlockTypeText {
				return nil, nil, fmt.Errorf("anthropic: system[%d]: unsupported block type %q", i, w.Type)
			}
			sb := ir.SystemBlock{Text: w.Text}
			if len(w.CacheControl) > 0 {
				canon, l, ok := parseCacheControl(w.CacheControl, fmt.Sprintf("system[%d].cache_control", i))
				if l != nil {
					losses = append(losses, *l)
				}
				if ok {
					sb.Native = []ir.NativeAnnotation{{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindCacheControl,
						Value:    canon,
						Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
					}}
				}
			}
			blocks = append(blocks, sb)
		}
		return blocks, losses, nil
	case []SystemBlockWire:
		blocks := make([]ir.SystemBlock, 0, len(v))
		var losses []ir.Loss
		for i, w := range v {
			if w.Type != BlockTypeText {
				return nil, nil, fmt.Errorf("anthropic: system[%d]: unsupported block type %q", i, w.Type)
			}
			sb := ir.SystemBlock{Text: w.Text}
			if len(w.CacheControl) > 0 {
				canon, l, ok := parseCacheControl(w.CacheControl, fmt.Sprintf("system[%d].cache_control", i))
				if l != nil {
					losses = append(losses, *l)
				}
				if ok {
					sb.Native = []ir.NativeAnnotation{{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindCacheControl,
						Value:    canon,
						Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
					}}
				}
			}
			blocks = append(blocks, sb)
		}
		return blocks, losses, nil
	default:
		return nil, nil, fmt.Errorf("anthropic: system is neither string nor block array")
	}
}

func isCustomToolInputSchema(raw json.RawMessage) bool {
	var obj struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	if obj.Type != "object" {
		return false
	}
	inputProp, ok := obj.Properties["input"]
	if !ok || inputProp.Type != "string" {
		return false
	}
	return len(obj.Properties) == 1
}

func extractCustomInput(raw json.RawMessage) (string, bool) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", false
	}
	if len(m) == 1 {
		if val, ok := m["input"].(string); ok {
			return val, true
		}
	}
	return "", false
}

func decodeBlocks(content any, path string, customCallIDs map[string]bool, declaredTools map[string]bool, customTools map[string]bool, isRequest bool) ([]ir.Block, []ir.Loss, error) {
	switch v := content.(type) {
	case nil:
		return nil, nil, fmt.Errorf("anthropic: %s is missing", path)
	case string:
		return []ir.Block{ir.TextBlock{Text: v}}, nil, nil
	case []any:
		blocks := make([]ir.Block, 0, len(v))
		var losses []ir.Loss
		for j, part := range v {
			raw, err := json.Marshal(part)
			if err != nil {
				return nil, nil, fmt.Errorf("anthropic: %s[%d]: %w", path, j, err)
			}
			var w BlockWire
			if err := json.Unmarshal(raw, &w); err != nil {
				return nil, nil, fmt.Errorf("anthropic: %s[%d]: %w", path, j, err)
			}
			block, blockLosses, mapped, err := decodeBlock(w, fmt.Sprintf("%s[%d]", path, j), customCallIDs, declaredTools, customTools, isRequest)
			if err != nil {
				return nil, nil, err
			}
			if mapped {
				if isRequest && len(w.CacheControl) > 0 {
					canon, l, ok := parseCacheControl(w.CacheControl, fmt.Sprintf("%s[%d].cache_control", path, j))
					if l != nil {
						losses = append(losses, *l)
					}
					if ok {
						attachCacheControlToBlock(&block, canon)
					}
				}
				blocks = append(blocks, block)
			}
			losses = append(losses, blockLosses...)
		}
		return blocks, losses, nil
	case []BlockWire:
		blocks := make([]ir.Block, 0, len(v))
		var losses []ir.Loss
		for j, w := range v {
			block, blockLosses, mapped, err := decodeBlock(w, fmt.Sprintf("%s[%d]", path, j), customCallIDs, declaredTools, customTools, isRequest)
			if err != nil {
				return nil, nil, err
			}
			if mapped {
				if isRequest && len(w.CacheControl) > 0 {
					canon, l, ok := parseCacheControl(w.CacheControl, fmt.Sprintf("%s[%d].cache_control", path, j))
					if l != nil {
						losses = append(losses, *l)
					}
					if ok {
						attachCacheControlToBlock(&block, canon)
					}
				}
				blocks = append(blocks, block)
			}
			losses = append(losses, blockLosses...)
		}
		return blocks, losses, nil
	default:
		return nil, nil, fmt.Errorf("anthropic: %s is neither string nor block array", path)
	}
}

func decodeBlock(w BlockWire, path string, customCallIDs map[string]bool, declaredTools map[string]bool, customTools map[string]bool, isRequest bool) (ir.Block, []ir.Loss, bool, error) {
	var block ir.Block
	var losses []ir.Loss
	switch w.Type {
	case BlockTypeText:
		cites, citeLosses, err := decodeTextCitations(w.Citations, path+".citations", isRequest)
		if err != nil {
			return nil, nil, false, err
		}
		losses = append(losses, citeLosses...)
		block = ir.TextBlock{Text: w.Text, Citations: cites}
	case BlockTypeThinking:
		block = ir.ThinkingBlock{Thinking: w.Thinking, Signature: w.Signature}
	case BlockTypeRedactedThinking:
		if !w.hasData {
			return nil, nil, false, fmt.Errorf("anthropic: %s.data is required", path)
		}
		block = ir.RedactedThinkingBlock{Data: w.Data}
	case BlockTypeImage:
		image, imageLoss, err := decodeImage(w.Source, path+".source")
		if err != nil {
			return nil, nil, false, err
		}
		if imageLoss != nil {
			return nil, []ir.Loss{*imageLoss}, false, nil
		}
		block = image
	case BlockTypeDocument:
		if !isRequest {
			return nil, []ir.Loss{{
				Path:   path,
				Field:  "type",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("Anthropic block type %q has no IR equivalent", w.Type),
			}}, false, nil
		}
		doc, docLoss, err := decodeDocument(w, path)
		if err != nil {
			return nil, nil, false, err
		}
		if docLoss != nil {
			return nil, []ir.Loss{*docLoss}, false, nil
		}
		block = doc
	case BlockTypeToolUse:
		if w.ID == "" {
			return nil, nil, false, fmt.Errorf("anthropic: %s.id is required", path)
		}
		if w.Name == "" {
			return nil, nil, false, fmt.Errorf("anthropic: %s.name is required", path)
		}
		if err := requireJSONObject(w.Input, path+".input"); err != nil {
			return nil, nil, false, err
		}
		if customTools != nil && customTools[w.Name] {
			input, _ := json.Marshal(w.Input)
			if customInput, ok := extractCustomInput(w.Input); ok {
				input, _ = json.Marshal(customInput)
			}
			block = ir.CustomToolUseBlock{ID: w.ID, Name: w.Name, Input: json.RawMessage(input)}
		} else if declaredTools != nil && declaredTools[w.Name] {
			input, err := inputToIRString(w.Input)
			if err != nil {
				return nil, nil, false, fmt.Errorf("anthropic: %s.input: %w", path, err)
			}
			block = ir.ToolUseBlock{ID: w.ID, Name: w.Name, Input: input}
		} else if customInput, ok := extractCustomInput(w.Input); ok {
			input, _ := json.Marshal(customInput)
			block = ir.CustomToolUseBlock{ID: w.ID, Name: w.Name, Input: json.RawMessage(input)}
		} else {
			input, err := inputToIRString(w.Input)
			if err != nil {
				return nil, nil, false, fmt.Errorf("anthropic: %s.input: %w", path, err)
			}
			block = ir.ToolUseBlock{ID: w.ID, Name: w.Name, Input: input}
		}
	case BlockTypeToolResult:
		if w.ToolUseID == "" {
			return nil, nil, false, fmt.Errorf("anthropic: %s.tool_use_id is required", path)
		}
		if customCallIDs != nil && customCallIDs[w.ToolUseID] {
			if str, ok := w.Content.(string); ok {
				block = ir.CustomToolResultBlock{
					ToolUseID: w.ToolUseID,
					Output:    ir.CustomToolOutput{Text: &str},
				}
			} else {
				content, contentLosses, err := decodeBlocks(w.Content, path+".content", customCallIDs, declaredTools, customTools, isRequest)
				if err != nil {
					return nil, nil, false, err
				}
				for idx, cb := range content {
					switch blk := cb.(type) {
					case ir.RedactedThinkingBlock:
						return nil, nil, false, fmt.Errorf("anthropic: %s.content[%d]: redacted_thinking is not permitted in tool_result", path, idx)
					case ir.TextBlock:
						if len(blk.Citations) > 0 {
							return nil, nil, false, fmt.Errorf("anthropic: %s.content[%d].citations: citations are not permitted in tool_result", path, idx)
						}
					}
				}
				losses = append(losses, contentLosses...)
				block = ir.CustomToolResultBlock{
					ToolUseID: w.ToolUseID,
					Output:    ir.CustomToolOutput{Content: content},
				}
			}
		} else {
			content, contentLosses, err := decodeBlocks(w.Content, path+".content", customCallIDs, declaredTools, customTools, isRequest)
			if err != nil {
				return nil, nil, false, err
			}
			for idx, cb := range content {
				switch blk := cb.(type) {
				case ir.RedactedThinkingBlock:
					return nil, nil, false, fmt.Errorf("anthropic: %s.content[%d]: redacted_thinking is not permitted in tool_result", path, idx)
				case ir.TextBlock:
					if len(blk.Citations) > 0 {
						return nil, nil, false, fmt.Errorf("anthropic: %s.content[%d].citations: citations are not permitted in tool_result", path, idx)
					}
				}
			}
			block = ir.ToolResultBlock{ToolUseID: w.ToolUseID, Content: content, IsError: w.IsError}
			losses = append(losses, contentLosses...)
		}
	default:
		return nil, []ir.Loss{{
			Path:   path,
			Field:  "type",
			Reason: ir.LossUnsupportedSemantic,
			Detail: fmt.Sprintf("Anthropic block type %q has no IR equivalent", w.Type),
		}}, false, nil
	}
	return block, losses, true, nil
}

func decodeImage(source *SourceWire, path string) (ir.ImageBlock, *ir.Loss, error) {
	if source == nil {
		return ir.ImageBlock{}, nil, fmt.Errorf("anthropic: %s is required", path)
	}
	switch source.Type {
	case SourceTypeBase64:
		if source.MediaType == "" {
			return ir.ImageBlock{}, nil, fmt.Errorf("anthropic: %s.media_type is required", path)
		}
		if source.Data == "" {
			return ir.ImageBlock{}, nil, fmt.Errorf("anthropic: %s.data is required", path)
		}
		return ir.ImageBlock{MediaType: source.MediaType, Data: source.Data}, nil, nil
	case SourceTypeURL:
		if source.URL == "" {
			return ir.ImageBlock{}, nil, fmt.Errorf("anthropic: %s.url is required", path)
		}
		return ir.ImageBlock{URL: source.URL}, nil, nil
	default:
		return ir.ImageBlock{}, &ir.Loss{
			Path:   path,
			Field:  "type",
			Reason: ir.LossUnsupportedSemantic,
			Detail: fmt.Sprintf("Anthropic image source type %q has no IR equivalent", source.Type),
		}, nil
	}
}

// decodeDocument decodes an Anthropic document block into ir.DocumentBlock
// (N-AN-16). The source form is carried by the field combination; base64
// bytes are preserved verbatim (INV-1). title/context/citations travel as
// native annotations with zero decode loss; block-level cache_control is
// attached by the caller like any other block.
func decodeDocument(w BlockWire, path string) (ir.DocumentBlock, *ir.Loss, error) {
	if w.Source == nil {
		return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source is required", path)
	}
	if w.Source.Type == "" {
		return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source.type is required", path)
	}
	var doc ir.DocumentBlock
	switch w.Source.Type {
	case SourceTypeBase64:
		if w.Source.MediaType == "" {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source.media_type is required", path)
		}
		if w.Source.Data == "" {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source.data is required", path)
		}
		if w.Source.has("url", w.Source.URL) || w.Source.has("file_id", w.Source.FileID) {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source has conflicting source fields", path)
		}
		doc = ir.DocumentBlock{MediaType: w.Source.MediaType, Data: w.Source.Data}
	case SourceTypeURL:
		if w.Source.URL == "" {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source.url is required", path)
		}
		if w.Source.has("media_type", w.Source.MediaType) || w.Source.has("data", w.Source.Data) || w.Source.has("file_id", w.Source.FileID) {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source has conflicting source fields", path)
		}
		doc = ir.DocumentBlock{URL: w.Source.URL}
	case SourceTypeFile:
		if w.Source.FileID == "" {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source.file_id is required", path)
		}
		if w.Source.has("media_type", w.Source.MediaType) || w.Source.has("data", w.Source.Data) || w.Source.has("url", w.Source.URL) {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.source has conflicting source fields", path)
		}
		doc = ir.DocumentBlock{FileID: w.Source.FileID}
	default:
		return ir.DocumentBlock{}, &ir.Loss{
			Path:   path,
			Field:  "type",
			Reason: ir.LossUnsupportedSemantic,
			Detail: fmt.Sprintf("Anthropic document source type %q has no IR equivalent", w.Source.Type),
		}, nil
	}
	if w.hasTitle || w.Title != "" {
		title, err := json.Marshal(w.Title)
		if err != nil {
			return ir.DocumentBlock{}, nil, err
		}
		doc.Native = append(doc.Native, ir.NativeAnnotation{
			Source:   ir.NativeSourceAnthropic,
			Kind:     ir.NativeKindDocumentTitle,
			Value:    string(title),
			Omission: &ir.NativeOmission{Field: "title", Reason: ir.LossUnmappedField, Detail: ir.DocumentTitleLossDetail},
		})
	}
	if w.hasContext || w.Context != "" {
		ctx, err := json.Marshal(w.Context)
		if err != nil {
			return ir.DocumentBlock{}, nil, err
		}
		doc.Native = append(doc.Native, ir.NativeAnnotation{
			Source:   ir.NativeSourceAnthropic,
			Kind:     ir.NativeKindDocumentContext,
			Value:    string(ctx),
			Omission: &ir.NativeOmission{Field: "context", Reason: ir.LossUnmappedField, Detail: ir.DocumentContextLossDetail},
		})
	}
	if len(w.Citations) > 0 {
		var obj map[string]any
		if err := json.Unmarshal(w.Citations, &obj); err != nil {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.citations must be a JSON object", path)
		}
		if obj == nil {
			return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.citations must be a JSON object", path)
		}
		if enabled, ok := obj["enabled"]; ok {
			if _, isBool := enabled.(bool); !isBool {
				return ir.DocumentBlock{}, nil, fmt.Errorf("anthropic: %s.citations.enabled must be a boolean", path)
			}
		}
		canon, err := json.Marshal(obj)
		if err != nil {
			return ir.DocumentBlock{}, nil, err
		}
		doc.Native = append(doc.Native, ir.NativeAnnotation{
			Source:   ir.NativeSourceAnthropic,
			Kind:     ir.NativeKindDocumentCitations,
			Value:    string(canon),
			Omission: &ir.NativeOmission{Field: "citations", Reason: ir.LossUnsupportedSemantic, Detail: ir.DocumentCitationsLossDetail},
		})
	}
	return doc, nil, nil
}

func requireJSONObject(raw json.RawMessage, path string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return fmt.Errorf("anthropic: %s is required", path)
	}
	if !json.Valid(trimmed) || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return fmt.Errorf("anthropic: %s must be a JSON object", path)
	}
	return nil
}

func decodeToolChoice(choice *ToolChoiceWire, customTools, nativeTools map[string]bool) (*ir.ToolChoice, []ir.Loss, error) {
	if choice == nil {
		return nil, nil, nil
	}
	var decoded *ir.ToolChoice
	var losses []ir.Loss
	switch choice.Type {
	case ToolChoiceTypeAuto, ToolChoiceTypeAny, ToolChoiceTypeNone:
		decoded = &ir.ToolChoice{Mode: choice.Type}
	case ToolChoiceTypeTool:
		if choice.Name == "" {
			return nil, nil, fmt.Errorf("anthropic: tool_choice.name is required for type tool")
		}
		if nativeTools != nil && nativeTools[choice.Name] {
			decoded = &ir.ToolChoice{Mode: ir.ToolChoiceTool, ToolType: ir.ToolTypeNative, Name: choice.Name}
		} else if customTools != nil && customTools[choice.Name] {
			decoded = &ir.ToolChoice{Mode: ir.ToolChoiceTool, ToolType: ir.ToolTypeCustom, Name: choice.Name}
		} else {
			decoded = &ir.ToolChoice{Mode: ir.ToolChoiceTool, Name: choice.Name}
		}
	default:
		losses = append(losses, ir.Loss{
			Path:   "tool_choice",
			Field:  "type",
			Reason: ir.LossUnsupportedSemantic,
			Detail: fmt.Sprintf("Anthropic tool_choice type %q has no IR equivalent", choice.Type),
		})
	}
	if choice.HasDisableParallelToolUse || choice.DisableParallelToolUse {
		if choice.Type == ToolChoiceTypeNone {
			return nil, nil, fmt.Errorf("anthropic: cannot specify disable_parallel_tool_use with tool_choice type none")
		}
	}
	return decoded, losses, nil
}

func validateSystemMessagePlacement(msgs []Message) error {
	for i, m := range msgs {
		if m.Role == RoleSystem {
			if i == 0 {
				return fmt.Errorf("anthropic: messages[0]: system message cannot appear at messages[0]")
			}
			prevRole := msgs[i-1].Role
			if prevRole != RoleUser && prevRole != RoleSystem {
				return fmt.Errorf("anthropic: messages[%d]: system message must follow a user turn", i)
			}
			last := i
			for last+1 < len(msgs) && msgs[last+1].Role == RoleSystem {
				last++
			}
			if last+1 < len(msgs) {
				nextRole := msgs[last+1].Role
				if nextRole != RoleAssistant {
					return fmt.Errorf("anthropic: messages[%d]: system message must be followed by an assistant turn or be the last message", i)
				}
			}
		}
	}
	return nil
}

func decodeSystemMessageContent(content any, msgIndex int) ([]ir.SystemBlock, []ir.Loss, error) {
	if content == nil {
		return nil, nil, fmt.Errorf("anthropic: messages[%d]: content is missing", msgIndex)
	}
	switch v := content.(type) {
	case string:
		return []ir.SystemBlock{{Text: v}}, nil, nil
	case []BlockWire:
		blocks := make([]ir.SystemBlock, 0, len(v))
		var losses []ir.Loss
		for j, b := range v {
			if b.Type != BlockTypeText {
				return nil, nil, fmt.Errorf("anthropic: messages[%d]: system content must be text", msgIndex)
			}
			sb := ir.SystemBlock{Text: b.Text}
			if len(b.CacheControl) > 0 {
				canon, l, ok := parseCacheControl(b.CacheControl, fmt.Sprintf("messages[%d].content[%d].cache_control", msgIndex, j))
				if l != nil {
					losses = append(losses, *l)
				}
				if ok {
					sb.Native = []ir.NativeAnnotation{{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindCacheControl,
						Value:    canon,
						Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
					}}
				}
			}
			blocks = append(blocks, sb)
		}
		return blocks, losses, nil
	case []SystemBlockWire:
		blocks := make([]ir.SystemBlock, 0, len(v))
		var losses []ir.Loss
		for j, w := range v {
			if w.Type != BlockTypeText {
				return nil, nil, fmt.Errorf("anthropic: messages[%d]: system content must be text", msgIndex)
			}
			sb := ir.SystemBlock{Text: w.Text}
			if len(w.CacheControl) > 0 {
				canon, l, ok := parseCacheControl(w.CacheControl, fmt.Sprintf("messages[%d].content[%d].cache_control", msgIndex, j))
				if l != nil {
					losses = append(losses, *l)
				}
				if ok {
					sb.Native = []ir.NativeAnnotation{{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindCacheControl,
						Value:    canon,
						Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
					}}
				}
			}
			blocks = append(blocks, sb)
		}
		return blocks, losses, nil
	case []any:
		blocks := make([]ir.SystemBlock, 0, len(v))
		var losses []ir.Loss
		for j, part := range v {
			raw, err := json.Marshal(part)
			if err != nil {
				return nil, nil, err
			}
			var w SystemBlockWire
			if err := json.Unmarshal(raw, &w); err != nil {
				return nil, nil, fmt.Errorf("anthropic: messages[%d]: %w", msgIndex, err)
			}
			if w.Type != BlockTypeText {
				return nil, nil, fmt.Errorf("anthropic: messages[%d]: system content must be text", msgIndex)
			}
			sb := ir.SystemBlock{Text: w.Text}
			if len(w.CacheControl) > 0 {
				canon, l, ok := parseCacheControl(w.CacheControl, fmt.Sprintf("messages[%d].content[%d].cache_control", msgIndex, j))
				if l != nil {
					losses = append(losses, *l)
				}
				if ok {
					sb.Native = []ir.NativeAnnotation{{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindCacheControl,
						Value:    canon,
						Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
					}}
				}
			}
			blocks = append(blocks, sb)
		}
		return blocks, losses, nil
	default:
		return nil, nil, fmt.Errorf("anthropic: messages[%d]: system content must be text", msgIndex)
	}
}

// DecodeResponse converts an Anthropic Messages wire response to the IR
// (face -> IR). Text, image, and tool_use blocks map near-identically;
// stop_reason maps by value (stop_sequence also carries the matched sequence),
// and usage maps 1:1.
// The envelope fields type and role are exempt from losses
// (vectors/README.md loss conventions).
func DecodeResponse(wire *Response, opts ...Option) (*ir.Response, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("anthropic: nil response")
	}
	var losses []ir.Loss
	o := newOptions(opts...)
	resp := &ir.Response{
		ID:    wire.ID,
		Model: o.models.Map(wire.Model),
	}
	for i, b := range wire.Content {
		block, blockLosses, mapped, err := decodeBlock(b, fmt.Sprintf("content[%d]", i), nil, nil, nil, false)
		if err != nil {
			return nil, nil, err
		}
		if mapped {
			resp.Content = append(resp.Content, block)
		}
		losses = append(losses, blockLosses...)
	}
	stop, loss, err := decodeStopReason(wire.StopReason, wire.StopSequence)
	if err != nil {
		return nil, nil, err
	}
	if loss != nil {
		losses = append(losses, *loss)
	}
	resp.StopReason = stop
	resp.StopSequence = wire.StopSequence
	if wire.Usage != nil {
		resp.Usage = ir.Usage{
			InputTokens:              wire.Usage.InputTokens,
			OutputTokens:             wire.Usage.OutputTokens,
			CacheCreationInputTokens: wire.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     wire.Usage.CacheReadInputTokens,
		}
	}
	return resp, losses, nil
}

func decodeStopReason(stop, stopSequence string) (ir.StopReason, *ir.Loss, error) {
	switch stop {
	case StopReasonEndTurn:
		return ir.StopEndTurn, nil, nil
	case StopReasonMaxTokens:
		return ir.StopMaxTokens, nil, nil
	case StopReasonStopSequence:
		return ir.StopSequence, nil, nil
	case StopReasonToolUse:
		return ir.StopToolUse, nil, nil
	case StopReasonRefusal:
		return ir.StopRefusal, nil, nil
	case "":
		return "", nil, fmt.Errorf("anthropic: stop_reason is missing")
	default:
		return ir.StopOther, &ir.Loss{
			Path:   "stop_reason",
			Field:  "stop_reason",
			Reason: ir.LossUnmappedValue,
			Detail: fmt.Sprintf("Anthropic stop_reason %q has no IR equivalent", stop),
		}, nil
	}
}

func decodeTextCitations(raw json.RawMessage, path string, isRequest bool) ([]ir.TextCitation, []ir.Loss, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, nil, fmt.Errorf("anthropic: %s must be an array", path)
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(trimmed, &raws); err != nil {
		return nil, nil, fmt.Errorf("anthropic: %s must be an array: %w", path, err)
	}
	if len(raws) == 0 {
		return nil, nil, nil
	}
	var cites []ir.TextCitation
	var losses []ir.Loss
	for i, r := range raws {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(r, &obj); err != nil {
			return nil, nil, fmt.Errorf("anthropic: %s must be an object", itemPath)
		}
		rawType, ok := obj["type"]
		if !ok || string(rawType) == "null" {
			return nil, nil, fmt.Errorf("anthropic: %s.type is required", itemPath)
		}
		var citeType string
		if err := json.Unmarshal(rawType, &citeType); err != nil {
			return nil, nil, fmt.Errorf("anthropic: %s.type must be a string", itemPath)
		}

		cleanObj := make(map[string]any)
		cleanObj["type"] = citeType

		switch citeType {
		case "char_location":
			if err := decodeCharLocation(obj, cleanObj, itemPath); err != nil {
				return nil, nil, err
			}
		case "page_location":
			if err := decodePageLocation(obj, cleanObj, itemPath); err != nil {
				return nil, nil, err
			}
		case "content_block_location":
			if err := decodeContentBlockLocation(obj, cleanObj, itemPath); err != nil {
				return nil, nil, err
			}
		case "web_search_result_location":
			if err := decodeWebSearchResultLocation(obj, cleanObj, itemPath); err != nil {
				return nil, nil, err
			}
		case "search_result_location":
			if err := decodeSearchResultLocation(obj, cleanObj, itemPath); err != nil {
				return nil, nil, err
			}
		default:
			losses = append(losses, ir.Loss{
				Path:   itemPath,
				Field:  "type",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("unsupported citation type %q", citeType),
			})
			continue
		}

		var extraKeys []string
		for k := range obj {
			if _, ok := cleanObj[k]; !ok {
				extraKeys = append(extraKeys, k)
			}
		}
		sort.Strings(extraKeys)
		for _, k := range extraKeys {
			losses = append(losses, ir.Loss{
				Path:   fmt.Sprintf("%s.%s", itemPath, k),
				Field:  k,
				Reason: ir.LossUnmappedField,
			})
		}

		compact, err := json.Marshal(cleanObj)
		if err != nil {
			return nil, nil, err
		}
		cites = append(cites, ir.TextCitation{
			Source: "anthropic",
			Value:  string(compact),
		})
	}
	return cites, losses, nil
}

func decodeCharLocation(obj map[string]json.RawMessage, clean map[string]any, itemPath string) error {
	txt, err := getRequiredString(obj, "cited_text", itemPath)
	if err != nil {
		return err
	}
	clean["cited_text"] = txt

	docIdx, err := getRequiredInt(obj, "document_index", itemPath)
	if err != nil {
		return err
	}
	if docIdx < 0 {
		return fmt.Errorf("anthropic: %s.document_index must be non-negative", itemPath)
	}
	clean["document_index"] = docIdx

	title, err := getNullableString(obj, "document_title", itemPath)
	if err != nil {
		return err
	}
	clean["document_title"] = title

	start, err := getRequiredInt(obj, "start_char_index", itemPath)
	if err != nil {
		return err
	}
	if start < 0 {
		return fmt.Errorf("anthropic: %s.start_char_index must be non-negative", itemPath)
	}
	clean["start_char_index"] = start

	end, err := getRequiredInt(obj, "end_char_index", itemPath)
	if err != nil {
		return err
	}
	if end <= start {
		return fmt.Errorf("anthropic: %s.end_char_index must be greater than start_char_index", itemPath)
	}
	clean["end_char_index"] = end

	fid, hasFid, err := getOptionalNullableString(obj, "file_id", itemPath)
	if err != nil {
		return err
	}
	if hasFid {
		clean["file_id"] = fid
	}
	return nil
}

func decodePageLocation(obj map[string]json.RawMessage, clean map[string]any, itemPath string) error {
	txt, err := getRequiredString(obj, "cited_text", itemPath)
	if err != nil {
		return err
	}
	clean["cited_text"] = txt

	docIdx, err := getRequiredInt(obj, "document_index", itemPath)
	if err != nil {
		return err
	}
	if docIdx < 0 {
		return fmt.Errorf("anthropic: %s.document_index must be non-negative", itemPath)
	}
	clean["document_index"] = docIdx

	title, err := getNullableString(obj, "document_title", itemPath)
	if err != nil {
		return err
	}
	clean["document_title"] = title

	start, err := getRequiredInt(obj, "start_page_number", itemPath)
	if err != nil {
		return err
	}
	if start < 1 {
		return fmt.Errorf("anthropic: %s.start_page_number must be at least 1", itemPath)
	}
	clean["start_page_number"] = start

	end, err := getRequiredInt(obj, "end_page_number", itemPath)
	if err != nil {
		return err
	}
	if end < start {
		return fmt.Errorf("anthropic: %s.end_page_number must be greater than or equal to start_page_number", itemPath)
	}
	clean["end_page_number"] = end

	fid, hasFid, err := getOptionalNullableString(obj, "file_id", itemPath)
	if err != nil {
		return err
	}
	if hasFid {
		clean["file_id"] = fid
	}
	return nil
}

func decodeContentBlockLocation(obj map[string]json.RawMessage, clean map[string]any, itemPath string) error {
	txt, err := getRequiredString(obj, "cited_text", itemPath)
	if err != nil {
		return err
	}
	clean["cited_text"] = txt

	docIdx, err := getRequiredInt(obj, "document_index", itemPath)
	if err != nil {
		return err
	}
	if docIdx < 0 {
		return fmt.Errorf("anthropic: %s.document_index must be non-negative", itemPath)
	}
	clean["document_index"] = docIdx

	title, err := getNullableString(obj, "document_title", itemPath)
	if err != nil {
		return err
	}
	clean["document_title"] = title

	start, err := getRequiredInt(obj, "start_block_index", itemPath)
	if err != nil {
		return err
	}
	if start < 0 {
		return fmt.Errorf("anthropic: %s.start_block_index must be non-negative", itemPath)
	}
	clean["start_block_index"] = start

	end, err := getRequiredInt(obj, "end_block_index", itemPath)
	if err != nil {
		return err
	}
	if end <= start {
		return fmt.Errorf("anthropic: %s.end_block_index must be greater than start_block_index", itemPath)
	}
	clean["end_block_index"] = end

	fid, hasFid, err := getOptionalNullableString(obj, "file_id", itemPath)
	if err != nil {
		return err
	}
	if hasFid {
		clean["file_id"] = fid
	}
	return nil
}

func decodeWebSearchResultLocation(obj map[string]json.RawMessage, clean map[string]any, itemPath string) error {
	txt, err := getRequiredString(obj, "cited_text", itemPath)
	if err != nil {
		return err
	}
	clean["cited_text"] = txt

	encIdx, err := getRequiredString(obj, "encrypted_index", itemPath)
	if err != nil {
		return err
	}
	clean["encrypted_index"] = encIdx

	title, err := getNullableString(obj, "title", itemPath)
	if err != nil {
		return err
	}
	clean["title"] = title

	url, err := getRequiredString(obj, "url", itemPath)
	if err != nil {
		return err
	}
	clean["url"] = url
	return nil
}

func decodeSearchResultLocation(obj map[string]json.RawMessage, clean map[string]any, itemPath string) error {
	txt, err := getRequiredString(obj, "cited_text", itemPath)
	if err != nil {
		return err
	}
	clean["cited_text"] = txt

	searchIdx, err := getRequiredInt(obj, "search_result_index", itemPath)
	if err != nil {
		return err
	}
	if searchIdx < 0 {
		return fmt.Errorf("anthropic: %s.search_result_index must be non-negative", itemPath)
	}
	clean["search_result_index"] = searchIdx

	source, err := getRequiredString(obj, "source", itemPath)
	if err != nil {
		return err
	}
	clean["source"] = source

	title, err := getNullableString(obj, "title", itemPath)
	if err != nil {
		return err
	}
	clean["title"] = title

	start, err := getRequiredInt(obj, "start_block_index", itemPath)
	if err != nil {
		return err
	}
	if start < 0 {
		return fmt.Errorf("anthropic: %s.start_block_index must be non-negative", itemPath)
	}
	clean["start_block_index"] = start

	end, err := getRequiredInt(obj, "end_block_index", itemPath)
	if err != nil {
		return err
	}
	if end <= start {
		return fmt.Errorf("anthropic: %s.end_block_index must be greater than start_block_index", itemPath)
	}
	clean["end_block_index"] = end
	return nil
}

func getRequiredString(obj map[string]json.RawMessage, field, itemPath string) (string, error) {
	raw, ok := obj[field]
	if !ok || string(raw) == "null" {
		return "", fmt.Errorf("anthropic: %s.%s is required", itemPath, field)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("anthropic: %s.%s must be a string", itemPath, field)
	}
	return s, nil
}

func getNullableString(obj map[string]json.RawMessage, field, itemPath string) (*string, error) {
	raw, ok := obj[field]
	if !ok {
		return nil, fmt.Errorf("anthropic: %s.%s is required", itemPath, field)
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("anthropic: %s.%s must be a string or null", itemPath, field)
	}
	return &s, nil
}

func getOptionalNullableString(obj map[string]json.RawMessage, field, itemPath string) (*string, bool, error) {
	raw, ok := obj[field]
	if !ok {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, true, fmt.Errorf("anthropic: %s.%s must be a string or null", itemPath, field)
	}
	return &s, true, nil
}

func getRequiredInt(obj map[string]json.RawMessage, field, itemPath string) (int, error) {
	raw, ok := obj[field]
	if !ok || string(raw) == "null" {
		return 0, fmt.Errorf("anthropic: %s.%s is required", itemPath, field)
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("anthropic: %s.%s must be an integer", itemPath, field)
	}
	return n, nil
}
