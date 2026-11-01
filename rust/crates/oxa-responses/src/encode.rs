//! IR → face conversions (spec/11 §4).

use serde_json::Value;

use oxa_ir::{
    Block, CustomToolOutput, Loss, LossReason, ReasoningEffort, Request as IrRequest,
    Response as IrResponse, ResponseFormat, StopReason, ToolType, Usage,
};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{encode_assistant_message, encode_tool_choice, encode_user_message, loss};
use crate::types::{
    ContentValue, ERROR_CODE_REFUSAL, ErrorWire, INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_CUSTOM_TOOL_CALL, ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT, ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_MESSAGE, ITEM_TYPE_REASONING, IncompleteWire, Input, InputTokenDetailsWire,
    OBJECT_RESPONSE, OutputItem, OutputPart, OutputTokenDetailsWire, OutputValue,
    PART_TYPE_INPUT_TEXT, PART_TYPE_OUTPUT_TEXT, ROLE_ASSISTANT, ROLE_USER, Request, Response,
    STATUS_COMPLETED, STATUS_FAILED, STATUS_INCOMPLETE, TOOL_TYPE_CUSTOM, TOOL_TYPE_FUNCTION,
    TextParams, ToolDef, UsageWire,
};

/// Converts an IR request to a Responses wire request (IR → face). System
/// content renders as `instructions`; a sole user text message without system
/// content renders as the `input` string shorthand (N-R-1 and N-R-2).
pub fn encode_request(req: &IrRequest, config: &Config) -> Result<(Request, Vec<Loss>), Error> {
    for tool in req.tools.iter().flatten() {
        tool.validate_strict().map_err(Error::new)?;
    }
    let mut losses = Vec::new();
    for (message_index, message) in req.messages.iter().enumerate() {
        for (block_index, block) in message.content.iter().enumerate() {
            match block {
                Block::WebSearchCall {
                    id,
                    status,
                    action,
                    extra,
                } => {
                    oxa_ir::validate_web_search_extra(extra)
                        .map_err(|error| Error::new(error.to_string()))?;
                    oxa_ir::inspect_web_search_call(
                        id,
                        status,
                        action,
                        &format!("messages[{message_index}].content[{block_index}]"),
                    )
                    .map_err(|error| Error::new(error.to_string()))?;
                }
                Block::CustomToolUse { id, name, .. } if id.is_empty() || name.is_empty() => {
                    return Err(Error::new(format!(
                        "responses: messages[{message_index}].content[{block_index}] custom_tool_use requires id and name"
                    )));
                }
                Block::CustomToolResult { tool_use_id, .. } if tool_use_id.is_empty() => {
                    return Err(Error::new(format!(
                        "responses: messages[{message_index}].content[{block_index}].tool_use_id is required"
                    )));
                }
                _ => {}
            }
        }
    }
    if let Some(metadata) = &req.metadata
        && metadata.keys().any(String::is_empty)
    {
        return Err(Error::new("responses: metadata key cannot be empty"));
    }
    let mut out = Request {
        model: config.map_model(&req.model),
        metadata: req.metadata.clone().filter(|metadata| !metadata.is_empty()),
        ..Request::default()
    };
    let leading_system_items = if !req.system.is_empty() {
        let (instructions, items) = encode_system_instructions(&req.system);
        out.instructions = instructions;
        items
    } else {
        Vec::new()
    };
    if let Some(tools) = &req.tools {
        out.tools = encode_responses_grouped_tools(tools, &mut losses)?;
    }
    let native_named_choice_dropped = req
        .tool_choice
        .as_ref()
        .map(|c| {
            let is_native = c.tool_type == Some(oxa_ir::ToolType::Native)
                || req.tools.as_deref().unwrap_or(&[]).iter().any(|t| {
                    t.tool_type == Some(oxa_ir::ToolType::Native)
                        && Some(&t.name) == c.name.as_ref()
                });
            let is_openai_native = req.tools.as_deref().unwrap_or(&[]).iter().any(|t| {
                t.tool_type == Some(oxa_ir::ToolType::Native)
                    && Some(&t.name) == c.name.as_ref()
                    && t.source.as_deref() == Some("openai")
            });
            is_native && !is_openai_native
        })
        .unwrap_or(false);

    let (mut choice, choice_loss) = encode_tool_choice(req.tool_choice.as_ref());
    if req
        .tool_choice
        .as_ref()
        .is_some_and(|c| c.mode == oxa_ir::ToolChoiceMode::Any)
        && req.tools.as_ref().map(|t| !t.is_empty()).unwrap_or(false)
        && out.tools.is_empty()
    {
        choice = None;
        losses.push(loss(
            "tool_choice",
            "tool_choice",
            LossReason::UnsupportedSemantic,
            "all tool declarations were dropped",
        ));
    } else if native_named_choice_dropped {
        let name = req
            .tool_choice
            .as_ref()
            .and_then(|c| c.name.as_deref())
            .unwrap_or("");
        choice = None;
        losses.push(loss(
            "tool_choice",
            "tool_choice",
            LossReason::UnsupportedSemantic,
            format!("native tool choice \"{name}\" cannot be represented by the target protocol"),
        ));
    } else if let Some(choice_loss) = choice_loss {
        losses.push(choice_loss);
    }
    out.tool_choice = choice;
    let mut parallel_tool_calls = None;
    if let Some(dp) = req.tool_choice.as_ref().and_then(|c| c.disable_parallel) {
        parallel_tool_calls = Some(serde_json::Value::Bool(!dp));
    } else if let Some(ann) = req
        .native
        .iter()
        .find(|a| a.source == "anthropic" && a.kind == "disable_parallel_tool_use")
        && let Ok(b) = serde_json::from_str::<bool>(&ann.value)
    {
        parallel_tool_calls = Some(serde_json::Value::Bool(!b));
    }
    out.parallel_tool_calls = parallel_tool_calls;

    let mut items = leading_system_items;
    for (index, message) in req.messages.iter().enumerate() {
        match message.role {
            oxa_ir::Role::User => {
                let (message_items, message_losses) =
                    encode_user_message(message, &format!("messages[{index}]"));
                items.extend(message_items);
                losses.extend(message_losses);
            }
            oxa_ir::Role::Assistant => {
                let (message_items, message_losses) = encode_assistant_message(
                    &message.content,
                    &format!("messages[{index}].content"),
                );
                items.extend(message_items);
                losses.extend(message_losses);
            }
        }
    }
    if req.system.is_empty()
        && let [item] = items.as_slice()
        && item.role == ROLE_USER
        && let Some(ContentValue::Text(text)) = &item.content
    {
        out.input = Input::Text(text.clone());
    } else {
        out.input = Input::Items(items);
    }

    if let Some(params) = &req.params {
        out.temperature = params.temperature;
        out.top_p = params.top_p;
        out.max_output_tokens = params.max_tokens;
        out.reasoning = params.reasoning_effort.map(|effort| {
            let effort = match effort {
                ReasoningEffort::Minimal => "minimal",
                ReasoningEffort::Low => "low",
                ReasoningEffort::Medium => "medium",
                ReasoningEffort::High => "high",
                ReasoningEffort::XHigh => "xhigh",
                ReasoningEffort::Max => "max",
            };
            serde_json::json!({ "effort": effort })
        });
        if let Some(rf) = &params.response_format {
            if matches!(rf, ResponseFormat::JsonSchema { name, .. } if name.is_empty()) {
                losses.push(loss(
                    "params.response_format",
                    "name",
                    LossReason::UnsupportedSemantic,
                    "Responses requires a non-empty schema name",
                ));
            } else {
                let format_val = match rf {
                    ResponseFormat::Text => serde_json::json!({ "type": "text" }),
                    ResponseFormat::JsonObject => serde_json::json!({ "type": "json_object" }),
                    ResponseFormat::JsonSchema {
                        name,
                        description,
                        schema,
                        strict,
                        ..
                    } => {
                        let mut obj = serde_json::Map::new();
                        obj.insert(
                            "type".to_string(),
                            serde_json::Value::String("json_schema".to_string()),
                        );
                        obj.insert("name".to_string(), serde_json::Value::String(name.clone()));
                        if let Some(desc) = description {
                            obj.insert(
                                "description".to_string(),
                                serde_json::Value::String(desc.clone()),
                            );
                        }
                        obj.insert(
                            "schema".to_string(),
                            serde_json::Value::Object(schema.clone()),
                        );
                        if let Some(s) = strict {
                            obj.insert("strict".to_string(), serde_json::Value::Bool(*s));
                        }
                        serde_json::Value::Object(obj)
                    }
                };
                out.text = Some(TextParams {
                    verbosity: params.verbosity.clone(),
                    format: Some(format_val),
                });
            }
        }
        if out.text.is_none() && params.verbosity.is_some() {
            out.text = Some(TextParams {
                verbosity: params.verbosity.clone(),
                format: None,
            });
        }
        if params
            .stop_sequences
            .as_ref()
            .is_some_and(|stops| !stops.is_empty())
        {
            losses.push(loss(
                "params.stop_sequences",
                "stop_sequences",
                LossReason::UnmappedField,
                "Responses requests have no stop-sequences parameter; the IR stop sequences are dropped.",
            ));
        }
    }
    for n in &req.native {
        if n.source == "openai" && n.kind == oxa_ir::NATIVE_KIND_STORE {
            out.store = serde_json::from_str::<bool>(&n.value).ok();
        }
    }
    let omission_losses = oxa_ir::omitted_native_request_losses(req, "responses")
        .map_err(|e| Error::new(e.to_string()))?;
    for l in omission_losses {
        if l.field == "disable_parallel_tool_use" && out.parallel_tool_calls.is_some() {
            continue;
        }
        losses.push(l);
    }
    Ok((out, losses))
}

