//! Nonstream behavior beyond the golden vectors: structural error
//! boundaries, the question context recovery, exact decimal range
//! validation, verbatim carries, and the full client-side round trip.

use std::collections::BTreeMap;

use oxa_ir::SystemBlock;
use oxa_ir::{Block, LossReason};
use oxa_jev::{Config, JevQuestion};

fn context() -> BTreeMap<String, JevQuestion> {
    [
        (
            "urgent".to_owned(),
            JevQuestion {
                kind: "noul".to_owned(),
                instructions: None,
                criteria: None,
            },
        ),
        (
            "mood".to_owned(),
            JevQuestion {
                kind: "choice".to_owned(),
                instructions: None,
                criteria: Some(serde_json::json!({"good":"g","bad":"b"})),
            },
        ),
        (
            "level".to_owned(),
            JevQuestion {
                kind: "score".to_owned(),
                instructions: None,
                criteria: Some(serde_json::json!(["low", "high"])),
            },
        ),
    ]
    .into_iter()
    .collect()
}

fn response_with(text: &str) -> oxa_ir::Response {
    oxa_ir::Response {
        id: "r".to_owned(),
        model: "m".to_owned(),
        content: vec![Block::Text {
            text: text.to_owned(),
            citations: None,
            native: Vec::new(),
        }],
        stop_reason: oxa_ir::StopReason::EndTurn,
        stop_sequence: None,
        usage: oxa_ir::Usage {
            input_tokens: 1,
            output_tokens: 2,
            cache_read_input_tokens: None,
            cache_creation_input_tokens: None,
            input_tokens_details: None,
            output_tokens_details: None,
        },
    }
}

fn degraded_fields(losses: &[oxa_ir::Loss]) -> Vec<String> {
    losses
        .iter()
        .filter(|loss| loss.reason == LossReason::Degraded)
        .map(|loss| loss.field.clone())
        .collect()
}

