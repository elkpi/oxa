//! IR → face conversions (spec/11 §4).

use oxa_ir::{Block, Loss, LossReason, Request as IrRequest, Response as IrResponse, StopReason};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{encode_assistant_message, encode_tool_choice, encode_user_message, loss};
use crate::types::{
    ContentValue, ERROR_CODE_REFUSAL, ErrorWire, INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_MESSAGE, IncompleteWire, Input, OBJECT_RESPONSE, OutputItem,
    OutputPart, PART_TYPE_OUTPUT_TEXT, ROLE_ASSISTANT, ROLE_USER, Request, Response,
    STATUS_COMPLETED, STATUS_FAILED, STATUS_INCOMPLETE, TOOL_TYPE_FUNCTION, ToolDef, UsageWire,
};

/// Converts an IR request to a Responses wire request (IR → face). System
/// content renders as `instructions`; a sole user text message without system
/// content renders as the `input` string shorthand (N-R-1 and N-R-2).
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
                oxa_ir::SystemBlock::Text { text } => instructions.push_str(text),
            }
        }
        out.instructions = Some(instructions);
    }
    if let Some(tools) = &req.tools {
        out.tools = tools
            .iter()
            .map(|tool| ToolDef {
                kind: TOOL_TYPE_FUNCTION.to_string(),
                name: tool.name.clone(),
                description: tool.description.clone().unwrap_or_default(),
                parameters: tool.input_schema.clone(),
                strict: None,
            })
            .collect();
    }
    let (choice, choice_loss) = encode_tool_choice(req.tool_choice.as_ref());
    out.tool_choice = choice;
    if let Some(choice_loss) = choice_loss {
        losses.push(choice_loss);
    }

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
    Ok((out, losses))
}

/// Converts an IR response to a Responses wire response (IR → face). Envelope
/// fields absent from the IR use N-R-12's documented defaults without loss.
pub fn encode_response(resp: &IrResponse, config: &Config) -> Result<(Response, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    let mut text = String::new();
    let mut has_text = false;
    let mut calls = Vec::new();
    for (index, block) in resp.content.iter().enumerate() {
        match block {
            Block::Text { text: value } => {
                text.push_str(value);
                has_text = true;
            }
            Block::ToolUse { id, name, input } => calls.push(OutputItem {
                kind: ITEM_TYPE_FUNCTION_CALL.to_string(),
                id: "fc_abc123".to_string(),
                status: STATUS_COMPLETED.to_string(),
                call_id: id.clone(),
                name: name.clone(),
                arguments: input.clone(),
                ..OutputItem::default()
            }),
            Block::Image { .. } | Block::ToolResult { .. } => losses.push(loss(
                format!("content[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in a Responses output item",
            )),
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
                total_tokens: resp.usage.input_tokens + resp.usage.output_tokens,
            }),
            incomplete_details,
            error,
        },
        losses,
    ))
}
