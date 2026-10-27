package responses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

type ContainerEntry struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type containerAnnotationPayload struct {
	Path []ContainerEntry `json:"path"`
}

// decodeToolItem recursively flattens a tool declaration or container.
func decodeToolItem(raw json.RawMessage, pathStack []ContainerEntry, path string, seenNames map[string]bool) ([]ir.Tool, []ir.Loss, error) {
	var probe struct {
		Type         string                `json:"type"`
		Name         string                `json:"name"`
		ServerLabel  string                `json:"server_label"`
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
		entry := ContainerEntry{Type: probe.Type, Name: probe.Name, Description: probe.Description}
		newStack := append(append([]ContainerEntry(nil), pathStack...), entry)

		var tools []ir.Tool
		var losses []ir.Loss
		for idx, childRaw := range probe.Tools {
			childPath := fmt.Sprintf("%s.tools[%d]", path, idx)
			childTools, childLosses, err := decodeToolItem(childRaw, newStack, childPath, seenNames)
			if err != nil {
				return nil, nil, err
			}
			tools = append(tools, childTools...)
			losses = append(losses, childLosses...)
		}
		return tools, losses, nil
	}

	prefix := ""
	if len(pathStack) > 0 {
		var names []string
		for _, e := range pathStack {
			names = append(names, e.Name)
		}
		prefix = strings.Join(names, ".")
	}

	// Validate defer_loading format if present
	var hasDeferLoading bool
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err == nil {
		if dlRaw, ok := rawMap["defer_loading"]; ok {
			var b bool
			if err := json.Unmarshal(dlRaw, &b); err != nil {
				return nil, nil, fmt.Errorf("responses: %s.defer_loading must be a boolean", path)
			}
			hasDeferLoading = b
		}
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
			Strict:      probe.Strict,
		}
	default:
		// Check whether this is a native tool or an unsupported tool
		isNative := probe.Name != "" || probe.ServerLabel != "" || isKnownOpenAINativeType(probe.Type)
		if !isNative {
			losses = append(losses, loss(
				path, "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses tool type %q has no IR equivalent", probe.Type),
			))
			return nil, losses, nil
		}

		toolName := probe.Name
		if toolName == "" {
			toolName = probe.ServerLabel
		}
		if toolName == "" {
			toolName = probe.Type
		}
		if seenNames[toolName] {
			return nil, nil, fmt.Errorf("responses: duplicate tool name %q", toolName)
		}
		seenNames[toolName] = true

		defStr := string(raw)
		var compacted bytes.Buffer
		if err := json.Compact(&compacted, raw); err == nil {
			defStr = compacted.String()
		}

		child = ir.Tool{
			Type:       ir.ToolTypeNative,
			Name:       toolName,
			Source:     ir.NativeSourceOpenAI,
			Definition: defStr,
		}
	}

	if child.Type != ir.ToolTypeNative && child.Type != ir.ToolTypeApplyPatch {
		if len(pathStack) > 0 {
			valObj := containerAnnotationPayload{Path: pathStack}
			valBytes, _ := json.Marshal(valObj)
			child.Native = append(child.Native, ir.NativeAnnotation{
				Source: ir.NativeSourceOpenAI,
				Kind:   ir.NativeKindContainer,
				Value:  string(valBytes),
				Omission: &ir.NativeOmission{
					Field:  "type",
					Reason: ir.LossDegraded,
					Detail: "tool container grouping was flattened into dotted tool names",
				},
			})
		}
		if hasDeferLoading {
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

func parseContainerPath(tool ir.Tool) []ContainerEntry {
	for _, ann := range tool.Native {
		if ann.Source == ir.NativeSourceOpenAI && ann.Kind == ir.NativeKindContainer {
			var payload containerAnnotationPayload
			if err := json.Unmarshal([]byte(ann.Value), &payload); err == nil && len(payload.Path) > 0 {
				return payload.Path
			}
			// Legacy string value
			var single string
			if err := json.Unmarshal([]byte(ann.Value), &single); err == nil && single != "" {
				dotIdx := strings.Index(tool.Name, ".")
				name := tool.Name
				if dotIdx > 0 {
					name = tool.Name[:dotIdx]
				}
				return []ContainerEntry{{Type: single, Name: name}}
			}
		}
	}
	return nil
}

// buildNestedContainer builds container ToolDef for a slice of tools that share pathStack.
func buildNestedContainer(pathStack []ContainerEntry, tools []ir.Tool) (ToolDef, error) {
	if len(pathStack) == 0 {
		return ToolDef{}, fmt.Errorf("empty pathStack")
	}
	curr := pathStack[0]
	if len(pathStack) == 1 {
		// Leaf container: tools are direct children
		var childRaw []json.RawMessage
		for _, subTool := range tools {
			strippedName := subTool.Name
			if dotIdx := strings.LastIndex(strippedName, "."); dotIdx >= 0 {
				strippedName = strippedName[dotIdx+1:]
			}
			subDef := ToolDef{
				Type:        ToolTypeFunction,
				Name:        strippedName,
				Description: subTool.Description,
				Parameters:  subTool.InputSchema,
				Strict:      subTool.Strict,
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
				return ToolDef{}, err
			}
			childRaw = append(childRaw, b)
		}
		return ToolDef{
			Type:        curr.Type,
			Name:        curr.Name,
			Description: curr.Description,
			Tools:       childRaw,
		}, nil
	}

	// Multi-level: group children by pathStack[1].Name
	var childRaw []json.RawMessage
	k := 0
	for k < len(tools) {
		subPath := parseContainerPath(tools[k])
		if len(subPath) < 2 {
			k++
			continue
		}
		nextName := subPath[1].Name
		m := k
		for m < len(tools) {
			p := parseContainerPath(tools[m])
			if len(p) < 2 || p[1].Name != nextName {
				break
			}
			m++
		}
		nestedDef, err := buildNestedContainer(pathStack[1:], tools[k:m])
		if err != nil {
			return ToolDef{}, err
		}
		b, err := json.Marshal(nestedDef)
		if err != nil {
			return ToolDef{}, err
		}
		childRaw = append(childRaw, b)
		k = m
	}

	return ToolDef{
		Type:        curr.Type,
		Name:        curr.Name,
		Description: curr.Description,
		Tools:       childRaw,
	}, nil
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

		cPath := parseContainerPath(tool)
		if len(cPath) > 0 {
			root := cPath[0]
			j := i
			for j < len(tools) {
				subTool := tools[j]
				if subTool.Type == ir.ToolTypeNative || subTool.Type == ir.ToolTypeApplyPatch {
					break
				}
				p := parseContainerPath(subTool)
				if len(p) == 0 || p[0].Name != root.Name {
					break
				}
				j++
			}
			cDef, err := buildNestedContainer(rootAndDesc(root, cPath), tools[i:j])
			if err != nil {
				return nil, nil, err
			}
			out = append(out, cDef)
			i = j
			continue
		}

		dotIdx := strings.Index(tool.Name, ".")
		if dotIdx > 0 {
			prefix := tool.Name[:dotIdx]
			var childRaw []json.RawMessage
			j := i
			for j < len(tools) {
				subTool := tools[j]
				if subTool.Type == ir.ToolTypeNative || subTool.Type == ir.ToolTypeApplyPatch {
					break
				}
				if !strings.HasPrefix(subTool.Name, prefix+".") {
					break
				}
				strippedName := strings.TrimPrefix(subTool.Name, prefix+".")
				subDef := ToolDef{
					Type:        ToolTypeFunction,
					Name:        strippedName,
					Description: subTool.Description,
					Parameters:  subTool.InputSchema,
					Strict:      subTool.Strict,
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
				Type:  ToolTypeNamespace,
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
			Strict:      tool.Strict,
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

func rootAndDesc(root ContainerEntry, path []ContainerEntry) []ContainerEntry {
	res := make([]ContainerEntry, len(path))
	copy(res, path)
	if res[0].Description == "" && root.Description != "" {
		res[0].Description = root.Description
	}
	return res
}
