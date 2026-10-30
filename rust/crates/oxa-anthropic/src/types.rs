//! Anthropic Messages wire types for the supported non-streaming subset.
//! JSON property names are pinned with explicit `serde(rename)`. Wire
//! `tool_use.input` objects are captured as raw JSON text
//! (`serde_json::value::RawValue`) so the exact source bytes survive every
//! conversion path un-parsed (spec/01 INV-1).

use std::fmt;

use serde::de::{Error, SeqAccess, Visitor};
use serde::{Deserialize, Deserializer, Serialize};
use serde_json::Value;
use serde_json::value::RawValue;

pub const ROLE_USER: &str = "user";
pub const ROLE_ASSISTANT: &str = "assistant";
pub const ROLE_SYSTEM: &str = "system";

pub const BLOCK_TYPE_TEXT: &str = "text";
pub const BLOCK_TYPE_THINKING: &str = "thinking";
pub const BLOCK_TYPE_REDACTED_THINKING: &str = "redacted_thinking";
pub const BLOCK_TYPE_IMAGE: &str = "image";
pub const BLOCK_TYPE_TOOL_USE: &str = "tool_use";
pub const BLOCK_TYPE_TOOL_RESULT: &str = "tool_result";
pub const BLOCK_TYPE_DOCUMENT: &str = "document";

pub const SOURCE_TYPE_BASE64: &str = "base64";
pub const SOURCE_TYPE_URL: &str = "url";
pub const SOURCE_TYPE_FILE: &str = "file";

pub const TOOL_CHOICE_TYPE_AUTO: &str = "auto";
pub const TOOL_CHOICE_TYPE_ANY: &str = "any";
pub const TOOL_CHOICE_TYPE_NONE: &str = "none";
pub const TOOL_CHOICE_TYPE_TOOL: &str = "tool";

pub const STOP_REASON_END_TURN: &str = "end_turn";
pub const STOP_REASON_MAX_TOKENS: &str = "max_tokens";
pub const STOP_REASON_STOP_SEQUENCE: &str = "stop_sequence";
pub const STOP_REASON_TOOL_USE: &str = "tool_use";
pub const STOP_REASON_REFUSAL: &str = "refusal";

pub const EVENT_TYPE_MESSAGE_START: &str = "message_start";
pub const EVENT_TYPE_CONTENT_BLOCK_START: &str = "content_block_start";
pub const EVENT_TYPE_CONTENT_BLOCK_DELTA: &str = "content_block_delta";
pub const EVENT_TYPE_CONTENT_BLOCK_STOP: &str = "content_block_stop";
pub const EVENT_TYPE_MESSAGE_DELTA: &str = "message_delta";
pub const EVENT_TYPE_MESSAGE_STOP: &str = "message_stop";

pub const DELTA_TYPE_TEXT_DELTA: &str = "text_delta";
pub const DELTA_TYPE_INPUT_JSON_DELTA: &str = "input_json_delta";
pub const DELTA_TYPE_THINKING_DELTA: &str = "thinking_delta";
pub const DELTA_TYPE_SIGNATURE_DELTA: &str = "signature_delta";
pub const DELTA_TYPE_CITATIONS: &str = "citations_delta";

pub const TYPE_MESSAGE: &str = "message";

