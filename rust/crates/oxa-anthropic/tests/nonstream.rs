//! Nonstream behavior beyond the golden vectors: structural error
//! boundaries, loss details, the max_tokens default, the string shorthand,
//! and verbatim tool-input bytes (INV-1).

use oxa_anthropic::{
    Config, Request, Response, decode_request, decode_request_json, decode_response,
    encode_request, encode_response,
};
use oxa_ir::{Block, LossReason, ReasoningEffort, StopReason, Usage};
use serde_json::Value;

fn wire_request(json: Value) -> Request {
    serde_json::from_value(json).expect("test wire request must deserialize")
}

fn wire_response(json: Value) -> Response {
    serde_json::from_value(json).expect("test wire response must deserialize")
}

fn minimal_request() -> Value {
    serde_json::json!({
        "model": "claude-sonnet-4-5",
        "max_tokens": 128,
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
fn max_tokens_must_be_positive() {
    let mut wire = minimal_request();
    wire["max_tokens"] = serde_json::json!(0);
    let err = decode_request(&wire_request(wire), &Config::default()).expect_err("zero max_tokens");
    assert!(err.to_string().contains("max_tokens is required"), "{err}");
}

#[test]
fn unknown_role_is_a_structural_error() {
    let mut wire = minimal_request();
    wire["messages"] = serde_json::json!([{ "role": "chief", "content": "Hello" }]);
    let err = decode_request(&wire_request(wire), &Config::default()).expect_err("unknown role");
    assert!(err.to_string().contains("unknown role \"chief\""), "{err}");
}

#[test]
fn request_without_messages_is_a_structural_error() {
    let mut wire = minimal_request();
    wire["messages"] = serde_json::json!([]);
    let err = decode_request(&wire_request(wire), &Config::default()).expect_err("no messages");
    assert!(err.to_string().contains("carries no messages"), "{err}");
}

#[test]
fn missing_stop_reason_is_a_structural_error() {
    let wire = wire_response(serde_json::json!({
        "id": "r", "type": "message", "role": "assistant", "model": "m",
        "content": [{ "type": "text", "text": "hi" }],
        "stop_reason": ""
    }));
    let err = decode_response(&wire, &Config::default()).expect_err("missing stop reason");
    assert!(err.to_string().contains("stop_reason is missing"), "{err}");
}

#[test]
fn unsupported_system_block_type_is_a_structural_error() {
    let mut wire = minimal_request();
    wire["system"] = serde_json::json!([{ "type": "image", "text": "nope" }]);
    let err = decode_request(&wire_request(wire), &Config::default()).expect_err("system type");
    assert!(
        err.to_string().contains("unsupported block type \"image\""),
        "{err}"
    );
}

#[test]
fn tool_use_without_id_name_or_object_input_are_errors() {
    let base = serde_json::json!({
        "model": "m",
        "max_tokens": 8,
        "messages": [
            { "role": "assistant", "content": [{ "type": "tool_use", "name": "f", "input": {} }] }
        ]
    });
    let err =
        decode_request(&wire_request(base.clone()), &Config::default()).expect_err("missing id");
    assert!(
        err.to_string()
            .contains("messages[0].content[0].id is required"),
        "{err}"
    );

    let mut no_name = base.clone();
    no_name["messages"][0]["content"][0] =
        serde_json::json!({ "type": "tool_use", "id": "t", "input": {} });
    let err = decode_request(&wire_request(no_name), &Config::default()).expect_err("missing name");
    assert!(err.to_string().contains(".name is required"), "{err}");

    let mut no_input = base;
    no_input["messages"][0]["content"][0] =
        serde_json::json!({ "type": "tool_use", "id": "t", "name": "f" });
    let err =
        decode_request(&wire_request(no_input), &Config::default()).expect_err("missing input");
    assert!(err.to_string().contains(".input is required"), "{err}");
}

#[test]
fn tool_choice_named_requires_a_name() {
    let mut wire = minimal_request();
    wire["tool_choice"] = serde_json::json!({ "type": "tool" });
    let err = decode_request(&wire_request(wire), &Config::default()).expect_err("decode name");
    assert!(
        err.to_string().contains("tool_choice.name is required"),
        "{err}"
    );

    let req = oxa_ir::Request {
        model: "m".to_string(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![oxa_ir::Block::Text {
                text: "hi".to_string(),
                citations: None,
                native: Vec::new(),
            }],
        }],
        tools: None,
        tool_choice: Some(oxa_ir::ToolChoice {
            mode: oxa_ir::ToolChoiceMode::Tool,
            name: None,
            tool_type: None,
        }),
        params: Some(oxa_ir::Params {
            temperature: None,
            top_p: None,
            max_tokens: Some(8),
            stop_sequences: None,
            reasoning_effort: None,
            response_format: None,
        }),
        metadata: None,
        native: Vec::new(),
    };
    let err = encode_request(&req, &Config::default()).expect_err("encode name");
    assert!(
        err.to_string().contains("tool_choice.name is required"),
        "{err}"
    );
}

#[test]
fn unknown_stop_reason_maps_to_other_with_a_loss() {
    let wire = wire_response(serde_json::json!({
        "id": "r", "type": "message", "role": "assistant", "model": "m",
        "content": [{ "type": "text", "text": "hi" }],
        "stop_reason": "model_context_window_exceeded"
    }));
    let (resp, losses) = decode_response(&wire, &Config::default()).expect("decode");
    assert_eq!(resp.stop_reason, StopReason::Other);
    let loss = loss_with(&losses, "stop_reason", "stop_reason");
    assert_eq!(loss.reason, LossReason::UnmappedValue);
}

#[test]
fn response_missing_content_is_a_structural_error() {
    let wire = wire_response(serde_json::json!({
        "id": "r", "type": "message", "role": "assistant", "model": "m",
        "content": [{ "type": "tool_result", "tool_use_id": "t" }],
        "stop_reason": "end_turn"
    }));
    let err = decode_response(&wire, &Config::default()).expect_err("missing content");
    assert!(
        err.to_string().contains("content[0].content is missing"),
        "{err}"
    );
}

#[test]
fn missing_max_tokens_defaults_to_4096_with_a_degraded_loss() {
    let req = oxa_ir::Request {
        model: "m".to_string(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![oxa_ir::Block::Text {
                text: "hi".to_string(),
                citations: None,
                native: Vec::new(),
            }],
        }],
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
        native: Vec::new(),
    };
    let (wire, losses) = encode_request(&req, &Config::default()).expect("encode");
    assert_eq!(wire.max_tokens, 4096);
    let loss = loss_with(&losses, "params", "max_tokens");
    assert_eq!(loss.reason, LossReason::Degraded);
    assert!(loss.detail.contains("defaulting to 4096"), "{loss:?}");
}

#[test]
fn single_text_message_renders_the_string_shorthand() {
    let req = oxa_ir::Request {
        model: "m".to_string(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![oxa_ir::Block::Text {
                text: "hi".to_string(),
                citations: None,
                native: Vec::new(),
            }],
        }],
        tools: None,
        tool_choice: None,
        params: Some(oxa_ir::Params {
            temperature: None,
            top_p: None,
            max_tokens: Some(8),
            stop_sequences: None,
            reasoning_effort: None,
            response_format: None,
        }),
        metadata: None,
        native: Vec::new(),
    };
    let (wire, _) = encode_request(&req, &Config::default()).expect("encode");
    assert_eq!(
        wire.messages[0].content,
        Some(oxa_anthropic::ContentValue::Text("hi".to_string()))
    );

    // With a system prompt the shorthand no longer applies.
    let mut with_system = req;
    with_system.system = vec![oxa_ir::SystemBlock::Text {
        text: "be brief".to_string(),
        native: Vec::new(),
    }];
    let (wire, _) = encode_request(&with_system, &Config::default()).expect("encode");
    assert!(matches!(
        wire.messages[0].content,
        Some(oxa_anthropic::ContentValue::Blocks(_))
    ));
}

