package ir

import (
	"encoding/json"
	"fmt"
)

// NativeAnnotation represents a dialect-specific request option preserved across IR (spec/15).
type NativeAnnotation struct {
	Source     string            `json:"source"`
	Kind       string            `json:"kind"`
	Value      string            `json:"value"`
	Omission   *NativeOmission   `json:"omission,omitempty"`
	Projection *NativeProjection `json:"projection,omitempty"`
}

// NativeOmission describes the loss to report if the target face cannot represent this option.
type NativeOmission struct {
	Field  string     `json:"field"`
	Reason LossReason `json:"reason"`
	Detail string     `json:"detail,omitempty"`
}

// NativeProjection tracks the derivation of a common IR parameter from this native option.
type NativeProjection struct {
	Origin string `json:"origin"` // "budget" | "explicit"
	Effort string `json:"effort"`
}

// Known native annotation kinds and sources.
const (
	NativeKindThinkingMode      = "thinking_mode"
	NativeKindThinkingBudget    = "thinking_budget"
	NativeKindThinkingDisplay   = "thinking_display"
	NativeKindOutputEffort      = "output_effort"
	NativeKindCacheControl      = "cache_control"
	NativeKindContextManagement = "context_management"
)

const (
	NativeSourceAnthropic = "anthropic"
)

type hostType int

const (
	hostRequest hostType = iota
	hostSystem
	hostContentBlock
	hostTool
)

// ValidateNativeRequest checks structural validity, singleton constraints, valid JSON value,
// and host authorization of all native annotations in the request.
func ValidateNativeRequest(req *Request) error {
	if req == nil {
		return nil
	}

	if err := validateAnnotations(req.Native, hostRequest); err != nil {
		return fmt.Errorf("ir: request native invalid: %w", err)
	}

	var budgetProj *NativeProjection
	var explicitProj *NativeProjection
	for _, n := range req.Native {
		if n.Projection != nil {
			switch n.Projection.Origin {
			case "budget":
				if n.Kind != NativeKindThinkingBudget {
					return fmt.Errorf("ir: budget projection is only allowed on thinking_budget")
				}
				if budgetProj != nil {
					return fmt.Errorf("ir: duplicate budget projection on request")
				}
				budgetProj = n.Projection
			case "explicit":
				if n.Kind != NativeKindOutputEffort {
					return fmt.Errorf("ir: explicit projection is only allowed on output_effort")
				}
				if explicitProj != nil {
					return fmt.Errorf("ir: duplicate explicit projection on request")
				}
				explicitProj = n.Projection
			default:
				return fmt.Errorf("ir: native projection origin %q invalid", n.Projection.Origin)
			}
			switch n.Projection.Effort {
			case "minimal", "low", "medium", "high", "xhigh", "max":
			default:
				return fmt.Errorf("ir: native projection effort %q invalid", n.Projection.Effort)
			}
		}
	}
	if budgetProj != nil && explicitProj != nil {
		return fmt.Errorf("ir: conflicting effort projections on request")
	}

	for i, sb := range req.System {
		if err := validateAnnotations(sb.Native, hostSystem); err != nil {
			return fmt.Errorf("ir: system[%d] native invalid: %w", i, err)
		}
	}

	for mIdx, msg := range req.Messages {
		for cIdx, blk := range msg.Content {
			if err := validateBlockAnnotations(blk, fmt.Sprintf("messages[%d].content[%d]", mIdx, cIdx)); err != nil {
				return err
			}
		}
	}

	for tIdx, tool := range req.Tools {
		if err := validateAnnotations(tool.Native, hostTool); err != nil {
			return fmt.Errorf("ir: tools[%d] native invalid: %w", tIdx, err)
		}
	}

	return nil
}

