use oxa_anthropic::{
    BlockWire, Config, MessageStartWire, StreamDecoder, StreamDelta, StreamEncoder, StreamEvent,
    UsageWire,
};
use oxa_ir::{Block, Delta, Event, StopReason, Usage};

fn msg_start(id: &str, model: &str) -> StreamEvent {
    StreamEvent {
        kind: "message_start".to_string(),
        message: Some(MessageStartWire {
            id: id.to_string(),
            kind: "message".to_string(),
            role: "assistant".to_string(),
            model: model.to_string(),
            content: Vec::new(),
            stop_reason: None,
            usage: Some(UsageWire {
                input_tokens: 0,
                output_tokens: 0,
                ..UsageWire::default()
            }),
        }),
        ..Default::default()
    }
}

fn block_start(index: i64, kind: &str) -> StreamEvent {
    StreamEvent {
        kind: "content_block_start".to_string(),
        index: Some(index),
        content_block: Some(BlockWire {
            kind: kind.to_string(),
            ..Default::default()
        }),
        ..Default::default()
    }
}

fn text_delta(index: i64, text: &str) -> StreamEvent {
    StreamEvent {
        kind: "content_block_delta".to_string(),
        index: Some(index),
        delta: Some(StreamDelta {
            kind: "text_delta".to_string(),
            text: text.to_string(),
            ..Default::default()
        }),
        ..Default::default()
    }
}

fn block_stop(index: i64) -> StreamEvent {
    StreamEvent {
        kind: "content_block_stop".to_string(),
        index: Some(index),
        ..Default::default()
    }
}

fn msg_delta(stop: &str, seq: Option<&str>, in_tokens: i64, out_tokens: i64) -> StreamEvent {
    StreamEvent {
        kind: "message_delta".to_string(),
        delta: Some(StreamDelta {
            stop_reason: Some(stop.to_string()),
            stop_sequence: seq.map(String::from),
            ..Default::default()
        }),
        usage: Some(UsageWire {
            input_tokens: in_tokens,
            output_tokens: out_tokens,
            ..UsageWire::default()
        }),
        ..Default::default()
    }
}

fn msg_stop() -> StreamEvent {
    StreamEvent {
        kind: "message_stop".to_string(),
        ..Default::default()
    }
}

