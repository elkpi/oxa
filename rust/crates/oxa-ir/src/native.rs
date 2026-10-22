use std::collections::HashSet;

use crate::codec::Error;
use crate::loss::{Loss, LossReason};
use crate::request::{Block, NativeAnnotation, Request, SystemBlock};

pub const NATIVE_KIND_THINKING_MODE: &str = "thinking_mode";
pub const NATIVE_KIND_THINKING_BUDGET: &str = "thinking_budget";
pub const NATIVE_KIND_THINKING_DISPLAY: &str = "thinking_display";
pub const NATIVE_KIND_OUTPUT_EFFORT: &str = "output_effort";
pub const NATIVE_KIND_CACHE_CONTROL: &str = "cache_control";
pub const NATIVE_KIND_CONTEXT_MANAGEMENT: &str = "context_management";
pub const NATIVE_KIND_DEFER_LOADING: &str = "defer_loading";
pub const NATIVE_SOURCE_ANTHROPIC: &str = "anthropic";

pub const CACHE_CONTROL_LOSS_DETAIL: &str =
    "anthropic-native cache breakpoint; the target face has no cache_control representation";

pub const DEFER_LOADING_LOSS_DETAIL: &str = "deferred tool loading is a provider-side context-assembly instruction; the tool definition is unchanged and loads up front when the target has no tool search";

#[derive(Clone, Copy, PartialEq, Eq)]
enum HostType {
    Request,
    System,
    Content,
    Tool,
}

pub fn validate_native_request(request: &Request) -> Result<(), Error> {
    validate_annotations(&request.native, HostType::Request, "request")?;

    let mut budget_proj = 0;
    let mut explicit_proj = 0;
    for n in &request.native {
        if let Some(ref proj) = n.projection {
            if proj.origin != "budget" && proj.origin != "explicit" {
                return Err(Error::Custom(format!(
                    "ir: native projection origin \"{}\" invalid",
                    proj.origin
                )));
            }
            if proj.origin == "budget" {
                if n.kind != NATIVE_KIND_THINKING_BUDGET {
                    return Err(Error::Custom(
                        "ir: budget projection is only allowed on thinking_budget".into(),
                    ));
                }
                budget_proj += 1;
                if budget_proj > 1 {
                    return Err(Error::Custom(
                        "ir: duplicate budget projection on request".into(),
                    ));
                }
            } else if proj.origin == "explicit" {
                if n.kind != NATIVE_KIND_OUTPUT_EFFORT {
                    return Err(Error::Custom(
                        "ir: explicit projection is only allowed on output_effort".into(),
                    ));
                }
                explicit_proj += 1;
                if explicit_proj > 1 {
                    return Err(Error::Custom(
                        "ir: duplicate explicit projection on request".into(),
                    ));
                }
            }
        }
    }

    if budget_proj > 0 && explicit_proj > 0 {
        return Err(Error::Custom(
            "ir: conflicting effort projections on request".into(),
        ));
    }

    for (i, sb) in request.system.iter().enumerate() {
        let SystemBlock::Text { native, .. } = sb;
        validate_annotations(native, HostType::System, &format!("system[{i}]"))?;
    }

    for (m_idx, msg) in request.messages.iter().enumerate() {
        for (c_idx, blk) in msg.content.iter().enumerate() {
            validate_block_native(
                blk,
                HostType::Content,
                &format!("messages[{m_idx}].content[{c_idx}]"),
            )?;
        }
    }

    if let Some(ref tools) = request.tools {
        for (t_idx, tool) in tools.iter().enumerate() {
            validate_annotations(&tool.native, HostType::Tool, &format!("tools[{t_idx}]"))?;
            let mut has_cache_control = false;
            let mut has_defer_loading = false;
            for n in &tool.native {
                if n.source == NATIVE_SOURCE_ANTHROPIC {
                    if n.kind == NATIVE_KIND_CACHE_CONTROL {
                        has_cache_control = true;
                    } else if n.kind == NATIVE_KIND_DEFER_LOADING {
                        has_defer_loading = true;
                    }
                }
            }
            if has_cache_control && has_defer_loading {
                return Err(Error::Custom(format!(
                    "ir: tools[{t_idx}]: cannot specify both defer_loading and cache_control"
                )));
            }
        }
    }

    Ok(())
}

