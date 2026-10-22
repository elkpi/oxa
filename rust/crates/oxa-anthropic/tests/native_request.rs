use oxa_anthropic::{
    Config, ContentValue, decode_request_json, decode_response_json, encode_request,
};
use oxa_ir::{LossReason, ReasoningEffort, to_json};

#[test]
fn test_native_thinking_combinations() {
    let raw = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [{"role": "user", "content": "hello"}],
        "thinking": {"type": "adaptive"},
        "output_config": {"effort": "xhigh"}
    }"#;

    let config = Config::default();
    let (req, losses) = decode_request_json(raw, &config).expect("decode");
    assert!(losses.is_empty(), "expected 0 losses, got {losses:?}");
    assert_eq!(
        req.params.as_ref().unwrap().reasoning_effort,
        Some(ReasoningEffort::XHigh)
    );

    let (wire, el) = encode_request(&req, &config).expect("encode");
    assert!(el.is_empty(), "expected 0 encode losses, got {el:?}");
    let th = wire.thinking.expect("thinking");
    assert_eq!(th.kind, "adaptive");
    let oc = wire.output_config.expect("output_config");
    assert_eq!(oc.effort.as_deref(), Some("xhigh"));
}

#[test]
fn test_native_edited_effort_wins() {
    let raw = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [{"role": "user", "content": "hello"}],
        "thinking": {"type": "adaptive"},
        "output_config": {"effort": "xhigh"}
    }"#;

    let config = Config::default();
    let (mut req, _) = decode_request_json(raw, &config).expect("decode");
    req.params.as_mut().unwrap().reasoning_effort = Some(ReasoningEffort::Low);

    let (wire, _) = encode_request(&req, &config).expect("encode");
    let oc = wire.output_config.expect("output_config");
    assert_eq!(oc.effort.as_deref(), Some("low"));
}

#[test]
fn test_native_unknown_enum_drops_unit() {
    let raw = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": "hello", "cache_control": {"type": "ephemeral", "ttl": "invalid_ttl"}}
                ]
            }
        ]
    }"#;

    let config = Config::default();
    let (req, losses) = decode_request_json(raw, &config).expect("decode");
    assert!(
        losses
            .iter()
            .any(|l| l.field == "ttl" && l.reason == LossReason::UnmappedValue),
        "expected unmapped-value loss on ttl"
    );

    let (wire, _) = encode_request(&req, &config).expect("encode");
    let json_val = serde_json::to_value(&wire.messages[0]).unwrap();
    assert!(!json_val.to_string().contains("cache_control"));
}

#[test]
fn test_explicit_effort_only_encodes_to_output_config() {
    let raw = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [{"role": "user", "content": "hi"}],
        "output_config": {"effort": "low"}
    }"#;

    let config = Config::default();
    let (req, losses) = decode_request_json(raw, &config).expect("decode");
    assert!(losses.is_empty());
    assert_eq!(
        req.params.as_ref().unwrap().reasoning_effort,
        Some(ReasoningEffort::Low)
    );

    let (wire, el) = encode_request(&req, &config).expect("encode");
    assert!(el.is_empty());
    assert!(wire.thinking.is_none());
    let oc = wire.output_config.expect("output_config");
    assert_eq!(oc.effort.as_deref(), Some("low"));
}

#[test]
fn test_context_management_recursive_audit() {
    let raw = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [{"role": "user", "content": "hi"}],
        "context_management": {
            "edits": [
                {
                    "type": "clear_tool_uses_20250919",
                    "future_policy": true,
                    "trigger": {
                        "type": "input_tokens",
                        "value": 30000,
                        "future_nested": false
                    }
                }
            ]
        }
    }"#;

    let config = Config::default();
    let (req, losses) = decode_request_json(raw, &config).expect("decode");
    assert!(
        losses
            .iter()
            .any(|l| l.field == "future_policy" && l.reason == LossReason::UnmappedField)
    );
    assert!(
        losses
            .iter()
            .any(|l| l.field == "future_nested" && l.reason == LossReason::UnmappedField)
    );

    let (wire, _) = encode_request(&req, &config).expect("encode");
    let cm = wire.context_management.expect("context_management");
    let edits = cm.edits.expect("edits");
    assert_eq!(edits.len(), 1);
    let edit_obj = edits[0].as_object().unwrap();
    assert!(!edit_obj.contains_key("future_policy"));
    let trig_obj = edit_obj.get("trigger").unwrap().as_object().unwrap();
    assert!(!trig_obj.contains_key("future_nested"));
}

