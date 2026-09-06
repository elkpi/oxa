use oxa_chatcompletions::{
    decode_request, ChoiceDelta, Chunk, Config, ContentValue, DeltaPayload, FunctionDelta,
    Message, Request, StreamDecoder, ToolCallDelta, ROLE_USER, TOOL_TYPE_FUNCTION,
};

fn chunk(delta: DeltaPayload, finish_reason: Option<&str>) -> Chunk {
    Chunk {
        id: "stream-1".to_string(),
        object: "chat.completion.chunk".to_string(),
        created: 0,
        model: "gpt-4o-mini".to_string(),
        choices: vec![ChoiceDelta {
            index: 0,
            delta,
            finish_reason: finish_reason.map(str::to_string),
        }],
        usage: None,
    }
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let request = Request {
        model: "gpt-4o-mini".to_string(),
        messages: vec![Message {
            role: ROLE_USER.to_string(),
            content: Some(ContentValue::Text("hello".to_string())),
            ..Default::default()
        }],
        ..Default::default()
    };
    let (decoded, losses) = decode_request(&request, &Config::default())?;
    assert_eq!(decoded.messages.len(), 1);
    assert!(losses.is_empty());

    let config = Config::default();
    let mut decoder = StreamDecoder::new(&config);
    let mut events = Vec::new();
    events.extend(decoder.feed(&chunk(
        DeltaPayload {
            role: "assistant".to_string(),
            ..Default::default()
        },
        None,
    ))?);
    events.extend(decoder.feed(&chunk(
        DeltaPayload {
            tool_calls: Some(vec![ToolCallDelta {
                index: 0,
                id: Some("call-1".to_string()),
                kind: Some(TOOL_TYPE_FUNCTION.to_string()),
                function: Some(FunctionDelta {
                    name: Some("lookup".to_string()),
                    arguments: Some("{\"q\"".to_string()),
                }),
            }]),
            ..Default::default()
        },
        None,
    ))?);
    events.extend(decoder.feed(&chunk(
        DeltaPayload {
            tool_calls: Some(vec![ToolCallDelta {
                index: 0,
                function: Some(FunctionDelta {
                    arguments: Some(":1}".to_string()),
                    ..Default::default()
                }),
                ..Default::default()
            }]),
            ..Default::default()
        },
        None,
    ))?);
    events.extend(decoder.feed(&chunk(
        DeltaPayload::default(),
        Some("tool_calls"),
    ))?);
    events.extend(decoder.flush()?);
    assert!(!events.is_empty());
    println!("rust consumer: OK");
    Ok(())
}
