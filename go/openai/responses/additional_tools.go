package responses

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v3/ir"
)

func decodeAdditionalTools(item InputItem, index, messageIndex, contentIndex int) (ir.ToolChangeGroup, []ir.Loss, error) {
	group := ir.ToolChangeGroup{MessageIndex: messageIndex, ContentIndex: contentIndex, ID: item.ID, Parts: []ir.ToolChangePart{}, Extra: item.Extra}
	if item.Role != RoleDeveloper {
		return group, nil, fmt.Errorf("responses: input[%d].role must be developer", index)
	}
	if item.Tools == nil {
		return group, nil, fmt.Errorf("responses: input[%d]: additional_tools requires tools array", index)
	}
	seen := make(map[string]bool)
	var losses []ir.Loss
	for i, raw := range item.Tools {
		tools, toolLosses, err := decodeToolItem(raw, nil, fmt.Sprintf("input[%d].tools[%d]", index, i), seen)
		if err != nil {
			return group, nil, err
		}
		losses = append(losses, toolLosses...)
		for _, tool := range tools {
			t := tool
			group.Parts = append(group.Parts, ir.ToolChangePart{Type: ir.ToolChangePartTypeToolChange, Change: &ir.ToolChange{Op: ir.ToolChangeOpAdd, Tool: &t}})
		}
	}
	return group, losses, nil
}

func hasAdditionalGroupAt(req *ir.Request, message, content int) bool {
	for _, group := range req.ToolChangeGroups {
		if group.MessageIndex == message && group.ContentIndex == content {
			return true
		}
	}
	return false
}

func hasInternalAdditionalGroups(req *ir.Request, message int) bool {
	for _, group := range req.ToolChangeGroups {
		if group.MessageIndex == message && group.ContentIndex >= 0 {
			return true
		}
	}
	return false
}

func encodeAdditionalSegment(items *[]InputItem, message ir.Message, path string) ([]ir.Loss, error) {
	switch message.Role {
	case ir.RoleAssistant:
		return encodeAssistantMessage(items, message.Content, path+".content")
	case ir.RoleUser:
		return encodeUserMessage(items, message, path)
	default:
		return nil, fmt.Errorf("responses: %s: unknown role %q", path, message.Role)
	}
}

func appendAdditionalGroups(items *[]InputItem, req *ir.Request, message, content int) ([]ir.Loss, error) {
	var losses []ir.Loss
	for i, group := range req.ToolChangeGroups {
		if group.MessageIndex != message || group.ContentIndex != content {
			continue
		}
		var tools []ir.Tool
		for j, part := range group.Parts {
			if part.Type == ir.ToolChangePartTypeToolChange && part.Change != nil && part.Change.Op == ir.ToolChangeOpAdd && part.Change.Tool != nil && (part.Change.Tool.Type != ir.ToolTypeNative || part.Change.Tool.Source == ir.NativeSourceOpenAI) {
				base := fmt.Sprintf("tool_change_groups[%d].parts[%d].change", i, j)
				for k, ann := range part.Change.Native {
					if ann.Source != ir.NativeSourceAnthropic && ann.Source != ir.NativeSourceOpenAI {
						losses = append(losses, loss(fmt.Sprintf("%s.native[%d]", base, k), "source", ir.LossUnsupportedSemantic, ""))
					}
					if ann.Source == ir.NativeSourceAnthropic && ann.Kind == ir.NativeKindCacheControl {
						losses = append(losses, loss(fmt.Sprintf("%s.native[%d]", base, k), "cache_control", ir.LossUnmappedField, "anthropic-native cache breakpoint; the target face has no cache_control representation"))
					}
				}
				for k, ann := range part.Change.Tool.Native {
					if ann.Source != ir.NativeSourceAnthropic && ann.Source != ir.NativeSourceOpenAI {
						losses = append(losses, loss(fmt.Sprintf("%s.tool.native[%d]", base, k), "source", ir.LossUnsupportedSemantic, ""))
					}
					if ann.Source == ir.NativeSourceAnthropic && ann.Kind == ir.NativeKindCacheControl {
						losses = append(losses, loss(fmt.Sprintf("%s.tool.native[%d]", base, k), "cache_control", ir.LossUnmappedField, "anthropic-native cache breakpoint; the target face has no cache_control representation"))
					}
				}
				for _, extra := range []struct {
					name   string
					fields map[string]json.RawMessage
				}{{"extra", part.Change.Extra}, {"tool_extra", part.Change.ToolExtra}, {"definition_extra", part.Change.DefinitionExtra}} {
					if len(extra.fields) > 0 {
						losses = append(losses, loss(base+"."+extra.name, extra.name, ir.LossUnmappedField, "source-native tool change extension cannot be represented on the target face"))
					}
				}
				tools = append(tools, *part.Change.Tool)
				continue
			}
			field, detail := "op", "positional tool availability change cannot be represented on the target face"
			if part.Type == ir.ToolChangePartTypeText {
				field, detail = "type", "positional system text has no equivalent on the target face"
			}
			losses = append(losses, loss(fmt.Sprintf("tool_change_groups[%d].parts[%d]", i, j), field, ir.LossUnsupportedSemantic, detail))
		}
		if len(group.Parts) != 0 && len(tools) == 0 {
			continue
		}
		defs, toolLosses, err := encodeGroupedTools(tools)
		if err != nil {
			return nil, err
		}
		losses = append(losses, toolLosses...)
		rawTools := make([]json.RawMessage, 0, len(defs))
		for _, def := range defs {
			raw, err := json.Marshal(def)
			if err != nil {
				return nil, err
			}
			rawTools = append(rawTools, raw)
		}
		*items = append(*items, InputItem{Type: ItemTypeAdditionalTools, Role: RoleDeveloper, ID: group.ID, Tools: rawTools, Extra: group.Extra})
	}
	return losses, nil
}

type additionalToolsMarker struct {
	index int
	group ir.ToolChangeGroup
}

func appendAdditionalMarkers(req *ir.Request, markers []additionalToolsMarker, origins []int) {
	for _, marker := range markers {
		firstOrigin, position := int(^uint(0)>>1), -1
		for i, origin := range origins {
			if origin > marker.index && origin < firstOrigin {
				firstOrigin, position = origin, i
			}
		}
		if position >= 0 {
			marker.group.ContentIndex = position
		} else if len(origins) > 0 {
			marker.group.MessageIndex++
		}
		req.ToolChangeGroups = append(req.ToolChangeGroups, marker.group)
	}
}
