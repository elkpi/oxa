package messages

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
)

func parseCacheControl(raw json.RawMessage, path string) (canonical string, loss *ir.Loss, ok bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", nil, false
	}
	typ, hasType := obj["type"]
	if !hasType || typ != "ephemeral" {
		return "", &ir.Loss{
			Path:   path + ".type",
			Field:  "type",
			Reason: ir.LossUnmappedValue,
			Detail: "cache_control type must be ephemeral",
		}, false
	}
	canonicalMap := map[string]any{"type": "ephemeral"}
	if ttlVal, hasTTL := obj["ttl"]; hasTTL {
		ttlStr, isStr := ttlVal.(string)
		if !isStr || (ttlStr != "5m" && ttlStr != "1h") {
			return "", &ir.Loss{
				Path:   path + ".ttl",
				Field:  "ttl",
				Reason: ir.LossUnmappedValue,
				Detail: "cache_control ttl must be 5m or 1h",
			}, false
		}
		canonicalMap["ttl"] = ttlStr
	}
	canonBytes, _ := json.Marshal(canonicalMap)
	return string(canonBytes), nil, true
}

func parseContextManagement(cm *ContextManagementWire, path string) (canonical string, losses []ir.Loss, ok bool) {
	if cm == nil || len(cm.Edits) == 0 {
		return "", nil, false
	}
	var validEdits []map[string]any
	for i, raw := range cm.Edits {
		editPath := fmt.Sprintf("%s.edits[%d]", path, i)
		var edit map[string]any
		if err := json.Unmarshal(raw, &edit); err != nil {
			continue
		}
		editType, _ := edit["type"].(string)
		switch editType {
		case "clear_tool_uses_20250919":
			valid := true
			if trig, ok := edit["trigger"].(map[string]any); ok {
				if trig["type"] != "input_tokens" {
					losses = append(losses, ir.Loss{
						Path:   editPath + ".trigger",
						Field:  "trigger",
						Reason: ir.LossUnmappedValue,
					})
					valid = false
				}
			}
			if keep, ok := edit["keep"].(map[string]any); ok {
				if keep["type"] != "tool_uses" {
					losses = append(losses, ir.Loss{
						Path:   editPath + ".keep",
						Field:  "keep",
						Reason: ir.LossUnmappedValue,
					})
					valid = false
				}
			}
			if valid {
				validEdits = append(validEdits, edit)
			}
		case "clear_thinking_20251015":
			valid := true
			if keep, ok := edit["keep"]; ok {
				if str, isStr := keep.(string); isStr {
					if str != "all" {
						losses = append(losses, ir.Loss{
							Path:   editPath + ".keep",
							Field:  "keep",
							Reason: ir.LossUnmappedValue,
						})
						valid = false
					}
				} else if m, isMap := keep.(map[string]any); isMap {
					if m["type"] != "thinking_turns" {
						losses = append(losses, ir.Loss{
							Path:   editPath + ".keep",
							Field:  "keep",
							Reason: ir.LossUnmappedValue,
						})
						valid = false
					}
				} else {
					losses = append(losses, ir.Loss{
						Path:   editPath + ".keep",
						Field:  "keep",
						Reason: ir.LossUnmappedValue,
					})
					valid = false
				}
			}
			if valid {
				validEdits = append(validEdits, edit)
			}
		default:
			losses = append(losses, ir.Loss{
				Path:   editPath,
				Field:  "type",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("unsupported context edit type %q", editType),
			})
		}
	}
	if len(validEdits) == 0 {
		return "", losses, false
	}
	b, _ := json.Marshal(map[string]any{"edits": validEdits})
	return string(b), losses, true
}

