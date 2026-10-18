use oxa_ir::{
    Block, LossReason, NativeAnnotation, NativeOmission, Params, ReasoningEffort, Request,
    SystemBlock, from_json, to_json, validate_native_request,
};

#[test]
fn test_native_portable_round_trip() {
    let req = Request {
        model: "test-model".into(),
        system: vec![SystemBlock::Text {
            text: "System prompt".into(),
            native: vec![NativeAnnotation {
                source: "anthropic".into(),
                kind: "cache_control".into(),
                value: r#"{"type":"ephemeral"}"#.into(),
                omission: Some(NativeOmission {
                    field: "cache_control".into(),
                    reason: LossReason::UnmappedField,
                    detail: None,
                }),
                projection: None,
            }],
        }],
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "Hello".into(),
                native: vec![NativeAnnotation {
                    source: "anthropic".into(),
                    kind: "cache_control".into(),
                    value: r#"{"ttl":"1h","type":"ephemeral"}"#.into(),
                    omission: Some(NativeOmission {
                        field: "cache_control".into(),
                        reason: LossReason::UnmappedField,
                        detail: None,
                    }),
                    projection: None,
                }],
            }],
        }],
        tools: None,
        tool_choice: None,
        params: Some(Params {
            reasoning_effort: Some(ReasoningEffort::XHigh),
            ..Default::default()
        }),
        metadata: None,
        native: vec![
            NativeAnnotation {
                source: "anthropic".into(),
                kind: "thinking_mode".into(),
                value: r#""adaptive""#.into(),
                omission: Some(NativeOmission {
                    field: "type".into(),
                    reason: LossReason::UnmappedField,
                    detail: None,
                }),
                projection: None,
            },
            NativeAnnotation {
                source: "anthropic".into(),
                kind: "context_management".into(),
                value: r#"{"edits":[{"keep":"all","type":"clear_thinking_20251015"}]}"#.into(),
                omission: Some(NativeOmission {
                    field: "context_management".into(),
                    reason: LossReason::UnsupportedSemantic,
                    detail: None,
                }),
                projection: None,
            },
        ],
    };

    let json_str = to_json(&req).expect("to_json");
    let decoded: Request = from_json(&json_str).expect("from_json");

    assert_eq!(decoded.native.len(), 2);
    assert_eq!(decoded.native[0].value, r#""adaptive""#);
    assert_eq!(decoded.system.len(), 1);
    assert_eq!(
        decoded.params.as_ref().unwrap().reasoning_effort,
        Some(ReasoningEffort::XHigh)
    );
}

#[test]
fn test_validate_native_request() {
    let duplicate = Request {
        model: "m".into(),
        system: vec![],
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "hi".into(),
                native: vec![],
            }],
        }],
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
        native: vec![
            NativeAnnotation {
                source: "anthropic".into(),
                kind: "thinking_mode".into(),
                value: r#""adaptive""#.into(),
                omission: None,
                projection: None,
            },
            NativeAnnotation {
                source: "anthropic".into(),
                kind: "thinking_mode".into(),
                value: r#""enabled""#.into(),
                omission: None,
                projection: None,
            },
        ],
    };

    assert!(validate_native_request(&duplicate).is_err());
}

#[test]
fn test_cache_control_omission_loss_detail() {
    use oxa_ir::{CACHE_CONTROL_LOSS_DETAIL, omitted_native_request_losses};

    let req = Request {
        model: "m".into(),
        system: vec![],
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "hi".into(),
                native: vec![NativeAnnotation {
                    source: "anthropic".into(),
                    kind: "cache_control".into(),
                    value: r#"{"type":"ephemeral"}"#.into(),
                    omission: Some(NativeOmission {
                        field: "cache_control".into(),
                        reason: LossReason::UnmappedField,
                        detail: Some(CACHE_CONTROL_LOSS_DETAIL.into()),
                    }),
                    projection: None,
                }],
            }],
        }],
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
        native: vec![],
    };

    let losses = omitted_native_request_losses(&req, "chatcompletions").expect("losses");
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].field, "cache_control");
    assert_eq!(losses[0].reason, LossReason::UnmappedField);
    assert_eq!(losses[0].detail, CACHE_CONTROL_LOSS_DETAIL);

    let req_no_om = Request {
        model: "m".into(),
        system: vec![],
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "hi".into(),
                native: vec![NativeAnnotation {
                    source: "anthropic".into(),
                    kind: "cache_control".into(),
                    value: r#"{"type":"ephemeral"}"#.into(),
                    omission: None,
                    projection: None,
                }],
            }],
        }],
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
        native: vec![],
    };
    let losses_no_om =
        omitted_native_request_losses(&req_no_om, "chatcompletions").expect("losses");
    assert_eq!(losses_no_om.len(), 1);
    assert_eq!(losses_no_om[0].field, "cache_control");
    assert_eq!(losses_no_om[0].reason, LossReason::UnmappedField);
    assert_eq!(losses_no_om[0].detail, CACHE_CONTROL_LOSS_DETAIL);

    let req_empty_detail = Request {
        model: "m".into(),
        system: vec![],
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "hi".into(),
                native: vec![NativeAnnotation {
                    source: "anthropic".into(),
                    kind: "cache_control".into(),
                    value: r#"{"type":"ephemeral"}"#.into(),
                    omission: Some(NativeOmission {
                        field: "cache_control".into(),
                        reason: LossReason::UnmappedField,
                        detail: None,
                    }),
                    projection: None,
                }],
            }],
        }],
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
        native: vec![],
    };
    let losses_empty =
        omitted_native_request_losses(&req_empty_detail, "chatcompletions").expect("losses");
    assert_eq!(losses_empty.len(), 1);
    assert_eq!(losses_empty[0].detail, CACHE_CONTROL_LOSS_DETAIL);
}
