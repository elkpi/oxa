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
			for k := range edit {
				switch k {
				case "type", "trigger", "keep", "clear_at_least", "exclude_tools", "clear_tool_inputs":
				default:
					losses = append(losses, ir.Loss{
						Path:   editPath + "." + k,
						Field:  k,
						Reason: ir.LossUnmappedField,
					})
				}
			}
			valid := true
			canonEdit := map[string]any{"type": "clear_tool_uses_20250919"}
			if trigVal, ok := edit["trigger"]; ok {
				if trig, isObj := trigVal.(map[string]any); isObj {
					for k := range trig {
						if k != "type" && k != "value" {
							losses = append(losses, ir.Loss{
								Path:   editPath + ".trigger." + k,
								Field:  k,
								Reason: ir.LossUnmappedField,
							})
						}
					}
					if trig["type"] != "input_tokens" {
						losses = append(losses, ir.Loss{
							Path:   editPath + ".trigger",
							Field:  "trigger",
							Reason: ir.LossUnmappedValue,
						})
						valid = false
					} else {
						canonEdit["trigger"] = map[string]any{"type": "input_tokens", "value": trig["value"]}
					}
				} else {
					losses = append(losses, ir.Loss{
						Path:   editPath + ".trigger",
						Field:  "trigger",
						Reason: ir.LossUnmappedValue,
					})
					valid = false
				}
			}
			if keepVal, ok := edit["keep"]; ok {
				if keep, isObj := keepVal.(map[string]any); isObj {
					for k := range keep {
						if k != "type" && k != "value" {
							losses = append(losses, ir.Loss{
								Path:   editPath + ".keep." + k,
								Field:  k,
								Reason: ir.LossUnmappedField,
							})
						}
					}
					if keep["type"] != "tool_uses" {
						losses = append(losses, ir.Loss{
							Path:   editPath + ".keep",
							Field:  "keep",
							Reason: ir.LossUnmappedValue,
						})
						valid = false
					} else {
						canonEdit["keep"] = map[string]any{"type": "tool_uses", "value": keep["value"]}
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
			if calVal, ok := edit["clear_at_least"]; ok {
				if cal, isObj := calVal.(map[string]any); isObj {
					for k := range cal {
						if k != "type" && k != "value" {
							losses = append(losses, ir.Loss{
								Path:   editPath + ".clear_at_least." + k,
								Field:  k,
								Reason: ir.LossUnmappedField,
							})
						}
					}
					if cal["type"] != "input_tokens" {
						losses = append(losses, ir.Loss{
							Path:   editPath + ".clear_at_least",
							Field:  "clear_at_least",
							Reason: ir.LossUnmappedValue,
						})
						valid = false
					} else {
						canonEdit["clear_at_least"] = map[string]any{"type": "input_tokens", "value": cal["value"]}
					}
				} else {
					losses = append(losses, ir.Loss{
						Path:   editPath + ".clear_at_least",
						Field:  "clear_at_least",
						Reason: ir.LossUnmappedValue,
					})
					valid = false
				}
			}
			if et, ok := edit["exclude_tools"]; ok {
				canonEdit["exclude_tools"] = et
			}
			if cti, ok := edit["clear_tool_inputs"]; ok {
				canonEdit["clear_tool_inputs"] = cti
			}
			if valid {
				validEdits = append(validEdits, canonEdit)
			}
		case "clear_thinking_20251015":
			for k := range edit {
				if k != "type" && k != "keep" {
					losses = append(losses, ir.Loss{
						Path:   editPath + "." + k,
						Field:  k,
						Reason: ir.LossUnmappedField,
					})
				}
			}
			valid := true
			canonEdit := map[string]any{"type": "clear_thinking_20251015"}
			if keep, ok := edit["keep"]; ok {
				if str, isStr := keep.(string); isStr {
					if str != "all" {
						losses = append(losses, ir.Loss{
							Path:   editPath + ".keep",
							Field:  "keep",
							Reason: ir.LossUnmappedValue,
						})
						valid = false
					} else {
						canonEdit["keep"] = "all"
					}
				} else if m, isMap := keep.(map[string]any); isMap {
					for k := range m {
						if k != "type" && k != "value" {
							losses = append(losses, ir.Loss{
								Path:   editPath + ".keep." + k,
								Field:  k,
								Reason: ir.LossUnmappedField,
							})
						}
					}
					if m["type"] != "thinking_turns" {
						losses = append(losses, ir.Loss{
							Path:   editPath + ".keep",
							Field:  "keep",
							Reason: ir.LossUnmappedValue,
						})
						valid = false
					} else {
						canonEdit["keep"] = map[string]any{"type": "thinking_turns", "value": m["value"]}
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
				validEdits = append(validEdits, canonEdit)
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
		case "between_tools":
			if wire.Thinking.hasBudget || wire.Thinking.hasDisplay || wire.Thinking.BudgetTokens > 0 || wire.Thinking.Display != "" {
				return nil, fmt.Errorf("anthropic: cannot specify budget_tokens or display with thinking.type between_tools")
			}
			req.Native = append(req.Native, ir.NativeAnnotation{
				Source:   ir.NativeSourceAnthropic,
				Kind:     ir.NativeKindThinkingMode,
				Value:    `"between_tools"`,
				Omission: &ir.NativeOmission{Field: "type", Reason: ir.LossUnmappedField},
			})
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
			if !wire.Thinking.hasBudget || wire.Thinking.BudgetTokens <= 0 {
				return nil, fmt.Errorf("anthropic: thinking.budget_tokens must be a positive integer")
			}
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
				if wire.Thinking.BudgetTokens <= 1024 {
					mapped = "minimal"
				} else if wire.Thinking.BudgetTokens <= 2048 {
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

	if wire.OutputConfig != nil && len(wire.OutputConfig.Format) > 0 {
		var format jsonOutputFormatWire
		if err := json.Unmarshal(wire.OutputConfig.Format, &format); err != nil || format.Type == "" {
			return nil, fmt.Errorf("anthropic: output_config.format requires an object with a non-empty type")
		}
		if format.Type != "json_schema" {
			losses = append(losses, ir.Loss{Path: "output_config.format.type", Field: "type", Reason: ir.LossUnmappedValue})
		} else {
			var schema map[string]json.RawMessage
			if err := json.Unmarshal(format.Schema, &schema); err != nil || schema == nil {
				return nil, fmt.Errorf("anthropic: output_config.format.schema must be an object")
			}
			req.Params.ResponseFormat = &ir.ResponseFormat{Type: ir.ResponseFormatJSONSchema, Schema: format.Schema}
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
				Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
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

	// 5. TopK
	if wire.TopK != nil {
		if *wire.TopK < 0 || *wire.TopK > 9007199254740991 {
			return nil, fmt.Errorf("anthropic: top_k must be a non-negative safe integer")
		}
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source:   ir.NativeSourceAnthropic,
			Kind:     ir.NativeKindTopK,
			Value:    fmt.Sprintf("%d", *wire.TopK),
			Omission: &ir.NativeOmission{Field: "top_k", Reason: ir.LossUnmappedField},
		})
	}

	return losses, nil
}

// cacheControlLossDetail reports the spec/15 s3.2 capability-gap detail for
// Anthropic cache breakpoints dropped when crossing to a face without
// cache_control representation.
const cacheControlLossDetail = ir.CacheControlLossDetail

func attachCacheControlToBlock(blk *ir.Block, canon string) {
	ann := ir.NativeAnnotation{
		Source:   ir.NativeSourceAnthropic,
		Kind:     ir.NativeKindCacheControl,
		Value:    canon,
		Omission: &ir.NativeOmission{Field: "cache_control", Reason: ir.LossUnmappedField, Detail: cacheControlLossDetail},
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
	case ir.DocumentBlock:
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

	for i, n := range req.Native {
		if n.Source == ir.NativeSourceOpenAI {
			switch n.Kind {
			case ir.NativeKindMaxCompletionTokens, ir.NativeKindStream:
				// no loss
			case ir.NativeKindStore:
				var b bool
				if err := json.Unmarshal([]byte(n.Value), &b); err == nil && b {
					field := "store"
					reason := ir.LossUnsupportedSemantic
					detail := "target does not support output retention"
					if n.Omission != nil {
						if n.Omission.Field != "" {
							field = n.Omission.Field
						}
						if n.Omission.Reason != "" {
							reason = n.Omission.Reason
						}
						if n.Omission.Detail != "" {
							detail = n.Omission.Detail
						}
					}
					losses = append(losses, ir.Loss{
						Path:   fmt.Sprintf("native[%d]", i),
						Field:  field,
						Reason: reason,
						Detail: detail,
					})
				}
			case ir.NativeKindStreamOptions:
				var so struct {
					IncludeUsage *bool `json:"include_usage"`
				}
				if err := json.Unmarshal([]byte(n.Value), &so); err == nil && so.IncludeUsage != nil && *so.IncludeUsage {
					field := "stream_options"
					reason := ir.LossDegraded
					detail := ir.StreamOptionsLossDetail
					if n.Omission != nil {
						if n.Omission.Field != "" {
							field = n.Omission.Field
						}
						if n.Omission.Reason != "" {
							reason = n.Omission.Reason
						}
						if n.Omission.Detail != "" {
							detail = n.Omission.Detail
						}
					}
					losses = append(losses, ir.Loss{
						Path:   fmt.Sprintf("native[%d]", i),
						Field:  field,
						Reason: reason,
						Detail: detail,
					})
				}
			default:
				if n.Omission != nil {
					losses = append(losses, ir.Loss{
						Path:   fmt.Sprintf("native[%d]", i),
						Field:  n.Omission.Field,
						Reason: n.Omission.Reason,
						Detail: n.Omission.Detail,
					})
				} else {
					losses = append(losses, ir.Loss{
						Path:   fmt.Sprintf("native[%d]", i),
						Field:  "kind",
						Reason: ir.LossUnsupportedSemantic,
						Detail: fmt.Sprintf("unsupported native option kind %q", n.Kind),
					})
				}
			}
			continue
		}
		if n.Source != ir.NativeSourceAnthropic {
			losses = append(losses, ir.Loss{
				Path:   fmt.Sprintf("native[%d]", i),
				Field:  "source",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("unsupported native option source %q", n.Source),
			})
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
			canon, l, ok := parseCacheControl(json.RawMessage(n.Value), fmt.Sprintf("native[%d].cache_control", i))
			if l != nil {
				losses = append(losses, *l)
			}
			if ok {
				wire.CacheControl = json.RawMessage(canon)
			}
		case ir.NativeKindContextManagement:
			var cm ContextManagementWire
			if err := json.Unmarshal([]byte(n.Value), &cm); err == nil {
				wire.ContextManagement = &cm
			}
		case ir.NativeKindTopK:
			var k int64
			if err := json.Unmarshal([]byte(n.Value), &k); err == nil {
				wire.TopK = &k
			}
		case ir.NativeKindDisableParallelToolUse:
			var b bool
			if err := json.Unmarshal([]byte(n.Value), &b); err == nil {
				if wire.ToolChoice == nil || wire.ToolChoice.Type == ToolChoiceTypeNone {
					return nil, fmt.Errorf("anthropic: cannot specify disable_parallel_tool_use when tool_choice is none or not specified")
				}
				if req.ToolChoice == nil || req.ToolChoice.DisableParallel == nil {
					wire.ToolChoice.DisableParallelToolUse = b
					wire.ToolChoice.HasDisableParallelToolUse = true
				}
			}
		default:
			losses = append(losses, ir.Loss{
				Path:   fmt.Sprintf("native[%d]", i),
				Field:  "kind",
				Reason: ir.LossUnsupportedSemantic,
				Detail: fmt.Sprintf("unsupported native option kind %q", n.Kind),
			})
		}
	}

	// Thinking / Effort synthesis
	currentEffort := req.Params.ReasoningEffort
	if nativeMode != "" || nativeEffort != "" || currentEffort != "" {
		switch nativeMode {
		case "between_tools":
			if nativeBudget != 0 || nativeDisplay != "" {
				return nil, fmt.Errorf("anthropic: cannot specify budget_tokens or display with thinking.type between_tools")
			}
			wire.Thinking = &ThinkingWire{Type: "between_tools"}
			if currentEffort != "" {
				wire.OutputConfig = &OutputConfigWire{Effort: currentEffort}
			}
		case "disabled":
			wire.Thinking = &ThinkingWire{Type: "disabled"}
			if nativeEffort != "" || nativeEffortProj != nil {
				if currentEffort != "" {
					wire.OutputConfig = &OutputConfigWire{Effort: currentEffort}
				}
			}
		case "adaptive":
			wire.Thinking = &ThinkingWire{Type: "adaptive"}
			if nativeDisplay != "" {
				wire.Thinking.Display = nativeDisplay
			}
			if currentEffort != "" {
				wire.OutputConfig = &OutputConfigWire{Effort: currentEffort}
			}
		case "enabled":
			if nativeBudget > 0 {
				if nativeBudgetProj != nil && nativeBudgetProj.Origin == "budget" {
					if currentEffort == nativeBudgetProj.Effort {
						// Effort unedited: exact restoration
						wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: nativeBudget}
						if nativeDisplay != "" {
							wire.Thinking.Display = nativeDisplay
						}
					} else if currentEffort != "" {
						// Effort edited: reconstruct budget from discrete table
						switch currentEffort {
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
							wire.OutputConfig = &OutputConfigWire{Effort: currentEffort}
							losses = append(losses, ir.Loss{Path: "params.reasoning_effort", Field: "reasoning_effort", Reason: ir.LossDegraded, Detail: "switched to adaptive thinking mode for extended effort"})
						}
						if nativeDisplay != "" && wire.Thinking != nil {
							wire.Thinking.Display = nativeDisplay
						}
					} else {
						// Effort was cleared! Thinking cannot be formed:
						wire.Thinking = nil
						losses = append(losses, ir.Loss{
							Path:   "thinking",
							Field:  "thinking",
							Reason: ir.LossUnsupportedSemantic,
							Detail: "thinking configuration invalidated after clearing reasoning_effort",
						})
					}
				} else {
					// Enabled budget with explicit effort
					wire.Thinking = &ThinkingWire{Type: "enabled", BudgetTokens: nativeBudget}
					if nativeDisplay != "" {
						wire.Thinking.Display = nativeDisplay
					}
					if currentEffort != "" {
						wire.OutputConfig = &OutputConfigWire{Effort: currentEffort}
					}
				}
			}
		default:
			// No nativeMode recorded:
			if nativeEffort != "" || nativeEffortProj != nil {
				// Explicit effort in native annotation
				if currentEffort != "" {
					wire.OutputConfig = &OutputConfigWire{Effort: currentEffort}
				}
			} else {
				// Standard unannotated IR ReasoningEffort mapping (budget table)
				switch currentEffort {
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
					wire.OutputConfig = &OutputConfigWire{Effort: currentEffort}
				}
			}
		}
	}

	return losses, nil
}

func getBlockCacheControl(item any) json.RawMessage {
	var nat []ir.NativeAnnotation
	switch b := item.(type) {
	case ir.SystemBlock:
		nat = b.Native
	case ir.Tool:
		nat = b.Native
	case ir.TextBlock:
		nat = b.Native
	case ir.ThinkingBlock:
		nat = b.Native
	case ir.ImageBlock:
		nat = b.Native
	case ir.DocumentBlock:
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

func getToolDeferLoading(tool ir.Tool) *bool {
	for _, n := range tool.Native {
		if n.Kind == ir.NativeKindDeferLoading {
			var b bool
			if err := json.Unmarshal([]byte(n.Value), &b); err == nil && b {
				val := true
				return &val
			}
		}
	}
	return nil
}

func getToolEagerInputStreaming(tool ir.Tool) *bool {
	for _, n := range tool.Native {
		if n.Kind == ir.NativeKindEagerInputStreaming {
			var b bool
			if err := json.Unmarshal([]byte(n.Value), &b); err == nil {
				return &b
			}
		}
	}
	return nil
}
