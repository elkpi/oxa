//! Request-side IR types (spec/01 §3). All JSON property names are pinned
//! with explicit `serde(rename)` so no rename-rule inference is involved.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::BTreeMap;

/// A conversation to be sent to a model, face-neutral (spec/01 §3.1).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Request {
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "system", default, skip_serializing_if = "Vec::is_empty")]
    pub system: Vec<SystemBlock>,
    #[serde(rename = "messages")]
    pub messages: Vec<Message>,
    #[serde(rename = "tools", default, skip_serializing_if = "Option::is_none")]
    pub tools: Option<Vec<Tool>>,
    #[serde(
        rename = "tool_choice",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub tool_choice: Option<ToolChoice>,
    #[serde(rename = "params", default, skip_serializing_if = "Option::is_none")]
    pub params: Option<Params>,
    #[serde(rename = "metadata", default, skip_serializing_if = "Option::is_none")]
    pub metadata: Option<BTreeMap<String, String>>,
}

/// System prompt content (spec/01 §3.2). Sealed; exactly one variant in v1.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum SystemBlock {
    #[serde(rename = "text")]
    Text {
        #[serde(rename = "text")]
        text: String,
    },
}

/// Message role (spec/01 §3.3).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Role {
    #[serde(rename = "user")]
    User,
    #[serde(rename = "assistant")]
    Assistant,
}

/// A message: role plus an ordered block list (spec/01 §3.3).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Message {
    #[serde(rename = "role")]
    pub role: Role,
    #[serde(rename = "content")]
    pub content: Vec<Block>,
}

/// A content block (spec/01 §3.4). Sealed; exactly four variants in v1.
/// `ToolUseBlock::input` is opaque raw JSON text (INV-1): it is a plain
/// string and is never parsed or re-serialized by any conversion path.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum Block {
    #[serde(rename = "text")]
    Text {
        #[serde(rename = "text")]
        text: String,
    },
    #[serde(rename = "image")]
    Image {
        #[serde(
            rename = "media_type",
            default,
            skip_serializing_if = "Option::is_none"
        )]
        media_type: Option<String>,
        #[serde(rename = "data", default, skip_serializing_if = "Option::is_none")]
        data: Option<String>,
        #[serde(rename = "url", default, skip_serializing_if = "Option::is_none")]
        url: Option<String>,
    },
    #[serde(rename = "tool_use")]
    ToolUse {
        #[serde(rename = "id")]
        id: String,
        #[serde(rename = "name")]
        name: String,
        #[serde(rename = "input")]
        input: String,
    },
    #[serde(rename = "tool_result")]
    ToolResult {
        #[serde(rename = "tool_use_id")]
        tool_use_id: String,
        #[serde(rename = "content")]
        content: Vec<Block>,
        #[serde(rename = "is_error", default, skip_serializing_if = "Option::is_none")]
        is_error: Option<bool>,
    },
}

/// A tool definition (spec/01 §3.5). `input_schema` is a JSON-Schema-shaped
/// object carried verbatim; implementations MUST NOT analyze or rewrite it.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Tool {
    #[serde(rename = "name")]
    pub name: String,
    #[serde(
        rename = "description",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub description: Option<String>,
    #[serde(rename = "input_schema")]
    pub input_schema: Value,
}

/// Tool choice (spec/01 §3.6). `name` is present iff mode is `tool`.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct ToolChoice {
    #[serde(rename = "mode")]
    pub mode: ToolChoiceMode,
    #[serde(rename = "name", default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum ToolChoiceMode {
    #[serde(rename = "auto")]
    Auto,
    #[serde(rename = "any")]
    Any,
    #[serde(rename = "tool")]
    Tool,
    #[serde(rename = "none")]
    None,
}

/// Sampling parameters (spec/01 §3.7). Absent fields are meaningful:
/// `Option` distinguishes absent from zero, matching the pointer semantics
/// of the Go reference implementation.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Params {
    #[serde(
        rename = "temperature",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub temperature: Option<f64>,
    #[serde(rename = "top_p", default, skip_serializing_if = "Option::is_none")]
    pub top_p: Option<f64>,
    #[serde(
        rename = "max_tokens",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub max_tokens: Option<i64>,
    #[serde(
        rename = "stop_sequences",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub stop_sequences: Option<Vec<String>>,
}
