//! IR → face conversions (spec/10 §4).

use oxa_ir::{
    Block, Loss, LossReason, ReasoningEffort, Request as IrRequest, Response as IrResponse,
    ResponseFormat, StopReason, Usage,
};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{
    encode_assistant_message, encode_tool_choice, encode_tool_result, encode_user_content, loss,
};
use crate::types::{
    Choice, CompletionTokensDetailsWire, ContentValue, FINISH_REASON_CONTENT_FILTER,
    FINISH_REASON_LENGTH, FINISH_REASON_STOP, FINISH_REASON_TOOL_CALLS, FunctionWire, Message,
    OBJECT_CHAT_COMPLETION, PromptTokensDetailsWire, ROLE_SYSTEM, ROLE_USER, Request, Response,
    TOOL_TYPE_FUNCTION, ToolWire, UsageWire,
};

/// Converts an IR request to a Chat Completions wire request (IR → face).
/// System content renders as one leading system message; text content remains
/// a string while image input renders as content parts.
pub fn encode_request(req: &IrRequest, config: &Config) -> Result<(Request, Vec<Loss>), Error> {
    for tool in req.tools.iter().flatten() {
        tool.validate_strict().map_err(Error::new)?;
    }
    let mut losses = Vec::new();
    if req
        .metadata
        .as_ref()
        .is_some_and(|metadata| !metadata.is_empty())
    {
        losses.push(loss(
            "metadata",
            "metadata",
            LossReason::UnmappedField,
            "Chat Completions requests have no metadata field; the IR metadata map is dropped.",
        ));
    }
    let mut out = Request {
        model: config.map_model(&req.model),
        ..Request::default()
    };
    if let Some(tools) = &req.tools {
        let mut supported = Vec::new();
        for (index, tool) in tools.iter().enumerate() {
            if tool.tool_type == Some(oxa_ir::ToolType::Custom) {
                let fmt = tool
                    .format
                    .as_ref()
                    .map(|f| crate::types::CustomFormatWire {
                        kind: f.format_type.clone(),
                        syntax: f.syntax.clone(),
                        definition: f.definition.clone(),
                    });
                supported.push(ToolWire {
                    kind: crate::types::TOOL_TYPE_CUSTOM.to_string(),
                    function: None,
                    custom: Some(crate::types::CustomToolWire {
                        name: tool.name.clone(),
                        description: tool.description.clone(),
                        format: Some(fmt.unwrap_or(crate::types::CustomFormatWire {
                            kind: "text".to_string(),
                            syntax: None,
                            definition: None,
                        })),
                    }),
                });
                continue;
            }
            if tool.tool_type == Some(oxa_ir::ToolType::Native) {
                let source = tool.source.as_deref().unwrap_or_default();
                losses.push(loss(
                    format!("tools[{index}]"),
                    "type",
                    LossReason::UnsupportedSemantic,
                    format!("native tool \"{}\" from \"{source}\" cannot be represented by the target protocol", tool.name),
                ));
                continue;
            }
            if tool.tool_type == Some(oxa_ir::ToolType::ApplyPatch) {
                losses.push(loss(
                    format!("tools[{index}]"),
                    "type",
                    LossReason::UnsupportedSemantic,
                    "apply_patch tool declarations are not implemented by this Chat Completions profile",
                ));
                continue;
            }
            supported.push(ToolWire {
                kind: TOOL_TYPE_FUNCTION.to_string(),
                function: Some(FunctionWire {
                    name: tool.name.clone(),
                    description: tool.description.clone().unwrap_or_default(),
                    parameters: non_null(
                        tool.input_schema
                            .as_ref()
                            .unwrap_or(&serde_json::Value::Null),
                    ),
                    arguments: String::new(),
                    strict: tool.strict,
                }),
                custom: None,
            });
        }
        if !supported.is_empty() {
            out.tools = Some(supported);
        }
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
            let name_retained = c
                .name
                .as_ref()
                .map(|n| {
                    out.tools.as_deref().unwrap_or(&[]).iter().any(|t| {
                        t.function.as_ref().map(|f| &f.name == n).unwrap_or(false)
                            || t.custom.as_ref().map(|cu| &cu.name == n).unwrap_or(false)
                    })
                })
                .unwrap_or(false);
            is_native && !name_retained
        })
        .unwrap_or(false);

    let (mut choice, choice_loss) = encode_tool_choice(req.tool_choice.as_ref());
    if req
        .tool_choice
        .as_ref()
        .is_some_and(|c| c.mode == oxa_ir::ToolChoiceMode::Any)
        && req.tools.as_ref().map(|ts| !ts.is_empty()).unwrap_or(false)
        && out.tools.as_ref().map(|ts| ts.is_empty()).unwrap_or(true)
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
    }
    out.tool_choice = choice;
    if let Some(loss) = choice_loss {
        losses.push(loss);
    }
    if !req.system.is_empty() {
        let mut text = String::new();
        for block in &req.system {
            match block {
                oxa_ir::SystemBlock::Text { text: value, .. } => text.push_str(value),
            }
        }
        out.messages.push(Message {
            role: ROLE_SYSTEM.to_string(),
            content: Some(ContentValue::Text(text)),
            ..Message::default()
        });
    }
    for (index, message) in req.messages.iter().enumerate() {
        match message.role {
            oxa_ir::Role::Assistant => {
                let (assistant, message_losses) = encode_assistant_message(
                    &message.content,
                    &format!("messages[{index}].content"),
                );
                out.messages.push(assistant);
                losses.extend(message_losses);
            }
            oxa_ir::Role::User => {
                let mut normal: Vec<Block> = Vec::new();
                let mut results: Vec<(usize, Block)> = Vec::new();
                let mut retained_results = 0usize;
                // N-CC-9: tool messages are hoisted ahead of the trailing user
                // content. When ordinary content precedes a tool result in the
                // source turn, that hoisting does not preserve the source order.
                let mut last_result: Option<usize> = None;
                let mut first_normal: Option<usize> = None;
                for (position, block) in message.content.iter().enumerate() {
                    if matches!(
                        block,
                        Block::ToolResult { .. }
                            | Block::CustomToolResult { .. }
                            | Block::ApplyPatchToolResult { .. }
                    ) {
                        results.push((position, block.clone()));
                        last_result = Some(position);
                        continue;
                    }
                    if first_normal.is_none() {
                        first_normal = Some(position);
                    }
                    normal.push(block.clone());
                }
                if let (Some(first), Some(last)) = (first_normal, last_result)
                    && first < last
                {
                    losses.push(loss(
                            format!("messages[{index}]"),
                            "ordering",
                            LossReason::Degraded,
                            "N-CC-9: tool messages are hoisted ahead of the trailing user content; source order is not preserved",
                        ));
                }
                // Tool messages must follow the assistant invocation
                // immediately. If the IR user turn also carries normal content,
                // render it after the tool-result run as a separate user
                // message.
                for (position, result) in &results {
                    if matches!(result, Block::ApplyPatchToolResult { .. }) {
                        losses.push(loss(
                            format!("messages[{index}].content[{position}]"),
                            "type",
                            LossReason::UnsupportedSemantic,
                            "apply_patch tool results are not implemented by this profile",
                        ));
                        continue;
                    }
                    retained_results += 1;
                    let (tool_message, result_losses) = encode_tool_result(
                        result,
                        &format!("messages[{index}].content[{position}]"),
                    );
                    out.messages.push(tool_message);
                    losses.extend(result_losses);
                }
                if !normal.is_empty() || retained_results == 0 {
                    let (content, content_losses) =
                        encode_user_content(&normal, &format!("messages[{index}].content"));
                    out.messages.push(Message {
                        role: ROLE_USER.to_string(),
                        content: Some(content),
                        ..Message::default()
                    });
                    losses.extend(content_losses);
                }
            }
        }
    }
    if let Some(params) = &req.params {
        out.temperature = params.temperature;
        out.top_p = params.top_p;
        out.max_tokens = params.max_tokens;
        let stop = params
            .stop_sequences
            .clone()
            .filter(|stops| !stops.is_empty());
        out.stop = stop;
        out.reasoning_effort = params.reasoning_effort.map(|effort| {
            match effort {
                ReasoningEffort::Minimal => "minimal",
                ReasoningEffort::Low => "low",
                ReasoningEffort::Medium => "medium",
                ReasoningEffort::High => "high",
                ReasoningEffort::XHigh => "xhigh",
                ReasoningEffort::Max => "max",
            }
            .to_string()
        });
        if let Some(rf) = &params.response_format {
            if matches!(rf, ResponseFormat::JsonSchema { name, .. } if name.is_empty()) {
                losses.push(loss(
                    "params.response_format",
                    "name",
                    LossReason::UnsupportedSemantic,
                    "Chat Completions requires a non-empty schema name",
                ));
            } else {
                out.response_format = Some(encode_response_format(rf));
            }
        }
    }
    let has_max_completion_tokens = req
        .native
        .iter()
        .any(|n| n.source == "openai" && n.kind == oxa_ir::NATIVE_KIND_MAX_COMPLETION_TOKENS);
    if has_max_completion_tokens {
        out.max_completion_tokens = out.max_tokens.take();
    }
    for n in &req.native {
        if n.source == "openai" {
            match n.kind.as_str() {
                oxa_ir::NATIVE_KIND_STORE => {
                    if n.value == "null" {
                        out.store = Some(serde_json::Value::Null);
                    } else if let Ok(b) = serde_json::from_str::<bool>(&n.value) {
                        out.store = Some(serde_json::Value::Bool(b));
                    }
                }
                oxa_ir::NATIVE_KIND_STREAM => {
                    if n.value == "null" {
                        out.stream = Some(serde_json::Value::Null);
                    } else if let Ok(b) = serde_json::from_str::<bool>(&n.value) {
                        out.stream = Some(serde_json::Value::Bool(b));
                    }
                }
                oxa_ir::NATIVE_KIND_STREAM_OPTIONS => {
                    if n.value == "null" {
                        out.stream_options = Some(serde_json::Value::Null);
                    } else if let Ok(val) = serde_json::from_str::<serde_json::Value>(&n.value) {
                        out.stream_options = Some(val);
                    }
                }
                _ => {}
            }
        }
    }
    let omission_losses = oxa_ir::omitted_native_request_losses(req, "chatcompletions")
        .map_err(|e| Error::new(e.to_string()))?;
    losses.extend(omission_losses);
    Ok((out, losses))
}

