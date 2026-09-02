//! Document-layer JSON codec (spec/01 §6). The `specVersion` property is a
//! property of the serialized document, not of the in-memory types: the
//! codec inserts it on encode and validates the const on decode.

use serde::Serialize;
use serde::de::DeserializeOwned;
use serde_json::Value;

/// The IR contract version (spec/01 §6), pinned by `const` in
/// `spec/schema/ir.schema.json` and echoed by every vector's `spec_version`.
pub const SPEC_VERSION: &str = "0.1.0";

/// Codec and validation errors.
#[derive(Debug)]
pub enum Error {
    /// The JSON text is not valid JSON or does not match an IR shape.
    Json(serde_json::Error),
    /// The document carries no `specVersion` property.
    MissingSpecVersion,
    /// The document carries a `specVersion` other than [`SPEC_VERSION`].
    SpecVersionMismatch(String),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Json(e) => write!(f, "IR JSON: {e}"),
            Error::MissingSpecVersion => write!(f, "IR document carries no specVersion"),
            Error::SpecVersionMismatch(got) => {
                write!(f, "IR document specVersion {got:?}, want {SPEC_VERSION:?}")
            }
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

/// Serializes one IR document (`Request`, `Response`, or `EventStream`) to
/// its JSON form, inserting the `specVersion` property.
pub fn to_json<T: Serialize>(value: &T) -> Result<String, Error> {
    let mut document = serde_json::to_value(value)?;
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
    match document.get("specVersion") {
        None => Err(Error::MissingSpecVersion),
        Some(v) if v == &Value::String(SPEC_VERSION.to_string()) => Ok(T::deserialize(&document)?),
        Some(other) => Err(Error::SpecVersionMismatch(
            other.as_str().unwrap_or_default().to_string(),
        )),
    }
}
