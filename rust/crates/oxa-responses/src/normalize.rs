//! Normalization rules N-R-1 through N-R-10 (spec/11 §5).

use serde_json::{Value, json};
use std::collections::{BTreeMap, BTreeSet};

use oxa_ir::{
    ApplyPatchCallStatus, ApplyPatchOperation, ApplyPatchResultStatus, Block, CustomToolOutput,
    Loss, LossReason, Message as IrMessage, Role, SystemBlock, ToolChoice, ToolChoiceMode,
    ToolType,
};

use crate::error::Error;
use crate::types::{
    ContentPart, ContentValue, ITEM_TYPE_CUSTOM_TOOL_CALL, ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT,
    ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_FUNCTION_CALL_OUTPUT, ITEM_TYPE_MESSAGE,
    ITEM_TYPE_REASONING, InputItem, OutputPart, OutputValue, PART_TYPE_INPUT_IMAGE,
    PART_TYPE_INPUT_TEXT, PART_TYPE_OUTPUT_TEXT, ROLE_ASSISTANT, ROLE_USER, TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE, TOOL_CHOICE_REQUIRED, TOOL_TYPE_CUSTOM, TOOL_TYPE_FUNCTION,
};

pub(crate) fn loss(
    path: impl Into<String>,
    field: impl Into<String>,
    reason: LossReason,
    detail: impl Into<String>,
) -> Loss {
    Loss {
        path: path.into(),
        field: field.into(),
        reason,
        detail: detail.into(),
    }
}

/// N-R-3 normalizes Responses string and parts-array content into IR blocks.
pub(crate) fn decode_content(
    content: Option<&ContentValue>,
    path: &str,
) -> (Vec<Block>, Vec<Loss>) {
    let Some(content) = content else {
        return (
            vec![Block::Text {
                text: String::new(),
                native: Vec::new(),
            }],
            Vec::new(),
        );
    };
    match content {
        ContentValue::Text(text) => (
            vec![Block::Text {
                text: text.clone(),
                native: Vec::new(),
            }],
            Vec::new(),
        ),
        ContentValue::Parts(parts) => {
            let mut blocks = Vec::with_capacity(parts.len());
            let mut losses = Vec::new();
            for (index, part) in parts.iter().enumerate() {
                let (decoded, part_losses) = decode_content_part(part, path, index);
                blocks.extend(decoded);
                losses.extend(part_losses);
            }
            (blocks, losses)
        }
    }
}

fn decode_content_part(part: &ContentPart, path: &str, index: usize) -> (Vec<Block>, Vec<Loss>) {
    match part.kind.as_str() {
        PART_TYPE_INPUT_TEXT => (
            vec![Block::Text {
                text: part.text.clone(),
                native: Vec::new(),
            }],
            Vec::new(),
        ),
        PART_TYPE_INPUT_IMAGE => {
            match decode_image_url(&part.image_url, &format!("{path}[{index}].image_url")) {
                Ok(block) => (vec![block], Vec::new()),
                Err(image_loss) => (Vec::new(), vec![image_loss]),
            }
        }
        other => (
            Vec::new(),
            vec![loss(
                format!("{path}[{index}]"),
                "type",
                LossReason::UnsupportedSemantic,
                format!("Responses input content part type {other:?} has no IR equivalent"),
            )],
        ),
    }
}

pub(crate) fn decode_image_url(raw: &str, path: &str) -> Result<Block, Loss> {
    if raw.starts_with("https:") {
        if is_valid_https_url(raw) {
            return Ok(Block::Image {
                media_type: None,
                data: None,
                url: Some(raw.to_string()),
                native: Vec::new(),
            });
        }
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "malformed https image URL has no IR equivalent",
        ));
    }
    let Some(data_part) = raw.strip_prefix("data:") else {
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "only https and base64 data image URLs are supported",
        ));
    };
    let Some((metadata, data)) = data_part.split_once(',') else {
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "malformed data image URL has no IR equivalent",
        ));
    };
    let Some(media_type) = metadata.strip_suffix(";base64") else {
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "malformed data image URL has no IR equivalent",
        ));
    };
    if !media_type.to_lowercase().starts_with("image/") || media_type == "image/" {
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "non-image data URL has no IR equivalent",
        ));
    }
    Ok(Block::Image {
        media_type: Some(media_type.to_string()),
        data: Some(data.to_string()),
        url: None,
        native: Vec::new(),
    })
}

fn is_valid_https_url(raw: &str) -> bool {
    let Some(rest) = raw.strip_prefix("https://") else {
        return false;
    };
    if rest.chars().any(|c| c.is_whitespace() || c.is_control()) {
        return false;
    }
    let host = rest.split(['/', '?', '#']).next().unwrap_or_default();
    !host.is_empty()
}

