//! Document-layer JSON codec (spec/01 §6). The `specVersion` property is a
//! property of the serialized document, not of the in-memory types: the
//! codec inserts it on encode and validates the const on decode.

use serde::Serialize;
use serde::de::DeserializeOwned;
use serde_json::Value;

/// The IR contract version emitted by this crate (spec/01 §6).
pub const SPEC_VERSION: &str = "0.2.0";

/// Codec and validation errors.
#[derive(Debug)]
pub enum Error {
    /// The JSON text is not valid JSON or does not match an IR shape.
    Json(serde_json::Error),
    /// The document carries no `specVersion` property.
    MissingSpecVersion,
    /// The document carries a `specVersion` other than [`SPEC_VERSION`].
    SpecVersionMismatch(String),
    /// A general validation error.
    Custom(String),
    /// Native annotations are forbidden in response and event content blocks.
    NativeForbidden(String),
    /// Document blocks are request-side only and forbidden in response content.
    DocumentForbidden(String),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Json(e) => write!(f, "IR JSON: {e}"),
            Error::MissingSpecVersion => write!(f, "IR document carries no specVersion"),
            Error::SpecVersionMismatch(got) => write!(
                f,
                "IR document specVersion {got:?}, want \"0.1.0\" or \"0.2.0\""
            ),
            Error::Custom(msg) => write!(f, "{msg}"),
            Error::NativeForbidden(msg) => write!(f, "{msg}"),
            Error::DocumentForbidden(msg) => write!(f, "{msg}"),
        }
    }
}

impl std::error::Error for Error {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            Error::Json(e) => Some(e),
            _ => None,
        }
    }
}

impl From<serde_json::Error> for Error {
    fn from(e: serde_json::Error) -> Self {
        Error::Json(e)
    }
}

fn check_no_native(val: &Value) -> Result<(), Error> {
    if let Value::Object(map) = val {
        if map.contains_key("native") {
            return Err(Error::NativeForbidden(
                "ir: native annotations are forbidden in response and event content blocks (spec/15)".into(),
            ));
        }
        if map.get("type").and_then(Value::as_str) == Some("web_search_call") {
            return Ok(());
        }
        for v in map.values() {
            check_no_native(v)?;
        }
    } else if let Value::Array(arr) = val {
        for v in arr {
            check_no_native(v)?;
        }
    }
    Ok(())
}

/// Rejects `document` blocks in response content: `DocumentBlock` is
/// request-side only (spec/01 §3.4). Only `Response` documents carry a
/// top-level `content` array.
fn check_no_document(val: &Value) -> Result<(), Error> {
    if let Value::Object(map) = val
        && let Some(Value::Array(content)) = map.get("content")
    {
        for block in content {
            if block.get("type").and_then(Value::as_str) == Some("document") {
                return Err(Error::DocumentForbidden(
                    "ir: document blocks are request-side only and forbidden in response content (spec/01)"
                        .into(),
                ));
            }
        }
    }
    Ok(())
}

