//! The OpenAI Chat Completions face of the oxa conversion library: the
//! non-streaming subset converted to and from the IR (spec/10). Wire types
//! are strictly separate from IR types; this crate imports `oxa-ir`,
//! `oxa-modelmap`, and the standard library only, never another face.
//!
//! Semantic unmappables are losses, never errors; errors are reserved for
//! structural type violations of known fields (spec/02 §4).

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
    CONTENT_PART_TYPE_IMAGE_URL, CONTENT_PART_TYPE_TEXT, Choice, ChoiceDelta, Chunk, ContentPart,
    ContentValue, DeltaPayload, FINISH_REASON_CONTENT_FILTER, FINISH_REASON_LENGTH,
    FINISH_REASON_STOP, FINISH_REASON_TOOL_CALLS, FunctionDelta, FunctionWire, ImageURLWire,
    Message, OBJECT_CHAT_COMPLETION, OBJECT_CHAT_COMPLETION_CHUNK, ROLE_ASSISTANT, ROLE_SYSTEM,
    ROLE_TOOL, ROLE_USER, Request, Response, TOOL_CHOICE_AUTO, TOOL_CHOICE_NONE,
    TOOL_CHOICE_REQUIRED, TOOL_TYPE_FUNCTION, ToolCall, ToolCallDelta, ToolWire, UsageWire,
};
