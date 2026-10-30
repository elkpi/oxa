use serde_json::Value;
use std::collections::BTreeMap;

use crate::{Error, Loss, LossReason};

pub fn validate_web_search_extra(extra: &BTreeMap<String, Value>) -> Result<(), Error> {
    if extra
        .keys()
        .any(|key| matches!(key.as_str(), "type" | "id" | "status" | "action"))
    {
        return Err(Error::Custom(
            "web_search_call extra cannot shadow known item fields".into(),
        ));
    }
    Ok(())
}

pub fn inspect_web_search_call(
    id: &str,
    status: &str,
    action: &Value,
    path: &str,
) -> Result<Vec<Loss>, Error> {
    let invalid = |message: &str| Error::Custom(format!("{path}: {message}"));
    if id.is_empty() || status.is_empty() {
        return Err(invalid("web_search_call requires non-empty id and status"));
    }
    let object = action
        .as_object()
        .ok_or_else(|| invalid("action must be an object"))?;
    let kind = object
        .get("type")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let keys: &[&str] = match kind {
        "search" => &["query", "queries", "sources"],
        "open_page" => &["url"],
        "find_in_page" => &["url", "pattern"],
        _ => return Err(invalid("unknown or absent action.type")),
    };
    if kind == "find_in_page" && (!object.contains_key("url") || !object.contains_key("pattern")) {
        return Err(invalid("find_in_page requires url and pattern"));
    }
    let mut losses = Vec::new();
    for (key, value) in object {
        if key == "type" {
            continue;
        }
        if !keys.contains(&key.as_str()) {
            losses.push(Loss {
                path: format!("{path}.action.{key}"),
                field: key.clone(),
                reason: LossReason::UnmappedField,
                detail: String::new(),
            });
            continue;
        }
        if kind == "open_page" && key == "url" && value.is_null() {
            continue;
        }
        match key.as_str() {
            "queries" => {
                if !value
                    .as_array()
                    .is_some_and(|a| a.iter().all(Value::is_string))
                {
                    return Err(invalid("action.queries must be an array of strings"));
                }
            }
            "sources" => {
                let sources = value
                    .as_array()
                    .ok_or_else(|| invalid("action.sources must be an array"))?;
                for (index, source) in sources.iter().enumerate() {
                    let source = source
                        .as_object()
                        .ok_or_else(|| invalid("source must be an object"))?;
                    if source.get("type").and_then(Value::as_str) != Some("url")
                        || !source.get("url").is_some_and(Value::is_string)
                    {
                        return Err(invalid("source requires type url and string url"));
                    }
                    for field in source
                        .keys()
                        .filter(|k| k.as_str() != "type" && k.as_str() != "url")
                    {
                        losses.push(Loss {
                            path: format!("{path}.action.sources[{index}].{field}"),
                            field: field.clone(),
                            reason: LossReason::UnmappedField,
                            detail: String::new(),
                        });
                    }
                }
            }
            _ if !value.is_string() => {
                return Err(invalid(&format!("action.{key} must be a string")));
            }
            _ => {}
        }
    }
    Ok(losses)
}

pub(crate) fn check_web_search_blocks(document: &Value) -> Result<(), Error> {
    fn check(block: &Value) -> Result<(), Error> {
        if block.get("type").and_then(Value::as_str) == Some("web_search_call") {
            let id = block.get("id").and_then(Value::as_str).unwrap_or_default();
            let status = block
                .get("status")
                .and_then(Value::as_str)
                .unwrap_or_default();
            inspect_web_search_call(
                id,
                status,
                block.get("action").unwrap_or(&Value::Null),
                "web_search_call",
            )?;
            if let Some(extra) = block.get("extra") {
                let extra = extra.as_object().ok_or_else(|| {
                    Error::Custom("web_search_call extra must be an object".into())
                })?;
                if extra
                    .keys()
                    .any(|key| matches!(key.as_str(), "type" | "id" | "status" | "action"))
                {
                    return Err(Error::Custom(
                        "web_search_call extra cannot shadow known item fields".into(),
                    ));
                }
            }
            if block.get("native").is_some() {
                return Err(Error::Custom(
                    "web_search_call does not support native annotations".into(),
                ));
            }
        }
        if let Some(children) = block.get("content").and_then(Value::as_array) {
            for child in children {
                check(child)?;
            }
        }
        Ok(())
    }
    if let Some(messages) = document.get("messages").and_then(Value::as_array) {
        for message in messages {
            check(message)?;
        }
    }
    if let Some(content) = document.get("content").and_then(Value::as_array) {
        for block in content {
            check(block)?;
        }
    }
    if let Some(events) = document.get("events").and_then(Value::as_array) {
        for event in events {
            if let Some(block) = event.get("block") {
                check(block)?;
            }
        }
    }
    Ok(())
}