func decodeRequestNative(wire *Request, req *ir.Request) ([]ir.Loss, error) {
	var losses []ir.Loss

	// 1. Thinking
	if wire.Thinking != nil {
		switch wire.Thinking.Type {
		case "adaptive", "disabled":
			req.Native = append(req.Native, ir.NativeAnnotation{
				Source:   ir.NativeSourceAnthropic,
				Kind:     ir.NativeKindThinkingMode,
				Value:    fmt.Sprintf("%q", wire.Thinking.Type),
				Omission: &ir.NativeOmission{Field: "type", Reason: ir.LossUnmappedField},
			})
		case "enabled":
			modeAnn := ir.NativeAnnotation{
				Source: ir.NativeSourceAnthropic,
				Kind:   ir.NativeKindThinkingMode,
				Value:  `"enabled"`,
			}
			if wire.OutputConfig != nil && wire.OutputConfig.Effort != "" {
				modeAnn.Omission = &ir.NativeOmission{Field: "type", Reason: ir.LossUnmappedField}
			}
			req.Native = append(req.Native, modeAnn)
			if wire.Thinking.BudgetTokens > 0 {
				if wire.OutputConfig != nil && wire.OutputConfig.Effort != "" {
					// Enabled budget + explicit effort: budget does not project to effort
					req.Native = append(req.Native, ir.NativeAnnotation{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindThinkingBudget,
						Value:    fmt.Sprintf("%d", wire.Thinking.BudgetTokens),
						Omission: &ir.NativeOmission{Field: "budget_tokens", Reason: ir.LossUnmappedField},
					})
				} else {
					// Enabled budget without explicit effort: projects to effort
					mapped := "high"
					if wire.Thinking.BudgetTokens <= 2048 {
						mapped = "low"
					} else if wire.Thinking.BudgetTokens <= 8192 {
						mapped = "medium"
					}
					req.Params.ReasoningEffort = mapped
					req.Native = append(req.Native, ir.NativeAnnotation{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindThinkingBudget,
						Value:    fmt.Sprintf("%d", wire.Thinking.BudgetTokens),
						Omission: &ir.NativeOmission{Field: "budget_tokens", Reason: ir.LossDegraded, Detail: "budget approximated"},
						Projection: &ir.NativeProjection{
							Origin: "budget",
							Effort: mapped,
						},
					})
				}
			}
		default:
			losses = append(losses, ir.Loss{
				Path:   "thinking.type",
				Field:  "type",
				Reason: ir.LossUnmappedValue,
				Detail: fmt.Sprintf("unknown thinking.type %q", wire.Thinking.Type),
			})
		}

		if wire.Thinking.Display != "" {
			switch wire.Thinking.Display {
			case "summarized", "omitted", "updates":
				req.Native = append(req.Native, ir.NativeAnnotation{
					Source:   ir.NativeSourceAnthropic,
					Kind:     ir.NativeKindThinkingDisplay,
					Value:    fmt.Sprintf("%q", wire.Thinking.Display),
					Omission: &ir.NativeOmission{Field: "display", Reason: ir.LossUnmappedField},
				})
			default:
				losses = append(losses, ir.Loss{
					Path:   "thinking.display",
					Field:  "display",
					Reason: ir.LossUnmappedValue,
					Detail: fmt.Sprintf("unknown thinking.display %q", wire.Thinking.Display),
				})
			}
		}
	}

	// 2. OutputConfig
	if wire.OutputConfig != nil && wire.OutputConfig.Effort != "" {
		switch wire.OutputConfig.Effort {
		case "low", "medium", "high", "xhigh", "max":
			req.Params.ReasoningEffort = wire.OutputConfig.Effort
			req.Native = append(req.Native, ir.NativeAnnotation{
				Source: ir.NativeSourceAnthropic,
				Kind:   ir.NativeKindOutputEffort,
				Value:  fmt.Sprintf("%q", wire.OutputConfig.Effort),
				Projection: &ir.NativeProjection{
					Origin: "explicit",
					Effort: wire.OutputConfig.Effort,
				},
			})
		default:
			losses = append(losses, ir.Loss{
				Path:   "output_config.effort",
				Field:  "effort",
				Reason: ir.LossUnmappedValue,
				Detail: fmt.Sprintf("unknown output_config.effort %q", wire.OutputConfig.Effort),
			})
		}
	}

	// 3. Top-level cache_control
	if len(wire.CacheControl) > 0 {
		canon, l, ok := parseCacheControl(wire.CacheControl, "cache_control")
		if l != nil {
			losses = append(losses, *l)
		}
		if ok {
			req.Native = append(req.Native, ir.NativeAnnotation{
				Source:   ir.NativeSourceAnthropic,
				Kind:     ir.NativeKindCacheControl,
				Value:    canon,
				Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField},
			})
		}
	}

	// 4. ContextManagement
	if wire.ContextManagement != nil {
		canon, cmLosses, ok := parseContextManagement(wire.ContextManagement, "context_management")
		losses = append(losses, cmLosses...)
		if ok {
			req.Native = append(req.Native, ir.NativeAnnotation{
				Source:   ir.NativeSourceAnthropic,
				Kind:     ir.NativeKindContextManagement,
				Value:    canon,
				Omission: &ir.NativeOmission{Field: "context_management", Reason: ir.LossUnsupportedSemantic},
			})
		}
	}

	// 5. System blocks cache_control
	switch sys := wire.System.(type) {
	case []SystemBlockWire:
		for i, sb := range sys {
			if len(sb.CacheControl) > 0 && i < len(req.System) {
				path := fmt.Sprintf("system[%d].cache_control", i)
				canon, l, ok := parseCacheControl(sb.CacheControl, path)
				if l != nil {
					losses = append(losses, *l)
				}
				if ok {
					req.System[i].Native = append(req.System[i].Native, ir.NativeAnnotation{
						Source:   ir.NativeSourceAnthropic,
						Kind:     ir.NativeKindCacheControl,
						Value:    canon,
						Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField},
					})
				}
			}
		}
	case []any:
		for i, part := range sys {
			if m, ok := part.(map[string]any); ok {
				if cc, ok := m["cache_control"]; ok && i < len(req.System) {
					path := fmt.Sprintf("system[%d].cache_control", i)
					ccBytes, _ := json.Marshal(cc)
					canon, l, ok := parseCacheControl(ccBytes, path)
					if l != nil {
						losses = append(losses, *l)
					}
					if ok {
						req.System[i].Native = append(req.System[i].Native, ir.NativeAnnotation{
							Source:   ir.NativeSourceAnthropic,
							Kind:     ir.NativeKindCacheControl,
							Value:    canon,
							Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField},
						})
					}
				}
			}
		}
	}

	// 6. Tools cache_control
	for i, tw := range wire.Tools {
		if len(tw.CacheControl) > 0 && i < len(req.Tools) {
			path := fmt.Sprintf("tools[%d].cache_control", i)
			canon, l, ok := parseCacheControl(tw.CacheControl, path)
			if l != nil {
				losses = append(losses, *l)
			}
			if ok {
				req.Tools[i].Native = append(req.Tools[i].Native, ir.NativeAnnotation{
					Source:   ir.NativeSourceAnthropic,
					Kind:     ir.NativeKindCacheControl,
					Value:    canon,
					Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField},
				})
			}
		}
	}

	// 7. Messages content cache_control (recursive for tool_result)
	for mIdx, msg := range wire.Messages {
		if mIdx >= len(req.Messages) {
			break
		}
		var blocks []BlockWire
		switch c := msg.Content.(type) {
		case []BlockWire:
			blocks = c
		case []any:
			for _, part := range c {
				var bw BlockWire
				raw, _ := json.Marshal(part)
				_ = json.Unmarshal(raw, &bw)
				blocks = append(blocks, bw)
			}
		}
		for cIdx, bw := range blocks {
			if cIdx >= len(req.Messages[mIdx].Content) {
				break
			}
			path := fmt.Sprintf("messages[%d].content[%d].cache_control", mIdx, cIdx)
			if len(bw.CacheControl) > 0 {
				canon, l, ok := parseCacheControl(bw.CacheControl, path)
				if l != nil {
					losses = append(losses, *l)
				}
				if ok {
					attachCacheControlToBlock(&req.Messages[mIdx].Content[cIdx], canon)
				}
			}

			if bw.Type == BlockTypeToolResult {
				if trb, ok := req.Messages[mIdx].Content[cIdx].(ir.ToolResultBlock); ok {
					var nestedWires []BlockWire
					switch nc := bw.Content.(type) {
					case []BlockWire:
						nestedWires = nc
					case []any:
						for _, part := range nc {
							var nbw BlockWire
							raw, _ := json.Marshal(part)
							_ = json.Unmarshal(raw, &nbw)
							nestedWires = append(nestedWires, nbw)
						}
					}
					for kIdx, nbw := range nestedWires {
						if kIdx >= len(trb.Content) {
							break
						}
						nPath := fmt.Sprintf("messages[%d].content[%d].content[%d].cache_control", mIdx, cIdx, kIdx)
						if len(nbw.CacheControl) > 0 {
							canon, l, ok := parseCacheControl(nbw.CacheControl, nPath)
							if l != nil {
								losses = append(losses, *l)
							}
							if ok {
								attachCacheControlToBlock(&trb.Content[kIdx], canon)
							}
						}
					}
					req.Messages[mIdx].Content[cIdx] = trb
				}
			}
		}
	}

	return losses, nil
}

