//! Codec alignment tests against the normative examples in
//! spec/01-intermediate-representation.md §8 and the JSON shapes the IR
//! schema pins. Golden vectors compare through `serde_json::Value`, so every
//! round trip here compares structurally (INV-7): object key order is
//! irrelevant, integers stay integers, and raw tool-input strings are opaque.

use oxa_ir::{
    Block, Delta, Event, EventStream, ReasoningEffort, Request, Response, from_json, to_json,
};
use serde_json::Value;

fn value(s: &str) -> Value {
    serde_json::from_str(s).expect("test JSON must parse")
}

fn assert_020_encoding_matches(input: &str, encoded: &str) {
    let mut expected = value(input);
    expected["specVersion"] = Value::String("0.2.0".to_string());
    assert_eq!(expected, value(encoded), "IR encoding differs from input");
}

const SPEC_REQUEST: &str = r#"{
  "specVersion": "0.1.0",
  "model": "claude-sonnet-4-5",
  "system": [{ "type": "text", "text": "You are a concise assistant." }],
  "messages": [
    { "role": "user", "content": [
      { "type": "text", "text": "What is the weather in Paris?" }
    ]},
    { "role": "assistant", "content": [
      { "type": "text", "text": "Let me check." },
      { "type": "tool_use", "id": "toolu_01", "name": "get_weather",
        "input": "{\"city\":\"Paris\"}" }
    ]},
    { "role": "user", "content": [
      { "type": "tool_result", "tool_use_id": "toolu_01", "content": [
        { "type": "text", "text": "18 C, clear" }
      ] }
    ]}
  ],
  "tools": [
    { "name": "get_weather", "description": "Current weather for a city",
      "input_schema": { "type": "object",
        "properties": { "city": { "type": "string" } },
        "required": ["city"] } }
  ],
  "tool_choice": { "mode": "auto" },
  "params": { "temperature": 0.7, "max_tokens": 1024 }
}"#;

const SPEC_RESPONSE: &str = r#"{
  "specVersion": "0.1.0",
  "id": "msg_017Y2hvcv",
  "model": "claude-sonnet-4-5",
  "content": [
    { "type": "text", "text": "It is 18 C and clear in Paris." }
  ],
  "stop_reason": "end_turn",
  "usage": { "input_tokens": 120, "output_tokens": 12 }
}"#;

const SPEC_EVENT_STREAM: &str = r#"{
  "specVersion": "0.1.0",
  "events": [
    { "type": "message_start", "id": "msg_017Y2hvcv", "model": "claude-sonnet-4-5" },
    { "type": "content_block_start", "index": 0,
      "block": { "type": "text", "text": "" } },
    { "type": "content_block_delta", "index": 0,
      "delta": { "type": "text_delta", "text": "It is 18 C" } },
    { "type": "content_block_delta", "index": 0,
      "delta": { "type": "text_delta", "text": " and clear in Paris." } },
    { "type": "content_block_stop", "index": 0 },
    { "type": "message_delta", "stop_reason": "end_turn",
      "usage": { "input_tokens": 120, "output_tokens": 12 } },
    { "type": "message_done" }
  ]
}"#;

#[test]
fn legacy_request_round_trips_as_spec20() {
    let req: Request = from_json(SPEC_REQUEST).expect("decode");
    let out = to_json(&req).expect("encode");
    assert_020_encoding_matches(SPEC_REQUEST, &out);
}

#[test]
fn legacy_response_round_trips_as_spec20() {
    let resp: Response = from_json(SPEC_RESPONSE).expect("decode");
    let out = to_json(&resp).expect("encode");
    assert_020_encoding_matches(SPEC_RESPONSE, &out);
}

#[test]
fn legacy_event_stream_round_trips_as_spec20() {
    let stream: EventStream = from_json(SPEC_EVENT_STREAM).expect("decode");
    let out = to_json(&stream).expect("encode");
    assert_020_encoding_matches(SPEC_EVENT_STREAM, &out);
    assert_eq!(stream.events.len(), 7);
    assert!(matches!(stream.events[0], Event::MessageStart { .. }));
    assert!(matches!(stream.events[6], Event::MessageDone {}));
}

#[test]
fn rejects_wrong_spec_version() {
    let bad = SPEC_RESPONSE.replace("\"0.1.0\"", "\"9.9.9\"");
    assert!(from_json::<Response>(&bad).is_err());
}