#[test]
fn encoding_ir_stop_other_is_a_structural_error() {
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
    assert!(err.to_string().contains("no Anthropic equivalent"), "{err}");
}

#[test]
fn unsupported_tool_result_content_is_dropped_with_a_loss() {
    let req = oxa_ir::Request {
        model: "m".to_string(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![oxa_ir::Block::ToolResult {
                tool_use_id: "call_1".to_string(),
                native: Vec::new(),
                content: vec![oxa_ir::Block::ToolUse {
                    id: "nested_1".to_string(),
                    name: "weather".to_string(),
                    input: r#"{"city":"Paris"}"#.to_string(),
                    native: Vec::new(),
                }],
                is_error: None,
            }],
        }],
        tools: None,
        tool_choice: None,
        params: Some(oxa_ir::Params {
            temperature: None,
            top_p: None,
            max_tokens: Some(8),
            stop_sequences: None,
            reasoning_effort: None,
            response_format: None,
        }),
        metadata: None,
        native: Vec::new(),
    };

    let (wire, losses) = encode_request(&req, &Config::default()).expect("encode");
    let Some(oxa_anthropic::ContentValue::Blocks(blocks)) = &wire.messages[0].content else {
        panic!("tool_result must render as a block array");
    };
    let Some(oxa_anthropic::ContentValue::Blocks(content)) = &blocks[0].content else {
        panic!("tool_result content must render as a block array");
    };
    assert!(content.is_empty());
    let loss = loss_with(&losses, "messages[0].content[0].content[0]", "type");
    assert_eq!(loss.reason, LossReason::UnsupportedSemantic);
}

