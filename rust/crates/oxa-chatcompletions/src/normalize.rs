//! Normalization rules N-CC-1 through N-CC-10 (spec/10 §5).

use serde_json::{Value, json};

use oxa_ir::{
    Block, Loss, LossReason, Message as IrMessage, Role, SystemBlock, ToolChoice, ToolChoiceMode,
};

use crate::types::{ContentPart, ContentValue, FunctionWire, ImageURLWire, Message, ToolCall};

pub(crate) fn loss(
    path: impl Into<String>,
    field: impl Into<String>,
    reason: LossReason,
    detail: impl Into<String>,
) -> Loss {
    Loss {
        path: path.into(),
        field: field.into(),
        reason,
        detail: detail.into(),
    }
}

/// N-CC-1 normalizes Chat Completions string and parts-array content into IR
/// blocks, dropping unknown parts as semantic losses.
pub(crate) fn decode_content(
    content: Option<&ContentValue>,
    path: &str,
) -> Result<(Vec<Block>, Vec<Loss>), crate::error::Error> {
    let Some(content) = content else {
        return Ok((
            vec![Block::Text {
                text: String::new(),
            }],
            Vec::new(),
        ));
    };
    match content {
        ContentValue::Text(text) => Ok((vec![Block::Text { text: text.clone() }], Vec::new())),
        ContentValue::Parts(parts) => {
            let mut blocks = Vec::with_capacity(parts.len());
            let mut losses = Vec::new();
            for (index, part) in parts.iter().enumerate() {
                let (decoded, part_losses) = decode_content_part(part, path, index);
                blocks.extend(decoded);
                losses.extend(part_losses);
            }
            Ok((blocks, losses))
        }
    }
}

fn decode_content_part(part: &ContentPart, path: &str, index: usize) -> (Vec<Block>, Vec<Loss>) {
    match part.kind.as_str() {
        "text" => (
            vec![Block::Text {
                text: part.text.clone(),
            }],
            Vec::new(),
        ),
        "image_url" => {
            let raw = part
                .image_url
                .as_ref()
                .map(|image| image.url.as_str())
                .unwrap_or_default();
            match decode_image_url(raw, &format!("{path}[{index}].image_url")) {
                Ok(block) => (vec![block], Vec::new()),
                Err(image_loss) => (Vec::new(), vec![image_loss]),
            }
        }
        other => (
            Vec::new(),
            vec![loss(
                format!("{path}[{index}]"),
                "type",
                LossReason::UnsupportedSemantic,
                format!("Chat Completions content part type {other:?} has no IR equivalent"),
            )],
        ),
    }
}

/// N-CC-2 normalizes supported https and data image URLs into image blocks.
fn decode_image_url(raw: &str, path: &str) -> Result<Block, Loss> {
    if raw.starts_with("https:") {
        if is_valid_https_url(raw) {
            return Ok(Block::Image {
                media_type: None,
                data: None,
                url: Some(raw.to_string()),
            });
        }
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "malformed https image URL has no IR equivalent",
        ));
    }
    let Some(data_part) = raw.strip_prefix("data:") else {
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "only https and base64 data image URLs are supported",
        ));
    };
    let Some((metadata, data)) = data_part.split_once(',') else {
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "malformed data image URL has no IR equivalent",
        ));
    };
    let Some(media_type) = metadata.strip_suffix(";base64") else {
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "malformed data image URL has no IR equivalent",
        ));
    };
    if !media_type.to_lowercase().starts_with("image/") || media_type == "image/" {
        return Err(loss(
            path,
            "image_url",
            LossReason::UnsupportedSemantic,
            "non-image data URL has no IR equivalent",
        ));
    }
    Ok(Block::Image {
        media_type: Some(media_type.to_string()),
        data: Some(data.to_string()),
        url: None,
    })
}

/// Mirrors the https acceptance of Go's `url.ParseRequestURI` closely enough
/// for vector behavior: an explicit https scheme with a non-empty host and no
/// whitespace or control characters.
fn is_valid_https_url(raw: &str) -> bool {
    let Some(rest) = raw.strip_prefix("https://") else {
        return false;
    };
    if rest.chars().any(|c| c.is_whitespace() || c.is_control()) {
        return false;
    }
    let host = rest.split(['/', '?', '#']).next().unwrap_or_default();
    !host.is_empty()
}

/// N-CC-3 appends function tool calls after the assistant's normal content.
/// The opaque arguments text becomes the IR raw input text directly (INV-1;
/// the Rust IR carries the text without its JSON string envelope).
pub(crate) fn decode_tool_calls(calls: &[ToolCall], path: &str) -> (Vec<Block>, Vec<Loss>) {
    let mut blocks = Vec::with_capacity(calls.len());
    let mut losses = Vec::new();
    for (index, call) in calls.iter().enumerate() {
        if call.kind != "function" {
            losses.push(loss(
                format!("{path}[{index}]"),
                "type",
                LossReason::UnsupportedSemantic,
                format!(
                    "Chat Completions tool call type {:?} has no IR equivalent",
                    call.kind
                ),
            ));
            continue;
        }
        blocks.push(Block::ToolUse {
            id: call.id.clone(),
            name: call.function.name.clone(),
            input: call.function.arguments.clone(),
        });
    }
    (blocks, losses)
}

