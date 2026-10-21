package messages

import (
	"bytes"
	"encoding/json"
	"fmt"

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
		var nat []ir.NativeAnnotation
		if len(tool.CacheControl) > 0 {
			canon, l, ok := parseCacheControl(tool.CacheControl, fmt.Sprintf("tools[%d].cache_control", i))
			if l != nil {
				losses = append(losses, *l)
			}
			if ok {
				nat = []ir.NativeAnnotation{{
					Source:   ir.NativeSourceAnthropic,
					Kind:     ir.NativeKindCacheControl,
					Value:    canon,
					Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
				}}
			}
		}
		if isCustomToolInputSchema(tool.InputSchema) {
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
			Native:      nat,
		})
	}
	choice, choiceLosses, err := decodeToolChoice(wire.ToolChoice, customTools, nativeTools)
	if err != nil {
		return nil, nil, err
	}
	req.ToolChoice = choice
	losses = append(losses, choiceLosses...)

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

	for i, m := range wire.Messages {
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
		block = ir.TextBlock{Text: w.Text}
	case BlockTypeThinking:
		block = ir.ThinkingBlock{Thinking: w.Thinking, Signature: w.Signature}
	case BlockTypeImage:
		image, imageLoss, err := decodeImage(w.Source, path+".source")
		if err != nil {
			return nil, nil, false, err
		}
		if imageLoss != nil {
			return nil, []ir.Loss{*imageLoss}, false, nil
		}
		block = image
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
	if choice.DisableParallelToolUse {
		losses = append(losses, ir.Loss{
			Path:   "tool_choice.disable_parallel_tool_use",
			Field:  "disable_parallel_tool_use",
			Reason: ir.LossUnmappedField,
			Detail: "Anthropic disable_parallel_tool_use has no IR equivalent in v1.",
		})
	}
	return decoded, losses, nil
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
