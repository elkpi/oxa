use oxa_responses::{Config, decode_request_json, encode_request};
use serde_json::{Value, json};

#[test]
fn additional_tools_roundtrip_between_calls() {
    let change = json!({"type":"additional_tools","role":"developer","id":"later","tools":[{"type":"function","name":"b","parameters":{"type":"object"}}]});
    let input = json!({"model":"m","input":[
        {"role":"user","content":"go"},
        {"type":"function_call","name":"a","call_id":"a","arguments":"{}"},
        change.clone(),
        {"type":"function_call","name":"b","call_id":"b","arguments":"{}"}]});
    let (ir, losses) = decode_request_json(&input.to_string(), &Config::default()).unwrap();
    assert!(losses.is_empty(), "{losses:?}");
    assert!(ir.tools.is_none());
    assert_eq!(ir.tool_change_groups.len(), 1);
    assert_eq!(
        (
            ir.tool_change_groups[0].message_index,
            ir.tool_change_groups[0].content_index
        ),
        (1, 1)
    );
    let (out, losses) = encode_request(&ir, &Config::default()).unwrap();
    assert!(losses.is_empty(), "{losses:?}");
    let out: Value = serde_json::to_value(out).unwrap();
    assert_eq!(out["input"][2], change);
}

#[test]
fn additional_tools_requires_developer_role() {
    let input = json!({"model":"m","input":[{"type":"additional_tools","role":"user","tools":[]},{"role":"user","content":"go"}]});
    assert!(decode_request_json(&input.to_string(), &Config::default()).is_err());
}

#[test]
fn additional_tools_unknown_envelope_roundtrips() {
    let change = json!({"type":"additional_tools","role":"developer","id":"id","status":"completed","future":{"keep":true},"tools":[]});
    let input = json!({"model":"m","input":[change.clone(),{"role":"user","content":"go"}]});
    let (ir, losses) = decode_request_json(&input.to_string(), &Config::default()).unwrap();
    assert!(losses.is_empty(), "{losses:?}");
    let (out, losses) = encode_request(&ir, &Config::default()).unwrap();
    assert!(losses.is_empty(), "{losses:?}");
    let out: Value = serde_json::to_value(out).unwrap();
    assert_eq!(out["input"][0], change);
}
