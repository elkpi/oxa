use oxa_anthropic::{
    Config, Request as WireRequest, decode_request, decode_request_json, encode_request,
};
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
        disable_parallel: None,
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

// The typed decode path must preserve the exact native declaration source,
// including configuration the extracted fields would drop (spec/16 N-NT-2).
#[test]
fn test_typed_decode_preserves_native_configuration() {
    let raw = r#"{"model":"m","max_tokens":32,"tools":[{"type":"advisor_20260301","name":"advisor","model":"opus","max_uses":2,"vendor_config":{"flag":true}}],"messages":[{"role":"user","content":"hi"}]}"#;
    let wire: WireRequest = serde_json::from_str(raw).expect("wire decode");
    let (ir_req, losses) = decode_request(&wire, &Config::default()).expect("decode");
    assert_eq!(losses.len(), 0);
    let definition = ir_req.tools.as_ref().expect("tools")[0]
        .definition
        .as_deref()
        .expect("definition");
    assert!(definition.contains(r#""model":"opus""#), "{definition}");
    assert!(definition.contains(r#""max_uses":2"#), "{definition}");
    assert!(
        definition.contains(r#""vendor_config":{"flag":true}"#),
        "{definition}"
    );
}

// An Anthropic explicit type=custom label with a real JSON Schema is an
// ordinary user tool: the schema survives as a function tool (spec/16 N-NT-2).
#[test]
fn test_explicit_custom_label_keeps_input_schema() {
    let raw = r#"{"model":"m","max_tokens":32,"tools":[{"type":"custom","name":"lookup","description":"Look up a city.","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"messages":[{"role":"user","content":"hi"}]}"#;
    let (req, losses) = decode_request_json(raw, &Config::default()).expect("decode");
    assert_eq!(losses.len(), 0);
    let tool = &req.tools.as_ref().expect("tools")[0];
    assert_ne!(tool.tool_type, Some(ToolType::Custom));
    assert!(tool.input_schema.is_some());

    let (wire, encode_losses) = encode_request(&req, &Config::default()).expect("encode");
    assert_eq!(encode_losses.len(), 0);
    let schema = wire.tools.as_ref().expect("wire tools")[0]
        .input_schema
        .as_ref()
        .expect("schema");
    assert!(schema.get("required").is_some(), "{schema}");
}

// The encoder must enforce the native identity invariants before replaying a
// definition (spec/16 N-NT-2): no name mismatch and no non-object payload.
#[test]
fn test_encode_rejects_contradicting_native_identity() {
    let raw = r#"{"model":"m","max_tokens":32,"tools":[{"type":"advisor_20260301","name":"advisor","model":"opus"}],"messages":[{"role":"user","content":"hi"}]}"#;
    let (mut req, _) = decode_request_json(raw, &Config::default()).expect("decode");
    req.tools.as_mut().expect("tools")[0].definition =
        Some(r#"{"type":"advisor_20260301","name":"other","model":"opus"}"#.into());
    assert!(encode_request(&req, &Config::default()).is_err());

    req.tools.as_mut().expect("tools")[0].definition = Some("{}".into());
    assert!(encode_request(&req, &Config::default()).is_err());
}
