use oxa_anthropic::{Config, decode_request_json, encode_request};
use oxa_ir::{LossReason, ToolChoice, ToolChoiceMode, ToolType};

#[test]
fn test_native_tool_declaration_fidelity() {
    let definition = r#"{ "type" : "advisor_20260301", "name":"advisor", "model":"opus", "max_uses":2, "vendor":{"n":1e+03}, "cache_control":{"type":"ephemeral"} }"#;
    let raw = format!(
        r#"{{"model":"m","max_tokens":32,"tools":[{definition}],"messages":[{{"role":"user","content":"hi"}}]}}"#
    );
    let (req, losses) = decode_request_json(&raw, &Config::default()).expect("decode");
    assert_eq!(losses.len(), 0);
    let tools = req.tools.as_ref().expect("tools");
    assert_eq!(tools.len(), 1);
    let tool = &tools[0];
    assert_eq!(tool.tool_type, Some(ToolType::Native));
    assert_eq!(tool.name, "advisor");
    assert_eq!(tool.source.as_deref(), Some("anthropic"));
    assert_eq!(tool.definition.as_deref(), Some(definition));

    let (wire, encode_losses) = encode_request(&req, &Config::default()).expect("encode");
    assert_eq!(encode_losses.len(), 0);
    let wire_tools = wire.tools.as_ref().expect("wire tools");
    let tool_json = serde_json::to_string(&wire_tools[0]).expect("serialize tool");
    assert!(tool_json.contains(r#""model":"opus""#));
    assert!(tool_json.contains(r#""max_uses":2"#));
    assert!(tool_json.contains(r#"1e+03"#));
}

#[test]
fn test_malformed_native_declarations_rejected() {
    let invalid = [
        r#"{"name":"function"}"#,
        r#"{"name":"function","input_schema":[]}"#,
        r#"{"type":null,"name":"function","input_schema":{}}"#,
        r#"{"type":7,"name":"function","input_schema":{}}"#,
        r#"{"type":"advisor_20260301","name":""}"#,
        r#"{"type":"advisor_20260301","name":7}"#,
    ];
    for t in invalid {
        let raw = format!(
            r#"{{"model":"m","max_tokens":32,"tools":[{t}],"messages":[{{"role":"user","content":"hi"}}]}}"#
        );
        assert!(
            decode_request_json(&raw, &Config::default()).is_err(),
            "expected error for {t}"
        );
    }
}

#[test]
fn test_foreign_native_tool_omitted_on_encode() {
    let raw = r#"{"model":"m","max_tokens":32,"tools":[{"type":"advisor_20260301","name":"advisor","model":"opus"}],"tool_choice":{"type":"tool","name":"advisor"},"messages":[{"role":"user","content":"hi"}]}"#;
    let (mut req, _) = decode_request_json(raw, &Config::default()).expect("decode");
    if let Some(ref mut tools) = req.tools {
        tools[0].source = Some("other-source".to_string());
    }
    req.tool_choice = Some(ToolChoice {
        mode: ToolChoiceMode::Tool,
        name: Some("advisor".to_string()),
        tool_type: None,
    });

    let (wire, losses) = encode_request(&req, &Config::default()).expect("encode");
    assert!(wire.tools.is_none());
    assert!(wire.tool_choice.is_none());
    assert_eq!(losses.len(), 2);
    assert_eq!(losses[0].field, "type");
    assert_eq!(losses[0].reason, LossReason::UnsupportedSemantic);
    assert_eq!(losses[1].field, "tool_choice");
    assert_eq!(losses[1].reason, LossReason::UnsupportedSemantic);
}
