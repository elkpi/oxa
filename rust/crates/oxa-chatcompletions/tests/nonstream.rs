//! Nonstream behavior beyond the golden vectors: structural error
//! boundaries, loss details, and the modelmap injection point.

use oxa_chatcompletions::{
    Config, Request, Response, decode_request, decode_response, encode_request, encode_response,
};
use oxa_ir::{LossReason, StopReason};
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
