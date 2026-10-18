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
pub const NATIVE_SOURCE_ANTHROPIC: &str = "anthropic";

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
            if proj.origin == "budget" {
                budget_proj += 1;
                if budget_proj > 1 {
                    return Err(Error::Custom(
                        "ir: duplicate budget projection on request".into(),
                    ));
                }
            } else if proj.origin == "explicit" {
                explicit_proj += 1;
                if explicit_proj > 1 {
                    return Err(Error::Custom(
                        "ir: duplicate explicit projection on request".into(),
                    ));
                }
            }
        }
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
        if serde_json::from_str::<serde_json::Value>(&ann.value).is_err() {
            return Err(Error::Custom(format!(
                "{path}.native[{i}]: value is not valid JSON text"
            )));
        }
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
                NATIVE_KIND_THINKING_MODE
                | NATIVE_KIND_THINKING_BUDGET
                | NATIVE_KIND_THINKING_DISPLAY
                | NATIVE_KIND_OUTPUT_EFFORT
                | NATIVE_KIND_CONTEXT_MANAGEMENT => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                }
                NATIVE_KIND_CACHE_CONTROL => {}
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
        NATIVE_KIND_THINKING_DISPLAY
        | NATIVE_KIND_CACHE_CONTROL
        | NATIVE_KIND_CONTEXT_MANAGEMENT => {
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
