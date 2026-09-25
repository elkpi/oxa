//! Nonstream behavior beyond the golden vectors: structural error
//! boundaries, loss details, and the modelmap injection point.

use oxa_chatcompletions::{
    Config, Request, Response, decode_request, decode_response, encode_request, encode_response,
};
use oxa_ir::{
    Block, InputTokensDetails, LossReason, OutputTokensDetails, Params, ReasoningEffort, Role,
    StopReason, Usage,
};
use oxa_modelmap::Table;
use serde_json::Value;

fn wire_request(json: Value) -> Request {
    serde_json::from_value(json).expect("test wire request must deserialize")
}

fn wire_response(json: Value) -> Response {
    serde_json::from_value(json).expect("test wire response must deserialize")
}

fn minimal_request() -> Value {
    serde_json::json!({
        "model": "gpt-4o-mini",
        "messages": [{ "role": "user", "content": "Hello" }]
    })
}

fn loss_with<'a>(losses: &'a [oxa_ir::Loss], path: &str, field: &str) -> &'a oxa_ir::Loss {
    losses
        .iter()
        .find(|loss| loss.path == path && loss.field == field)
        .unwrap_or_else(|| panic!("loss {path}.{field} not found in {losses:?}"))
}

#[test]
fn unknown_role_is_a_structural_error() {
    let wire = wire_request(serde_json::json!({
        "model": "m",
        "messages": [{ "role": "chief", "content": "Hello" }]
    }));
    let err = decode_request(&wire, &Config::default()).expect_err("unknown role");
    assert!(err.to_string().contains("unknown role \"chief\""), "{err}");
}

#[test]
fn request_without_conversation_messages_is_a_structural_error() {
    let wire = wire_request(serde_json::json!({
        "model": "m",
        "messages": [{ "role": "system", "content": "Be concise." }]
    }));
    let err = decode_request(&wire, &Config::default()).expect_err("system only");
    assert!(
        err.to_string()
            .contains("request carries no conversation messages"),
        "{err}"
    );
}

#[test]
fn response_without_choices_is_a_structural_error() {
    let wire = wire_response(serde_json::json!({
        "id": "r", "object": "chat.completion", "created": 0,
        "model": "m", "choices": []
    }));
    let err = decode_response(&wire, &Config::default()).expect_err("no choices");
    assert!(err.to_string().contains("carries no choices"), "{err}");
}

#[test]
fn missing_finish_reason_is_a_structural_error() {
    let wire = wire_response(serde_json::json!({
        "id": "r", "object": "chat.completion", "created": 0,
        "model": "m",
        "choices": [{ "index": 0, "message": { "role": "assistant", "content": "" }, "finish_reason": "" }]
    }));
    let err = decode_response(&wire, &Config::default()).expect_err("missing finish reason");
    assert!(
        err.to_string().contains("finish_reason is missing"),
        "{err}"
    );
}

#[test]
fn decodes_reasoning_content_and_effort() {
    let wire = wire_request(serde_json::json!({
        "model": "o3-mini",
        "reasoning_effort": "high",
        "messages": [
            { "role": "user", "content": "question" },
            {
                "role": "assistant",
                "reasoning_content": "Plan carefully.",
                "content": "answer"
            }
        ]
    }));
    let (request, losses) = decode_request(&wire, &Config::default()).expect("decode");

    assert_eq!(
        request
            .params
            .as_ref()
            .and_then(|params| params.reasoning_effort),
        Some(ReasoningEffort::High)
    );
    assert!(matches!(
        request.messages[1].content.first(),
        Some(Block::Thinking { thinking, signature: None }) if thinking == "Plan carefully."
    ));
    assert!(losses.is_empty());
}

#[test]
fn drops_unknown_reasoning_effort_with_one_value_loss() {
    let wire = wire_request(serde_json::json!({
        "model": "o3-mini",
        "reasoning_effort": "ultra",
        "messages": [{ "role": "user", "content": "question" }]
    }));
    let (request, losses) = decode_request(&wire, &Config::default()).expect("decode");

    assert_eq!(
        request
            .params
            .as_ref()
            .and_then(|params| params.reasoning_effort),
        None
    );
    let loss = loss_with(&losses, "reasoning_effort", "reasoning_effort");
    assert_eq!(loss.reason, LossReason::UnmappedValue);
}

