package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

// decodeToolItem recursively flattens a tool declaration or container.
func decodeToolItem(raw json.RawMessage, prefix string, containerType string, path string, seenNames map[string]bool) ([]ir.Tool, []ir.Loss, error) {
	var probe struct {
		Type         string                `json:"type"`
		Name         string                `json:"name"`
		Description  string                `json:"description"`
		Parameters   json.RawMessage       `json:"parameters"`
		Strict       *bool                 `json:"strict"`
		Format       *CustomToolFormatWire `json:"format"`
		DeferLoading *bool                 `json:"defer_loading"`
		Tools        []json.RawMessage     `json:"tools"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, nil, fmt.Errorf("responses: %s: invalid JSON object: %w", path, err)
	}

	if probe.Type == ToolTypeNamespace || probe.Type == ToolTypeToolbox {
		if probe.Name == "" || strings.HasPrefix(probe.Name, ".") || strings.HasSuffix(probe.Name, ".") || strings.Contains(probe.Name, "..") {
			return nil, nil, fmt.Errorf("responses: %s: invalid %s name %q", path, probe.Type, probe.Name)
		}
		if len(probe.Tools) == 0 {
			return nil, nil, fmt.Errorf("responses: %s: %s requires non-empty tools array", path, probe.Type)
		}
		currentPrefix := probe.Name
		if prefix != "" {
			currentPrefix = prefix + "." + probe.Name
		}
		var tools []ir.Tool
		var losses []ir.Loss
		for idx, childRaw := range probe.Tools {
			childPath := fmt.Sprintf("%s.tools[%d]", path, idx)
			childTools, childLosses, err := decodeToolItem(childRaw, currentPrefix, probe.Type, childPath, seenNames)
			if err != nil {
				return nil, nil, err
			}
			tools = append(tools, childTools...)
			losses = append(losses, childLosses...)
		}
		return tools, losses, nil
	}

	var child ir.Tool
	var losses []ir.Loss

	switch probe.Type {
	case ToolTypeApplyPatch:
		child = ir.Tool{Type: ir.ToolTypeApplyPatch}
	case ToolTypeCustom:
		if probe.Name == "" {
			return nil, nil, fmt.Errorf("responses: %s: custom tool name is required", path)
		}
		finalName := probe.Name
		if prefix != "" && !strings.HasPrefix(finalName, prefix+".") {
			finalName = prefix + "." + finalName
		}
		if seenNames[finalName] {
			return nil, nil, fmt.Errorf("responses: duplicate tool name %q", finalName)
		}
		seenNames[finalName] = true

		format, formatLosses, err := decodeCustomFormat(probe.Format, path+".format")
		if err != nil {
			return nil, nil, err
		}
		losses = append(losses, formatLosses...)
		if len(formatLosses) > 0 {
			return nil, losses, nil
		}
		child = ir.Tool{
			Type:        ir.ToolTypeCustom,
			Name:        finalName,
			Description: probe.Description,
			Format:      format,
		}
	case ToolTypeFunction, "":
		if probe.Name == "" {
			return nil, nil, fmt.Errorf("responses: %s: function tool name is required", path)
		}
		finalName := probe.Name
		if prefix != "" && !strings.HasPrefix(finalName, prefix+".") {
			finalName = prefix + "." + finalName
		}
		if seenNames[finalName] {
			return nil, nil, fmt.Errorf("responses: duplicate tool name %q", finalName)
		}
		seenNames[finalName] = true

		child = ir.Tool{
			Type:        ir.ToolTypeFunction,
			Name:        finalName,
			Description: probe.Description,
			InputSchema: probe.Parameters,
		}
		if probe.Strict != nil {
			losses = append(losses, loss(
				path+".strict", "strict", ir.LossUnmappedField,
				"Responses function tool strict mode has no IR equivalent in v1.",
			))
		}
	default:
		// Native tool declaration (web_search, file_search, code_interpreter, etc.)
		toolName := probe.Name
		if toolName == "" {
			toolName = probe.Type
		}
		if seenNames[toolName] {
			return nil, nil, fmt.Errorf("responses: duplicate tool name %q", toolName)
		}
		seenNames[toolName] = true

		child = ir.Tool{
			Type:       ir.ToolTypeNative,
			Name:       toolName,
			Source:     ir.NativeSourceOpenAI,
			Definition: string(raw),
		}
	}

	if child.Type != ir.ToolTypeNative && child.Type != ir.ToolTypeApplyPatch {
		if containerType != "" {
			child.Native = append(child.Native, ir.NativeAnnotation{
				Source: ir.NativeSourceOpenAI,
				Kind:   ir.NativeKindContainer,
				Value:  fmt.Sprintf("%q", containerType),
				Omission: &ir.NativeOmission{
					Field:  "type",
					Reason: ir.LossDegraded,
					Detail: "tool container grouping was flattened into dotted tool names",
				},
			})
		}
		if probe.DeferLoading != nil && *probe.DeferLoading {
			child.Native = append(child.Native, ir.NativeAnnotation{
				Source: ir.NativeSourceOpenAI,
				Kind:   ir.NativeKindDeferLoading,
				Value:  "true",
				Omission: &ir.NativeOmission{
					Field:  "defer_loading",
					Reason: ir.LossDegraded,
					Detail: ir.DeferLoadingLossDetail,
				},
			})
		}
	}

	return []ir.Tool{child}, losses, nil
}

// encodeGroupedTools groups flat IR tools back into wire ToolDef structures.
func encodeGroupedTools(tools []ir.Tool) ([]ToolDef, []ir.Loss, error) {
	if len(tools) == 0 {
		return nil, nil, nil
	}

	var out []ToolDef
	var losses []ir.Loss

	i := 0
	for i < len(tools) {
		tool := tools[i]
		if tool.Type == ir.ToolTypeNative {
			if tool.Source == ir.NativeSourceOpenAI {
				out = append(out, ToolDef{
					Type:          tool.Name,
					RawDefinition: tool.Definition,
				})
			} else {
				losses = append(losses, ir.NativeToolLoss(tool, fmt.Sprintf("tools[%d]", i)))
			}
			i++
			continue
		}

		if tool.Type == ir.ToolTypeApplyPatch {
			out = append(out, ToolDef{Type: ToolTypeApplyPatch})
			i++
			continue
		}

		// Check for container or namespace prefix
		containerKind := ""
		for _, ann := range tool.Native {
			if ann.Source == ir.NativeSourceOpenAI && ann.Kind == ir.NativeKindContainer {
				var unquoted string
				if err := json.Unmarshal([]byte(ann.Value), &unquoted); err == nil {
					containerKind = unquoted
				} else {
					containerKind = strings.Trim(ann.Value, "\"")
				}
				break
			}
		}

		dotIdx := strings.Index(tool.Name, ".")
		if dotIdx > 0 {
			prefix := tool.Name[:dotIdx]
			if containerKind == "" {
				containerKind = ToolTypeNamespace
			}
			// Gather consecutive tools with the same prefix
			var childRaw []json.RawMessage
			j := i
			for j < len(tools) && strings.HasPrefix(tools[j].Name, prefix+".") {
				subTool := tools[j]
				strippedName := strings.TrimPrefix(subTool.Name, prefix+".")
				subDef := ToolDef{
					Type:        ToolTypeFunction,
					Name:        strippedName,
					Description: subTool.Description,
					Parameters:  subTool.InputSchema,
				}
				if subTool.Type == ir.ToolTypeCustom {
					subDef.Type = ToolTypeCustom
					subDef.Parameters = nil
					subDef.Format = encodeCustomFormat(subTool.Format)
				}
				for _, ann := range subTool.Native {
					if ann.Kind == ir.NativeKindDeferLoading && ann.Value == "true" {
						t := true
						subDef.DeferLoading = &t
					}
				}
				b, err := json.Marshal(subDef)
				if err != nil {
					return nil, nil, err
				}
				childRaw = append(childRaw, b)
				j++
			}
			out = append(out, ToolDef{
				Type:  containerKind,
				Name:  prefix,
				Tools: childRaw,
			})
			i = j
			continue
		}

		// Standalone tool
		tDef := ToolDef{
			Type:        ToolTypeFunction,
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
		}
		if tool.Type == ir.ToolTypeCustom {
			tDef.Type = ToolTypeCustom
			tDef.Parameters = nil
			tDef.Format = encodeCustomFormat(tool.Format)
		}
		for _, ann := range tool.Native {
			if ann.Kind == ir.NativeKindDeferLoading && ann.Value == "true" {
				t := true
				tDef.DeferLoading = &t
			}
		}
		out = append(out, tDef)
		i++
	}

	return out, losses, nil
}