#[test]
fn stream_decoder_happy_path() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    let mut got = Vec::new();

    got.extend(d.feed(&msg_start("msg_1", "claude-3")).unwrap());
    got.extend(d.feed(&block_start(0, "text")).unwrap());
    got.extend(d.feed(&text_delta(0, "Hel")).unwrap());
    got.extend(d.feed(&text_delta(0, "lo")).unwrap());
    got.extend(d.feed(&block_stop(0)).unwrap());
    got.extend(d.feed(&msg_delta("end_turn", None, 3, 5)).unwrap());
    got.extend(d.feed(&msg_stop()).unwrap());
    got.extend(d.flush().unwrap());

    let want = vec![
        Event::MessageStart {
            id: "msg_1".to_string(),
            model: "claude-3".to_string(),
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
                ..Usage::default()
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

    // Flush without message_stop is an error
    let mut d = StreamDecoder::new(&config);
    d.feed(&msg_start("m", "c")).unwrap();
    assert!(d.flush().is_err());

    // Flush twice is an error
    d.feed(&msg_delta("end_turn", None, 0, 0)).unwrap();
    d.feed(&msg_stop()).unwrap();
    assert!(d.flush().is_ok());
    assert!(d.flush().is_err());

    // Duplicate message_start
    let mut d = StreamDecoder::new(&config);
    d.feed(&msg_start("m", "c")).unwrap();
    assert!(d.feed(&msg_start("m", "c")).is_err());

    // Block start before message_start
    let mut d = StreamDecoder::new(&config);
    assert!(d.feed(&block_start(0, "text")).is_err());

    // Block delta before block start
    let mut d = StreamDecoder::new(&config);
    d.feed(&msg_start("m", "c")).unwrap();
    assert!(d.feed(&text_delta(0, "x")).is_err());

    // Non-consecutive index
    let mut d = StreamDecoder::new(&config);
    d.feed(&msg_start("m", "c")).unwrap();
    assert!(d.feed(&block_start(1, "text")).is_err());
}

#[test]
fn stream_decoder_maps_thinking_signature_and_cache_usage() {
    let config = Config::default();
    let mut decoder = StreamDecoder::new(&config);
    let mut got = Vec::new();

    got.extend(decoder.feed(&msg_start("msg_2", "claude")).unwrap());
    let mut start = block_start(0, "thinking");
    start.content_block.as_mut().unwrap().thinking = Some(String::new());
    got.extend(decoder.feed(&start).unwrap());
    got.extend(
        decoder
            .feed(&StreamEvent {
                kind: "content_block_delta".to_string(),
                index: Some(0),
                delta: Some(StreamDelta {
                    kind: "thinking_delta".to_string(),
                    thinking: Some("consider".to_string()),
                    ..Default::default()
                }),
                ..Default::default()
            })
            .unwrap(),
    );
    got.extend(
        decoder
            .feed(&StreamEvent {
                kind: "content_block_delta".to_string(),
                index: Some(0),
                delta: Some(StreamDelta {
                    kind: "signature_delta".to_string(),
                    signature: Some("opaque-signature".to_string()),
                    ..Default::default()
                }),
                ..Default::default()
            })
            .unwrap(),
    );
    got.extend(decoder.feed(&block_stop(0)).unwrap());
    let mut terminal = msg_delta("end_turn", None, 10, 15);
    terminal.usage.as_mut().unwrap().cache_creation_input_tokens = Some(5);
    terminal.usage.as_mut().unwrap().cache_read_input_tokens = Some(20);
    got.extend(decoder.feed(&terminal).unwrap());
    got.extend(decoder.feed(&msg_stop()).unwrap());
    got.extend(decoder.flush().unwrap());

    assert_eq!(
        got,
        vec![
            Event::MessageStart {
                id: "msg_2".to_string(),
                model: "claude".to_string(),
            },
            Event::ContentBlockStart {
                index: 0,
                block: Block::Thinking {
                    thinking: String::new(),
                    signature: None,
                },
            },
            Event::ContentBlockDelta {
                index: 0,
                delta: Delta::ThinkingDelta {
                    text: "consider".to_string(),
                },
            },
            Event::ContentBlockDelta {
                index: 0,
                delta: Delta::SignatureDelta {
                    signature: "opaque-signature".to_string(),
                },
            },
            Event::ContentBlockStop { index: 0 },
            Event::MessageDelta {
                stop_reason: StopReason::EndTurn,
                stop_sequence: None,
                usage: Usage {
                    input_tokens: 10,
                    output_tokens: 15,
                    cache_creation_input_tokens: Some(5),
                    cache_read_input_tokens: Some(20),
                    ..Usage::default()
                },
            },
            Event::MessageDone {},
        ]
    );
    assert!(decoder.losses().is_empty());
}

#[test]
fn stream_encoder_synthesizes_thinking_and_signature_on_block_stop() {
    let config = Config::default();
    let mut encoder = StreamEncoder::new(&config);
    encoder
        .apply(&Event::MessageStart {
            id: "msg_3".to_string(),
            model: "claude".to_string(),
        })
        .unwrap();

    let (start, losses) = encoder
        .apply(&Event::ContentBlockStart {
            index: 0,
            block: Block::Thinking {
                thinking: "consider".to_string(),
                signature: Some("opaque-signature".to_string()),
            },
        })
        .unwrap();
    assert!(losses.is_empty());
    assert_eq!(start[0].content_block.as_ref().unwrap().kind, "thinking");
    assert_eq!(
        start[0].content_block.as_ref().unwrap().thinking.as_deref(),
        None
    );
    assert_eq!(start[0].content_block.as_ref().unwrap().signature, None);

    let (stop, losses) = encoder
        .apply(&Event::ContentBlockStop { index: 0 })
        .unwrap();
    assert!(losses.is_empty());
    assert_eq!(stop.len(), 3);
    assert_eq!(stop[0].delta.as_ref().unwrap().kind, "thinking_delta");
    assert_eq!(
        stop[0].delta.as_ref().unwrap().thinking.as_deref(),
        Some("consider")
    );
    assert_eq!(stop[1].delta.as_ref().unwrap().kind, "signature_delta");
    assert_eq!(
        stop[1].delta.as_ref().unwrap().signature.as_deref(),
        Some("opaque-signature")
    );
    assert_eq!(stop[2].kind, "content_block_stop");
}

#[test]
fn stream_encoder_forwards_thinking_deltas_and_terminal_cache_usage() {
    let config = Config::default();
    let mut encoder = StreamEncoder::new(&config);
    encoder
        .apply(&Event::MessageStart {
            id: "msg_4".to_string(),
            model: "claude".to_string(),
        })
        .unwrap();
    encoder
        .apply(&Event::ContentBlockStart {
            index: 0,
            block: Block::Thinking {
                thinking: String::new(),
                signature: None,
            },
        })
        .unwrap();
    let (thinking, _) = encoder
        .apply(&Event::ContentBlockDelta {
            index: 0,
            delta: Delta::ThinkingDelta {
                text: "consider".to_string(),
            },
        })
        .unwrap();
    assert_eq!(
        thinking[0].delta.as_ref().unwrap().thinking.as_deref(),
        Some("consider")
    );
    let (signature, _) = encoder
        .apply(&Event::ContentBlockDelta {
            index: 0,
            delta: Delta::SignatureDelta {
                signature: "opaque-signature".to_string(),
            },
        })
        .unwrap();
    assert_eq!(
        signature[0].delta.as_ref().unwrap().signature.as_deref(),
        Some("opaque-signature")
    );
    let (stop, _) = encoder
        .apply(&Event::ContentBlockStop { index: 0 })
        .unwrap();
    assert_eq!(stop.len(), 1);

    let (terminal, _) = encoder
        .apply(&Event::MessageDelta {
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 10,
                output_tokens: 15,
                cache_creation_input_tokens: Some(5),
                cache_read_input_tokens: Some(20),
                ..Usage::default()
            },
        })
        .unwrap();
    let usage = terminal[0].usage.as_ref().unwrap();
    assert_eq!(usage.cache_creation_input_tokens, Some(5));
    assert_eq!(usage.cache_read_input_tokens, Some(20));
}

