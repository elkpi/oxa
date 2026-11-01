//! OpenAI Responses wire types, intentionally distinct from `oxa-ir` types.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;

pub const ROLE_SYSTEM: &str = "system";
pub const ROLE_USER: &str = "user";
pub const ROLE_ASSISTANT: &str = "assistant";
pub const ROLE_DEVELOPER: &str = "developer";

pub const TOOL_TYPE_FUNCTION: &str = "function";
pub const TOOL_TYPE_CUSTOM: &str = "custom";
pub const TOOL_TYPE_APPLY_PATCH: &str = "apply_patch";

pub const TOOL_CHOICE_AUTO: &str = "auto";
pub const TOOL_CHOICE_NONE: &str = "none";
pub const TOOL_CHOICE_REQUIRED: &str = "required";

pub const ITEM_TYPE_MESSAGE: &str = "message";
pub const ITEM_TYPE_FUNCTION_CALL: &str = "function_call";
pub const ITEM_TYPE_FUNCTION_CALL_OUTPUT: &str = "function_call_output";
pub const ITEM_TYPE_CUSTOM_TOOL_CALL: &str = "custom_tool_call";
pub const ITEM_TYPE_CUSTOM_TOOL_CALL_OUTPUT: &str = "custom_tool_call_output";
pub const ITEM_TYPE_APPLY_PATCH_CALL: &str = "apply_patch_call";
pub const ITEM_TYPE_APPLY_PATCH_CALL_OUTPUT: &str = "apply_patch_call_output";
pub const ITEM_TYPE_REASONING: &str = "reasoning";
pub const ITEM_TYPE_WEB_SEARCH_CALL: &str = "web_search_call";

pub const PART_TYPE_INPUT_TEXT: &str = "input_text";
pub const PART_TYPE_OUTPUT_TEXT: &str = "output_text";
pub const PART_TYPE_INPUT_IMAGE: &str = "input_image";
pub const PART_TYPE_INPUT_FILE: &str = "input_file";

pub const STATUS_IN_PROGRESS: &str = "in_progress";
pub const STATUS_COMPLETED: &str = "completed";
pub const STATUS_INCOMPLETE: &str = "incomplete";
pub const STATUS_FAILED: &str = "failed";

pub const INCOMPLETE_REASON_MAX_OUTPUT_TOKENS: &str = "max_output_tokens";

pub const ERROR_CODE_REFUSAL: &str = "refusal";

pub const OBJECT_RESPONSE: &str = "response";

pub const EVENT_TYPE_RESPONSE_CREATED: &str = "response.created";
pub const EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED: &str = "response.output_item.added";
pub const EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE: &str = "response.output_item.done";
pub const EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED: &str = "response.content_part.added";
pub const EVENT_TYPE_RESPONSE_CONTENT_PART_DONE: &str = "response.content_part.done";
pub const EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA: &str = "response.output_text.delta";
pub const EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE: &str = "response.output_text.done";
pub const EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_ADDED: &str =
    "response.reasoning_summary_part.added";
pub const EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_DONE: &str =
    "response.reasoning_summary_part.done";
pub const EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DELTA: &str =
    "response.reasoning_summary_text.delta";
pub const EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DONE: &str =
    "response.reasoning_summary_text.done";
pub const EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA: &str =
    "response.function_call_arguments.delta";
pub const EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE: &str =
    "response.function_call_arguments.done";
pub const EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DELTA: &str =
    "response.custom_tool_call_input.delta";
pub const EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DONE: &str = "response.custom_tool_call_input.done";
pub const EVENT_TYPE_RESPONSE_COMPLETED: &str = "response.completed";
pub const EVENT_TYPE_RESPONSE_INCOMPLETE: &str = "response.incomplete";
pub const EVENT_TYPE_RESPONSE_FAILED: &str = "response.failed";

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
    #[serde(rename = "store", default, skip_serializing_if = "Option::is_none")]
    pub store: Option<bool>,
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
    pub parallel_tool_calls: Option<Value>,
    #[serde(
        rename = "prompt_cache_key",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub prompt_cache_key: Option<Value>,
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

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct ApplyPatchOperationWire {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "path", default, skip_serializing_if = "Option::is_none")]
    pub path: Option<serde_json::Value>,
    #[serde(rename = "diff", default, skip_serializing_if = "Option::is_none")]
    pub diff: Option<serde_json::Value>,
}

