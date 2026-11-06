package messages

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v3/ir"
)

// decodeToolChangeSystem preserves the ordering within a mixed system message.
func decodeToolChangeSystem(content any, index int) ([]ir.ToolChangePart, bool, []ir.Loss, error) {
	var blocks []BlockWire
	switch v := content.(type) {
	case []BlockWire:
		blocks = v
	case []any:
		for _, item := range v {
			raw, err := json.Marshal(item)
			if err != nil {
				return nil, false, nil, err
			}
			var block BlockWire
			if err := json.Unmarshal(raw, &block); err != nil {
				return nil, false, nil, err
			}
			blocks = append(blocks, block)
		}
	default:
		return nil, false, nil, nil
	}
	hasChange := false
	for _, b := range blocks {
		if b.Type == "tool_addition" || b.Type == "tool_removal" {
			hasChange = true
		}
	}
	if !hasChange {
		return nil, false, nil, nil
	}
	parts := make([]ir.ToolChangePart, 0, len(blocks))
	var losses []ir.Loss
	for j, b := range blocks {
		path := fmt.Sprintf("messages[%d].content[%d]", index, j)
		if b.Type == BlockTypeText {
			part := ir.ToolChangePart{Type: ir.ToolChangePartTypeText, Text: b.Text}
			if len(b.CacheControl) > 0 {
				canon, loss, ok := parseCacheControl(b.CacheControl, path+".cache_control")
				if loss != nil {
					losses = append(losses, *loss)
				}
				if ok {
					part.Native = []ir.NativeAnnotation{{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindCacheControl, Value: canon, Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail}}}
				}
			}
			parts = append(parts, part)
			continue
		}
		if b.Type != "tool_addition" && b.Type != "tool_removal" {
			return nil, true, nil, fmt.Errorf("anthropic: %s: unsupported system content type %q", path, b.Type)
		}
		change, changeLosses, err := decodeToolChange(b, path)
		if err != nil {
			return nil, true, nil, err
		}
		losses = append(losses, changeLosses...)
		parts = append(parts, ir.ToolChangePart{Type: ir.ToolChangePartTypeToolChange, Change: change})
	}
	return parts, true, losses, nil
}

