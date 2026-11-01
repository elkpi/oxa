//! Face → IR conversions (spec/11 §4).

use serde::{Deserialize, Serialize};
use serde_json::Value;

use oxa_ir::{
    Block, CustomToolFormat, CustomToolOutput, InputTokensDetails, Loss, LossReason,
    OutputTokensDetails, Params, ReasoningEffort, Request as IrRequest, Response as IrResponse,
    ResponseFormat, Role, StopReason, ToolType, Usage,
};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{
    content_system, decode_assistant_run, decode_content, decode_output_run, decode_tool_choice,
    loss,
};
use crate::types::{
    INCOMPLETE_REASON_MAX_OUTPUT_TOKENS, ITEM_TYPE_CUSTOM_TOOL_CALL,
    ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT, ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE, ITEM_TYPE_REASONING, Input, OutputValue, PART_TYPE_INPUT_IMAGE,
    PART_TYPE_INPUT_TEXT, PART_TYPE_OUTPUT_TEXT, ROLE_ASSISTANT, ROLE_DEVELOPER, ROLE_SYSTEM,
    ROLE_USER, Request, Response, STATUS_COMPLETED, STATUS_FAILED, STATUS_INCOMPLETE,
    TOOL_TYPE_CUSTOM, TOOL_TYPE_FUNCTION, UsageWire,
};