/// One Responses input item.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct InputItem {
    #[serde(rename = "type", default, skip_serializing_if = "String::is_empty")]
    pub kind: String,
    #[serde(rename = "id", default, skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(rename = "status", default, skip_serializing_if = "String::is_empty")]
    pub status: String,
    #[serde(rename = "role", default, skip_serializing_if = "String::is_empty")]
    pub role: String,
    #[serde(rename = "content", default, skip_serializing_if = "Option::is_none")]
    pub content: Option<ContentValue>,
    #[serde(
        rename = "summary",
        default,
        skip_serializing_if = "Vec::is_empty",
        deserialize_with = "deserialize_summary_parts"
    )]
    pub summary: Vec<OutputPart>,
    #[serde(rename = "call_id", default, skip_serializing_if = "String::is_empty")]
    pub call_id: String,
    #[serde(rename = "name", default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(rename = "namespace", default, skip_serializing_if = "Option::is_none")]
    pub namespace: Option<String>,
    #[serde(
        rename = "arguments",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub arguments: String,
    #[serde(rename = "input", default, skip_serializing_if = "Option::is_none")]
    pub input: Option<String>,
    #[serde(rename = "output", default, skip_serializing_if = "Option::is_none")]
    pub output: Option<OutputValue>,
    #[serde(rename = "operation", default, skip_serializing_if = "Option::is_none")]
    pub operation: Option<ApplyPatchOperationWire>,
    #[serde(rename = "tools", default, skip_serializing_if = "Option::is_none")]
    pub tools: Option<Vec<Value>>,
    #[serde(rename = "action", default, skip_serializing_if = "Option::is_none")]
    pub action: Option<Value>,
    #[serde(flatten, default, skip_serializing_if = "BTreeMap::is_empty")]
    pub extra: BTreeMap<String, Value>,
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
    #[serde(rename = "refusal", default, skip_serializing_if = "Option::is_none")]
    pub refusal: Option<String>,
    #[serde(
        rename = "image_url",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub image_url: String,
    #[serde(rename = "detail", default, skip_serializing_if = "Option::is_none")]
    pub detail: Option<String>,
    #[serde(rename = "file_id", default, skip_serializing_if = "Option::is_none")]
    pub file_id: Option<String>,
    #[serde(rename = "file_url", default, skip_serializing_if = "Option::is_none")]
    pub file_url: Option<String>,
    #[serde(rename = "file_data", default, skip_serializing_if = "Option::is_none")]
    pub file_data: Option<String>,
    #[serde(rename = "filename", default, skip_serializing_if = "Option::is_none")]
    pub filename: Option<String>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct ToolDef {
    pub kind: String,
    pub name: String,
    pub description: String,
    pub parameters: Option<Value>,
    pub format: Option<Value>,
    pub strict: Option<bool>,
    pub defer_loading: Option<bool>,
    pub tools: Option<Vec<Value>>,
    pub raw: Option<Value>,
    pub raw_definition: Option<String>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
struct ToolDefWire {
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
    #[serde(
        rename = "parameters",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub parameters: Option<Value>,
    #[serde(rename = "format", default, skip_serializing_if = "Option::is_none")]
    pub format: Option<Value>,
    #[serde(rename = "strict", default, skip_serializing_if = "Option::is_none")]
    pub strict: Option<bool>,
    #[serde(
        rename = "defer_loading",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub defer_loading: Option<bool>,
    #[serde(rename = "tools", default, skip_serializing_if = "Option::is_none")]
    pub tools: Option<Vec<Value>>,
}

impl Serialize for ToolDef {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        if let Some(ref raw) = self.raw {
            return raw.serialize(serializer);
        }
        let wire = ToolDefWire {
            kind: self.kind.clone(),
            name: self.name.clone(),
            description: self.description.clone(),
            parameters: self.parameters.clone(),
            format: self.format.clone(),
            strict: self.strict,
            defer_loading: self.defer_loading,
            tools: self.tools.clone(),
        };
        wire.serialize(serializer)
    }
}

