//! Normalization rules N-R-1 through N-R-10 (spec/11 §5).

use serde_json::{Value, json};

use oxa_ir::{
    Block, Loss, LossReason, Message as IrMessage, Role, SystemBlock, ToolChoice, ToolChoiceMode,
};

use crate::error::Error;
use crate::types::{
    ContentPart, ContentValue, ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_FUNCTION_CALL_OUTPUT,
    ITEM_TYPE_MESSAGE, InputItem, PART_TYPE_INPUT_IMAGE, PART_TYPE_INPUT_TEXT, ROLE_ASSISTANT,
    ROLE_USER, TOOL_CHOICE_AUTO, TOOL_CHOICE_NONE, TOOL_CHOICE_REQUIRED, TOOL_TYPE_FUNCTION,
};

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

/// N-R-3 normalizes Responses string and parts-array content into IR blocks.
pub(crate) fn decode_content(
    content: Option<&ContentValue>,
    path: &str,
) -> (Vec<Block>, Vec<Loss>) {
    let Some(content) = content else {
        return (
            vec![Block::Text {
                text: String::new(),
            }],
            Vec::new(),
        );
    };
    match content {
        ContentValue::Text(text) => (vec![Block::Text { text: text.clone() }], Vec::new()),
        ContentValue::Parts(parts) => {
            let mut blocks = Vec::with_capacity(parts.len());
            let mut losses = Vec::new();
            for (index, part) in parts.iter().enumerate() {
                let (decoded, part_losses) = decode_content_part(part, path, index);
                blocks.extend(decoded);
                losses.extend(part_losses);
            }
            (blocks, losses)
        }
    }
}

fn decode_content_part(part: &ContentPart, path: &str, index: usize) -> (Vec<Block>, Vec<Loss>) {
    match part.kind.as_str() {
        PART_TYPE_INPUT_TEXT => (
            vec![Block::Text {
                text: part.text.clone(),
            }],
            Vec::new(),
        ),
        PART_TYPE_INPUT_IMAGE => {
            match decode_image_url(&part.image_url, &format!("{path}[{index}].image_url")) {
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
                format!("Responses input content part type {other:?} has no IR equivalent"),
            )],
        ),
    }
}

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

/// N-R-5 merges assistant items and function-call items into one assistant
/// message, with ordinary content before tool-use blocks.
pub(crate) fn decode_assistant_run(
    items: &[InputItem],
    start: usize,
) -> Result<(Option<IrMessage>, usize, Vec<Loss>), Error> {
    let mut text = Vec::new();
    let mut calls = Vec::new();
    let mut losses = Vec::new();
    let mut index = start;
    while index < items.len() {
        let item = &items[index];
        if item.kind == ITEM_TYPE_FUNCTION_CALL {
            calls.push(Block::ToolUse {
                id: item.call_id.clone(),
                name: item.name.clone(),
                input: item.arguments.clone(),
            });
            index += 1;
            continue;
        }
        if !(item.kind.is_empty() || item.kind == ITEM_TYPE_MESSAGE) || item.role != ROLE_ASSISTANT
        {
            break;
        }
        let (blocks, block_losses) =
            decode_content(item.content.as_ref(), &format!("input[{index}].content"));
        text.extend(blocks);
        losses.extend(block_losses);
        index += 1;
    }
    text.extend(calls);
    if text.is_empty() {
        return Ok((None, index, losses));
    }
    Ok((
        Some(IrMessage {
            role: Role::Assistant,
            content: text,
        }),
        index,
        losses,
    ))
}

/// N-R-6 merges a maximal function_call_output run into one user message.
pub(crate) fn decode_output_run(items: &[InputItem], start: usize) -> (IrMessage, usize) {
    let mut content = Vec::new();
    let mut index = start;
    while index < items.len() && items[index].kind == ITEM_TYPE_FUNCTION_CALL_OUTPUT {
        content.push(Block::ToolResult {
            tool_use_id: items[index].call_id.clone(),
            content: vec![Block::Text {
                text: items[index].output.clone(),
            }],
            is_error: None,
        });
        index += 1;
    }
    (
        IrMessage {
            role: Role::User,
            content,
        },
        index,
    )
}

