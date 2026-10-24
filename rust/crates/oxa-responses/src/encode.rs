//! IR → face conversions (spec/11 §4).

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
    let mut losses = Vec::new();
    for (message_index, message) in req.messages.iter().enumerate() {
        for (block_index, block) in message.content.iter().enumerate() {
            match block {
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
    if req
        .metadata
        .as_ref()
        .is_some_and(|metadata| !metadata.is_empty())
    {
        losses.push(loss(
            "metadata",
            "metadata",
            LossReason::UnmappedField,
            "Responses requests have a string-valued metadata field with no IR equivalent; the IR metadata map is dropped.",
        ));
    }
    let mut out = Request {
        model: config.map_model(&req.model),
        ..Request::default()
    };
    if !req.system.is_empty() {
        let mut instructions = String::new();
        for block in &req.system {
            match block {
                oxa_ir::SystemBlock::Text { text, .. } => instructions.push_str(text),
            }
        }
        out.instructions = Some(instructions);
    }
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

    let mut items = Vec::new();
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
            let format_val = match rf {
                ResponseFormat::Text => serde_json::json!({ "type": "text" }),
                ResponseFormat::JsonObject => serde_json::json!({ "type": "json_object" }),
                ResponseFormat::JsonSchema {
                    name,
                    description,
                    schema,
                    strict,
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
                verbosity: None,
                format: Some(format_val),
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
    let omission_losses = oxa_ir::omitted_native_request_losses(req, "responses")
        .map_err(|e| Error::new(e.to_string()))?;
    losses.extend(omission_losses);
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
    let mut text = String::new();
    let mut has_text = false;
    let mut calls = Vec::new();
    let mut apc_index = 0usize;
    let mut apco_index = 0usize;
    for (index, block) in resp.content.iter().enumerate() {
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
                        format!("content[{index}].signature"),
                        "signature",
                        LossReason::UnmappedField,
                        "Responses reasoning output items carry no provider signature",
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
        }
    }
    let mut output = calls;
    if has_text || resp.content.is_empty() {
        output.insert(
            0,
            OutputItem {
                kind: ITEM_TYPE_MESSAGE.to_string(),
                id: "msg_abc123".to_string(),
                status: STATUS_COMPLETED.to_string(),
                role: ROLE_ASSISTANT.to_string(),
                content: vec![OutputPart {
                    kind: PART_TYPE_OUTPUT_TEXT.to_string(),
                    text,
                    annotations: Vec::new(),
                }],
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
                message: String::new(),
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
                    format!("native tool \"{}\" from \"{source}\" cannot be represented by the target protocol", tool.name),
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

        let mut container_kind = String::new();
        for ann in &tool.native {
            if ann.source == "openai" && ann.kind == "container" {
                let raw_val = &ann.value;
                if let Ok(s) = serde_json::from_str::<String>(raw_val) {
                    container_kind = s;
                } else {
                    container_kind = raw_val.trim_matches('"').to_string();
                }
                break;
            }
        }

        let dot_idx = tool.name.find('.');
        if let Some(idx) = dot_idx {
            let prefix = &tool.name[..idx];
            if container_kind.is_empty() {
                container_kind = "namespace".to_string();
            }
            let mut child_tools = Vec::new();
            let mut j = i;
            while j < tools.len() && tools[j].name.starts_with(&format!("{prefix}.")) {
                let sub_tool = &tools[j];
                let stripped_name = &sub_tool.name[prefix.len() + 1..];
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
                    if has_defer {
                        m.insert("defer_loading".into(), serde_json::Value::Bool(true));
                    }
                    child_tools.push(serde_json::Value::Object(m));
                }
                j += 1;
            }
            out.push(ToolDef {
                kind: container_kind,
                name: prefix.to_string(),
                tools: Some(child_tools),
                ..ToolDef::default()
            });
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
                        image_url: String::new(),
                        detail: None,
                        file_id: None,
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