/// Converts a Responses wire request to the IR (face → IR). Semantic
/// unmappables are losses, never errors; errors are reserved for structural
/// type violations of known fields (spec/02 §4).
pub fn decode_request(wire: &Request, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    if wire.parallel_tool_calls.is_some() {
        losses.push(loss(
            "parallel_tool_calls",
            "parallel_tool_calls",
            LossReason::UnmappedField,
            "Responses parallel tool calls have no IR equivalent in v1.",
        ));
    }

    let metadata = match &wire.metadata {
        Some(md) => {
            if md.keys().any(String::is_empty) {
                return Err(Error::new("responses: metadata key cannot be empty"));
            }
            if md.is_empty() {
                None
            } else {
                Some(md.clone())
            }
        }
        None => None,
    };
    let mut request = IrRequest {
        model: config.map_model(&wire.model),
        system: Vec::new(),
        messages: Vec::new(),
        tools: None,
        tool_choice: None,
        params: None,
        metadata,
        native: Vec::new(),
    };
    if let Some(instructions) = &wire.instructions {
        request.system.push(oxa_ir::SystemBlock::Text {
            text: instructions.clone(),
            role: String::new(),
            native: Vec::new(),
        });
    }

    let mut seen_names = std::collections::HashSet::new();
    let mut tools = Vec::new();
    for (index, tool) in wire.tools.iter().enumerate() {
        let val = serde_json::to_value(tool).map_err(|e| Error::new(e.to_string()))?;
        tools.extend(decode_responses_tool_item(
            &val,
            tool.raw_definition.as_deref(),
            &[],
            &format!("tools[{index}]"),
            &mut seen_names,
            &mut losses,
        )?);
    }
    if !tools.is_empty() {
        request.tools = Some(tools);
    }
    let (choice, choice_losses) = decode_tool_choice(wire.tool_choice.as_ref());
    request.tool_choice = choice;
    losses.extend(choice_losses);

    match &wire.input {
        Input::Text(text) => request.messages.push(oxa_ir::Message {
            role: Role::User,
            content: vec![Block::Text {
                text: text.clone(),
                citations: None,
                native: Vec::new(),
            }],
        }),
        Input::Items(items) => {
            let (decoded_calls, decoded_results, dropped, pair_losses) =
                crate::normalize::precheck_apply_patch_pairs(items)?;
            losses.extend(pair_losses);

            let mut index = 0usize;
            while index < items.len() {
                let item = &items[index];
                match item.kind.as_str() {
                    "" | ITEM_TYPE_MESSAGE => match item.role.as_str() {
                        ROLE_SYSTEM | ROLE_DEVELOPER => {
                            let path = format!("input[{index}].content");
                            let (content, content_losses) =
                                decode_content(item.content.as_ref(), &path, false)?;
                            let (mut system, system_losses) = content_system(content, &path);
                            if item.role == ROLE_DEVELOPER {
                                for block in &mut system {
                                    let oxa_ir::SystemBlock::Text { role, .. } = block;
                                    *role = "developer".to_string();
                                }
                            }
                            request.system.extend(system);
                            losses.extend(content_losses);
                            losses.extend(system_losses);
                            index += 1;
                        }
                        ROLE_USER => {
                            let (mut content, content_losses) = decode_content(
                                item.content.as_ref(),
                                &format!("input[{index}].content"),
                                true,
                            )?;
                            if content.is_empty() {
                                content.push(Block::Text {
                                    text: String::new(),
                                    citations: None,
                                    native: Vec::new(),
                                });
                            }
                            request.messages.push(oxa_ir::Message {
                                role: Role::User,
                                content,
                            });
                            losses.extend(content_losses);
                            index += 1;
                        }
                        ROLE_ASSISTANT => {
                            let (message, next, run_losses) =
                                decode_assistant_run(items, index, &decoded_calls, &dropped)?;
                            if let Some(message) = message {
                                request.messages.push(message);
                            }
                            losses.extend(run_losses);
                            index = next;
                        }
                        other => {
                            return Err(Error::new(format!(
                                "responses: input[{index}]: unknown role {other:?}"
                            )));
                        }
                    },
                    ITEM_TYPE_FUNCTION_CALL
                    | ITEM_TYPE_CUSTOM_TOOL_CALL
                    | crate::types::ITEM_TYPE_APPLY_PATCH_CALL
                    | ITEM_TYPE_REASONING
                    | crate::types::ITEM_TYPE_WEB_SEARCH_CALL => {
                        let (message, next, run_losses) =
                            decode_assistant_run(items, index, &decoded_calls, &dropped)?;
                        if let Some(message) = message {
                            request.messages.push(message);
                        }
                        losses.extend(run_losses);
                        index = next;
                    }
                    ITEM_TYPE_FUNCTION_CALL_OUTPUT
                    | ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT
                    | crate::types::ITEM_TYPE_APPLY_PATCH_CALL_OUTPUT => {
                        let (message, next, output_losses) =
                            decode_output_run(items, index, &decoded_results, &dropped)?;
                        if let Some(message) = message {
                            request.messages.push(message);
                        }
                        losses.extend(output_losses);
                        index = next;
                    }
                    "additional_tools" => {
                        let Some(ref item_tools) = item.tools else {
                            return Err(Error::new(format!(
                                "responses: input[{index}]: additional_tools requires non-empty tools array"
                            )));
                        };
                        if item_tools.is_empty() {
                            return Err(Error::new(format!(
                                "responses: input[{index}]: additional_tools requires non-empty tools array"
                            )));
                        }
                        let mut req_tools = request.tools.take().unwrap_or_default();
                        for (t_idx, t_entry) in item_tools.iter().enumerate() {
                            req_tools.extend(decode_responses_tool_item(
                                t_entry,
                                None,
                                &[],
                                &format!("input[{index}].tools[{t_idx}]"),
                                &mut seen_names,
                                &mut losses,
                            )?);
                        }
                        if !req_tools.is_empty() {
                            request.tools = Some(req_tools);
                        }
                        index += 1;
                    }
                    other => {
                        losses.push(loss(
                            format!("input[{index}]"),
                            "type",
                            LossReason::UnsupportedSemantic,
                            format!("Responses input item type {other:?} has no IR equivalent"),
                        ));
                        index += 1;
                    }
                }
            }
        }
    }
    if request.messages.is_empty() {
        if request.system.is_empty() {
            return Err(Error::new(
                "responses: request carries no conversation input",
            ));
        }
        request.messages.push(oxa_ir::Message {
            role: Role::User,
            content: vec![Block::Text {
                text: String::new(),
                citations: None,
                native: Vec::new(),
            }],
        });
        losses.push(loss(
            "input",
            "input",
            LossReason::Degraded,
            "An empty user message was added because the IR requires at least one conversation message.",
        ));
    }
    validate_tool_history(&request)?;
    let reasoning_effort = decode_reasoning_effort(wire.reasoning.as_ref(), &mut losses)?;
    let response_format = decode_text_format(
        wire.text.as_ref().and_then(|text| text.format.as_ref()),
        &mut losses,
    )?;
    let verbosity = match wire
        .text
        .as_ref()
        .and_then(|text| text.verbosity.as_deref())
    {
        None => None,
        Some(value @ ("low" | "medium" | "high")) => Some(value.to_string()),
        Some(_) => {
            losses.push(loss(
                "text.verbosity",
                "verbosity",
                LossReason::UnmappedValue,
                "unknown output verbosity",
            ));
            None
        }
    };
    let params = Params {
        temperature: wire.temperature,
        top_p: wire.top_p,
        max_tokens: wire.max_output_tokens,
        stop_sequences: None,
        reasoning_effort,
        verbosity,
        response_format,
    };
    if params.temperature.is_some()
        || params.top_p.is_some()
        || params.max_tokens.is_some()
        || params.reasoning_effort.is_some()
        || params.verbosity.is_some()
        || params.response_format.is_some()
    {
        request.params = Some(params);
    }
    Ok((request, losses))
}

