//! Streaming event types (spec/01 §5).

use serde::{Deserialize, Serialize};

use crate::request::Block;
use crate::response::{StopReason, Usage};

/// One streaming event (spec/01 §5.1). Sealed; exactly six variants in v1,
/// discriminated on the JSON `type` property. The event sequence obeys the
/// INV-5 grammar and INV-6 index discipline, enforced by [`crate::checker`].
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum Event {
    #[serde(rename = "message_start")]
    MessageStart {
        #[serde(rename = "id")]
        id: String,
        #[serde(rename = "model")]
        model: String,
    },
    #[serde(rename = "content_block_start")]
    ContentBlockStart {
        #[serde(rename = "index")]
        index: i64,
        #[serde(rename = "block")]
        block: Block,
    },
    #[serde(rename = "content_block_delta")]
    ContentBlockDelta {
        #[serde(rename = "index")]
        index: i64,
        #[serde(rename = "delta")]
        delta: Delta,
    },
    #[serde(rename = "content_block_stop")]
    ContentBlockStop {
        #[serde(rename = "index")]
        index: i64,
    },
    #[serde(rename = "message_delta")]
    MessageDelta {
        #[serde(rename = "stop_reason")]
        stop_reason: StopReason,
        #[serde(
            rename = "stop_sequence",
            default,
            skip_serializing_if = "Option::is_none"
        )]
        stop_sequence: Option<String>,
        #[serde(rename = "usage")]
        usage: Usage,
    },
    #[serde(rename = "message_done")]
    MessageDone {},
}

/// The delta payload of a `content_block_delta` event (spec/01 §5.2).
/// `InputJSONDelta::partial_json` is opaque raw JSON text (INV-1).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum Delta {
    #[serde(rename = "text_delta")]
    TextDelta {
        #[serde(rename = "text")]
        text: String,
    },
    #[serde(rename = "input_json_delta")]
    InputJsonDelta {
        #[serde(rename = "partial_json")]
        partial_json: String,
    },
}

/// The JSON document form of a streamed response (spec/01 §5.3).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct EventStream {
    #[serde(rename = "events")]
    pub events: Vec<Event>,
}