/// The Anthropic Messages wire request. `max_tokens` is required by the API
/// and is therefore not omitted.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Request {
    #[serde(rename = "model")]
    pub model: String,
    /// A string or an array of system blocks.
    #[serde(rename = "system", skip_serializing_if = "Option::is_none")]
    pub system: Option<SystemValue>,
    #[serde(rename = "messages")]
    pub messages: Vec<Message>,
    #[serde(rename = "max_tokens")]
    pub max_tokens: i64,
    #[serde(rename = "temperature", skip_serializing_if = "Option::is_none")]
    pub temperature: Option<f64>,
    #[serde(rename = "top_p", skip_serializing_if = "Option::is_none")]
    pub top_p: Option<f64>,
    #[serde(rename = "top_k", skip_serializing_if = "Option::is_none")]
    pub top_k: Option<i64>,
    #[serde(rename = "stop_sequences", skip_serializing_if = "Option::is_none")]
    pub stop_sequences: Option<Vec<String>>,
    #[serde(rename = "thinking", skip_serializing_if = "Option::is_none")]
    pub thinking: Option<ThinkingWire>,
    /// The specific {user_id} semantic; presence is dropped with a single
    /// unmapped-field loss.
    #[serde(rename = "metadata", skip_serializing_if = "Option::is_none")]
    pub metadata: Option<Value>,
    #[serde(rename = "tools", skip_serializing_if = "Option::is_none")]
    pub tools: Option<Vec<ToolWire>>,
    #[serde(rename = "tool_choice", skip_serializing_if = "Option::is_none")]
    pub tool_choice: Option<ToolChoiceWire>,
    #[serde(rename = "cache_control", skip_serializing_if = "Option::is_none")]
    pub cache_control: Option<Value>,
    #[serde(rename = "output_config", skip_serializing_if = "Option::is_none")]
    pub output_config: Option<OutputConfigWire>,
    #[serde(rename = "context_management", skip_serializing_if = "Option::is_none")]
    pub context_management: Option<ContextManagementWire>,
}

/// The request-side Anthropic thinking configuration.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ThinkingWire {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(
        rename = "budget_tokens",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub budget_tokens: Option<i64>,
    #[serde(rename = "display", skip_serializing_if = "Option::is_none")]
    pub display: Option<String>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct OutputConfigWire {
    #[serde(rename = "effort", skip_serializing_if = "Option::is_none")]
    pub effort: Option<String>,
    #[serde(rename = "format", skip_serializing_if = "Option::is_none")]
    pub format: Option<Value>,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ContextManagementWire {
    #[serde(rename = "edits", skip_serializing_if = "Option::is_none")]
    pub edits: Option<Vec<Value>>,
}

/// Request system content: a plain string or a block array.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum SystemValue {
    Text(String),
    Blocks(Vec<SystemBlockWire>),
}

/// One element of the system block array. `cache_control` is an Anthropic
/// prompt-caching annotation with no IR equivalent in v1; it is dropped with
/// an unmapped-field loss.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct SystemBlockWire {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "text")]
    pub text: String,
    #[serde(rename = "cache_control", skip_serializing_if = "Option::is_none")]
    pub cache_control: Option<Value>,
}

/// One element of the wire tools array. `input_schema` is a JSON-Schema-shaped
/// object carried verbatim (spec/01 §3.5).
#[derive(Clone, Debug, Default, PartialEq)]
pub struct ToolWire {
    pub tool_type: Option<String>,
    pub name: String,
    pub description: String,
    pub input_schema: Option<Value>,
    pub strict: Option<bool>,
    pub cache_control: Option<Value>,
    pub defer_loading: Option<bool>,
    pub eager_input_streaming: Option<bool>,
    pub raw_definition: Option<String>,
}

