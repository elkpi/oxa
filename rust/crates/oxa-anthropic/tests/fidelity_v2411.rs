use oxa_anthropic::{
    Config, decode_request_json, decode_response_json, encode_request, encode_response,
};
use oxa_ir::Block;

#[test]
fn test_redacted_thinking_roundtrip() {
    let raw = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 4096,
        "messages": [
            {"role": "user", "content": "Prior prompt."},
            {
                "role": "assistant",
                "content": [
                    {"type": "redacted_thinking", "data": "OPAQUE_ENCRYPTED_DATA"},
                    {"type": "text", "text": "Follow up."}
                ]
            },
            {"role": "user", "content": "Continue."}
        ]
    }"#;

    let config = Config::default();
    let (req, losses) = decode_request_json(raw, &config).expect("decode");
    assert!(losses.is_empty(), "expected 0 losses, got: {losses:?}");

    let asst = &req.messages[1].content;
    assert_eq!(asst.len(), 2);
    match &asst[0] {
        Block::RedactedThinking { data, .. } => assert_eq!(data, "OPAQUE_ENCRYPTED_DATA"),
        other => panic!("expected Block::RedactedThinking, got {other:?}"),
    }

    let (wire, enc_losses) = encode_request(&req, &config).expect("encode");
    assert!(
        enc_losses.is_empty(),
        "expected 0 losses, got: {enc_losses:?}"
    );
    let wire_json = serde_json::to_string(&wire).unwrap();
    assert!(wire_json.contains(r#""type":"redacted_thinking""#));
    assert!(wire_json.contains(r#""data":"OPAQUE_ENCRYPTED_DATA""#));
}

#[test]
fn test_between_tools_roundtrip_and_conflict() {
    let raw = r#"{
        "model": "claude-sonnet-5-5",
        "max_tokens": 4096,
        "thinking": {"type": "between_tools"},
        "messages": [{"role": "user", "content": "Hello"}]
    }"#;

    let config = Config::default();
    let (req, losses) = decode_request_json(raw, &config).expect("decode");
    assert!(losses.is_empty(), "expected 0 losses, got: {losses:?}");
    let mode_ann = req.native.iter().find(|a| a.kind == "thinking_mode");
    assert_eq!(
        mode_ann.map(|a| a.value.as_str()),
        Some("\"between_tools\"")
    );

    let (wire, enc_losses) = encode_request(&req, &config).expect("encode");
    assert!(enc_losses.is_empty());
    assert_eq!(
        wire.thinking.as_ref().map(|t| t.kind.as_str()),
        Some("between_tools")
    );

    // Conflict with budget
    let conflict = r#"{
        "model": "claude-sonnet-5-5",
        "max_tokens": 4096,
        "thinking": {"type": "between_tools", "budget_tokens": 2048},
        "messages": [{"role": "user", "content": "Hello"}]
    }"#;
    assert!(decode_request_json(conflict, &config).is_err());
}

#[test]
fn test_top_k_roundtrip() {
    let config = Config::default();
    for k in [0, 40, 100] {
        let raw = format!(
            r#"{{
                "model": "claude-opus-4-6",
                "max_tokens": 1024,
                "top_k": {k},
                "messages": [{{"role": "user", "content": "Hello"}}]
            }}"#
        );
        let (req, losses) = decode_request_json(&raw, &config).expect("decode");
        assert!(losses.is_empty());
        let ann = req.native.iter().find(|a| a.kind == "top_k");
        assert_eq!(
            ann.map(|a| a.value.as_str()),
            Some(k.to_string()).as_deref()
        );

        let (wire, enc_losses) = encode_request(&req, &config).expect("encode");
        assert!(enc_losses.is_empty());
        assert_eq!(wire.top_k, Some(k));
    }
}

#[test]
fn test_disable_parallel_tool_use_roundtrip_and_none() {
    let config = Config::default();
    for flag in [true, false] {
        let raw = format!(
            r#"{{
                "model": "claude-sonnet-4-5",
                "max_tokens": 1024,
                "messages": [{{"role": "user", "content": "Hello"}}],
                "tools": [{{"name": "t1", "input_schema": {{"type": "object"}}}}],
                "tool_choice": {{"type": "auto", "disable_parallel_tool_use": {flag}}}
            }}"#
        );
        let (req, losses) = decode_request_json(&raw, &config).expect("decode");
        assert!(losses.is_empty());
        let ann = req
            .native
            .iter()
            .find(|a| a.kind == "disable_parallel_tool_use");
        assert_eq!(
            ann.map(|a| a.value.as_str()),
            Some(if flag { "true" } else { "false" })
        );

        let (wire, enc_losses) = encode_request(&req, &config).expect("encode");
        assert!(enc_losses.is_empty());
        assert_eq!(
            wire.tool_choice.and_then(|tc| tc.disable_parallel_tool_use),
            Some(flag)
        );
    }

    let none_raw = r#"{
        "model": "claude-sonnet-4-5",
        "max_tokens": 1024,
        "messages": [{"role": "user", "content": "Hello"}],
        "tools": [{"name": "t1", "input_schema": {"type": "object"}}],
        "tool_choice": {"type": "none", "disable_parallel_tool_use": true}
    }"#;
    assert!(decode_request_json(none_raw, &config).is_err());
}

