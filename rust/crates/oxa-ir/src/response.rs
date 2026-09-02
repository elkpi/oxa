//! Response-side IR types (spec/01 §4).

use serde::{Deserialize, Serialize};

use crate::request::Block;

/// A model response, face-neutral (spec/01 §4.1). `content` MAY be empty
/// (an event stream with zero blocks aggregates to this); it is always
/// serialized, even as `[]`.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Response {
    #[serde(rename = "id")]
    pub id: String,
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "content")]
    pub content: Vec<Block>,
    #[serde(rename = "stop_reason")]
    pub stop_reason: StopReason,
    #[serde(
        rename = "stop_sequence",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub stop_sequence: Option<String>,
    #[serde(rename = "usage")]
    pub usage: Usage,
}

/// Terminal reason (spec/01 §4.1). `other` is the escape hatch for
/// face-native stop reasons with no IR equivalent; mapping to it MUST
/// record a loss (spec/02).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum StopReason {
    #[serde(rename = "end_turn")]
    EndTurn,
    #[serde(rename = "max_tokens")]
    MaxTokens,
    #[serde(rename = "stop_sequence")]
    StopSequence,
    #[serde(rename = "tool_use")]
    ToolUse,
    #[serde(rename = "refusal")]
    Refusal,
    #[serde(rename = "other")]
    Other,
}

/// Token usage totals (spec/01 §4.2).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Usage {
    #[serde(rename = "input_tokens")]
    pub input_tokens: i64,
    #[serde(rename = "output_tokens")]
    pub output_tokens: i64,
}
