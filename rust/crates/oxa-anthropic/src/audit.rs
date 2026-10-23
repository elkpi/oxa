use crate::Error;
use oxa_ir::{Loss, LossReason};
use serde::de::{MapAccess, Visitor};
use serde::{Deserialize, Deserializer};
use serde_json::value::RawValue;
use std::collections::{BTreeMap, BTreeSet};
use std::fmt;

struct RawObject<'a>(Vec<(String, &'a RawValue)>);

impl<'de> Deserialize<'de> for RawObject<'de> {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct ObjectVisitor;
        impl<'de> Visitor<'de> for ObjectVisitor {
            type Value = RawObject<'de>;
            fn expecting(&self, formatter: &mut fmt::Formatter) -> fmt::Result {
                formatter.write_str("a JSON object")
            }
            fn visit_map<M: MapAccess<'de>>(self, mut map: M) -> Result<Self::Value, M::Error> {
                let mut entries = Vec::new();
                while let Some(key) = map.next_key::<String>()? {
                    entries.push((key, map.next_value::<&'de RawValue>()?));
                }
                Ok(RawObject(entries))
            }
        }
        deserializer.deserialize_map(ObjectVisitor)
    }
}

fn tag<'a>(object: &'a RawObject<'a>, key: &str) -> Option<String> {
    object
        .0
        .iter()
        .rev()
        .find(|(name, _)| name == key)
        .and_then(|(_, raw)| serde_json::from_str(raw.get()).ok())
}

#[derive(Default)]
struct Audit {
    losses: Vec<(Loss, usize)>,
    positions: BTreeMap<String, usize>,
}

fn join(path: &str, key: &str) -> String {
    if path.is_empty() {
        key.to_owned()
    } else {
        format!("{path}.{key}")
    }
}

fn walk(
    raw: &RawValue,
    name: &str,
    path: &str,
    base: usize,
    audit: &mut Audit,
) -> Result<(), Error> {
    audit
        .positions
        .insert(path.to_owned(), raw.get().as_ptr() as usize - base);
    if name.is_empty() {
        return Ok(());
    }
    if let Some(item_kind) = name.strip_suffix("[]") {
        if raw.get().starts_with('[') {
            let items: Vec<&RawValue> =
                serde_json::from_str(raw.get()).map_err(|error| Error::new(error.to_string()))?;
            for (index, item) in items.iter().enumerate() {
                walk(item, item_kind, &format!("{path}[{index}]"), base, audit)?;
            }
        }
        return Ok(());
    }
    if !raw.get().starts_with('{') {
        return Ok(());
    }
    let object: RawObject<'_> =
        serde_json::from_str(raw.get()).map_err(|error| Error::new(error.to_string()))?;
    if name == "tool"
        && let Some(tool_type) = tag(&object, "type")
        && !tool_type.is_empty()
        && tool_type != "function"
        && tool_type != "custom"
    {
        for (key, value) in &object.0 {
            let field_path = join(path, key);
            let position = value.get().as_ptr() as usize - base;
            audit.positions.insert(field_path, position);
        }
        return Ok(());
    }
    let Some(fields) = shape(name, &object) else {
        return Ok(());
    };
    for (key, value) in &object.0 {
        let field_path = join(path, key);
        let position = value.get().as_ptr() as usize - base;
        audit.positions.insert(field_path.clone(), position);
        if let Some((_, child)) = fields.iter().find(|(field, _)| *field == key) {
            walk(value, child, &field_path, base, audit)?;
        } else {
            audit.losses.push((
                Loss {
                    path: field_path,
                    field: key.clone(),
                    reason: LossReason::UnmappedField,
                    detail: String::new(),
                },
                position,
            ));
        }
    }
    Ok(())
}

fn source_path(path: &str, positions: &BTreeMap<String, usize>) -> String {
    if positions.contains_key(path) {
        return path.to_owned();
    }
    let response = join("response", path);
    if positions.contains_key(&response) {
        return response;
    }
    let delta = join("delta", path);
    if positions.contains_key(&delta) {
        return delta;
    }
    if path.starts_with("output[")
        && let Some(close) = path.find(']')
    {
        let tail = &path[close + 1..];
        let item = format!("item{tail}");
        if positions.contains_key(&item) {
            return item;
        }
        if let Some(after) = tail
            .strip_prefix(".content[")
            .and_then(|value| value.find(']').map(|close| &value[close + 1..]))
        {
            let part = format!("part{after}");
            if positions.contains_key(&part) {
                return part;
            }
        }
    }
    if let Some((_, tail)) = path.split_once(".content_block") {
        let block = format!("content_block{tail}");
        if positions.contains_key(&block) {
            return block;
        }
    }
    path.to_owned()
}

