//! Face → IR conversions (spec/10 §4).

use serde_json::Value;

use oxa_ir::{
    Block, Loss, LossReason, Params, Request as IrRequest, Response as IrResponse, Role,
    StopReason, Usage,
};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{
    content_system, decode_content, decode_tool_calls, decode_tool_choice, decode_tool_result_run,
    loss,
};
use crate::types::{Request, Response};

/// Converts a Chat Completions wire request to the IR (face → IR). Semantic
/// unmappables are losses, never errors; errors are reserved for structural
/// type violations of known fields (spec/02 §4).
pub fn decode_request(wire: &Request, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    for (name, present, detail) in [
        (
            "parallel_tool_calls",
            wire.parallel_tool_calls.is_some(),
            "Chat Completions parallel tool calls have no IR equivalent in v1.",
        ),
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
            "response_format",
            wire.response_format.is_some(),
            "Chat Completions response_format has no IR equivalent in v1.",
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
        (
            "metadata",
            wire.metadata.is_some(),
            "Chat Completions request metadata has no IR equivalent in v1.",
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
    };
    if let Some(wire_tools) = &wire.tools {
        let mut tools = Vec::with_capacity(wire_tools.len());
        for (index, tool) in wire_tools.iter().enumerate() {
            if tool.kind != "function" {
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
            tools.push(oxa_ir::Tool {
                name: tool.function.name.clone(),
                description: non_empty(tool.function.description.clone()),
                input_schema: tool.function.parameters.clone().unwrap_or(Value::Null),
            });
        }
        if !tools.is_empty() {
            req.tools = Some(tools);
        }
    }
    let (choice, choice_losses) = decode_tool_choice(wire.tool_choice.as_ref());
    req.tool_choice = choice;
    losses.extend(choice_losses);

    let mut system: Vec<oxa_ir::SystemBlock> = Vec::new();
    let mut messages: Vec<oxa_ir::Message> = Vec::new();
    let mut index = 0usize;
    while index < wire.messages.len() {
        let message = &wire.messages[index];
        if message.role == "tool" {
            let (merged, next, result_losses) = decode_tool_result_run(&wire.messages, index)?;
            messages.push(merged);
            losses.extend(result_losses);
            index = next;
            continue;
        }

        let content_path = format!("messages[{index}].content");
        let (mut content, content_losses) =
            decode_content(message.content.as_ref(), &content_path)?;
        losses.extend(content_losses);
        if message.role != "system" && content.is_empty() {
            // IR conversation messages cannot have empty content (spec/01 §3.3).
            content.push(Block::Text {
                text: String::new(),
            });
        }
        match message.role.as_str() {
            "system" => {
                let (system_blocks, system_losses) = content_system(content, &content_path);
                system.extend(system_blocks);
                losses.extend(system_losses);
            }
            "user" => messages.push(oxa_ir::Message {
                role: Role::User,
                content,
            }),
            "assistant" => {
                let default_calls = Vec::new();
                let calls = message.tool_calls.as_deref().unwrap_or(&default_calls);
                let (tool_blocks, tool_losses) =
                    decode_tool_calls(calls, &format!("messages[{index}].tool_calls"));
                if message.content.is_none() && !tool_blocks.is_empty() {
                    // A tool-only assistant message has no normal content to
                    // prepend.
                    content.clear();
                }
                content.extend(tool_blocks);
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
    let stop = wire.stop.clone().filter(|stops| !stops.is_empty());
    let params = Params {
        temperature: wire.temperature,
        top_p: wire.top_p,
        max_tokens: wire.max_tokens,
        stop_sequences: stop,
    };
    let params_set = params.temperature.is_some()
        || params.top_p.is_some()
        || params.max_tokens.is_some()
        || params.stop_sequences.is_some();
    if params_set {
        req.params = Some(params);
    }
    Ok((req, losses))
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
    )?;
    losses.extend(content_losses);
    let default_calls = Vec::new();
    let calls = choice
        .message
        .tool_calls
        .as_deref()
        .unwrap_or(&default_calls);
    let (tool_blocks, tool_losses) = decode_tool_calls(calls, "choices[0].message.tool_calls");
    if choice.message.content.is_none() && !tool_blocks.is_empty() {
        // A tool-only assistant response has no normal content to prepend.
        blocks.clear();
    }
    blocks.extend(tool_blocks);
    losses.extend(tool_losses);
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
        usage: wire
            .usage
            .as_ref()
            .map(|usage| Usage {
                input_tokens: usage.prompt_tokens,
                output_tokens: usage.completion_tokens,
            })
            .unwrap_or(Usage {
                input_tokens: 0,
                output_tokens: 0,
            }),
    };
    Ok((resp, losses))
}

fn decode_finish_reason(finish: &str) -> Result<(StopReason, Option<Loss>), Error> {
    match finish {
        "stop" => Ok((StopReason::EndTurn, None)),
        "length" => Ok((StopReason::MaxTokens, None)),
        "content_filter" => Ok((StopReason::Refusal, None)),
        "tool_calls" => Ok((StopReason::ToolUse, None)),
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
