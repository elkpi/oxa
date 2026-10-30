use std::collections::BTreeMap;

use serde_json::{Map, Value, value::RawValue};

use crate::{Error, Request, ResponseFormat, from_json, to_json};

/// 提取对象成员并保留其原始 JSON 文本。
pub fn raw_json_at<'a>(text: &'a str, path: &[&str]) -> Result<Option<&'a str>, Error> {
    let mut current = text;
    for key in path {
        let object: BTreeMap<String, &RawValue> = serde_json::from_str(current)?;
        let Some(value) = object.get(*key) else {
            return Ok(None);
        };
        current = value.get();
    }
    Ok(Some(current))
}

/// 仅当原始文本与 IR schema 一致时替换序列化结果。
pub fn insert_schema_raw(
    text: String,
    path: &[&str],
    schema: &Map<String, Value>,
    raw: Option<&str>,
) -> Result<String, Error> {
    let Some(raw) = raw else { return Ok(text) };
    let source: Value = serde_json::from_str(raw)?;
    if source.as_object() != Some(schema) {
        return Err(Error::Custom(
            "response format schema_raw does not match schema".into(),
        ));
    }
    let existing = raw_json_at(&text, path)?
        .ok_or_else(|| Error::Custom("response format schema not found in encoded JSON".into()))?;
    let start = existing.as_ptr() as usize - text.as_ptr() as usize;
    let end = start + existing.len();
    let mut output = String::with_capacity(text.len() - existing.len() + raw.len());
    output.push_str(&text[..start]);
    output.push_str(raw);
    output.push_str(&text[end..]);
    Ok(output)
}

/// 解析 IR 请求并保留响应 schema 的原始文本。
pub fn request_from_json(text: &str) -> Result<Request, Error> {
    let mut req: Request = from_json(text)?;
    if let Some(ResponseFormat::JsonSchema { schema_raw, .. }) =
        req.params.as_mut().and_then(|p| p.response_format.as_mut())
    {
        *schema_raw =
            raw_json_at(text, &["params", "response_format", "schema"])?.map(str::to_owned);
    }
    Ok(req)
}

/// 序列化 IR 请求并保留响应 schema 的原始文本。
pub fn request_to_json(req: &Request) -> Result<String, Error> {
    let text = to_json(req)?;
    if let Some(ResponseFormat::JsonSchema {
        schema, schema_raw, ..
    }) = req.params.as_ref().and_then(|p| p.response_format.as_ref())
    {
        return insert_schema_raw(
            text,
            &["params", "response_format", "schema"],
            schema,
            schema_raw.as_deref(),
        );
    }
    Ok(text)
}
