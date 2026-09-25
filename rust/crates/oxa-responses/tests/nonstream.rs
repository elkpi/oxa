use oxa_ir::{
    Block, InputTokensDetails, LossReason, Message, OutputTokensDetails, Params, ReasoningEffort,
    Request as IrRequest, Response as IrResponse, Role, StopReason, Usage,
};
use oxa_responses::{
    Config, ContentValue, Input, InputItem, OutputItem, OutputPart, Request, Response, UsageWire,
    decode_request, decode_response, encode_request, encode_response,
};

#[test]
fn decodes_function_call_arguments_without_normalizing_their_json_text() {
    let wire = Request {
        model: "gpt-4o-mini".to_string(),
        input: Input::Items(vec![InputItem {
            kind: "function_call".to_string(),
            call_id: "call_1".to_string(),
            name: "weather".to_string(),
            arguments: "{\"temperature\":1e+01}".to_string(),
            ..InputItem::default()
        }]),
        ..Request::default()
    };

    let (request, losses) = decode_request(&wire, &Config::default()).expect("decode request");

    assert!(losses.is_empty());
    assert_eq!(
        request.messages,
        vec![Message {
            role: Role::Assistant,
            content: vec![Block::ToolUse {
                id: "call_1".to_string(),
                name: "weather".to_string(),
                input: "{\"temperature\":1e+01}".to_string(),
            }],
        }]
    );
}

#[test]
fn encodes_tool_results_before_normal_user_content_and_reports_reordering() {
    let request = IrRequest {
        model: "gpt-4o-mini".to_string(),
        system: Vec::new(),
        messages: vec![Message {
            role: Role::User,
            content: vec![
                Block::Text {
                    text: "Use the tool result.".to_string(),
                },
                Block::ToolResult {
                    tool_use_id: "call_1".to_string(),
                    content: vec![Block::Text {
                        text: "Sunny".to_string(),
                    }],
                    is_error: None,
                },
            ],
        }],
        tools: None,
        tool_choice: None,
        params: Some(Params {
            temperature: None,
            top_p: None,
            max_tokens: None,
            stop_sequences: None,
            reasoning_effort: None,
        }),
        metadata: None,
    };

    let (wire, losses) = encode_request(&request, &Config::default()).expect("encode request");

    assert_eq!(
        wire.input,
        Input::Items(vec![
            InputItem {
                kind: "function_call_output".to_string(),
                call_id: "call_1".to_string(),
                output: "Sunny".to_string(),
                ..InputItem::default()
            },
            InputItem {
                role: "user".to_string(),
                content: Some(ContentValue::Text("Use the tool result.".to_string())),
                ..InputItem::default()
            },
        ])
    );
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].reason, LossReason::Degraded);
    assert_eq!(losses[0].path, "messages[0].content");
}

#[test]
fn decodes_request_reasoning_items_and_effort() {
    let wire = Request {
        model: "o3-mini".to_string(),
        input: Input::Items(vec![
            InputItem {
                kind: "message".to_string(),
                role: "user".to_string(),
                content: Some(ContentValue::Text("question".to_string())),
                ..InputItem::default()
            },
            InputItem {
                kind: "reasoning".to_string(),
                id: "rs_1".to_string(),
                summary: vec![OutputPart {
                    kind: "summary_text".to_string(),
                    text: "Think first.".to_string(),
                    ..OutputPart::default()
                }],
                ..InputItem::default()
            },
            InputItem {
                kind: "message".to_string(),
                role: "assistant".to_string(),
                content: Some(ContentValue::Text("answer".to_string())),
                ..InputItem::default()
            },
        ]),
        reasoning: Some(serde_json::json!({
            "effort": "high",
            "summary": "auto"
        })),
        ..Request::default()
    };
    let (request, losses) = decode_request(&wire, &Config::default()).expect("decode request");

    assert_eq!(
        request
            .params
            .as_ref()
            .and_then(|params| params.reasoning_effort),
        Some(ReasoningEffort::High)
    );
    assert!(matches!(
        request.messages[1].content.first(),
        Some(Block::Thinking { thinking, signature: None }) if thinking == "Think first."
    ));
    assert!(
        matches!(request.messages[1].content.get(1), Some(Block::Text { text }) if text == "answer")
    );
    assert!(losses.iter().any(|loss| {
        loss.path == "reasoning.summary"
            && loss.field == "summary"
            && loss.reason == LossReason::UnmappedField
    }));
}

