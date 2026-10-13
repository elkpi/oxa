use crate::{Config, Error, Request, Response, decode_request, decode_response};
use oxa_ir::{Loss, Request as IrRequest, Response as IrResponse};

pub fn decode_request_json(raw: &str, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let wire: Request = serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let (value, losses) = decode_request(&wire, config)?;
    Ok((value, crate::audit::audit_losses(raw, "request", losses)?))
}

pub fn decode_response_json(raw: &str, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    let wire: Response =
        serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let (value, losses) = decode_response(&wire, config)?;
    Ok((value, crate::audit::audit_losses(raw, "response", losses)?))
}
