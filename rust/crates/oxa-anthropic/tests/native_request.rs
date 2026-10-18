use oxa_anthropic::{Config, decode_request_json, encode_request};
use oxa_ir::{LossReason, ReasoningEffort};

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