func attachCacheControlToBlock(blk *ir.Block, canon string) {
	ann := ir.NativeAnnotation{
		Source:   ir.NativeSourceAnthropic,
		Kind:     ir.NativeKindCacheControl,
		Value:    canon,
		Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField},
	}
	switch b := (*blk).(type) {
	case ir.TextBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	case ir.ThinkingBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	case ir.ImageBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	case ir.ToolUseBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	case ir.ToolResultBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	case ir.CustomToolUseBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	case ir.CustomToolResultBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	case ir.ApplyPatchToolUseBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	case ir.ApplyPatchToolResultBlock:
		b.Native = append(b.Native, ann)
		*blk = b
	}
}

func encodeRequestNative(req *ir.Request, wire *Request) ([]ir.Loss, error) {
	var losses []ir.Loss

	// 1. Process Request.Native
	var nativeMode string
	var nativeBudget int64
	var nativeBudgetProj *ir.NativeProjection
	var nativeDisplay string
	var nativeEffort string
	var nativeEffortProj *ir.NativeProjection

	for _, n := range req.Native {
		if n.Source != ir.NativeSourceAnthropic {
			continue
		}
		switch n.Kind {
		case ir.NativeKindThinkingMode:
			_ = json.Unmarshal([]byte(n.Value), &nativeMode)
		case ir.NativeKindThinkingBudget:
			_ = json.Unmarshal([]byte(n.Value), &nativeBudget)
			nativeBudgetProj = n.Projection
		case ir.NativeKindThinkingDisplay:
			_ = json.Unmarshal([]byte(n.Value), &nativeDisplay)
		case ir.NativeKindOutputEffort:
			_ = json.Unmarshal([]byte(n.Value), &nativeEffort)
			nativeEffortProj = n.Projection
		case ir.NativeKindCacheControl:
			wire.CacheControl = json.RawMessage(n.Value)
		case ir.NativeKindContextManagement:
			var cm ContextManagementWire
			if err := json.Unmarshal([]byte(n.Value), &cm); err == nil {
				wire.ContextManagement = &cm
			}
		}
	}

	// Thinking / Effort synthesis
	if nativeMode != "" || nativeEffort != "" || req.Params.ReasoningEffort != "" {
		if nativeMode == "disabled" {
			wire.Thinking = &ThinkingWire{Type: "disabled"}
		} else if nativeMode == "adaptive" {
			wire.Thinking = &ThinkingWire{Type: "adaptive"}
			if nativeDisplay != "" {
				wire.Thinking.Display = nativeDisplay
			}
			if req.Params.ReasoningEffort != "" {
				wire.OutputConfig = &OutputConfigWire{Effort: req.Params.ReasoningEffort}
			}
		} else if nativeMode == "enabled" {
			if nativeBudget > 0 {
				if nativeBudgetProj != nil && nativeBudgetProj.Origin == "budget" {
					if req.Params.ReasoningEffort == nativeBudgetProj.Effort {
						// Effort unedited: exact restoration
						wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: nativeBudget}
					} else {
						// Effort edited: reconstruct budget from discrete table
						switch req.Params.ReasoningEffort {
						case "minimal":
							wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: 1024}
							losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "budget approximated"})
						case "low":
							wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: 2048}
							losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "budget approximated"})
						case "medium":
							wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: 8192}
							losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "budget approximated"})
						case "high":
							wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: 16384}
							losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "budget approximated"})
						case "xhigh", "max":
							wire.Thinking = &ThinkingWire{Type: "adaptive"}
							wire.OutputConfig = &OutputConfigWire{Effort: req.Params.ReasoningEffort}
							losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "switched to adaptive thinking mode for extended effort"})
						}
					}
				} else {
					// Enabled budget with explicit effort
					wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: nativeBudget}
					if req.Params.ReasoningEffort != "" {
						wire.OutputConfig = &OutputConfigWire{Effort: req.Params.ReasoningEffort}
					}
				}
			}
			if nativeDisplay != "" && wire.Thinking != nil {
				wire.Thinking.Display = nativeDisplay
			}
		} else {
			// No nativeMode recorded: standard IR ReasoningEffort mapping
			switch req.Params.ReasoningEffort {
			case "minimal":
				wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: 1024}
				losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "budget approximated"})
			case "low":
				wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: 2048}
				losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "budget approximated"})
			case "medium":
				wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: 8192}
				losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "budget approximated"})
			case "high":
				wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: 16384}
				losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "budget approximated"})
			case "xhigh", "max":
				wire.Thinking = &ThinkingWire{Type: "adaptive"}
				wire.OutputConfig = &OutputConfigWire{Effort: req.Params.ReasoningEffort}
			}
		}
	}

	_ = nativeEffortProj

	// 2. System blocks cache_control
	if sysBlocks, ok := wire.System.([]SystemBlockWire); ok {
		for i, sb := range req.System {
			if i < len(sysBlocks) {
				for _, n := range sb.Native {
					if n.Source == ir.NativeSourceAnthropic && n.Kind == ir.NativeKindCacheControl {
						sysBlocks[i].CacheControl = json.RawMessage(n.Value)
					}
				}
			}
		}
		wire.System = sysBlocks
	}

	// 3. Tools cache_control
	for i, tool := range req.Tools {
		if i < len(wire.Tools) {
			for _, n := range tool.Native {
				if n.Source == ir.NativeSourceAnthropic && n.Kind == ir.NativeKindCacheControl {
					wire.Tools[i].CacheControl = json.RawMessage(n.Value)
				}
			}
		}
	}

	return losses, nil
}

func getBlockCacheControl(item any) json.RawMessage {
	var nat []ir.NativeAnnotation
	switch b := item.(type) {
	case ir.Tool:
		nat = b.Native
	case ir.TextBlock:
		nat = b.Native
	case ir.ThinkingBlock:
		nat = b.Native
	case ir.ImageBlock:
		nat = b.Native
	case ir.ToolUseBlock:
		nat = b.Native
	case ir.ToolResultBlock:
		nat = b.Native
	case ir.CustomToolUseBlock:
		nat = b.Native
	case ir.CustomToolResultBlock:
		nat = b.Native
	case ir.ApplyPatchToolUseBlock:
		nat = b.Native
	case ir.ApplyPatchToolResultBlock:
		nat = b.Native
	}
	for _, n := range nat {
		if n.Source == ir.NativeSourceAnthropic && n.Kind == ir.NativeKindCacheControl {
			return json.RawMessage(n.Value)
		}
	}
	return nil
}
