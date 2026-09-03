//! IR → face conversions (spec/10 §4).

use oxa_ir::{Block, Loss, LossReason, Request as IrRequest, Response as IrResponse, StopReason};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{
    encode_assistant_message, encode_tool_choice, encode_tool_result, encode_user_content, loss,
};
use crate::types::{
    Choice, ContentValue, FunctionWire, Message, Request, Response, ToolWire, UsageWire,
};

/// Converts an IR request to a Chat Completions wire request (IR → face).
/// System content renders as one leading system message; text content remains
/// a string while image input renders as content parts.
pub fn encode_request(req: &IrRequest, config: &Config) -> Result<(Request, Vec<Loss>), Error> {
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
        out.tools = Some(
            tools
                .iter()
                .map(|tool| ToolWire {
                    kind: "function".to_string(),
                    function: FunctionWire {
                        name: tool.name.clone(),
                        description: tool.description.clone().unwrap_or_default(),
                        parameters: non_null(&tool.input_schema),
                        arguments: String::new(),
                    },
                })
                .collect(),
        );
    }
    let (choice, choice_loss) = encode_tool_choice(req.tool_choice.as_ref());
    out.tool_choice = choice;
    if let Some(choice_loss) = choice_loss {
        losses.push(choice_loss);
    }
    if !req.system.is_empty() {
        let mut text = String::new();
        for block in &req.system {
            match block {
                oxa_ir::SystemBlock::Text { text: value } => text.push_str(value),
            }
        }
        out.messages.push(Message {
            role: "system".to_string(),
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
                let mut results: Vec<Block> = Vec::new();
                // N-CC-9: tool messages are hoisted ahead of the trailing user
                // content. When ordinary content precedes a tool result in the
                // source turn, that hoisting does not preserve the source order.
                let mut last_result: Option<usize> = None;
                let mut first_normal: Option<usize> = None;
                for (position, block) in message.content.iter().enumerate() {
                    if matches!(block, Block::ToolResult { .. }) {
                        results.push(block.clone());
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
                for (position, result) in results.iter().enumerate() {
                    let (tool_message, result_losses) = encode_tool_result(
                        result,
                        &format!("messages[{index}].content[{position}]"),
                    );
                    out.messages.push(tool_message);
                    losses.extend(result_losses);
                }
                if !normal.is_empty() || results.is_empty() {
                    let (content, content_losses) =
                        encode_user_content(&normal, &format!("messages[{index}].content"));
                    out.messages.push(Message {
                        role: "user".to_string(),
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
    }
    Ok((out, losses))
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
    let (message, mut losses) = encode_assistant_message(&resp.content, "content");
    let (finish, finish_loss) = encode_finish_reason(resp.stop_reason)?;
    if let Some(l) = finish_loss {
        losses.push(l);
    }
    Ok((
        Response {
            id: resp.id.clone(),
            object: "chat.completion".to_string(),
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
                total_tokens: resp.usage.input_tokens + resp.usage.output_tokens,
            }),
        },
        losses,
    ))
}

pub(crate) fn encode_finish_reason(
    stop: StopReason,
) -> Result<(&'static str, Option<Loss>), Error> {
    match stop {
        StopReason::EndTurn => Ok(("stop", None)),
        StopReason::MaxTokens => Ok(("length", None)),
        StopReason::Refusal => Ok(("content_filter", None)),
        StopReason::ToolUse => Ok(("tool_calls", None)),
        StopReason::StopSequence => Ok((
            "stop",
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
