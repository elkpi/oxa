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
            ("messages", "message[]"),
            ("temperature", ""),
            ("top_p", ""),
            ("max_tokens", ""),
            ("max_completion_tokens", ""),
            ("store", ""),
            ("stream", ""),
            ("stream_options", "stream_options"),
            ("stop", ""),
            ("reasoning_effort", ""),
            ("verbosity", ""),
            ("tools", "tool[]"),
            ("tool_choice", "tool_choice"),
            ("parallel_tool_calls", ""),
            ("response_format", "format"),
            ("metadata", ""),
        ]),
        "response" => Some(&[
            ("id", ""),
            ("object", ""),
            ("created", ""),
            ("model", ""),
            ("choices", "choice[]"),
            ("usage", "usage"),
        ]),
        "event" => Some(&[
            ("id", ""),
            ("object", ""),
            ("created", ""),
            ("model", ""),
            ("choices", "choice_delta[]"),
            ("usage", "usage"),
        ]),
        "message" => Some(&[
            ("role", ""),
            ("content", "part[]"),
            ("refusal", ""),
            ("reasoning_content", ""),
            ("tool_calls", "tool_call[]"),
            ("tool_call_id", ""),
        ]),
        "stream_options" => Some(&[("include_usage", ""), ("include_obfuscation", "")]),
        "part" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "text" => shape("text", object),
                "image_url" => shape("image", object),
                "file" => shape("file_part", object),
                _ => None,
            },
            None => None,
        },
        "text" => Some(&[("type", ""), ("text", "")]),
        "image" => Some(&[("type", ""), ("image_url", "image_url")]),
        "image_url" => Some(&[("url", "")]),
        "file_part" => Some(&[("type", ""), ("file", "file_def")]),
        "file_def" => Some(&[("file_id", ""), ("file_data", ""), ("filename", "")]),
        "tool" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "function" => shape("function_tool", object),
                "custom" => shape("custom_tool", object),
                _ => None,
            },
            None => None,
        },
        "function_tool" => Some(&[("type", ""), ("function", "function_definition")]),
        "custom_tool" => Some(&[("type", ""), ("custom", "custom_decl")]),
        "custom_decl" => Some(&[
            ("name", ""),
            ("description", ""),
            ("format", "custom_format"),
        ]),
        "custom_format" => Some(&[("type", ""), ("syntax", ""), ("definition", "")]),
        "function" => Some(&[
            ("name", ""),
            ("description", ""),
            ("parameters", ""),
            ("arguments", ""),
        ]),
        "function_definition" => Some(&[
            ("name", ""),
            ("description", ""),
            ("parameters", ""),
            ("strict", ""),
            ("arguments", ""),
        ]),
        "tool_call" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "function" => shape("function_call", object),
                "custom" => shape("custom_call", object),
                _ => None,
            },
            None => shape("function_call", object),
        },
        "function_call" => Some(&[("id", ""), ("type", ""), ("function", "function")]),
        "custom_call" => Some(&[("id", ""), ("type", ""), ("custom", "custom_args")]),
        "custom_args" => Some(&[("name", ""), ("input", "")]),
        "tool_choice" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "function" => shape("named_choice", object),
                "custom" => shape("named_custom_choice", object),
                _ => None,
            },
            None => None,
        },
        "named_choice" => Some(&[("type", ""), ("function", "named_function")]),
        "named_custom_choice" => Some(&[("type", ""), ("custom", "named_function")]),
        "named_function" => Some(&[("name", "")]),
        "format" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "text" => shape("plain_format", object),
                "json_object" => shape("plain_format", object),
                "json_schema" => shape("schema_format", object),
                _ => None,
            },
            None => None,
        },
        "plain_format" => Some(&[("type", "")]),
        "schema_format" => Some(&[("type", ""), ("json_schema", "json_schema")]),
        "json_schema" => Some(&[
            ("name", ""),
            ("description", ""),
            ("schema", ""),
            ("strict", ""),
        ]),
        "choice" => Some(&[("index", ""), ("message", "message"), ("finish_reason", "")]),
        "choice_delta" => Some(&[("index", ""), ("delta", "delta"), ("finish_reason", "")]),
        "delta" => Some(&[
            ("role", ""),
            ("content", ""),
            ("reasoning_content", ""),
            ("tool_calls", "tool_delta[]"),
        ]),
        "tool_delta" => Some(&[
            ("index", ""),
            ("id", ""),
            ("type", ""),
            ("function", "function_delta"),
            ("custom", "custom_args"),
        ]),
        "function_delta" => Some(&[("name", ""), ("arguments", "")]),
        "usage" => Some(&[
            ("prompt_tokens", ""),
            ("completion_tokens", ""),
            ("total_tokens", ""),
            ("prompt_tokens_details", "input_detail"),
            ("completion_tokens_details", "output_detail"),
        ]),
        "input_detail" => Some(&[("cached_tokens", "")]),
        "output_detail" => Some(&[("reasoning_tokens", "")]),
        _ => None,
    }
}
