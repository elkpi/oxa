//! Chat Completions wire types for the supported non-streaming subset. JSON
//! property names are pinned with explicit `serde(rename)`; presence-only
//! loss fields are `Option<Value>` so their presence can be detected without
//! interpreting them (spec/02 §4).

use serde::{Deserialize, Serialize};
use serde_json::Value;

pub const ROLE_SYSTEM: &str = "system";
pub const ROLE_USER: &str = "user";
pub const ROLE_ASSISTANT: &str = "assistant";
pub const ROLE_TOOL: &str = "tool";

pub const TOOL_TYPE_FUNCTION: &str = "function";

pub const CONTENT_PART_TYPE_TEXT: &str = "text";
pub const CONTENT_PART_TYPE_IMAGE_URL: &str = "image_url";

pub const TOOL_CHOICE_AUTO: &str = "auto";
pub const TOOL_CHOICE_NONE: &str = "none";
pub const TOOL_CHOICE_REQUIRED: &str = "required";

pub const FINISH_REASON_STOP: &str = "stop";
pub const FINISH_REASON_LENGTH: &str = "length";
pub const FINISH_REASON_CONTENT_FILTER: &str = "content_filter";
pub const FINISH_REASON_TOOL_CALLS: &str = "tool_calls";

pub const OBJECT_CHAT_COMPLETION: &str = "chat.completion";
pub const OBJECT_CHAT_COMPLETION_CHUNK: &str = "chat.completion.chunk";

/// The Chat Completions wire request for the supported non-streaming subset.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Request {
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "messages")]
    pub messages: Vec<Message>,
    #[serde(rename = "temperature", skip_serializing_if = "Option::is_none")]
    pub temperature: Option<f64>,
    #[serde(rename = "top_p", skip_serializing_if = "Option::is_none")]
    pub top_p: Option<f64>,
    #[serde(rename = "max_tokens", skip_serializing_if = "Option::is_none")]
    pub max_tokens: Option<i64>,
    #[serde(rename = "stop", skip_serializing_if = "Option::is_none")]
    pub stop: Option<Vec<String>>,
    #[serde(rename = "tools", skip_serializing_if = "Option::is_none")]
    pub tools: Option<Vec<ToolWire>>,
    /// auto | none | required | a named-function object; kept raw so the
    /// decode side can mirror the documented loss behavior for unsupported
    /// forms.
    #[serde(rename = "tool_choice", skip_serializing_if = "Option::is_none")]
    pub tool_choice: Option<Value>,

    // The remaining fields have no IR equivalent in v1; their presence is
    // dropped with an unmapped-field loss (N-CC-9, spec/02 §4).
    #[serde(
        rename = "parallel_tool_calls",
        skip_serializing_if = "Option::is_none"
    )]
    pub parallel_tool_calls: Option<Value>,
    #[serde(rename = "functions", skip_serializing_if = "Option::is_none")]
    pub functions: Option<Value>,
    #[serde(rename = "function_call", skip_serializing_if = "Option::is_none")]
    pub function_call: Option<Value>,
    #[serde(rename = "response_format", skip_serializing_if = "Option::is_none")]
    pub response_format: Option<Value>,
    #[serde(rename = "logprobs", skip_serializing_if = "Option::is_none")]
    pub logprobs: Option<Value>,
    #[serde(rename = "top_logprobs", skip_serializing_if = "Option::is_none")]
    pub top_logprobs: Option<Value>,
    #[serde(rename = "metadata", skip_serializing_if = "Option::is_none")]
    pub metadata: Option<Value>,
}

/// One element of a wire tools array. The supported tool variant is
/// type:function.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ToolWire {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "function")]
    pub function: FunctionWire,
}

/// A Chat Completions function definition or call payload. `parameters` is a
/// JSON-Schema-shaped object carried verbatim; `arguments` is raw JSON text
/// held as a plain string and copied into the IR without parsing (INV-1).
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct FunctionWire {
    #[serde(rename = "name")]
    pub name: String,
    #[serde(rename = "description", skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(rename = "parameters", skip_serializing_if = "Option::is_none")]
    pub parameters: Option<Value>,
    #[serde(rename = "arguments", skip_serializing_if = "String::is_empty")]
    pub arguments: String,
}

