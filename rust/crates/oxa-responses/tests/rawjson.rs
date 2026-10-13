use oxa_responses::{Config, decode_request_json};

#[test]
fn unknown_fields_preserve_presence_and_source_order() {
    let raw = r#"{"model":"m","input":"hi","store":false,"previous_response_id":"r1","unknown_extension":null}"#;
    let (request, losses) = decode_request_json(raw, &Config::default()).unwrap();
    assert_eq!(request.messages.len(), 1);
    assert_eq!(
        losses
            .iter()
            .map(|loss| loss.path.as_str())
            .collect::<Vec<_>>(),
        ["store", "previous_response_id", "unknown_extension"]
    );
}

#[test]
fn opaque_schema_and_whole_unsupported_units_are_not_recursed() {
    let raw = r#"{"model":"m","input":"hi","tools":[{"type":"function","name":"f","parameters":{"unknown_extension":true}},{"type":"custom","unknown_extension":true}]}"#;
    let (_, losses) = decode_request_json(raw, &Config::default()).unwrap();
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].reason, oxa_ir::LossReason::UnsupportedSemantic);
}
