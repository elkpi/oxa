use oxa_chatcompletions::{
    ChoiceDelta, Chunk, Config, DeltaPayload, StreamDecoder, StreamEncoder, ToolCallDelta,
    UsageWire,
};
use oxa_ir::{Block, Delta, Event, LossReason, StopReason, Usage};

fn chunk_role(id: &str, model: &str) -> Chunk {
    Chunk {
        id: id.to_string(),
        object: "chat.completion.chunk".to_string(),
        model: model.to_string(),
        choices: vec![ChoiceDelta {
            index: 0,
            delta: DeltaPayload {
                role: "assistant".to_string(),
                ..Default::default()
            },
            finish_reason: None,
        }],
        ..Default::default()
    }
}

fn chunk_content(s: &str) -> Chunk {
    Chunk {
        object: "chat.completion.chunk".to_string(),
        choices: vec![ChoiceDelta {
            index: 0,
            delta: DeltaPayload {
                content: Some(s.to_string()),
                ..Default::default()
            },
            finish_reason: None,
        }],
        ..Default::default()
    }
}

fn chunk_finish(reason: &str) -> Chunk {
    Chunk {
        object: "chat.completion.chunk".to_string(),
        choices: vec![ChoiceDelta {
            index: 0,
            delta: DeltaPayload::default(),
            finish_reason: Some(reason.to_string()),
        }],
        ..Default::default()
    }
}

fn chunk_usage() -> Chunk {
    Chunk {
        object: "chat.completion.chunk".to_string(),
        choices: Vec::new(),
        usage: Some(UsageWire {
            prompt_tokens: 3,
            completion_tokens: 5,
            total_tokens: 8,
        }),
        ..Default::default()
    }
}

#[test]
fn stream_decoder_happy_path() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    let mut got = Vec::new();

    got.extend(d.feed(&chunk_role("chatcmpl-1", "gpt-4o")).unwrap());
    got.extend(d.feed(&chunk_content("Hel")).unwrap());
    got.extend(d.feed(&chunk_content("lo")).unwrap());
    got.extend(d.feed(&chunk_finish("stop")).unwrap());
    got.extend(d.feed(&chunk_usage()).unwrap());
    got.extend(d.flush().unwrap());

    let want = vec![
        Event::MessageStart {
            id: "chatcmpl-1".to_string(),
            model: "gpt-4o".to_string(),
        },
        Event::ContentBlockStart {
            index: 0,
            block: Block::Text {
                text: String::new(),
            },
        },
        Event::ContentBlockDelta {
            index: 0,
            delta: Delta::TextDelta {
                text: "Hel".to_string(),
            },
        },
        Event::ContentBlockDelta {
            index: 0,
            delta: Delta::TextDelta {
                text: "lo".to_string(),
            },
        },
        Event::ContentBlockStop { index: 0 },
        Event::MessageDelta {
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 3,
                output_tokens: 5,
            },
        },
        Event::MessageDone {},
    ];

    assert_eq!(got, want);
    assert!(d.losses().is_empty());
}

#[test]
fn stream_decoder_errors() {
    let config = Config::default();

    // Flush without finish_reason is an error
    let mut d = StreamDecoder::new(&config);
    d.feed(&chunk_role("c1", "m")).unwrap();
    assert!(d.flush().is_err());

    // Flush twice is an error
    let mut d = StreamDecoder::new(&config);
    d.feed(&chunk_finish("stop")).unwrap();
    assert!(d.flush().is_ok());
    assert!(d.flush().is_err());

    // Feed after flush is an error
    assert!(d.feed(&chunk_content("x")).is_err());

    // Duplicate finish_reason is an error
    let mut d = StreamDecoder::new(&config);
    d.feed(&chunk_finish("stop")).unwrap();
    assert!(d.feed(&chunk_finish("stop")).is_err());

    // Chunk stream already started with role
    let mut d = StreamDecoder::new(&config);
    d.feed(&chunk_role("c1", "m")).unwrap();
    assert!(d.feed(&chunk_role("c1", "m")).is_err());

    // Tool call non-consecutive index
    let mut d = StreamDecoder::new(&config);
    let bad_call = Chunk {
        choices: vec![ChoiceDelta {
            index: 0,
            delta: DeltaPayload {
                tool_calls: Some(vec![ToolCallDelta {
                    index: 5, // skip index 0
                    id: Some("call_1".to_string()),
                    kind: Some("function".to_string()),
                    function: None,
                }]),
                ..Default::default()
            },
            finish_reason: None,
        }],
        ..Default::default()
    };
    assert!(d.feed(&bad_call).is_err());
}

#[test]
fn stream_encoder_happy_path() {
    let config = Config::default();
    let mut e = StreamEncoder::new(&config);

    let (c1, l1) = e
        .apply(&Event::MessageStart {
            id: "chatcmpl-1".to_string(),
            model: "gpt-4o".to_string(),
        })
        .unwrap();
    assert_eq!(c1.len(), 1);
    assert_eq!(c1[0].choices[0].delta.role, "assistant");
    assert!(l1.is_empty());

    let (c2, _) = e
        .apply(&Event::ContentBlockStart {
            index: 0,
            block: Block::Text {
                text: String::new(),
            },
        })
        .unwrap();
    assert!(c2.is_empty());

    let (c3, _) = e
        .apply(&Event::ContentBlockDelta {
            index: 0,
            delta: Delta::TextDelta {
                text: "Hi".to_string(),
            },
        })
        .unwrap();
    assert_eq!(c3.len(), 1);
    assert_eq!(c3[0].choices[0].delta.content.as_deref(), Some("Hi"));

    let (c4, _) = e.apply(&Event::ContentBlockStop { index: 0 }).unwrap();
    assert!(c4.is_empty());

    let (c5, l5) = e
        .apply(&Event::MessageDelta {
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 10,
                output_tokens: 2,
            },
        })
        .unwrap();
    assert_eq!(c5.len(), 1);
    assert_eq!(c5[0].choices[0].finish_reason.as_deref(), Some("stop"));
    assert_eq!(c5[0].usage.as_ref().map(|u| u.prompt_tokens), Some(10));
    assert!(l5.is_empty());

    let (c6, _) = e.apply(&Event::MessageDone {}).unwrap();
    assert!(c6.is_empty());
}

#[test]
fn stream_encoder_ordering_degrade_loss() {
    let config = Config::default();
    let mut e = StreamEncoder::new(&config);

    e.apply(&Event::MessageStart {
        id: "c1".to_string(),
        model: "m".to_string(),
    })
    .unwrap();

    // Tool block first
    e.apply(&Event::ContentBlockStart {
        index: 0,
        block: Block::ToolUse {
            id: "call_1".to_string(),
            name: "weather".to_string(),
            input: "{}".to_string(),
        },
    })
    .unwrap();
    e.apply(&Event::ContentBlockStop { index: 0 }).unwrap();

    // Text block after tool block -> ordering degradation
    e.apply(&Event::ContentBlockStart {
        index: 1,
        block: Block::Text {
            text: String::new(),
        },
    })
    .unwrap();
    e.apply(&Event::ContentBlockStop { index: 1 }).unwrap();

    let (_, losses) = e
        .apply(&Event::MessageDelta {
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 0,
                output_tokens: 0,
            },
        })
        .unwrap();
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].reason, LossReason::Degraded);
}