/// Converts an IR response to a Responses wire response (IR → face). Envelope
/// fields absent from the IR use N-R-12's documented defaults without loss.
pub fn encode_response(resp: &IrResponse, config: &Config) -> Result<(Response, Vec<Loss>), Error> {
    validate_usage(&resp.usage)?;
    let total_tokens = resp
        .usage
        .input_tokens
        .checked_add(resp.usage.output_tokens)
        .ok_or_else(|| Error::new("responses: derived total_tokens exceeds int64"))?;
    let mut losses = Vec::new();
    let mut thinkings = Vec::new();
    let mut text_parts: Vec<OutputPart> = Vec::new();
    let mut calls = Vec::new();
    let mut apc_index = 0usize;
    let mut apco_index = 0usize;
    let mut refusal_msg = None;
    for (index, block) in resp.content.iter().enumerate() {
        match block {
            Block::Text {
                text: value,
                citations,
                ..
            } => {
                let mut part_annotations: Vec<Value> = Vec::new();
                if let Some(cites) = citations
                    && !cites.is_empty()
                {
                    let mut has_anthropic = false;
                    for c in cites {
                        if c.source == "openai" {
                            if c.value.is_empty() {
                                return Err(Error::new("ir: citation value cannot be empty"));
                            }
                            let obj: Value = serde_json::from_str(&c.value).map_err(|e| {
                                Error::new(format!("ir: citation value must be a JSON object: {e}"))
                            })?;
                            let Value::Object(map) = &obj else {
                                return Err(Error::new("ir: citation value must be a JSON object"));
                            };
                            let Some(Value::String(ann_type)) = map.get("type") else {
                                return Err(Error::new("ir: citation value requires type field"));
                            };
                            if ann_type.is_empty() {
                                return Err(Error::new(
                                    "ir: citation type must be a non-empty string",
                                ));
                            }
                            part_annotations.push(obj);
                        } else if c.source == "anthropic" {
                            has_anthropic = true;
                        } else {
                            return Err(Error::new(format!(
                                "ir: unknown citation source {:?}",
                                c.source
                            )));
                        }
                    }
                    if has_anthropic {
                        losses.push(loss(
                            format!("content[{index}].citations"),
                            "citations",
                            LossReason::UnsupportedSemantic,
                            "anthropic text citations; the target face cannot preserve source locations and replay metadata",
                        ));
                    }
                }
                text_parts.push(OutputPart {
                    kind: PART_TYPE_OUTPUT_TEXT.to_string(),
                    text: value.clone(),
                    annotations: part_annotations,
                    ..Default::default()
                });
            }
            Block::RedactedThinking { .. } => {
                losses.push(loss(
                    format!("content[{index}]"),
                    "type",
                    LossReason::UnsupportedSemantic,
                    "anthropic encrypted thinking replay state; the target face cannot preserve this block",
                ));
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
                    ..Default::default()
                });
                if signature
                    .as_deref()
                    .is_some_and(|signature| !signature.is_empty())
                {
                    losses.push(loss(
                        format!("content[{index}].signature"),
                        "signature",
                        LossReason::UnmappedField,
                        "Responses reasoning output items carry no provider signature",
                    ));
                }
            }
            Block::WebSearchCall {
                id,
                status,
                action,
                extra,
            } => {
                oxa_ir::validate_web_search_extra(extra)
                    .map_err(|error| Error::new(error.to_string()))?;
                oxa_ir::inspect_web_search_call(id, status, action, &format!("content[{index}]"))
                    .map_err(|error| Error::new(error.to_string()))?;
                calls.push(OutputItem {
                    kind: "web_search_call".into(),
                    id: id.clone(),
                    status: status.clone(),
                    action: Some(action.clone()),
                    extra: extra.clone(),
                    ..OutputItem::default()
                });
            }
            Block::ApplyPatchToolUse {
                id,
                status,
                operation,
                ..
            } => {
                let status_str = match status {
                    oxa_ir::ApplyPatchCallStatus::InProgress => "in_progress",
                    oxa_ir::ApplyPatchCallStatus::Completed => "completed",
                };
                calls.push(OutputItem {
                    kind: "apply_patch_call".to_string(),
                    id: format!("apc_{apc_index}"),
                    status: status_str.to_string(),
                    call_id: id.clone(),
                    operation: Some(crate::normalize::encode_apply_patch_operation(operation)),
                    ..OutputItem::default()
                });
                apc_index += 1;
            }
            Block::ApplyPatchToolResult {
                tool_use_id,
                status,
                output,
                ..
            } => {
                let status_str = match status {
                    oxa_ir::ApplyPatchResultStatus::Completed => "completed",
                    oxa_ir::ApplyPatchResultStatus::Failed => "failed",
                };
                calls.push(OutputItem {
                    kind: "apply_patch_call_output".to_string(),
                    id: format!("apco_{apco_index}"),
                    status: status_str.to_string(),
                    call_id: tool_use_id.clone(),
                    output: output.as_ref().map(|s| OutputValue::Text(s.clone())),
                    ..OutputItem::default()
                });
                apco_index += 1;
            }
            Block::ToolUse {
                id, name, input, ..
            } => {
                let dot_idx = name.find('.');
                let (ns, nm) = if let Some(idx) = dot_idx {
                    (Some(name[..idx].to_string()), name[idx + 1..].to_string())
                } else {
                    (None, name.clone())
                };
                calls.push(OutputItem {
                    kind: ITEM_TYPE_FUNCTION_CALL.to_string(),
                    id: "fc_abc123".to_string(),
                    status: STATUS_COMPLETED.to_string(),
                    call_id: id.clone(),
                    name: nm,
                    namespace: ns,
                    arguments: input.clone(),
                    ..OutputItem::default()
                });
            }
            Block::CustomToolUse {
                id, name, input, ..
            } => {
                if id.is_empty() || name.is_empty() {
                    return Err(Error::new(format!(
                        "responses: content[{index}] custom_tool_use requires id and name"
                    )));
                }
                let dot_idx = name.find('.');
                let (ns, nm) = if let Some(idx) = dot_idx {
                    (Some(name[..idx].to_string()), name[idx + 1..].to_string())
                } else {
                    (None, name.clone())
                };
                calls.push(OutputItem {
                    kind: ITEM_TYPE_CUSTOM_TOOL_CALL.to_string(),
                    id: "ctc_abc123".to_string(),
                    call_id: id.clone(),
                    name: nm,
                    namespace: ns,
                    input: Some(input.clone()),
                    ..OutputItem::default()
                });
            }
            Block::CustomToolResult {
                tool_use_id,
                output,
                ..
            } => {
                if tool_use_id.is_empty() {
                    return Err(Error::new(format!(
                        "responses: content[{index}].tool_use_id is required"
                    )));
                }
                calls.push(OutputItem {
                    kind: ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT.to_string(),
                    call_id: tool_use_id.clone(),
                    output: Some(encode_custom_output(
                        output,
                        &format!("content[{index}]"),
                        &mut losses,
                    )),
                    ..OutputItem::default()
                });
            }
            Block::Image { .. } | Block::ToolResult { .. } | Block::Document { .. } => {
                losses.push(loss(
                    format!("content[{index}]"),
                    "content",
                    LossReason::UnsupportedSemantic,
                    "this IR block cannot be rendered in a Responses output item",
                ))
            }
            Block::Refusal {
                refusal, category, ..
            } => {
                if category.is_some() {
                    losses.push(loss(
                        format!("content[{index}].category"),
                        "category",
                        LossReason::UnsupportedSemantic,
                        "Anthropic refusal category has no Responses equivalent",
                    ));
                }
                text_parts.push(OutputPart {
                    kind: "refusal".to_string(),
                    refusal: Some(refusal.clone()),
                    ..OutputPart::default()
                });
                if refusal_msg.is_none() {
                    refusal_msg = Some(refusal.clone());
                }
            }
        }
    }
    let mut output = calls;
    if !text_parts.is_empty() || (resp.content.is_empty() && thinkings.is_empty()) {
        output.insert(
            0,
            OutputItem {
                kind: ITEM_TYPE_MESSAGE.to_string(),
                id: "msg_abc123".to_string(),
                status: STATUS_COMPLETED.to_string(),
                role: ROLE_ASSISTANT.to_string(),
                content: if text_parts.is_empty() {
                    vec![OutputPart {
                        kind: PART_TYPE_OUTPUT_TEXT.to_string(),
                        text: String::new(),
                        annotations: Vec::new(),
                        ..Default::default()
                    }]
                } else {
                    text_parts
                },
                ..OutputItem::default()
            },
        );
    }
    if !thinkings.is_empty() {
        output.insert(
            0,
            OutputItem {
                kind: ITEM_TYPE_REASONING.to_string(),
                id: "rs_abc123".to_string(),
                summary: thinkings,
                ..OutputItem::default()
            },
        );
    }
    let (status, incomplete_details, error) = match resp.stop_reason {
        StopReason::EndTurn | StopReason::ToolUse => (STATUS_COMPLETED.to_string(), None, None),
        StopReason::MaxTokens => (
            STATUS_INCOMPLETE.to_string(),
            Some(IncompleteWire {
                reason: INCOMPLETE_REASON_MAX_OUTPUT_TOKENS.to_string(),
            }),
            None,
        ),
        StopReason::StopSequence => {
            losses.push(loss(
                "",
                "stop_sequence",
                LossReason::UnmappedValue,
                "Responses status carries no stop-sequence identity; the matched IR stop sequence is lost",
            ));
            (STATUS_COMPLETED.to_string(), None, None)
        }
        StopReason::Refusal => (
            STATUS_FAILED.to_string(),
            None,
            Some(ErrorWire {
                code: ERROR_CODE_REFUSAL.to_string(),
                message: refusal_msg.unwrap_or_default(),
            }),
        ),
        StopReason::Other => {
            return Err(Error::new(
                "responses: stop reason \"other\" has no Responses equivalent",
            ));
        }
    };
    Ok((
        Response {
            id: resp.id.clone(),
            object: OBJECT_RESPONSE.to_string(),
            status,
            model: config.map_model(&resp.model),
            output,
            usage: Some(UsageWire {
                input_tokens: resp.usage.input_tokens,
                output_tokens: resp.usage.output_tokens,
                total_tokens,
                input_token_details: resp.usage.input_tokens_details.map(|details| {
                    InputTokenDetailsWire {
                        cached_tokens: details.cached_tokens,
                    }
                }),
                output_token_details: resp.usage.output_tokens_details.map(|details| {
                    OutputTokenDetailsWire {
                        reasoning_tokens: details.reasoning_tokens,
                    }
                }),
            }),
            incomplete_details,
            error,
        },
        losses,
    ))
}

