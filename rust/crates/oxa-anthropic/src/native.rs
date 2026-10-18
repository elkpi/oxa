use oxa_ir::{
    Loss, LossReason, NATIVE_KIND_CACHE_CONTROL, NATIVE_KIND_CONTEXT_MANAGEMENT,
    NATIVE_KIND_OUTPUT_EFFORT, NATIVE_KIND_THINKING_BUDGET, NATIVE_KIND_THINKING_DISPLAY,
    NATIVE_KIND_THINKING_MODE, NATIVE_SOURCE_ANTHROPIC, NativeAnnotation, NativeOmission,
    NativeProjection, ReasoningEffort, Request as IrRequest,
};
use serde_json::Value;

use crate::types::{ContextManagementWire, OutputConfigWire, Request, ThinkingWire};

pub fn parse_cache_control(raw: &Value, path: &str) -> (Option<String>, Option<Loss>) {
    let Some(obj) = raw.as_object() else {
        return (None, None);
    };
    if obj.get("type").and_then(|v| v.as_str()) != Some("ephemeral") {
        return (
            None,
            Some(Loss {
                path: format!("{path}.type"),
                field: "type".into(),
                reason: LossReason::UnmappedValue,
                detail: "cache_control type must be ephemeral".into(),
            }),
        );
    }
    let mut canon = serde_json::Map::new();
    if let Some(ttl_val) = obj.get("ttl") {
        let Some(ttl_str) = ttl_val.as_str() else {
            return (
                None,
                Some(Loss {
                    path: format!("{path}.ttl"),
                    field: "ttl".into(),
                    reason: LossReason::UnmappedValue,
                    detail: "cache_control ttl must be 5m or 1h".into(),
                }),
            );
        };
        if ttl_str != "5m" && ttl_str != "1h" {
            return (
                None,
                Some(Loss {
                    path: format!("{path}.ttl"),
                    field: "ttl".into(),
                    reason: LossReason::UnmappedValue,
                    detail: "cache_control ttl must be 5m or 1h".into(),
                }),
            );
        }
        canon.insert("ttl".into(), Value::String(ttl_str.into()));
    }
    canon.insert("type".into(), Value::String("ephemeral".into()));
    (Some(Value::Object(canon).to_string()), None)
}

