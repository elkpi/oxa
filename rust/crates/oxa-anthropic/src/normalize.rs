//! Normalization rules N-AN-1 through N-AN-6 (spec/12 §5) and the shared
//! INV-1 input helpers.

use serde_json::value::RawValue;

use oxa_ir::{Block, Loss, LossReason, SystemBlock, ToolChoice, ToolChoiceMode};

use crate::error::Error;
use crate::types::{
    BLOCK_TYPE_IMAGE, BLOCK_TYPE_TEXT, BLOCK_TYPE_TOOL_RESULT, BLOCK_TYPE_TOOL_USE, BlockWire,
    ContentValue, SOURCE_TYPE_BASE64, SOURCE_TYPE_URL, SystemBlockWire, TOOL_CHOICE_TYPE_ANY,
    TOOL_CHOICE_TYPE_AUTO, TOOL_CHOICE_TYPE_NONE, TOOL_CHOICE_TYPE_TOOL, ToolChoiceWire,
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

/// Converts a wire `tool_use.input` raw JSON object into the IR string form
/// (INV-1): the exact source bytes become the string payload. The object
/// bytes are never parsed or re-serialized as an object.
pub(crate) fn input_to_ir_string(raw: &RawValue) -> String {
    raw.get().to_string()
}

/// Converts an IR `tool_use.input` string back into the raw JSON object bytes
/// it carries, verbatim (INV-1). The payload must be a JSON object.
pub(crate) fn input_from_ir_string(input: &str, path: &str) -> Result<Box<RawValue>, Error> {
    if input.is_empty() {
        return Err(Error::new("anthropic: tool_use input is required"));
    }
    let raw = RawValue::from_string(input.to_string())
        .map_err(|_| Error::new(format!("anthropic: {path}.input is not valid JSON")))?;
    require_json_object(raw.get(), &format!("{path}.input"))?;
    Ok(raw)
}

/// Requires the raw JSON text to be one JSON object (Go `requireJSONObject`).
pub(crate) fn require_json_object(raw: &str, path: &str) -> Result<(), Error> {
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err(Error::new(format!("anthropic: {path} is required")));
    }
    if !trimmed.starts_with('{') || !trimmed.ends_with('}') {
        return Err(Error::new(format!(
            "anthropic: {path} must be a JSON object"
        )));
    }
    Ok(())
}

/// Decodes wire system content: a string becomes one text block; the block
/// array form maps near-identically with cache_control losses.
pub(crate) fn decode_system(
    system: Option<&crate::types::SystemValue>,
) -> Result<(Vec<SystemBlock>, Vec<Loss>), Error> {
    let Some(system) = system else {
        return Ok((Vec::new(), Vec::new()));
    };
    match system {
        crate::types::SystemValue::Text(text) => {
            Ok((vec![SystemBlock::Text { text: text.clone() }], Vec::new()))
        }
        crate::types::SystemValue::Blocks(blocks) => {
            let mut out = Vec::with_capacity(blocks.len());
            let mut losses = Vec::new();
            for (index, block) in blocks.iter().enumerate() {
                if block.kind != BLOCK_TYPE_TEXT {
                    return Err(Error::new(format!(
                        "anthropic: system[{index}]: unsupported block type {:?}",
                        block.kind
                    )));
                }
                out.push(SystemBlock::Text {
                    text: block.text.clone(),
                });
                if block.cache_control.is_some() {
                    losses.push(loss(
                        format!("system[{index}].cache_control"),
                        "cache_control",
                        LossReason::UnmappedField,
                        "Anthropic prompt caching annotations have no IR equivalent in v1.",
                    ));
                }
            }
            Ok((out, losses))
        }
    }
}

/// Renders IR system blocks as the wire block array form.
pub(crate) fn encode_system(system: &[SystemBlock]) -> Vec<SystemBlockWire> {
    system
        .iter()
        .map(|block| match block {
            SystemBlock::Text { text } => SystemBlockWire {
                kind: BLOCK_TYPE_TEXT.to_string(),
                text: text.clone(),
                cache_control: None,
            },
        })
        .collect()
}

