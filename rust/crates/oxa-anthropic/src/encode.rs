//! IR → face conversions (spec/12 §4).

use serde_json::value::RawValue;

use oxa_ir::{Block, Loss, LossReason, Request as IrRequest, Response as IrResponse, StopReason};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::{
    encode_system, encode_tool_choice, input_from_ir_string, invalid_image_loss, loss,
    require_json_object, unsupported_block_loss,
};
use crate::types::{
    BLOCK_TYPE_IMAGE, BLOCK_TYPE_TEXT, BLOCK_TYPE_TOOL_RESULT, BLOCK_TYPE_TOOL_USE, BlockWire,
    ContentValue, Message, ROLE_ASSISTANT, ROLE_USER, Request, Response, SOURCE_TYPE_BASE64,
    SOURCE_TYPE_URL, STOP_REASON_END_TURN, STOP_REASON_MAX_TOKENS, STOP_REASON_REFUSAL,
    STOP_REASON_STOP_SEQUENCE, STOP_REASON_TOOL_USE, SourceWire, TYPE_MESSAGE, ToolWire, UsageWire,
};

/// Applied when an IR request carries no `params.max_tokens` and the
/// Anthropic Messages API's required max_tokens must still be emitted. 4096
/// is the value the Anthropic docs recommend as the default maximum token
/// count ("Set max_tokens", docs.claude.com; the docs' own examples use
/// 4096).
const DEFAULT_MAX_TOKENS: i64 = 4096;

/// Converts an IR request to an Anthropic Messages wire request (IR → face).
/// The IR system renders as the system block array; params map back to
/// temperature/top_p/stop_sequences; max_tokens is required on the wire, so a
/// missing `params.max_tokens` is filled with `DEFAULT_MAX_TOKENS` and
/// recorded as a degraded loss naming the default (spec/03 §3).
///
/// Message content rendering follows the from-ir rendering defaults pinned by
/// the seed vectors (vectors/README.md): the string shorthand is used only
/// for a request whose entire conversation is a single message of a single
/// text block and that carries no system prompt; every other request renders
/// block arrays.
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
            "Anthropic request metadata is the user_id semantic, not an arbitrary string map; the IR metadata map is dropped.",
        ));
    }
    let mut out = Request {
        model: config.map_model(&req.model),
        ..Request::default()
    };
    if !req.system.is_empty() {
        out.system = Some(crate::types::SystemValue::Blocks(encode_system(
            &req.system,
        )));
    }
    if let Some(tools) = &req.tools {
        let mut wire_tools = Vec::with_capacity(tools.len());
        for (index, tool) in tools.iter().enumerate() {
            let schema = serde_json::to_string(&tool.input_schema).unwrap_or_default();
            require_json_object(&schema, &format!("tools[{index}].input_schema"))?;
            wire_tools.push(ToolWire {
                name: tool.name.clone(),
                description: tool.description.clone().unwrap_or_default(),
                input_schema: tool.input_schema.clone(),
            });
        }
        out.tools = Some(wire_tools);
    }
    let (choice, choice_losses) = encode_tool_choice(req.tool_choice.as_ref())?;
    out.tool_choice = choice;
    losses.extend(choice_losses);

    let shorthand = req.system.is_empty()
        && req.messages.len() == 1
        && req.messages[0].content.len() == 1
        && matches!(req.messages[0].content[0], Block::Text { .. });
    for (index, message) in req.messages.iter().enumerate() {
        let role = match message.role {
            oxa_ir::Role::User => ROLE_USER,
            oxa_ir::Role::Assistant => ROLE_ASSISTANT,
        };
        let (blocks, block_losses) =
            encode_request_blocks(&message.content, &format!("messages[{index}].content"))?;
        losses.extend(block_losses);
        let content = if shorthand {
            let Block::Text { text } = &req.messages[0].content[0] else {
                unreachable!("shorthand checked the single text block above");
            };
            ContentValue::Text(text.clone())
        } else {
            ContentValue::Blocks(blocks)
        };
        out.messages.push(Message {
            role: role.to_string(),
            content: Some(content),
        });
    }

    let max_tokens = match req.params.as_ref().and_then(|params| params.max_tokens) {
        Some(max_tokens) => max_tokens,
        None => {
            losses.push(loss(
                "params",
                "max_tokens",
                LossReason::Degraded,
                format!(
                    "Anthropic Messages requires max_tokens; defaulting to {DEFAULT_MAX_TOKENS}"
                ),
            ));
            DEFAULT_MAX_TOKENS
        }
    };
    out.max_tokens = max_tokens;
    if let Some(params) = &req.params {
        out.temperature = params.temperature;
        out.top_p = params.top_p;
        out.stop_sequences = params
            .stop_sequences
            .clone()
            .filter(|sequences| !sequences.is_empty());
    }
    Ok((out, losses))
}