fn parse_context_management(cm: &ContextManagementWire, path: &str) -> (Option<String>, Vec<Loss>) {
    let mut losses = Vec::new();
    let Some(ref edits) = cm.edits else {
        return (None, losses);
    };
    let mut valid_edits = Vec::new();

    for (i, raw) in edits.iter().enumerate() {
        let edit_path = format!("{path}.edits[{i}]");
        let Some(edit) = raw.as_object() else {
            continue;
        };
        let edit_type = edit
            .get("type")
            .and_then(|v| v.as_str())
            .unwrap_or_default();
        match edit_type {
            "clear_tool_uses_20250919" => {
                for k in edit.keys() {
                    if k != "type"
                        && k != "trigger"
                        && k != "keep"
                        && k != "clear_at_least"
                        && k != "exclude_tools"
                        && k != "clear_tool_inputs"
                    {
                        losses.push(Loss {
                            path: format!("{edit_path}.{k}"),
                            field: k.clone(),
                            reason: LossReason::UnmappedField,
                            detail: String::new(),
                        });
                    }
                }
                let mut valid = true;
                let mut canon_edit = serde_json::Map::new();
                canon_edit.insert(
                    "type".into(),
                    Value::String("clear_tool_uses_20250919".into()),
                );

                if let Some(trig_val) = edit.get("trigger") {
                    if let Some(trig) = trig_val.as_object() {
                        for k in trig.keys() {
                            if k != "type" && k != "value" {
                                losses.push(Loss {
                                    path: format!("{edit_path}.trigger.{k}"),
                                    field: k.clone(),
                                    reason: LossReason::UnmappedField,
                                    detail: String::new(),
                                });
                            }
                        }
                        if trig.get("type").and_then(|v| v.as_str()) != Some("input_tokens") {
                            losses.push(Loss {
                                path: format!("{edit_path}.trigger"),
                                field: "trigger".into(),
                                reason: LossReason::UnmappedValue,
                                detail: String::new(),
                            });
                            valid = false;
                        } else {
                            let mut trig_obj = serde_json::Map::new();
                            trig_obj.insert("type".into(), Value::String("input_tokens".into()));
                            if let Some(v) = trig.get("value") {
                                trig_obj.insert("value".into(), v.clone());
                            }
                            canon_edit.insert("trigger".into(), Value::Object(trig_obj));
                        }
                    } else {
                        losses.push(Loss {
                            path: format!("{edit_path}.trigger"),
                            field: "trigger".into(),
                            reason: LossReason::UnmappedValue,
                            detail: String::new(),
                        });
                        valid = false;
                    }
                }

                if let Some(keep_val) = edit.get("keep") {
                    if let Some(keep) = keep_val.as_object() {
                        for k in keep.keys() {
                            if k != "type" && k != "value" {
                                losses.push(Loss {
                                    path: format!("{edit_path}.keep.{k}"),
                                    field: k.clone(),
                                    reason: LossReason::UnmappedField,
                                    detail: String::new(),
                                });
                            }
                        }
                        if keep.get("type").and_then(|v| v.as_str()) != Some("tool_uses") {
                            losses.push(Loss {
                                path: format!("{edit_path}.keep"),
                                field: "keep".into(),
                                reason: LossReason::UnmappedValue,
                                detail: String::new(),
                            });
                            valid = false;
                        } else {
                            let mut keep_obj = serde_json::Map::new();
                            keep_obj.insert("type".into(), Value::String("tool_uses".into()));
                            if let Some(v) = keep.get("value") {
                                keep_obj.insert("value".into(), v.clone());
                            }
                            canon_edit.insert("keep".into(), Value::Object(keep_obj));
                        }
                    } else {
                        losses.push(Loss {
                            path: format!("{edit_path}.keep"),
                            field: "keep".into(),
                            reason: LossReason::UnmappedValue,
                            detail: String::new(),
                        });
                        valid = false;
                    }
                }

                if let Some(cal_val) = edit.get("clear_at_least") {
                    if let Some(cal) = cal_val.as_object() {
                        for k in cal.keys() {
                            if k != "type" && k != "value" {
                                losses.push(Loss {
                                    path: format!("{edit_path}.clear_at_least.{k}"),
                                    field: k.clone(),
                                    reason: LossReason::UnmappedField,
                                    detail: String::new(),
                                });
                            }
                        }
                        if cal.get("type").and_then(|v| v.as_str()) != Some("input_tokens") {
                            losses.push(Loss {
                                path: format!("{edit_path}.clear_at_least"),
                                field: "clear_at_least".into(),
                                reason: LossReason::UnmappedValue,
                                detail: String::new(),
                            });
                            valid = false;
                        } else {
                            let mut cal_obj = serde_json::Map::new();
                            cal_obj.insert("type".into(), Value::String("input_tokens".into()));
                            if let Some(v) = cal.get("value") {
                                cal_obj.insert("value".into(), v.clone());
                            }
                            canon_edit.insert("clear_at_least".into(), Value::Object(cal_obj));
                        }
                    } else {
                        losses.push(Loss {
                            path: format!("{edit_path}.clear_at_least"),
                            field: "clear_at_least".into(),
                            reason: LossReason::UnmappedValue,
                            detail: String::new(),
                        });
                        valid = false;
                    }
                }

                if let Some(et) = edit.get("exclude_tools") {
                    canon_edit.insert("exclude_tools".into(), et.clone());
                }
                if let Some(cti) = edit.get("clear_tool_inputs") {
                    canon_edit.insert("clear_tool_inputs".into(), cti.clone());
                }

                if valid {
                    valid_edits.push(Value::Object(canon_edit));
                }
            }
            "clear_thinking_20251015" => {
                for k in edit.keys() {
                    if k != "type" && k != "keep" {
                        losses.push(Loss {
                            path: format!("{edit_path}.{k}"),
                            field: k.clone(),
                            reason: LossReason::UnmappedField,
                            detail: String::new(),
                        });
                    }
                }
                let mut valid = true;
                let mut canon_edit = serde_json::Map::new();
                canon_edit.insert(
                    "type".into(),
                    Value::String("clear_thinking_20251015".into()),
                );

                if let Some(keep) = edit.get("keep") {
                    if let Some(str_val) = keep.as_str() {
                        if str_val != "all" {
                            losses.push(Loss {
                                path: format!("{edit_path}.keep"),
                                field: "keep".into(),
                                reason: LossReason::UnmappedValue,
                                detail: String::new(),
                            });
                            valid = false;
                        } else {
                            canon_edit.insert("keep".into(), Value::String("all".into()));
                        }
                    } else if let Some(obj_val) = keep.as_object() {
                        for k in obj_val.keys() {
                            if k != "type" && k != "value" {
                                losses.push(Loss {
                                    path: format!("{edit_path}.keep.{k}"),
                                    field: k.clone(),
                                    reason: LossReason::UnmappedField,
                                    detail: String::new(),
                                });
                            }
                        }
                        if obj_val.get("type").and_then(|v| v.as_str()) != Some("thinking_turns") {
                            losses.push(Loss {
                                path: format!("{edit_path}.keep"),
                                field: "keep".into(),
                                reason: LossReason::UnmappedValue,
                                detail: String::new(),
                            });
                            valid = false;
                        } else {
                            let mut keep_obj = serde_json::Map::new();
                            keep_obj.insert("type".into(), Value::String("thinking_turns".into()));
                            if let Some(v) = obj_val.get("value") {
                                keep_obj.insert("value".into(), v.clone());
                            }
                            canon_edit.insert("keep".into(), Value::Object(keep_obj));
                        }
                    } else {
                        losses.push(Loss {
                            path: format!("{edit_path}.keep"),
                            field: "keep".into(),
                            reason: LossReason::UnmappedValue,
                            detail: String::new(),
                        });
                        valid = false;
                    }
                }
                if valid {
                    valid_edits.push(Value::Object(canon_edit));
                }
            }
            _ => {
                losses.push(Loss {
                    path: edit_path,
                    field: "type".into(),
                    reason: LossReason::UnsupportedSemantic,
                    detail: format!("unsupported context edit type \"{edit_type}\""),
                });
            }
        }
    }

    if valid_edits.is_empty() {
        return (None, losses);
    }

    let mut map = serde_json::Map::new();
    map.insert("edits".into(), Value::Array(valid_edits));
    (Some(Value::Object(map).to_string()), losses)
}