pub(crate) fn clean_json(raw: &RawValue, name: &str) -> Result<String, Error> {
    if name.is_empty() {
        return Ok(raw.get().to_string());
    }
    if let Some(item_kind) = name.strip_suffix("[]") {
        if raw.get().starts_with('[') {
            let items: Vec<&RawValue> =
                serde_json::from_str(raw.get()).map_err(|error| Error::new(error.to_string()))?;
            let mut out = String::from("[");
            for (i, item) in items.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                out.push_str(&clean_json(item, item_kind)?);
            }
            out.push(']');
            return Ok(out);
        }
        return Ok(raw.get().to_string());
    }
    if !raw.get().starts_with('{') {
        return Ok(raw.get().to_string());
    }
    let object: RawObject<'_> =
        serde_json::from_str(raw.get()).map_err(|error| Error::new(error.to_string()))?;
    if name == "tool"
        && let Some(tool_type) = tag(&object, "type")
        && !tool_type.is_empty()
        && tool_type != "function"
        && tool_type != "custom"
    {
        return Ok(raw.get().to_string());
    }
    let Some(fields) = shape(name, &object) else {
        let mut out = String::from("{");
        let mut first = true;
        for (k, v) in &object.0 {
            let keep = if name == "event" {
                [
                    "type",
                    "item_id",
                    "output_index",
                    "content_index",
                    "call_id",
                    "name",
                ]
                .contains(&k.as_str())
            } else {
                k == "type"
            };
            if keep {
                if !first {
                    out.push(',');
                }
                first = false;
                let k_json =
                    serde_json::to_string(k).map_err(|error| Error::new(error.to_string()))?;
                out.push_str(&k_json);
                out.push(':');
                out.push_str(v.get());
            }
        }
        out.push('}');
        return Ok(out);
    };
    let mut out = String::from("{");
    let mut first = true;
    for (key, value) in &object.0 {
        if let Some((_, child_kind)) = fields.iter().find(|(f, _)| f == key) {
            if !first {
                out.push(',');
            }
            first = false;
            let key_json =
                serde_json::to_string(key).map_err(|error| Error::new(error.to_string()))?;
            out.push_str(&key_json);
            out.push(':');
            out.push_str(&clean_json(value, child_kind)?);
        }
    }
    out.push('}');
    Ok(out)
}

pub(crate) fn audit_losses(raw: &str, name: &str, losses: Vec<Loss>) -> Result<Vec<Loss>, Error> {
    let root: &RawValue =
        serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let mut audit = Audit::default();
    walk(root, name, "", root.get().as_ptr() as usize, &mut audit)?;
    let existing: BTreeSet<_> = losses
        .iter()
        .map(|loss| {
            (
                loss.path.clone(),
                loss.field.clone(),
                format!("{:?}", loss.reason),
            )
        })
        .collect();
    let mut entries: Vec<_> = losses
        .into_iter()
        .map(|loss| {
            let path = source_path(&loss.path, &audit.positions);
            let position = if loss.reason == LossReason::UnsupportedSemantic {
                audit.positions.get(&path).copied()
            } else {
                audit
                    .positions
                    .get(&join(&path, &loss.field))
                    .or_else(|| audit.positions.get(&path))
                    .copied()
            }
            .unwrap_or(usize::MAX);
            (loss, position)
        })
        .collect();
    for (loss, position) in audit.losses {
        if !existing.contains(&(
            loss.path.clone(),
            loss.field.clone(),
            format!("{:?}", loss.reason),
        )) {
            entries.push((loss, position));
        }
    }
    entries.sort_by_key(|(_, position)| *position);
    Ok(entries.into_iter().map(|(loss, _)| loss).collect())
}

