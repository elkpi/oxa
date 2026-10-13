use oxa_anthropic::{Config, decode_request_json};

#[test]
fn raw_request_keeps_tool_input_text_and_owner() {
    let input = r#"{ "z": 9007199254740993, "a": 1e+01, "text": "A" }"#;
    let raw = format!(
        r#"{{"model":"m","max_tokens":32,"messages":[{{"role":"user","content":"invoke"}},{{"role":"assistant","content":[{{"type":"tool_use","input":{input},"id":"t1","name":"f"}}]}},{{"role":"user","content":[{{"type":"tool_result","tool_use_id":"t1","content":"ok"}}]}}]}}"#
    );
    let (request, losses) = decode_request_json(&raw, &Config::default()).unwrap();
    assert!(losses.is_empty());
    match &request.messages[1].content[0] {
        oxa_ir::Block::ToolUse { input: actual, .. } => assert_eq!(actual, input),
        block => panic!("expected tool use, got {block:?}"),
    }
}
