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
    BLOCK_TYPE_DOCUMENT, BLOCK_TYPE_IMAGE, BLOCK_TYPE_REDACTED_THINKING, BLOCK_TYPE_TEXT,
    BLOCK_TYPE_THINKING, BLOCK_TYPE_TOOL_RESULT, BLOCK_TYPE_TOOL_USE, BlockWire, ContentValue,
    Message, ROLE_ASSISTANT, ROLE_USER, Request, Response, SOURCE_TYPE_BASE64, SOURCE_TYPE_FILE,
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
    for tool in req.tools.iter().flatten() {
        tool.validate_strict().map_err(Error::new)?;
    }
    oxa_ir::validate_native_request(req).map_err(|e| Error::new(e.to_string()))?;
    let mut losses = Vec::new();
    let metadata = encode_anthropic_metadata(req.metadata.as_ref(), &mut losses);
    let mut out = Request {
        model: config.map_model(&req.model),
        metadata,
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
            for (t_ann_idx, ann) in tool.native.iter().enumerate() {
                if ann.source != oxa_ir::NATIVE_SOURCE_ANTHROPIC
                    && ann.kind != oxa_ir::NATIVE_KIND_DEFER_LOADING
                {
                    if let Some(omission) = &ann.omission {
                        losses.push(loss(
                            format!("tools[{index}].native[{t_ann_idx}]"),
                            &omission.field,
                            omission.reason,
                            omission.detail.clone().unwrap_or_default(),
                        ));
                    } else {
                        losses.push(loss(
                            format!("tools[{index}].native[{t_ann_idx}]"),
                            "source",
                            LossReason::UnsupportedSemantic,
                            format!("unsupported native option source {:?}", ann.source),
                        ));
                    }
                }
            }
            if tool.tool_type == Some(oxa_ir::ToolType::Native) {
                let source = tool.source.as_deref().unwrap_or_default();
                if source != oxa_ir::NATIVE_SOURCE_ANTHROPIC {
                    losses.push(loss(
                        format!("tools[{index}]"),
                        "type",
                        LossReason::UnsupportedSemantic,
                        format!("native tool \"{}\" from \"{source}\" cannot be represented by the target protocol", tool.name),
                    ));
                    continue;
                }
                let def = tool.definition.as_deref().unwrap_or_default();
                let wire_val: serde_json::Value = serde_json::from_str(def).map_err(|e| {
                    Error::new(format!("anthropic: invalid native tool definition: {e}"))
                })?;
                let mut tw: ToolWire = serde_json::from_value(wire_val).map_err(|e| {
                    Error::new(format!("anthropic: invalid native tool definition: {e}"))
                })?;
                if tw.name != tool.name {
                    return Err(Error::new(
                        "anthropic: native tool definition name must match tool name",
                    ));
                }
                match tw.tool_type.as_deref() {
                    Some(t) if !t.is_empty() && t != "function" && t != "custom" => {}
                    _ => {
                        return Err(Error::new(
                            "anthropic: native tool definition requires a native type",
                        ));
                    }
                }
                tw.raw_definition = Some(def.to_string());
                wire_tools.push(tw);
                continue;
            }
            if tool.tool_type == Some(oxa_ir::ToolType::Custom) {
                if let Some(fmt) = &tool.format
                    && fmt.format_type == "grammar"
                {
                    losses.push(loss(
                        format!("tools[{index}].format"),
                        "format",
                        LossReason::UnsupportedSemantic,
                        "Anthropic Messages structured tool wrapper cannot represent custom tool format constraints",
                    ));
                }
                let cache_control = crate::native::get_block_cache_control(&tool.native);
                let defer_loading = crate::native::get_tool_defer_loading(&tool.native);
                let eager_input_streaming =
                    crate::native::get_tool_eager_input_streaming(&tool.native);
                if cache_control.is_some() && defer_loading == Some(true) {
                    return Err(Error::new(format!(
                        "anthropic: tools[{index}]: cannot specify both defer_loading and cache_control"
                    )));
                }
                wire_tools.push(ToolWire {
                    tool_type: None,
                    name: tool.name.clone(),
                    description: tool.description.clone().unwrap_or_default(),
                    input_schema: Some(serde_json::json!({
                        "type": "object",
                        "properties": {"input": {"type": "string"}},
                        "required": ["input"],
                    })),
                    strict: None,
                    cache_control,
                    defer_loading,
                    eager_input_streaming,
                    raw_definition: None,
                });
                continue;
            }
            if tool.tool_type == Some(oxa_ir::ToolType::ApplyPatch) {
                losses.push(loss(
                    format!("tools[{index}]"),
                    "type",
                    LossReason::UnsupportedSemantic,
                    "apply_patch tools have no equivalent in Anthropic Messages",
                ));
                continue;
            }
            let schema_val = tool
                .input_schema
                .as_ref()
                .unwrap_or(&serde_json::Value::Null);
            let schema = serde_json::to_string(schema_val).unwrap_or_default();
            require_json_object(&schema, &format!("tools[{index}].input_schema"))?;
            let cache_control = crate::native::get_block_cache_control(&tool.native);
            let defer_loading = crate::native::get_tool_defer_loading(&tool.native);
            let eager_input_streaming = crate::native::get_tool_eager_input_streaming(&tool.native);
            if cache_control.is_some() && defer_loading == Some(true) {
                return Err(Error::new(format!(
                    "anthropic: tools[{index}]: cannot specify both defer_loading and cache_control"
                )));
            }
            wire_tools.push(ToolWire {
                tool_type: None,
                name: tool.name.clone(),
                description: tool.description.clone().unwrap_or_default(),
                input_schema: Some(schema_val.clone()),
                strict: tool.strict,
                cache_control,
                defer_loading,
                eager_input_streaming,
                raw_definition: None,
            });
        }
        if !wire_tools.is_empty() {
            out.tools = Some(wire_tools);
        }
    }
    let native_named_choice_dropped = req
        .tool_choice
        .as_ref()
        .map(|c| {
            let is_native = c.tool_type == Some(oxa_ir::ToolType::Native)
                || req.tools.as_deref().unwrap_or(&[]).iter().any(|t| {
                    t.tool_type == Some(oxa_ir::ToolType::Native)
                        && Some(&t.name) == c.name.as_ref()
                });
            let name_retained = c
                .name
                .as_ref()
                .map(|n| {
                    out.tools
                        .as_deref()
                        .unwrap_or(&[])
                        .iter()
                        .any(|t| &t.name == n)
                })
                .unwrap_or(false);
            is_native && !name_retained
        })
        .unwrap_or(false);

    let (mut choice, choice_losses) = encode_tool_choice(req.tool_choice.as_ref())?;
    losses.extend(choice_losses);
    if req
        .tool_choice
        .as_ref()
        .is_some_and(|c| c.mode == oxa_ir::ToolChoiceMode::Any)
        && req.tools.as_ref().map(|t| !t.is_empty()).unwrap_or(false)
        && out.tools.as_ref().map(|t| t.is_empty()).unwrap_or(true)
    {
        choice = None;
        losses.push(loss(
            "tool_choice",
            "tool_choice",
            LossReason::UnsupportedSemantic,
            "all tool declarations were dropped",
        ));
    } else if native_named_choice_dropped {
        let name = req
            .tool_choice
            .as_ref()
            .and_then(|c| c.name.as_deref())
            .unwrap_or("");
        choice = None;
        losses.push(loss(
            "tool_choice",
            "tool_choice",
            LossReason::UnsupportedSemantic,
            format!("native tool choice \"{name}\" cannot be represented by the target protocol"),
        ));
    }
    out.tool_choice = choice;

    let shorthand = req.system.is_empty()
        && req.messages.len() == 1
        && req.messages[0].content.len() == 1
        && matches!(&req.messages[0].content[0], Block::Text { native, citations, .. } if native.is_empty() && citations.as_ref().is_none_or(|c| c.is_empty()));
    for (index, message) in req.messages.iter().enumerate() {
        if message.role == oxa_ir::Role::User {
            for (j, b) in message.content.iter().enumerate() {
                if matches!(b, Block::RedactedThinking { .. }) {
                    return Err(Error::new(format!(
                        "anthropic: messages[{index}].content[{j}]: redacted_thinking is not permitted in user messages"
                    )));
                }
                if let Block::Text { citations, .. } = b
                    && citations.as_ref().is_some_and(|c| !c.is_empty())
                {
                    return Err(Error::new(format!(
                        "anthropic: messages[{index}].content[{j}].citations: citations are not permitted on user text blocks"
                    )));
                }
            }
        }
        let role = match message.role {
            oxa_ir::Role::User => ROLE_USER,
            oxa_ir::Role::Assistant => ROLE_ASSISTANT,
        };
        let (blocks, block_losses) =
            encode_request_blocks(&message.content, &format!("messages[{index}].content"))?;
        losses.extend(block_losses);
        let content = if shorthand {
            let Block::Text { text, .. } = &req.messages[0].content[0] else {
                unreachable!("shorthand checked the single text block above");
            };
            ContentValue::Text(text.clone())
        } else {
            let mut blocks = blocks;
            if blocks.is_empty() {
                blocks.push(BlockWire {
                    kind: BLOCK_TYPE_TEXT.to_string(),
                    text: String::new(),
                    ..BlockWire::default()
                });
            }
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
    let nat_losses = crate::native::encode_request_native(req, &mut out)?;
    losses.extend(nat_losses);
    if let Some(format) = req.params.as_ref().and_then(|p| p.response_format.as_ref()) {
        match format {
            oxa_ir::ResponseFormat::JsonSchema {
                name,
                description,
                schema,
                strict,
                ..
            } => {
                let config = out.output_config.get_or_insert_with(Default::default);
                config.format = Some(serde_json::json!({"type":"json_schema", "schema":schema}));
                for (field, present) in [
                    ("name", !name.is_empty()),
                    (
                        "description",
                        description.as_ref().is_some_and(|s| !s.is_empty()),
                    ),
                    ("strict", strict.is_some()),
                ] {
                    if present {
                        losses.push(loss(
                            format!("params.response_format.{field}"),
                            field,
                            LossReason::UnmappedField,
                            "",
                        ));
                    }
                }
            }
            _ => losses.push(loss(
                "params.response_format",
                "response_format",
                LossReason::UnmappedField,
                "",
            )),
        }
    }
    Ok((out, losses))
}

fn encode_request_citations(
    cites: &[oxa_ir::TextCitation],
    path: &str,
) -> Result<(Option<serde_json::Value>, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    let mut items = Vec::new();
    for (i, c) in cites.iter().enumerate() {
        if c.source != "anthropic" {
            return Err(Error::new(format!(
                "anthropic: {path}[{i}]: unknown citation source {:?}",
                c.source
            )));
        }
        let mut obj: serde_json::Map<String, serde_json::Value> =
            serde_json::from_str(&c.value).map_err(|e| Error::new(e.to_string()))?;
        if let Some(fid) = obj.remove("file_id")
            && !fid.is_null()
        {
            losses.push(Loss {
                path: format!("{path}[{i}].file_id"),
                field: "file_id".to_string(),
                reason: LossReason::UnmappedField,
                detail: "response citation file_id has no request-side representation".to_string(),
            });
        }
        items.push(serde_json::Value::Object(obj));
    }
    Ok((Some(serde_json::Value::Array(items)), losses))
}

pub(crate) fn encode_response_citations(
    cites: &[oxa_ir::TextCitation],
) -> Result<serde_json::Value, Error> {
    let mut items = Vec::new();
    for (i, c) in cites.iter().enumerate() {
        if c.source != "anthropic" {
            return Err(Error::new(format!(
                "anthropic: citations[{i}]: unknown citation source {:?}",
                c.source
            )));
        }
        let val: serde_json::Value =
            serde_json::from_str(&c.value).map_err(|e| Error::new(e.to_string()))?;
        items.push(val);
    }
    Ok(serde_json::Value::Array(items))
}

fn encode_request_blocks(
    blocks: &[Block],
    path: &str,
) -> Result<(Vec<BlockWire>, Vec<Loss>), Error> {
    let mut out = Vec::with_capacity(blocks.len());
    let mut losses = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        let (mut encoded, block_losses, mapped) =
            encode_request_block(block, &format!("{path}[{index}]"))?;
        if mapped {
            let native = block.native();
            if let Some(cc) = crate::native::get_block_cache_control(native) {
                encoded.cache_control = Some(cc);
            }
            out.push(encoded);
        }
        losses.extend(block_losses);
    }
    Ok((out, losses))
}