fn encode_request_blocks(
    blocks: &[Block],
    path: &str,
) -> Result<(Vec<BlockWire>, Vec<Loss>), Error> {
    let mut out = Vec::with_capacity(blocks.len());
    let mut losses = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        let (encoded, block_losses, mapped) =
            encode_request_block(block, &format!("{path}[{index}]"))?;
        if mapped {
            out.push(encoded);
        }
        losses.extend(block_losses);
    }
    Ok((out, losses))
}

/// Returns (wire block, losses, mapped).
fn encode_request_block(block: &Block, path: &str) -> Result<(BlockWire, Vec<Loss>, bool), Error> {
    match block {
        Block::Text { text } => Ok((
            BlockWire {
                kind: BLOCK_TYPE_TEXT.to_string(),
                text: text.clone(),
                ..BlockWire::default()
            },
            Vec::new(),
            true,
        )),
        Block::Image {
            media_type,
            data,
            url,
        } => encode_image_block(media_type.as_deref(), data.as_deref(), url.as_deref(), path),
        Block::ToolUse { id, name, input } => {
            if id.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.id is required")));
            }
            if name.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.name is required")));
            }
            let raw = input_from_ir_string(input, path)?;
            Ok((
                BlockWire {
                    kind: BLOCK_TYPE_TOOL_USE.to_string(),
                    id: id.clone(),
                    name: name.clone(),
                    input: Some(raw),
                    ..BlockWire::default()
                },
                Vec::new(),
                true,
            ))
        }
        Block::ToolResult { .. } => encode_tool_result_block(block, path),
    }
}

/// Returns (wire block, losses, mapped). The IR image must carry exactly one
/// of data or url; violations are dropped with a loss.
fn encode_image_block(
    media_type: Option<&str>,
    data: Option<&str>,
    url: Option<&str>,
    path: &str,
) -> Result<(BlockWire, Vec<Loss>, bool), Error> {
    let has_data = data.is_some_and(|value| !value.is_empty());
    let has_url = url.is_some_and(|value| !value.is_empty());
    if has_data == has_url {
        return Ok((
            BlockWire::default(),
            vec![invalid_image_loss(
                path,
                "image must contain exactly one of data or url",
            )],
            false,
        ));
    }
    if has_data {
        let Some(media_type) = media_type.filter(|value| !value.is_empty()) else {
            return Ok((
                BlockWire::default(),
                vec![invalid_image_loss(
                    path,
                    "base64 image data requires media_type",
                )],
                false,
            ));
        };
        return Ok((
            BlockWire {
                kind: BLOCK_TYPE_IMAGE.to_string(),
                source: Some(SourceWire {
                    kind: SOURCE_TYPE_BASE64.to_string(),
                    media_type: media_type.to_string(),
                    data: data.unwrap_or_default().to_string(),
                    url: String::new(),
                }),
                ..BlockWire::default()
            },
            Vec::new(),
            true,
        ));
    }
    if media_type.is_some_and(|value| !value.is_empty()) {
        return Ok((
            BlockWire::default(),
            vec![invalid_image_loss(
                path,
                "URL image must not carry media_type",
            )],
            false,
        ));
    }
    Ok((
        BlockWire {
            kind: BLOCK_TYPE_IMAGE.to_string(),
            source: Some(SourceWire {
                kind: SOURCE_TYPE_URL.to_string(),
                url: url.unwrap_or_default().to_string(),
                ..SourceWire::default()
            }),
            ..BlockWire::default()
        },
        Vec::new(),
        true,
    ))
}

