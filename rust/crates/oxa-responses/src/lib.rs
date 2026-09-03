//! The OpenAI Responses face of the oxa conversion library.
//!
//! Wire types remain separate from IR types, and this face depends only on
//! `oxa-ir` and `oxa-modelmap`, never another face crate.

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
    ContentPart, ContentValue, ErrorWire, IncompleteWire, Input, InputItem, OutputItem, OutputPart,
    Request, Response, StreamEvent, TextParams, ToolDef, UsageWire,
};