fn validate_annotations(
    annotations: &[NativeAnnotation],
    host: HostType,
    path: &str,
) -> Result<(), Error> {
    let mut seen = HashSet::new();
    for (i, ann) in annotations.iter().enumerate() {
        if ann.source.is_empty() {
            return Err(Error::Custom(format!(
                "{path}.native[{i}]: source cannot be empty"
            )));
        }
        if ann.kind.is_empty() {
            return Err(Error::Custom(format!(
                "{path}.native[{i}]: kind cannot be empty"
            )));
        }
        let parsed_json: serde_json::Value = serde_json::from_str(&ann.value).map_err(|_| {
            Error::Custom(format!("{path}.native[{i}]: value is not valid JSON text"))
        })?;
        if let Some(ref om) = ann.omission
            && om.field.is_empty()
        {
            return Err(Error::Custom(format!(
                "{path}.native[{i}]: omission field cannot be empty"
            )));
        }
        let key = format!("{}:{}", ann.source, ann.kind);
        if !seen.insert(key) {
            return Err(Error::Custom(format!(
                "{path}: duplicate native annotation for {}:{}",
                ann.source, ann.kind
            )));
        }

        if ann.source == NATIVE_SOURCE_ANTHROPIC {
            match ann.kind.as_str() {
                NATIVE_KIND_THINKING_MODE => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let s = parsed_json.as_str().unwrap_or_default();
                    if s != "adaptive" && s != "enabled" && s != "disabled" {
                        return Err(Error::Custom(format!("invalid thinking_mode {s:?}")));
                    }
                }
                NATIVE_KIND_THINKING_BUDGET => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let n = parsed_json.as_i64().unwrap_or(0);
                    if n <= 0 {
                        return Err(Error::Custom(
                            "anthropic thinking_budget must be a positive integer".into(),
                        ));
                    }
                }
                NATIVE_KIND_THINKING_DISPLAY => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let s = parsed_json.as_str().unwrap_or_default();
                    if s != "summarized" && s != "omitted" && s != "updates" {
                        return Err(Error::Custom(format!("invalid thinking_display {s:?}")));
                    }
                }
                NATIVE_KIND_OUTPUT_EFFORT => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let s = parsed_json.as_str().unwrap_or_default();
                    if s != "low" && s != "medium" && s != "high" && s != "xhigh" && s != "max" {
                        return Err(Error::Custom(format!("invalid output_effort {s:?}")));
                    }
                }
                NATIVE_KIND_CONTEXT_MANAGEMENT => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let Some(obj) = parsed_json.as_object() else {
                        return Err(Error::Custom(
                            "context_management requires edits array".into(),
                        ));
                    };
                    if !obj.get("edits").is_some_and(|e| e.is_array()) {
                        return Err(Error::Custom(
                            "context_management requires edits array".into(),
                        ));
                    }
                }
                NATIVE_KIND_CACHE_CONTROL => {
                    let Some(obj) = parsed_json.as_object() else {
                        return Err(Error::Custom("invalid cache_control object".into()));
                    };
                    if obj.get("type").and_then(|t| t.as_str()) != Some("ephemeral") {
                        return Err(Error::Custom("invalid cache_control object".into()));
                    }
                    if let Some(ttl) = obj.get("ttl") {
                        let ttl_str = ttl.as_str().unwrap_or_default();
                        if ttl_str != "5m" && ttl_str != "1h" {
                            return Err(Error::Custom(format!(
                                "invalid cache_control ttl {ttl_str:?}"
                            )));
                        }
                    }
                }
                NATIVE_KIND_DEFER_LOADING => {
                    if host != HostType::Tool {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Tool",
                            ann.kind
                        )));
                    }
                    if parsed_json.as_bool() != Some(true) {
                        return Err(Error::Custom(format!(
                            "invalid defer_loading {parsed_json:?}"
                        )));
                    }
                }
                _ => {}
            }
        }
    }
    Ok(())
}