#[test]
fn encodes_reasoning_content_and_reports_signature_losses() {
    let request = oxa_ir::Request {
        model: "o3-mini".to_string(),
        system: Vec::new(),
        messages: vec![
            oxa_ir::Message {
                role: Role::User,
                content: vec![Block::Text {
                    text: "question".to_string(),
                }],
            },
            oxa_ir::Message {
                role: Role::Assistant,
                content: vec![
                    Block::Thinking {
                        thinking: "Plan.".to_string(),
                        signature: Some("opaque".to_string()),
                    },
                    Block::Text {
                        text: "answer".to_string(),
                    },
                ],
            },
        ],
        tools: None,
        tool_choice: None,
        params: Some(Params {
            temperature: None,
            top_p: None,
            max_tokens: None,
            stop_sequences: None,
            reasoning_effort: Some(ReasoningEffort::High),
        }),
        metadata: None,
    };
    let (wire, request_losses) =
        encode_request(&request, &Config::default()).expect("encode request");
    assert_eq!(wire.reasoning_effort.as_deref(), Some("high"));
    assert_eq!(wire.messages[1].reasoning_content.as_deref(), Some("Plan."));
    assert_eq!(
        loss_with(
            &request_losses,
            "messages[1].content[0].signature",
            "signature"
        )
        .reason,
        LossReason::UnmappedField
    );

    let response = oxa_ir::Response {
        id: "r".to_string(),
        model: "o3-mini".to_string(),
        content: vec![Block::Thinking {
            thinking: "Plan.".to_string(),
            signature: Some("opaque".to_string()),
        }],
        stop_reason: StopReason::EndTurn,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 1,
            output_tokens: 2,
            ..Usage::default()
        },
    };
    let (wire, response_losses) =
        encode_response(&response, &Config::default()).expect("encode response");
    assert_eq!(
        wire.choices[0].message.reasoning_content.as_deref(),
        Some("Plan.")
    );
    assert_eq!(
        loss_with(&response_losses, "content[0].signature", "signature").reason,
        LossReason::UnmappedField
    );
}

#[test]
fn maps_optional_usage_details_in_both_directions() {
    let wire = wire_response(serde_json::json!({
        "id": "r",
        "object": "chat.completion",
        "created": 0,
        "model": "o3-mini",
        "choices": [{
            "index": 0,
            "message": { "role": "assistant", "content": "answer" },
            "finish_reason": "stop"
        }],
        "usage": {
            "prompt_tokens": 5,
            "completion_tokens": 7,
            "total_tokens": 12,
            "prompt_tokens_details": { "cached_tokens": 0 },
            "completion_tokens_details": { "reasoning_tokens": 3 }
        }
    }));
    let (decoded, losses) = decode_response(&wire, &Config::default()).expect("decode usage");
    assert_eq!(
        decoded.usage.input_tokens_details,
        Some(InputTokensDetails { cached_tokens: 0 })
    );
    assert_eq!(
        decoded.usage.output_tokens_details,
        Some(OutputTokensDetails {
            reasoning_tokens: 3
        })
    );
    assert!(losses.is_empty());

    let mut response = decoded;
    response.usage.cache_read_input_tokens = Some(0);
    response.usage.cache_creation_input_tokens = Some(4);
    let (encoded, _) = encode_response(&response, &Config::default()).expect("encode usage");
    let usage = encoded.usage.expect("usage present");
    assert_eq!(usage.prompt_tokens_details.unwrap().cached_tokens, 0);
    assert_eq!(usage.completion_tokens_details.unwrap().reasoning_tokens, 3);
}

#[test]
fn encodes_multiple_thinking_blocks_in_encounter_order() {
    let response = oxa_ir::Response {
        id: "r".to_string(),
        model: "o3-mini".to_string(),
        content: vec![
            Block::Thinking {
                thinking: "First.".to_string(),
                signature: None,
            },
            Block::Thinking {
                thinking: "Second.".to_string(),
                signature: None,
            },
        ],
        stop_reason: StopReason::EndTurn,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 1,
            output_tokens: 2,
            ..Usage::default()
        },
    };
    let (wire, losses) = encode_response(&response, &Config::default()).expect("encode");

    assert_eq!(
        wire.choices[0].message.reasoning_content.as_deref(),
        Some("First.Second.")
    );
    assert!(losses.is_empty());
}

#[test]
fn rejects_negative_usage_details_on_decode_and_encode() {
    let wire = wire_response(serde_json::json!({
        "id": "r",
        "object": "chat.completion",
        "created": 0,
        "model": "o3-mini",
        "choices": [{
            "index": 0,
            "message": { "role": "assistant", "content": "answer" },
            "finish_reason": "stop"
        }],
        "usage": {
            "prompt_tokens": 1,
            "completion_tokens": 2,
            "total_tokens": 3,
            "prompt_tokens_details": { "cached_tokens": -1 }
        }
    }));
    assert!(decode_response(&wire, &Config::default()).is_err());

    let response = oxa_ir::Response {
        id: "r".to_string(),
        model: "o3-mini".to_string(),
        content: Vec::new(),
        stop_reason: StopReason::EndTurn,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 1,
            output_tokens: 2,
            output_tokens_details: Some(OutputTokensDetails {
                reasoning_tokens: -1,
            }),
            ..Usage::default()
        },
    };
    assert!(encode_response(&response, &Config::default()).is_err());
}