/// N-CC-4 merges consecutive role:tool messages into one user message of
/// tool_result blocks, as required by INV-4. Returns the merged message, the
/// index of the first message after the run, and the run's losses.
pub(crate) fn decode_tool_result_run(
    messages: &[Message],
    start: usize,
) -> Result<(IrMessage, usize, Vec<Loss>), crate::error::Error> {
    let mut content: Vec<Block> = Vec::new();
    let mut losses = Vec::new();
    let mut index = start;
    while index < messages.len() && messages[index].role == "tool" {
        let (blocks, block_losses) = decode_content(
            messages[index].content.as_ref(),
            &format!("messages[{index}].content"),
        )?;
        losses.extend(block_losses);
        content.push(Block::ToolResult {
            tool_use_id: messages[index].tool_call_id.clone(),
            content: blocks,
            is_error: None,
        });
        if messages[index].function_call.is_some() {
            losses.push(loss(
                format!("messages[{index}].function_call"),
                "function_call",
                LossReason::UnmappedField,
                "legacy Chat Completions function_call has no IR equivalent",
            ));
        }
        index += 1;
    }
    Ok((
        IrMessage {
            role: Role::User,
            content,
        },
        index,
        losses,
    ))
}

/// N-CC-5 maps the Chat Completions tool_choice forms to the IR modes.
pub(crate) fn decode_tool_choice(value: Option<&Value>) -> (Option<ToolChoice>, Vec<Loss>) {
    let Some(value) = value else {
        return (None, Vec::new());
    };
    match value {
        Value::String(choice) => match choice.as_str() {
            "auto" => (
                Some(ToolChoice {
                    mode: ToolChoiceMode::Auto,
                    name: None,
                }),
                Vec::new(),
            ),
            "none" => (
                Some(ToolChoice {
                    mode: ToolChoiceMode::None,
                    name: None,
                }),
                Vec::new(),
            ),
            "required" => (
                Some(ToolChoice {
                    mode: ToolChoiceMode::Any,
                    name: None,
                }),
                Vec::new(),
            ),
            other => (
                None,
                vec![loss(
                    "tool_choice",
                    "tool_choice",
                    LossReason::UnsupportedSemantic,
                    format!("Chat Completions tool_choice {other:?} has no IR equivalent"),
                )],
            ),
        },
        Value::Object(choice) => {
            let kind = choice
                .get("type")
                .and_then(Value::as_str)
                .unwrap_or_default();
            let name = choice
                .get("function")
                .and_then(|function| function.get("name"))
                .and_then(Value::as_str)
                .unwrap_or_default();
            if kind == "function" && !name.is_empty() {
                (
                    Some(ToolChoice {
                        mode: ToolChoiceMode::Tool,
                        name: Some(name.to_string()),
                    }),
                    Vec::new(),
                )
            } else {
                (
                    None,
                    vec![loss(
                        "tool_choice",
                        "tool_choice",
                        LossReason::UnsupportedSemantic,
                        "only named function Chat Completions tool_choice values are supported",
                    )],
                )
            }
        }
        _ => (
            None,
            vec![loss(
                "tool_choice",
                "tool_choice",
                LossReason::UnsupportedSemantic,
                "Chat Completions tool_choice has no IR equivalent",
            )],
        ),
    }
}

/// N-CC-6 renders the IR's four tool-choice modes in Chat Completions form.
pub(crate) fn encode_tool_choice(choice: Option<&ToolChoice>) -> (Option<Value>, Option<Loss>) {
    let Some(choice) = choice else {
        return (None, None);
    };
    match choice.mode {
        ToolChoiceMode::Auto => (Some(json!("auto")), None),
        ToolChoiceMode::None => (Some(json!("none")), None),
        ToolChoiceMode::Any => (Some(json!("required")), None),
        ToolChoiceMode::Tool => match choice.name.as_deref() {
            Some(name) if !name.is_empty() => (
                Some(json!({
                    "type": "function",
                    "function": { "name": name }
                })),
                None,
            ),
            _ => (
                None,
                Some(loss(
                    "tool_choice",
                    "tool_choice",
                    LossReason::UnsupportedSemantic,
                    "IR named tool choice has no function name",
                )),
            ),
        },
    }
}

