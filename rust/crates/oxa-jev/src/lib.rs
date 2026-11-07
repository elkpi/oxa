//! The SystemOne Jev face of the oxa conversion library: the structured
//! decision protocol (`state + questions → typed answers`) projected onto the
//! IR conversation model (spec/17). Nonstream only — Jev has no streaming
//! mode. Wire types are strictly separate from IR types; this crate imports
//! `oxa-ir`, `oxa-modelmap`, and the standard library only, never another
//! face. Semantic unmappables are losses, never errors; errors are reserved
//! for structural type violations of known fields (spec/02 §4).

mod config;
mod decode;
mod encode;
mod error;
mod json_util;
mod question;
mod rawjson;

pub use config::Config;
pub use decode::{WireRequest, WireResponse, WireUsage, decode_request, decode_response};
pub use encode::{encode_request, encode_response};
pub use error::Error;
pub use question::JevQuestion;
pub use rawjson::{decode_request_json, decode_response_json};

/// Renders the synthesized wire JSON for a rebuilt request (the vector
/// harness renders wire structs through serde).
pub fn encode_request_json(
    request: &IrRequest,
    config: &Config,
) -> Result<(String, Vec<oxa_ir::Loss>), Error> {
    let (wire, losses) = encode_request(request, config)?;
    let text = serde_json::to_string(&wire).map_err(|err| Error::new(err.to_string()))?;
    Ok((text, losses))
}

/// Renders the synthesized wire JSON for a parsed response (the vector
/// harness renders wire structs through serde).
pub fn encode_response_json(
    response: &IrResponse,
    config: &Config,
) -> Result<(String, Vec<oxa_ir::Loss>), Error> {
    let (wire, losses) = encode_response(response, config)?;
    let text = serde_json::to_string(&wire).map_err(|err| Error::new(err.to_string()))?;
    Ok((text, losses))
}

use oxa_ir::Request as IrRequest;
use oxa_ir::Response as IrResponse;