fn validate_citation_value(val_str: &str) -> Result<(), Error> {
    if val_str.is_empty() {
        return Err(Error::Custom("ir: citation value cannot be empty".into()));
    }
    let val: serde_json::Value = serde_json::from_str(val_str)
        .map_err(|e| Error::Custom(format!("ir: citation value must be a JSON object: {e}")))?;
    let Some(map) = val.as_object() else {
        return Err(Error::Custom(
            "ir: citation value must be a JSON object".into(),
        ));
    };
    let Some(cite_type) = map.get("type").and_then(|v| v.as_str()) else {
        return Err(Error::Custom(
            "ir: citation value requires type field".into(),
        ));
    };
    match cite_type {
        "char_location" => {
            for k in map.keys() {
                if ![
                    "type",
                    "cited_text",
                    "document_index",
                    "document_title",
                    "start_char_index",
                    "end_char_index",
                    "file_id",
                ]
                .contains(&k.as_str())
                {
                    return Err(Error::Custom(format!(
                        "ir: unknown property {k:?} on char_location citation"
                    )));
                }
            }
            if !map.get("cited_text").is_some_and(|v| v.is_string()) {
                return Err(Error::Custom(
                    "ir: char_location requires cited_text".into(),
                ));
            }
            if !map
                .get("document_index")
                .is_some_and(|v| v.as_i64().is_some_and(|n| n >= 0))
            {
                return Err(Error::Custom(
                    "ir: char_location requires document_index >= 0".into(),
                ));
            }
            if !map
                .get("document_title")
                .is_some_and(|v| v.is_null() || v.is_string())
            {
                return Err(Error::Custom(
                    "ir: char_location requires document_title".into(),
                ));
            }
            let start = map.get("start_char_index").and_then(|v| v.as_i64());
            let end = map.get("end_char_index").and_then(|v| v.as_i64());
            if start.is_none() || start.unwrap() < 0 {
                return Err(Error::Custom(
                    "ir: char_location requires start_char_index >= 0".into(),
                ));
            }
            if end.is_none() || end.unwrap() <= start.unwrap() {
                return Err(Error::Custom(
                    "ir: char_location requires end_char_index > start_char_index".into(),
                ));
            }
        }
        "page_location" => {
            for k in map.keys() {
                if ![
                    "type",
                    "cited_text",
                    "document_index",
                    "document_title",
                    "start_page_number",
                    "end_page_number",
                    "file_id",
                ]
                .contains(&k.as_str())
                {
                    return Err(Error::Custom(format!(
                        "ir: unknown property {k:?} on page_location citation"
                    )));
                }
            }
            if !map.get("cited_text").is_some_and(|v| v.is_string()) {
                return Err(Error::Custom(
                    "ir: page_location requires cited_text".into(),
                ));
            }
            if !map
                .get("document_index")
                .is_some_and(|v| v.as_i64().is_some_and(|n| n >= 0))
            {
                return Err(Error::Custom(
                    "ir: page_location requires document_index >= 0".into(),
                ));
            }
            if !map
                .get("document_title")
                .is_some_and(|v| v.is_null() || v.is_string())
            {
                return Err(Error::Custom(
                    "ir: page_location requires document_title".into(),
                ));
            }
            let start = map.get("start_page_number").and_then(|v| v.as_i64());
            let end = map.get("end_page_number").and_then(|v| v.as_i64());
            if start.is_none() || start.unwrap() < 1 {
                return Err(Error::Custom(
                    "ir: page_location requires start_page_number >= 1".into(),
                ));
            }
            if end.is_none() || end.unwrap() < start.unwrap() {
                return Err(Error::Custom(
                    "ir: page_location requires end_page_number >= start_page_number".into(),
                ));
            }
        }
        "content_block_location" => {
            for k in map.keys() {
                if ![
                    "type",
                    "cited_text",
                    "document_index",
                    "document_title",
                    "start_block_index",
                    "end_block_index",
                    "file_id",
                ]
                .contains(&k.as_str())
                {
                    return Err(Error::Custom(format!(
                        "ir: unknown property {k:?} on content_block_location citation"
                    )));
                }
            }
            if !map.get("cited_text").is_some_and(|v| v.is_string()) {
                return Err(Error::Custom(
                    "ir: content_block_location requires cited_text".into(),
                ));
            }
            if !map
                .get("document_index")
                .is_some_and(|v| v.as_i64().is_some_and(|n| n >= 0))
            {
                return Err(Error::Custom(
                    "ir: content_block_location requires document_index >= 0".into(),
                ));
            }
            if !map
                .get("document_title")
                .is_some_and(|v| v.is_null() || v.is_string())
            {
                return Err(Error::Custom(
                    "ir: content_block_location requires document_title".into(),
                ));
            }
            let start = map.get("start_block_index").and_then(|v| v.as_i64());
            let end = map.get("end_block_index").and_then(|v| v.as_i64());
            if start.is_none() || start.unwrap() < 0 {
                return Err(Error::Custom(
                    "ir: content_block_location requires start_block_index >= 0".into(),
                ));
            }
            if end.is_none() || end.unwrap() <= start.unwrap() {
                return Err(Error::Custom(
                    "ir: content_block_location requires end_block_index > start_block_index"
                        .into(),
                ));
            }
        }
        "web_search_result_location" => {
            for k in map.keys() {
                if !["type", "cited_text", "encrypted_index", "title", "url"].contains(&k.as_str())
                {
                    return Err(Error::Custom(format!(
                        "ir: unknown property {k:?} on web_search_result_location citation"
                    )));
                }
            }
            if !map.get("cited_text").is_some_and(|v| v.is_string()) {
                return Err(Error::Custom(
                    "ir: web_search_result_location requires cited_text".into(),
                ));
            }
            if !map.get("encrypted_index").is_some_and(|v| v.is_string()) {
                return Err(Error::Custom(
                    "ir: web_search_result_location requires encrypted_index".into(),
                ));
            }
            if !map
                .get("title")
                .is_some_and(|v| v.is_null() || v.is_string())
            {
                return Err(Error::Custom(
                    "ir: web_search_result_location requires title".into(),
                ));
            }
            if !map.get("url").is_some_and(|v| v.is_string()) {
                return Err(Error::Custom(
                    "ir: web_search_result_location requires url".into(),
                ));
            }
        }
        "search_result_location" => {
            for k in map.keys() {
                if ![
                    "type",
                    "cited_text",
                    "search_result_index",
                    "source",
                    "title",
                    "start_block_index",
                    "end_block_index",
                ]
                .contains(&k.as_str())
                {
                    return Err(Error::Custom(format!(
                        "ir: unknown property {k:?} on search_result_location citation"
                    )));
                }
            }
            if !map.get("cited_text").is_some_and(|v| v.is_string()) {
                return Err(Error::Custom(
                    "ir: search_result_location requires cited_text".into(),
                ));
            }
            if !map
                .get("search_result_index")
                .is_some_and(|v| v.as_i64().is_some_and(|n| n >= 0))
            {
                return Err(Error::Custom(
                    "ir: search_result_location requires search_result_index >= 0".into(),
                ));
            }
            if !map.get("source").is_some_and(|v| v.is_string()) {
                return Err(Error::Custom(
                    "ir: search_result_location requires source".into(),
                ));
            }
            if !map
                .get("title")
                .is_some_and(|v| v.is_null() || v.is_string())
            {
                return Err(Error::Custom(
                    "ir: search_result_location requires title".into(),
                ));
            }
            let start = map.get("start_block_index").and_then(|v| v.as_i64());
            let end = map.get("end_block_index").and_then(|v| v.as_i64());
            if start.is_none() || start.unwrap() < 0 {
                return Err(Error::Custom(
                    "ir: search_result_location requires start_block_index >= 0".into(),
                ));
            }
            if end.is_none() || end.unwrap() <= start.unwrap() {
                return Err(Error::Custom(
                    "ir: search_result_location requires end_block_index > start_block_index"
                        .into(),
                ));
            }
        }
        _ => {
            return Err(Error::Custom(format!(
                "ir: unknown citation type {cite_type:?}"
            )));
        }
    }
    Ok(())
}