func decodeToolChange(b BlockWire, path string) (*ir.ToolChange, []ir.Loss, error) {
	c := &ir.ToolChange{Op: ir.ToolChangeOpAdd}
	if b.Type == "tool_removal" {
		c.Op = ir.ToolChangeOpRemove
	}
	var tool map[string]json.RawMessage
	if len(b.Tool) == 0 || json.Unmarshal(b.Tool, &tool) != nil || tool == nil {
		return nil, nil, fmt.Errorf("anthropic: %s.tool must be an object", path)
	}
	var kind string
	if json.Unmarshal(tool["type"], &kind) != nil || kind == "" {
		return nil, nil, fmt.Errorf("anthropic: %s.tool.type is required", path)
	}
	if kind == "tool_definition" {
		if c.Op == ir.ToolChangeOpRemove {
			return nil, nil, fmt.Errorf("anthropic: %s.tool: removal cannot carry a definition", path)
		}
		var definition ToolWire
		if len(tool["definition"]) == 0 || json.Unmarshal(tool["definition"], &definition) != nil {
			return nil, nil, fmt.Errorf("anthropic: %s.tool.definition is invalid", path)
		}
		tmp, _, err := DecodeRequest(&Request{Model: "m", MaxTokens: 1, Tools: []ToolWire{definition}, Messages: []Message{{Role: RoleUser, Content: "x"}}})
		if err != nil {
			return nil, nil, fmt.Errorf("anthropic: %s.tool.definition: %w", path, err)
		}
		c.Tool = &tmp.Tools[0]
		for k, v := range tool {
			if k != "type" && k != "definition" {
				if c.ToolExtra == nil {
					c.ToolExtra = map[string]json.RawMessage{}
				}
				c.ToolExtra[k] = v
			}
		}
		var definitionFields map[string]json.RawMessage
		if json.Unmarshal(tool["definition"], &definitionFields) == nil && c.Tool.Type != ir.ToolTypeNative {
			for k, v := range definitionFields {
				switch k {
				case "type", "name", "description", "input_schema", "strict", "cache_control", "defer_loading", "eager_input_streaming":
				default:
					if c.DefinitionExtra == nil {
						c.DefinitionExtra = map[string]json.RawMessage{}
					}
					c.DefinitionExtra[k] = v
				}
			}
		}
	} else {
		r := &ir.ToolChangeRef{Type: kind}
		if raw, ok := tool["name"]; ok && json.Unmarshal(raw, &r.Name) != nil {
			return nil, nil, fmt.Errorf("anthropic: %s.tool.name must be a string", path)
		}
		if raw, ok := tool["server_name"]; ok && json.Unmarshal(raw, &r.ServerName) != nil {
			return nil, nil, fmt.Errorf("anthropic: %s.tool.server_name must be a string", path)
		}
		for k, v := range tool {
			if k != "type" && k != "name" && k != "server_name" {
				if r.Extra == nil {
					r.Extra = map[string]json.RawMessage{}
				}
				r.Extra[k] = v
			}
		}
		c.Ref = r
	}
	var fields map[string]json.RawMessage
	if len(b.rawChange) > 0 && json.Unmarshal(b.rawChange, &fields) == nil {
		for k, v := range fields {
			if k != "type" && k != "tool" && k != "cache_control" {
				if c.Extra == nil {
					c.Extra = map[string]json.RawMessage{}
				}
				c.Extra[k] = v
			}
		}
	}
	var losses []ir.Loss
	if len(b.CacheControl) > 0 {
		canon, loss, ok := parseCacheControl(b.CacheControl, path+".cache_control")
		if loss != nil {
			losses = append(losses, *loss)
		}
		if ok {
			c.Native = append(c.Native, ir.NativeAnnotation{Source: ir.NativeSourceAnthropic, Kind: ir.NativeKindCacheControl, Value: canon, Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail}})
		}
	}
	if _, err := json.Marshal(c); err != nil {
		return nil, nil, fmt.Errorf("anthropic: %s: %w", path, err)
	}
	return c, losses, nil
}

