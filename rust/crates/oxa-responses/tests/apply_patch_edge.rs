use oxa_responses::{Config, StreamDecoder, decode_request_json, decode_response_json};

#[test]
fn test_apply_patch_nonstream_validation() {
    let cases = [
        r#"{"id":"r","model":"m","object":"response","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file"}}]}"#,
        r#"{"id":"r","model":"m","object":"response","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file","path":null}}]}"#,
        r#"{"id":"r","model":"m","object":"response","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"create_file","path":"a"}}]}"#,
        r#"{"id":"r","model":"m","object":"response","status":"completed","output":[{"id":"apc","type":"apply_patch_call","status":"completed","operation":{"type":"delete_file","path":"a"}}]}"#,
        r#"{"id":"r","model":"m","object":"response","status":"completed","output":[{"type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file","path":"a"}}]}"#,
        r#"{"id":"r","model":"m","object":"response","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed"}]}"#,
        r#"{"id":"r","model":"m","object":"response","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","operation":{"type":"delete_file","path":"a"}}]}"#,
    ];
    for case in cases {
        let res = decode_response_json(case, &Config::default());
        assert!(res.is_err(), "expected error for: {case}");
    }
}

#[test]
fn test_apply_patch_request_association() {
    let cases = [
        // duplicate call_id in same turn
        r#"{"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call","call_id":"same","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call","call_id":"same","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call_output","call_id":"same","status":"completed"},{"type":"apply_patch_call_output","call_id":"same","status":"completed"}]}"#,
        // mismatched call_id in same turn
        r#"{"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call","call_id":"a","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call_output","call_id":"b","status":"future"}]}"#,
        // orphaned result
        r#"{"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call_output","call_id":"c","status":"completed"}]}"#,
    ];
    for case in cases {
        let res = decode_request_json(case, &Config::default());
        assert!(res.is_err(), "expected INV-3 error for: {case}");
    }
}

#[test]
fn test_apply_patch_streaming_unknown_semantics() {
    let scenarios = [
        (r#"{"type":"future","extra":true}"#, "completed"),
        (r#"{"type":"delete_file","path":"a"}"#, "future"),
        (
            r#"{"type":"delete_file","path":"a","diff":42}"#,
            "completed",
        ),
    ];
    for (op, status) in scenarios {
        let mut d = StreamDecoder::new(&Config::default());
        d.feed_json(r#"{"type":"response.created","response":{"id":"r","model":"m","status":"in_progress","output":[]}}"#).unwrap();
        d.feed_json(&format!(
            r#"{{"type":"response.output_item.added","output_index":0,"item":{{"id":"apc","type":"apply_patch_call","call_id":"c","status":"in_progress","operation":{op}}}}}"#
        )).unwrap();
        let events = d.feed_json(&format!(
            r#"{{"type":"response.output_item.done","output_index":0,"item":{{"id":"apc","type":"apply_patch_call","call_id":"c","status":"{status}","operation":{op}}}}}"#
        )).unwrap();
        if status == "future" || op.contains("future") {
            assert!(events.is_empty(), "expected 0 events for unknown semantics");
        }
        d.feed_json(&format!(
            r#"{{"type":"response.completed","response":{{"id":"r","model":"m","status":"completed","output":[{{"id":"apc","type":"apply_patch_call","call_id":"c","status":"{status}","operation":{op}}}]}}}}"#
        )).unwrap();
        d.flush().unwrap();
        assert!(!d.losses().is_empty());
    }
}
