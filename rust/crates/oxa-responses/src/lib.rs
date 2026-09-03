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
    ContentPart, ContentValue, ERROR_CODE_REFUSAL, EVENT_TYPE_RESPONSE_COMPLETED,
    EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED, EVENT_TYPE_RESPONSE_CONTENT_PART_DONE,
    EVENT_TYPE_RESPONSE_CREATED, EVENT_TYPE_RESPONSE_FAILED,
    EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA, EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE,
    EVENT_TYPE_RESPONSE_INCOMPLETE, EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED,
    EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE, EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA,
    EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE, ErrorWire, INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_FUNCTION_CALL_OUTPUT, ITEM_TYPE_MESSAGE, IncompleteWire,
    Input, InputItem, OBJECT_RESPONSE, OutputItem, OutputPart, PART_TYPE_INPUT_IMAGE,
    PART_TYPE_INPUT_TEXT, PART_TYPE_OUTPUT_TEXT, ROLE_ASSISTANT, ROLE_DEVELOPER, ROLE_SYSTEM,
    ROLE_USER, Request, Response, STATUS_COMPLETED, STATUS_FAILED, STATUS_IN_PROGRESS,
    STATUS_INCOMPLETE, StreamEvent, TOOL_CHOICE_AUTO, TOOL_CHOICE_NONE, TOOL_CHOICE_REQUIRED,
    TOOL_TYPE_FUNCTION, TextParams, ToolDef, UsageWire,
};