#[test]
fn test_response_native_isolation() {
    let raw = r#"{
        "id": "r1",
        "model": "claude-opus-5-5",
        "stop_reason": "end_turn",
        "content": [{"type": "text", "text": "hi", "cache_control": {"type": "ephemeral"}}],
        "usage": {"input_tokens": 1, "output_tokens": 1}
    }"#;

    let config = Config::default();
    let (resp, losses) = decode_response_json(raw, &config).expect("decode");
    assert!(
        losses
            .iter()
            .any(|l| l.field == "cache_control" && l.reason == LossReason::UnmappedField)
    );
    // Must serialize cleanly through IR response encoder without throwing NativeForbidden
    let json_str = to_json(&resp).expect("serialize response IR");
    assert!(json_str.contains(r#""id":"r1""#));
}

#[test]
fn test_custom_tool_result_nested_cache_control() {
    let raw = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "tools": [
            {
                "name": "f",
                "input_schema": {
                    "type": "object",
                    "properties": {"input": {"type": "string"}},
                    "required": ["input"]
                }
            }
        ],
        "messages": [
            {"role": "user", "content": "hi"},
            {
                "role": "assistant",
                "content": [{"type": "tool_use", "id": "c1", "name": "f", "input": {"input": "run"}}]
            },
            {
                "role": "user",
                "content": [
                    {
                        "type": "tool_result",
                        "tool_use_id": "c1",
                        "content": [
                            {
                                "type": "text",
                                "text": "result",
                                "cache_control": {"type": "ephemeral"}
                            }
                        ]
                    }
                ]
            }
        ]
    }"#;

    let config = Config::default();
    let (req, _) = decode_request_json(raw, &config).expect("decode");
    let ir_json = to_json(&req).expect("to_json");
    let reloaded: oxa_ir::Request = oxa_ir::from_json(&ir_json).expect("from_json");
    let (wire, _) = encode_request(&reloaded, &config).expect("encode");
    let user_msg = &wire.messages[2];
    let Some(ContentValue::Blocks(ref blocks)) = user_msg.content else {
        panic!("expected blocks");
    };
    assert_eq!(blocks[0].kind, "tool_result");
    let Some(ContentValue::Blocks(ref inner_blocks)) = blocks[0].content else {
        panic!("expected inner blocks");
    };
    assert_eq!(inner_blocks[0].kind, "text");
    assert!(inner_blocks[0].cache_control.is_some());
}

#[test]
fn test_defer_loading_tool_native_option() {
    let config = Config::default();

    // 1. defer_loading: true round-trip with 0 decode losses
    let raw = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [{"role": "user", "content": "hi"}],
        "tools": [
            {
                "name": "t1",
                "description": "tool 1",
                "input_schema": {"type": "object"},
                "defer_loading": true
            }
        ]
    }"#;
    let (req, losses) = decode_request_json(raw, &config).expect("decode");
    assert_eq!(losses.len(), 0);
    let tools = req.tools.as_ref().unwrap();
    assert_eq!(tools.len(), 1);
    assert_eq!(tools[0].native.len(), 1);
    assert_eq!(tools[0].native[0].kind, oxa_ir::NATIVE_KIND_DEFER_LOADING);
    assert_eq!(tools[0].native[0].value, "true");
    assert_eq!(
        tools[0].native[0].omission.as_ref().unwrap().reason,
        oxa_ir::LossReason::Degraded
    );

    let (wire, el) = encode_request(&req, &config).expect("encode");
    assert_eq!(el.len(), 0);
    assert_eq!(wire.tools.as_ref().unwrap().len(), 1);
    assert_eq!(wire.tools.as_ref().unwrap()[0].defer_loading, Some(true));

    // 2. defer_loading: false produces no annotation
    let raw_false = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [{"role": "user", "content": "hi"}],
        "tools": [
            {
                "name": "t1",
                "input_schema": {"type": "object"},
                "defer_loading": false
            }
        ]
    }"#;
    let (req_false, losses_false) = decode_request_json(raw_false, &config).expect("decode");
    assert_eq!(losses_false.len(), 0);
    let tools_false = req_false.tools.as_ref().unwrap();
    assert_eq!(tools_false[0].native.len(), 0);

    // 3. conflict: defer_loading: true AND cache_control
    let raw_conflict = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [{"role": "user", "content": "hi"}],
        "tools": [
            {
                "name": "t1",
                "input_schema": {"type": "object"},
                "defer_loading": true,
                "cache_control": {"type": "ephemeral"}
            }
        ]
    }"#;
    let err = decode_request_json(raw_conflict, &config).unwrap_err();
    assert!(
        err.to_string()
            .contains("cannot specify both defer_loading and cache_control")
    );

    // 4. non-boolean defer_loading
    let raw_non_bool = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 16000,
        "messages": [{"role": "user", "content": "hi"}],
        "tools": [
            {
                "name": "t1",
                "input_schema": {"type": "object"},
                "defer_loading": "true"
            }
        ]
    }"#;
    let err_non_bool = decode_request_json(raw_non_bool, &config).unwrap_err();
    assert!(
        err_non_bool
            .to_string()
            .contains("tool defer_loading must be a boolean")
    );
}
