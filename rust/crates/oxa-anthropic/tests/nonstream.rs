//! Nonstream behavior beyond the golden vectors: structural error
//! boundaries, loss details, the max_tokens default, the string shorthand,
//! and verbatim tool-input bytes (INV-1).

use oxa_anthropic::{
    Config, Request, Response, decode_request, decode_response, encode_request, encode_response,
};
use oxa_ir::{LossReason, StopReason};
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
            }],
        }],
        tools: None,
        tool_choice: Some(oxa_ir::ToolChoice {
            mode: oxa_ir::ToolChoiceMode::Tool,
            name: None,
        }),
        params: Some(oxa_ir::Params {
            temperature: None,
            top_p: None,
            max_tokens: Some(8),
            stop_sequences: None,
        }),
        metadata: None,
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
            }],
        }],
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
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
            }],
        }],
        tools: None,
        tool_choice: None,
        params: Some(oxa_ir::Params {
            temperature: None,
            top_p: None,
            max_tokens: Some(8),
            stop_sequences: None,
        }),
        metadata: None,
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
                content: vec![oxa_ir::Block::ToolUse {
                    id: "nested_1".to_string(),
                    name: "weather".to_string(),
                    input: r#"{"city":"Paris"}"#.to_string(),
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
        }),
        metadata: None,
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