/// Returns (wire block, losses, mapped).
fn encode_request_block(block: &Block, path: &str) -> Result<(BlockWire, Vec<Loss>, bool), Error> {
    match block {
        Block::Text {
            text, citations, ..
        } => {
            let (wire_cites, cite_losses) = if let Some(cites) = citations {
                encode_request_citations(cites, &format!("{path}.citations"))?
            } else {
                (None, Vec::new())
            };
            Ok((
                BlockWire {
                    kind: BLOCK_TYPE_TEXT.to_string(),
                    text: text.clone(),
                    citations: wire_cites,
                    ..BlockWire::default()
                },
                cite_losses,
                true,
            ))
        }
        Block::RedactedThinking { data, .. } => Ok((
            BlockWire {
                kind: BLOCK_TYPE_REDACTED_THINKING.to_string(),
                data: Some(data.clone()),
                ..BlockWire::default()
            },
            Vec::new(),
            true,
        )),
        Block::Thinking {
            thinking,
            signature,
            ..
        } => {
            // An empty signature is absent (the Go zero-value convention), so
            // Some("") replays as unsigned with the degraded loss.
            let signature = signature
                .as_deref()
                .filter(|signature| !signature.is_empty());
            let losses = if signature.is_none() {
                vec![loss(
                    path,
                    "signature",
                    LossReason::Degraded,
                    "unsigned thinking block; Anthropic may reject on replay",
                )]
            } else {
                Vec::new()
            };
            Ok((
                BlockWire {
                    kind: BLOCK_TYPE_THINKING.to_string(),
                    thinking: Some(thinking.clone()),
                    signature: signature.map(str::to_string),
                    ..BlockWire::default()
                },
                losses,
                true,
            ))
        }
        Block::Image {
            media_type,
            data,
            url,
            ..
        } => encode_image_block(media_type.as_deref(), data.as_deref(), url.as_deref(), path),
        Block::Document {
            media_type,
            data,
            url,
            file_id,
            native,
        } => {
            let (mut wire, doc_losses, mapped) = encode_document_block(
                media_type.as_deref(),
                data.as_deref(),
                url.as_deref(),
                file_id.as_deref(),
                native,
                path,
            )?;
            if mapped && let Some(cc) = crate::native::get_block_cache_control(native) {
                wire.cache_control = Some(cc);
            }
            Ok((wire, doc_losses, mapped))
        }
        Block::ToolUse {
            id, name, input, ..
        } => {
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
        Block::CustomToolUse {
            id, name, input, ..
        } => {
            if id.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.id is required")));
            }
            if name.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.name is required")));
            }
            let raw_json = serde_json::json!({"input": input}).to_string();
            let raw = RawValue::from_string(raw_json).map_err(|e| Error::new(e.to_string()))?;
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
        Block::CustomToolResult {
            tool_use_id,
            output,
            ..
        } => {
            if tool_use_id.is_empty() {
                return Err(Error::new(format!(
                    "anthropic: {path}.tool_use_id is required"
                )));
            }
            match output {
                oxa_ir::CustomToolOutput::Text(t) => Ok((
                    BlockWire {
                        kind: BLOCK_TYPE_TOOL_RESULT.to_string(),
                        tool_use_id: tool_use_id.clone(),
                        content: Some(ContentValue::Text(t.clone())),
                        ..BlockWire::default()
                    },
                    Vec::new(),
                    true,
                )),
                oxa_ir::CustomToolOutput::Content(blocks) => {
                    let mut content_wires = Vec::new();
                    let mut losses = Vec::new();
                    for (i, inner) in blocks.iter().enumerate() {
                        let (mut w, l, mapped) =
                            encode_request_block(inner, &format!("{path}.content[{i}]"))?;
                        if mapped {
                            if let Some(cc) = crate::native::get_block_cache_control(inner.native())
                            {
                                w.cache_control = Some(cc);
                            }
                            content_wires.push(w);
                        }
                        losses.extend(l);
                    }
                    Ok((
                        BlockWire {
                            kind: BLOCK_TYPE_TOOL_RESULT.to_string(),
                            tool_use_id: tool_use_id.clone(),
                            content: Some(ContentValue::Blocks(content_wires)),
                            ..BlockWire::default()
                        },
                        losses,
                        true,
                    ))
                }
            }
        }
        Block::WebSearchCall { .. }
        | Block::ApplyPatchToolUse { .. }
        | Block::ApplyPatchToolResult { .. } => Ok((
            BlockWire::default(),
            vec![loss(
                path,
                "type",
                LossReason::UnsupportedSemantic,
                "Anthropic has no native tool block mapping for this IR block",
            )],
            false,
        )),
    }
}