/// Decodes wire message content: string or block array; a missing content is
/// a structural error.
pub(crate) fn decode_content(
    content: Option<&ContentValue>,
    path: &str,
) -> Result<(Vec<Block>, Vec<Loss>), Error> {
    let Some(content) = content else {
        return Err(Error::new(format!("anthropic: {path} is missing")));
    };
    match content {
        ContentValue::Text(text) => Ok((vec![Block::Text { text: text.clone() }], Vec::new())),
        ContentValue::Blocks(blocks) => {
            let mut out = Vec::with_capacity(blocks.len());
            let mut losses = Vec::new();
            for (index, block) in blocks.iter().enumerate() {
                let (decoded, block_losses, mapped) =
                    decode_block(block, &format!("{path}[{index}]"))?;
                if mapped {
                    out.extend(decoded);
                }
                losses.extend(block_losses);
            }
            Ok((out, losses))
        }
    }
}

/// Decodes one wire block. Returns the IR blocks (empty when the block was
/// dropped), the block's losses, and whether the block mapped at all (unknown
/// block types are dropped with a loss, N-AN-5).
pub(crate) fn decode_block(
    wire: &BlockWire,
    path: &str,
) -> Result<(Vec<Block>, Vec<Loss>, bool), Error> {
    let mut block: Option<Block> = None;
    let mut losses: Vec<Loss> = Vec::new();
    match wire.kind.as_str() {
        BLOCK_TYPE_TEXT => {
            block = Some(Block::Text {
                text: wire.text.clone(),
            });
        }
        BLOCK_TYPE_IMAGE => {
            let Some(source) = wire.source.as_ref() else {
                return Err(Error::new(format!("anthropic: {path}.source is required")));
            };
            match source.kind.as_str() {
                SOURCE_TYPE_BASE64 => {
                    if source.media_type.is_empty() {
                        return Err(Error::new(format!(
                            "anthropic: {path}.source.media_type is required"
                        )));
                    }
                    if source.data.is_empty() {
                        return Err(Error::new(format!(
                            "anthropic: {path}.source.data is required"
                        )));
                    }
                    block = Some(Block::Image {
                        media_type: Some(source.media_type.clone()),
                        data: Some(source.data.clone()),
                        url: None,
                    });
                }
                SOURCE_TYPE_URL => {
                    if source.url.is_empty() {
                        return Err(Error::new(format!(
                            "anthropic: {path}.source.url is required"
                        )));
                    }
                    block = Some(Block::Image {
                        media_type: None,
                        data: None,
                        url: Some(source.url.clone()),
                    });
                }
                other => losses.push(loss(
                    format!("{path}.source"),
                    "type",
                    LossReason::UnsupportedSemantic,
                    format!("Anthropic image source type {other:?} has no IR equivalent"),
                )),
            }
        }
        BLOCK_TYPE_TOOL_USE => {
            if wire.id.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.id is required")));
            }
            if wire.name.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.name is required")));
            }
            let raw = wire.input.as_deref().map(RawValue::get).unwrap_or_default();
            require_json_object(raw, &format!("{path}.input"))?;
            block = Some(Block::ToolUse {
                id: wire.id.clone(),
                name: wire.name.clone(),
                input: input_to_ir_string(wire.input.as_deref().expect("input checked above")),
            });
        }
        BLOCK_TYPE_TOOL_RESULT => {
            if wire.tool_use_id.is_empty() {
                return Err(Error::new(format!(
                    "anthropic: {path}.tool_use_id is required"
                )));
            }
            let (content, content_losses) =
                decode_content(wire.content.as_ref(), &format!("{path}.content"))?;
            losses.extend(content_losses);
            block = Some(Block::ToolResult {
                tool_use_id: wire.tool_use_id.clone(),
                content,
                is_error: wire.is_error.then_some(true),
            });
        }
        other => {
            losses.push(loss(
                path,
                "type",
                LossReason::UnsupportedSemantic,
                format!("Anthropic block type {other:?} has no IR equivalent"),
            ));
            return Ok((Vec::new(), losses, false));
        }
    }
    if wire.cache_control.is_some() {
        losses.push(loss(
            format!("{path}.cache_control"),
            "cache_control",
            LossReason::UnmappedField,
            "Anthropic prompt caching annotations have no IR equivalent in v1.",
        ));
    }
    Ok((block.into_iter().collect(), losses, true))
}