#[test]
fn test_text_citations_roundtrip() {
    let raw = r#"{
        "id": "msg_cite",
        "type": "message",
        "role": "assistant",
        "model": "claude-sonnet-4-6",
        "content": [
            {
                "type": "text",
                "text": "Summary.",
                "citations": [
                    {
                        "type": "char_location",
                        "cited_text": "quote",
                        "document_index": 0,
                        "document_title": "Doc",
                        "start_char_index": 0,
                        "end_char_index": 5,
                        "file_id": "file_1"
                    }
                ]
            }
        ],
        "stop_reason": "end_turn",
        "usage": {"input_tokens": 10, "output_tokens": 10}
    }"#;

    let config = Config::default();
    let (resp, losses) = decode_response_json(raw, &config).expect("decode");
    assert!(losses.is_empty());
    match &resp.content[0] {
        Block::Text { citations, .. } => {
            let cites = citations.as_ref().expect("citations present");
            assert_eq!(cites.len(), 1);
            assert_eq!(cites[0].source, "anthropic");
        }
        other => panic!("expected Block::Text, got {other:?}"),
    }

    let (wire, enc_losses) = encode_response(&resp, &config).expect("encode");
    assert!(enc_losses.is_empty());
    let wire_json = serde_json::to_string(&wire).unwrap();
    assert!(wire_json.contains(r#""char_location""#));
    assert!(wire_json.contains(r#""file_1""#));
}

#[test]
fn test_redacted_thinking_data_validation_and_debug_mask() {
    let config = Config::default();
    let raw_empty = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 1024,
        "messages": [
            {"role": "user", "content": "Hi"},
            {"role": "assistant", "content": [{"type": "redacted_thinking", "data": ""}, {"type": "text", "text": "Ok"}]}
        ]
    }"#;
    let (req, _) = decode_request_json(raw_empty, &config).expect("decode");
    let (wire, _) = encode_request(&req, &config).expect("encode");
    let wire_json = serde_json::to_string(&wire).unwrap();
    assert!(wire_json.contains(r#""data":"""#));

    // debug masking
    let blk = Block::RedactedThinking {
        data: "SECRET_PAYLOAD_12345".into(),
        native: Vec::new(),
    };
    let debug_str = format!("{blk:?}");
    assert!(!debug_str.contains("SECRET_PAYLOAD_12345"));
    assert!(debug_str.contains("[REDACTED len=20]"));

    let raw_null = r#"{
        "model": "claude-opus-5-5",
        "max_tokens": 1024,
        "messages": [
            {"role": "user", "content": "Hi"},
            {"role": "assistant", "content": [{"type": "redacted_thinking", "data": null}, {"type": "text", "text": "Ok"}]}
        ]
    }"#;
    assert!(decode_request_json(raw_null, &config).is_err());
}

#[test]
fn test_disable_parallel_tool_use_null_rejected_and_conflict() {
    let config = Config::default();
    let raw_null = r#"{
        "model": "claude-sonnet-4-5",
        "max_tokens": 1024,
        "messages": [{"role": "user", "content": "Hi"}],
        "tools": [{"name": "t1", "input_schema": {"type": "object"}}],
        "tool_choice": {"type": "auto", "disable_parallel_tool_use": null}
    }"#;
    assert!(decode_request_json(raw_null, &config).is_err());

    let req_conflict = oxa_ir::Request {
        model: "claude-sonnet-4-5".into(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "Hi".into(),
                citations: None,
                native: Vec::new(),
            }],
        }],
        tools: None,
        tool_choice: Some(oxa_ir::ToolChoice {
            mode: oxa_ir::ToolChoiceMode::None,
            name: None,
            tool_type: None,
            disable_parallel: None,
        }),
        params: None,
        metadata: None,
        native: vec![oxa_ir::NativeAnnotation {
            source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
            kind: oxa_ir::NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE.into(),
            value: "true".into(),
            omission: None,
            projection: None,
        }],
    };
    assert!(encode_request(&req_conflict, &config).is_err());
}

#[test]
fn test_between_tools_encode_conflict() {
    let config = Config::default();
    let req_conflict = oxa_ir::Request {
        model: "claude-sonnet-5-5".into(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "Hi".into(),
                citations: None,
                native: Vec::new(),
            }],
        }],
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
        native: vec![
            oxa_ir::NativeAnnotation {
                source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                kind: oxa_ir::NATIVE_KIND_THINKING_MODE.into(),
                value: r#""between_tools""#.into(),
                omission: None,
                projection: None,
            },
            oxa_ir::NativeAnnotation {
                source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                kind: oxa_ir::NATIVE_KIND_THINKING_BUDGET.into(),
                value: "2048".into(),
                omission: None,
                projection: None,
            },
        ],
    };
    assert!(encode_request(&req_conflict, &config).is_err());
}

#[test]
fn test_omitted_native_losses_default_fallback() {
    let req = oxa_ir::Request {
        model: "claude-opus-4-6".into(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "Hi".into(),
                citations: None,
                native: Vec::new(),
            }],
        }],
        tools: None,
        tool_choice: Some(oxa_ir::ToolChoice {
            mode: oxa_ir::ToolChoiceMode::Auto,
            name: None,
            tool_type: None,
            disable_parallel: None,
        }),
        params: None,
        metadata: None,
        native: vec![
            oxa_ir::NativeAnnotation {
                source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                kind: oxa_ir::NATIVE_KIND_TOP_K.into(),
                value: "40".into(),
                omission: None,
                projection: None,
            },
            oxa_ir::NativeAnnotation {
                source: oxa_ir::NATIVE_SOURCE_ANTHROPIC.into(),
                kind: oxa_ir::NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE.into(),
                value: "true".into(),
                omission: None,
                projection: None,
            },
        ],
    };
    let losses = oxa_ir::omitted_native_request_losses(&req, "chatcompletions").expect("losses");
    assert!(
        losses
            .iter()
            .any(|l| l.field == "top_k" && l.reason == oxa_ir::LossReason::UnmappedField)
    );
    assert!(
        losses.iter().any(|l| l.field == "disable_parallel_tool_use"
            && l.reason == oxa_ir::LossReason::UnmappedField)
    );
}