fn validate_block_native(blk: &Block, host: HostType, path: &str) -> Result<(), Error> {
    match blk {
        Block::Text { native, .. }
        | Block::Thinking { native, .. }
        | Block::Image { native, .. }
        | Block::ToolUse { native, .. }
        | Block::CustomToolUse { native, .. }
        | Block::ApplyPatchToolUse { native, .. }
        | Block::ApplyPatchToolResult { native, .. } => {
            validate_annotations(native, host, path)?;
        }
        Block::ToolResult {
            content, native, ..
        } => {
            validate_annotations(native, host, path)?;
            for (k, child) in content.iter().enumerate() {
                validate_block_native(child, host, &format!("{path}.content[{k}]"))?;
            }
        }
        Block::CustomToolResult { output, native, .. } => {
            validate_annotations(native, host, path)?;
            if let crate::request::CustomToolOutput::Content(children) = output {
                for (k, child) in children.iter().enumerate() {
                    validate_block_native(child, host, &format!("{path}.output.content[{k}]"))?;
                }
            }
        }
    }
    Ok(())
}

pub fn omitted_native_request_losses(request: &Request, _target: &str) -> Result<Vec<Loss>, Error> {
    validate_native_request(request)?;

    let mut losses = Vec::new();
    let current_effort = request.params.as_ref().and_then(|p| p.reasoning_effort);

    let has_active_budget_proj = request.native.iter().any(|n| {
        n.source == NATIVE_SOURCE_ANTHROPIC
            && n.kind == NATIVE_KIND_THINKING_BUDGET
            && n.projection
                .as_ref()
                .map(|p| p.origin == "budget" && Some(p.effort) == current_effort)
                .unwrap_or(false)
    });

    for (i, ann) in request.native.iter().enumerate() {
        append_omission_loss(
            &mut losses,
            ann,
            &format!("native[{i}]"),
            has_active_budget_proj,
            current_effort,
        );
    }

    for (s_idx, sb) in request.system.iter().enumerate() {
        let SystemBlock::Text { native, .. } = sb;
        for (i, ann) in native.iter().enumerate() {
            append_omission_loss(
                &mut losses,
                ann,
                &format!("system[{s_idx}].native[{i}]"),
                false,
                None,
            );
        }
    }

    for (m_idx, msg) in request.messages.iter().enumerate() {
        for (c_idx, blk) in msg.content.iter().enumerate() {
            collect_block_omissions(
                &mut losses,
                blk,
                &format!("messages[{m_idx}].content[{c_idx}]"),
            );
        }
    }

    if let Some(ref tools) = request.tools {
        for (t_idx, tool) in tools.iter().enumerate() {
            for (i, ann) in tool.native.iter().enumerate() {
                append_omission_loss(
                    &mut losses,
                    ann,
                    &format!("tools[{t_idx}].native[{i}]"),
                    false,
                    None,
                );
            }
        }
    }

    Ok(losses)
}

fn collect_block_omissions(losses: &mut Vec<Loss>, blk: &Block, path: &str) {
    let (native, children) = match blk {
        Block::Text { native, .. }
        | Block::Thinking { native, .. }
        | Block::Image { native, .. }
        | Block::ToolUse { native, .. }
        | Block::CustomToolUse { native, .. }
        | Block::ApplyPatchToolUse { native, .. }
        | Block::ApplyPatchToolResult { native, .. } => (native, None),
        Block::ToolResult {
            native, content, ..
        } => (native, Some(content.as_slice())),
        Block::CustomToolResult { native, output, .. } => {
            let ch = match output {
                crate::request::CustomToolOutput::Content(c) => Some(c.as_slice()),
                _ => None,
            };
            (native, ch)
        }
    };

    for (i, ann) in native.iter().enumerate() {
        append_omission_loss(losses, ann, &format!("{path}.native[{i}]"), false, None);
    }

    if let Some(ch) = children {
        for (k, child) in ch.iter().enumerate() {
            collect_block_omissions(losses, child, &format!("{path}.content[{k}]"));
        }
    }
}