func appendToolChangeGroups(out *Request, req *ir.Request, position int) ([]ir.Loss, error) {
	var losses []ir.Loss
	for i, group := range req.ToolChangeGroups {
		if group.MessageIndex != position {
			continue
		}
		if len(group.Extra) > 0 {
			losses = append(losses, ir.Loss{Path: fmt.Sprintf("tool_change_groups[%d].extra", i), Field: "extra", Reason: ir.LossUnmappedField, Detail: "additional_tools item extensions have no equivalent on the target face"})
		}
		valid := group.ContentIndex == -1 && position > 0 && req.Messages[position-1].Role == ir.RoleUser && (position == len(req.Messages) || req.Messages[position].Role == ir.RoleAssistant)
		if !valid || len(group.Parts) == 0 {
			if len(group.Parts) == 0 {
				losses = append(losses, ir.Loss{Path: fmt.Sprintf("tool_change_groups[%d]", i), Field: "tools", Reason: ir.LossUnsupportedSemantic, Detail: "empty positional tool group cannot be represented on the target face"})
			}
			for j, part := range group.Parts {
				field, detail := "op", "positional tool availability change cannot be represented on the target face"
				if part.Type == ir.ToolChangePartTypeText {
					field, detail = "type", "positional system text has no equivalent on the target face"
				}
				losses = append(losses, ir.Loss{Path: fmt.Sprintf("tool_change_groups[%d].parts[%d]", i, j), Field: field, Reason: ir.LossUnsupportedSemantic, Detail: detail})
			}
			continue
		}
		blocks := make([]BlockWire, 0, len(group.Parts))
		for j, part := range group.Parts {
			if part.Type == ir.ToolChangePartTypeText {
				block := BlockWire{Type: BlockTypeText, Text: part.Text}
				for _, ann := range part.Native {
					if ann.Source == ir.NativeSourceAnthropic && ann.Kind == ir.NativeKindCacheControl {
						block.CacheControl = json.RawMessage(ann.Value)
					}
				}
				blocks = append(blocks, block)
				continue
			}
			if part.Type != ir.ToolChangePartTypeToolChange || part.Change == nil {
				return nil, fmt.Errorf("anthropic: tool_change_groups[%d].parts[%d] is invalid", i, j)
			}
			if part.Change.Tool != nil && part.Change.Tool.Type == ir.ToolTypeNative && part.Change.Tool.Source != ir.NativeSourceAnthropic {
				losses = append(losses, ir.Loss{Path: fmt.Sprintf("tool_change_groups[%d].parts[%d]", i, j), Field: "op", Reason: ir.LossUnsupportedSemantic, Detail: "positional tool availability change cannot be represented on the target face"})
				continue
			}
			raw, err := encodeToolChange(part.Change)
			if err != nil {
				return nil, fmt.Errorf("anthropic: tool_change_groups[%d].parts[%d]: %w", i, j, err)
			}
			blocks = append(blocks, BlockWire{Type: "tool_addition", rawChange: raw})
		}
		if group.ID != "" {
			losses = append(losses, ir.Loss{Path: fmt.Sprintf("tool_change_groups[%d].id", i), Field: "id", Reason: ir.LossUnmappedField, Detail: "additional_tools item id has no equivalent on the target face"})
		}
		if len(blocks) > 0 {
			out.Messages = append(out.Messages, Message{Role: RoleSystem, Content: blocks})
		}
	}
	return losses, nil
}

func encodeToolChange(change *ir.ToolChange) (json.RawMessage, error) {
	if _, err := json.Marshal(change); err != nil {
		return nil, err
	}
	obj := make(map[string]any, len(change.Extra)+3)
	for k, v := range change.Extra {
		obj[k] = v
	}
	obj["type"] = "tool_addition"
	if change.Op == ir.ToolChangeOpRemove {
		obj["type"] = "tool_removal"
	}
	if change.Ref != nil {
		ref := map[string]any{"type": change.Ref.Type}
		for k, v := range change.Ref.Extra {
			ref[k] = v
		}
		if change.Ref.Name != "" {
			ref["name"] = change.Ref.Name
		}
		if change.Ref.ServerName != "" {
			ref["server_name"] = change.Ref.ServerName
		}
		obj["tool"] = ref
	} else {
		max := int64(1)
		wire, _, err := EncodeRequest(&ir.Request{Model: "m", Params: ir.Params{MaxTokens: &max}, Tools: []ir.Tool{*change.Tool}, Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "x"}}}}})
		if err != nil {
			return nil, err
		}
		if len(wire.Tools) != 1 {
			return nil, fmt.Errorf("tool definition cannot be represented")
		}
		definitionRaw, err := json.Marshal(wire.Tools[0])
		if err != nil {
			return nil, err
		}
		var definition map[string]json.RawMessage
		if err := json.Unmarshal(definitionRaw, &definition); err != nil {
			return nil, err
		}
		for k, v := range change.DefinitionExtra {
			if _, exists := definition[k]; exists {
				return nil, fmt.Errorf("definition extra conflicts with %s", k)
			}
			definition[k] = v
		}
		wrapper := map[string]any{"type": "tool_definition", "definition": definition}
		for k, v := range change.ToolExtra {
			if k == "type" || k == "definition" {
				return nil, fmt.Errorf("tool extra conflicts with %s", k)
			}
			wrapper[k] = v
		}
		obj["tool"] = wrapper
	}
	for _, ann := range change.Native {
		if ann.Source == ir.NativeSourceAnthropic && ann.Kind == ir.NativeKindCacheControl {
			obj["cache_control"] = json.RawMessage(ann.Value)
		}
	}
	return json.Marshal(obj)
}