pub(crate) fn decode_apply_patch_call_status(
    status: &str,
    path: &str,
    losses: &mut Vec<Loss>,
) -> Result<Option<ApplyPatchCallStatus>, Error> {
    if status.is_empty() {
        return Err(Error::new(format!("responses: {path}.status is required")));
    }
    match status {
        "in_progress" => Ok(Some(ApplyPatchCallStatus::InProgress)),
        "completed" => Ok(Some(ApplyPatchCallStatus::Completed)),
        other => {
            losses.push(loss(
                format!("{path}.status"),
                "status",
                LossReason::UnmappedValue,
                format!("unknown apply_patch_call status {other:?}"),
            ));
            Ok(None)
        }
    }
}

pub(crate) fn decode_apply_patch_result_status(
    status: &str,
    path: &str,
    losses: &mut Vec<Loss>,
) -> Result<Option<ApplyPatchResultStatus>, Error> {
    if status.is_empty() {
        return Err(Error::new(format!("responses: {path}.status is required")));
    }
    match status {
        "completed" => Ok(Some(ApplyPatchResultStatus::Completed)),
        "failed" => Ok(Some(ApplyPatchResultStatus::Failed)),
        other => {
            losses.push(loss(
                format!("{path}.status"),
                "status",
                LossReason::UnmappedValue,
                format!("unknown apply_patch_call_output status {other:?}"),
            ));
            Ok(None)
        }
    }
}

pub(crate) fn decode_apply_patch_operation(
    op: Option<&crate::types::ApplyPatchOperationWire>,
    path: &str,
    losses: &mut Vec<Loss>,
) -> Result<Option<ApplyPatchOperation>, Error> {
    let Some(op) = op else {
        return Err(Error::new(format!(
            "responses: {path}.operation is required"
        )));
    };
    if op.kind.is_empty() {
        return Err(Error::new(format!(
            "responses: {path}.operation.type is required"
        )));
    }
    match op.kind.as_str() {
        "create_file" => {
            let path_str = op.path.as_ref().and_then(|v| v.as_str()).ok_or_else(|| {
                Error::new(format!("responses: {path}.operation.path must be a string"))
            })?;
            let diff_str = op.diff.as_ref().and_then(|v| v.as_str()).ok_or_else(|| {
                Error::new(format!(
                    "responses: {path}.diff is required for create_file"
                ))
            })?;
            Ok(Some(ApplyPatchOperation::CreateFile {
                path: path_str.to_string(),
                diff: diff_str.to_string(),
            }))
        }
        "update_file" => {
            let path_str = op.path.as_ref().and_then(|v| v.as_str()).ok_or_else(|| {
                Error::new(format!("responses: {path}.operation.path must be a string"))
            })?;
            let diff_str = op.diff.as_ref().and_then(|v| v.as_str()).ok_or_else(|| {
                Error::new(format!(
                    "responses: {path}.diff is required for update_file"
                ))
            })?;
            Ok(Some(ApplyPatchOperation::UpdateFile {
                path: path_str.to_string(),
                diff: diff_str.to_string(),
            }))
        }
        "delete_file" => {
            let path_str = op.path.as_ref().and_then(|v| v.as_str()).ok_or_else(|| {
                Error::new(format!("responses: {path}.operation.path must be a string"))
            })?;
            Ok(Some(ApplyPatchOperation::DeleteFile {
                path: path_str.to_string(),
            }))
        }
        other => {
            losses.push(loss(
                path,
                "operation",
                LossReason::UnsupportedSemantic,
                format!("unsupported operation type {other:?}"),
            ));
            Ok(None)
        }
    }
}

pub(crate) fn encode_apply_patch_operation(
    op: &ApplyPatchOperation,
) -> crate::types::ApplyPatchOperationWire {
    match op {
        ApplyPatchOperation::CreateFile { path, diff } => crate::types::ApplyPatchOperationWire {
            kind: "create_file".to_string(),
            path: Some(serde_json::Value::String(path.clone())),
            diff: Some(serde_json::Value::String(diff.clone())),
        },
        ApplyPatchOperation::UpdateFile { path, diff } => crate::types::ApplyPatchOperationWire {
            kind: "update_file".to_string(),
            path: Some(serde_json::Value::String(path.clone())),
            diff: Some(serde_json::Value::String(diff.clone())),
        },
        ApplyPatchOperation::DeleteFile { path } => crate::types::ApplyPatchOperationWire {
            kind: "delete_file".to_string(),
            path: Some(serde_json::Value::String(path.clone())),
            diff: None,
        },
    }
}

