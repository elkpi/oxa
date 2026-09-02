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
mod types;

pub use config::Config;
pub use decode::{decode_request, decode_response};
pub use encode::{encode_request, encode_response};
pub use error::Error;
pub use types::{
    Choice, ContentPart, ContentValue, FunctionWire, ImageURLWire, Message, Request, Response,
    ToolCall, ToolWire, UsageWire,
};
