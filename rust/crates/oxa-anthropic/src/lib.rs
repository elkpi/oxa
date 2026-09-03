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
    BlockWire, ContentValue, Message, MessageStartWire, Request, Response, SourceWire, StreamDelta,
    StreamEvent, SystemBlockWire, SystemValue, ToolChoiceWire, ToolWire, UsageWire,
};