#[allow(clippy::type_complexity)]
pub(crate) fn precheck_apply_patch_pairs(
    items: &[InputItem],
) -> Result<
    (
        BTreeMap<usize, Block>,
        BTreeMap<usize, Block>,
        BTreeSet<usize>,
        Vec<Loss>,
    ),
    Error,
> {
    let mut decoded_calls = BTreeMap::new();
    let mut decoded_results = BTreeMap::new();
    let mut dropped = BTreeSet::new();
    let mut losses = Vec::new();

    let mut i = 0;
    while i < items.len() {
        let item = &items[i];
        if item.kind == "apply_patch_call_output" {
            return Err(Error::new(format!(
                "responses: input[{i}]: apply_patch tool result without preceding tool calls (INV-3)"
            )));
        }
        let is_assistant = (item.kind.is_empty() || item.kind == ITEM_TYPE_MESSAGE)
            && item.role == ROLE_ASSISTANT
            || item.kind == ITEM_TYPE_FUNCTION_CALL
            || item.kind == ITEM_TYPE_CUSTOM_TOOL_CALL
            || item.kind == "apply_patch_call"
            || item.kind == ITEM_TYPE_REASONING;

        if !is_assistant {
            i += 1;
            continue;
        }

        let mut calls: Vec<(String, String, usize)> = Vec::new();
        let mut seen_call_ids: BTreeSet<String> = BTreeSet::new();
        let mut a_start = i;
        while a_start < items.len() {
            let it = &items[a_start];
            let kind = match it.kind.as_str() {
                ITEM_TYPE_FUNCTION_CALL => "function",
                ITEM_TYPE_CUSTOM_TOOL_CALL => "custom",
                "apply_patch_call" => "apply_patch",
                ITEM_TYPE_REASONING => "",
                "" | ITEM_TYPE_MESSAGE if it.role == ROLE_ASSISTANT => "",
                _ => break,
            };
            if !kind.is_empty() {
                if kind == "apply_patch" && it.call_id.is_empty() {
                    return Err(Error::new(format!(
                        "responses: input[{a_start}]: apply_patch tool call requires nonempty call_id (INV-3)"
                    )));
                }
                if seen_call_ids.contains(&it.call_id) {
                    return Err(Error::new(format!(
                        "responses: input[{a_start}]: duplicate call_id {:?} in turn (INV-3)",
                        it.call_id
                    )));
                }
                seen_call_ids.insert(it.call_id.clone());
                calls.push((it.call_id.clone(), kind.to_string(), a_start));
            }
            a_start += 1;
        }

        let mut results: Vec<(String, String, usize)> = Vec::new();
        let mut o_start = a_start;
        while o_start < items.len() {
            let it = &items[o_start];
            let kind = match it.kind.as_str() {
                ITEM_TYPE_FUNCTION_CALL_OUTPUT => "function",
                ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT => "custom",
                "apply_patch_call_output" => "apply_patch",
                _ => break,
            };
            if kind == "apply_patch" && it.call_id.is_empty() {
                return Err(Error::new(format!(
                    "responses: input[{o_start}]: apply_patch tool result requires nonempty call_id (INV-3)"
                )));
            }
            results.push((it.call_id.clone(), kind.to_string(), o_start));
            o_start += 1;
        }

        let has_apply_patch = calls.iter().any(|c| c.1 == "apply_patch")
            || results.iter().any(|r| r.1 == "apply_patch");

        if has_apply_patch {
            if calls.len() != results.len() {
                return Err(Error::new(format!(
                    "responses: tool calls count ({}) and results count ({}) mismatch (INV-3)",
                    calls.len(),
                    results.len()
                )));
            }
            let matched = calls
                .iter()
                .zip(&results)
                .all(|(c, r)| c.0 == r.0 && c.1 == r.1);
            if !matched {
                return Err(Error::new(
                    "responses: tool result identity/type/order mismatch (INV-3)",
                ));
            }
            for (c, r) in calls.iter().zip(&results) {
                if c.1 != "apply_patch" {
                    continue;
                }
                let c_idx = c.2;
                let o_idx = r.2;
                let c_item = &items[c_idx];
                let o_item = &items[o_idx];

                let mut call_losses = Vec::new();
                let op = decode_apply_patch_operation(
                    c_item.operation.as_ref(),
                    &format!("input[{c_idx}]"),
                    &mut call_losses,
                )?;
                let call_status = decode_apply_patch_call_status(
                    &c_item.status,
                    &format!("input[{c_idx}]"),
                    &mut call_losses,
                )?;

                let mut res_losses = Vec::new();
                let res_status = decode_apply_patch_result_status(
                    &o_item.status,
                    &format!("input[{o_idx}]"),
                    &mut res_losses,
                )?;

                let output_str = match &o_item.output {
                    None => None,
                    Some(OutputValue::Text(s)) => Some(s.clone()),
                    Some(_) => {
                        return Err(Error::new(format!(
                            "responses: input[{o_idx}].output must be a string"
                        )));
                    }
                };

                let has_call_loss = op.is_none() || call_status.is_none();
                let has_res_loss = res_status.is_none();

                if has_call_loss || has_res_loss {
                    dropped.insert(c_idx);
                    dropped.insert(o_idx);
                    if has_call_loss {
                        losses.extend(call_losses);
                    } else {
                        losses.push(loss(
                            format!("input[{c_idx}]"),
                            "type",
                            LossReason::UnsupportedSemantic,
                            "paired apply_patch result has unsupported semantic",
                        ));
                    }
                    if has_res_loss {
                        losses.extend(res_losses);
                    } else {
                        losses.push(loss(
                            format!("input[{o_idx}]"),
                            "type",
                            LossReason::UnsupportedSemantic,
                            "paired apply_patch call has unsupported semantic",
                        ));
                    }
                } else if let (Some(op), Some(cs), Some(rs)) = (op, call_status, res_status) {
                    decoded_calls.insert(
                        c_idx,
                        Block::ApplyPatchToolUse {
                            id: c_item.call_id.clone(),
                            status: cs,
                            operation: op,
                            native: Vec::new(),
                        },
                    );
                    decoded_results.insert(
                        o_idx,
                        Block::ApplyPatchToolResult {
                            tool_use_id: o_item.call_id.clone(),
                            status: rs,
                            output: output_str,
                            native: Vec::new(),
                        },
                    );
                }
            }
        }

        i = o_start;
    }

    Ok((decoded_calls, decoded_results, dropped, losses))
}

