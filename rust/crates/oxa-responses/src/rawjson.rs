use crate::{Config, Error, Request, Response, decode_request, decode_response};
use oxa_ir::{Loss, Request as IrRequest, Response as IrResponse};
use serde_json::value::RawValue;
use std::collections::BTreeMap;

fn web_search_extra(raw: &RawValue) -> Result<BTreeMap<String, serde_json::Value>, Error> {
    let fields: BTreeMap<String, &RawValue> =
        serde_json::from_str(raw.get()).map_err(|error| Error::new(error.to_string()))?;
    let mut extra = BTreeMap::new();
    for (key, value) in fields {
        if matches!(key.as_str(), "type" | "id" | "status" | "action") {
            continue;
        }
        extra.insert(
            key,
            serde_json::from_str(value.get()).map_err(|error| Error::new(error.to_string()))?,
        );
    }
    Ok(extra)
}

fn original_array<'a>(raw: &'a str, key: &str) -> Result<Option<Vec<&'a RawValue>>, Error> {
    let Some(value) =
        oxa_ir::raw_json_at(raw, &[key]).map_err(|error| Error::new(error.to_string()))?
    else {
        return Ok(None);
    };
    if !value.trim_start().starts_with('[') {
        return Ok(None);
    }
    serde_json::from_str(value)
        .map(Some)
        .map_err(|error| Error::new(error.to_string()))
}

pub fn decode_request_json(raw: &str, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let root: &RawValue =
        serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let cleaned = crate::audit::clean_json(root, "request")?;
    let mut wire: Request =
        serde_json::from_str(&cleaned).map_err(|error| Error::new(error.to_string()))?;
    if let (Some(original), crate::types::Input::Items(items)) =
        (original_array(raw, "input")?, &mut wire.input)
    {
        for (source, item) in original.iter().zip(items.iter_mut()) {
            if item.kind == "web_search_call" {
                item.extra = web_search_extra(source)?;
            }
        }
    }
    let (mut value, losses) = decode_request(&wire, config)?;
    if let Some(oxa_ir::ResponseFormat::JsonSchema { schema_raw, .. }) = value
        .params
        .as_mut()
        .and_then(|p| p.response_format.as_mut())
    {
        *schema_raw = oxa_ir::raw_json_at(raw, &["text", "format", "schema"])
            .map_err(|error| Error::new(error.to_string()))?
            .map(str::to_owned);
    }
    Ok((value, crate::audit::audit_losses(raw, "request", losses)?))
}

pub fn decode_response_json(raw: &str, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    let root: &RawValue =
        serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let cleaned = crate::audit::clean_json(root, "response")?;
    let mut wire: Response =
        serde_json::from_str(&cleaned).map_err(|error| Error::new(error.to_string()))?;
    if let Some(original) = original_array(raw, "output")? {
        for (source, item) in original.iter().zip(wire.output.iter_mut()) {
            if item.kind == "web_search_call" {
                item.extra = web_search_extra(source)?;
            }
        }
    }
    let (value, losses) = decode_response(&wire, config)?;
    Ok((value, crate::audit::audit_losses(raw, "response", losses)?))
}