#[test]
fn rejects_negative_nonstream_usage_details() {
    for detail in [
        serde_json::json!({ "prompt_tokens_details": { "cached_tokens": -1 } }),
        serde_json::json!({ "completion_tokens_details": { "reasoning_tokens": -1 } }),
    ] {
        let mut wire = serde_json::json!({
            "id": "r",
            "object": "chat.completion",
            "created": 0,
            "model": "o3-mini",
            "choices": [{
                "index": 0,
                "message": { "role": "assistant", "content": "answer" },
                "finish_reason": "stop"
            }],
            "usage": {
                "prompt_tokens": 1,
                "completion_tokens": 2,
                "total_tokens": 3
            }
        });
        for (key, value) in detail.as_object().unwrap() {
            wire["usage"][key] = value.clone();
        }
        let err = decode_response(&wire_response(wire), &Config::default())
            .expect_err("negative usage detail");
        assert!(err.to_string().contains("non-negative"), "{err}");
    }

    let invalid = [
        Usage {
            input_tokens: 1,
            output_tokens: 2,
            input_tokens_details: Some(InputTokensDetails { cached_tokens: -1 }),
            ..Usage::default()
        },
        Usage {
            input_tokens: 1,
            output_tokens: 2,
            output_tokens_details: Some(OutputTokensDetails {
                reasoning_tokens: -1,
            }),
            ..Usage::default()
        },
    ];
    for usage in invalid {
        let response = oxa_ir::Response {
            id: "r".to_string(),
            model: "o3-mini".to_string(),
            content: Vec::new(),
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage,
        };
        assert!(encode_response(&response, &Config::default()).is_err());
    }
}

#[test]
fn unknown_finish_reason_maps_to_other_with_a_loss() {
    let wire = wire_response(serde_json::json!({
        "id": "r", "object": "chat.completion", "created": 0,
        "model": "m",
        "choices": [{ "index": 0, "message": { "role": "assistant", "content": "hi" }, "finish_reason": "function_call" }]
    }));
    let (resp, losses) = decode_response(&wire, &Config::default()).expect("decode");
    assert_eq!(resp.stop_reason, StopReason::Other);
    let loss = loss_with(&losses, "choices[0].finish_reason", "finish_reason");
    assert_eq!(loss.reason, LossReason::UnmappedValue);
    assert!(
        loss.detail
            .contains("finish_reason \"function_call\" has no IR equivalent"),
        "{loss:?}"
    );
}

#[test]
fn unsupported_tool_choice_string_is_a_loss() {
    let mut wire = minimal_request();
    wire["tool_choice"] = serde_json::json!("methinks");
    let (_, losses) = decode_request(&wire_request(wire), &Config::default()).expect("decode");
    let loss = loss_with(&losses, "tool_choice", "tool_choice");
    assert_eq!(loss.reason, LossReason::UnsupportedSemantic);
    assert!(
        loss.detail
            .contains("Chat Completions tool_choice \"methinks\" has no IR equivalent"),
        "{loss:?}"
    );
}

#[test]
fn non_function_tool_type_is_a_loss() {
    let mut wire = minimal_request();
    wire["tools"] = serde_json::json!([
        { "type": "web_search", "function": { "name": "search" } }
    ]);
    let (req, losses) = decode_request(&wire_request(wire), &Config::default()).expect("decode");
    assert!(req.tools.is_none(), "unsupported tool is dropped");
    let loss = loss_with(&losses, "tools[0]", "type");
    assert_eq!(loss.reason, LossReason::UnsupportedSemantic);
}

#[test]
fn non_function_tool_call_type_is_a_loss() {
    let mut wire = minimal_request();
    wire["messages"] = serde_json::json!([
        { "role": "user", "content": "go" },
        {
            "role": "assistant",
            "content": null,
            "tool_calls": [
                { "id": "call_1", "type": "custom", "function": { "name": "f", "arguments": "{}" } }
            ]
        }
    ]);
    let (req, losses) = decode_request(&wire_request(wire), &Config::default()).expect("decode");
    let assistant = &req.messages[1];
    assert!(
        assistant
            .content
            .iter()
            .all(|block| matches!(block, oxa_ir::Block::Text { .. })),
        "custom tool call is dropped: {:?}",
        assistant.content
    );
    let loss = loss_with(&losses, "messages[1].tool_calls[0]", "type");
    assert_eq!(loss.reason, LossReason::UnsupportedSemantic);
}