/// N-R-5 merges assistant items and function-call items into one assistant
/// message, with ordinary content before tool-use blocks.
pub(crate) fn decode_assistant_run(
    items: &[InputItem],
    start: usize,
    decoded_calls: &BTreeMap<usize, Block>,
    dropped: &BTreeSet<usize>,
) -> Result<(Option<IrMessage>, usize, Vec<Loss>), Error> {
    let mut thinkings = Vec::new();
    let mut text = Vec::new();
    let mut calls = Vec::new();
    let mut losses = Vec::new();
    let mut index = start;
    while index < items.len() {
        let item = &items[index];
        if item.kind == "apply_patch_call" {
            if dropped.contains(&index) {
                index += 1;
                continue;
            }
            if let Some(block) = decoded_calls.get(&index) {
                calls.push(block.clone());
                index += 1;
                continue;
            }
            return Err(Error::new(format!(
                "responses: input[{index}]: unpaired apply_patch_call (INV-3)"
            )));
        }
        if item.kind == ITEM_TYPE_REASONING {
            for (part_index, part) in item.summary.iter().enumerate() {
                if part.kind == PART_TYPE_OUTPUT_TEXT || part.kind == "summary_text" {
                    if !part.text.is_empty() {
                        thinkings.push(Block::Thinking {
                            thinking: part.text.clone(),
                            signature: None,
                            native: Vec::new(),
                        });
                    }
                } else {
                    losses.push(loss(
                        format!("input[{index}].summary[{part_index}]"),
                        "type",
                        LossReason::UnsupportedSemantic,
                        format!(
                            "Responses reasoning summary type {:?} has no IR equivalent",
                            part.kind
                        ),
                    ));
                }
            }
            index += 1;
            continue;
        }
        if item.kind == ITEM_TYPE_FUNCTION_CALL {
            calls.push(Block::ToolUse {
                id: item.call_id.clone(),
                name: item.name.clone(),
                input: item.arguments.clone(),
                native: Vec::new(),
            });
            index += 1;
            continue;
        }
        if item.kind == ITEM_TYPE_CUSTOM_TOOL_CALL {
            let input = item.input.as_ref().ok_or_else(|| {
                Error::new(format!(
                    "responses: input[{index}].input is required for custom_tool_call"
                ))
            })?;
            if item.call_id.is_empty() || item.name.is_empty() {
                return Err(Error::new(format!(
                    "responses: input[{index}] custom_tool_call requires call_id and name"
                )));
            }
            calls.push(Block::CustomToolUse {
                id: item.call_id.clone(),
                name: item.name.clone(),
                input: input.clone(),
                native: Vec::new(),
            });
            index += 1;
            continue;
        }
        if !(item.kind.is_empty() || item.kind == ITEM_TYPE_MESSAGE) || item.role != ROLE_ASSISTANT
        {
            break;
        }
        let (blocks, block_losses) =
            decode_content(item.content.as_ref(), &format!("input[{index}].content"));
        text.extend(blocks);
        losses.extend(block_losses);
        index += 1;
    }
    let mut content = thinkings;
    content.extend(text);
    content.extend(calls);
    if content.is_empty() {
        return Ok((None, index, losses));
    }
    Ok((
        Some(IrMessage {
            role: Role::Assistant,
            content,
        }),
        index,
        losses,
    ))
}