/// N-R-9 maps Responses tool choice forms to the IR modes.
pub(crate) fn decode_tool_choice(value: Option<&Value>) -> (Option<ToolChoice>, Vec<Loss>) {
    let Some(value) = value else {
        return (None, Vec::new());
    };
    match value {
        Value::String(choice) => match choice.as_str() {
            TOOL_CHOICE_AUTO => (
                Some(ToolChoice {
                    mode: ToolChoiceMode::Auto,
                    name: None,
                }),
                Vec::new(),
            ),
            TOOL_CHOICE_NONE => (
                Some(ToolChoice {
                    mode: ToolChoiceMode::None,
                    name: None,
                }),
                Vec::new(),
            ),
            TOOL_CHOICE_REQUIRED => (
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
                    format!("Responses tool_choice {other:?} has no IR equivalent"),
                )],
            ),
        },
        Value::Object(choice) => {
            let kind = choice
                .get("type")
                .and_then(Value::as_str)
                .unwrap_or_default();
            let name = choice
                .get("name")
                .and_then(Value::as_str)
                .unwrap_or_default();
            if kind == TOOL_TYPE_FUNCTION && !name.is_empty() {
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
                        "only named function Responses tool_choice values are supported",
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
                "Responses tool_choice has no IR equivalent",
            )],
        ),
    }
}

/// N-R-9 renders the IR's four tool-choice modes in Responses form.
pub(crate) fn encode_tool_choice(choice: Option<&ToolChoice>) -> (Option<Value>, Option<Loss>) {
    let Some(choice) = choice else {
        return (None, None);
    };
    match choice.mode {
        ToolChoiceMode::Auto => (Some(json!(TOOL_CHOICE_AUTO)), None),
        ToolChoiceMode::None => (Some(json!(TOOL_CHOICE_NONE)), None),
        ToolChoiceMode::Any => (Some(json!(TOOL_CHOICE_REQUIRED)), None),
        ToolChoiceMode::Tool => match choice.name.as_deref() {
            Some(name) if !name.is_empty() => (
                Some(json!({ "type": TOOL_TYPE_FUNCTION, "name": name })),
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

/// Renders an IR image as a Responses input_image part.
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
                "IR image media type has no Responses input_image equivalent",
            ));
        }
        return Ok(ContentPart {
            kind: PART_TYPE_INPUT_IMAGE.to_string(),
            text: String::new(),
            image_url: format!("data:{media_type};base64,{data}"),
        });
    }
    if let Some(url) = url
        && is_valid_https_url(url)
    {
        return Ok(ContentPart {
            kind: PART_TYPE_INPUT_IMAGE.to_string(),
            text: String::new(),
            image_url: url.to_string(),
        });
    }
    Err(loss(
        path,
        "image",
        LossReason::UnsupportedSemantic,
        "IR image has no supported Responses input_image equivalent",
    ))
}

/// N-R-3 and N-R-4 render user content as string shorthand only for exactly
/// one text block; otherwise the supported content parts array is used.
pub(crate) fn encode_user_content(blocks: &[Block], path: &str) -> (ContentValue, Vec<Loss>) {
    if let [Block::Text { text }] = blocks {
        return (ContentValue::Text(text.clone()), Vec::new());
    }
    let mut parts = Vec::new();
    let mut losses = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        match block {
            Block::Text { text } => parts.push(ContentPart {
                kind: PART_TYPE_INPUT_TEXT.to_string(),
                text: text.clone(),
                image_url: String::new(),
            }),
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
                Ok(part) => parts.push(part),
                Err(image_loss) => losses.push(image_loss),
            },
            _ => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in a Responses user message",
            )),
        }
    }
    (ContentValue::Parts(parts), losses)
}

