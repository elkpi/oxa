//! A question definition for response-encode context (spec/17 §4.1).

use serde_json::Value;

/// The original request's question, keyed by its caller-chosen ID.
#[derive(Clone, Debug)]
pub struct JevQuestion {
    /// The question type (`noul`, `choice`, `score`).
    pub kind: String,
    pub instructions: Option<String>,
    pub criteria: Option<Value>,
}