fn encode_tool_result_block(
    block: &Block,
    path: &str,
) -> Result<(BlockWire, Vec<Loss>, bool), Error> {
    let Block::ToolResult {
        tool_use_id,
        content,
        is_error,
    } = block
    else {
        return Err(Error::new(format!(
            "anthropic: {path} is not a tool_result block"
        )));
    };
    if tool_use_id.is_empty() {
        return Err(Error::new(format!(
            "anthropic: {path}.tool_use_id is required"
        )));
    }
    let mut wire_content: Vec<BlockWire> = Vec::with_capacity(content.len());
    let mut losses = Vec::new();
    for (index, inner) in content.iter().enumerate() {
        let content_path = format!("{path}.content[{index}]");
        match inner {
            Block::Text { text } => wire_content.push(BlockWire {
                kind: BLOCK_TYPE_TEXT.to_string(),
                text: text.clone(),
                ..BlockWire::default()
            }),
            Block::Image {
                media_type,
                data,
                url,
            } => {
                let (image, image_losses, mapped) = encode_image_block(
                    media_type.as_deref(),
                    data.as_deref(),
                    url.as_deref(),
                    &content_path,
                )?;
                if mapped {
                    wire_content.push(image);
                }
                losses.extend(image_losses);
            }
            _ => losses.push(unsupported_block_loss(
                &content_path,
                "this IR block type has no Anthropic equivalent in this position".to_string(),
            )),
        }
    }
    Ok((
        BlockWire {
            kind: BLOCK_TYPE_TOOL_RESULT.to_string(),
            tool_use_id: tool_use_id.clone(),
            content: Some(ContentValue::Blocks(wire_content)),
            is_error: is_error == &Some(true),
            ..BlockWire::default()
        },
        losses,
        true,
    ))
}

/// Converts an IR response to an Anthropic Messages wire response (IR →
/// face). Near-identity; the envelope fields type ("message") and role
/// ("assistant") are rendered defaults and record no loss
/// (vectors/README.md "From-ir rendering defaults").
pub fn encode_response(resp: &IrResponse, config: &Config) -> Result<(Response, Vec<Loss>), Error> {
    let mut out = Response {
        id: resp.id.clone(),
        kind: TYPE_MESSAGE.to_string(),
        role: ROLE_ASSISTANT.to_string(),
        model: config.map_model(&resp.model),
        usage: Some(UsageWire {
            input_tokens: resp.usage.input_tokens,
            output_tokens: resp.usage.output_tokens,
        }),
        ..Response::default()
    };
    let mut losses = Vec::new();
    for (index, block) in resp.content.iter().enumerate() {
        let (encoded, block_losses, mapped) =
            encode_response_block(block, &format!("content[{index}]"))?;
        if mapped {
            out.content.push(encoded);
        }
        losses.extend(block_losses);
    }
    let (reason, seq) = encode_stop_reason(resp.stop_reason, resp.stop_sequence.as_deref())?;
    out.stop_reason = reason.to_string();
    if let Some(s) = seq {
        out.stop_sequence = s;
    }
    Ok((out, losses))
}

pub(crate) fn encode_stop_reason(
    stop: StopReason,
    seq: Option<&str>,
) -> Result<(&'static str, Option<String>), Error> {
    match stop {
        StopReason::EndTurn => Ok((STOP_REASON_END_TURN, None)),
        StopReason::MaxTokens => Ok((STOP_REASON_MAX_TOKENS, None)),
        StopReason::StopSequence => {
            let s = seq.filter(|s| !s.is_empty()).map(|s| s.to_string());
            Ok((STOP_REASON_STOP_SEQUENCE, s))
        }
        StopReason::ToolUse => Ok((STOP_REASON_TOOL_USE, None)),
        StopReason::Refusal => Ok((STOP_REASON_REFUSAL, None)),
        StopReason::Other => Err(Error::new(
            "anthropic: stop reason \"other\" has no Anthropic equivalent",
        )),
    }
}

/// Returns (wire block, losses, mapped).
fn encode_response_block(block: &Block, path: &str) -> Result<(BlockWire, Vec<Loss>, bool), Error> {
    match block {
        Block::Text { text } => Ok((
            BlockWire {
                kind: BLOCK_TYPE_TEXT.to_string(),
                text: text.clone(),
                ..BlockWire::default()
            },
            Vec::new(),
            true,
        )),
        Block::Image {
            media_type,
            data,
            url,
        } => encode_image_block(media_type.as_deref(), data.as_deref(), url.as_deref(), path),
        Block::ToolUse { id, name, input } => {
            if id.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.id is required")));
            }
            if name.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.name is required")));
            }
            let raw: Box<RawValue> = input_from_ir_string(input, path)?;
            Ok((
                BlockWire {
                    kind: BLOCK_TYPE_TOOL_USE.to_string(),
                    id: id.clone(),
                    name: name.clone(),
                    input: Some(raw),
                    ..BlockWire::default()
                },
                Vec::new(),
                true,
            ))
        }
        _ => Ok((
            BlockWire::default(),
            vec![unsupported_block_loss(
                path,
                "this IR block type has no Anthropic equivalent in this position".to_string(),
            )],
            false,
        )),
    }
}