/// N-R-10 hoists function_call_output items ahead of regular user content.
pub(crate) fn encode_user_message(message: &IrMessage, path: &str) -> (Vec<InputItem>, Vec<Loss>) {
    let mut normal = Vec::new();
    let mut results = Vec::new();
    let mut first_normal = None;
    let mut last_result = None;
    for (index, block) in message.content.iter().enumerate() {
        if matches!(block, Block::ToolResult { .. }) {
            results.push(block.clone());
            last_result = Some(index);
        } else {
            first_normal.get_or_insert(index);
            normal.push(block.clone());
        }
    }
    let mut losses = Vec::new();
    if let (Some(first), Some(last)) = (first_normal, last_result)
        && first < last
    {
        losses.push(loss(
            format!("{path}.content"),
            "ordering",
            LossReason::Degraded,
            "N-R-10: function_call_output items are hoisted ahead of the trailing user content; source order is not preserved",
        ));
    }
    let mut items = Vec::new();
    for (index, block) in results.iter().enumerate() {
        let Block::ToolResult {
            tool_use_id,
            content,
            is_error,
        } = block
        else {
            unreachable!("results contains only tool result blocks");
        };
        let mut output = String::new();
        for (content_index, inner) in content.iter().enumerate() {
            match inner {
                Block::Text { text } => output.push_str(text),
                _ => losses.push(loss(
                    format!("{path}.content[{index}].content[{content_index}]"),
                    "content",
                    LossReason::UnsupportedSemantic,
                    "this IR block cannot be rendered in a Responses function_call_output item",
                )),
            }
        }
        if *is_error == Some(true) {
            losses.push(loss(
                format!("{path}.content[{index}].is_error"),
                "is_error",
                LossReason::UnmappedField,
                "Responses function_call_output items have no is_error field",
            ));
        }
        items.push(InputItem {
            kind: ITEM_TYPE_FUNCTION_CALL_OUTPUT.to_string(),
            call_id: tool_use_id.clone(),
            output,
            ..InputItem::default()
        });
    }
    if !normal.is_empty() || results.is_empty() {
        let (content, content_losses) = encode_user_content(&normal, &format!("{path}.content"));
        items.push(InputItem {
            role: ROLE_USER.to_string(),
            content: Some(content),
            ..InputItem::default()
        });
        losses.extend(content_losses);
    }
    (items, losses)
}

/// N-R-5 renders assistant text before function_call items and preserves the
/// opaque arguments text without parsing or re-serializing it.
pub(crate) fn encode_assistant_message(
    blocks: &[Block],
    path: &str,
) -> (Vec<InputItem>, Vec<Loss>) {
    let mut text = String::new();
    let mut has_text = false;
    let mut calls = Vec::new();
    let mut losses = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        match block {
            Block::Text { text: value } => {
                text.push_str(value);
                has_text = true;
            }
            Block::ToolUse { id, name, input } => calls.push(InputItem {
                kind: ITEM_TYPE_FUNCTION_CALL.to_string(),
                call_id: id.clone(),
                name: name.clone(),
                arguments: input.clone(),
                ..InputItem::default()
            }),
            Block::Image { .. } | Block::ToolResult { .. } => losses.push(loss(
                format!("{path}[{index}]"),
                "content",
                LossReason::UnsupportedSemantic,
                "this IR block cannot be rendered in a Responses assistant input item",
            )),
        }
    }
    let mut items = Vec::new();
    if has_text || blocks.is_empty() {
        items.push(InputItem {
            role: ROLE_ASSISTANT.to_string(),
            content: Some(ContentValue::Text(text)),
            ..InputItem::default()
        });
    }
    items.extend(calls);
    (items, losses)
}

pub(crate) fn content_system(blocks: Vec<Block>, path: &str) -> (Vec<SystemBlock>, Vec<Loss>) {
    let mut out = Vec::new();
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
