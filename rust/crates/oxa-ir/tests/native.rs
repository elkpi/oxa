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
            role: String::new(),
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
                citations: None,
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
                citations: None,
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
                citations: None,
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
                citations: None,
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
                citations: None,
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

#[test]
fn test_fingerprint_losses() {
    use oxa_ir::{Loss, LossReason, fingerprint_losses};

    let losses = vec![
        Loss {
            path: "tools[0]".into(),
            field: "eager_input_streaming".into(),
            reason: LossReason::Degraded,
            detail: "sensitive detail 1".into(),
        },
        Loss {
            path: "native[1]".into(),
            field: "store".into(),
            reason: LossReason::UnsupportedSemantic,
            detail: "sensitive detail 2".into(),
        },
        Loss {
            path: "stream_options".into(),
            field: "extra".into(),
            reason: LossReason::UnmappedField,
            detail: "sensitive detail 3".into(),
        },
    ];

    let all = fingerprint_losses(&losses, 0);
    assert_eq!(all.len(), 3);
    assert_eq!(all[0].path, "tools[0]");
    assert_eq!(all[0].field, "eager_input_streaming");
    assert_eq!(all[0].reason, LossReason::Degraded);

    let truncated = fingerprint_losses(&losses, 2);
    assert_eq!(truncated.len(), 2);
    assert_eq!(truncated[1].field, "store");
}

#[test]
fn test_validate_native_request_new_kinds() {
    use oxa_ir::{
        NATIVE_KIND_EAGER_INPUT_STREAMING, NATIVE_KIND_MAX_COMPLETION_TOKENS, NATIVE_KIND_STORE,
        NATIVE_KIND_STREAM, NATIVE_KIND_STREAM_OPTIONS, NATIVE_SOURCE_ANTHROPIC,
        NATIVE_SOURCE_OPENAI, Tool, validate_native_request,
    };

    let req = Request {
        model: "m".into(),
        system: vec![],
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "hi".into(),
                citations: None,
                native: vec![],
            }],
        }],
        tools: Some(vec![Tool {
            name: "t1".into(),
            description: None,
            input_schema: Some(serde_json::json!({"type": "object"})),
            strict: None,
            tool_type: None,
            format: None,
            source: None,
            definition: None,
            native: vec![NativeAnnotation {
                source: NATIVE_SOURCE_ANTHROPIC.into(),
                kind: NATIVE_KIND_EAGER_INPUT_STREAMING.into(),
                value: "true".into(),
                omission: None,
                projection: None,
            }],
        }]),
        tool_choice: None,
        params: None,
        metadata: None,
        native: vec![
            NativeAnnotation {
                source: NATIVE_SOURCE_OPENAI.into(),
                kind: NATIVE_KIND_MAX_COMPLETION_TOKENS.into(),
                value: "true".into(),
                omission: None,
                projection: None,
            },
            NativeAnnotation {
                source: NATIVE_SOURCE_OPENAI.into(),
                kind: NATIVE_KIND_STORE.into(),
                value: "false".into(),
                omission: None,
                projection: None,
            },
            NativeAnnotation {
                source: NATIVE_SOURCE_OPENAI.into(),
                kind: NATIVE_KIND_STREAM.into(),
                value: "true".into(),
                omission: None,
                projection: None,
            },
            NativeAnnotation {
                source: NATIVE_SOURCE_OPENAI.into(),
                kind: NATIVE_KIND_STREAM_OPTIONS.into(),
                value: r#"{"include_usage":true}"#.into(),
                omission: None,
                projection: None,
            },
        ],
    };
    assert!(validate_native_request(&req).is_ok());

    let mut invalid = req.clone();
    invalid.tools.as_mut().unwrap()[0].native[0].value = r#""not_bool""#.into();
    assert!(validate_native_request(&invalid).is_err());

    let mut invalid_null = req.clone();
    invalid_null.tools.as_mut().unwrap()[0].native[0].value = "null".into();
    assert!(validate_native_request(&invalid_null).is_err());

    let mut invalid_so = req.clone();
    invalid_so.native[3].value = r#"{"include_usage":"yes"}"#.into();
    assert!(validate_native_request(&invalid_so).is_err());
}
