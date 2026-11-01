//! Face → IR conversions (spec/10 §4).

use serde_json::Value;

use oxa_ir::{
    Block, InputTokensDetails, Loss, LossReason, OutputTokensDetails, Params, ReasoningEffort,
    Request as IrRequest, Response as IrResponse, ResponseFormat, Role, StopReason, ToolChoice,
    ToolChoiceMode, Usage,
};

use std::collections::{BTreeMap, BTreeSet};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{
    content_system, decode_content, decode_tool_calls, decode_tool_choice, decode_tool_result_run,
    loss,
};
use crate::types::{
    ContentValue, FINISH_REASON_CONTENT_FILTER, FINISH_REASON_LENGTH, FINISH_REASON_STOP,
    FINISH_REASON_TOOL_CALLS, ROLE_ASSISTANT, ROLE_SYSTEM, ROLE_TOOL, ROLE_USER, Request, Response,
    TOOL_TYPE_CUSTOM, TOOL_TYPE_FUNCTION,
};

/// Converts a Chat Completions wire request to the IR (face → IR). Semantic
/// unmappables are losses, never errors; errors are reserved for structural
/// type violations of known fields (spec/02 §4).
pub fn decode_request(wire: &Request, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    for (name, present, detail) in [
        (
            "functions",
            wire.functions.is_some(),
            "legacy Chat Completions functions have no IR equivalent in v1.",
        ),
        (
            "function_call",
            wire.function_call.is_some(),
            "legacy Chat Completions function_call has no IR equivalent in v1.",
        ),
        (
            "logprobs",
            wire.logprobs.is_some(),
            "Chat Completions log-probability sampling has no IR equivalent in v1.",
        ),
        (
            "top_logprobs",
            wire.top_logprobs.is_some(),
            "Chat Completions log-probability sampling has no IR equivalent in v1.",
        ),
    ] {
        if present {
            losses.push(loss(name, name, LossReason::UnmappedField, detail));
        }
    }

    let mut req = IrRequest {
        model: config.map_model(&wire.model),
        system: Vec::new(),
        messages: Vec::new(),
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
        native: Vec::new(),
    };
    if let Some(value) = &wire.metadata {
        req.metadata = string_metadata(value, "chatcompletions")?;
    }
    if let Some(wire_tools) = &wire.tools {
        let mut tools = Vec::with_capacity(wire_tools.len());
        for (index, tool) in wire_tools.iter().enumerate() {
            if tool.kind == TOOL_TYPE_CUSTOM
                && let Some(custom) = &tool.custom
            {
                let mut fmt: Option<oxa_ir::CustomToolFormat> = None;
                if let Some(f) = &custom.format {
                    if f.kind == "grammar" {
                        if f.syntax.as_deref() == Some("lark")
                            || f.syntax.as_deref() == Some("regex")
                        {
                            fmt = Some(oxa_ir::CustomToolFormat {
                                format_type: "grammar".to_string(),
                                syntax: f.syntax.clone(),
                                definition: f.definition.clone(),
                            });
                        } else {
                            losses.push(loss(
                                format!("tools[{index}].custom.format"),
                                "syntax",
                                LossReason::UnsupportedSemantic,
                                format!("unknown custom grammar syntax {:?}", f.syntax),
                            ));
                        }
                    } else if f.kind == "text" {
                        fmt = Some(oxa_ir::CustomToolFormat {
                            format_type: "text".to_string(),
                            syntax: None,
                            definition: None,
                        });
                    } else {
                        losses.push(loss(
                            format!("tools[{index}].custom.format"),
                            "type",
                            LossReason::UnsupportedSemantic,
                            format!("unknown custom format {:?}", f.kind),
                        ));
                    }
                }
                tools.push(oxa_ir::Tool {
                    tool_type: Some(oxa_ir::ToolType::Custom),
                    name: custom.name.clone(),
                    description: custom.description.clone(),
                    input_schema: None,
                    strict: None,
                    format: fmt,
                    native: Vec::new(),
                    source: None,
                    definition: None,
                });
                continue;
            }
            if tool.kind != TOOL_TYPE_FUNCTION {
                losses.push(loss(
                    format!("tools[{index}]"),
                    "type",
                    LossReason::UnsupportedSemantic,
                    format!(
                        "Chat Completions tool type {:?} has no IR equivalent",
                        tool.kind
                    ),
                ));
                continue;
            }
            let func = tool.function.clone().unwrap_or_default();
            tools.push(oxa_ir::Tool {
                tool_type: None,
                name: func.name,
                description: non_empty(func.description),
                input_schema: Some(func.parameters.unwrap_or(Value::Null)),
                strict: func.strict,
                format: None,
                native: Vec::new(),
                source: None,
                definition: None,
            });
        }
        if !tools.is_empty() {
            req.tools = Some(tools);
        }
    }
    let (mut choice, choice_losses) = decode_tool_choice(wire.tool_choice.as_ref());
    if let Some(val) = &wire.parallel_tool_calls {
        let Some(ptc) = val.as_bool() else {
            return Err(Error::new(
                "chatcompletions: parallel_tool_calls must be a boolean",
            ));
        };
        let dp = !ptc;
        match choice.as_mut() {
            Some(tc) => tc.disable_parallel = Some(dp),
            None => {
                choice = Some(ToolChoice {
                    mode: ToolChoiceMode::Auto,
                    name: None,
                    tool_type: None,
                    disable_parallel: Some(dp),
                });
            }
        }
    }
    req.tool_choice = choice;
    losses.extend(choice_losses);

    let mut custom_call_ids = BTreeSet::new();
    for msg in &wire.messages {
        if msg.role == ROLE_ASSISTANT
            && let Some(calls) = &msg.tool_calls
        {
            for c in calls {
                if c.kind == TOOL_TYPE_CUSTOM {
                    custom_call_ids.insert(c.id.clone());
                }
            }
        }
    }

    let mut system: Vec<oxa_ir::SystemBlock> = Vec::new();
    let mut messages: Vec<oxa_ir::Message> = Vec::new();
    let mut index = 0usize;
    while index < wire.messages.len() {
        let message = &wire.messages[index];
        if message.role == ROLE_TOOL {
            let (merged, next, result_losses) =
                decode_tool_result_run(&wire.messages, index, &custom_call_ids)?;
            messages.push(merged);
            losses.extend(result_losses);
            index = next;
            continue;
        }

        let is_user = message.role == ROLE_USER;
        let content_path = format!("messages[{index}].content");
        let (mut content, content_losses) =
            decode_content(message.content.as_ref(), &content_path, is_user)?;
        losses.extend(content_losses);
        if message.role == ROLE_ASSISTANT {
            if let Some(reasoning) = &message.reasoning_content
                && !reasoning.is_empty()
            {
                content.insert(
                    0,
                    Block::Thinking {
                        thinking: reasoning.clone(),
                        signature: None,
                        native: Vec::new(),
                    },
                );
            }
        } else if message.reasoning_content.is_some() {
            losses.push(loss(
                format!("messages[{index}].reasoning_content"),
                "reasoning_content",
                LossReason::UnmappedField,
                "Chat Completions reasoning_content is defined only for assistant messages",
            ));
        }
        if message.role != ROLE_SYSTEM && message.role != "developer" && content.is_empty() {
            // IR conversation messages cannot have empty content (spec/01 §3.3).
            content.push(Block::Text {
                text: String::new(),
                citations: None,
                native: Vec::new(),
            });
        }
        match message.role.as_str() {
            ROLE_SYSTEM | "developer" => {
                let (mut system_blocks, system_losses) = content_system(content, &content_path);
                if message.role == "developer" {
                    for block in &mut system_blocks {
                        let oxa_ir::SystemBlock::Text { role, .. } = block;
                        *role = "developer".to_string();
                    }
                }
                system.extend(system_blocks);
                losses.extend(system_losses);
            }
            ROLE_USER => messages.push(oxa_ir::Message {
                role: Role::User,
                content,
            }),
            ROLE_ASSISTANT => {
                let default_calls = Vec::new();
                let calls = message.tool_calls.as_deref().unwrap_or(&default_calls);
                let (tool_blocks, tool_losses) =
                    decode_tool_calls(calls, &format!("messages[{index}].tool_calls"));
                let has_refusal = message.refusal.as_deref().is_some_and(|r| !r.is_empty());
                if (message.content.is_none()
                    || matches!(message.content, Some(ContentValue::Null(_))))
                    && (!tool_blocks.is_empty() || has_refusal)
                {
                    // Drop only the empty placeholder; reasoning blocks remain.
                    content.retain(
                        |block| !matches!(block, Block::Text { text, .. } if text.is_empty()),
                    );
                }
                content.extend(tool_blocks);
                if let Some(ref r) = message.refusal
                    && !r.is_empty()
                {
                    content.push(Block::Refusal {
                        refusal: r.clone(),
                        category: None,
                        native: Vec::new(),
                    });
                }
                messages.push(oxa_ir::Message {
                    role: Role::Assistant,
                    content,
                });
                losses.extend(tool_losses);
            }
            other => {
                return Err(Error::new(format!(
                    "chatcompletions: messages[{index}]: unknown role {other:?}"
                )));
            }
        }
        if message.function_call.is_some() {
            losses.push(loss(
                format!("messages[{index}].function_call"),
                "function_call",
                LossReason::UnmappedField,
                "legacy Chat Completions function_call has no IR equivalent",
            ));
        }
        index += 1;
    }
    if messages.is_empty() {
        return Err(Error::new(
            "chatcompletions: request carries no conversation messages",
        ));
    }
    req.system = system;
    req.messages = messages;
    if wire.max_tokens.is_some() && wire.max_completion_tokens.is_some() {
        return Err(Error::new(
            "chatcompletions: cannot specify both max_tokens and max_completion_tokens",
        ));
    }
    if let Some(mct) = wire.max_completion_tokens
        && mct <= 0
    {
        return Err(Error::new(
            "chatcompletions: max_completion_tokens must be a positive integer",
        ));
    }
    let mut native = Vec::new();
    let max_tokens = if let Some(mct) = wire.max_completion_tokens {
        native.push(oxa_ir::NativeAnnotation {
            source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
            kind: oxa_ir::NATIVE_KIND_MAX_COMPLETION_TOKENS.into(),
            value: "true".into(),
            omission: None,
            projection: None,
        });
        Some(mct)
    } else {
        wire.max_tokens
    };
    if let Some(ref store_val) = wire.store {
        match store_val {
            Value::Bool(b) => {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_STORE.into(),
                    value: b.to_string(),
                    omission: None,
                    projection: None,
                });
            }
            Value::Null => {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_STORE.into(),
                    value: "null".into(),
                    omission: None,
                    projection: None,
                });
            }
            _ => {
                return Err(Error::new(
                    "chatcompletions: store must be a boolean or null",
                ));
            }
        }
    }
    if let Some(ref stream_val) = wire.stream {
        match stream_val {
            Value::Bool(b) => {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_STREAM.into(),
                    value: b.to_string(),
                    omission: None,
                    projection: None,
                });
            }
            Value::Null => {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_STREAM.into(),
                    value: "null".into(),
                    omission: None,
                    projection: None,
                });
            }
            _ => {
                return Err(Error::new(
                    "chatcompletions: stream must be a boolean or null",
                ));
            }
        }
    }
    if let Some(ref so_val) = wire.stream_options {
        match so_val {
            Value::Object(map) => {
                if let Some(u) = map.get("include_usage")
                    && !u.is_boolean()
                {
                    return Err(Error::new(
                        "chatcompletions: stream_options.include_usage must be a boolean",
                    ));
                }
                if let Some(ob) = map.get("include_obfuscation")
                    && !ob.is_boolean()
                {
                    return Err(Error::new(
                        "chatcompletions: stream_options.include_obfuscation must be a boolean",
                    ));
                }
                let mut clean_so = serde_json::Map::new();
                let mut has_usage_true = false;
                if let Some(Value::Bool(ob)) = map.get("include_obfuscation") {
                    clean_so.insert("include_obfuscation".into(), Value::Bool(*ob));
                }
                if let Some(Value::Bool(u)) = map.get("include_usage") {
                    clean_so.insert("include_usage".into(), Value::Bool(*u));
                    if *u {
                        has_usage_true = true;
                    }
                }
                let val = serde_json::to_string(&clean_so).unwrap_or_default();
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_STREAM_OPTIONS.into(),
                    value: val,
                    omission: if has_usage_true {
                        Some(oxa_ir::NativeOmission {
                            field: "stream_options".into(),
                            reason: LossReason::Degraded,
                            detail: Some(oxa_ir::STREAM_OPTIONS_LOSS_DETAIL.into()),
                        })
                    } else {
                        None
                    },
                    projection: None,
                });
            }
            Value::Null => {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_STREAM_OPTIONS.into(),
                    value: "null".into(),
                    omission: None,
                    projection: None,
                });
            }
            _ => {
                return Err(Error::new(
                    "chatcompletions: stream_options must be an object or null",
                ));
            }
        }
    }
    if let Some(val) = &wire.prompt_cache_key {
        let Some(pck) = val.as_str() else {
            return Err(Error::new(
                "chatcompletions: prompt_cache_key must be a string",
            ));
        };
        native.push(oxa_ir::NativeAnnotation {
            source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
            kind: oxa_ir::NATIVE_KIND_PROMPT_CACHE_KEY.into(),
            value: serde_json::to_string(pck).map_err(|e| Error::new(e.to_string()))?,
            omission: Some(oxa_ir::NativeOmission {
                field: "prompt_cache_key".into(),
                reason: LossReason::UnmappedField,
                detail: Some(oxa_ir::PROMPT_CACHE_KEY_LOSS_DETAIL.into()),
            }),
            projection: None,
        });
    }
    if let Some(val) = &wire.prompt_cache_options {
        match val {
            Value::Object(map) => {
                if let Some(m) = map.get("mode") {
                    let Some(s) = m.as_str() else {
                        return Err(Error::new(
                            "chatcompletions: prompt_cache_options.mode must be a string",
                        ));
                    };
                    if s != "implicit" && s != "explicit" {
                        return Err(Error::new(
                            "chatcompletions: prompt_cache_options.mode must be 'implicit' or 'explicit'",
                        ));
                    }
                }
                if let Some(t) = map.get("ttl") {
                    let Some(s) = t.as_str() else {
                        return Err(Error::new(
                            "chatcompletions: prompt_cache_options.ttl must be a string",
                        ));
                    };
                    if s != "30m" {
                        return Err(Error::new(
                            "chatcompletions: prompt_cache_options.ttl must be '30m'",
                        ));
                    }
                }
                let val_str = serde_json::to_string(map).unwrap_or_default();
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_PROMPT_CACHE_OPTIONS.into(),
                    value: val_str,
                    omission: Some(oxa_ir::NativeOmission {
                        field: "prompt_cache_options".into(),
                        reason: LossReason::UnmappedField,
                        detail: Some(oxa_ir::PROMPT_CACHE_OPTIONS_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                });
            }
            Value::Null => {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_OPENAI.into(),
                    kind: oxa_ir::NATIVE_KIND_PROMPT_CACHE_OPTIONS.into(),
                    value: "null".into(),
                    omission: Some(oxa_ir::NativeOmission {
                        field: "prompt_cache_options".into(),
                        reason: LossReason::UnmappedField,
                        detail: Some(oxa_ir::PROMPT_CACHE_OPTIONS_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                });
            }
            _ => {
                return Err(Error::new(
                    "chatcompletions: prompt_cache_options must be an object or null",
                ));
            }
        }
    }
    req.native = native;
    let stop = wire.stop.clone().filter(|stops| !stops.is_empty());
    let response_format = decode_response_format(wire.response_format.as_ref(), &mut losses)?;
    let verbosity = match wire.verbosity.as_deref() {
        None => None,
        Some(value @ ("low" | "medium" | "high")) => Some(value.to_string()),
        Some(_) => {
            losses.push(loss(
                "verbosity",
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
        max_tokens,
        stop_sequences: stop,
        reasoning_effort: decode_reasoning_effort(wire.reasoning_effort.as_deref(), &mut losses),
        verbosity,
        response_format,
    };
    let params_set = params.temperature.is_some()
        || params.top_p.is_some()
        || params.max_tokens.is_some()
        || params.stop_sequences.is_some()
        || params.reasoning_effort.is_some()
        || params.verbosity.is_some()
        || params.response_format.is_some();
    if params_set {
        req.params = Some(params);
    }
    Ok((req, losses))
}

fn decode_response_format(
    value: Option<&serde_json::Value>,
    losses: &mut Vec<Loss>,
) -> Result<Option<ResponseFormat>, Error> {
    let Some(value) = value else {
        return Ok(None);
    };
    let serde_json::Value::Object(map) = value else {
        return Err(Error::new(
            "chatcompletions: response_format must be an object",
        ));
    };
    let Some(serde_json::Value::String(type_str)) = map.get("type") else {
        return Err(Error::new(
            "chatcompletions: response_format requires string type",
        ));
    };
    match type_str.as_str() {
        "text" => Ok(Some(ResponseFormat::Text)),
        "json_object" => Ok(Some(ResponseFormat::JsonObject)),
        "json_schema" => {
            let Some(serde_json::Value::Object(schema_obj)) = map.get("json_schema") else {
                return Err(Error::new(
                    "chatcompletions: response_format of type json_schema requires json_schema object",
                ));
            };
            let Some(serde_json::Value::String(name)) = schema_obj.get("name") else {
                return Err(Error::new(
                    "chatcompletions: response_format.json_schema requires string name",
                ));
            };
            if name.is_empty() {
                return Err(Error::new(
                    "chatcompletions: response_format.json_schema name must not be empty",
                ));
            }
            let Some(schema) = schema_obj.get("schema") else {
                return Err(Error::new(
                    "chatcompletions: response_format.json_schema requires schema",
                ));
            };
            if !schema.is_object() {
                return Err(Error::new(
                    "chatcompletions: response_format.json_schema schema must be an object",
                ));
            }
            let description = match schema_obj.get("description") {
                Some(value) => Some(
                    value
                        .as_str()
                        .ok_or_else(|| Error::new("chatcompletions: response_format.json_schema description must be a string"))?
                        .to_string(),
                ),
                None => None,
            };
            let strict = match schema_obj.get("strict") {
                Some(value) if value.is_boolean() => Some(value.as_bool().unwrap_or(false)),
                Some(_) => {
                    return Err(Error::new(
                        "chatcompletions: response_format.json_schema strict must be a boolean",
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
                "response_format.type",
                "type",
                LossReason::UnmappedValue,
                format!("unknown response_format type {other:?}"),
            ));
            Ok(None)
        }
    }
}

fn decode_reasoning_effort(value: Option<&str>, losses: &mut Vec<Loss>) -> Option<ReasoningEffort> {
    match value {
        Some("minimal") => Some(ReasoningEffort::Minimal),
        Some("low") => Some(ReasoningEffort::Low),
        Some("medium") => Some(ReasoningEffort::Medium),
        Some("high") => Some(ReasoningEffort::High),
        Some("xhigh") => Some(ReasoningEffort::XHigh),
        Some("max") => Some(ReasoningEffort::Max),
        Some(other) => {
            losses.push(loss(
                "reasoning_effort",
                "reasoning_effort",
                LossReason::UnmappedValue,
                format!("Chat Completions reasoning_effort {other:?} has no IR equivalent"),
            ));
            None
        }
        None => None,
    }
}

pub(crate) fn decode_usage(wire: Option<&crate::types::UsageWire>) -> Result<Usage, Error> {
    let Some(wire) = wire else {
        return Ok(Usage::default());
    };
    Ok(Usage {
        input_tokens: nonnegative_usage(wire.prompt_tokens, "usage.prompt_tokens")?,
        output_tokens: nonnegative_usage(wire.completion_tokens, "usage.completion_tokens")?,
        input_tokens_details: wire
            .prompt_tokens_details
            .as_ref()
            .map(|details| {
                nonnegative_usage(
                    details.cached_tokens,
                    "usage.prompt_tokens_details.cached_tokens",
                )
                .map(|cached_tokens| InputTokensDetails { cached_tokens })
            })
            .transpose()?,
        output_tokens_details: wire
            .completion_tokens_details
            .as_ref()
            .map(|details| {
                nonnegative_usage(
                    details.reasoning_tokens,
                    "usage.completion_tokens_details.reasoning_tokens",
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
            "chatcompletions: {path} must be non-negative"
        )));
    }
    Ok(value)
}

fn non_empty(value: String) -> Option<String> {
    if value.is_empty() { None } else { Some(value) }
}

/// Converts a Chat Completions wire response to the IR (face → IR). Envelope
/// fields (object, created, choices[].index, message.role) are exempt from
/// losses (vectors/README.md loss conventions); usage.total_tokens is
/// derived. Unknown finish_reason values map to other plus an unmapped-value
/// loss (spec/01 §4.1).
pub fn decode_response(wire: &Response, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    if wire.choices.is_empty() {
        return Err(Error::new("chatcompletions: response carries no choices"));
    }
    let mut losses = Vec::new();
    let choice = &wire.choices[0];
    let (mut blocks, content_losses) = decode_content(
        choice.message.content.as_ref(),
        "choices[0].message.content",
        false,
    )?;
    losses.extend(content_losses);
    if choice.message.role == ROLE_ASSISTANT {
        if let Some(reasoning) = &choice.message.reasoning_content
            && !reasoning.is_empty()
        {
            blocks.insert(
                0,
                Block::Thinking {
                    thinking: reasoning.clone(),
                    signature: None,
                    native: Vec::new(),
                },
            );
        }
    } else if choice.message.reasoning_content.is_some() {
        losses.push(loss(
            "choices[0].message.reasoning_content",
            "reasoning_content",
            LossReason::UnmappedField,
            "Chat Completions reasoning_content is defined only for assistant messages",
        ));
    }
    let default_calls = Vec::new();
    let calls = choice
        .message
        .tool_calls
        .as_deref()
        .unwrap_or(&default_calls);
    let (tool_blocks, tool_losses) = decode_tool_calls(calls, "choices[0].message.tool_calls");
    let has_refusal = choice
        .message
        .refusal
        .as_deref()
        .is_some_and(|r| !r.is_empty());
    if (choice.message.content.is_none()
        || matches!(choice.message.content, Some(ContentValue::Null(_))))
        && (!tool_blocks.is_empty() || has_refusal)
    {
        // Drop only the empty placeholder; reasoning blocks remain.
        blocks.retain(|block| !matches!(block, Block::Text { text, .. } if text.is_empty()));
    }
    blocks.extend(tool_blocks);
    losses.extend(tool_losses);
    if let Some(ref r) = choice.message.refusal
        && !r.is_empty()
    {
        blocks.push(Block::Refusal {
            refusal: r.clone(),
            category: None,
            native: Vec::new(),
        });
    }
    if choice.message.function_call.is_some() {
        losses.push(loss(
            "choices[0].message.function_call",
            "function_call",
            LossReason::UnmappedField,
            "legacy Chat Completions function_call has no IR equivalent",
        ));
    }
    let (stop, finish_loss) = decode_finish_reason(&choice.finish_reason)?;
    if let Some(finish_loss) = finish_loss {
        losses.push(finish_loss);
    }
    let resp = IrResponse {
        id: wire.id.clone(),
        model: config.map_model(&wire.model),
        content: blocks,
        stop_reason: stop,
        stop_sequence: None,
        usage: decode_usage(wire.usage.as_ref())?,
    };
    Ok((resp, losses))
}

pub(crate) fn decode_finish_reason(finish: &str) -> Result<(StopReason, Option<Loss>), Error> {
    match finish {
        FINISH_REASON_STOP => Ok((StopReason::EndTurn, None)),
        FINISH_REASON_LENGTH => Ok((StopReason::MaxTokens, None)),
        FINISH_REASON_CONTENT_FILTER => Ok((StopReason::Refusal, None)),
        FINISH_REASON_TOOL_CALLS => Ok((StopReason::ToolUse, None)),
        "" => Err(Error::new(
            "chatcompletions: choices[0].finish_reason is missing",
        )),
        other => Ok((
            StopReason::Other,
            Some(loss(
                "choices[0].finish_reason",
                "finish_reason",
                LossReason::UnmappedValue,
                format!("Chat Completions finish_reason {other:?} has no IR equivalent"),
            )),
        )),
    }
}

fn string_metadata(value: &Value, face: &str) -> Result<Option<BTreeMap<String, String>>, Error> {
    if value.is_null() {
        return Ok(None);
    }
    let Some(object) = value.as_object() else {
        return Err(Error::new(format!("{face}: metadata must be an object")));
    };
    let mut out = BTreeMap::new();
    for (key, item) in object {
        if key.is_empty() {
            return Err(Error::new(format!("{face}: metadata key cannot be empty")));
        }
        let Some(text) = item.as_str() else {
            return Err(Error::new(format!(
                "{face}: metadata.{key} must be a string"
            )));
        };
        out.insert(key.clone(), text.to_string());
    }
    if out.is_empty() {
        Ok(None)
    } else {
        Ok(Some(out))
    }
}
