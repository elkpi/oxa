//! OpenAI Responses wire types, intentionally distinct from `oxa-ir` types.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// A Responses request.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct Request {
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "input", default)]
    pub input: Input,
    #[serde(
        rename = "instructions",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub instructions: Option<String>,
    #[serde(
        rename = "temperature",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub temperature: Option<f64>,
    #[serde(rename = "top_p", default, skip_serializing_if = "Option::is_none")]
    pub top_p: Option<f64>,
    #[serde(
        rename = "max_output_tokens",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub max_output_tokens: Option<i64>,
    #[serde(rename = "tools", default, skip_serializing_if = "Vec::is_empty")]
    pub tools: Vec<ToolDef>,
    #[serde(
        rename = "tool_choice",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub tool_choice: Option<Value>,
    #[serde(rename = "metadata", default, skip_serializing_if = "Option::is_none")]
    pub metadata: Option<BTreeMap<String, String>>,
    #[serde(rename = "text", default, skip_serializing_if = "Option::is_none")]
    pub text: Option<TextParams>,
    #[serde(rename = "reasoning", default, skip_serializing_if = "Option::is_none")]
    pub reasoning: Option<Value>,
    #[serde(
        rename = "parallel_tool_calls",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub parallel_tool_calls: Option<bool>,
}

/// Responses accepts a string shorthand or an ordered item array as input.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum Input {
    Text(String),
    Items(Vec<InputItem>),
}

impl Default for Input {
    fn default() -> Self {
        Input::Items(Vec::new())
    }
}

/// One Responses input item.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct InputItem {
    #[serde(rename = "type", default, skip_serializing_if = "String::is_empty")]
    pub kind: String,
    #[serde(rename = "role", default, skip_serializing_if = "String::is_empty")]
    pub role: String,
    #[serde(rename = "content", default, skip_serializing_if = "Option::is_none")]
    pub content: Option<ContentValue>,
    #[serde(rename = "call_id", default, skip_serializing_if = "String::is_empty")]
    pub call_id: String,
    #[serde(rename = "name", default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(
        rename = "arguments",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub arguments: String,
    #[serde(rename = "output", default, skip_serializing_if = "String::is_empty")]
    pub output: String,
}

/// Responses message content, either text shorthand or a part array.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum ContentValue {
    Text(String),
    Parts(Vec<ContentPart>),
}

/// One Responses input content part.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct ContentPart {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "text", default, skip_serializing_if = "String::is_empty")]
    pub text: String,
    #[serde(
        rename = "image_url",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub image_url: String,
}

/// A Responses function-tool definition.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct ToolDef {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "name", default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(
        rename = "description",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub description: String,
    #[serde(rename = "parameters", default)]
    pub parameters: Value,
    #[serde(rename = "strict", default, skip_serializing_if = "Option::is_none")]
    pub strict: Option<bool>,
}

/// Responses text-output configuration that has no v1 IR equivalent.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct TextParams {
    #[serde(rename = "verbosity", default, skip_serializing_if = "Option::is_none")]
    pub verbosity: Option<String>,
    #[serde(rename = "format", default, skip_serializing_if = "Option::is_none")]
    pub format: Option<Value>,
}

/// A Responses response envelope.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct Response {
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "object", default, skip_serializing_if = "String::is_empty")]
    pub object: String,
    #[serde(rename = "status")]
    pub status: String,
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "output", default)]
    pub output: Vec<OutputItem>,
    #[serde(rename = "usage", default, skip_serializing_if = "Option::is_none")]
    pub usage: Option<UsageWire>,
    #[serde(
        rename = "incomplete_details",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub incomplete_details: Option<IncompleteWire>,
    #[serde(rename = "error", default, skip_serializing_if = "Option::is_none")]
    pub error: Option<ErrorWire>,
}

/// One Responses output item.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct OutputItem {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "id", default, skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(rename = "status", default, skip_serializing_if = "String::is_empty")]
    pub status: String,
    #[serde(rename = "role", default, skip_serializing_if = "String::is_empty")]
    pub role: String,
    #[serde(rename = "content", default, skip_serializing_if = "Vec::is_empty")]
    pub content: Vec<OutputPart>,
    #[serde(rename = "call_id", default, skip_serializing_if = "String::is_empty")]
    pub call_id: String,
    #[serde(rename = "name", default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(
        rename = "arguments",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub arguments: String,
}

/// One Responses message-output content part.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct OutputPart {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "text", default)]
    pub text: String,
    #[serde(rename = "annotations", default)]
    pub annotations: Vec<Value>,
}

/// Responses token usage. `total_tokens` is an envelope-derived field.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct UsageWire {
    #[serde(rename = "input_tokens")]
    pub input_tokens: i64,
    #[serde(rename = "output_tokens")]
    pub output_tokens: i64,
    #[serde(rename = "total_tokens")]
    pub total_tokens: i64,
}

/// Why a Responses response was incomplete.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct IncompleteWire {
    #[serde(rename = "reason")]
    pub reason: String,
}

/// A failed Responses response's error identity.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct ErrorWire {
    #[serde(rename = "code", default)]
    pub code: String,
    #[serde(rename = "message", default)]
    pub message: String,
}

/// One OpenAI Responses streaming event.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct StreamEvent {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "response", skip_serializing_if = "Option::is_none")]
    pub response: Option<Response>,
    #[serde(rename = "item_id", skip_serializing_if = "Option::is_none")]
    pub item_id: Option<String>,
    #[serde(rename = "output_index", skip_serializing_if = "Option::is_none")]
    pub output_index: Option<i64>,
    #[serde(rename = "content_index", skip_serializing_if = "Option::is_none")]
    pub content_index: Option<i64>,
    #[serde(rename = "item", skip_serializing_if = "Option::is_none")]
    pub item: Option<OutputItem>,
    #[serde(rename = "part", skip_serializing_if = "Option::is_none")]
    pub part: Option<OutputPart>,
    #[serde(rename = "delta", skip_serializing_if = "Option::is_none")]
    pub delta: Option<String>,
    #[serde(rename = "text", skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
    #[serde(rename = "call_id", skip_serializing_if = "Option::is_none")]
    pub call_id: Option<String>,
    #[serde(rename = "name", skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(rename = "arguments", skip_serializing_if = "Option::is_none")]
    pub arguments: Option<String>,
    #[serde(rename = "sequence_number", skip_serializing_if = "Option::is_none")]
    pub sequence_number: Option<i64>,
}
