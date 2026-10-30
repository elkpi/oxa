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
    if name == "tool"
        && let Some(tool_type) = tag(&object, "type")
        && !tool_type.is_empty()
        && tool_type != "function"
        && tool_type != "custom"
        && tool_type != "apply_patch"
        && tool_type != "namespace"
        && tool_type != "toolbox"
        && is_responses_native_tool(&tool_type, &object)
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
            ("input", "input_item[]"),
            ("instructions", ""),
            ("temperature", ""),
            ("top_p", ""),
            ("max_output_tokens", ""),
            ("tools", "tool[]"),
            ("tool_choice", "tool_choice"),
            ("text", "text_params"),
            ("reasoning", "reasoning"),
        ]),
        "response" => Some(&[
            ("id", ""),
            ("object", ""),
            ("status", ""),
            ("model", ""),
            ("output", "output_item[]"),
            ("usage", "usage"),
            ("incomplete_details", "incomplete"),
            ("error", ""),
        ]),
        "input_item" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "" => shape("input_message", object),
                "message" => shape("input_message", object),
                "function_call" => shape("function_call", object),
                "function_call_output" => shape("function_output", object),
                "custom_tool_call" => shape("custom_tool_call", object),
                "custom_tool_call_output" => shape("custom_tool_output", object),
                "apply_patch_call" => shape("apply_patch_call", object),
                "apply_patch_call_output" => shape("apply_patch_call_output", object),
                "reasoning" => shape("reasoning_item", object),
                "web_search_call" => shape("web_search_call", object),
                "additional_tools" => shape("additional_tools_item", object),
                _ => None,
            },
            None => shape("input_message", object),
        },
        "additional_tools_item" => Some(&[("type", ""), ("role", ""), ("tools", "tool[]")]),
        "web_search_call" => Some(&[("type", ""), ("id", ""), ("status", ""), ("action", "")]),
        "input_message" => Some(&[
            ("type", ""),
            ("id", ""),
            ("status", ""),
            ("role", ""),
            ("content", "input_part[]"),
        ]),
        "function_call" => Some(&[
            ("type", ""),
            ("id", ""),
            ("status", ""),
            ("call_id", ""),
            ("name", ""),
            ("namespace", ""),
            ("arguments", ""),
        ]),
        "custom_tool_call" => Some(&[
            ("type", ""),
            ("id", ""),
            ("call_id", ""),
            ("name", ""),
            ("namespace", ""),
            ("input", ""),
        ]),
        "custom_tool_output" => {
            Some(&[("type", ""), ("call_id", ""), ("output", "custom_output[]")])
        }
        "custom_output" => match tag(object, "type").as_deref() {
            Some("input_text") => Some(&[("type", ""), ("text", "")]),
            Some("input_image") => Some(&[
                ("type", ""),
                ("image_url", ""),
                ("file_id", ""),
                ("detail", ""),
            ]),
            _ => None,
        },
        "function_output" => Some(&[
            ("type", ""),
            ("id", ""),
            ("status", ""),
            ("call_id", ""),
            ("output", ""),
        ]),
        "apply_patch_call" => Some(&[
            ("type", ""),
            ("id", ""),
            ("status", ""),
            ("call_id", ""),
            ("operation", "apply_patch_operation"),
        ]),
        "apply_patch_call_output" => Some(&[
            ("type", ""),
            ("id", ""),
            ("status", ""),
            ("call_id", ""),
            ("output", ""),
        ]),
        "apply_patch_operation" => match tag(object, "type").as_deref() {
            Some("create_file" | "update_file") => {
                Some(&[("type", ""), ("path", ""), ("diff", "")])
            }
            Some("delete_file") => Some(&[("type", ""), ("path", "")]),
            _ => None,
        },
        "reasoning_item" => Some(&[
            ("type", ""),
            ("id", ""),
            ("status", ""),
            ("summary", "summary_part[]"),
        ]),
        "input_part" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "input_text" => shape("plain_part", object),
                "output_text" => shape("plain_part", object),
                "input_image" => shape("input_image", object),
                _ => None,
            },
            None => None,
        },
        "plain_part" => Some(&[("type", ""), ("text", ""), ("annotations", "")]),
        "input_image" => Some(&[("type", ""), ("image_url", "")]),
        "summary_part" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "summary_text" => shape("plain_part", object),
                "output_text" => shape("plain_part", object),
                _ => None,
            },
            None => None,
        },
        "output_part" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "output_text" => shape("plain_part", object),
                _ => None,
            },
            None => None,
        },
        "output_item" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "message" => shape("output_message", object),
                "function_call" => shape("function_call", object),
                "custom_tool_call" => shape("custom_tool_call", object),
                "apply_patch_call" => shape("apply_patch_call", object),
                "apply_patch_call_output" => shape("apply_patch_call_output", object),
                "reasoning" => shape("reasoning_item", object),
                "web_search_call" => shape("web_search_call", object),
                _ => None,
            },
            None => None,
        },
        "output_message" => Some(&[
            ("type", ""),
            ("id", ""),
            ("status", ""),
            ("role", ""),
            ("content", "output_part[]"),
        ]),
        "tool" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "apply_patch" => shape("apply_patch_tool", object),
                "function" => shape("function_tool", object),
                "custom" => shape("custom_tool", object),
                "namespace" | "toolbox" => shape("container_tool", object),
                _ => None,
            },
            None => None,
        },
        "container_tool" => Some(&[
            ("type", ""),
            ("name", ""),
            ("description", ""),
            ("tools", "tool[]"),
        ]),
        "apply_patch_tool" => Some(&[("type", "")]),
        "function_tool" => Some(&[
            ("type", ""),
            ("name", ""),
            ("description", ""),
            ("parameters", ""),
            ("strict", ""),
            ("defer_loading", ""),
        ]),
        "custom_tool" => Some(&[
            ("type", ""),
            ("name", ""),
            ("description", ""),
            ("format", "custom_format"),
            ("defer_loading", ""),
        ]),
        "custom_format" => match tag(object, "type").as_deref() {
            Some("text") => Some(&[("type", "")]),
            Some("grammar") => Some(&[("type", ""), ("syntax", ""), ("definition", "")]),
            _ => Some(&[("type", "")]),
        },
        "tool_choice" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "apply_patch" => shape("apply_patch_choice", object),
                "function" | "custom" => shape("named_choice", object),
                _ => None,
            },
            None => None,
        },
        "apply_patch_choice" => Some(&[("type", "")]),
        "named_choice" => Some(&[("type", ""), ("name", ""), ("namespace", "")]),
        "text_params" => Some(&[("format", "format")]),
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
        "schema_format" => Some(&[
            ("type", ""),
            ("name", ""),
            ("description", ""),
            ("schema", ""),
            ("strict", ""),
        ]),
        "reasoning" => Some(&[("effort", ""), ("summary", "")]),
        "usage" => Some(&[
            ("input_tokens", ""),
            ("output_tokens", ""),
            ("total_tokens", ""),
            ("input_token_details", "input_detail"),
            ("output_token_details", "output_detail"),
        ]),
        "input_detail" => Some(&[("cached_tokens", "")]),
        "output_detail" => Some(&[("reasoning_tokens", "")]),
        "incomplete" => Some(&[("reason", "")]),
        "event" => match tag(object, "type") {
            Some(value) => match value.as_str() {
                "response.created" => shape("response_event", object),
                "response.completed" => shape("response_event", object),
                "response.incomplete" => shape("response_event", object),
                "response.failed" => shape("response_event", object),
                "response.output_item.added" => shape("item_event", object),
                "response.output_item.done" => shape("item_event", object),
                "response.content_part.added" => shape("part_event", object),
                "response.content_part.done" => shape("part_event", object),
                "response.reasoning_summary_part.added" => shape("summary_part_event", object),
                "response.reasoning_summary_part.done" => shape("summary_part_event", object),
                "response.output_text.delta" => shape("text_delta_event", object),
                "response.reasoning_summary_text.delta" => shape("text_delta_event", object),
                "response.output_text.done" => shape("text_done_event", object),
                "response.reasoning_summary_text.done" => shape("text_done_event", object),
                "response.function_call_arguments.delta" => shape("args_delta_event", object),
                "response.function_call_arguments.done" => shape("args_done_event", object),
                "response.custom_tool_call_input.delta" => shape("args_delta_event", object),
                "response.custom_tool_call_input.done" => shape("custom_input_done_event", object),
                _ => None,
            },
            None => None,
        },
        "response_event" => Some(&[
            ("type", ""),
            ("response", "response"),
            ("sequence_number", ""),
        ]),
        "item_event" => Some(&[
            ("type", ""),
            ("output_index", ""),
            ("item", "output_item"),
            ("sequence_number", ""),
        ]),
        "part_event" => Some(&[
            ("type", ""),
            ("item_id", ""),
            ("output_index", ""),
            ("content_index", ""),
            ("part", "output_part"),
            ("sequence_number", ""),
        ]),
        "summary_part_event" => Some(&[
            ("type", ""),
            ("item_id", ""),
            ("output_index", ""),
            ("content_index", ""),
            ("part", "summary_part"),
            ("sequence_number", ""),
        ]),
        "text_delta_event" => Some(&[
            ("type", ""),
            ("item_id", ""),
            ("output_index", ""),
            ("content_index", ""),
            ("delta", ""),
            ("sequence_number", ""),
        ]),
        "text_done_event" => Some(&[
            ("type", ""),
            ("item_id", ""),
            ("output_index", ""),
            ("content_index", ""),
            ("text", ""),
            ("sequence_number", ""),
        ]),
        "args_delta_event" => Some(&[
            ("type", ""),
            ("item_id", ""),
            ("output_index", ""),
            ("delta", ""),
            ("sequence_number", ""),
        ]),
        "args_done_event" => Some(&[
            ("type", ""),
            ("item_id", ""),
            ("output_index", ""),
            ("call_id", ""),
            ("name", ""),
            ("arguments", ""),
            ("sequence_number", ""),
        ]),
        "custom_input_done_event" => Some(&[
            ("type", ""),
            ("item_id", ""),
            ("output_index", ""),
            ("input", ""),
            ("sequence_number", ""),
        ]),
        _ => None,
    }
}

pub(crate) fn is_known_openai_native_type(kind: &str) -> bool {
    matches!(
        kind,
        "web_search"
            | "file_search"
            | "code_interpreter"
            | "computer"
            | "computer_use_preview"
            | "mcp"
            | "tool_search"
            | "image_generation"
            | "local_shell"
            | "shell"
            | "programmatic_tool_calling"
    ) || kind.starts_with("web_search_")
        || kind.starts_with("file_search_")
        || kind.starts_with("computer_")
}

fn is_responses_native_tool(tool_type: &str, object: &RawObject<'_>) -> bool {
    let has_name = object
        .0
        .iter()
        .any(|(k, v)| k == "name" && v.get() != "\"\"");
    let has_server_label = object
        .0
        .iter()
        .any(|(k, v)| k == "server_label" && v.get() != "\"\"");
    has_name || has_server_label || is_known_openai_native_type(tool_type)
}
