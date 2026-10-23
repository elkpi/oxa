//! Normalization rules N-AN-1 through N-AN-6 (spec/12 §5) and the shared
//! INV-1 input helpers.

use std::collections::BTreeSet;

use serde_json::value::RawValue;

use oxa_ir::{Block, Loss, LossReason, SystemBlock, ToolChoice, ToolChoiceMode};

use crate::error::Error;
use crate::types::{
    BLOCK_TYPE_DOCUMENT, BLOCK_TYPE_IMAGE, BLOCK_TYPE_TEXT, BLOCK_TYPE_THINKING,
    BLOCK_TYPE_TOOL_RESULT, BLOCK_TYPE_TOOL_USE, BlockWire, ContentValue, SOURCE_TYPE_BASE64,
    SOURCE_TYPE_FILE, SOURCE_TYPE_URL, SystemBlockWire, TOOL_CHOICE_TYPE_ANY,
    TOOL_CHOICE_TYPE_AUTO, TOOL_CHOICE_TYPE_NONE, TOOL_CHOICE_TYPE_TOOL, ToolChoiceWire,
};

pub(crate) fn extract_custom_input(raw: &str) -> Option<String> {
    let value: serde_json::Value = serde_json::from_str(raw).ok()?;
    let obj = value.as_object()?;
    if obj.len() == 1
        && let Some(serde_json::Value::String(s)) = obj.get("input")
    {
        return Some(s.clone());
    }
    None
}

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
        crate::types::SystemValue::Text(text) => Ok((
            vec![SystemBlock::Text {
                text: text.clone(),
                native: Vec::new(),
            }],
            Vec::new(),
        )),
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
                let mut native = Vec::new();
                if let Some(ref cc) = block.cache_control {
                    let path = format!("system[{index}].cache_control");
                    let (canon, l) = crate::native::parse_cache_control(cc, &path);
                    if let Some(loss) = l {
                        losses.push(loss);
                    }
                    if let Some(val) = canon {
                        native.push(oxa_ir::NativeAnnotation {
                            source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                            kind: oxa_ir::NATIVE_KIND_CACHE_CONTROL.into(),
                            value: val,
                            omission: Some(oxa_ir::NativeOmission {
                                field: "cache_control".into(),
                                reason: LossReason::UnmappedField,
                                detail: Some(crate::native::CACHE_CONTROL_LOSS_DETAIL.into()),
                            }),
                            projection: None,
                        });
                    }
                }
                out.push(SystemBlock::Text {
                    text: block.text.clone(),
                    native,
                });
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
            SystemBlock::Text { text, native } => SystemBlockWire {
                kind: BLOCK_TYPE_TEXT.to_string(),
                text: text.clone(),
                cache_control: crate::native::get_block_cache_control(native),
            },
        })
        .collect()
}