pub fn decode_request_native(wire: &Request, req: &mut IrRequest) -> Vec<Loss> {
    let mut losses = Vec::new();

    // 1. Thinking
    if let Some(ref thinking) = wire.thinking {
        match thinking.kind.as_str() {
            "adaptive" | "disabled" => {
                req.native.push(NativeAnnotation {
                    source: NATIVE_SOURCE_ANTHROPIC.into(),
                    kind: NATIVE_KIND_THINKING_MODE.into(),
                    value: serde_json::to_string(&thinking.kind).unwrap_or_default(),
                    omission: Some(NativeOmission {
                        field: "type".into(),
                        reason: LossReason::UnmappedField,
                        detail: None,
                    }),
                    projection: None,
                });
            }
            "enabled" => {
                let has_explicit_effort = wire
                    .output_config
                    .as_ref()
                    .and_then(|oc| oc.effort.as_deref())
                    .is_some();
                req.native.push(NativeAnnotation {
                    source: NATIVE_SOURCE_ANTHROPIC.into(),
                    kind: NATIVE_KIND_THINKING_MODE.into(),
                    value: r#""enabled""#.into(),
                    omission: if has_explicit_effort {
                        Some(NativeOmission {
                            field: "type".into(),
                            reason: LossReason::UnmappedField,
                            detail: None,
                        })
                    } else {
                        None
                    },
                    projection: None,
                });

                if let Some(budget) = thinking.budget_tokens
                    && budget > 0
                {
                    if has_explicit_effort {
                        req.native.push(NativeAnnotation {
                            source: NATIVE_SOURCE_ANTHROPIC.into(),
                            kind: NATIVE_KIND_THINKING_BUDGET.into(),
                            value: budget.to_string(),
                            omission: Some(NativeOmission {
                                field: "budget_tokens".into(),
                                reason: LossReason::UnmappedField,
                                detail: None,
                            }),
                            projection: None,
                        });
                    } else {
                        let mapped = if budget <= 2048 {
                            ReasoningEffort::Low
                        } else if budget <= 8192 {
                            ReasoningEffort::Medium
                        } else {
                            ReasoningEffort::High
                        };
                        if let Some(ref mut params) = req.params {
                            params.reasoning_effort = Some(mapped);
                        }
                        req.native.push(NativeAnnotation {
                            source: NATIVE_SOURCE_ANTHROPIC.into(),
                            kind: NATIVE_KIND_THINKING_BUDGET.into(),
                            value: budget.to_string(),
                            omission: Some(NativeOmission {
                                field: "budget_tokens".into(),
                                reason: LossReason::Degraded,
                                detail: Some("budget approximated".into()),
                            }),
                            projection: Some(NativeProjection {
                                origin: "budget".into(),
                                effort: mapped,
                            }),
                        });
                    }
                }
            }
            other => {
                losses.push(Loss {
                    path: "thinking.type".into(),
                    field: "type".into(),
                    reason: LossReason::UnmappedValue,
                    detail: format!("unknown thinking.type \"{other}\""),
                });
            }
        }

        if let Some(ref display) = thinking.display {
            match display.as_str() {
                "summarized" | "omitted" | "updates" => {
                    req.native.push(NativeAnnotation {
                        source: NATIVE_SOURCE_ANTHROPIC.into(),
                        kind: NATIVE_KIND_THINKING_DISPLAY.into(),
                        value: serde_json::to_string(display).unwrap_or_default(),
                        omission: Some(NativeOmission {
                            field: "display".into(),
                            reason: LossReason::UnmappedField,
                            detail: None,
                        }),
                        projection: None,
                    });
                }
                other => {
                    losses.push(Loss {
                        path: "thinking.display".into(),
                        field: "display".into(),
                        reason: LossReason::UnmappedValue,
                        detail: format!("unknown thinking.display \"{other}\""),
                    });
                }
            }
        }
    }

    // 2. OutputConfig
    if let Some(ref oc) = wire.output_config
        && let Some(ref effort) = oc.effort
    {
        let mapped = match effort.as_str() {
            "low" => Some(ReasoningEffort::Low),
            "medium" => Some(ReasoningEffort::Medium),
            "high" => Some(ReasoningEffort::High),
            "xhigh" => Some(ReasoningEffort::XHigh),
            "max" => Some(ReasoningEffort::Max),
            _ => None,
        };
        if let Some(me) = mapped {
            if let Some(ref mut params) = req.params {
                params.reasoning_effort = Some(me);
            }
            req.native.push(NativeAnnotation {
                source: NATIVE_SOURCE_ANTHROPIC.into(),
                kind: NATIVE_KIND_OUTPUT_EFFORT.into(),
                value: serde_json::to_string(effort).unwrap_or_default(),
                omission: None,
                projection: Some(NativeProjection {
                    origin: "explicit".into(),
                    effort: me,
                }),
            });
        } else {
            losses.push(Loss {
                path: "output_config.effort".into(),
                field: "effort".into(),
                reason: LossReason::UnmappedValue,
                detail: format!("unknown output_config.effort \"{effort}\""),
            });
        }
    }

    // 3. Top-level cache_control
    if let Some(ref cc) = wire.cache_control {
        let (canon, l) = parse_cache_control(cc, "cache_control");
        if let Some(loss) = l {
            losses.push(loss);
        }
        if let Some(val) = canon {
            req.native.push(NativeAnnotation {
                source: NATIVE_SOURCE_ANTHROPIC.into(),
                kind: NATIVE_KIND_CACHE_CONTROL.into(),
                value: val,
                omission: Some(NativeOmission {
                    field: "cache_control".into(),
                    reason: LossReason::UnmappedField,
                    detail: None,
                }),
                projection: None,
            });
        }
    }

    // 4. ContextManagement
    if let Some(ref cm) = wire.context_management {
        let (canon, cm_losses) = parse_context_management(cm, "context_management");
        losses.extend(cm_losses);
        if let Some(val) = canon {
            req.native.push(NativeAnnotation {
                source: NATIVE_SOURCE_ANTHROPIC.into(),
                kind: NATIVE_KIND_CONTEXT_MANAGEMENT.into(),
                value: val,
                omission: Some(NativeOmission {
                    field: "context_management".into(),
                    reason: LossReason::UnsupportedSemantic,
                    detail: None,
                }),
                projection: None,
            });
        }
    }

    losses
}