/// N-R-6 merges a maximal function_call_output run into one user message.
pub(crate) fn decode_output_run(
    items: &[InputItem],
    start: usize,
    decoded_results: &BTreeMap<usize, Block>,
    dropped: &BTreeSet<usize>,
) -> Result<(Option<IrMessage>, usize, Vec<Loss>), Error> {
    let mut content = Vec::new();
    let mut losses = Vec::new();
    let mut index = start;
    while index < items.len()
        && (items[index].kind == ITEM_TYPE_FUNCTION_CALL_OUTPUT
            || items[index].kind == ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT
            || items[index].kind == "apply_patch_call_output")
    {
        let item = &items[index];
        if item.kind == "apply_patch_call_output" {
            if dropped.contains(&index) {
                index += 1;
                continue;
            }
            if let Some(block) = decoded_results.get(&index) {
                content.push(block.clone());
                index += 1;
                continue;
            }
            return Err(Error::new(format!(
                "responses: input[{index}]: unpaired apply_patch_call_output (INV-3)"
            )));
        }
        let output = item.output.as_ref().ok_or_else(|| {
            Error::new(format!(
                "responses: input[{index}].output is required for tool output"
            ))
        })?;
        if item.kind == ITEM_TYPE_FUNCTION_CALL_OUTPUT {
            let OutputValue::Text(text) = output else {
                return Err(Error::new(
                    "responses: function_call_output must be a string",
                ));
            };
            content.push(Block::ToolResult {
                tool_use_id: item.call_id.clone(),
                content: vec![Block::Text {
                    text: text.clone(),
                    native: Vec::new(),
                }],
                is_error: None,
                native: Vec::new(),
            });
        } else {
            if item.call_id.is_empty() {
                return Err(Error::new(format!(
                    "responses: input[{index}].call_id is required for custom_tool_call_output"
                )));
            }
            let custom = match output {
                OutputValue::Text(text) => CustomToolOutput::Text(text.clone()),
                OutputValue::Parts(parts) => {
                    let mut blocks = Vec::new();
                    for (j, p) in parts.iter().enumerate() {
                        let path = format!("input[{index}].output[{j}]");
                        match p.kind.as_str() {
                            PART_TYPE_INPUT_TEXT => {
                                if !p.image_url.is_empty() {
                                    losses.push(loss(
                                        format!("{path}.image_url"),
                                        "image_url",
                                        LossReason::UnmappedField,
                                        "input_text has no image_url field",
                                    ));
                                }
                                blocks.push(Block::Text {
                                    text: p.text.clone(),
                                    native: Vec::new(),
                                });
                            }
                            PART_TYPE_INPUT_IMAGE if !p.image_url.is_empty() => {
                                match decode_image_url(&p.image_url, &format!("{path}.image_url")) {
                                    Ok(b) => blocks.push(b),
                                    Err(l) => losses.push(l),
                                }
                                if p.detail.is_some() {
                                    losses.push(loss(
                                        format!("{path}.detail"),
                                        "detail",
                                        LossReason::UnmappedField,
                                        "input_image detail has no IR equivalent",
                                    ));
                                }
                            }
                            _ => losses.push(loss(
                                path,
                                "type",
                                LossReason::UnsupportedSemantic,
                                "custom tool output content is not supported",
                            )),
                        }
                    }
                    CustomToolOutput::Content(blocks)
                }
            };
            content.push(Block::CustomToolResult {
                tool_use_id: item.call_id.clone(),
                output: custom,
                native: Vec::new(),
            });
        }
        index += 1;
    }
    if content.is_empty() {
        return Ok((None, index, losses));
    }
    Ok((
        Some(IrMessage {
            role: Role::User,
            content,
        }),
        index,
        losses,
    ))
}