fn decode_custom_output(
    output: &OutputValue,
    item_index: usize,
    losses: &mut Vec<Loss>,
) -> CustomToolOutput {
    match output {
        OutputValue::Text(s) => CustomToolOutput::Text(s.clone()),
        OutputValue::Parts(parts) => {
            let mut blocks = Vec::new();
            for (i, p) in parts.iter().enumerate() {
                let path = format!("output[{item_index}].output[{i}]");
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
                            citations: None,
                            native: Vec::new(),
                        });
                    }
                    PART_TYPE_INPUT_IMAGE if !p.image_url.is_empty() => {
                        if !p.text.is_empty() {
                            losses.push(loss(
                                format!("{path}.text"),
                                "text",
                                LossReason::UnmappedField,
                                "input_image has no text field",
                            ));
                        }
                        if p.file_id.is_some() {
                            losses.push(loss(
                                format!("{path}.file_id"),
                                "file_id",
                                LossReason::UnmappedField,
                                "input_image file_id has no IR equivalent",
                            ));
                        }
                        match crate::normalize::decode_image_url(
                            &p.image_url,
                            &format!("{path}.image_url"),
                        ) {
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
    }
}

fn validate_tool_history(req: &IrRequest) -> Result<(), Error> {
    let has_custom = req.messages.iter().any(|m| {
        m.content.iter().any(|b| {
            matches!(
                b,
                Block::CustomToolUse { .. }
                    | Block::CustomToolResult { .. }
                    | Block::ApplyPatchToolUse { .. }
                    | Block::ApplyPatchToolResult { .. }
            )
        })
    });
    if !has_custom {
        return Ok(());
    }
    if req.messages.first().is_some_and(|m| {
        m.role != Role::User
            && !m
                .content
                .iter()
                .any(|b| matches!(b, Block::WebSearchCall { .. }))
    }) {
        return Err(Error::new(
            "responses: conversation must begin with a user message",
        ));
    }
    let mut pending: Vec<(String, &'static str)> = Vec::new();
    for (mi, m) in req.messages.iter().enumerate() {
        if m.role == Role::Assistant {
            pending.clear();
            for b in &m.content {
                match b {
                    Block::ToolUse { id, name, .. } => {
                        if id.is_empty() || name.is_empty() {
                            return Err(Error::new(format!(
                                "responses: messages[{mi}] tool call requires nonempty id and name"
                            )));
                        }
                        if pending.iter().any(|(x, _)| x == id) {
                            return Err(Error::new(format!(
                                "responses: messages[{mi}] has duplicate tool call id {id:?}"
                            )));
                        }
                        pending.push((id.clone(), "function"));
                    }
                    Block::CustomToolUse { id, name, .. } => {
                        if id.is_empty() || name.is_empty() {
                            return Err(Error::new(format!(
                                "responses: messages[{mi}] tool call requires nonempty id and name"
                            )));
                        }
                        if pending.iter().any(|(x, _)| x == id) {
                            return Err(Error::new(format!(
                                "responses: messages[{mi}] has duplicate tool call id {id:?}"
                            )));
                        }
                        pending.push((id.clone(), "custom"));
                    }
                    Block::ApplyPatchToolUse { id, .. } => {
                        if id.is_empty() {
                            return Err(Error::new(format!(
                                "responses: messages[{mi}] tool call requires nonempty id"
                            )));
                        }
                        if pending.iter().any(|(x, _)| x == id) {
                            return Err(Error::new(format!(
                                "responses: messages[{mi}] has duplicate tool call id {id:?}"
                            )));
                        }
                        pending.push((id.clone(), "apply_patch"));
                    }
                    _ => {}
                }
            }
        } else {
            let results: Vec<(&str, &'static str)> = m
                .content
                .iter()
                .filter_map(|b| match b {
                    Block::ToolResult { tool_use_id, .. } => {
                        Some((tool_use_id.as_str(), "function"))
                    }
                    Block::CustomToolResult { tool_use_id, .. } => {
                        Some((tool_use_id.as_str(), "custom"))
                    }
                    Block::ApplyPatchToolResult { tool_use_id, .. } => {
                        Some((tool_use_id.as_str(), "apply_patch"))
                    }
                    _ => None,
                })
                .collect();
            if !results.is_empty() {
                if results.len() != pending.len() {
                    return Err(Error::new(format!(
                        "responses: messages[{mi}] results do not match preceding calls"
                    )));
                }
                for ((id, kind), (got, r_kind)) in pending.iter().zip(results.iter()) {
                    if id.as_str() != *got || kind != r_kind {
                        return Err(Error::new(format!(
                            "responses: messages[{mi}] result call id or category mismatch"
                        )));
                    }
                }
            }
            pending.clear();
        }
    }
    Ok(())
}

fn decode_text_format(
    value: Option<&serde_json::Value>,
    losses: &mut Vec<Loss>,
) -> Result<Option<ResponseFormat>, Error> {
    let Some(value) = value else {
        return Ok(None);
    };
    let serde_json::Value::Object(map) = value else {
        return Err(Error::new("responses: text.format must be an object"));
    };
    let Some(serde_json::Value::String(type_str)) = map.get("type") else {
        return Err(Error::new("responses: text.format requires string type"));
    };
    match type_str.as_str() {
        "text" => Ok(Some(ResponseFormat::Text)),
        "json_object" => Ok(Some(ResponseFormat::JsonObject)),
        "json_schema" => {
            let Some(serde_json::Value::String(name)) = map.get("name") else {
                return Err(Error::new(
                    "responses: text.format of type json_schema requires string name",
                ));
            };
            if name.is_empty() {
                return Err(Error::new("responses: text.format.name must not be empty"));
            }
            let Some(schema) = map.get("schema") else {
                return Err(Error::new(
                    "responses: text.format of type json_schema requires schema",
                ));
            };
            if !schema.is_object() {
                return Err(Error::new(
                    "responses: text.format schema must be an object",
                ));
            }
            let description = match map.get("description") {
                Some(value) => Some(
                    value
                        .as_str()
                        .ok_or_else(|| {
                            Error::new("responses: text.format description must be a string")
                        })?
                        .to_string(),
                ),
                None => None,
            };
            let strict = match map.get("strict") {
                Some(value) if value.is_boolean() => Some(value.as_bool().unwrap_or(false)),
                Some(_) => {
                    return Err(Error::new(
                        "responses: text.format strict must be a boolean",
                    ));
                }
                None => None,
            };
            Ok(Some(ResponseFormat::JsonSchema {
                name: name.clone(),
                description,
                schema: schema.as_object().cloned().unwrap_or_default(),
                schema_raw: None,
                strict,
            }))
        }
        other => {
            losses.push(loss(
                "text.format.type",
                "type",
                LossReason::UnmappedValue,
                format!("unknown text.format type {other:?}"),
            ));
            Ok(None)
        }
    }
}

fn decode_reasoning_effort(
    value: Option<&serde_json::Value>,
    losses: &mut Vec<Loss>,
) -> Result<Option<ReasoningEffort>, Error> {
    let effort = match value {
        None | Some(serde_json::Value::Null) => return Ok(None),
        Some(serde_json::Value::String(effort)) => effort.as_str(),
        Some(serde_json::Value::Object(reasoning)) => {
            if reasoning.contains_key("summary") {
                losses.push(loss(
                    "reasoning.summary",
                    "summary",
                    LossReason::UnmappedField,
                    "Responses reasoning summary preference has no IR equivalent",
                ));
            }
            match reasoning.get("effort") {
                None | Some(serde_json::Value::Null) => return Ok(None),
                Some(serde_json::Value::String(effort)) => effort.as_str(),
                Some(_) => return Err(Error::new("responses: reasoning.effort must be a string")),
            }
        }
        Some(_) => {
            return Err(Error::new(
                "responses: reasoning must be an object or string",
            ));
        }
    };
    let mapped = match effort {
        "minimal" => Some(ReasoningEffort::Minimal),
        "low" => Some(ReasoningEffort::Low),
        "medium" => Some(ReasoningEffort::Medium),
        "high" => Some(ReasoningEffort::High),
        "xhigh" => Some(ReasoningEffort::XHigh),
        "max" => Some(ReasoningEffort::Max),
        other => {
            losses.push(loss(
                "reasoning.effort",
                "effort",
                LossReason::UnmappedValue,
                format!("unknown reasoning effort value {other:?}"),
            ));
            None
        }
    };
    Ok(mapped)
}

#[allow(dead_code)]
fn non_empty(value: String) -> Option<String> {
    if value.is_empty() { None } else { Some(value) }
}

/// Converts a Responses wire response to the IR (face → IR). Envelope fields
/// (object, item ids/status) are exempt from losses; usage.total_tokens is
/// derived and therefore ignored.
pub fn decode_response(wire: &Response, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    let mut thinkings = Vec::new();
    let mut text = Vec::new();
    let mut calls = Vec::new();
    let mut has_tool_use = false;
    for (item_index, item) in wire.output.iter().enumerate() {
        match item.kind.as_str() {
            ITEM_TYPE_REASONING => {
                if item.summary.is_empty() {
                    losses.push(loss(
                        format!("output[{item_index}]"),
                        "type",
                        LossReason::UnsupportedSemantic,
                        "Responses reasoning output item with empty summary carries no convertible content",
                    ));
                } else {
                    for (part_index, part) in item.summary.iter().enumerate() {
                        if part.kind == PART_TYPE_OUTPUT_TEXT || part.kind == "summary_text" {
                            if !part.annotations.is_empty() {
                                losses.push(loss(
                                    format!(
                                        "output[{item_index}].summary[{part_index}].annotations"
                                    ),
                                    "annotations",
                                    LossReason::UnmappedField,
                                    "Responses reasoning summary annotations have no IR equivalent",
                                ));
                            }
                            thinkings.push(Block::Thinking {
                                thinking: part.text.clone(),
                                signature: None,
                                native: Vec::new(),
                            });
                        } else {
                            losses.push(loss(
                                format!("output[{item_index}].summary[{part_index}]"),
                                "type",
                                LossReason::UnsupportedSemantic,
                                format!(
                                    "Responses reasoning summary type {:?} has no IR equivalent",
                                    part.kind
                                ),
                            ));
                        }
                    }
                }
                if !item.encrypted_content.is_empty() {
                    losses.push(loss(
                        format!("output[{item_index}].encrypted_content"),
                        "encrypted_content",
                        LossReason::UnmappedField,
                        "Responses reasoning encrypted_content has no IR equivalent",
                    ));
                }
            }
            ITEM_TYPE_MESSAGE => {
                for (content_index, part) in item.content.iter().enumerate() {
                    if part.kind == "refusal" {
                        let refusal = part.refusal.as_deref().ok_or_else(|| {
                            Error::new(format!(
                                "responses: output[{item_index}].content[{content_index}].refusal is required"
                            ))
                        })?;
                        text.push(Block::Refusal {
                            refusal: refusal.to_string(),
                            category: None,
                            native: Vec::new(),
                        });
                        continue;
                    }
                    if part.kind != PART_TYPE_OUTPUT_TEXT {
                        losses.push(loss(
                            format!("output[{item_index}].content[{content_index}]"),
                            "type",
                            LossReason::UnsupportedSemantic,
                            format!(
                                "Responses output content type {:?} has no IR equivalent",
                                part.kind
                            ),
                        ));
                        continue;
                    }
                    let mut cites: Vec<oxa_ir::TextCitation> = Vec::new();
                    if !part.annotations.is_empty() {
                        for (k, ann) in part.annotations.iter().enumerate() {
                            let Value::Object(map) = ann else {
                                return Err(Error::new(format!(
                                    "responses: output[{item_index}].content[{content_index}].annotations[{k}] must be an object"
                                )));
                            };
                            let Some(Value::String(ann_type)) = map.get("type") else {
                                return Err(Error::new(format!(
                                    "responses: output[{item_index}].content[{content_index}].annotations[{k}] requires type field"
                                )));
                            };
                            if ann_type.is_empty() {
                                return Err(Error::new(format!(
                                    "responses: output[{item_index}].content[{content_index}].annotations[{k}].type must be a non-empty string"
                                )));
                            }
                            let compact = serde_json::to_string(map)
                                .map_err(|e| Error::new(e.to_string()))?;
                            cites.push(oxa_ir::TextCitation {
                                source: "openai".into(),
                                value: compact,
                            });
                        }
                    }
                    text.push(Block::Text {
                        text: part.text.clone(),
                        citations: if cites.is_empty() { None } else { Some(cites) },
                        native: Vec::new(),
                    });
                }
            }
            "web_search_call" => {
                let action = item.action.as_ref().unwrap_or(&serde_json::Value::Null);
                losses.extend(
                    oxa_ir::inspect_web_search_call(
                        &item.id,
                        &item.status,
                        action,
                        &format!("output[{item_index}]"),
                    )
                    .map_err(|error| Error::new(error.to_string()))?,
                );
                calls.push(Block::WebSearchCall {
                    id: item.id.clone(),
                    status: item.status.clone(),
                    action: action.clone(),
                    extra: item.extra.clone(),
                });
            }
            ITEM_TYPE_FUNCTION_CALL => {
                let ns = item.namespace.as_deref().unwrap_or_default();
                let final_name = if !ns.is_empty() && !item.name.starts_with(&format!("{ns}.")) {
                    format!("{ns}.{}", item.name)
                } else {
                    item.name.clone()
                };
                calls.push(Block::ToolUse {
                    id: item.call_id.clone(),
                    name: final_name,
                    input: item.arguments.clone(),
                    native: Vec::new(),
                });
                has_tool_use = true;
            }
            crate::types::ITEM_TYPE_APPLY_PATCH_CALL => {
                if item.id.is_empty() {
                    return Err(Error::new(format!(
                        "responses: output[{item_index}].id is required"
                    )));
                }
                if item.call_id.is_empty() {
                    return Err(Error::new(format!(
                        "responses: output[{item_index}].call_id is required"
                    )));
                }
                let status = crate::normalize::decode_apply_patch_call_status(
                    &item.status,
                    &format!("output[{item_index}]"),
                    &mut losses,
                )?;
                let Some(status) = status else {
                    continue;
                };
                let op = crate::normalize::decode_apply_patch_operation(
                    item.operation.as_ref(),
                    &format!("output[{item_index}]"),
                    &mut losses,
                )?;
                let Some(op) = op else {
                    continue;
                };
                calls.push(Block::ApplyPatchToolUse {
                    id: item.call_id.clone(),
                    status,
                    operation: op,
                    native: Vec::new(),
                });
                has_tool_use = true;
            }
            crate::types::ITEM_TYPE_APPLY_PATCH_CALL_OUTPUT => {
                if item.id.is_empty() {
                    return Err(Error::new(format!(
                        "responses: output[{item_index}].id is required"
                    )));
                }
                if item.call_id.is_empty() {
                    return Err(Error::new(format!(
                        "responses: output[{item_index}].call_id is required"
                    )));
                }
                let status = crate::normalize::decode_apply_patch_result_status(
                    &item.status,
                    &format!("output[{item_index}]"),
                    &mut losses,
                )?;
                let Some(status) = status else {
                    continue;
                };
                let output = match &item.output {
                    None => None,
                    Some(OutputValue::Text(s)) => Some(s.clone()),
                    Some(_) => {
                        return Err(Error::new(format!(
                            "responses: output[{item_index}].output must be a string"
                        )));
                    }
                };
                calls.push(Block::ApplyPatchToolResult {
                    tool_use_id: item.call_id.clone(),
                    status,
                    output,
                    native: Vec::new(),
                });
            }
            ITEM_TYPE_CUSTOM_TOOL_CALL => {
                let input = item.input.as_ref().ok_or_else(|| {
                    Error::new(format!(
                        "responses: output[{item_index}].input is required for custom_tool_call"
                    ))
                })?;
                if item.call_id.is_empty() || item.name.is_empty() {
                    return Err(Error::new(format!(
                        "responses: output[{item_index}] custom_tool_call requires call_id and name"
                    )));
                }
                let ns = item.namespace.as_deref().unwrap_or_default();
                let final_name = if !ns.is_empty() && !item.name.starts_with(&format!("{ns}.")) {
                    format!("{ns}.{}", item.name)
                } else {
                    item.name.clone()
                };
                calls.push(Block::CustomToolUse {
                    id: item.call_id.clone(),
                    name: final_name,
                    input: input.clone(),
                    native: Vec::new(),
                });
                has_tool_use = true;
            }
            ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT => {
                if item.call_id.is_empty() {
                    return Err(Error::new(format!(
                        "responses: output[{item_index}].call_id is required for custom_tool_call_output"
                    )));
                }
                let output=item.output.as_ref().ok_or_else(||Error::new(format!("responses: output[{item_index}].output is required for custom_tool_call_output")))?;
                calls.push(Block::CustomToolResult {
                    tool_use_id: item.call_id.clone(),
                    output: decode_custom_output(output, item_index, &mut losses),
                    native: Vec::new(),
                });
            }
            other => losses.push(loss(
                format!("output[{item_index}]"),
                "type",
                LossReason::UnsupportedSemantic,
                format!("Responses output item type {other:?} has no IR equivalent"),
            )),
        }
    }
    let (mut stop_reason, status_losses) = decode_status(wire, has_tool_use)?;
    losses.extend(status_losses);
    let mut content = thinkings;
    content.extend(text);
    content.extend(calls);
    let has_refusal = content.iter().any(|b| matches!(b, Block::Refusal { .. }));
    if has_refusal && stop_reason == StopReason::EndTurn {
        stop_reason = StopReason::Refusal;
    }
    if !has_refusal
        && stop_reason == StopReason::Refusal
        && let Some(ref err) = wire.error
        && err.code == "refusal"
        && !err.message.is_empty()
    {
        content.push(Block::Refusal {
            refusal: err.message.clone(),
            category: None,
            native: Vec::new(),
        });
    }
    Ok((
        IrResponse {
            id: wire.id.clone(),
            model: config.map_model(&wire.model),
            content,
            stop_reason,
            stop_sequence: None,
            usage: decode_usage(wire.usage.as_ref())?,
        },
        losses,
    ))
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct ContainerEntry {
    pub r#type: String,
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
}

#[derive(Serialize, Deserialize)]
pub struct ContainerPathPayload {
    pub path: Vec<ContainerEntry>,
}

fn compact_json(raw: &str) -> String {
    let mut out = String::with_capacity(raw.len());
    let mut in_string = false;
    let mut escape = false;
    for ch in raw.chars() {
        if in_string {
            out.push(ch);
            if escape {
                escape = false;
            } else if ch == '\\' {
                escape = true;
            } else if ch == '"' {
                in_string = false;
            }
        } else {
            if ch == '"' {
                in_string = true;
                out.push(ch);
            } else if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
                out.push(ch);
            }
        }
    }
    out
}

fn decode_responses_tool_item(
    value: &serde_json::Value,
    raw_def: Option<&str>,
    path_stack: &[ContainerEntry],
    path: &str,
    seen_names: &mut std::collections::HashSet<String>,
    losses: &mut Vec<Loss>,
) -> Result<Vec<oxa_ir::Tool>, Error> {
    let Some(obj) = value.as_object() else {
        return Err(Error::new(format!("responses: {path} must be an object")));
    };
    let kind = obj
        .get("type")
        .and_then(|v| v.as_str())
        .unwrap_or("function");

    if kind == "namespace" || kind == "toolbox" {
        let name = obj.get("name").and_then(|v| v.as_str()).unwrap_or("");
        if name.is_empty() || name.starts_with('.') || name.ends_with('.') || name.contains("..") {
            return Err(Error::new(format!(
                "responses: {path}: invalid {kind} name {name:?}"
            )));
        }
        let Some(tools_arr) = obj.get("tools").and_then(|v| v.as_array()) else {
            return Err(Error::new(format!(
                "responses: {path}: {kind} requires non-empty tools array"
            )));
        };
        if tools_arr.is_empty() {
            return Err(Error::new(format!(
                "responses: {path}: {kind} requires non-empty tools array"
            )));
        }
        let desc = obj
            .get("description")
            .and_then(|v| v.as_str())
            .map(|s| s.to_string());
        let mut new_stack = path_stack.to_vec();
        new_stack.push(ContainerEntry {
            r#type: kind.to_string(),
            name: name.to_string(),
            description: desc,
        });
        let mut out = Vec::new();
        for (idx, child) in tools_arr.iter().enumerate() {
            out.extend(decode_responses_tool_item(
                child,
                None,
                &new_stack,
                &format!("{path}.tools[{idx}]"),
                seen_names,
                losses,
            )?);
        }
        return Ok(out);
    }

    let prefix = path_stack
        .iter()
        .map(|e| e.name.as_str())
        .collect::<Vec<_>>()
        .join(".");

    if kind == crate::types::TOOL_TYPE_APPLY_PATCH {
        return Ok(vec![oxa_ir::Tool {
            tool_type: Some(ToolType::ApplyPatch),
            name: String::new(),
            description: None,
            input_schema: None,
            strict: None,
            format: None,
            native: Vec::new(),
            source: None,
            definition: None,
        }]);
    }

    if kind == TOOL_TYPE_CUSTOM {
        let raw_name = obj.get("name").and_then(|v| v.as_str()).unwrap_or("");
        if raw_name.is_empty() {
            return Err(Error::new(format!(
                "responses: {path}.name is required for custom tool"
            )));
        }
        let final_name = if !prefix.is_empty() && !raw_name.starts_with(&format!("{prefix}.")) {
            format!("{prefix}.{raw_name}")
        } else {
            raw_name.to_string()
        };
        if seen_names.contains(&final_name) {
            return Err(Error::new(format!(
                "responses: duplicate tool name {final_name:?}"
            )));
        }
        seen_names.insert(final_name.clone());

        let format = match obj.get("format") {
            None => None,
            Some(v) => match serde_json::from_value::<CustomToolFormat>(v.clone()) {
                Ok(f)
                    if f.format_type == "text" && f.syntax.is_none() && f.definition.is_none() =>
                {
                    Some(f)
                }
                Ok(f)
                    if f.format_type == "grammar"
                        && matches!(f.syntax.as_deref(), Some("lark" | "regex"))
                        && f.definition.is_some() =>
                {
                    Some(f)
                }
                _ => {
                    losses.push(loss(
                        format!("{path}.format"),
                        "format",
                        LossReason::UnsupportedSemantic,
                        "custom tool format is not supported",
                    ));
                    None
                }
            },
        };
        if obj.get("format").is_some() && format.is_none() {
            return Ok(Vec::new());
        }

        let mut native = Vec::new();
        if !path_stack.is_empty() {
            let val = serde_json::to_string(&ContainerPathPayload {
                path: path_stack.to_vec(),
            })
            .unwrap_or_default();
            native.push(oxa_ir::NativeAnnotation {
                source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                kind: oxa_ir::NATIVE_KIND_CONTAINER.into(),
                value: val,
                omission: Some(oxa_ir::NativeOmission {
                    field: "type".into(),
                    reason: LossReason::Degraded,
                    detail: Some(
                        "tool container grouping was flattened into dotted tool names".into(),
                    ),
                }),
                projection: None,
            });
        }
        if let Some(dl_val) = obj.get("defer_loading") {
            let Some(dl_bool) = dl_val.as_bool() else {
                return Err(Error::new(format!(
                    "responses: {path}.defer_loading must be a boolean"
                )));
            };
            if dl_bool {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_DEFER_LOADING.into(),
                    value: "true".into(),
                    omission: Some(oxa_ir::NativeOmission {
                        field: "defer_loading".into(),
                        reason: LossReason::Degraded,
                        detail: Some(oxa_ir::DEFER_LOADING_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                });
            }
        }

        let desc = obj
            .get("description")
            .and_then(|v| v.as_str())
            .map(|s| s.to_string());
        return Ok(vec![oxa_ir::Tool {
            tool_type: Some(ToolType::Custom),
            name: final_name,
            description: desc,
            input_schema: None,
            strict: None,
            format,
            native,
            source: None,
            definition: None,
        }]);
    }

    if kind == TOOL_TYPE_FUNCTION || kind.is_empty() {
        let raw_name = obj.get("name").and_then(|v| v.as_str()).unwrap_or("");
        if raw_name.is_empty() {
            return Err(Error::new(format!(
                "responses: {path}.name is required for function tool"
            )));
        }
        let final_name = if !prefix.is_empty() && !raw_name.starts_with(&format!("{prefix}.")) {
            format!("{prefix}.{raw_name}")
        } else {
            raw_name.to_string()
        };
        if seen_names.contains(&final_name) {
            return Err(Error::new(format!(
                "responses: duplicate tool name {final_name:?}"
            )));
        }
        seen_names.insert(final_name.clone());

        let strict = match obj.get("strict") {
            None | Some(serde_json::Value::Null) => None,
            Some(serde_json::Value::Bool(value)) => Some(*value),
            Some(_) => {
                return Err(Error::new(format!(
                    "responses: {path}.strict must be a boolean"
                )));
            }
        };

        let mut native = Vec::new();
        if !path_stack.is_empty() {
            let val = serde_json::to_string(&ContainerPathPayload {
                path: path_stack.to_vec(),
            })
            .unwrap_or_default();
            native.push(oxa_ir::NativeAnnotation {
                source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                kind: oxa_ir::NATIVE_KIND_CONTAINER.into(),
                value: val,
                omission: Some(oxa_ir::NativeOmission {
                    field: "type".into(),
                    reason: LossReason::Degraded,
                    detail: Some(
                        "tool container grouping was flattened into dotted tool names".into(),
                    ),
                }),
                projection: None,
            });
        }
        if let Some(dl_val) = obj.get("defer_loading") {
            let Some(dl_bool) = dl_val.as_bool() else {
                return Err(Error::new(format!(
                    "responses: {path}.defer_loading must be a boolean"
                )));
            };
            if dl_bool {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_DEFER_LOADING.into(),
                    value: "true".into(),
                    omission: Some(oxa_ir::NativeOmission {
                        field: "defer_loading".into(),
                        reason: LossReason::Degraded,
                        detail: Some(oxa_ir::DEFER_LOADING_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                });
            }
        }

        let desc = obj
            .get("description")
            .and_then(|v| v.as_str())
            .map(|s| s.to_string());
        return Ok(vec![oxa_ir::Tool {
            tool_type: None,
            name: final_name,
            description: desc,
            input_schema: obj.get("parameters").cloned(),
            strict,
            format: None,
            native,
            source: None,
            definition: None,
        }]);
    }

    // Native tool declaration vs unsupported tool
    let has_name = obj
        .get("name")
        .and_then(|v| v.as_str())
        .is_some_and(|s| !s.is_empty());
    let has_server_label = obj
        .get("server_label")
        .and_then(|v| v.as_str())
        .is_some_and(|s| !s.is_empty());
    let is_known_native = crate::audit::is_known_openai_native_type(kind);

    if has_name || has_server_label || is_known_native {
        let tool_name = obj
            .get("name")
            .and_then(|v| v.as_str())
            .or_else(|| obj.get("server_label").and_then(|v| v.as_str()))
            .unwrap_or(kind);
        if seen_names.contains(tool_name) {
            return Err(Error::new(format!(
                "responses: duplicate tool name {tool_name:?}"
            )));
        }
        seen_names.insert(tool_name.to_string());

        let def_str = if let Some(raw) = raw_def {
            compact_json(raw)
        } else {
            serde_json::to_string(value).unwrap_or_default()
        };

        return Ok(vec![oxa_ir::Tool {
            tool_type: Some(ToolType::Native),
            name: tool_name.to_string(),
            description: None,
            input_schema: None,
            strict: None,
            format: None,
            native: Vec::new(),
            source: Some("openai".into()),
            definition: Some(def_str),
        }]);
    }

    losses.push(loss(
        path.to_string(),
        "type",
        LossReason::UnsupportedSemantic,
        format!("Responses tool type {kind:?} has no IR equivalent"),
    ));
    Ok(Vec::new())
}

fn decode_usage(wire: Option<&UsageWire>) -> Result<Usage, Error> {
    let Some(wire) = wire else {
        return Ok(Usage::default());
    };
    Ok(Usage {
        input_tokens: nonnegative_usage(wire.input_tokens, "usage.input_tokens")?,
        output_tokens: nonnegative_usage(wire.output_tokens, "usage.output_tokens")?,
        input_tokens_details: wire
            .input_token_details
            .map(|details| {
                nonnegative_usage(
                    details.cached_tokens,
                    "usage.input_token_details.cached_tokens",
                )
                .map(|cached_tokens| InputTokensDetails { cached_tokens })
            })
            .transpose()?,
        output_tokens_details: wire
            .output_token_details
            .map(|details| {
                nonnegative_usage(
                    details.reasoning_tokens,
                    "usage.output_token_details.reasoning_tokens",
                )
                .map(|reasoning_tokens| OutputTokensDetails { reasoning_tokens })
            })
            .transpose()?,
        ..Usage::default()
    })
}

fn nonnegative_usage(value: i64, path: &str) -> Result<i64, Error> {
    if value < 0 {
        return Err(Error::new(format!(
            "responses: {path} must be non-negative"
        )));
    }
    Ok(value)
}

pub(crate) fn decode_status(
    wire: &Response,
    has_tool_use: bool,
) -> Result<(StopReason, Vec<Loss>), Error> {
    if let Some(error) = &wire.error {
        if error.code == "refusal" {
            return Ok((StopReason::Refusal, Vec::new()));
        }
        return Ok((
            StopReason::Other,
            vec![loss(
                "error",
                "error",
                LossReason::UnsupportedSemantic,
                format!(
                    "failed Responses response carries error {:?}: {}",
                    error.code, error.message
                ),
            )],
        ));
    }
    match wire.status.as_str() {
        STATUS_COMPLETED => Ok((
            if has_tool_use {
                StopReason::ToolUse
            } else {
                StopReason::EndTurn
            },
            Vec::new(),
        )),
        STATUS_INCOMPLETE => {
            let reason = wire
                .incomplete_details
                .as_ref()
                .map(|details| details.reason.as_str())
                .unwrap_or_default();
            if reason == INCOMPLETE_REASON_MAX_OUTPUT_TOKENS {
                Ok((StopReason::MaxTokens, Vec::new()))
            } else {
                Ok((
                    StopReason::Other,
                    vec![loss(
                        "incomplete_details.reason",
                        "reason",
                        LossReason::UnmappedValue,
                        format!(
                            "Responses incomplete_details reason {reason:?} has no IR equivalent"
                        ),
                    )],
                ))
            }
        }
        STATUS_FAILED => Ok((
            StopReason::Other,
            vec![loss(
                "error",
                "error",
                LossReason::UnsupportedSemantic,
                "failed Responses response carries no error object",
            )],
        )),
        other => Err(Error::new(format!(
            "responses: status {other:?} has no IR equivalent"
        ))),
    }
}