impl<'de> Deserialize<'de> for ToolDef {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let raw_val: Box<serde_json::value::RawValue> = Box::deserialize(deserializer)?;
        let raw_str = raw_val.get();
        let val: Value = serde_json::from_str(raw_str).map_err(serde::de::Error::custom)?;
        let wire: ToolDefWire = serde_json::from_str(raw_str).map_err(serde::de::Error::custom)?;
        Ok(ToolDef {
            kind: wire.kind,
            name: wire.name,
            description: wire.description,
            parameters: wire.parameters,
            format: wire.format,
            strict: wire.strict,
            defer_loading: wire.defer_loading,
            tools: wire.tools,
            raw: Some(val),
            raw_definition: Some(raw_str.to_string()),
        })
    }
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
    #[serde(
        rename = "summary",
        default,
        skip_serializing_if = "Vec::is_empty",
        deserialize_with = "deserialize_summary_parts"
    )]
    pub summary: Vec<OutputPart>,
    #[serde(
        rename = "encrypted_content",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub encrypted_content: String,
    #[serde(rename = "call_id", default, skip_serializing_if = "String::is_empty")]
    pub call_id: String,
    #[serde(rename = "name", default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(rename = "namespace", default, skip_serializing_if = "Option::is_none")]
    pub namespace: Option<String>,
    #[serde(
        rename = "arguments",
        default,
        skip_serializing_if = "String::is_empty"
    )]
    pub arguments: String,
    #[serde(rename = "input", default, skip_serializing_if = "Option::is_none")]
    pub input: Option<String>,
    #[serde(rename = "output", default, skip_serializing_if = "Option::is_none")]
    pub output: Option<OutputValue>,
    #[serde(rename = "operation", default, skip_serializing_if = "Option::is_none")]
    pub operation: Option<ApplyPatchOperationWire>,
    #[serde(rename = "action", default, skip_serializing_if = "Option::is_none")]
    pub action: Option<Value>,
    #[serde(flatten, default, skip_serializing_if = "BTreeMap::is_empty")]
    pub extra: BTreeMap<String, Value>,
}

/// A custom-tool output string or ordered input-content list.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum OutputValue {
    Text(String),
    Parts(Vec<ContentPart>),
}

/// One Responses message-output content part.
#[derive(Clone, Debug, Default, PartialEq, Deserialize)]
pub struct OutputPart {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "text", default)]
    pub text: String,
    #[serde(rename = "refusal", default)]
    pub refusal: Option<String>,
    #[serde(rename = "annotations", default)]
    pub annotations: Vec<Value>,
}

impl Serialize for OutputPart {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        use serde::ser::SerializeStruct;
        if self.kind == "refusal" {
            let mut state = serializer.serialize_struct("OutputPart", 2)?;
            state.serialize_field("type", &self.kind)?;
            state.serialize_field("refusal", self.refusal.as_deref().unwrap_or(""))?;
            state.end()
        } else {
            let mut state = serializer.serialize_struct("OutputPart", 3)?;
            state.serialize_field("type", &self.kind)?;
            state.serialize_field("text", &self.text)?;
            state.serialize_field("annotations", &self.annotations)?;
            state.end()
        }
    }
}

fn deserialize_summary_parts<'de, D>(deserializer: D) -> Result<Vec<OutputPart>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    #[derive(Deserialize)]
    struct SummaryPart {
        #[serde(rename = "type", default)]
        kind: String,
        #[serde(default)]
        text: String,
        #[serde(default)]
        annotations: Vec<Value>,
    }

    Ok(Vec::<SummaryPart>::deserialize(deserializer)?
        .into_iter()
        .map(|part| OutputPart {
            kind: part.kind,
            text: part.text,
            annotations: part.annotations,
            ..Default::default()
        })
        .collect())
}

/// Cached input-token breakdown from Responses.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct InputTokenDetailsWire {
    #[serde(rename = "cached_tokens")]
    pub cached_tokens: i64,
}

/// Reasoning output-token breakdown from Responses.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct OutputTokenDetailsWire {
    #[serde(rename = "reasoning_tokens")]
    pub reasoning_tokens: i64,
}

/// Responses token usage. `total_tokens` is an envelope-derived field.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct UsageWire {
    #[serde(rename = "input_tokens")]
    pub input_tokens: i64,
    #[serde(rename = "output_tokens")]
    pub output_tokens: i64,
    #[serde(rename = "total_tokens")]
    pub total_tokens: i64,
    #[serde(
        rename = "input_token_details",
        skip_serializing_if = "Option::is_none"
    )]
    pub input_token_details: Option<InputTokenDetailsWire>,
    #[serde(
        rename = "output_token_details",
        skip_serializing_if = "Option::is_none"
    )]
    pub output_token_details: Option<OutputTokenDetailsWire>,
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
    #[serde(rename = "namespace", skip_serializing_if = "Option::is_none")]
    pub namespace: Option<String>,
    #[serde(rename = "arguments", skip_serializing_if = "Option::is_none")]
    pub arguments: Option<String>,
    #[serde(rename = "input", skip_serializing_if = "Option::is_none")]
    pub input: Option<String>,
    #[serde(rename = "sequence_number", skip_serializing_if = "Option::is_none")]
    pub sequence_number: Option<i64>,
}