/// Returns (wire block, losses, mapped). The IR image must carry exactly one
/// of data or url; violations are dropped with a loss.
/// Encodes an IR document block to the Anthropic document wire shape
/// (N-AN-16): exactly one of data/url/file_id, base64 requiring media_type,
/// with document-level metadata restored from native annotations.
#[allow(clippy::too_many_arguments)]
fn encode_document_block(
    media_type: Option<&str>,
    data: Option<&str>,
    url: Option<&str>,
    file_id: Option<&str>,
    native: &[oxa_ir::NativeAnnotation],
    path: &str,
) -> Result<(BlockWire, Vec<Loss>, bool), Error> {
    let has_data = data.is_some_and(|value| !value.is_empty());
    let has_url = url.is_some_and(|value| !value.is_empty());
    let has_file = file_id.is_some_and(|value| !value.is_empty());
    let source_count = (has_data as i32) + (has_url as i32) + (has_file as i32);
    let invalid = |detail: &str| {
        Ok((
            BlockWire::default(),
            vec![Loss {
                path: path.to_string(),
                field: "document".into(),
                reason: LossReason::UnsupportedSemantic,
                detail: detail.to_string(),
            }],
            false,
        ))
    };
    let source = if source_count != 1 {
        return invalid("document must contain exactly one of data, url, or file_id");
    } else if has_data {
        let Some(media_type) = media_type.filter(|value| !value.is_empty()) else {
            return invalid("base64 document data requires media_type");
        };
        SourceWire {
            kind: SOURCE_TYPE_BASE64.to_string(),
            media_type: media_type.to_string(),
            data: data.unwrap_or_default().to_string(),
            url: String::new(),
            file_id: String::new(),
            has_media_type: false,
            has_data: false,
            has_url: false,
            has_file_id: false,
        }
    } else if has_url {
        if media_type.is_some_and(|value| !value.is_empty()) {
            return invalid("url document must not carry media_type");
        }
        SourceWire {
            kind: SOURCE_TYPE_URL.to_string(),
            media_type: String::new(),
            data: String::new(),
            url: url.unwrap_or_default().to_string(),
            file_id: String::new(),
            has_media_type: false,
            has_data: false,
            has_url: false,
            has_file_id: false,
        }
    } else {
        if media_type.is_some_and(|value| !value.is_empty()) {
            return invalid("file document must not carry media_type");
        }
        SourceWire {
            kind: SOURCE_TYPE_FILE.to_string(),
            media_type: String::new(),
            data: String::new(),
            url: String::new(),
            file_id: file_id.unwrap_or_default().to_string(),
            has_media_type: false,
            has_data: false,
            has_url: false,
            has_file_id: false,
        }
    };
    let mut wire = BlockWire {
        kind: BLOCK_TYPE_DOCUMENT.to_string(),
        source: Some(source),
        ..BlockWire::default()
    };
    for n in native {
        if n.source != oxa_ir::NATIVE_SOURCE_ANTHROPIC {
            continue;
        }
        let parsed: serde_json::Value = match serde_json::from_str(&n.value) {
            Ok(value) => value,
            Err(_) => continue,
        };
        match n.kind.as_str() {
            oxa_ir::NATIVE_KIND_DOCUMENT_TITLE => {
                if let Some(title) = parsed.as_str() {
                    wire.title = Some(title.to_string());
                }
            }
            oxa_ir::NATIVE_KIND_DOCUMENT_CONTEXT => {
                if let Some(context) = parsed.as_str() {
                    wire.context = Some(context.to_string());
                }
            }
            oxa_ir::NATIVE_KIND_DOCUMENT_CITATIONS if parsed.is_object() => {
                wire.citations = Some(parsed);
            }
            _ => {}
        }
    }
    Ok((wire, Vec::new(), true))
}

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
                    file_id: String::new(),
                    has_media_type: false,
                    has_data: false,
                    has_url: false,
                    has_file_id: false,
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
        native,
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
            Block::RedactedThinking { .. } => {
                return Err(Error::new(format!(
                    "anthropic: {content_path}: redacted_thinking is not permitted in tool_result"
                )));
            }
            Block::Text {
                text,
                native,
                citations,
            } => {
                if citations.as_ref().is_some_and(|c| !c.is_empty()) {
                    return Err(Error::new(format!(
                        "anthropic: {content_path}.citations: citations are not permitted in tool_result"
                    )));
                }
                wire_content.push(BlockWire {
                    kind: BLOCK_TYPE_TEXT.to_string(),
                    cache_control: crate::native::get_block_cache_control(native),
                    text: text.clone(),
                    ..BlockWire::default()
                })
            }
            Block::Image {
                media_type,
                data,
                url,
                native,
            } => {
                let (mut image, image_losses, mapped) = encode_image_block(
                    media_type.as_deref(),
                    data.as_deref(),
                    url.as_deref(),
                    &content_path,
                )?;
                if mapped {
                    if let Some(cc) = crate::native::get_block_cache_control(native) {
                        image.cache_control = Some(cc);
                    }
                    wire_content.push(image);
                }
                losses.extend(image_losses);
            }
            Block::Document {
                media_type,
                data,
                url,
                file_id,
                native,
            } => {
                let (mut doc, doc_losses, mapped) = encode_document_block(
                    media_type.as_deref(),
                    data.as_deref(),
                    url.as_deref(),
                    file_id.as_deref(),
                    native,
                    &content_path,
                )?;
                if mapped {
                    if let Some(cc) = crate::native::get_block_cache_control(native) {
                        doc.cache_control = Some(cc);
                    }
                    wire_content.push(doc);
                }
                losses.extend(doc_losses);
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
            cache_control: crate::native::get_block_cache_control(native),
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
            cache_creation_input_tokens: resp.usage.cache_creation_input_tokens,
            cache_read_input_tokens: resp.usage.cache_read_input_tokens,
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
    let has_native_call = resp
        .content
        .iter()
        .any(|block| matches!(block, Block::ToolUse { .. } | Block::CustomToolUse { .. }));
    let has_unsupported = resp
        .content
        .iter()
        .any(|block| matches!(block, Block::ApplyPatchToolUse { .. }));
    let effective_stop =
        if resp.stop_reason == StopReason::ToolUse && has_unsupported && !has_native_call {
            losses.push(loss(
                "stop_reason",
                "stop_reason",
                LossReason::Degraded,
                "apply_patch calls were dropped; tool_use normalized to end_turn",
            ));
            StopReason::EndTurn
        } else {
            resp.stop_reason
        };
    let (reason, seq) = encode_stop_reason(effective_stop, resp.stop_sequence.as_deref())?;
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
        Block::Text {
            text, citations, ..
        } => {
            let wire_cites = citations
                .as_ref()
                .map(|cites| encode_response_citations(cites))
                .transpose()?;
            Ok((
                BlockWire {
                    kind: BLOCK_TYPE_TEXT.to_string(),
                    text: text.clone(),
                    citations: wire_cites,
                    ..BlockWire::default()
                },
                Vec::new(),
                true,
            ))
        }
        Block::RedactedThinking { data, .. } => Ok((
            BlockWire {
                kind: BLOCK_TYPE_REDACTED_THINKING.to_string(),
                data: Some(data.clone()),
                ..BlockWire::default()
            },
            Vec::new(),
            true,
        )),
        Block::Thinking {
            thinking,
            signature,
            ..
        } => {
            // An empty signature is absent (the Go zero-value convention), so
            // Some("") replays as unsigned with the degraded loss.
            let signature = signature
                .as_deref()
                .filter(|signature| !signature.is_empty());
            let losses = if signature.is_none() {
                vec![loss(
                    path,
                    "signature",
                    LossReason::Degraded,
                    "unsigned thinking block; Anthropic may reject on replay",
                )]
            } else {
                Vec::new()
            };
            Ok((
                BlockWire {
                    kind: BLOCK_TYPE_THINKING.to_string(),
                    thinking: Some(thinking.clone()),
                    signature: signature.map(str::to_string),
                    ..BlockWire::default()
                },
                losses,
                true,
            ))
        }
        Block::Image {
            media_type,
            data,
            url,
            ..
        } => encode_image_block(media_type.as_deref(), data.as_deref(), url.as_deref(), path),
        Block::ToolUse {
            id, name, input, ..
        } => {
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
        Block::CustomToolUse {
            id, name, input, ..
        } => {
            if id.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.id is required")));
            }
            if name.is_empty() {
                return Err(Error::new(format!("anthropic: {path}.name is required")));
            }
            let raw_json = serde_json::json!({"input": input}).to_string();
            let raw = RawValue::from_string(raw_json).map_err(|e| Error::new(e.to_string()))?;
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

fn encode_anthropic_metadata(
    metadata: Option<&std::collections::BTreeMap<String, String>>,
    losses: &mut Vec<Loss>,
) -> Option<serde_json::Value> {
    let Some(metadata) = metadata.filter(|value| !value.is_empty()) else {
        return None;
    };
    let mut user_id = None;
    for (key, value) in metadata {
        if key == "user_id" {
            user_id = Some(value.clone());
            continue;
        }
        losses.push(loss(
            format!("metadata.{key}"),
            key,
            LossReason::UnmappedField,
            "Anthropic request metadata only preserves user_id; this IR metadata key is dropped.",
        ));
    }
    user_id.map(|value| serde_json::json!({ "user_id": value }))
}