fn append_omission_loss(
    losses: &mut Vec<Loss>,
    ann: &NativeAnnotation,
    path: &str,
    has_active_budget_proj: bool,
    current_effort: Option<crate::request::ReasoningEffort>,
) {
    if ann.source != NATIVE_SOURCE_ANTHROPIC {
        losses.push(Loss {
            path: path.into(),
            field: "source".into(),
            reason: LossReason::UnsupportedSemantic,
            detail: format!("unsupported native option source \"{}\"", ann.source),
        });
        return;
    }

    match ann.kind.as_str() {
        NATIVE_KIND_OUTPUT_EFFORT => {}
        NATIVE_KIND_THINKING_MODE => {
            if has_active_budget_proj {
                return;
            }
            if let Some(ref om) = ann.omission {
                losses.push(Loss {
                    path: path.into(),
                    field: om.field.clone(),
                    reason: om.reason,
                    detail: om.detail.clone().unwrap_or_default(),
                });
            } else {
                losses.push(Loss {
                    path: path.into(),
                    field: "type".into(),
                    reason: LossReason::UnmappedField,
                    detail: String::new(),
                });
            }
        }
        NATIVE_KIND_THINKING_BUDGET => {
            if let Some(ref proj) = ann.projection
                && proj.origin == "budget"
            {
                if Some(proj.effort) == current_effort
                    && let Some(ref om) = ann.omission
                {
                    losses.push(Loss {
                        path: path.into(),
                        field: om.field.clone(),
                        reason: om.reason,
                        detail: om.detail.clone().unwrap_or_default(),
                    });
                }
                return;
            }
            if let Some(ref om) = ann.omission {
                losses.push(Loss {
                    path: path.into(),
                    field: om.field.clone(),
                    reason: om.reason,
                    detail: om.detail.clone().unwrap_or_default(),
                });
            }
        }
        NATIVE_KIND_CACHE_CONTROL => {
            let (field, reason, detail) = if let Some(ref om) = ann.omission {
                let f = if om.field.is_empty() {
                    "cache_control".to_string()
                } else {
                    om.field.clone()
                };
                let r = om.reason;
                let d = match om.detail.as_deref() {
                    Some(s) if !s.is_empty() => s.to_string(),
                    _ => CACHE_CONTROL_LOSS_DETAIL.to_string(),
                };
                (f, r, d)
            } else {
                (
                    "cache_control".to_string(),
                    LossReason::UnmappedField,
                    CACHE_CONTROL_LOSS_DETAIL.to_string(),
                )
            };
            losses.push(Loss {
                path: path.into(),
                field,
                reason,
                detail,
            });
        }
        NATIVE_KIND_DEFER_LOADING => {
            let (field, reason, detail) = if let Some(ref om) = ann.omission {
                let f = if om.field.is_empty() {
                    "defer_loading".to_string()
                } else {
                    om.field.clone()
                };
                let r = om.reason;
                let d = match om.detail.as_deref() {
                    Some(s) if !s.is_empty() => s.to_string(),
                    _ => DEFER_LOADING_LOSS_DETAIL.to_string(),
                };
                (f, r, d)
            } else {
                (
                    "defer_loading".to_string(),
                    LossReason::Degraded,
                    DEFER_LOADING_LOSS_DETAIL.to_string(),
                )
            };
            losses.push(Loss {
                path: path.into(),
                field,
                reason,
                detail,
            });
        }
        NATIVE_KIND_THINKING_DISPLAY | NATIVE_KIND_CONTEXT_MANAGEMENT => {
            if let Some(ref om) = ann.omission {
                losses.push(Loss {
                    path: path.into(),
                    field: om.field.clone(),
                    reason: om.reason,
                    detail: om.detail.clone().unwrap_or_default(),
                });
            }
        }
        _ => {
            losses.push(Loss {
                path: path.into(),
                field: "kind".into(),
                reason: LossReason::UnsupportedSemantic,
                detail: format!("unsupported native option kind \"{}\"", ann.kind),
            });
        }
    }
}