impl<'de> Deserialize<'de> for ToolWire {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        // Capture the exact declaration source so native tools survive typed
        // decode verbatim (spec/16 N-NT-2); ordinary tools re-serialize from
        // their fields so caller-side edits are honored.
        let raw_json = Box::<serde_json::value::RawValue>::deserialize(deserializer)?;
        let map: serde_json::Map<String, Value> =
            serde_json::from_str(raw_json.get()).map_err(serde::de::Error::custom)?;
        let tool_type = match map.get("type") {
            None => None,
            Some(Value::String(s)) => Some(s.clone()),
            Some(_) => {
                return Err(serde::de::Error::custom(
                    "anthropic: tool type must be a string",
                ));
            }
        };
        let name_val = map
            .get("name")
            .ok_or_else(|| serde::de::Error::custom("anthropic: tool name is required"))?;
        let name = match name_val {
            Value::String(s) if !s.is_empty() => s.clone(),
            Value::String(_) => {
                return Err(serde::de::Error::custom(
                    "anthropic: tool name cannot be empty",
                ));
            }
            _ => {
                return Err(serde::de::Error::custom(
                    "anthropic: tool name must be a string",
                ));
            }
        };
        let description = map
            .get("description")
            .and_then(|v| v.as_str())
            .unwrap_or_default()
            .to_string();
        let input_schema = map.get("input_schema").cloned();
        let strict = if map
            .get("type")
            .and_then(Value::as_str)
            .is_some_and(|t| !t.is_empty() && t != "function" && t != "custom")
        {
            None
        } else {
            match map.get("strict") {
                None => None,
                Some(Value::Bool(value)) => Some(*value),
                Some(_) => {
                    return Err(serde::de::Error::custom(
                        "anthropic: tool strict must be a boolean",
                    ));
                }
            }
        };
        let cache_control = map.get("cache_control").cloned();
        let is_native = tool_type
            .as_deref()
            .is_some_and(|t| !t.is_empty() && t != "function" && t != "custom");
        let defer_loading = if is_native {
            None
        } else {
            match map.get("defer_loading") {
                None => None,
                Some(Value::Bool(b)) => Some(*b),
                Some(_) => {
                    return Err(serde::de::Error::custom(
                        "anthropic: tool defer_loading must be a boolean",
                    ));
                }
            }
        };
        let eager_input_streaming = if is_native {
            None
        } else {
            match map.get("eager_input_streaming") {
                None => None,
                Some(Value::Bool(b)) => Some(*b),
                Some(_) => {
                    return Err(serde::de::Error::custom(
                        "anthropic: tool eager_input_streaming must be a boolean",
                    ));
                }
            }
        };
        Ok(ToolWire {
            tool_type,
            name,
            description,
            input_schema,
            strict,
            cache_control,
            defer_loading,
            eager_input_streaming,
            raw_definition: is_native.then(|| raw_json.get().to_string()),
        })
    }
}

impl Serialize for ToolWire {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        if let Some(ref raw_json) = self.raw_definition {
            let raw: &serde_json::value::RawValue =
                serde_json::from_str(raw_json).map_err(serde::ser::Error::custom)?;
            return raw.serialize(serializer);
        }
        #[derive(Serialize)]
        struct Shadow<'a> {
            #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
            tool_type: Option<&'a str>,
            name: &'a str,
            #[serde(skip_serializing_if = "str::is_empty")]
            description: &'a str,
            #[serde(skip_serializing_if = "Option::is_none")]
            input_schema: Option<&'a Value>,
            #[serde(skip_serializing_if = "Option::is_none")]
            strict: Option<bool>,
            #[serde(skip_serializing_if = "Option::is_none")]
            cache_control: Option<&'a Value>,
            #[serde(skip_serializing_if = "Option::is_none")]
            defer_loading: Option<bool>,
            #[serde(skip_serializing_if = "Option::is_none")]
            eager_input_streaming: Option<bool>,
        }
        let s = Shadow {
            tool_type: self.tool_type.as_deref(),
            name: &self.name,
            description: &self.description,
            input_schema: self.input_schema.as_ref(),
            strict: self.strict,
            cache_control: self.cache_control.as_ref(),
            defer_loading: self.defer_loading,
            eager_input_streaming: self.eager_input_streaming,
        };
        s.serialize(serializer)
    }
}

fn deserialize_bool_or_reject_null<'de, D>(deserializer: D) -> Result<Option<bool>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    struct BoolVisitor;
    impl<'de> serde::de::Visitor<'de> for BoolVisitor {
        type Value = Option<bool>;
        fn expecting(&self, formatter: &mut std::fmt::Formatter) -> std::fmt::Result {
            formatter.write_str("a boolean")
        }
        fn visit_bool<E>(self, v: bool) -> Result<Self::Value, E> {
            Ok(Some(v))
        }
    }
    deserializer.deserialize_bool(BoolVisitor)
}