#[test]
fn scalar_answers_recover_their_types_from_question_context() {
    let config = Config::with_questions(context());
    let (wire, losses) = oxa_jev::encode_response(
        &response_with(r#"{"urgent": 0.95, "mood": "surprise", "level": 1.5}"#),
        &config,
    )
    .expect("encode_response");
    let answers = &wire.answers;
    assert_eq!(
        answers
            .get("urgent")
            .and_then(|v| v.get("type"))
            .and_then(|v| v.as_str()),
        Some("noul")
    );
    assert_eq!(
        answers
            .get("mood")
            .and_then(|v| v.get("type"))
            .and_then(|v| v.as_str()),
        Some("choice")
    );
    assert_eq!(
        answers
            .get("level")
            .and_then(|v| v.get("type"))
            .and_then(|v| v.as_str()),
        Some("score")
    );
    let degraded = degraded_fields(&losses);
    assert_eq!(degraded.len(), 2);
}

#[test]
fn missing_context_answers_and_empty_responses_are_structural_errors() {
    let config = Config::with_questions(context());
    let err = oxa_jev::encode_response(&response_with(r#"{"mood": "good"}"#), &config)
        .expect_err("missing answers");
    assert!(
        err.to_string().contains("is missing from the response"),
        "{err}"
    );
    let err = oxa_jev::encode_response(&response_with("null"), &config).expect_err("null answers");
    assert!(err.to_string().contains("not a JSON object"), "{err}");
    let err = oxa_jev::encode_response(&response_with("{}"), &config).expect_err("empty answers");
    assert!(err.to_string().contains("carries no answers"), "{err}");
}

#[test]
fn unknown_answer_properties_are_dropped_with_one_loss() {
    let (_, losses) = oxa_jev::decode_response_json(
        r#"{"model":"m","answers":{"q":{"type":"noul","noul":1,"ext":"x"}},"usage":{"input_tokens":1,"output_tokens":2}}"#,
        &Config::default(),
    )
    .expect("decode_response_json");
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].field, "ext");
}

#[test]
fn losses_follow_source_encounter_order_in_raw_requests() {
    let (_, losses) = oxa_jev::decode_request_json(
        r#"{"z_ext": 1, "model": "m", "state": {}, "a_ext": 2, "questions": {"q": {"type": "noul"}}, "assets": {}}"#,
        &Config::default(),
    )
    .expect("decode_request_json");
    let fields: Vec<&str> = losses.iter().map(|loss| loss.field.as_str()).collect();
    assert_eq!(fields, ["z_ext", "state", "a_ext", "assets"]);
}

#[test]
fn scalar_state_shapes_are_structural_errors() {
    let err = oxa_jev::decode_request_json(
        r#"{"model":"m","state":true,"questions":{"q":{"type":"noul"}}}"#,
        &Config::default(),
    )
    .expect_err("boolean state");
    assert!(
        err.to_string()
            .contains("state must be a string, object, or array"),
        "{err}"
    );
}

#[test]
fn an_empty_model_name_is_a_structural_error() {
    let err = oxa_jev::decode_request_json(
        r#"{"model":"","state":"s","questions":{"q":{"type":"noul"}}}"#,
        &Config::default(),
    )
    .expect_err("empty request model");
    assert!(err.to_string().contains("model is required"), "{err}");
    let err = oxa_jev::decode_response_json(
        r#"{"model":"","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}}"#,
        &Config::default(),
    )
    .expect_err("empty response model");
    assert!(err.to_string().contains("model is required"), "{err}");
}

#[test]
fn a_whole_unsupported_question_suppresses_its_inner_field_audit() {
    let (_, losses) = oxa_jev::decode_request_json(
        r#"{"model":"m","state":"s","questions":{"box":{"type":"box","bogus":1},"ok":{"type":"noul"}}}"#,
        &Config::default(),
    )
    .expect("decode_request_json");
    let box_losses: Vec<&oxa_ir::Loss> = losses
        .iter()
        .filter(|loss| loss.path == "questions.box")
        .collect();
    assert_eq!(box_losses.len(), 1);
    assert_eq!(box_losses[0].field, "type");
}

#[test]
fn range_validation_uses_exact_decimal_comparison() {
    let config = Config::with_questions(context());
    let (_, losses) = oxa_jev::encode_response(
        &response_with(
            r#"{"urgent": 1.0000000000000001, "mood": "surprise", "level": 1.0000000000000001}"#,
        ),
        &config,
    )
    .expect("encode_response");
    // urgent noul > 1 and level score off its discrete levels are caught by
    // the exact decimal comparison; mood's choice is outside its criteria.
    let degraded = degraded_fields(&losses);
    assert_eq!(degraded.len(), 3, "losses = {losses:?}");
}

#[test]
fn typed_answers_carry_verbatim() {
    let (_, losses) = oxa_jev::encode_response(
        &response_with(
            r#"{"q":{"type":"choice","choice":"yes","probabilities":{"z":0.2,"a":0.8}}}"#,
        ),
        &Config::default(),
    )
    .expect("encode_response");
    assert!(losses.is_empty(), "losses = {losses:?}");
    // The carried probabilities object keeps its source key order and tokens.
    let (decoded, _) = oxa_jev::decode_response_json(
        r#"{"model":"m","answers":{"q":{"type":"choice","choice":"yes","probabilities":{"z":0.2,"a":0.8}}},"usage":{"input_tokens":1,"output_tokens":2}}"#,
        &Config::default(),
    )
    .expect("decode_response_json");
    let text = match decoded.content.first() {
        Some(Block::Text { text, .. }) => text.clone(),
        other => panic!("expected a text block, got {other:?}"),
    };
    assert!(
        text.contains(r#""probabilities":{"z":0.2,"a":0.8}"#),
        "canon text = {text}"
    );
}

#[test]
fn integer_like_keys_sort_lexicographically_in_canonical_text() {
    let (request, _) = oxa_jev::decode_request_json(
        r#"{"model":"m","state":{"10":"ten","2":"two"},"questions":{"q":{"type":"noul"}}}"#,
        &Config::default(),
    )
    .expect("decode_request_json");
    let system_text = match request.system.first() {
        Some(SystemBlock::Text { text, .. }) => text.clone(),
        other => panic!("expected a text system block, got {other:?}"),
    };
    assert!(
        system_text.starts_with(r#"{"10":"ten","2":"two"}"#),
        "system = {system_text}"
    );
}

#[test]
fn non_derived_user_content_reports_a_loss_on_encode() {
    let (request, _) = oxa_jev::decode_request_json(
        r#"{"model":"m","state":"state text","questions":{"q":{"type":"noul","instructions":"q?"}}}"#,
        &Config::default(),
    )
    .expect("decode_request_json");
    let mixed = oxa_ir::Request {
        messages: vec![
            oxa_ir::Message {
                role: oxa_ir::Role::User,
                content: vec![Block::Text {
                    text: "question q (noul): q?".to_owned(),
                    citations: None,
                    native: Vec::new(),
                }],
            },
            oxa_ir::Message {
                role: oxa_ir::Role::User,
                content: vec![Block::Text {
                    text: "CUSTOMER INPUT".to_owned(),
                    citations: None,
                    native: Vec::new(),
                }],
            },
        ],
        ..request.clone()
    };
    let (wire, losses) =
        oxa_jev::encode_request(&mixed, &Config::default()).expect("encode_request");
    let state = wire.state.as_str().expect("state is a string");
    assert!(!state.contains("CUSTOMER INPUT"));
    assert!(!state.contains("question q"));
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].path, "messages[1]");
}

#[test]
fn the_full_client_side_round_trip_reconstructs_questions_and_answers() {
    let (request, _) = oxa_jev::decode_request_json(
        r#"{"model":"jev-latest","state":"The customer said the delivery was late.","questions":{"sentiment":{"type":"choice","instructions":"What is the overall sentiment?","criteria":{"positive":"happy","negative":"unhappy","mixed":"both"}},"is_urgent":{"type":"noul","instructions":"Does this convey urgency?"}}}"#,
        &Config::default(),
    )
    .expect("decode_request_json");
    let (wire_request, losses) =
        oxa_jev::encode_request(&request, &Config::default()).expect("encode_request");
    assert_eq!(
        wire_request.state.as_str(),
        Some("The customer said the delivery was late.")
    );
    let question_ids: Vec<&String> = wire_request.questions.keys().collect();
    assert_eq!(
        question_ids
            .iter()
            .map(|id| id.as_str())
            .collect::<Vec<_>>(),
        vec!["is_urgent", "sentiment"]
    );
    let degraded = degraded_fields(&losses);
    assert_eq!(degraded.len(), 1);

    let questions: BTreeMap<String, JevQuestion> = wire_request
        .questions
        .iter()
        .map(|(id, value)| {
            (
                id.clone(),
                JevQuestion {
                    kind: value
                        .get("type")
                        .and_then(|v| v.as_str())
                        .unwrap_or_default()
                        .to_owned(),
                    instructions: value
                        .get("instructions")
                        .and_then(|v| v.as_str())
                        .map(str::to_owned),
                    criteria: value.get("criteria").cloned(),
                },
            )
        })
        .collect();
    let (wire_response, response_losses) = oxa_jev::encode_response(
        &response_with(
            r#"{"is_urgent": {"type": "noul", "noul": 0}, "sentiment": {"type": "choice", "choice": "mixed"}}"#,
        ),
        &Config::with_questions(questions),
    )
    .expect("encode_response");
    assert!(response_losses.is_empty(), "{response_losses:?}");
    assert_eq!(
        wire_response
            .answers
            .get("sentiment")
            .and_then(|v| v.get("choice"))
            .and_then(|v| v.as_str()),
        Some("mixed")
    );
    assert_eq!(
        wire_response
            .answers
            .get("is_urgent")
            .and_then(|v| v.get("noul"))
            .and_then(|v| v.as_i64()),
        Some(0)
    );
}
