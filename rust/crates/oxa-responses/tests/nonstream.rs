use oxa_ir::{
    Block, LossReason, Message, Params, Request as IrRequest, Response as IrResponse, Role,
    StopReason, Usage,
};
use oxa_responses::{
    Config, ContentValue, Input, InputItem, Request, decode_request, encode_request,
    encode_response,
};

#[test]
fn decodes_function_call_arguments_without_normalizing_their_json_text() {
    let wire = Request {
        model: "gpt-4o-mini".to_string(),
        input: Input::Items(vec![InputItem {
            kind: "function_call".to_string(),
            call_id: "call_1".to_string(),
            name: "weather".to_string(),
            arguments: "{\"temperature\":1e+01}".to_string(),
            ..InputItem::default()
        }]),
        ..Request::default()
    };

    let (request, losses) = decode_request(&wire, &Config::default()).expect("decode request");

    assert!(losses.is_empty());
    assert_eq!(
        request.messages,
        vec![Message {
            role: Role::Assistant,
            content: vec![Block::ToolUse {
                id: "call_1".to_string(),
                name: "weather".to_string(),
                input: "{\"temperature\":1e+01}".to_string(),
            }],
        }]
    );
}

#[test]
fn encodes_tool_results_before_normal_user_content_and_reports_reordering() {
    let request = IrRequest {
        model: "gpt-4o-mini".to_string(),
        system: Vec::new(),
        messages: vec![Message {
            role: Role::User,
            content: vec![
                Block::Text {
                    text: "Use the tool result.".to_string(),
                },
                Block::ToolResult {
                    tool_use_id: "call_1".to_string(),
                    content: vec![Block::Text {
                        text: "Sunny".to_string(),
                    }],
                    is_error: None,
                },
            ],
        }],
        tools: None,
        tool_choice: None,
        params: Some(Params {
            temperature: None,
            top_p: None,
            max_tokens: None,
            stop_sequences: None,
        }),
        metadata: None,
    };

    let (wire, losses) = encode_request(&request, &Config::default()).expect("encode request");

    assert_eq!(
        wire.input,
        Input::Items(vec![
            InputItem {
                kind: "function_call_output".to_string(),
                call_id: "call_1".to_string(),
                output: "Sunny".to_string(),
                ..InputItem::default()
            },
            InputItem {
                role: "user".to_string(),
                content: Some(ContentValue::Text("Use the tool result.".to_string())),
                ..InputItem::default()
            },
        ])
    );
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].reason, LossReason::Degraded);
    assert_eq!(losses[0].path, "messages[0].content");
}

#[test]
fn encodes_refusal_with_the_required_empty_error_message() {
    let response = IrResponse {
        id: "resp_1".to_string(),
        model: "gpt-4o-mini".to_string(),
        content: vec![Block::Text {
            text: "I cannot help with that.".to_string(),
        }],
        stop_reason: StopReason::Refusal,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 5,
            output_tokens: 6,
        },
    };

    let (wire, losses) = encode_response(&response, &Config::default()).expect("encode response");
    let rendered = serde_json::to_value(wire).expect("serialize response");

    assert!(losses.is_empty());
    assert_eq!(rendered["error"]["code"], "refusal");
    assert_eq!(rendered["error"]["message"], "");
}