fn shape(name: &str, object: &RawObject<'_>) -> Option<&'static [(&'static str, &'static str)]> {
    match name {
        "request" => Some(&[
            ("model", ""),
            ("system", "system_block[]"),
            ("messages", "message[]"),
            ("max_tokens", ""),
            ("temperature", ""),
            ("top_p", ""),
            ("stop_sequences", ""),
            ("tools", "tool[]"),
            ("tool_choice", "tool_choice"),
            ("thinking", "thinking_config"),
            ("cache_control", ""),
            ("output_config", "output_config"),
            ("context_management", "context_management"),
        ]),
        "output_config" => Some(&[("effort", "")]),
        "context_management" => Some(&[("edits", "")]),
        "response" => Some(&[
            ("id", ""),
            ("type", ""),
            ("role", ""),
            ("model", ""),
            ("content", "block[]"),
            ("stop_reason", ""),
            ("stop_sequence", ""),
            ("usage", "usage"),
        ]),
        "message" => Some(&[("role", ""), ("content", "block[]")]),
        "system_block" => Some(&[("type", ""), ("text", ""), ("cache_control", "")]),
        "tool" => Some(&[
            ("type", ""),
            ("name", ""),
            ("description", ""),
            ("input_schema", ""),
            ("cache_control", ""),
            ("defer_loading", ""),
        ]),
        "tool_choice" => Some(&[("type", ""), ("name", "")]),
        "thinking_config" => Some(&[("type", ""), ("budget_tokens", ""), ("display", "")]),
        "block" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "text" => shape("text", object),
                "thinking" => shape("thinking", object),
                "tool_use" => shape("tool_use", object),
                "tool_result" => shape("tool_result", object),
                "image" => shape("image", object),
                "document" => shape("document", object),
                _ => None,
            },
            None => None,
        },
        "text" => Some(&[("type", ""), ("text", ""), ("cache_control", "")]),
        "thinking" => Some(&[
            ("type", ""),
            ("thinking", ""),
            ("signature", ""),
            ("cache_control", ""),
        ]),
        "tool_use" => Some(&[
            ("type", ""),
            ("id", ""),
            ("name", ""),
            ("input", ""),
            ("cache_control", ""),
        ]),
        "tool_result" => Some(&[
            ("type", ""),
            ("tool_use_id", ""),
            ("content", "block[]"),
            ("is_error", ""),
            ("cache_control", ""),
        ]),
        "image" => Some(&[("type", ""), ("source", "source"), ("cache_control", "")]),
        "document" => Some(&[
            ("type", ""),
            ("source", "document_source"),
            ("title", ""),
            ("context", ""),
            ("citations", ""),
            ("cache_control", ""),
        ]),
        "document_source" => match tag(object, "type") {
            Some(value) if ["base64", "url", "file"].contains(&value.as_str()) => {
                shape("document_source_fields", object)
            }
            _ => None,
        },
        "document_source_fields" => Some(&[
            ("type", ""),
            ("media_type", ""),
            ("data", ""),
            ("url", ""),
            ("file_id", ""),
        ]),
        "source" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "base64" => shape("base64", object),
                "url" => shape("url", object),
                "file" => shape("file", object),
                _ => None,
            },
            None => None,
        },
        "base64" => Some(&[("type", ""), ("media_type", ""), ("data", "")]),
        "url" => Some(&[("type", ""), ("url", "")]),
        "file" => Some(&[("type", ""), ("file_id", "")]),
        "usage" => Some(&[
            ("input_tokens", ""),
            ("output_tokens", ""),
            ("cache_creation_input_tokens", ""),
            ("cache_read_input_tokens", ""),
        ]),
        "event" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "message_start" => shape("start", object),
                "content_block_start" => shape("block_start", object),
                "content_block_delta" => shape("block_delta", object),
                "content_block_stop" => shape("block_stop", object),
                "message_delta" => shape("message_delta", object),
                "message_stop" => shape("message_stop", object),
                _ => None,
            },
            None => None,
        },
        "start" => Some(&[("type", ""), ("message", "response")]),
        "block_start" => Some(&[
            ("type", ""),
            ("index", ""),
            ("content_block", "stream_block"),
        ]),
        "stream_block" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "text" => shape("text", object),
                "thinking" => shape("thinking", object),
                "tool_use" => shape("tool_use", object),
                _ => None,
            },
            None => None,
        },
        "block_delta" => Some(&[("type", ""), ("index", ""), ("delta", "delta")]),
        "block_stop" => Some(&[("type", ""), ("index", "")]),
        "message_delta" => Some(&[("type", ""), ("delta", "stop_delta"), ("usage", "usage")]),
        "message_stop" => Some(&[("type", "")]),
        "stop_delta" => Some(&[("stop_reason", ""), ("stop_sequence", "")]),
        "delta" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "text_delta" => shape("text_delta", object),
                "input_json_delta" => shape("json_delta", object),
                "thinking_delta" => shape("thinking_delta", object),
                "signature_delta" => shape("signature_delta", object),
                _ => None,
            },
            None => None,
        },
        "text_delta" => Some(&[("type", ""), ("text", "")]),
        "json_delta" => Some(&[("type", ""), ("partial_json", "")]),
        "thinking_delta" => Some(&[("type", ""), ("thinking", "")]),
        "signature_delta" => Some(&[("type", ""), ("signature", "")]),
        _ => None,
    }
}
