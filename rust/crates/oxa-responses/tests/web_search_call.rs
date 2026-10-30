use oxa_ir::Block;
use oxa_responses::{Config, decode_request_json};

#[test]
fn unknown_top_level_fields_are_retained() {
    let raw = r#"{"model":"m","input":[{"role":"user","content":"hi"},{"type":"web_search_call","id":"ws","status":"completed","action":{"type":"search"},"role":true}]}"#;
    let (request, losses) = decode_request_json(raw, &Config::default()).unwrap();
    match &request.messages[1].content[0] {
        Block::WebSearchCall { extra, .. } => {
            assert_eq!(extra.get("role").and_then(|v| v.as_bool()), Some(true))
        }
        other => panic!("unexpected block: {other:?}"),
    }
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].path, "input[1].role");
}
