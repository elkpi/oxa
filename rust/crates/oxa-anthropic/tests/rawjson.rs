use oxa_anthropic::{Config, StreamDecoder, decode_request_json};
use oxa_ir::LossReason;

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

#[test]
fn raw_request_ignores_union_irrelevant_fields_and_reports_unmapped() {
    let raw = r#"{"model":"m","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"hi","name":42}]}]}"#;
    let (request, losses) = decode_request_json(raw, &Config::default()).unwrap();
    assert_eq!(
        request.messages[0].content,
        vec![oxa_ir::Block::Text {
            text: "hi".to_string()
        }]
    );
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].path, "messages[0].content[0].name");
    assert_eq!(losses[0].reason, LossReason::UnmappedField);
}

#[test]
fn raw_stream_unknown_event_preserves_lifecycle_and_reports_unsupported() {
    let mut decoder = StreamDecoder::new(&Config::default());
    let events = decoder
        .feed_json(r#"{"type":"new_event","index":"irrelevant","extension":null}"#)
        .unwrap();
    assert!(events.is_empty());
    let losses = decoder.losses();
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].path, "type");
    assert_eq!(losses[0].reason, LossReason::UnsupportedSemantic);
}