#[test]
fn drops_unknown_responses_reasoning_effort() {
    let wire = Request {
        model: "o3-mini".to_string(),
        input: Input::Text("question".to_string()),
        reasoning: Some(serde_json::json!({ "effort": "ultra" })),
        ..Request::default()
    };
    let (request, losses) = decode_request(&wire, &Config::default()).expect("decode request");
    assert_eq!(
        request
            .params
            .as_ref()
            .and_then(|params| params.reasoning_effort),
        None
    );
    assert!(losses.iter().any(|loss| {
        loss.path == "reasoning.effort"
            && loss.field == "effort"
            && loss.reason == LossReason::UnmappedValue
    }));
}

#[test]
fn decodes_reasoning_output_encrypted_content_and_usage_details() {
    let wire = Response {
        id: "resp_reasoning".to_string(),
        object: "response".to_string(),
        status: "completed".to_string(),
        model: "o3-mini".to_string(),
        output: vec![
            OutputItem {
                kind: "reasoning".to_string(),
                id: "rs_1".to_string(),
                summary: vec![OutputPart {
                    kind: "output_text".to_string(),
                    text: "Analyze.".to_string(),
                    ..OutputPart::default()
                }],
                encrypted_content: "opaque".to_string(),
                ..OutputItem::default()
            },
            OutputItem {
                kind: "message".to_string(),
                id: "msg_1".to_string(),
                role: "assistant".to_string(),
                content: vec![OutputPart {
                    kind: "output_text".to_string(),
                    text: "Answer.".to_string(),
                    ..OutputPart::default()
                }],
                ..OutputItem::default()
            },
        ],
        usage: Some(UsageWire {
            input_tokens: 10,
            output_tokens: 15,
            total_tokens: 25,
            input_token_details: Some(oxa_responses::InputTokenDetailsWire { cached_tokens: 4 }),
            output_token_details: Some(oxa_responses::OutputTokenDetailsWire {
                reasoning_tokens: 8,
            }),
        }),
        ..Response::default()
    };
    let (decoded, losses) = decode_response(&wire, &Config::default()).expect("decode response");

    assert!(matches!(
        decoded.content.first(),
        Some(Block::Thinking { thinking, .. }) if thinking == "Analyze."
    ));
    assert!(matches!(decoded.content.get(1), Some(Block::Text { text }) if text == "Answer."));
    assert_eq!(
        decoded.usage.input_tokens_details,
        Some(InputTokensDetails { cached_tokens: 4 })
    );
    assert_eq!(
        decoded.usage.output_tokens_details,
        Some(OutputTokensDetails {
            reasoning_tokens: 8
        })
    );
    assert!(
        losses
            .iter()
            .any(|loss| loss.path == "output[0].encrypted_content")
    );
}

#[test]
fn encodes_reasoning_summary_and_usage_details() {
    let response = IrResponse {
        id: "resp_reasoning".to_string(),
        model: "o3-mini".to_string(),
        content: vec![
            Block::Thinking {
                thinking: "Analyze.".to_string(),
                signature: Some("opaque".to_string()),
            },
            Block::Text {
                text: "Answer.".to_string(),
            },
        ],
        stop_reason: StopReason::EndTurn,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 10,
            output_tokens: 15,
            input_tokens_details: Some(InputTokensDetails { cached_tokens: 4 }),
            output_tokens_details: Some(OutputTokensDetails {
                reasoning_tokens: 8,
            }),
            ..Usage::default()
        },
    };
    let (wire, losses) = encode_response(&response, &Config::default()).expect("encode response");

    assert_eq!(wire.output[0].kind, "reasoning");
    assert_eq!(wire.output[0].summary[0].text, "Analyze.");
    assert_eq!(wire.output[1].kind, "message");
    assert_eq!(
        wire.usage
            .as_ref()
            .unwrap()
            .input_token_details
            .as_ref()
            .unwrap()
            .cached_tokens,
        4
    );
    assert_eq!(
        wire.usage
            .as_ref()
            .unwrap()
            .output_token_details
            .as_ref()
            .unwrap()
            .reasoning_tokens,
        8
    );
    assert!(
        losses
            .iter()
            .any(|loss| loss.field == "signature" && loss.reason == LossReason::UnmappedField)
    );
}

#[test]
fn encodes_refusal_with_the_required_empty_error_message() {
    let response = IrResponse {
        id: "resp_1".to_string(),
        model: "gpt-4o-mini".to_string(),
        content: vec![Block::Text {
            text: "I cannot help with that.".to_string(),
        }],
        stop_reason: StopReason::Refusal,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 5,
            output_tokens: 6,
            ..Usage::default()
        },
    };

    let (wire, losses) = encode_response(&response, &Config::default()).expect("encode response");
    let rendered = serde_json::to_value(wire).expect("serialize response");

    assert!(losses.is_empty());
    assert_eq!(rendered["error"]["code"], "refusal");
    assert_eq!(rendered["error"]["message"], "");
}