/// The wire tool_choice object. `disable_parallel_tool_use` is preserved as native annotation (N-AN-19).
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ToolChoiceWire {
    /// auto | any | tool
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "name", skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(
        rename = "disable_parallel_tool_use",
        default,
        deserialize_with = "deserialize_bool_or_reject_null",
        skip_serializing_if = "Option::is_none"
    )]
    pub disable_parallel_tool_use: Option<bool>,
}

/// A wire message. Content is either a string or an array of blocks.
#[derive(Clone, Debug, Default, PartialEq, Serialize)]
#[serde(default)]
pub struct Message {
    #[serde(rename = "role")]
    pub role: String,
    #[serde(rename = "content")]
    pub content: Option<ContentValue>,
}

#[derive(Default, Deserialize)]
#[serde(default)]
struct GenericMessage {
    #[serde(rename = "role")]
    role: String,
    #[serde(rename = "content")]
    content: Option<Value>,
}

fn canonicalize_generic_numbers(value: Value) -> Value {
    match value {
        Value::Number(number) => {
            if number.is_i64() || number.is_u64() {
                Value::Number(number)
            } else {
                number
                    .as_f64()
                    .and_then(serde_json::Number::from_f64)
                    .map(Value::Number)
                    .unwrap_or(Value::Number(number))
            }
        }
        Value::Array(items) => Value::Array(
            items
                .into_iter()
                .map(canonicalize_generic_numbers)
                .collect(),
        ),
        Value::Object(fields) => Value::Object(
            fields
                .into_iter()
                .map(|(k, v)| (k, canonicalize_generic_numbers(v)))
                .collect(),
        ),
        other => other,
    }
}

impl<'de> Deserialize<'de> for Message {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let GenericMessage { role, content } = GenericMessage::deserialize(deserializer)?;
        let content = content
            .map(|content| {
                serde_json::from_value(canonicalize_generic_numbers(content))
                    .map_err(D::Error::custom)
            })
            .transpose()?;
        Ok(Message { role, content })
    }
}

/// Message and tool-result content: a plain string or a block array.
#[derive(Clone, Debug, PartialEq, Serialize)]
#[serde(untagged)]
pub enum ContentValue {
    Text(String),
    Blocks(Vec<BlockWire>),
}

impl<'de> Deserialize<'de> for ContentValue {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        struct ContentVisitor;

        impl<'de> Visitor<'de> for ContentVisitor {
            type Value = ContentValue;

            fn expecting(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
                formatter.write_str("a string or an array of Anthropic content blocks")
            }

            fn visit_str<E>(self, value: &str) -> Result<Self::Value, E>
            where
                E: serde::de::Error,
            {
                Ok(ContentValue::Text(value.to_string()))
            }

            fn visit_string<E>(self, value: String) -> Result<Self::Value, E>
            where
                E: serde::de::Error,
            {
                Ok(ContentValue::Text(value))
            }

            fn visit_seq<A>(self, mut sequence: A) -> Result<Self::Value, A::Error>
            where
                A: SeqAccess<'de>,
            {
                let mut blocks = Vec::new();
                while let Some(block) = sequence.next_element::<BlockWire>()? {
                    blocks.push(block);
                }
                Ok(ContentValue::Blocks(blocks))
            }
        }

        deserializer.deserialize_any(ContentVisitor)
    }
}

fn deserialize_optional_string<'de, D>(deserializer: D) -> Result<Option<String>, D::Error>
where
    D: Deserializer<'de>,
{
    String::deserialize(deserializer).map(Some)
}

/// Deserializes an optional JSON value while distinguishing an explicit
/// `null` from an absent field: `null` becomes `Some(Value::Null)`.
fn deserialize_optional_value<'de, D>(deserializer: D) -> Result<Option<Value>, D::Error>
where
    D: Deserializer<'de>,
{
    Value::deserialize(deserializer).map(Some)
}

