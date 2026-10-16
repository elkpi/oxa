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
    let raw = r#"{"model":"m","input":"hi","tools":[{"type":"function","name":"f","parameters":{"unknown_extension":true}},{"type":"unsupported_tool","unknown_extension":true}]}"#;
    let (_, losses) = decode_request_json(raw, &Config::default()).unwrap();
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].reason, oxa_ir::LossReason::UnsupportedSemantic);
}

#[test]
fn custom_format_and_nested_output_unknown_fields_are_audited() {
    let raw = r#"{"model":"m","tools":[{"type":"custom","name":"exec","format":{"type":"grammar","syntax":"regex","definition":".*","extension":null}}],"input":[{"role":"user","content":"go"},{"type":"custom_tool_call","call_id":"c","name":"exec","input":"opaque"},{"type":"custom_tool_call_output","call_id":"c","output":[{"type":"input_text","text":"ok","extension":false}]}]}"#;
    let (_, losses) = decode_request_json(raw, &Config::default()).unwrap();
    assert_eq!(
        losses
            .iter()
            .map(|loss| loss.path.as_str())
            .collect::<Vec<_>>(),
        ["tools[0].format.extension", "input[2].output[0].extension",]
    );
}

#[test]
fn unsupported_custom_output_items_are_reported_once_without_descendant_audit() {
    let raw = r#"{"model":"m","input":[{"role":"user","content":"go"},{"type":"custom_tool_call","call_id":"c","name":"exec","input":""},{"type":"custom_tool_call_output","call_id":"c","output":[{"type":"input_file","file_id":"f","text":"ignored","image_url":"ignored","extension":false}]}]}"#;
    let (_, losses) = decode_request_json(raw, &Config::default()).unwrap();
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].path, "input[2].output[0]");
    assert_eq!(losses[0].field, "type");
}

#[test]
fn custom_text_part_audits_fields_from_other_variants() {
    let raw = r#"{"model":"m","input":[{"role":"user","content":"go"},{"type":"custom_tool_call","call_id":"c","name":"exec","input":""},{"type":"custom_tool_call_output","call_id":"c","output":[{"type":"input_text","text":"ok","image_url":"https://example.com/x.png"}]}]}"#;
    let (_, losses) = decode_request_json(raw, &Config::default()).unwrap();
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].path, "input[2].output[0].image_url");
    assert_eq!(losses[0].reason, oxa_ir::LossReason::UnmappedField);
}
