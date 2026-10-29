use oxa_ir::{request_from_json, request_to_json};

#[test]
fn openai_to_anthropic_preserves_schema_source() {
    let schema = r#"{ "type" : "object", "maximum" : 1e400 }"#;
    let chat = format!(
        r#"{{"model":"m","max_tokens":64,"messages":[{{"role":"user","content":"hi"}}],"response_format":{{"type":"json_schema","json_schema":{{"name":"result","schema":{schema}}}}}}}"#
    );
    let responses = format!(
        r#"{{"model":"m","max_output_tokens":64,"input":"hi","text":{{"format":{{"type":"json_schema","name":"result","schema":{schema}}}}}}}"#
    );
    let (chat_req, _) =
        oxa_chatcompletions::decode_request_json(&chat, &Default::default()).unwrap();
    let (responses_req, _) =
        oxa_responses::decode_request_json(&responses, &Default::default()).unwrap();
    for req in [chat_req, responses_req] {
        let canonical = request_to_json(&req).unwrap();
        assert!(canonical.contains(schema), "{canonical}");
        let req = request_from_json(&canonical).unwrap();
        let (out, losses) = oxa_anthropic::encode_request_json(&req, &Default::default()).unwrap();
        assert_eq!(losses.len(), 1);
        assert!(out.contains(schema), "{out}");
    }
}