/// One content block: text, image, tool_use, or tool_result. `input` is a raw
/// JSON object captured byte-exactly; decoders and encoders MUST copy it
/// without parsing or re-serializing it (INV-1).
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct BlockWire {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "text", skip_serializing_if = "String::is_empty")]
    pub text: String,
    #[serde(rename = "thinking", skip_serializing_if = "Option::is_none")]
    pub thinking: Option<String>,
    #[serde(rename = "signature", skip_serializing_if = "Option::is_none")]
    pub signature: Option<String>,
    // redacted_thinking
    #[serde(rename = "data", skip_serializing_if = "Option::is_none")]
    pub data: Option<String>,
    #[serde(rename = "cache_control", skip_serializing_if = "Option::is_none")]
    pub cache_control: Option<Value>,
    // tool_use
    #[serde(rename = "id", skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(rename = "name", skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(rename = "input", skip_serializing_if = "Option::is_none")]
    pub input: Option<Box<RawValue>>,
    // tool_result
    #[serde(rename = "tool_use_id", skip_serializing_if = "String::is_empty")]
    pub tool_use_id: String,
    #[serde(rename = "content", skip_serializing_if = "Option::is_none")]
    pub content: Option<ContentValue>,
    #[serde(rename = "is_error", skip_serializing_if = "std::ops::Not::not")]
    pub is_error: bool,
    // image
    #[serde(rename = "source", skip_serializing_if = "Option::is_none")]
    pub source: Option<SourceWire>,
    // document
    #[serde(
        rename = "title",
        default,
        deserialize_with = "deserialize_optional_string",
        skip_serializing_if = "Option::is_none"
    )]
    pub title: Option<String>,
    #[serde(
        rename = "context",
        default,
        deserialize_with = "deserialize_optional_string",
        skip_serializing_if = "Option::is_none"
    )]
    pub context: Option<String>,
    #[serde(
        rename = "citations",
        default,
        deserialize_with = "deserialize_optional_value",
        skip_serializing_if = "Option::is_none"
    )]
    pub citations: Option<Value>,
}

impl PartialEq for BlockWire {
    fn eq(&self, other: &Self) -> bool {
        self.kind == other.kind
            && self.text == other.text
            && self.thinking == other.thinking
            && self.signature == other.signature
            && self.data == other.data
            && self.cache_control == other.cache_control
            && self.id == other.id
            && self.name == other.name
            && self.input.as_deref().map(RawValue::get) == other.input.as_deref().map(RawValue::get)
            && self.tool_use_id == other.tool_use_id
            && self.content == other.content
            && self.is_error == other.is_error
            && self.source == other.source
            && self.title == other.title
            && self.context == other.context
            && self.citations == other.citations
    }
}

/// The source object of an image or document block: a base64 payload
/// (media_type + data), a URL, or a Files-API reference (file_id).
#[derive(Clone, Debug, Default, Serialize)]
pub struct SourceWire {
    /// base64 | url | file
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "media_type", skip_serializing_if = "String::is_empty")]
    pub media_type: String,
    #[serde(rename = "data", skip_serializing_if = "String::is_empty")]
    pub data: String,
    #[serde(rename = "url", skip_serializing_if = "String::is_empty")]
    pub url: String,
    #[serde(rename = "file_id", skip_serializing_if = "String::is_empty")]
    pub file_id: String,
    #[serde(skip)]
    pub has_media_type: bool,
    #[serde(skip)]
    pub has_data: bool,
    #[serde(skip)]
    pub has_url: bool,
    #[serde(skip)]
    pub has_file_id: bool,
}

impl PartialEq for SourceWire {
    fn eq(&self, other: &Self) -> bool {
        self.kind == other.kind
            && self.media_type == other.media_type
            && self.data == other.data
            && self.url == other.url
            && self.file_id == other.file_id
    }
}

