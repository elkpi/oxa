use oxa_anthropic::{Config, decode_request_json, encode_request_json};
use oxa_ir::{ResponseFormat, request_from_json, request_to_json};

#[test]
fn output_schema_source_survives_canonical_and_anthropic_json() {
    let schema = r#"{ "type" : "object", "properties" : { "n" : {"type":"string"} } }"#;
    let raw = format!(
        r#"{{"model":"claude","max_tokens":64,"messages":[{{"role":"user","content":"hi"}}],"output_config":{{"format":{{"type":"json_schema","schema":{schema}}}}}}}"#
    );
    let (req, losses) = decode_request_json(&raw, &Config::default()).unwrap();
    assert!(losses.is_empty());
    let canonical = request_to_json(&req).unwrap();
    assert!(canonical.contains(schema), "{canonical}");
    let req = request_from_json(&canonical).unwrap();
    let (encoded, losses) = encode_request_json(&req, &Config::default()).unwrap();
    assert!(losses.is_empty());
    assert!(encoded.contains(schema), "{encoded}");
}

#[test]
fn opaque_schema_allows_large_json_numbers() {
    let schema = r#"{"type":"object","maximum":123456789012345678901234567890,"default":1e400}"#;
    let raw = format!(
        r#"{{"model":"claude","max_tokens":64,"messages":[{{"role":"user","content":"hi"}}],"output_config":{{"format":{{"type":"json_schema","schema":{schema}}}}}}}"#
    );
    let (req, _) = decode_request_json(&raw, &Config::default()).unwrap();
    let canonical = request_to_json(&req).unwrap();
    let req = request_from_json(&canonical).unwrap();
    let (encoded, _) = encode_request_json(&req, &Config::default()).unwrap();
    assert!(encoded.contains(schema), "{encoded}");
}

#[test]
fn output_config_container_types_are_validated() {
    for value in ["null", "[]", "42", r#""invalid""#, "false"] {
        let raw = format!(
            r#"{{"model":"m","max_tokens":64,"messages":[{{"role":"user","content":"hi"}}],"output_config":{value}}}"#
        );
        let result = decode_request_json(&raw, &Config::default());
        if value == "null" {
            let (req, losses) = result.unwrap();
            assert!(req.params.unwrap().response_format.is_none());
            assert!(losses.is_empty());
        } else {
            assert!(result.is_err(), "{value}");
        }
    }
}

#[test]
fn malformed_output_formats_are_structural_errors() {
    for format in [
        "null",
        "{}",
        r#"{"type":"json_schema"}"#,
        r#"{"type":"json_schema","schema":null}"#,
        r#"{"type":"json_schema","schema":[]}"#,
        r#"{"type":false}"#,
    ] {
        let raw = format!(
            r#"{{"model":"claude","max_tokens":64,"messages":[{{"role":"user","content":"hi"}}],"output_config":{{"format":{format}}}}}"#
        );
        assert!(
            decode_request_json(&raw, &Config::default()).is_err(),
            "{format}"
        );
    }
}

#[test]
fn edited_schema_cannot_reuse_stale_raw() {
    let raw = r#"{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}}"#;
    let (mut req, _) = decode_request_json(raw, &Config::default()).unwrap();
    let Some(ResponseFormat::JsonSchema { schema, .. }) =
        &mut req.params.as_mut().unwrap().response_format
    else {
        panic!("missing schema")
    };
    schema.insert("title".into(), "changed".into());
    assert!(encode_request_json(&req, &Config::default()).is_err());
}
