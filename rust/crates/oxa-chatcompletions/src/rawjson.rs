use crate::{Config, Error, Request, Response, decode_request, decode_response};
use oxa_ir::{Loss, Request as IrRequest, Response as IrResponse};
use serde_json::value::RawValue;

pub fn decode_request_json(raw: &str, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let root: &RawValue =
        serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let cleaned = crate::audit::clean_json(root, "request")?;
    let wire: Request =
        serde_json::from_str(&cleaned).map_err(|error| Error::new(error.to_string()))?;
    let (mut value, losses) = decode_request(&wire, config)?;
    if let Some(oxa_ir::ResponseFormat::JsonSchema { schema_raw, .. }) = value
        .params
        .as_mut()
        .and_then(|p| p.response_format.as_mut())
    {
        *schema_raw = oxa_ir::raw_json_at(raw, &["response_format", "json_schema", "schema"])
            .map_err(|error| Error::new(error.to_string()))?
            .map(str::to_owned);
    }
    Ok((value, crate::audit::audit_losses(raw, "request", losses)?))
}

pub fn decode_response_json(raw: &str, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    let root: &RawValue =
        serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let cleaned = crate::audit::clean_json(root, "response")?;
    let wire: Response =
        serde_json::from_str(&cleaned).map_err(|error| Error::new(error.to_string()))?;
    let (value, losses) = decode_response(&wire, config)?;
    Ok((value, crate::audit::audit_losses(raw, "response", losses)?))
}
