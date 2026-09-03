//! The Anthropic Messages face of the oxa conversion library: the
//! non-streaming subset converted to and from the IR (spec/12). Wire types
//! are strictly separate from IR types; this crate imports `oxa-ir`,
//! `oxa-modelmap`, and the standard library only, never another face.
//!
//! Semantic unmappables are losses, never errors; errors are reserved for
//! structural type violations of known fields (spec/02 §4). Wire
//! `tool_use.input` objects are carried as raw JSON text without parsing
//! (INV-1, N-AN-4).

mod config;
mod decode;
mod encode;
mod error;
mod normalize;
mod streamin;
mod streamout;
mod types;

pub use config::Config;
pub use decode::{decode_request, decode_response};
pub use encode::{encode_request, encode_response};
pub use error::Error;
pub use streamin::StreamDecoder;
pub use streamout::StreamEncoder;
pub use types::{
    BLOCK_TYPE_IMAGE, BLOCK_TYPE_TEXT, BLOCK_TYPE_TOOL_RESULT, BLOCK_TYPE_TOOL_USE, BlockWire,
    ContentValue, DELTA_TYPE_INPUT_JSON_DELTA, DELTA_TYPE_TEXT_DELTA,
    EVENT_TYPE_CONTENT_BLOCK_DELTA, EVENT_TYPE_CONTENT_BLOCK_START, EVENT_TYPE_CONTENT_BLOCK_STOP,
    EVENT_TYPE_MESSAGE_DELTA, EVENT_TYPE_MESSAGE_START, EVENT_TYPE_MESSAGE_STOP, Message,
    MessageStartWire, ROLE_ASSISTANT, ROLE_USER, Request, Response, SOURCE_TYPE_BASE64,
    SOURCE_TYPE_URL, STOP_REASON_END_TURN, STOP_REASON_MAX_TOKENS, STOP_REASON_REFUSAL,
    STOP_REASON_STOP_SEQUENCE, STOP_REASON_TOOL_USE, SourceWire, StreamDelta, StreamEvent,
    SystemBlockWire, SystemValue, TOOL_CHOICE_TYPE_ANY, TOOL_CHOICE_TYPE_AUTO,
    TOOL_CHOICE_TYPE_NONE, TOOL_CHOICE_TYPE_TOOL, TYPE_MESSAGE, ToolChoiceWire, ToolWire,
    UsageWire,
};
