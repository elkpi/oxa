//! Anthropic tool-change unions shared by system messages and compaction.
use crate::types::{BlockWire, ContentValue};
use crate::{Config, Error, Message, Request, ToolWire};
use oxa_ir::{
    Loss, LossReason, NativeAnnotation, NativeOmission, ToolChange, ToolChangeOp, ToolChangeRef,
};
use serde_json::Value;
use std::collections::BTreeMap;

pub(crate) fn cache_annotation(
    value: Option<&Value>,
    path: &str,
    losses: &mut Vec<Loss>,
) -> Vec<NativeAnnotation> {
    let Some(value) = value else {
        return Vec::new();
    };
    let (canonical, failure) = crate::native::parse_cache_control(value, path);
    if let Some(failure) = failure {
        losses.push(failure);
    }
    canonical
        .into_iter()
        .map(|value| NativeAnnotation {
            source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
            kind: oxa_ir::NATIVE_KIND_CACHE_CONTROL.into(),
            value,
            omission: Some(NativeOmission {
                field: "cache_control".into(),
                reason: LossReason::UnmappedField,
                detail: Some(crate::native::CACHE_CONTROL_LOSS_DETAIL.into()),
            }),
            projection: None,
        })
        .collect()
}

pub(crate) fn decode_change(
    wire: &BlockWire,
    path: &str,
) -> Result<(ToolChange, Vec<Loss>), Error> {
    let op = match wire.kind.as_str() {
        "tool_addition" => ToolChangeOp::Add,
        "tool_removal" => ToolChangeOp::Remove,
        _ => {
            return Err(Error::new(format!(
                "anthropic: {path}.type must be tool_addition or tool_removal"
            )));
        }
    };
    let target = wire
        .tool
        .as_ref()
        .and_then(Value::as_object)
        .ok_or_else(|| Error::new(format!("anthropic: {path}.tool must be an object")))?;
    let kind = target
        .get("type")
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty())
        .ok_or_else(|| Error::new(format!("anthropic: {path}.tool.type is required")))?;
    let mut losses = Vec::new();
    let native = cache_annotation(
        wire.cache_control.as_ref(),
        &format!("{path}.cache_control"),
        &mut losses,
    );
    let mut result = ToolChange {
        op,
        tool: None,
        reference: None,
        native,
        extra: (!wire.extra.is_empty()).then(|| wire.extra.clone()),
        tool_extra: None,
        definition_extra: None,
    };
    if kind == "tool_definition" {
        if op == ToolChangeOp::Remove {
            return Err(Error::new(format!(
                "anthropic: {path}.tool: removal cannot carry a definition"
            )));
        }
        let definition = target
            .get("definition")
            .filter(|v| v.is_object())
            .ok_or_else(|| {
                Error::new(format!(
                    "anthropic: {path}.tool.definition must be an object"
                ))
            })?;
        let tool: ToolWire = serde_json::from_str(&definition.to_string())
            .map_err(|e| Error::new(format!("anthropic: {path}.tool.definition: {e}")))?;
        let synthetic = Request {
            model: "m".into(),
            max_tokens: 1,
            tools: Some(vec![tool]),
            messages: vec![Message {
                role: "user".into(),
                content: Some(ContentValue::Text("x".into())),
                ..Message::default()
            }],
            ..Request::default()
        };
        let (decoded, nested_losses) = crate::decode_request(&synthetic, &Config::default())
            .map_err(|e| Error::new(format!("anthropic: {path}.tool.definition: {e}")))?;
        losses.extend(nested_losses);
        result.tool = decoded.tools.and_then(|mut tools| tools.pop());
        let tool_extra: BTreeMap<_, _> = target
            .iter()
            .filter(|(k, _)| !matches!(k.as_str(), "type" | "definition"))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect();
        result.tool_extra = (!tool_extra.is_empty()).then_some(tool_extra);
        if result
            .tool
            .as_ref()
            .is_some_and(|t| t.tool_type != Some(oxa_ir::ToolType::Native))
        {
            let extra: BTreeMap<_, _> = definition
                .as_object()
                .unwrap()
                .iter()
                .filter(|(k, _)| {
                    !matches!(
                        k.as_str(),
                        "type"
                            | "name"
                            | "description"
                            | "input_schema"
                            | "strict"
                            | "cache_control"
                            | "defer_loading"
                            | "eager_input_streaming"
                    )
                })
                .map(|(k, v)| (k.clone(), v.clone()))
                .collect();
            result.definition_extra = (!extra.is_empty()).then_some(extra);
        }
    } else {
        let name = match target.get("name") {
            None => "",
            Some(Value::String(s)) => s.as_str(),
            Some(_) => {
                return Err(Error::new(format!(
                    "anthropic: {path}.tool.name must be a string"
                )));
            }
        };
        let server = match target.get("server_name") {
            None => None,
            Some(Value::String(s)) => Some(s.as_str()),
            Some(_) => {
                return Err(Error::new(format!(
                    "anthropic: {path}.tool.server_name must be a string"
                )));
            }
        };
        let valid = match kind {
            "tool_reference" => !name.is_empty() && server.is_none(),
            "mcp_tool_reference" => !name.is_empty() && server.is_some_and(|s| !s.is_empty()),
            "mcp_toolset_reference" => name.is_empty() && server.is_some_and(|s| !s.is_empty()),
            _ => false,
        };
        if !valid {
            return Err(Error::new(format!(
                "anthropic: {path}.tool: invalid {kind} reference"
            )));
        }
        let extra: BTreeMap<_, _> = target
            .iter()
            .filter(|(k, _)| !matches!(k.as_str(), "type" | "name" | "server_name"))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect();
        result.reference = Some(ToolChangeRef {
            ref_type: kind.into(),
            name: name.into(),
            server_name: server.map(str::to_owned),
            extra: (!extra.is_empty()).then_some(extra),
        });
    }
    Ok((result, losses))
}