fn parse_container_path(tool: &oxa_ir::Tool) -> Option<Vec<crate::decode::ContainerEntry>> {
    for ann in &tool.native {
        if ann.source == "openai" && ann.kind == "container" {
            if let Ok(payload) =
                serde_json::from_str::<crate::decode::ContainerPathPayload>(&ann.value)
                && !payload.path.is_empty()
            {
                return Some(payload.path);
            }
            let single = ann.value.trim_matches('"');
            if !single.is_empty() {
                let dot_idx = tool.name.find('.');
                let name = if let Some(idx) = dot_idx {
                    tool.name[..idx].to_string()
                } else {
                    tool.name.clone()
                };
                return Some(vec![crate::decode::ContainerEntry {
                    r#type: single.to_string(),
                    name,
                    description: None,
                }]);
            }
        }
    }
    None
}

fn build_nested_container(
    path_stack: &[crate::decode::ContainerEntry],
    tools: &[oxa_ir::Tool],
) -> Result<ToolDef, Error> {
    if path_stack.is_empty() {
        return Err(Error::new("empty path_stack"));
    }
    let curr = &path_stack[0];
    if path_stack.len() == 1 {
        let mut child_tools = Vec::new();
        for sub_tool in tools {
            let last_dot = sub_tool.name.rfind('.');
            let stripped_name = if let Some(idx) = last_dot {
                &sub_tool.name[idx + 1..]
            } else {
                &sub_tool.name
            };
            let has_defer = sub_tool
                .native
                .iter()
                .any(|ann| ann.kind == "defer_loading" && ann.value == "true");

            if sub_tool.tool_type == Some(ToolType::Custom) {
                let fmt_val = match &sub_tool.format {
                    Some(f) => serde_json::to_value(f).ok(),
                    None => None,
                };
                let mut m = serde_json::Map::new();
                m.insert(
                    "type".into(),
                    serde_json::Value::String(TOOL_TYPE_CUSTOM.into()),
                );
                m.insert(
                    "name".into(),
                    serde_json::Value::String(stripped_name.into()),
                );
                if let Some(ref d) = sub_tool.description {
                    m.insert("description".into(), serde_json::Value::String(d.clone()));
                }
                if let Some(f) = fmt_val {
                    m.insert("format".into(), f);
                }
                if has_defer {
                    m.insert("defer_loading".into(), serde_json::Value::Bool(true));
                }
                child_tools.push(serde_json::Value::Object(m));
            } else {
                let mut m = serde_json::Map::new();
                m.insert(
                    "type".into(),
                    serde_json::Value::String(TOOL_TYPE_FUNCTION.into()),
                );
                m.insert(
                    "name".into(),
                    serde_json::Value::String(stripped_name.into()),
                );
                if let Some(ref d) = sub_tool.description {
                    m.insert("description".into(), serde_json::Value::String(d.clone()));
                }
                if let Some(ref p) = sub_tool.input_schema {
                    m.insert("parameters".into(), p.clone());
                }
                if let Some(strict) = sub_tool.strict {
                    m.insert("strict".into(), serde_json::Value::Bool(strict));
                }
                if has_defer {
                    m.insert("defer_loading".into(), serde_json::Value::Bool(true));
                }
                child_tools.push(serde_json::Value::Object(m));
            }
        }
        let mut obj = serde_json::Map::new();
        obj.insert(
            "type".into(),
            serde_json::Value::String(curr.r#type.clone()),
        );
        obj.insert("name".into(), serde_json::Value::String(curr.name.clone()));
        if let Some(ref d) = curr.description {
            obj.insert("description".into(), serde_json::Value::String(d.clone()));
        }
        obj.insert(
            "tools".into(),
            serde_json::Value::Array(child_tools.clone()),
        );
        return Ok(ToolDef {
            kind: curr.r#type.clone(),
            name: curr.name.clone(),
            description: curr.description.clone().unwrap_or_default(),
            tools: Some(child_tools),
            raw: Some(serde_json::Value::Object(obj)),
            ..ToolDef::default()
        });
    }

    // Multi-level
    let mut child_tools = Vec::new();
    let mut k = 0;
    while k < tools.len() {
        let sub_path = parse_container_path(&tools[k]);
        if sub_path.as_ref().is_none_or(|p| p.len() < 2) {
            k += 1;
            continue;
        }
        let next_name = sub_path.as_ref().unwrap()[1].name.clone();
        let mut m = k;
        while m < tools.len() {
            let p = parse_container_path(&tools[m]);
            if p.as_ref().map_or(0, |p| p.len()) < 2 || p.as_ref().unwrap()[1].name != next_name {
                break;
            }
            m += 1;
        }
        let nested_def = build_nested_container(&path_stack[1..], &tools[k..m])?;
        if let Some(raw) = nested_def.raw {
            child_tools.push(raw);
        }
        k = m;
    }
    let mut obj = serde_json::Map::new();
    obj.insert(
        "type".into(),
        serde_json::Value::String(curr.r#type.clone()),
    );
    obj.insert("name".into(), serde_json::Value::String(curr.name.clone()));
    if let Some(ref d) = curr.description {
        obj.insert("description".into(), serde_json::Value::String(d.clone()));
    }
    obj.insert(
        "tools".into(),
        serde_json::Value::Array(child_tools.clone()),
    );
    Ok(ToolDef {
        kind: curr.r#type.clone(),
        name: curr.name.clone(),
        description: curr.description.clone().unwrap_or_default(),
        tools: Some(child_tools),
        raw: Some(serde_json::Value::Object(obj)),
        ..ToolDef::default()
    })
}

fn encode_responses_grouped_tools(
    tools: &[oxa_ir::Tool],
    losses: &mut Vec<Loss>,
) -> Result<Vec<ToolDef>, Error> {
    let mut out = Vec::new();
    let mut i = 0;
    while i < tools.len() {
        let tool = &tools[i];
        if tool.tool_type == Some(ToolType::Native) {
            if tool.source.as_deref() == Some("openai") {
                let val: serde_json::Value =
                    serde_json::from_str(tool.definition.as_deref().unwrap_or("{}"))
                        .map_err(|e| Error::new(e.to_string()))?;
                out.push(ToolDef {
                    kind: tool.name.clone(),
                    raw: Some(val),
                    ..ToolDef::default()
                });
            } else {
                let source = tool.source.as_deref().unwrap_or_default();
                losses.push(loss(
                    format!("tools[{i}]"),
                    "type",
                    LossReason::UnsupportedSemantic,
                    format!(
                        "native tool \"{}\" from \"{source}\" cannot be represented by the target protocol",
                        tool.name
                    ),
                ));
            }
            i += 1;
            continue;
        }

        if tool.tool_type == Some(ToolType::ApplyPatch) {
            out.push(ToolDef {
                kind: "apply_patch".to_string(),
                ..ToolDef::default()
            });
            i += 1;
            continue;
        }

        let container_path = parse_container_path(tool);
        let dot_idx = tool.name.find('.');
        if let Some(ref path_entries) = container_path {
            let top_name = &path_entries[0].name;
            let mut j = i;
            while j < tools.len() {
                let next_tool = &tools[j];
                if next_tool.tool_type == Some(ToolType::Native)
                    || next_tool.tool_type == Some(ToolType::ApplyPatch)
                {
                    break;
                }
                let next_path = parse_container_path(next_tool);
                if next_path
                    .as_ref()
                    .is_none_or(|p| p.is_empty() || &p[0].name != top_name)
                {
                    break;
                }
                j += 1;
            }
            out.push(build_nested_container(path_entries, &tools[i..j])?);
            i = j;
            continue;
        } else if let Some(idx) = dot_idx {
            let prefix = &tool.name[..idx];
            let mut j = i;
            while j < tools.len() {
                let next_tool = &tools[j];
                if next_tool.tool_type == Some(ToolType::Native)
                    || next_tool.tool_type == Some(ToolType::ApplyPatch)
                {
                    break;
                }
                if !next_tool.name.starts_with(&format!("{prefix}.")) {
                    break;
                }
                j += 1;
            }
            let synthetic = vec![crate::decode::ContainerEntry {
                r#type: "namespace".to_string(),
                name: prefix.to_string(),
                description: None,
            }];
            out.push(build_nested_container(&synthetic, &tools[i..j])?);
            i = j;
            continue;
        }

        let has_defer = tool
            .native
            .iter()
            .any(|ann| ann.kind == "defer_loading" && ann.value == "true");
        if tool.tool_type == Some(ToolType::Custom) {
            let fmt_val = match &tool.format {
                Some(f) => serde_json::to_value(f).ok(),
                None => None,
            };
            out.push(ToolDef {
                kind: TOOL_TYPE_CUSTOM.to_string(),
                name: tool.name.clone(),
                description: tool.description.clone().unwrap_or_default(),
                format: fmt_val,
                defer_loading: if has_defer { Some(true) } else { None },
                ..ToolDef::default()
            });
        } else {
            out.push(ToolDef {
                kind: TOOL_TYPE_FUNCTION.to_string(),
                name: tool.name.clone(),
                description: tool.description.clone().unwrap_or_default(),
                parameters: tool.input_schema.clone(),
                strict: tool.strict,
                defer_loading: if has_defer { Some(true) } else { None },
                ..ToolDef::default()
            });
        }
        i += 1;
    }
    Ok(out)
}

fn encode_custom_output(
    output: &CustomToolOutput,
    path: &str,
    losses: &mut Vec<Loss>,
) -> OutputValue {
    match output {
        CustomToolOutput::Text(s) => OutputValue::Text(s.clone()),
        CustomToolOutput::Content(blocks) => {
            let mut parts = Vec::new();
            for (i, b) in blocks.iter().enumerate() {
                match b {
                    Block::Text { text, .. } => parts.push(crate::types::ContentPart {
                        kind: PART_TYPE_INPUT_TEXT.to_string(),
                        text: text.clone(),
                        ..Default::default()
                    }),
                    Block::Image {
                        media_type,
                        data,
                        url,
                        ..
                    } => match crate::normalize::encode_image_block(
                        media_type.as_deref(),
                        data.as_deref(),
                        url.as_deref(),
                        &format!("{path}.output[{i}]"),
                    ) {
                        Ok(part) => parts.push(part),
                        Err(loss) => losses.push(loss),
                    },
                    _ => losses.push(loss(
                        format!("{path}.output[{i}]"),
                        "type",
                        LossReason::UnsupportedSemantic,
                        "custom tool output content is not supported",
                    )),
                }
            }
            OutputValue::Parts(parts)
        }
    }
}

fn validate_usage(usage: &Usage) -> Result<(), Error> {
    for (path, value) in [
        ("usage.input_tokens", Some(usage.input_tokens)),
        ("usage.output_tokens", Some(usage.output_tokens)),
        (
            "usage.cache_read_input_tokens",
            usage.cache_read_input_tokens,
        ),
        (
            "usage.cache_creation_input_tokens",
            usage.cache_creation_input_tokens,
        ),
        (
            "usage.input_tokens_details.cached_tokens",
            usage
                .input_tokens_details
                .map(|details| details.cached_tokens),
        ),
        (
            "usage.output_tokens_details.reasoning_tokens",
            usage
                .output_tokens_details
                .map(|details| details.reasoning_tokens),
        ),
    ] {
        if value.is_some_and(|value| value < 0) {
            return Err(Error::new(format!(
                "responses: {path} must be non-negative"
            )));
        }
    }
    Ok(())
}

fn encode_system_instructions(
    blocks: &[oxa_ir::SystemBlock],
) -> (Option<String>, Vec<crate::types::InputItem>) {
    let has_developer = blocks.iter().any(
        |block| matches!(block, oxa_ir::SystemBlock::Text { role, .. } if role == "developer"),
    );
    if !has_developer {
        let mut text = String::new();
        for block in blocks {
            let oxa_ir::SystemBlock::Text { text: value, .. } = block;
            text.push_str(value);
        }
        return (Some(text), Vec::new());
    }
    let mut index = 0;
    let instructions = if matches!(
        blocks.first(),
        Some(oxa_ir::SystemBlock::Text { role, .. }) if role == "developer"
    ) {
        None
    } else {
        let mut text = String::new();
        while index < blocks.len()
            && !matches!(
                &blocks[index],
                oxa_ir::SystemBlock::Text { role, .. } if role == "developer"
            )
        {
            let oxa_ir::SystemBlock::Text { text: value, .. } = &blocks[index];
            text.push_str(value);
            index += 1;
        }
        Some(text)
    };
    let mut items = Vec::new();
    while index < blocks.len() {
        let oxa_ir::SystemBlock::Text { text, role, .. } = &blocks[index];
        items.push(crate::types::InputItem {
            role: if role == "developer" {
                "developer".to_string()
            } else {
                "system".to_string()
            },
            content: Some(crate::types::ContentValue::Text(text.clone())),
            ..Default::default()
        });
        index += 1;
    }
    (instructions, items)
}