fn encode_response_format(rf: &ResponseFormat) -> serde_json::Value {
    match rf {
        ResponseFormat::Text => serde_json::json!({ "type": "text" }),
        ResponseFormat::JsonObject => serde_json::json!({ "type": "json_object" }),
        ResponseFormat::JsonSchema {
            name,
            description,
            schema,
            strict,
            ..
        } => {
            let mut inner = serde_json::Map::new();
            inner.insert("name".to_string(), serde_json::Value::String(name.clone()));
            if let Some(desc) = description {
                inner.insert(
                    "description".to_string(),
                    serde_json::Value::String(desc.clone()),
                );
            }
            inner.insert(
                "schema".to_string(),
                serde_json::Value::Object(schema.clone()),
            );
            if let Some(s) = strict {
                inner.insert("strict".to_string(), serde_json::Value::Bool(*s));
            }
            serde_json::json!({
                "type": "json_schema",
                "json_schema": serde_json::Value::Object(inner)
            })
        }
    }
}

fn non_null(value: &serde_json::Value) -> Option<serde_json::Value> {
    if value.is_null() {
        None
    } else {
        Some(value.clone())
    }
}

/// Converts an IR response to a Chat Completions wire response (IR → face).
/// Envelope fields absent from the IR are rendered with the documented
/// defaults (object "chat.completion", created 0, single choice index 0,
/// message role assistant) and record no loss. usage.total_tokens is derived
/// and recomputed.
pub fn encode_response(resp: &IrResponse, config: &Config) -> Result<(Response, Vec<Loss>), Error> {
    validate_usage(&resp.usage)?;
    let total_tokens = resp
        .usage
        .input_tokens
        .checked_add(resp.usage.output_tokens)
        .ok_or_else(|| Error::new("chatcompletions: derived total_tokens exceeds int64"))?;
    let (message, mut losses) = encode_assistant_message(&resp.content, "content");
    let has_native_call = resp
        .content
        .iter()
        .any(|b| matches!(b, Block::ToolUse { .. } | Block::CustomToolUse { .. }));
    let has_unsupported = resp
        .content
        .iter()
        .any(|b| matches!(b, Block::ApplyPatchToolUse { .. }));
    let effective_stop =
        if resp.stop_reason == StopReason::ToolUse && has_unsupported && !has_native_call {
            losses.push(loss(
                "stop_reason",
                "stop_reason",
                LossReason::Degraded,
                "apply_patch calls were dropped; tool_use normalized to end_turn",
            ));
            StopReason::EndTurn
        } else {
            resp.stop_reason
        };
    let (finish, finish_loss) = encode_finish_reason(effective_stop)?;
    if let Some(l) = finish_loss {
        losses.push(l);
    }
    Ok((
        Response {
            id: resp.id.clone(),
            object: OBJECT_CHAT_COMPLETION.to_string(),
            created: 0,
            model: config.map_model(&resp.model),
            choices: vec![Choice {
                index: 0,
                message,
                finish_reason: finish.to_string(),
            }],
            usage: Some(UsageWire {
                prompt_tokens: resp.usage.input_tokens,
                completion_tokens: resp.usage.output_tokens,
                total_tokens,
                prompt_tokens_details: resp.usage.input_tokens_details.map(|details| {
                    PromptTokensDetailsWire {
                        cached_tokens: details.cached_tokens,
                    }
                }),
                completion_tokens_details: resp.usage.output_tokens_details.map(|details| {
                    CompletionTokensDetailsWire {
                        reasoning_tokens: details.reasoning_tokens,
                    }
                }),
            }),
        },
        losses,
    ))
}

pub(crate) fn validate_usage(usage: &Usage) -> Result<(), Error> {
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
                "chatcompletions: {path} must be non-negative"
            )));
        }
    }
    Ok(())
}

pub(crate) fn encode_finish_reason(
    stop: StopReason,
) -> Result<(&'static str, Option<Loss>), Error> {
    match stop {
        StopReason::EndTurn => Ok((FINISH_REASON_STOP, None)),
        StopReason::MaxTokens => Ok((FINISH_REASON_LENGTH, None)),
        StopReason::Refusal => Ok((FINISH_REASON_CONTENT_FILTER, None)),
        StopReason::ToolUse => Ok((FINISH_REASON_TOOL_CALLS, None)),
        StopReason::StopSequence => Ok((
            FINISH_REASON_STOP,
            Some(loss(
                "",
                "stop_sequence",
                LossReason::UnmappedValue,
                "Chat Completions finish_reason \"stop\" does not identify the matched stop sequence",
            )),
        )),
        StopReason::Other => Err(Error::new(
            "chatcompletions: stop reason \"other\" has no Chat Completions equivalent",
        )),
    }
}