#[test]
fn block_discriminant_shapes_are_pinned() {
    // Every block variant serializes with the JSON `type` discriminant and
    // snake_case property names from spec/01 §3.4.
    let cases: &[(&str, Value)] = &[
        (
            r#"{"type":"text","text":"hi"}"#,
            value(r#"{"text":"hi","type":"text"}"#),
        ),
        (
            r#"{"type":"image","url":"https://example.com/cat.png"}"#,
            value(r#"{"type":"image","url":"https://example.com/cat.png"}"#),
        ),
        (
            r#"{"type":"tool_use","id":"call_1","name":"f","input":"{}"}"#,
            value(r#"{"type":"tool_use","id":"call_1","name":"f","input":"{}"}"#),
        ),
        (
            r#"{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"ok"}]}"#,
            value(
                r#"{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"ok"}]}"#,
            ),
        ),
    ];
    for (json, expected) in cases {
        let block: oxa_ir::Block = serde_json::from_str(json).expect("decode block");
        let out = serde_json::to_value(&block).expect("encode block");
        assert_eq!(&out, expected, "block shape for {json}");
    }
}

#[test]
fn absent_and_zero_are_distinct_in_params() {
    let req: Request = from_json(
        r#"{
        "specVersion": "0.1.0",
        "model": "m",
        "messages": [{ "role": "user", "content": [{ "type": "text", "text": "hi" }] }],
        "params": { "max_tokens": 0 }
    }"#,
    )
    .expect("decode");
    let out: Value = serde_json::from_str(&to_json(&req).expect("encode")).expect("value");
    let params = out.get("params").expect("params present");
    assert_eq!(params.get("max_tokens"), Some(&value("0")));
    assert!(
        params.get("temperature").is_none(),
        "absent temperature stays absent"
    );

    let bare: Request = from_json(
        r#"{
        "specVersion": "0.1.0",
        "model": "m",
        "messages": [{ "role": "user", "content": [{ "type": "text", "text": "hi" }] }]
    }"#,
    )
    .expect("decode");
    let bare_out: Value = serde_json::from_str(&to_json(&bare).expect("encode")).expect("value");
    assert!(
        bare_out.get("params").is_none(),
        "absent params stays absent"
    );
    assert!(
        bare_out.get("system").is_none(),
        "absent system stays absent"
    );
}

#[test]
fn emits_spec_020_and_dual_reads_010() {
    let legacy: Request = from_json(SPEC_REQUEST).expect("read legacy 0.1.0 request");
    let encoded = to_json(&legacy).expect("encode as 0.2.0");
    let encoded_value = value(&encoded);
    assert_eq!(encoded_value["specVersion"].as_str(), Some("0.2.0"));

    let current = SPEC_REQUEST.replace("\"0.1.0\"", "\"0.2.0\"");
    let decoded: Request = from_json(&current).expect("read 0.2.0 request");
    assert_eq!(decoded, legacy);
}

#[test]
fn thinking_blocks_and_deltas_use_locked_json_shapes() {
    let signed = Block::Thinking {
        thinking: "consider".to_string(),
        signature: Some("sig_opaque".to_string()),
    };
    assert_eq!(
        serde_json::to_value(&signed).expect("encode thinking block"),
        value(r#"{"type":"thinking","thinking":"consider","signature":"sig_opaque"}"#),
    );

    let unsigned = Block::Thinking {
        thinking: "reasoning".to_string(),
        signature: None,
    };
    assert_eq!(
        serde_json::to_value(&unsigned).expect("encode unsigned thinking block"),
        value(r#"{"type":"thinking","thinking":"reasoning"}"#),
    );

    let deltas = [
        Delta::ThinkingDelta {
            text: "reasoning".to_string(),
        },
        Delta::SignatureDelta {
            signature: "sig_opaque".to_string(),
        },
    ];
    assert_eq!(
        serde_json::to_value(deltas).expect("encode reasoning deltas"),
        value(
            r#"[{"type":"thinking_delta","text":"reasoning"},{"type":"signature_delta","signature":"sig_opaque"}]"#,
        ),
    );
}

#[test]
fn reasoning_effort_and_optional_usage_details_round_trip() {
    let mut request: Request = from_json(SPEC_REQUEST).expect("decode request");
    request
        .params
        .as_mut()
        .expect("params present")
        .reasoning_effort = Some(ReasoningEffort::High);
    let request_json = to_json(&request).expect("encode reasoning effort");
    let request_value = value(&request_json);
    assert_eq!(request_value["params"]["reasoning_effort"], "high");
    assert_eq!(
        from_json::<Request>(&request_json).expect("round-trip effort"),
        request
    );

    let absent: Response = from_json(SPEC_RESPONSE).expect("decode response without details");
    let absent_json = value(&to_json(&absent).expect("encode absent details"));
    assert!(
        absent_json["usage"]
            .get("cache_read_input_tokens")
            .is_none()
    );
    assert!(absent_json["usage"].get("input_tokens_details").is_none());

    let mut response: Response = from_json(SPEC_RESPONSE).expect("decode response");
    response.usage.cache_read_input_tokens = Some(0);
    response.usage.cache_creation_input_tokens = Some(5);
    response.usage.input_tokens_details = Some(oxa_ir::InputTokensDetails { cached_tokens: 0 });
    response.usage.output_tokens_details = Some(oxa_ir::OutputTokensDetails {
        reasoning_tokens: 7,
    });
    let encoded = to_json(&response).expect("encode usage details");
    let encoded_value = value(&encoded);
    assert_eq!(encoded_value["usage"]["cache_read_input_tokens"], 0);
    assert_eq!(encoded_value["usage"]["cache_creation_input_tokens"], 5);
    assert_eq!(
        encoded_value["usage"]["input_tokens_details"]["cached_tokens"],
        0
    );
    assert_eq!(
        encoded_value["usage"]["output_tokens_details"]["reasoning_tokens"],
        7
    );
    assert_eq!(
        from_json::<Response>(&encoded).expect("round-trip usage details"),
        response
    );
}

#[test]
fn integers_stay_integers_in_value_comparison() {
    // INV-7: 1 and 1.0 are not structurally equal even though JSON Schema
    // treats them as the same number.
    assert_ne!(value("1"), value("1.0"));
    assert_eq!(value("120"), value("120"));
}