#[test]
fn block_content_deserializes_raw_tool_input_verbatim() {
    let content: oxa_anthropic::ContentValue =
        serde_json::from_str(r#"[{"type":"tool_use","id":"t1","name":"f","input":{"x":1e+01}}]"#)
            .expect("block content must deserialize");
    let oxa_anthropic::ContentValue::Blocks(blocks) = content else {
        panic!("array content must remain blocks");
    };
    assert_eq!(
        blocks[0]
            .input
            .as_deref()
            .map(serde_json::value::RawValue::get),
        Some(r#"{"x":1e+01}"#)
    );
}

#[test]
fn request_json_deserialization_canonicalizes_tool_input_in_generic_content() {
    // Anthropic request `messages[].content` follows the reference face's
    // generic content boundary. The intermediate value has no raw source
    // bytes, so tool input is serialized canonically before reaching the
    // typed block representation.
    let wire: Request = serde_json::from_str(
        r#"{
            "model":"claude-sonnet-4-5",
            "max_tokens":128,
            "messages":[{
                "role":"assistant",
                "content":[{
                    "type":"tool_use",
                    "id":"t1",
                    "name":"f",
                    "input":{"x":1e+01}
                }]
            }]
        }"#,
    )
    .expect("deserialize");
    let (req, _) = decode_request(&wire, &Config::default()).expect("decode");
    let oxa_ir::Block::ToolUse { input, .. } = &req.messages[0].content[0] else {
        panic!("expected tool_use block");
    };
    assert_eq!(input, r#"{"x":10.0}"#);

    let (encoded, _) = encode_request(&req, &Config::default()).expect("encode");
    let encoded = serde_json::to_string(&encoded).expect("serialize");
    assert!(
        encoded.contains(r#""input":{"x":10.0}"#),
        "input remains canonical in the wire JSON: {encoded}"
    );
}

#[test]
fn decodes_anthropic_reasoning_effort_at_budget_boundaries() {
    for (budget, expected) in [
        (2048, ReasoningEffort::Low),
        (2049, ReasoningEffort::Medium),
        (8192, ReasoningEffort::Medium),
        (8193, ReasoningEffort::High),
    ] {
        let mut wire = minimal_request();
        wire["thinking"] = serde_json::json!({
            "type": "enabled",
            "budget_tokens": budget
        });
        let (decoded, losses) =
            decode_request(&wire_request(wire), &Config::default()).expect("decode request");
        assert_eq!(
            decoded
                .params
                .as_ref()
                .and_then(|params| params.reasoning_effort),
            Some(expected),
            "budget {budget}"
        );
        assert!(losses.is_empty(), "budget {budget}");
    }
}

#[test]
fn encodes_reasoning_effort_and_reports_unsigned_thinking_replay() {
    let req = oxa_ir::Request {
        model: "m".to_string(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::Assistant,
            content: vec![Block::Thinking {
                thinking: "reasoning".to_string(),
                signature: None,
                native: Vec::new(),
            }],
        }],
        tools: None,
        tool_choice: None,
        params: Some(oxa_ir::Params {
            temperature: None,
            top_p: None,
            max_tokens: Some(8),
            stop_sequences: None,
            reasoning_effort: Some(ReasoningEffort::Minimal),
            response_format: None,
        }),
        metadata: None,
        native: Vec::new(),
    };

    let (wire, losses) = encode_request(&req, &Config::default()).expect("encode request");
    let thinking = wire.thinking.as_ref().expect("thinking config");
    assert_eq!(thinking.kind, "enabled");
    assert_eq!(thinking.budget_tokens, Some(1024));
    let Some(oxa_anthropic::ContentValue::Blocks(blocks)) = &wire.messages[0].content else {
        panic!("thinking block must render as a block array");
    };
    assert_eq!(blocks[0].kind, "thinking");
    assert_eq!(blocks[0].thinking.as_deref(), Some("reasoning"));
    assert_eq!(blocks[0].signature, None);
    assert_eq!(losses.len(), 2);
    assert_eq!(
        loss_with(&losses, "params.reasoning_effort", "reasoning_effort").reason,
        LossReason::Degraded
    );
    assert_eq!(
        loss_with(&losses, "messages[0].content[0]", "signature").detail,
        "unsigned thinking block; Anthropic may reject on replay"
    );
}

#[test]
fn preserves_thinking_signature_and_optional_cache_usage() {
    let wire = wire_response(serde_json::json!({
        "id": "msg_1",
        "type": "message",
        "role": "assistant",
        "model": "claude",
        "content": [{
            "type": "thinking",
            "thinking": "consider",
            "signature": "opaque-signature"
        }],
        "stop_reason": "end_turn",
        "usage": {
            "input_tokens": 4,
            "output_tokens": 2,
            "cache_read_input_tokens": 0
        }
    }));
    let (decoded, losses) = decode_response(&wire, &Config::default()).expect("decode response");
    assert!(losses.is_empty());
    assert_eq!(
        decoded.content,
        vec![Block::Thinking {
            thinking: "consider".to_string(),
            signature: Some("opaque-signature".to_string()),
            native: Vec::new(),
        }]
    );
    assert_eq!(
        decoded.usage,
        Usage {
            input_tokens: 4,
            output_tokens: 2,
            cache_read_input_tokens: Some(0),
            ..Usage::default()
        }
    );

    let (encoded, losses) = encode_response(&decoded, &Config::default()).expect("encode response");
    assert!(losses.is_empty());
    assert_eq!(encoded.content[0].kind, "thinking");
    assert_eq!(encoded.content[0].thinking.as_deref(), Some("consider"));
    assert_eq!(
        encoded.content[0].signature.as_deref(),
        Some("opaque-signature")
    );
    assert_eq!(
        encoded
            .usage
            .as_ref()
            .and_then(|usage| usage.cache_read_input_tokens),
        Some(0)
    );
    assert_eq!(
        encoded
            .usage
            .as_ref()
            .and_then(|usage| usage.cache_creation_input_tokens),
        None
    );
}

#[test]
fn decodes_a_thinking_block_with_no_thinking_field_as_empty_text() {
    let wire = wire_response(serde_json::json!({
        "id": "msg_bare",
        "type": "message",
        "role": "assistant",
        "model": "claude",
        "content": [{ "type": "thinking", "signature": "sig_x" }],
        "stop_reason": "end_turn",
        "usage": { "input_tokens": 1, "output_tokens": 1 }
    }));
    let (decoded, losses) = decode_response(&wire, &Config::default()).expect("decode response");
    assert!(losses.is_empty());
    assert_eq!(
        decoded.content,
        vec![Block::Thinking {
            thinking: String::new(),
            signature: Some("sig_x".to_string()),
            native: Vec::new(),
        }]
    );
}

#[test]
fn empty_signature_is_treated_as_unsigned_on_encode() {
    let response = oxa_ir::Response {
        id: "msg_empty_sig".to_string(),
        model: "claude".to_string(),
        content: vec![Block::Thinking {
            thinking: "consider".to_string(),
            signature: Some(String::new()),
            native: Vec::new(),
        }],
        stop_reason: StopReason::EndTurn,
        stop_sequence: None,
        usage: Usage::default(),
    };
    let (wire, losses) = encode_response(&response, &Config::default()).expect("encode response");
    let loss = loss_with(&losses, "content[0]", "signature");
    assert_eq!(loss.reason, LossReason::Degraded);
    assert_eq!(wire.content[0].signature, None);
}

#[test]
fn test_conflicting_function_declaration_not_promoted_to_custom() {
    let wire = wire_request(serde_json::json!({
        "model": "claude-3-5-sonnet-20241022",
        "max_tokens": 1024,
        "tools": [
            {
                "name": "echo",
                "input_schema": {
                    "type": "object",
                    "properties": {"input": {"type": "string"}, "extra": {"type": "string"}},
                    "required": ["input"]
                }
            }
        ],
        "messages": [
            {"role": "user", "content": "hi"},
            {
                "role": "assistant",
                "content": [
                    {"type": "tool_use", "id": "call_1", "name": "echo", "input": {"input": "hello"}}
                ]
            },
            {
                "role": "user",
                "content": [{"type": "tool_result", "tool_use_id": "call_1", "content": "ok"}]
            }
        ]
    }));
    let (req, losses) = decode_request(&wire, &Config::default()).expect("decode request");
    assert!(losses.is_empty());
    assert_eq!(req.tools.as_ref().unwrap()[0].tool_type, None);
    assert!(matches!(req.messages[1].content[0], Block::ToolUse { .. }));
    assert!(matches!(
        req.messages[2].content[0],
        Block::ToolResult { .. }
    ));
}

#[test]
fn test_grammar_format_loss_on_anthropic_encode() {
    let req = oxa_ir::Request {
        model: "claude".to_string(),
        system: Vec::new(),
        messages: vec![oxa_ir::Message {
            role: oxa_ir::Role::User,
            content: vec![Block::Text {
                text: "hi".to_string(),
                citations: None,
                native: Vec::new(),
            }],
        }],
        tools: Some(vec![oxa_ir::Tool {
            tool_type: Some(oxa_ir::ToolType::Custom),
            name: "calc".to_string(),
            description: None,
            input_schema: None,
            format: Some(oxa_ir::CustomToolFormat {
                format_type: "grammar".to_string(),
                syntax: Some("regex".to_string()),
                definition: Some("[0-9]+".to_string()),
            }),
            native: Vec::new(),
            source: None,
            definition: None,
        }]),
        tool_choice: None,
        params: Some(oxa_ir::Params {
            max_tokens: Some(1024),
            ..Default::default()
        }),
        metadata: None,
        native: Vec::new(),
    };
    let (_, losses) = encode_request(&req, &Config::default()).expect("encode request");
    assert!(
        losses
            .iter()
            .any(|l| l.path == "tools[0].format" && l.reason == LossReason::UnsupportedSemantic)
    );
}

#[test]
fn test_mid_conversation_system_messages() {
    let wire = wire_request(serde_json::json!({
        "model": "claude-opus-5-5",
        "max_tokens": 1024,
        "system": [{"type": "text", "text": "Base prompt."}],
        "messages": [
            {"role": "user", "content": "Hi"},
            {"role": "assistant", "content": "Hello"},
            {"role": "user", "content": "Summarize"},
            {"role": "system", "content": "Terse mode."},
            {
                "role": "system",
                "content": [
                    {
                        "type": "text",
                        "text": "Under 20 words.",
                        "cache_control": {"type": "ephemeral"}
                    }
                ]
            }
        ]
    }));
    let (req, losses) = decode_request(&wire, &Config::default()).expect("decode request");
    assert_eq!(req.messages.len(), 3);
    assert_eq!(req.system.len(), 3);
    let oxa_ir::SystemBlock::Text { text: t0, .. } = &req.system[0];
    assert_eq!(t0, "Base prompt.");
    let oxa_ir::SystemBlock::Text { text: t1, .. } = &req.system[1];
    assert_eq!(t1, "Terse mode.");
    let oxa_ir::SystemBlock::Text {
        text: t2,
        native: n2,
    } = &req.system[2];
    assert_eq!(t2, "Under 20 words.");
    assert_eq!(n2.len(), 1);
    assert_eq!(n2[0].kind, "cache_control");

    assert_eq!(losses.len(), 2);
    assert_eq!(losses[0].path, "messages[3].role");
    assert_eq!(losses[0].field, "role");
    assert_eq!(losses[0].reason, LossReason::Degraded);
    assert!(
        losses[0]
            .detail
            .contains("carried as request-level system content")
    );
    assert_eq!(losses[1].path, "messages[4].role");
    assert_eq!(losses[1].field, "role");
    assert_eq!(losses[1].reason, LossReason::Degraded);
}

#[test]
fn test_mid_conversation_system_loss_order() {
    let wire = wire_request(serde_json::json!({
        "model": "claude-opus-5-5",
        "max_tokens": 1024,
        "messages": [
            {"role": "user", "content": "Hi"},
            {"role": "system", "content": [
                {"type": "text", "text": "x", "cache_control": {"type": "ephemeral", "ttl": "10m"}}
            ]}
        ]
    }));
    let (_, losses) = decode_request(&wire, &Config::default()).expect("decode request");
    assert_eq!(losses.len(), 2);
    assert_eq!(losses[0].path, "messages[1].role");
    assert_eq!(losses[0].reason, LossReason::Degraded);
    assert_eq!(losses[1].path, "messages[1].content[0].cache_control.ttl");
    assert_eq!(losses[1].reason, LossReason::UnmappedValue);
}

#[test]
fn test_mid_conversation_system_placement_and_content_errors() {
    // index 0
    let wire = wire_request(serde_json::json!({
        "model": "claude-opus-5-5",
        "max_tokens": 1024,
        "messages": [
            {"role": "system", "content": "Init"},
            {"role": "user", "content": "Hi"}
        ]
    }));
    let err = decode_request(&wire, &Config::default()).expect_err("system at 0");
    assert!(
        err.to_string()
            .contains("messages[0]: system message cannot appear at messages[0]")
    );

    // followed by user
    let wire = wire_request(serde_json::json!({
        "model": "claude-opus-5-5",
        "max_tokens": 1024,
        "messages": [
            {"role": "user", "content": "Hi"},
            {"role": "system", "content": "Mid"},
            {"role": "user", "content": "Another"}
        ]
    }));
    let err = decode_request(&wire, &Config::default()).expect_err("followed by user");
    assert!(
        err.to_string().contains(
            "system message must be followed by an assistant turn or be the last message"
        )
    );

    // preceded by assistant
    let wire = wire_request(serde_json::json!({
        "model": "claude-opus-5-5",
        "max_tokens": 1024,
        "messages": [
            {"role": "user", "content": "Hi"},
            {"role": "assistant", "content": "Answer"},
            {"role": "system", "content": "Mid"}
        ]
    }));
    let err = decode_request(&wire, &Config::default()).expect_err("preceded by assistant");
    assert!(
        err.to_string()
            .contains("system message must follow a user turn")
    );

    // non-text block
    let wire = wire_request(serde_json::json!({
        "model": "claude-opus-5-5",
        "max_tokens": 1024,
        "messages": [
            {"role": "user", "content": "Hi"},
            {
                "role": "system",
                "content": [
                    {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "abc"}}
                ]
            }
        ]
    }));
    let err = decode_request(&wire, &Config::default()).expect_err("non-text content");
    assert!(err.to_string().contains("system content must be text"));
}

#[test]
fn document_source_rejects_explicit_conflicting_empty_field() {
    let raw = r#"{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"YQ==","url":""}}]}]}"#;
    let err = decode_request_json(raw, &Config::default()).expect_err("conflicting source field");
    assert!(err.to_string().contains("conflicting source fields"));

    let raw = r#"{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"file","file_id":"f"},"title":"","context":""}]}]}"#;
    let (request, _) = decode_request_json(raw, &Config::default()).expect("empty metadata decode");
    let (wire, _) = encode_request(&request, &Config::default()).expect("empty metadata encode");
    let value = serde_json::to_value(wire).expect("serialize wire");
    let block = &value["messages"][0]["content"][0];
    assert_eq!(block["title"], "");
    assert_eq!(block["context"], "");
}

#[test]
fn document_ir_with_url_and_file_id_drops_with_loss() {
    let raw = r#"{"specVersion":"0.2.0","model":"m","params":{"max_tokens":1},"messages":[{"role":"user","content":[{"type":"text","text":"keep"},{"type":"document","url":"https://example.com/a.pdf","file_id":"f"}]}]}"#;
    let request = oxa_ir::from_json::<oxa_ir::Request>(raw).expect("valid IR");
    let (wire, losses) = encode_request(&request, &Config::default()).expect("encode");
    let value = serde_json::to_value(wire).expect("serialize wire");
    let content = value["messages"][0]["content"].as_array().expect("content");
    assert_eq!(content.len(), 1, "document block must be dropped");
    let loss = loss_with(&losses, "messages[0].content[1]", "document");
    assert_eq!(loss.reason, LossReason::UnsupportedSemantic);
}

#[test]
fn document_null_citations_is_structural_error() {
    let raw = r#"{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"file","file_id":"f"},"citations":null}]}]}"#;
    let err = decode_request_json(raw, &Config::default()).expect_err("null citations");
    assert!(err.to_string().contains("citations must be a JSON object"));
}

#[test]
fn document_typed_wire_conflicting_source_is_error() {
    let mut wire: Request = serde_json::from_value(serde_json::json!({
        "model": "m",
        "max_tokens": 1,
        "messages": [{ "role": "user", "content": [
            { "type": "document", "source": { "type": "file", "file_id": "f" } }
        ]}]
    }))
    .expect("typed wire");
    if let Some(oxa_anthropic::ContentValue::Blocks(blocks)) = &mut wire.messages[0].content {
        blocks[0].source.as_mut().unwrap().url = "https://example.com/a.pdf".into();
    }
    let err = decode_request(&wire, &Config::default()).expect_err("edited conflicting source");
    assert!(err.to_string().contains("conflicting source fields"));
}

#[test]
fn image_file_source_drops_as_whole_block_without_audit_noise() {
    let raw = r#"{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"f","future":1}}]}]}"#;
    let (request, losses) = decode_request_json(raw, &Config::default()).expect("decode");
    assert_eq!(
        request.messages[0].content.len(),
        0,
        "image must be dropped"
    );
    assert_eq!(losses.len(), 1, "exactly the whole-block loss: {losses:?}");
    assert_eq!(losses[0].reason, LossReason::UnsupportedSemantic);

    let raw = r#"{"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":123}}]}]}"#;
    let (request, losses) = decode_request_json(raw, &Config::default()).expect("decode");
    assert_eq!(request.messages[0].content.len(), 0);
    assert_eq!(losses.len(), 1);
}

#[test]
fn ir_response_rejects_document_blocks() {
    let raw = r#"{"specVersion":"0.2.0","id":"r","model":"m","content":[{"type":"document","file_id":"f"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}"#;
    let err =
        oxa_ir::from_json::<oxa_ir::Response>(raw).expect_err("document is request-side only");
    assert!(err.to_string().contains("request-side only"));

    let response = oxa_ir::Response {
        id: "r".into(),
        model: "m".into(),
        content: vec![Block::Document {
            media_type: None,
            data: None,
            url: None,
            file_id: Some("f".into()),
            native: Vec::new(),
        }],
        stop_reason: StopReason::EndTurn,
        stop_sequence: None,
        usage: Usage::default(),
    };
    let err = oxa_ir::to_json(&response).expect_err("document is request-side only");
    assert!(err.to_string().contains("request-side only"));
}
