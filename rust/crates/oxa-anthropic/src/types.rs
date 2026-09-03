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
    #[serde(rename = "stop_sequences", skip_serializing_if = "Option::is_none")]
    pub stop_sequences: Option<Vec<String>>,
    /// The specific {user_id} semantic; presence is dropped with a single
    /// unmapped-field loss.
    #[serde(rename = "metadata", skip_serializing_if = "Option::is_none")]
    pub metadata: Option<Value>,
    #[serde(rename = "tools", skip_serializing_if = "Option::is_none")]
    pub tools: Option<Vec<ToolWire>>,
    #[serde(rename = "tool_choice", skip_serializing_if = "Option::is_none")]
    pub tool_choice: Option<ToolChoiceWire>,
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
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ToolWire {
    #[serde(rename = "name")]
    pub name: String,
    #[serde(rename = "description", skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(rename = "input_schema")]
    pub input_schema: Value,
}

/// The wire tool_choice object. `disable_parallel_tool_use` has no IR
/// equivalent and is dropped with an unmapped-field loss (N-AN-6).
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
        skip_serializing_if = "std::ops::Not::not"
    )]
    pub disable_parallel_tool_use: bool,
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

impl<'de> Deserialize<'de> for Message {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let GenericMessage { role, content } = GenericMessage::deserialize(deserializer)?;
        let content = content
            .map(|content| serde_json::from_value(content).map_err(D::Error::custom))
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
}

impl PartialEq for BlockWire {
    fn eq(&self, other: &Self) -> bool {
        self.kind == other.kind
            && self.text == other.text
            && self.cache_control == other.cache_control
            && self.id == other.id
            && self.name == other.name
            && self.input.as_deref().map(RawValue::get) == other.input.as_deref().map(RawValue::get)
            && self.tool_use_id == other.tool_use_id
            && self.content == other.content
            && self.is_error == other.is_error
            && self.source == other.source
    }
}

/// The source object of an image block: a base64 payload (media_type + data)
/// or a URL.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct SourceWire {
    /// base64 | url
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "media_type", skip_serializing_if = "String::is_empty")]
    pub media_type: String,
    #[serde(rename = "data", skip_serializing_if = "String::is_empty")]
    pub data: String,
    #[serde(rename = "url", skip_serializing_if = "String::is_empty")]
    pub url: String,
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
    #[serde(rename = "partial_json", skip_serializing_if = "Option::is_none")]
    pub partial_json: Option<String>,
    #[serde(rename = "stop_reason", skip_serializing_if = "Option::is_none")]
    pub stop_reason: Option<String>,
    #[serde(rename = "stop_sequence", skip_serializing_if = "Option::is_none")]
    pub stop_sequence: Option<String>,
}