func validateAnnotations(annotations []NativeAnnotation, host hostType) error {
	seen := make(map[string]bool)
	for i, ann := range annotations {
		if ann.Source == "" {
			return fmt.Errorf("annotation[%d] source cannot be empty", i)
		}
		if ann.Kind == "" {
			return fmt.Errorf("annotation[%d] kind cannot be empty", i)
		}
		var js json.RawMessage
		if err := json.Unmarshal([]byte(ann.Value), &js); err != nil {
			return fmt.Errorf("annotation[%d] value is not valid JSON text: %w", i, err)
		}
		if ann.Omission != nil {
			if ann.Omission.Field == "" {
				return fmt.Errorf("annotation[%d] omission field cannot be empty", i)
			}
			switch ann.Omission.Reason {
			case LossUnmappedField, LossUnmappedValue, LossUnsupportedSemantic, LossDegraded:
			default:
				return fmt.Errorf("annotation[%d] omission reason %q invalid", i, ann.Omission.Reason)
			}
		}

		key := ann.Source + ":" + ann.Kind
		if seen[key] {
			return fmt.Errorf("duplicate native annotation for %s", key)
		}
		seen[key] = true

		if ann.Source == NativeSourceAnthropic {
			switch ann.Kind {
			case NativeKindThinkingMode:
				if host != hostRequest {
					return fmt.Errorf("anthropic %s is only allowed on Request", ann.Kind)
				}
				var str string
				if err := json.Unmarshal([]byte(ann.Value), &str); err != nil {
					return fmt.Errorf("anthropic thinking_mode value must be a JSON string: %w", err)
				}
				if str != "adaptive" && str != "enabled" && str != "disabled" {
					return fmt.Errorf("invalid thinking_mode %q", str)
				}
			case NativeKindThinkingBudget:
				if host != hostRequest {
					return fmt.Errorf("anthropic %s is only allowed on Request", ann.Kind)
				}
				var budget int64
				if err := json.Unmarshal([]byte(ann.Value), &budget); err != nil || budget <= 0 {
					return fmt.Errorf("anthropic thinking_budget must be a positive integer")
				}
			case NativeKindThinkingDisplay:
				if host != hostRequest {
					return fmt.Errorf("anthropic %s is only allowed on Request", ann.Kind)
				}
				var str string
				if err := json.Unmarshal([]byte(ann.Value), &str); err != nil || (str != "summarized" && str != "omitted" && str != "updates") {
					return fmt.Errorf("invalid thinking_display %s", ann.Value)
				}
			case NativeKindOutputEffort:
				if host != hostRequest {
					return fmt.Errorf("anthropic %s is only allowed on Request", ann.Kind)
				}
				var str string
				if err := json.Unmarshal([]byte(ann.Value), &str); err != nil || (str != "low" && str != "medium" && str != "high" && str != "xhigh" && str != "max") {
					return fmt.Errorf("invalid output_effort %s", ann.Value)
				}
			case NativeKindContextManagement:
				if host != hostRequest {
					return fmt.Errorf("anthropic %s is only allowed on Request", ann.Kind)
				}
				var obj map[string]any
				if err := json.Unmarshal([]byte(ann.Value), &obj); err != nil {
					return fmt.Errorf("invalid context_management %s", ann.Value)
				}
				if _, ok := obj["edits"].([]any); !ok {
					return fmt.Errorf("context_management requires edits array")
				}
			case NativeKindCacheControl:
				var obj map[string]any
				if err := json.Unmarshal([]byte(ann.Value), &obj); err != nil || obj["type"] != "ephemeral" {
					return fmt.Errorf("invalid cache_control %s", ann.Value)
				}
				if ttl, ok := obj["ttl"]; ok {
					if ttl != "5m" && ttl != "1h" {
						return fmt.Errorf("invalid cache_control ttl %v", ttl)
					}
				}
			default:
			}
		}
	}
	return nil
}

func validateBlockAnnotations(b Block, path string) error {
	var nat []NativeAnnotation
	switch blk := b.(type) {
	case TextBlock:
		nat = blk.Native
	case ImageBlock:
		nat = blk.Native
	case ToolUseBlock:
		nat = blk.Native
	case ToolResultBlock:
		nat = blk.Native
		for k, child := range blk.Content {
			if err := validateBlockAnnotations(child, fmt.Sprintf("%s.content[%d]", path, k)); err != nil {
				return err
			}
		}
	case ThinkingBlock:
		nat = blk.Native
	case CustomToolUseBlock:
		nat = blk.Native
	case CustomToolResultBlock:
		nat = blk.Native
		for k, child := range blk.Output.Content {
			if err := validateBlockAnnotations(child, fmt.Sprintf("%s.output.content[%d]", path, k)); err != nil {
				return err
			}
		}
	case ApplyPatchToolUseBlock:
		nat = blk.Native
	case ApplyPatchToolResultBlock:
		nat = blk.Native
	}
	if err := validateAnnotations(nat, hostContentBlock); err != nil {
		return fmt.Errorf("ir: %s native invalid: %w", path, err)
	}
	return nil
}