/// N-R-9 maps Responses tool choice forms to the IR modes.
pub(crate) fn decode_tool_choice(value: Option<&Value>) -> (Option<ToolChoice>, Vec<Loss>) {
    let Some(value) = value else {
        return (None, Vec::new());
    };
    match value {
        Value::String(choice) => match choice.as_str() {
            TOOL_CHOICE_AUTO => (
                Some(ToolChoice {
                    mode: ToolChoiceMode::Auto,
                    name: None,
                    tool_type: None,
                }),
                Vec::new(),
            ),
            TOOL_CHOICE_NONE => (
                Some(ToolChoice {
                    mode: ToolChoiceMode::None,
                    name: None,
                    tool_type: None,
                }),
                Vec::new(),
            ),
            TOOL_CHOICE_REQUIRED => (
                Some(ToolChoice {
                    mode: ToolChoiceMode::Any,
                    name: None,
                    tool_type: None,
                }),
                Vec::new(),
            ),
            other => (
                None,
                vec![loss(
                    "tool_choice",
                    "tool_choice",
                    LossReason::UnsupportedSemantic,
                    format!("Responses tool_choice {other:?} has no IR equivalent"),
                )],
            ),
        },
        Value::Object(choice) => {
            let kind = choice
                .get("type")
                .and_then(Value::as_str)
                .unwrap_or_default();
            if kind == "apply_patch" {
                return (
                    Some(ToolChoice {
                        mode: ToolChoiceMode::Tool,
                        name: None,
                        tool_type: Some(ToolType::ApplyPatch),
                    }),
                    Vec::new(),
                );
            }
            let name = choice
                .get("name")
                .and_then(Value::as_str)
                .unwrap_or_default();
            if (kind == TOOL_TYPE_FUNCTION || kind == TOOL_TYPE_CUSTOM) && !name.is_empty() {
                (
                    Some(ToolChoice {
                        mode: ToolChoiceMode::Tool,
                        name: Some(name.to_string()),
                        tool_type: if kind == TOOL_TYPE_CUSTOM {
                            Some(ToolType::Custom)
                        } else {
                            None
                        },
                    }),
                    Vec::new(),
                )
            } else {
                (
                    None,
                    vec![loss(
                        "tool_choice",
                        "tool_choice",
                        LossReason::UnsupportedSemantic,
                        "only named function Responses tool_choice values are supported",
                    )],
                )
            }
        }
        _ => (
            None,
            vec![loss(
                "tool_choice",
                "tool_choice",
                LossReason::UnsupportedSemantic,
                "Responses tool_choice has no IR equivalent",
            )],
        ),
    }
}

/// N-R-9 renders the IR's four tool-choice modes in Responses form.
pub(crate) fn encode_tool_choice(choice: Option<&ToolChoice>) -> (Option<Value>, Option<Loss>) {
    let Some(choice) = choice else {
        return (None, None);
    };
    match choice.mode {
        ToolChoiceMode::Auto => (Some(json!(TOOL_CHOICE_AUTO)), None),
        ToolChoiceMode::None => (Some(json!(TOOL_CHOICE_NONE)), None),
        ToolChoiceMode::Any => (Some(json!(TOOL_CHOICE_REQUIRED)), None),
        ToolChoiceMode::Tool => {
            if choice.tool_type == Some(ToolType::ApplyPatch) {
                return (Some(json!({ "type": "apply_patch" })), None);
            }
            match choice.name.as_deref() {
                Some(name) if !name.is_empty() => (
                    Some(
                        json!({ "type": if choice.tool_type==Some(ToolType::Custom){TOOL_TYPE_CUSTOM}else{TOOL_TYPE_FUNCTION}, "name":name }),
                    ),
                    None,
                ),
                _ => (
                    None,
                    Some(loss(
                        "tool_choice",
                        "tool_choice",
                        LossReason::UnsupportedSemantic,
                        "IR named tool choice has no function name",
                    )),
                ),
            }
        }
    }
}

/// Renders an IR image as a Responses input_image part.
pub(crate) fn encode_image_block(
    media_type: Option<&str>,
    data: Option<&str>,
    url: Option<&str>,
    path: &str,
) -> Result<ContentPart, Loss> {
    let data = data.filter(|value| !value.is_empty());
    let url = url.filter(|value| !value.is_empty());
    if data.is_some() && url.is_some() {
        return Err(loss(
            path,
            "image",
            LossReason::UnsupportedSemantic,
            "IR image contains both data and URL",
        ));
    }
    if let Some(data) = data {
        let media_type = media_type.unwrap_or_default();
        if !media_type.to_lowercase().starts_with("image/") || media_type == "image/" {
            return Err(loss(
                path,
                "media_type",
                LossReason::UnsupportedSemantic,
                "IR image media type has no Responses input_image equivalent",
            ));
        }
        return Ok(ContentPart {
            kind: PART_TYPE_INPUT_IMAGE.to_string(),
            text: String::new(),
            image_url: format!("data:{media_type};base64,{data}"),
            detail: None,
            file_id: None,
        });
    }
    if let Some(url) = url
        && is_valid_https_url(url)
    {
        return Ok(ContentPart {
            kind: PART_TYPE_INPUT_IMAGE.to_string(),
            text: String::new(),
            image_url: url.to_string(),
            detail: None,
            file_id: None,
        });
    }
    Err(loss(
        path,
        "image",
        LossReason::UnsupportedSemantic,
        "IR image has no supported Responses input_image equivalent",
    ))
}