pub fn get_block_cache_control(native: &[NativeAnnotation]) -> Option<Value> {
    for n in native {
        if n.source == NATIVE_SOURCE_ANTHROPIC && n.kind == NATIVE_KIND_CACHE_CONTROL {
            return serde_json::from_str(&n.value).ok();
        }
    }
    None
}

pub fn encode_request_native(req: &IrRequest, wire: &mut Request) -> Vec<Loss> {
    let mut losses = Vec::new();

    let mut native_mode: Option<String> = None;
    let mut native_budget: Option<i64> = None;
    let mut native_budget_proj: Option<&NativeProjection> = None;
    let mut native_display: Option<String> = None;
    let mut native_effort: Option<String> = None;
    let mut native_effort_proj: Option<&NativeProjection> = None;

    for (i, n) in req.native.iter().enumerate() {
        if n.source != NATIVE_SOURCE_ANTHROPIC {
            losses.push(Loss {
                path: format!("native[{i}]"),
                field: "source".into(),
                reason: LossReason::UnsupportedSemantic,
                detail: format!("unsupported native option source \"{}\"", n.source),
            });
            continue;
        }
        match n.kind.as_str() {
            NATIVE_KIND_THINKING_MODE => {
                native_mode = serde_json::from_str(&n.value).ok();
            }
            NATIVE_KIND_THINKING_BUDGET => {
                native_budget = serde_json::from_str(&n.value).ok();
                native_budget_proj = n.projection.as_ref();
            }
            NATIVE_KIND_THINKING_DISPLAY => {
                native_display = serde_json::from_str(&n.value).ok();
            }
            NATIVE_KIND_OUTPUT_EFFORT => {
                native_effort = serde_json::from_str(&n.value).ok();
                native_effort_proj = n.projection.as_ref();
            }
            NATIVE_KIND_CACHE_CONTROL => {
                wire.cache_control = serde_json::from_str(&n.value).ok();
            }
            NATIVE_KIND_CONTEXT_MANAGEMENT => {
                wire.context_management = serde_json::from_str(&n.value).ok();
            }
            _ => {
                losses.push(Loss {
                    path: format!("native[{i}]"),
                    field: "kind".into(),
                    reason: LossReason::UnsupportedSemantic,
                    detail: format!("unsupported native option kind \"{}\"", n.kind),
                });
            }
        }
    }

    let current_effort = req.params.as_ref().and_then(|p| p.reasoning_effort);

    if native_mode.is_some() || native_effort.is_some() || current_effort.is_some() {
        if native_mode.as_deref() == Some("disabled") {
            wire.thinking = Some(ThinkingWire {
                kind: "disabled".into(),
                budget_tokens: None,
                display: None,
            });
            if (native_effort.is_some() || native_effort_proj.is_some())
                && let Some(eff) = current_effort
            {
                wire.output_config = Some(OutputConfigWire {
                    effort: Some(eff.as_str().into()),
                    format: None,
                });
            }
        } else if native_mode.as_deref() == Some("adaptive") {
            wire.thinking = Some(ThinkingWire {
                kind: "adaptive".into(),
                budget_tokens: None,
                display: native_display,
            });
            if let Some(eff) = current_effort {
                wire.output_config = Some(OutputConfigWire {
                    effort: Some(eff.as_str().into()),
                    format: None,
                });
            }
        } else if native_mode.as_deref() == Some("enabled") {
            if let Some(budget) = native_budget {
                if let Some(proj) = native_budget_proj {
                    if proj.origin == "budget" {
                        if Some(proj.effort) == current_effort {
                            wire.thinking = Some(ThinkingWire {
                                kind: "enabled".into(),
                                budget_tokens: Some(budget),
                                display: native_display.clone(),
                            });
                        } else if let Some(eff) = current_effort {
                            match eff {
                                ReasoningEffort::Minimal => {
                                    wire.thinking = Some(ThinkingWire {
                                        kind: "enabled".into(),
                                        budget_tokens: Some(1024),
                                        display: native_display.clone(),
                                    });
                                    losses.push(Loss {
                                        path: "params.reasoning_effort".into(),
                                        field: "reasoning_effort".into(),
                                        reason: LossReason::Degraded,
                                        detail: "budget approximated".into(),
                                    });
                                }
                                ReasoningEffort::Low => {
                                    wire.thinking = Some(ThinkingWire {
                                        kind: "enabled".into(),
                                        budget_tokens: Some(2048),
                                        display: native_display.clone(),
                                    });
                                    losses.push(Loss {
                                        path: "params.reasoning_effort".into(),
                                        field: "reasoning_effort".into(),
                                        reason: LossReason::Degraded,
                                        detail: "budget approximated".into(),
                                    });
                                }
                                ReasoningEffort::Medium => {
                                    wire.thinking = Some(ThinkingWire {
                                        kind: "enabled".into(),
                                        budget_tokens: Some(8192),
                                        display: native_display.clone(),
                                    });
                                    losses.push(Loss {
                                        path: "params.reasoning_effort".into(),
                                        field: "reasoning_effort".into(),
                                        reason: LossReason::Degraded,
                                        detail: "budget approximated".into(),
                                    });
                                }
                                ReasoningEffort::High => {
                                    wire.thinking = Some(ThinkingWire {
                                        kind: "enabled".into(),
                                        budget_tokens: Some(16384),
                                        display: native_display.clone(),
                                    });
                                    losses.push(Loss {
                                        path: "params.reasoning_effort".into(),
                                        field: "reasoning_effort".into(),
                                        reason: LossReason::Degraded,
                                        detail: "budget approximated".into(),
                                    });
                                }
                                ReasoningEffort::XHigh | ReasoningEffort::Max => {
                                    wire.thinking = Some(ThinkingWire {
                                        kind: "adaptive".into(),
                                        budget_tokens: None,
                                        display: native_display.clone(),
                                    });
                                    wire.output_config = Some(OutputConfigWire {
                                        effort: Some(eff.as_str().into()),
                                        format: None,
                                    });
                                    losses.push(Loss {
                                        path: "params.reasoning_effort".into(),
                                        field: "reasoning_effort".into(),
                                        reason: LossReason::Degraded,
                                        detail:
                                            "switched to adaptive thinking mode for extended effort"
                                                .into(),
                                    });
                                }
                            }
                        } else {
                            losses.push(Loss {
                                path: "thinking".into(),
                                field: "thinking".into(),
                                reason: LossReason::UnsupportedSemantic,
                                detail: "budget projection invalidated when effort is cleared"
                                    .into(),
                            });
                        }
                    }
                } else {
                    wire.thinking = Some(ThinkingWire {
                        kind: "enabled".into(),
                        budget_tokens: Some(budget),
                        display: native_display.clone(),
                    });
                    if let Some(eff) = current_effort {
                        wire.output_config = Some(OutputConfigWire {
                            effort: Some(eff.as_str().into()),
                            format: None,
                        });
                    }
                }
            }
        } else if native_effort.is_some() || native_effort_proj.is_some() {
            if let Some(eff) = current_effort {
                wire.output_config = Some(OutputConfigWire {
                    effort: Some(eff.as_str().into()),
                    format: None,
                });
            }
        } else if let Some(eff) = current_effort {
            match eff {
                ReasoningEffort::Minimal => {
                    wire.thinking = Some(ThinkingWire {
                        kind: "enabled".into(),
                        budget_tokens: Some(1024),
                        display: None,
                    });
                    losses.push(Loss {
                        path: "params.reasoning_effort".into(),
                        field: "reasoning_effort".into(),
                        reason: LossReason::Degraded,
                        detail: "budget approximated".into(),
                    });
                }
                ReasoningEffort::Low => {
                    wire.thinking = Some(ThinkingWire {
                        kind: "enabled".into(),
                        budget_tokens: Some(2048),
                        display: None,
                    });
                    losses.push(Loss {
                        path: "params.reasoning_effort".into(),
                        field: "reasoning_effort".into(),
                        reason: LossReason::Degraded,
                        detail: "budget approximated".into(),
                    });
                }
                ReasoningEffort::Medium => {
                    wire.thinking = Some(ThinkingWire {
                        kind: "enabled".into(),
                        budget_tokens: Some(8192),
                        display: None,
                    });
                    losses.push(Loss {
                        path: "params.reasoning_effort".into(),
                        field: "reasoning_effort".into(),
                        reason: LossReason::Degraded,
                        detail: "budget approximated".into(),
                    });
                }
                ReasoningEffort::High => {
                    wire.thinking = Some(ThinkingWire {
                        kind: "enabled".into(),
                        budget_tokens: Some(16384),
                        display: None,
                    });
                    losses.push(Loss {
                        path: "params.reasoning_effort".into(),
                        field: "reasoning_effort".into(),
                        reason: LossReason::Degraded,
                        detail: "budget approximated".into(),
                    });
                }
                ReasoningEffort::XHigh | ReasoningEffort::Max => {
                    wire.thinking = Some(ThinkingWire {
                        kind: "adaptive".into(),
                        budget_tokens: None,
                        display: None,
                    });
                    wire.output_config = Some(OutputConfigWire {
                        effort: Some(eff.as_str().into()),
                        format: None,
                    });
                }
            }
        }
    }

    losses
}