/// N-CC-7 renders image blocks as the supported image_url content parts.
pub(crate) fn encode_image_block(
    media_type: Option<&str>,
    data: Option<&str>,
    url: Option<&str>,
    path: &str,
) -> Result<ContentPart, Loss> {
    let data = data.filter(|value| !value.is_empty());
    let url = url.filter(|value| !value.is_empty());
    if data.is_some() && url.is_some() {
        return Err(loss(
            path,
            "image",
            LossReason::UnsupportedSemantic,
            "IR image contains both data and URL",
        ));
    }
    if let Some(data) = data {
        let media_type = media_type.unwrap_or_default();
        if !media_type.to_lowercase().starts_with("image/") || media_type == "image/" {
            return Err(loss(
                path,
                "media_type",
                LossReason::UnsupportedSemantic,
                "IR image media type has no Chat Completions image_url equivalent",
            ));
        }
        return Ok(ContentPart {
            kind: "image_url".to_string(),
            text: String::new(),
            image_url: Some(ImageURLWire {
                url: format!("data:{media_type};base64,{data}"),
            }),
        });
    }
    if let Some(url) = url
        && is_valid_https_url(url)
    {
        return Ok(ContentPart {
            kind: "image_url".to_string(),
            text: String::new(),
            image_url: Some(ImageURLWire {
                url: url.to_string(),
            }),
        });
    }
    Err(loss(
        path,
        "image",
        LossReason::UnsupportedSemantic,
        "IR image has no supported Chat Completions image_url equivalent",
    ))
}

/// N-CC-8 renders normal user content as a string when text-only and as a
/// parts array when it contains images.
pub(crate) fn encode_user_content(blocks: &[Block], path: &str) -> (ContentValue, Vec<Loss>) {
    let mut parts: Vec<ContentPart> = Vec::new();
    let mut text = String::new();
    let mut has_image = false;
    let mut losses = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        match block {
            Block::Text { text: value } => {
                parts.push(ContentPart {
                    kind: "text".to_string(),
                    text: value.clone(),
                    image_url: None,
                });
                text.push_str(value);
            }
            Block::Image {
                media_type,
                data,
                url,
            } => match encode_image_block(
                media_type.as_deref(),
                data.as_deref(),
                url.as_deref(),
                &format!("{path}[{index}]"),
            ) {
                Ok(part) => {
                    has_image = true;
                    parts.push(part);
                }
                Err(image_loss) => losses.push(image_loss),
            },
            _ => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in a Chat Completions user message",
            )),
        }
    }
    if has_image {
        (ContentValue::Parts(parts), losses)
    } else {
        (ContentValue::Text(text), losses)
    }
}

/// N-CC-9 renders assistant text and tool_use blocks, preserving the opaque
/// function.arguments text (INV-1; the Rust IR input already carries the raw
/// text without its JSON string envelope).
pub(crate) fn encode_assistant_message(blocks: &[Block], path: &str) -> (Message, Vec<Loss>) {
    let mut out = Message {
        role: "assistant".to_string(),
        ..Message::default()
    };
    let mut text = String::new();
    let mut losses = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        match block {
            Block::Text { text: value } => text.push_str(value),
            Block::ToolUse { id, name, input } => {
                out.tool_calls.get_or_insert_with(Vec::new).push(ToolCall {
                    id: id.clone(),
                    kind: "function".to_string(),
                    function: FunctionWire {
                        name: name.clone(),
                        description: String::new(),
                        parameters: None,
                        arguments: input.clone(),
                    },
                });
            }
            Block::Image { .. } => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "IR image block cannot be rendered in a Chat Completions assistant message",
            )),
            Block::ToolResult { .. } => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "IR tool_result block cannot be rendered in a Chat Completions assistant message",
            )),
        }
    }
    out.content = Some(ContentValue::Text(text));
    (out, losses)
}

/// N-CC-10 renders tool_result blocks as role:tool messages and reports
/// result content that Chat Completions cannot represent instead of panicking.
pub(crate) fn encode_tool_result(block: &Block, path: &str) -> (Message, Vec<Loss>) {
    let Block::ToolResult {
        tool_use_id,
        content,
        is_error,
    } = block
    else {
        return (
            Message {
                role: "tool".to_string(),
                ..Message::default()
            },
            vec![loss(
                path,
                "content",
                LossReason::UnsupportedSemantic,
                "IR block is not a tool_result block",
            )],
        );
    };
    let mut text = String::new();
    let mut losses = Vec::new();
    for (index, inner) in content.iter().enumerate() {
        match inner {
            Block::Text { text: value } => text.push_str(value),
            _ => losses.push(loss(
                format!("{path}.content[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in a Chat Completions tool result",
            )),
        }
    }
    if is_error == &Some(true) {
        losses.push(loss(
            format!("{path}.is_error"),
            "is_error",
            LossReason::UnmappedField,
            "Chat Completions tool messages have no is_error field",
        ));
    }
    (
        Message {
            role: "tool".to_string(),
            content: Some(ContentValue::Text(text)),
            tool_call_id: tool_use_id.clone(),
            ..Message::default()
        },
        losses,
    )
}

pub(crate) fn content_system(blocks: Vec<Block>, path: &str) -> (Vec<SystemBlock>, Vec<Loss>) {
    let mut out = Vec::with_capacity(blocks.len());
    let mut losses = Vec::new();
    for (index, block) in blocks.into_iter().enumerate() {
        match block {
            Block::Text { text } => out.push(SystemBlock::Text { text }),
            _ => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in the IR system field",
            )),
        }
    }
    (out, losses)
}