/// N-R-3 and N-R-4 render user content as string shorthand only for exactly
/// one text block; otherwise the supported content parts array is used.
pub(crate) fn encode_user_content(blocks: &[Block], path: &str) -> (ContentValue, Vec<Loss>) {
    if let [Block::Text { text, .. }] = blocks {
        return (ContentValue::Text(text.clone()), Vec::new());
    }
    let mut parts = Vec::new();
    let mut losses = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        match block {
            Block::Text { text, .. } => parts.push(ContentPart {
                kind: PART_TYPE_INPUT_TEXT.to_string(),
                text: text.clone(),
                image_url: String::new(),
                detail: None,
                file_id: None,
            }),
            Block::Image {
                media_type,
                data,
                url,
                ..
            } => match encode_image_block(
                media_type.as_deref(),
                data.as_deref(),
                url.as_deref(),
                &format!("{path}[{index}]"),
            ) {
                Ok(part) => parts.push(part),
                Err(image_loss) => losses.push(image_loss),
            },
            _ => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in a Responses user message",
            )),
        }
    }
    (ContentValue::Parts(parts), losses)
}

/// N-R-10 hoists function_call_output items ahead of regular user content.
pub(crate) fn encode_user_message(message: &IrMessage, path: &str) -> (Vec<InputItem>, Vec<Loss>) {
    let mut normal = Vec::new();
    let mut results = Vec::new();
    let mut first = None;
    let mut last = None;
    for (i, b) in message.content.iter().enumerate() {
        if matches!(
            b,
            Block::ToolResult { .. }
                | Block::CustomToolResult { .. }
                | Block::ApplyPatchToolResult { .. }
        ) {
            results.push(b.clone());
            last = Some(i);
        } else {
            first.get_or_insert(i);
            normal.push(b.clone());
        }
    }
    let mut losses = Vec::new();
    if let (Some(a), Some(b)) = (first, last)
        && a < b
    {
        losses.push(loss(
            format!("{path}.content"),
            "ordering",
            LossReason::Degraded,
            "tool output items are hoisted ahead of trailing user content",
        ));
    }
    let mut items = Vec::new();
    for (i, b) in results.iter().enumerate() {
        match b {
            Block::ToolResult {
                tool_use_id,
                content,
                is_error,
                ..
            } => {
                let mut out = String::new();
                for inner in content {
                    if let Block::Text { text, .. } = inner {
                        out.push_str(text)
                    } else {
                        losses.push(loss(
                            format!("{path}.content[{i}].content"),
                            "content",
                            LossReason::UnsupportedSemantic,
                            "unsupported function result content",
                        ));
                    }
                }
                if *is_error == Some(true) {
                    losses.push(loss(
                        format!("{path}.content[{i}].is_error"),
                        "is_error",
                        LossReason::UnmappedField,
                        "Responses function output has no is_error",
                    ));
                }
                items.push(InputItem {
                    kind: ITEM_TYPE_FUNCTION_CALL_OUTPUT.to_string(),
                    call_id: tool_use_id.clone(),
                    output: Some(OutputValue::Text(out)),
                    ..InputItem::default()
                });
            }
            Block::CustomToolResult {
                tool_use_id,
                output,
                ..
            } => {
                let out = match output {
                    CustomToolOutput::Text(s) => OutputValue::Text(s.clone()),
                    CustomToolOutput::Content(bs) => {
                        let mut parts = Vec::new();
                        for (j, b) in bs.iter().enumerate() {
                            match b {
                                Block::Text { text, .. } => parts.push(ContentPart {
                                    kind: PART_TYPE_INPUT_TEXT.to_string(),
                                    text: text.clone(),
                                    image_url: String::new(),
                                    detail: None,
                                    file_id: None,
                                }),
                                Block::Image {
                                    media_type,
                                    data,
                                    url,
                                    ..
                                } => match encode_image_block(
                                    media_type.as_deref(),
                                    data.as_deref(),
                                    url.as_deref(),
                                    &format!("{path}.content[{i}].output[{j}]"),
                                ) {
                                    Ok(p) => parts.push(p),
                                    Err(l) => losses.push(l),
                                },
                                _ => losses.push(loss(
                                    format!("{path}.content[{i}].output[{j}]"),
                                    "type",
                                    LossReason::UnsupportedSemantic,
                                    "custom tool output content is not supported",
                                )),
                            }
                        }
                        OutputValue::Parts(parts)
                    }
                };
                items.push(InputItem {
                    kind: ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT.to_string(),
                    call_id: tool_use_id.clone(),
                    output: Some(out),
                    ..InputItem::default()
                });
            }
            Block::ApplyPatchToolResult {
                tool_use_id,
                status,
                output,
                ..
            } => {
                let status_str = match status {
                    ApplyPatchResultStatus::Completed => "completed",
                    ApplyPatchResultStatus::Failed => "failed",
                };
                items.push(InputItem {
                    kind: "apply_patch_call_output".to_string(),
                    call_id: tool_use_id.clone(),
                    status: status_str.to_string(),
                    output: output.as_ref().map(|s| OutputValue::Text(s.clone())),
                    ..InputItem::default()
                });
            }
            _ => unreachable!(),
        }
    }
    if !normal.is_empty() || results.is_empty() {
        let (c, l) = encode_user_content(&normal, &format!("{path}.content"));
        items.push(InputItem {
            role: ROLE_USER.to_string(),
            content: Some(c),
            ..InputItem::default()
        });
        losses.extend(l);
    }
    (items, losses)
}

