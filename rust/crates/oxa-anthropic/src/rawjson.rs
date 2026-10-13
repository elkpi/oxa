use crate::types::*;
use crate::{Config, Error, Response, decode_request, decode_response};
use oxa_ir::{Loss, Request as IrRequest, Response as IrResponse};
use serde::Deserialize;
use serde_json::{Value, value::RawValue};

#[derive(Default, Deserialize)]
#[serde(default)]
struct RawRequest<'a> {
    model: String,
    system: Option<SystemValue>,
    #[serde(borrow)]
    messages: Vec<RawMessage<'a>>,
    max_tokens: i64,
    temperature: Option<f64>,
    top_p: Option<f64>,
    stop_sequences: Option<Vec<String>>,
    thinking: Option<ThinkingWire>,
    metadata: Option<Value>,
    tools: Option<Vec<ToolWire>>,
    tool_choice: Option<ToolChoiceWire>,
}

#[derive(Deserialize)]
struct RawMessage<'a> {
    role: String,
    #[serde(borrow)]
    content: Option<&'a RawValue>,
}

pub fn decode_request_json(raw: &str, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let source: RawRequest<'_> =
        serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let messages = source
        .messages
        .into_iter()
        .map(|message| {
            let content = message
                .content
                .map(|value| serde_json::from_str(value.get()))
                .transpose()
                .map_err(|error| Error::new(error.to_string()))?;
            Ok(Message {
                role: message.role,
                content,
            })
        })
        .collect::<Result<Vec<_>, Error>>()?;
    let wire = Request {
        model: source.model,
        system: source.system,
        messages,
        max_tokens: source.max_tokens,
        temperature: source.temperature,
        top_p: source.top_p,
        stop_sequences: source.stop_sequences,
        thinking: source.thinking,
        metadata: source.metadata,
        tools: source.tools,
        tool_choice: source.tool_choice,
    };
    let (value, losses) = decode_request(&wire, config)?;
    Ok((value, crate::audit::audit_losses(raw, "request", losses)?))
}

pub fn decode_response_json(raw: &str, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    let wire: Response =
        serde_json::from_str(raw).map_err(|error| Error::new(error.to_string()))?;
    let (value, losses) = decode_response(&wire, config)?;
    Ok((value, crate::audit::audit_losses(raw, "response", losses)?))
}