// OmittedNativeRequestLosses computes all losses resulting from omitting unsupported native annotations
// when targeting a face (such as "chatcompletions" or "responses").
func OmittedNativeRequestLosses(req *Request, target string) ([]Loss, error) {
	if req == nil {
		return nil, nil
	}
	if err := ValidateNativeRequest(req); err != nil {
		return nil, err
	}

	var losses []Loss

	// Check if request has an active, unedited budget projection
	hasActiveBudgetProj := false
	for _, n := range req.Native {
		if n.Source == NativeSourceAnthropic && n.Kind == NativeKindThinkingBudget && n.Projection != nil {
			if n.Projection.Origin == "budget" && n.Projection.Effort == req.Params.ReasoningEffort {
				hasActiveBudgetProj = true
				break
			}
		}
	}

	// 1. Request.Native
	for i, ann := range req.Native {
		path := fmt.Sprintf("native[%d]", i)
		losses = appendAnnotationLoss(losses, ann, path, hasActiveBudgetProj, req.Params.ReasoningEffort)
	}

	// 2. System blocks
	for sIdx, sb := range req.System {
		for i, ann := range sb.Native {
			path := fmt.Sprintf("system[%d].native[%d]", sIdx, i)
			losses = appendAnnotationLoss(losses, ann, path, false, "")
		}
	}

	// 3. Messages & Content blocks (depth-first)
	for mIdx, msg := range req.Messages {
		for cIdx, blk := range msg.Content {
			basePath := fmt.Sprintf("messages[%d].content[%d]", mIdx, cIdx)
			losses = collectBlockOmissionLosses(losses, blk, basePath)
		}
	}

	// 4. Tools
	for tIdx, tool := range req.Tools {
		for i, ann := range tool.Native {
			path := fmt.Sprintf("tools[%d].native[%d]", tIdx, i)
			losses = appendAnnotationLoss(losses, ann, path, false, "")
		}
	}

	return losses, nil
}

func collectBlockOmissionLosses(losses []Loss, b Block, path string) []Loss {
	var nat []NativeAnnotation
	var children []Block
	switch blk := b.(type) {
	case TextBlock:
		nat = blk.Native
	case ImageBlock:
		nat = blk.Native
	case ToolUseBlock:
		nat = blk.Native
	case ToolResultBlock:
		nat = blk.Native
		children = blk.Content
	case ThinkingBlock:
		nat = blk.Native
	case CustomToolUseBlock:
		nat = blk.Native
	case CustomToolResultBlock:
		nat = blk.Native
		children = blk.Output.Content
	case ApplyPatchToolUseBlock:
		nat = blk.Native
	case ApplyPatchToolResultBlock:
		nat = blk.Native
	}

	for i, ann := range nat {
		p := fmt.Sprintf("%s.native[%d]", path, i)
		losses = appendAnnotationLoss(losses, ann, p, false, "")
	}

	for k, child := range children {
		childPath := fmt.Sprintf("%s.content[%d]", path, k)
		losses = collectBlockOmissionLosses(losses, child, childPath)
	}
	return losses
}

func appendAnnotationLoss(losses []Loss, ann NativeAnnotation, path string, hasActiveBudgetProj bool, currentEffort string) []Loss {
	if ann.Source != NativeSourceAnthropic {
		// Unknown source
		return append(losses, Loss{
			Path:   path,
			Field:  "source",
			Reason: LossUnsupportedSemantic,
			Detail: fmt.Sprintf("unsupported native option source %q", ann.Source),
		})
	}

	switch ann.Kind {
	case NativeKindOutputEffort:
		// Effort is mapped to common IR ReasoningEffort; no omission loss.
		return losses

	case NativeKindThinkingMode:
		if hasActiveBudgetProj {
			// Mode loss is covered by the budget degraded loss.
			return losses
		}
		if ann.Omission != nil {
			return append(losses, Loss{
				Path:   path,
				Field:  ann.Omission.Field,
				Reason: ann.Omission.Reason,
				Detail: ann.Omission.Detail,
			})
		}
		return append(losses, Loss{
			Path:   path,
			Field:  "type",
			Reason: LossUnmappedField,
		})

	case NativeKindThinkingBudget:
		if ann.Projection != nil && ann.Projection.Origin == "budget" {
			if ann.Projection.Effort == currentEffort {
				// Valid budget projection: emit budget degraded loss
				if ann.Omission != nil {
					return append(losses, Loss{
						Path:   path,
						Field:  ann.Omission.Field,
						Reason: ann.Omission.Reason,
						Detail: ann.Omission.Detail,
					})
				}
			}
			// If edited (ann.Projection.Effort != currentEffort), stale budget is suppressed.
			return losses
		}
		// Enabled budget + explicit effort: emit omission loss (unmapped-field)
		if ann.Omission != nil {
			return append(losses, Loss{
				Path:   path,
				Field:  ann.Omission.Field,
				Reason: ann.Omission.Reason,
				Detail: ann.Omission.Detail,
			})
		}

	case NativeKindThinkingDisplay, NativeKindCacheControl, NativeKindContextManagement:
		if ann.Omission != nil {
			return append(losses, Loss{
				Path:   path,
				Field:  ann.Omission.Field,
				Reason: ann.Omission.Reason,
				Detail: ann.Omission.Detail,
			})
		}

	default:
		// Source known ("anthropic") but kind unknown
		return append(losses, Loss{
			Path:   path,
			Field:  "kind",
			Reason: LossUnsupportedSemantic,
			Detail: fmt.Sprintf("unsupported native option kind %q", ann.Kind),
		})
	}

	return losses
}