impl<'de> Deserialize<'de> for SourceWire {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let value = Value::deserialize(deserializer)?;
        let object = value
            .as_object()
            .ok_or_else(|| D::Error::custom("source must be an object"))?;
        let read = |key: &str| -> Result<(String, bool), D::Error> {
            match object.get(key) {
                None => Ok((String::new(), false)),
                Some(v) => v
                    .as_str()
                    .map(|s| (s.to_string(), true))
                    .ok_or_else(|| D::Error::custom(format!("source.{key} must be a string"))),
            }
        };
        let (kind, _) = read("type")?;
        let (media_type, has_media_type) = read("media_type")?;
        let (data, has_data) = read("data")?;
        let (url, has_url) = read("url")?;
        let (file_id, has_file_id) = read("file_id")?;
        Ok(Self {
            kind,
            media_type,
            data,
            url,
            file_id,
            has_media_type,
            has_data,
            has_url,
            has_file_id,
        })
    }
}

/// The Anthropic Messages wire response object.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Response {
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "role")]
    pub role: String,
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "content")]
    pub content: Vec<BlockWire>,
    #[serde(rename = "stop_reason")]
    pub stop_reason: String,
    #[serde(rename = "stop_sequence", skip_serializing_if = "String::is_empty")]
    pub stop_sequence: String,
    #[serde(rename = "usage")]
    pub usage: Option<UsageWire>,
}

/// The wire usage object; input/output tokens map 1:1 to IR usage.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct UsageWire {
    #[serde(rename = "input_tokens")]
    pub input_tokens: i64,
    #[serde(rename = "output_tokens")]
    pub output_tokens: i64,
    #[serde(
        rename = "cache_creation_input_tokens",
        skip_serializing_if = "Option::is_none"
    )]
    pub cache_creation_input_tokens: Option<i64>,
    #[serde(
        rename = "cache_read_input_tokens",
        skip_serializing_if = "Option::is_none"
    )]
    pub cache_read_input_tokens: Option<i64>,
}

/// One Anthropic Messages streaming event.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct StreamEvent {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "message", skip_serializing_if = "Option::is_none")]
    pub message: Option<MessageStartWire>,
    #[serde(rename = "index", skip_serializing_if = "Option::is_none")]
    pub index: Option<i64>,
    #[serde(rename = "content_block", skip_serializing_if = "Option::is_none")]
    pub content_block: Option<BlockWire>,
    #[serde(rename = "delta", skip_serializing_if = "Option::is_none")]
    pub delta: Option<StreamDelta>,
    #[serde(rename = "usage", skip_serializing_if = "Option::is_none")]
    pub usage: Option<UsageWire>,
}

/// The message envelope carried by a message_start streaming event.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct MessageStartWire {
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "role")]
    pub role: String,
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "content")]
    pub content: Vec<BlockWire>,
    #[serde(rename = "stop_reason")]
    pub stop_reason: Option<String>,
    #[serde(rename = "usage")]
    pub usage: Option<UsageWire>,
}

/// The delta payload of content_block_delta and message_delta.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct StreamDelta {
    #[serde(rename = "type", skip_serializing_if = "String::is_empty")]
    pub kind: String,
    #[serde(rename = "text", skip_serializing_if = "String::is_empty")]
    pub text: String,
    #[serde(rename = "thinking", skip_serializing_if = "Option::is_none")]
    pub thinking: Option<String>,
    #[serde(rename = "signature", skip_serializing_if = "Option::is_none")]
    pub signature: Option<String>,
    #[serde(rename = "citation", skip_serializing_if = "Option::is_none")]
    pub citation: Option<Value>,
    #[serde(rename = "partial_json", skip_serializing_if = "Option::is_none")]
    pub partial_json: Option<String>,
    #[serde(rename = "stop_reason", skip_serializing_if = "Option::is_none")]
    pub stop_reason: Option<String>,
    #[serde(rename = "stop_sequence", skip_serializing_if = "Option::is_none")]
    pub stop_sequence: Option<String>,
}
