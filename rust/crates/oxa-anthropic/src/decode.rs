//! Face → IR conversions (spec/12 §4).

use oxa_ir::{
    Loss, LossReason, Params, Request as IrRequest, Response as IrResponse, StopReason, Usage,
};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{decode_block, decode_content, decode_system, decode_tool_choice, loss};
use crate::types::{
    ROLE_ASSISTANT, ROLE_USER, Request, Response, STOP_REASON_END_TURN, STOP_REASON_MAX_TOKENS,
    STOP_REASON_REFUSAL, STOP_REASON_STOP_SEQUENCE, STOP_REASON_TOOL_USE,
};

/// Converts an Anthropic Messages wire request to the IR (face → IR). The
/// mapping is near-identity: system (string or block array) becomes the IR
/// system, required max_tokens becomes `params.max_tokens`, and
/// temperature/top_p/stop_sequences map 1:1. cache_control annotations have
/// no IR equivalent in v1 and are dropped with unmapped-field losses.
pub fn decode_request(wire: &Request, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    if wire.max_tokens <= 0 {
        return Err(Error::new(
            "anthropic: max_tokens is required and must be positive",
        ));
    }
    if wire.metadata.is_some() {
        // Anthropic's wire metadata is the specific {user_id} semantic, not
        // an arbitrary string map, so it is dropped symmetrically with a
        // single unmapped-field loss.
        losses.push(loss(
            "metadata",
            "metadata",
            LossReason::UnmappedField,
            "Anthropic request metadata (user_id) has no IR equivalent in v1.",
        ));
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
            let schema = serde_json::to_string(&tool.input_schema).unwrap_or_default();
            crate::normalize::require_json_object(
                &schema,
                &format!("tools[{index}].input_schema"),
            )?;
            tools.push(oxa_ir::Tool {
                name: tool.name.clone(),
                description: non_empty(tool.description.clone()),
                input_schema: tool.input_schema.clone(),
            });
        }
        if !tools.is_empty() {
            req.tools = Some(tools);
        }
    }
    let (choice, choice_losses) = decode_tool_choice(wire.tool_choice.as_ref())?;
    req.tool_choice = choice;
    losses.extend(choice_losses);

    let (system, system_losses) = decode_system(wire.system.as_ref())?;
    losses.extend(system_losses);
    req.system = system;

    for (index, message) in wire.messages.iter().enumerate() {
        let role = match message.role.as_str() {
            ROLE_USER => oxa_ir::Role::User,
            ROLE_ASSISTANT => oxa_ir::Role::Assistant,
            other => {
                return Err(Error::new(format!(
                    "anthropic: messages[{index}]: unknown role {other:?}"
                )));
            }
        };
        let (blocks, block_losses) = decode_content(
            message.content.as_ref(),
            &format!("messages[{index}].content"),
        )?;
        losses.extend(block_losses);
        req.messages.push(oxa_ir::Message {
            role,
            content: blocks,
        });
    }
    if req.messages.is_empty() {
        return Err(Error::new("anthropic: request carries no messages"));
    }

    let stop = wire
        .stop_sequences
        .clone()
        .filter(|sequences| !sequences.is_empty());
    let params = Params {
        temperature: wire.temperature,
        top_p: wire.top_p,
        max_tokens: Some(wire.max_tokens),
        stop_sequences: stop,
    };
    req.params = Some(params);
    Ok((req, losses))
}

fn non_empty(value: String) -> Option<String> {
    if value.is_empty() { None } else { Some(value) }
}

/// Converts an Anthropic Messages wire response to the IR (face → IR). Text,
/// image, and tool_use blocks map near-identically; stop_reason maps by value
/// (stop_sequence also carries the matched sequence), and usage maps 1:1. The
/// envelope fields type and role are exempt from losses
/// (vectors/README.md loss conventions).
pub fn decode_response(wire: &Response, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    let mut content = Vec::new();
    for (index, block) in wire.content.iter().enumerate() {
        let (decoded, block_losses, mapped) = decode_block(block, &format!("content[{index}]"))?;
        if mapped {
            content.extend(decoded);
        }
        losses.extend(block_losses);
    }
    let (stop, stop_loss) = decode_stop_reason(&wire.stop_reason)?;
    if let Some(stop_loss) = stop_loss {
        losses.push(stop_loss);
    }
    let resp = IrResponse {
        id: wire.id.clone(),
        model: config.map_model(&wire.model),
        content,
        stop_reason: stop,
        stop_sequence: non_empty(wire.stop_sequence.clone()),
        usage: wire
            .usage
            .as_ref()
            .map(|usage| Usage {
                input_tokens: usage.input_tokens,
                output_tokens: usage.output_tokens,
            })
            .unwrap_or(Usage {
                input_tokens: 0,
                output_tokens: 0,
            }),
    };
    Ok((resp, losses))
}

pub(crate) fn decode_stop_reason(stop: &str) -> Result<(StopReason, Option<Loss>), Error> {
    match stop {
        STOP_REASON_END_TURN => Ok((StopReason::EndTurn, None)),
        STOP_REASON_MAX_TOKENS => Ok((StopReason::MaxTokens, None)),
        STOP_REASON_STOP_SEQUENCE => Ok((StopReason::StopSequence, None)),
        STOP_REASON_TOOL_USE => Ok((StopReason::ToolUse, None)),
        STOP_REASON_REFUSAL => Ok((StopReason::Refusal, None)),
        "" => Err(Error::new("anthropic: stop_reason is missing")),
        other => Ok((
            StopReason::Other,
            Some(loss(
                "stop_reason",
                "stop_reason",
                LossReason::UnmappedValue,
                format!("Anthropic stop_reason {other:?} has no IR equivalent"),
            )),
        )),
    }
}