#[test]
fn encoding_an_ir_stop_other_is_a_structural_error() {
    let resp = oxa_ir::Response {
        id: "r".to_string(),
        model: "m".to_string(),
        content: Vec::new(),
        stop_reason: StopReason::Other,
        stop_sequence: None,
        usage: oxa_ir::Usage {
            input_tokens: 0,
            output_tokens: 0,
            ..oxa_ir::Usage::default()
        },
    };
    let err = encode_response(&resp, &Config::default()).expect_err("stop other");
    assert!(
        err.to_string().contains("no Chat Completions equivalent"),
        "{err}"
    );
}

#[test]
fn encoding_a_stop_sequence_reports_the_value_loss() {
    let resp = oxa_ir::Response {
        id: "r".to_string(),
        model: "m".to_string(),
        content: Vec::new(),
        stop_reason: StopReason::StopSequence,
        stop_sequence: Some("END".to_string()),
        usage: oxa_ir::Usage {
            input_tokens: 1,
            output_tokens: 2,
            ..oxa_ir::Usage::default()
        },
    };
    let (wire, losses) = encode_response(&resp, &Config::default()).expect("encode");
    assert_eq!(wire.choices[0].finish_reason, "stop");
    let loss = loss_with(&losses, "", "stop_sequence");
    assert_eq!(loss.reason, LossReason::UnmappedValue);
    assert_eq!(wire.usage.as_ref().expect("usage").total_tokens, 3);
}

#[test]
fn model_map_applies_on_both_directions() {
    let mut table = Table::new();
    table.insert("gpt-4o-mini", "claude-haiku-4-5");
    let config = Config::with_model_map(table);

    // Decode: the wire model is rewritten in the IR.
    let (req, _) = decode_request(&wire_request(minimal_request()), &config).expect("decode");
    assert_eq!(req.model, "claude-haiku-4-5");

    // Encode: the table applies to whatever model value flows through. The
    // mapped value has no entry of its own, so it passes through unchanged.
    let (wire, _) = encode_request(&req, &config).expect("encode");
    assert_eq!(wire.model, "claude-haiku-4-5");

    // An IR request still carrying the source name maps the same way.
    let mut source_req = req;
    source_req.model = "gpt-4o-mini".to_string();
    let (wire, _) = encode_request(&source_req, &config).expect("encode");
    assert_eq!(wire.model, "claude-haiku-4-5");
}

#[test]
fn request_metadata_losses_are_symmetric() {
    // Request side: presence of wire metadata is one unmapped-field loss.
    let mut wire = minimal_request();
    wire["metadata"] = serde_json::json!({ "request_id": "abc" });
    let (req, losses) = decode_request(&wire_request(wire), &Config::default()).expect("decode");
    let loss = loss_with(&losses, "metadata", "metadata");
    assert_eq!(loss.reason, LossReason::UnmappedField);

    // IR side: a populated metadata map loses on encode.
    let mut ir_req = req;
    ir_req.metadata = Some(
        [("request_id".to_string(), "abc".to_string())]
            .into_iter()
            .collect(),
    );
    let (_, encode_losses) = encode_request(&ir_req, &Config::default()).expect("encode");
    let loss = loss_with(&encode_losses, "metadata", "metadata");
    assert_eq!(loss.reason, LossReason::UnmappedField);
}

#[test]
fn tool_argument_text_is_preserved_verbatim() {
    let mut wire = minimal_request();
    wire["messages"] = serde_json::json!([
        { "role": "user", "content": "go" },
        {
            "role": "assistant",
            "content": null,
            "tool_calls": [
                {
                    "id": "call_1",
                    "type": "function",
                    "function": {
                        "name": "f",
                        "arguments": "{\"x\":1e+01,\"y\":\"café\"}"
                    }
                }
            ]
        }
    ]);
    let (req, _) = decode_request(&wire_request(wire), &Config::default()).expect("decode");
    let oxa_ir::Block::ToolUse { input, .. } = &req.messages[1].content[0] else {
        panic!("expected tool_use block");
    };
    assert_eq!(input, r#"{"x":1e+01,"y":"café"}"#);

    let (wire, _) = encode_request(&req, &Config::default()).expect("encode");
    let assistant = &wire.messages[1];
    let calls = assistant.tool_calls.as_ref().expect("tool calls");
    assert_eq!(calls[0].function.arguments, r#"{"x":1e+01,"y":"café"}"#);
}