fn check_citations(val: &Value) -> Result<(), Error> {
    if let Value::Object(map) = val {
        if map.get("type").and_then(Value::as_str) == Some("web_search_call") {
            return Ok(());
        }
        if let Some(Value::Array(cites)) = map.get("citations") {
            for c in cites {
                if let Some(src) = c.get("source").and_then(Value::as_str)
                    && src != "anthropic"
                {
                    return Err(Error::Custom(format!(
                        "ir: unknown citation source {src:?}"
                    )));
                }
                if let Some(v) = c.get("value").and_then(Value::as_str) {
                    validate_citation_value(v)?;
                }
            }
        }
        if let Some(delta) = map.get("delta")
            && delta.get("type").and_then(Value::as_str) == Some("citation_delta")
            && let Some(c) = delta.get("citation")
        {
            if let Some(src) = c.get("source").and_then(Value::as_str)
                && src != "anthropic"
            {
                return Err(Error::Custom(format!(
                    "ir: unknown citation source {src:?}"
                )));
            }
            if let Some(v) = c.get("value").and_then(Value::as_str) {
                validate_citation_value(v)?;
            }
        }
        for v in map.values() {
            check_citations(v)?;
        }
    } else if let Value::Array(arr) = val {
        for v in arr {
            check_citations(v)?;
        }
    }
    Ok(())
}

/// Serializes one IR document (`Request`, `Response`, or `EventStream`) to
/// its JSON form, inserting the `specVersion` property.
pub fn to_json<T: Serialize>(value: &T) -> Result<String, Error> {
    let mut document = serde_json::to_value(value)?;
    if document.get("content").is_some() || document.get("events").is_some() {
        check_no_native(&document)?;
    }
    check_no_document(&document)?;
    check_citations(&document)?;
    crate::web_search::check_web_search_blocks(&document)?;
    let object = document.as_object_mut().ok_or(Error::MissingSpecVersion)?;
    object.insert(
        "specVersion".to_string(),
        Value::String(SPEC_VERSION.to_string()),
    );
    Ok(serde_json::to_string(&document)?)
}

/// Deserializes one IR document from its JSON form, validating the
/// `specVersion` const first.
pub fn from_json<T: DeserializeOwned>(json: &str) -> Result<T, Error> {
    let document: Value = serde_json::from_str(json)?;
    if document.get("content").is_some() || document.get("events").is_some() {
        check_no_native(&document)?;
    }
    check_no_document(&document)?;
    check_citations(&document)?;
    crate::web_search::check_web_search_blocks(&document)?;
    if let Some(format) = document
        .get("params")
        .and_then(|p| p.get("response_format"))
        && format.get("type").and_then(Value::as_str) == Some("json_schema")
        && let Some(name) = format.get("name")
        && name.as_str().is_none_or(str::is_empty)
    {
        return Err(Error::Custom(
            "params.response_format name must be non-empty when present".into(),
        ));
    }
    match document.get("specVersion") {
        None => Err(Error::MissingSpecVersion),
        Some(v) if v == "0.1.0" || v == "0.2.0" => Ok(serde_json::from_str(json)?),
        Some(other) => Err(Error::SpecVersionMismatch(
            other.as_str().unwrap_or_default().to_string(),
        )),
    }
}