/// Decodes wire message content: string or block array; a missing content is
/// a structural error.
pub(crate) fn decode_content(
    content: Option<&ContentValue>,
    path: &str,
    custom_tools: &BTreeSet<String>,
    custom_call_ids: &mut BTreeSet<String>,
    declared_tools: &BTreeSet<String>,
    is_request: bool,
) -> Result<(Vec<Block>, Vec<Loss>), Error> {
    let Some(content) = content else {
        return Err(Error::new(format!("anthropic: {path} is missing")));
    };
    match content {
        ContentValue::Text(text) => Ok((
            vec![Block::Text {
                text: text.clone(),
                native: Vec::new(),
            }],
            Vec::new(),
        )),
        ContentValue::Blocks(blocks) => {
            let mut out = Vec::with_capacity(blocks.len());
            let mut losses = Vec::new();
            for (index, block) in blocks.iter().enumerate() {
                let (decoded, block_losses, mapped) = decode_block(
                    block,
                    &format!("{path}[{index}]"),
                    custom_tools,
                    custom_call_ids,
                    declared_tools,
                    is_request,
                )?;
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
    custom_tools: &BTreeSet<String>,
    custom_call_ids: &mut BTreeSet<String>,
    declared_tools: &BTreeSet<String>,
    is_request: bool,
) -> Result<(Vec<Block>, Vec<Loss>, bool), Error> {
    let mut block: Option<Block> = None;
    let mut losses: Vec<Loss> = Vec::new();
    match wire.kind.as_str() {
        BLOCK_TYPE_TEXT => {
            block = Some(Block::Text {
                text: wire.text.clone(),
                native: Vec::new(),
            });
        }
        BLOCK_TYPE_THINKING => {
            block = Some(Block::Thinking {
                thinking: wire.thinking.clone().unwrap_or_default(),
                signature: wire.signature.clone(),
                native: Vec::new(),
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
                        native: Vec::new(),
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
                        native: Vec::new(),
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
        BLOCK_TYPE_DOCUMENT => {
            if !is_request {
                losses.push(loss(
                    path.to_string(),
                    "type",
                    LossReason::UnsupportedSemantic,
                    "Anthropic block type \"document\" has no IR equivalent".to_string(),
                ));
                return Ok((Vec::new(), losses, false));
            }
            let Some(source) = wire.source.as_ref() else {
                return Err(Error::new(format!("anthropic: {path}.source is required")));
            };
            if source.kind.is_empty() {
                return Err(Error::new(format!(
                    "anthropic: {path}.source.type is required"
                )));
            }
            let mut doc = match source.kind.as_str() {
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
                    if source.has_url
                        || !source.url.is_empty()
                        || source.has_file_id
                        || !source.file_id.is_empty()
                    {
                        return Err(Error::new(format!(
                            "anthropic: {path}.source has conflicting source fields"
                        )));
                    }
                    Block::Document {
                        media_type: Some(source.media_type.clone()),
                        data: Some(source.data.clone()),
                        url: None,
                        file_id: None,
                        native: Vec::new(),
                    }
                }
                SOURCE_TYPE_URL => {
                    if source.url.is_empty() {
                        return Err(Error::new(format!(
                            "anthropic: {path}.source.url is required"
                        )));
                    }
                    if source.has_media_type
                        || !source.media_type.is_empty()
                        || source.has_data
                        || !source.data.is_empty()
                        || source.has_file_id
                        || !source.file_id.is_empty()
                    {
                        return Err(Error::new(format!(
                            "anthropic: {path}.source has conflicting source fields"
                        )));
                    }
                    Block::Document {
                        media_type: None,
                        data: None,
                        url: Some(source.url.clone()),
                        file_id: None,
                        native: Vec::new(),
                    }
                }
                SOURCE_TYPE_FILE => {
                    if source.file_id.is_empty() {
                        return Err(Error::new(format!(
                            "anthropic: {path}.source.file_id is required"
                        )));
                    }
                    if source.has_media_type
                        || !source.media_type.is_empty()
                        || source.has_data
                        || !source.data.is_empty()
                        || source.has_url
                        || !source.url.is_empty()
                    {
                        return Err(Error::new(format!(
                            "anthropic: {path}.source has conflicting source fields"
                        )));
                    }
                    Block::Document {
                        media_type: None,
                        data: None,
                        url: None,
                        file_id: Some(source.file_id.clone()),
                        native: Vec::new(),
                    }
                }
                other => {
                    losses.push(loss(
                        path.to_string(),
                        "type",
                        LossReason::UnsupportedSemantic,
                        format!("Anthropic document source type {other:?} has no IR equivalent"),
                    ));
                    return Ok((Vec::new(), losses, false));
                }
            };
            let Block::Document { native, .. } = &mut doc else {
                unreachable!("document branch constructed a Document block");
            };
            if let Some(title) = wire.title.as_ref() {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                    kind: oxa_ir::NATIVE_KIND_DOCUMENT_TITLE.into(),
                    value: serde_json::to_string(title).map_err(|e| Error::new(e.to_string()))?,
                    omission: Some(oxa_ir::NativeOmission {
                        field: "title".into(),
                        reason: LossReason::UnmappedField,
                        detail: Some(oxa_ir::DOCUMENT_TITLE_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                });
            }
            if let Some(context) = wire.context.as_ref() {
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                    kind: oxa_ir::NATIVE_KIND_DOCUMENT_CONTEXT.into(),
                    value: serde_json::to_string(context).map_err(|e| Error::new(e.to_string()))?,
                    omission: Some(oxa_ir::NativeOmission {
                        field: "context".into(),
                        reason: LossReason::UnmappedField,
                        detail: Some(oxa_ir::DOCUMENT_CONTEXT_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                });
            }
            if let Some(citations) = wire.citations.as_ref() {
                let Some(obj) = citations.as_object() else {
                    return Err(Error::new(format!(
                        "anthropic: {path}.citations must be a JSON object"
                    )));
                };
                if let Some(enabled) = obj.get("enabled")
                    && !enabled.is_boolean()
                {
                    return Err(Error::new(format!(
                        "anthropic: {path}.citations.enabled must be a boolean"
                    )));
                }
                native.push(oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                    kind: oxa_ir::NATIVE_KIND_DOCUMENT_CITATIONS.into(),
                    value: serde_json::to_string(citations)
                        .map_err(|e| Error::new(e.to_string()))?,
                    omission: Some(oxa_ir::NativeOmission {
                        field: "citations".into(),
                        reason: LossReason::UnsupportedSemantic,
                        detail: Some(oxa_ir::DOCUMENT_CITATIONS_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                });
            }
            block = Some(doc);
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
            let custom_input = extract_custom_input(raw);
            let is_custom = if custom_tools.contains(&wire.name) {
                true
            } else if declared_tools.contains(&wire.name) {
                false
            } else {
                custom_input.is_some()
            };
            if is_custom {
                custom_call_ids.insert(wire.id.clone());
                block = Some(Block::CustomToolUse {
                    id: wire.id.clone(),
                    name: wire.name.clone(),
                    input: custom_input.unwrap_or_default(),
                    native: Vec::new(),
                });
            } else {
                block = Some(Block::ToolUse {
                    id: wire.id.clone(),
                    name: wire.name.clone(),
                    input: input_to_ir_string(wire.input.as_deref().expect("input checked above")),
                    native: Vec::new(),
                });
            }
        }
        BLOCK_TYPE_TOOL_RESULT => {
            if wire.tool_use_id.is_empty() {
                return Err(Error::new(format!(
                    "anthropic: {path}.tool_use_id is required"
                )));
            }
            if custom_call_ids.contains(&wire.tool_use_id) {
                let output = match wire.content.as_ref() {
                    Some(ContentValue::Text(t)) => oxa_ir::CustomToolOutput::Text(t.clone()),
                    _ => {
                        let (content, content_losses) = decode_content(
                            wire.content.as_ref(),
                            &format!("{path}.content"),
                            custom_tools,
                            custom_call_ids,
                            declared_tools,
                            is_request,
                        )?;
                        losses.extend(content_losses);
                        oxa_ir::CustomToolOutput::Content(content)
                    }
                };
                block = Some(Block::CustomToolResult {
                    tool_use_id: wire.tool_use_id.clone(),
                    output,
                    native: Vec::new(),
                });
            } else {
                let (content, content_losses) = decode_content(
                    wire.content.as_ref(),
                    &format!("{path}.content"),
                    custom_tools,
                    custom_call_ids,
                    declared_tools,
                    is_request,
                )?;
                losses.extend(content_losses);
                block = Some(Block::ToolResult {
                    tool_use_id: wire.tool_use_id.clone(),
                    content,
                    is_error: wire.is_error.then_some(true),
                    native: Vec::new(),
                });
            }
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
    if let Some(ref cc) = wire.cache_control {
        if is_request {
            let (canon, l) =
                crate::native::parse_cache_control(cc, &format!("{path}.cache_control"));
            if let Some(loss) = l {
                losses.push(loss);
            }
            if let Some(val) = canon
                && let Some(ref mut b) = block
            {
                let ann = oxa_ir::NativeAnnotation {
                    source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                    kind: oxa_ir::NATIVE_KIND_CACHE_CONTROL.into(),
                    value: val,
                    omission: Some(oxa_ir::NativeOmission {
                        field: "cache_control".into(),
                        reason: LossReason::UnmappedField,
                        detail: Some(crate::native::CACHE_CONTROL_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                };
                match b {
                    Block::Text { native, .. }
                    | Block::Thinking { native, .. }
                    | Block::Image { native, .. }
                    | Block::Document { native, .. }
                    | Block::ToolUse { native, .. }
                    | Block::CustomToolUse { native, .. }
                    | Block::ApplyPatchToolUse { native, .. }
                    | Block::ApplyPatchToolResult { native, .. }
                    | Block::ToolResult { native, .. }
                    | Block::CustomToolResult { native, .. } => {
                        native.push(ann);
                    }
                }
            }
        } else {
            let (canon, l) =
                crate::native::parse_cache_control(cc, &format!("{path}.cache_control"));
            if let Some(loss) = l {
                losses.push(loss);
            }
            if canon.is_some() {
                losses.push(loss(
                    format!("{path}.cache_control"),
                    "cache_control",
                    LossReason::UnmappedField,
                    "Anthropic response block cache_control has no IR equivalent",
                ));
            }
        }
    }
    Ok((block.into_iter().collect(), losses, true))
}

/// Decodes the wire tool_choice object (N-AN-6).
pub(crate) fn decode_tool_choice(
    choice: Option<&ToolChoiceWire>,
    custom_tools: &BTreeSet<String>,
    native_tools: &BTreeSet<String>,
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
                tool_type: None,
            })
        }
        TOOL_CHOICE_TYPE_ANY => {
            decoded = Some(ToolChoice {
                mode: ToolChoiceMode::Any,
                name: None,
                tool_type: None,
            })
        }
        TOOL_CHOICE_TYPE_NONE => {
            decoded = Some(ToolChoice {
                mode: ToolChoiceMode::None,
                name: None,
                tool_type: None,
            })
        }
        TOOL_CHOICE_TYPE_TOOL => {
            if choice.name.is_empty() {
                return Err(Error::new(
                    "anthropic: tool_choice.name is required for type tool",
                ));
            }
            let tool_type = if native_tools.contains(&choice.name) {
                Some(oxa_ir::ToolType::Native)
            } else if custom_tools.contains(&choice.name) {
                Some(oxa_ir::ToolType::Custom)
            } else {
                None
            };
            decoded = Some(ToolChoice {
                mode: ToolChoiceMode::Tool,
                name: Some(choice.name.clone()),
                tool_type,
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
    if choice.tool_type == Some(oxa_ir::ToolType::ApplyPatch) {
        return Ok((
            None,
            vec![loss(
                "tool_choice",
                "tool_choice",
                LossReason::UnsupportedSemantic,
                "tool choice has no equivalent",
            )],
        ));
    }
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
