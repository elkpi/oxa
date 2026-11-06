use oxa_ir::{Request, from_json};

#[test]
fn invalid_tool_changes_and_positions_are_rejected() {
    for group in [
        r#"{"message_index":2,"content_index":-1,"parts":[]}"#,
        r#"{"message_index":0,"content_index":3,"parts":[]}"#,
        r#"{"message_index":0,"content_index":-1,"parts":[{"type":"tool_change","change":{"op":"remove","ref":{"type":"mcp_tool_reference","name":"n"}}}]}"#,
        r#"{"message_index":0,"content_index":-1,"parts":[{"type":"tool_change","change":{"op":"remove","tool":{"name":"x","input_schema":{}}}}]}"#,
    ] {
        let input = format!(
            r#"{{"specVersion":"0.3.0","model":"m","messages":[{{"role":"user","content":[{{"type":"text","text":"go"}}]}}],"tool_change_groups":[{group}]}}"#
        );
        assert!(from_json::<Request>(&input).is_err(), "accepted {group}");
    }
}

#[test]
fn compaction_rejects_null_fields_other_than_content_and_is_request_only() {
    for field in [
        r#""tool_changes":null"#,
        r#""encrypted_content":null"#,
        r#""signature":null"#,
    ] {
        let input = format!(
            r#"{{"specVersion":"0.3.0","model":"m","messages":[{{"role":"user","content":[{{"type":"compaction",{field}}}]}}]}}"#
        );
        assert!(from_json::<Request>(&input).is_err(), "accepted {field}");
    }
    let response = r#"{"specVersion":"0.3.0","id":"r","model":"m","content":[{"type":"compaction","content":"secret"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}"#;
    assert!(from_json::<oxa_ir::Response>(response).is_err());
}
