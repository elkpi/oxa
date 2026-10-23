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

/// Serializes one IR document (`Request`, `Response`, or `EventStream`) to
/// its JSON form, inserting the `specVersion` property.
pub fn to_json<T: Serialize>(value: &T) -> Result<String, Error> {
    let mut document = serde_json::to_value(value)?;
    if document.get("content").is_some() || document.get("events").is_some() {
        check_no_native(&document)?;
    }
    check_no_document(&document)?;
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
    match document.get("specVersion") {
        None => Err(Error::MissingSpecVersion),
        Some(v) if v == "0.1.0" || v == "0.2.0" => Ok(T::deserialize(&document)?),
        Some(other) => Err(Error::SpecVersionMismatch(
            other.as_str().unwrap_or_default().to_string(),
        )),
    }
}