/// N-R-5 renders assistant text before function_call items and preserves the
/// opaque arguments text without parsing or re-serializing it.
pub(crate) fn encode_assistant_message(
    blocks: &[Block],
    path: &str,
) -> (Vec<InputItem>, Vec<Loss>) {
    let mut thinkings = Vec::new();
    let mut text = String::new();
    let mut has_text = false;
    let mut calls = Vec::new();
    let mut losses = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        match block {
            Block::Text { text: value, .. } => {
                text.push_str(value);
                has_text = true;
            }
            Block::Thinking {
                thinking,
                signature,
                ..
            } => {
                thinkings.push(OutputPart {
                    kind: PART_TYPE_OUTPUT_TEXT.to_string(),
                    text: thinking.clone(),
                    annotations: Vec::new(),
                });
                if signature
                    .as_deref()
                    .is_some_and(|signature| !signature.is_empty())
                {
                    losses.push(loss(
                        format!("{path}[{index}].signature"),
                        "signature",
                        LossReason::UnmappedField,
                        "Responses reasoning input items carry no provider signature",
                    ));
                }
            }
            Block::ApplyPatchToolUse {
                id,
                status,
                operation,
                ..
            } => {
                let status_str = match status {
                    ApplyPatchCallStatus::InProgress => "in_progress",
                    ApplyPatchCallStatus::Completed => "completed",
                };
                calls.push(InputItem {
                    kind: "apply_patch_call".to_string(),
                    call_id: id.clone(),
                    status: status_str.to_string(),
                    operation: Some(encode_apply_patch_operation(operation)),
                    ..InputItem::default()
                });
            }
            Block::ToolUse {
                id, name, input, ..
            } => calls.push(InputItem {
                kind: ITEM_TYPE_FUNCTION_CALL.to_string(),
                call_id: id.clone(),
                name: name.clone(),
                arguments: input.clone(),
                ..InputItem::default()
            }),
            Block::CustomToolUse {
                id, name, input, ..
            } => calls.push(InputItem {
                kind: ITEM_TYPE_CUSTOM_TOOL_CALL.to_string(),
                call_id: id.clone(),
                name: name.clone(),
                input: Some(input.clone()),
                ..InputItem::default()
            }),
            Block::Image { .. }
            | Block::ToolResult { .. }
            | Block::CustomToolResult { .. }
            | Block::ApplyPatchToolResult { .. } => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in a Responses assistant input item",
            )),
        }
    }
    let mut items = Vec::new();
    if !thinkings.is_empty() {
        items.push(InputItem {
            kind: ITEM_TYPE_REASONING.to_string(),
            summary: thinkings,
            ..InputItem::default()
        });
    }
    if has_text || blocks.is_empty() {
        items.push(InputItem {
            role: ROLE_ASSISTANT.to_string(),
            content: Some(ContentValue::Text(text)),
            ..InputItem::default()
        });
    }
    items.extend(calls);
    (items, losses)
}

pub(crate) fn content_system(blocks: Vec<Block>, path: &str) -> (Vec<SystemBlock>, Vec<Loss>) {
    let mut out = Vec::new();
    let mut losses = Vec::new();
    for (index, block) in blocks.into_iter().enumerate() {
        match block {
            Block::Text { text, .. } => out.push(SystemBlock::Text {
                text,
                native: Vec::new(),
            }),
            _ => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in the IR system field",
            )),
        }
    }
    (out, losses)
}
