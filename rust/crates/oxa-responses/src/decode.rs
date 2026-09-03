//! Face → IR conversions (spec/11 §4).

use oxa_ir::{
    Block, Loss, LossReason, Params, Request as IrRequest, Response as IrResponse, Role,
    StopReason, Usage,
};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{
    content_system, decode_assistant_run, decode_content, decode_output_run, decode_tool_choice,
    loss,
};
use crate::types::{
    INCOMPLETE_REASON_MAX_OUTPUT_TOKENS, ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE, Input, PART_TYPE_OUTPUT_TEXT, ROLE_ASSISTANT, ROLE_SYSTEM, ROLE_USER,
    Request, Response, STATUS_COMPLETED, STATUS_FAILED, STATUS_INCOMPLETE, TOOL_TYPE_FUNCTION,
};

/// Converts a Responses wire request to the IR (face → IR). Semantic
/// unmappables are losses, never errors; errors are reserved for structural
/// type violations of known fields (spec/02 §4).
pub fn decode_request(wire: &Request, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    for (path, field, present, detail) in [
        (
            "metadata",
            "metadata",
            wire.metadata
                .as_ref()
                .is_some_and(|metadata| !metadata.is_empty()),
            "Responses request metadata has no IR equivalent in v1.",
        ),
        (
            "text.verbosity",
            "verbosity",
            wire.text
                .as_ref()
                .and_then(|text| text.verbosity.as_ref())
                .is_some(),
            "Responses output verbosity has no IR equivalent in v1.",
        ),
        (
            "text.format",
            "format",
            wire.text
                .as_ref()
                .and_then(|text| text.format.as_ref())
                .is_some(),
            "Responses text output format has no IR equivalent in v1.",
        ),
        (
            "reasoning",
            "reasoning",
            wire.reasoning.is_some(),
            "Responses reasoning effort configuration has no IR equivalent in v1.",
        ),
        (
            "parallel_tool_calls",
            "parallel_tool_calls",
            wire.parallel_tool_calls.is_some(),
            "Responses parallel tool calls have no IR equivalent in v1.",
        ),
    ] {
        if present {
            losses.push(loss(path, field, LossReason::UnmappedField, detail));
        }
    }

    let mut request = IrRequest {
        model: config.map_model(&wire.model),
        system: Vec::new(),
        messages: Vec::new(),
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
    };
    if let Some(instructions) = &wire.instructions {
        request.system.push(oxa_ir::SystemBlock::Text {
            text: instructions.clone(),
        });
    }

    let mut tools = Vec::new();
    for (index, tool) in wire.tools.iter().enumerate() {
        if tool.kind != TOOL_TYPE_FUNCTION {
            losses.push(loss(
                format!("tools[{index}]"),
                "type",
                LossReason::UnsupportedSemantic,
                format!("Responses tool type {:?} has no IR equivalent", tool.kind),
            ));
            continue;
        }
        tools.push(oxa_ir::Tool {
            name: tool.name.clone(),
            description: non_empty(tool.description.clone()),
            input_schema: tool.parameters.clone(),
        });
        if tool.strict.is_some() {
            losses.push(loss(
                format!("tools[{index}].strict"),
                "strict",
                LossReason::UnmappedField,
                "Responses function tool strict mode has no IR equivalent in v1.",
            ));
        }
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
            content: vec![Block::Text { text: text.clone() }],
        }),
        Input::Items(items) => {
            let mut index = 0usize;
            while index < items.len() {
                let item = &items[index];
                match item.kind.as_str() {
                    "" | ITEM_TYPE_MESSAGE => match item.role.as_str() {
                        ROLE_SYSTEM => {
                            let path = format!("input[{index}].content");
                            let (content, content_losses) =
                                decode_content(item.content.as_ref(), &path);
                            let (system, system_losses) = content_system(content, &path);
                            request.system.extend(system);
                            losses.extend(content_losses);
                            losses.extend(system_losses);
                            index += 1;
                        }
                        ROLE_USER => {
                            let (mut content, content_losses) = decode_content(
                                item.content.as_ref(),
                                &format!("input[{index}].content"),
                            );
                            if content.is_empty() {
                                content.push(Block::Text {
                                    text: String::new(),
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
                            let (message, next, run_losses) = decode_assistant_run(items, index)?;
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
                    ITEM_TYPE_FUNCTION_CALL => {
                        let (message, next, run_losses) = decode_assistant_run(items, index)?;
                        if let Some(message) = message {
                            request.messages.push(message);
                        }
                        losses.extend(run_losses);
                        index = next;
                    }
                    ITEM_TYPE_FUNCTION_CALL_OUTPUT => {
                        let (message, next) = decode_output_run(items, index);
                        request.messages.push(message);
                        index = next;
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
        return Err(Error::new(
            "responses: request carries no conversation input",
        ));
    }
    let params = Params {
        temperature: wire.temperature,
        top_p: wire.top_p,
        max_tokens: wire.max_output_tokens,
        stop_sequences: None,
    };
    if params.temperature.is_some() || params.top_p.is_some() || params.max_tokens.is_some() {
        request.params = Some(params);
    }
    Ok((request, losses))
}

fn non_empty(value: String) -> Option<String> {
    if value.is_empty() { None } else { Some(value) }
}

/// Converts a Responses wire response to the IR (face → IR). Envelope fields
/// (object, item ids/status) are exempt from losses; usage.total_tokens is
/// derived and therefore ignored.
pub fn decode_response(wire: &Response, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    let mut text = Vec::new();
    let mut calls = Vec::new();
    let mut has_tool_use = false;
    for (item_index, item) in wire.output.iter().enumerate() {
        match item.kind.as_str() {
            ITEM_TYPE_MESSAGE => {
                for (content_index, part) in item.content.iter().enumerate() {
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
                    if !part.annotations.is_empty() {
                        losses.push(loss(
                            format!("output[{item_index}].content[{content_index}].annotations"),
                            "annotations",
                            LossReason::UnmappedField,
                            "Responses output annotations have no IR equivalent in v1.",
                        ));
                    }
                    text.push(Block::Text {
                        text: part.text.clone(),
                    });
                }
            }
            ITEM_TYPE_FUNCTION_CALL => {
                calls.push(Block::ToolUse {
                    id: item.call_id.clone(),
                    name: item.name.clone(),
                    input: item.arguments.clone(),
                });
                has_tool_use = true;
            }
            other => losses.push(loss(
                format!("output[{item_index}]"),
                "type",
                LossReason::UnsupportedSemantic,
                format!("Responses output item type {other:?} has no IR equivalent"),
            )),
        }
    }
    let (stop_reason, status_losses) = decode_status(wire, has_tool_use)?;
    losses.extend(status_losses);
    text.extend(calls);
    let usage = wire.usage.unwrap_or_default();
    Ok((
        IrResponse {
            id: wire.id.clone(),
            model: config.map_model(&wire.model),
            content: text,
            stop_reason,
            stop_sequence: None,
            usage: Usage {
                input_tokens: usage.input_tokens,
                output_tokens: usage.output_tokens,
            },
        },
        losses,
    ))
}

pub(crate) fn decode_status(
    wire: &Response,
    has_tool_use: bool,
) -> Result<(StopReason, Vec<Loss>), Error> {
    if let Some(error) = &wire.error {
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
