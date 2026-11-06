use oxa_anthropic::{Config, decode_request_json, encode_request_json};
use serde_json::{Value, json};

#[test]
fn tool_change_and_compaction_roundtrip_preserves_payload() {
    let change = json!({"type":"tool_addition", "cache_control":{"type":"ephemeral"},
        "tool":{"type":"mcp_toolset_reference","server_name":"svc","future":true}});
    let compaction =
        json!({"type":"compaction","content":null,"tool_changes":[],"future":"opaque"});
    let request = json!({"model":"m","max_tokens":10,"messages":[
        {"role":"user","content":[compaction.clone(),{"type":"text","text":"go"}]},
        {"role":"system","content":[{"type":"text","text":"context"},change.clone()]},
        {"role":"assistant","content":"ok"}]});
    let (ir, losses) = decode_request_json(&request.to_string(), &Config::default()).unwrap();
    assert!(losses.is_empty(), "{losses:?}");
    let (encoded, losses) = encode_request_json(&ir, &Config::default()).unwrap();
    assert!(losses.is_empty(), "{losses:?}");
    let encoded: Value = serde_json::from_str(&encoded).unwrap();
    assert_eq!(encoded["messages"][0]["content"][0], compaction);
    assert_eq!(encoded["messages"][1]["content"][1], change);
}

#[test]
fn malformed_tool_changes_are_structural_errors() {
    for tool in [
        json!({"type":"tool_reference"}),
        json!({"type":"mcp_tool_reference","name":"n"}),
        json!({"type":"mcp_toolset_reference","server_name":"s","name":"n"}),
        json!({"type":"tool_definition","definition":{"name":"x"}}),
    ] {
        let request = json!({"model":"m","max_tokens":10,"messages":[
            {"role":"user","content":"go"},
            {"role":"system","content":[{"type":"tool_removal","tool":tool}]},
            {"role":"assistant","content":"ok"}]});
        assert!(
            decode_request_json(&request.to_string(), &Config::default()).is_err(),
            "{tool}"
        );
    }
}

#[test]
fn compaction_rejects_wrong_position_and_null_changes() {
    for messages in [
        json!([{"role":"user","content":[{"type":"text","text":"go"},{"type":"compaction","content":"summary"}]}]),
        json!([{"role":"user","content":"go"},{"role":"assistant","content":[{"type":"compaction","content":"summary"}]}]),
        json!([{"role":"user","content":[{"type":"compaction","content":"summary","tool_changes":null}]}]),
    ] {
        let request = json!({"model":"m","max_tokens":10,"messages":messages});
        assert!(
            decode_request_json(&request.to_string(), &Config::default()).is_err(),
            "{request}"
        );
    }
}

#[test]
fn server_tool_definition_roundtrips_inside_addition() {
    let change = json!({"type":"tool_addition","tool":{"type":"tool_definition","definition":{"type":"bash_20250124","name":"bash","future":true}}});
    let request = json!({"model":"m","max_tokens":10,"messages":[
        {"role":"user","content":"go"},
        {"role":"system","content":[change.clone()]},
        {"role":"assistant","content":"ok"}]});
    let (ir, losses) = decode_request_json(&request.to_string(), &Config::default()).unwrap();
    assert!(losses.is_empty(), "{losses:?}");
    let (out, losses) = encode_request_json(&ir, &Config::default()).unwrap();
    assert!(losses.is_empty(), "{losses:?}");
    let out: Value = serde_json::from_str(&out).unwrap();
    assert_eq!(out["messages"][1]["content"][0], change);
}