/// Decodes the wire tool_choice object (N-AN-6).
pub(crate) fn decode_tool_choice(
    choice: Option<&ToolChoiceWire>,
) -> Result<(Option<ToolChoice>, Vec<Loss>), Error> {
    let Some(choice) = choice else {
        return Ok((None, Vec::new()));
    };
    let mut decoded = None;
    let mut losses = Vec::new();
    match choice.kind.as_str() {
        TOOL_CHOICE_TYPE_AUTO => {
            decoded = Some(ToolChoice {
                mode: ToolChoiceMode::Auto,
                name: None,
            })
        }
        TOOL_CHOICE_TYPE_ANY => {
            decoded = Some(ToolChoice {
                mode: ToolChoiceMode::Any,
                name: None,
            })
        }
        TOOL_CHOICE_TYPE_NONE => {
            decoded = Some(ToolChoice {
                mode: ToolChoiceMode::None,
                name: None,
            })
        }
        TOOL_CHOICE_TYPE_TOOL => {
            if choice.name.is_empty() {
                return Err(Error::new(
                    "anthropic: tool_choice.name is required for type tool",
                ));
            }
            decoded = Some(ToolChoice {
                mode: ToolChoiceMode::Tool,
                name: Some(choice.name.clone()),
            });
        }
        other => losses.push(loss(
            "tool_choice",
            "type",
            LossReason::UnsupportedSemantic,
            format!("Anthropic tool_choice type {other:?} has no IR equivalent"),
        )),
    }
    if choice.disable_parallel_tool_use {
        losses.push(loss(
            "tool_choice.disable_parallel_tool_use",
            "disable_parallel_tool_use",
            LossReason::UnmappedField,
            "Anthropic disable_parallel_tool_use has no IR equivalent in v1.",
        ));
    }
    Ok((decoded, losses))
}

/// Renders the IR tool_choice modes in wire form.
pub(crate) fn encode_tool_choice(
    choice: Option<&ToolChoice>,
) -> Result<(Option<ToolChoiceWire>, Vec<Loss>), Error> {
    let Some(choice) = choice else {
        return Ok((None, Vec::new()));
    };
    let wire = match choice.mode {
        ToolChoiceMode::Auto => ToolChoiceWire {
            kind: TOOL_CHOICE_TYPE_AUTO.to_string(),
            ..ToolChoiceWire::default()
        },
        ToolChoiceMode::Any => ToolChoiceWire {
            kind: TOOL_CHOICE_TYPE_ANY.to_string(),
            ..ToolChoiceWire::default()
        },
        ToolChoiceMode::None => ToolChoiceWire {
            kind: TOOL_CHOICE_TYPE_NONE.to_string(),
            ..ToolChoiceWire::default()
        },
        ToolChoiceMode::Tool => {
            let Some(name) = choice.name.as_deref().filter(|name| !name.is_empty()) else {
                return Err(Error::new(
                    "anthropic: tool_choice.name is required for mode tool",
                ));
            };
            ToolChoiceWire {
                kind: TOOL_CHOICE_TYPE_TOOL.to_string(),
                name: name.to_string(),
                disable_parallel_tool_use: false,
            }
        }
    };
    Ok((Some(wire), Vec::new()))
}

pub(crate) fn invalid_image_loss(path: &str, detail: &str) -> Loss {
    loss(
        path,
        "image",
        LossReason::UnsupportedSemantic,
        detail.to_string(),
    )
}

/// Records an IR block that Anthropic cannot represent at the given position.
pub(crate) fn unsupported_block_loss(path: &str, detail: String) -> Loss {
    loss(path, "type", LossReason::UnsupportedSemantic, detail)
}