#[test]
fn stream_encoder_rejects_thinking_delta_after_signature_delta() {
    let config = Config::default();
    let mut encoder = StreamEncoder::new(&config);
    encoder
        .apply(&Event::MessageStart {
            id: "msg_5".to_string(),
            model: "claude".to_string(),
        })
        .unwrap();
    encoder
        .apply(&Event::ContentBlockStart {
            index: 0,
            block: Block::Thinking {
                thinking: String::new(),
                signature: None,
            },
        })
        .unwrap();
    encoder
        .apply(&Event::ContentBlockDelta {
            index: 0,
            delta: Delta::SignatureDelta {
                signature: "sig".to_string(),
            },
        })
        .unwrap();
    let error = encoder
        .apply(&Event::ContentBlockDelta {
            index: 0,
            delta: Delta::ThinkingDelta {
                text: "late".to_string(),
            },
        })
        .expect_err("thinking cannot follow signature");
    assert!(
        error.to_string().contains("after signature_delta"),
        "{error}"
    );
}

#[test]
fn stream_encoder_happy_path() {
    let config = Config::default();
    let mut e = StreamEncoder::new(&config);

    let (evs, _) = e
        .apply(&Event::MessageStart {
            id: "msg_1".to_string(),
            model: "claude-3".to_string(),
        })
        .unwrap();
    assert_eq!(evs.len(), 1);
    assert_eq!(evs[0].kind, "message_start");

    let (evs, _) = e
        .apply(&Event::ContentBlockStart {
            index: 0,
            block: Block::Text {
                text: String::new(),
            },
        })
        .unwrap();
    assert_eq!(evs.len(), 1);
    assert_eq!(evs[0].kind, "content_block_start");

    let (evs, _) = e
        .apply(&Event::ContentBlockDelta {
            index: 0,
            delta: Delta::TextDelta {
                text: "Hi".to_string(),
            },
        })
        .unwrap();
    assert_eq!(evs.len(), 1);
    assert_eq!(evs[0].kind, "content_block_delta");

    let (evs, _) = e.apply(&Event::ContentBlockStop { index: 0 }).unwrap();
    assert_eq!(evs.len(), 1);
    assert_eq!(evs[0].kind, "content_block_stop");

    let (evs, _) = e
        .apply(&Event::MessageDelta {
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 3,
                output_tokens: 5,
                ..Usage::default()
            },
        })
        .unwrap();
    assert_eq!(evs.len(), 1);
    assert_eq!(evs[0].kind, "message_delta");

    let (evs, _) = e.apply(&Event::MessageDone {}).unwrap();
    assert_eq!(evs.len(), 1);
    assert_eq!(evs[0].kind, "message_stop");
}