/// A wire message. `content` is a string or an array of content parts;
/// `tool_calls` is populated on assistant messages, `tool_call_id` on
/// role:"tool" result messages.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Message {
    #[serde(rename = "role")]
    pub role: String,
    #[serde(rename = "content", skip_serializing_if = "Option::is_none")]
    pub content: Option<ContentValue>,
    #[serde(rename = "tool_calls", skip_serializing_if = "Option::is_none")]
    pub tool_calls: Option<Vec<ToolCall>>,
    #[serde(rename = "tool_call_id", skip_serializing_if = "String::is_empty")]
    pub tool_call_id: String,
    #[serde(rename = "function_call", skip_serializing_if = "Option::is_none")]
    pub function_call: Option<Value>,
}

/// An assistant function invocation. `arguments` is opaque raw JSON text
/// (INV-1); it is never parsed on any conversion path.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ToolCall {
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "function")]
    pub function: FunctionWire,
}

/// One element of a parts-array message content.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ContentPart {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "text", skip_serializing_if = "String::is_empty")]
    pub text: String,
    #[serde(rename = "image_url", skip_serializing_if = "Option::is_none")]
    pub image_url: Option<ImageURLWire>,
}

/// Holds the URL of an image_url content part: either an https URL or a
/// data:<image MIME>;base64,<payload> URL in the supported set.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ImageURLWire {
    #[serde(rename = "url")]
    pub url: String,
}

/// Message content: a plain string or a parts array (N-CC-1).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum ContentValue {
    Text(String),
    Parts(Vec<ContentPart>),
}

/// The Chat Completions wire response object.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Response {
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "object")]
    pub object: String,
    #[serde(rename = "created")]
    pub created: i64,
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "choices")]
    pub choices: Vec<Choice>,
    #[serde(rename = "usage")]
    pub usage: Option<UsageWire>,
}

/// One element of a wire response's choices array.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Choice {
    #[serde(rename = "index")]
    pub index: i64,
    #[serde(rename = "message")]
    pub message: Message,
    #[serde(rename = "finish_reason")]
    pub finish_reason: String,
}

/// The wire usage object. `total_tokens` is derived (prompt + completion) and
/// recomputed on encode, so its absence on the IR side carries no loss
/// (vectors/README.md loss conventions, DERIVED fields).
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct UsageWire {
    #[serde(rename = "prompt_tokens")]
    pub prompt_tokens: i64,
    #[serde(rename = "completion_tokens")]
    pub completion_tokens: i64,
    #[serde(rename = "total_tokens")]
    pub total_tokens: i64,
}

/// One Chat Completions streamed chunk (object "chat.completion.chunk").
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Chunk {
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "object")]
    pub object: String,
    #[serde(rename = "created")]
    pub created: i64,
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "choices")]
    pub choices: Vec<ChoiceDelta>,
    #[serde(rename = "usage", skip_serializing_if = "Option::is_none")]
    pub usage: Option<UsageWire>,
}

/// One element of a wire chunk's choices array.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ChoiceDelta {
    #[serde(rename = "index")]
    pub index: i64,
    #[serde(rename = "delta")]
    pub delta: DeltaPayload,
    #[serde(rename = "finish_reason")]
    pub finish_reason: Option<String>,
}

/// The incremental delta object of a chunk choice.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct DeltaPayload {
    #[serde(rename = "role", skip_serializing_if = "String::is_empty")]
    pub role: String,
    #[serde(rename = "content", skip_serializing_if = "Option::is_none")]
    pub content: Option<String>,
    #[serde(rename = "tool_calls", skip_serializing_if = "Option::is_none")]
    pub tool_calls: Option<Vec<ToolCallDelta>>,
}

/// One incremental Chat Completions tool call.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ToolCallDelta {
    #[serde(rename = "index")]
    pub index: usize,
    #[serde(rename = "id", skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub kind: Option<String>,
    #[serde(rename = "function", skip_serializing_if = "Option::is_none")]
    pub function: Option<FunctionDelta>,
}

/// The incremental function payload of a tool call.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct FunctionDelta {
    #[serde(rename = "name", skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(rename = "arguments", skip_serializing_if = "Option::is_none")]
    pub arguments: Option<String>,
}
