use oxa_ir::{Block, Delta, Event, StopReason, Usage};
use oxa_responses::{
    Config, OutputItem, OutputPart, Response, StreamDecoder, StreamEncoder, StreamEvent, UsageWire,
};

fn stream_created(id: &str, model: &str) -> StreamEvent {
    StreamEvent {
        kind: "response.created".to_string(),
        response: Some(Response {
            id: id.to_string(),
            object: "response".to_string(),
            status: "in_progress".to_string(),
            model: model.to_string(),
            output: Vec::new(),
            ..Default::default()
        }),
        ..Default::default()
    }
}

fn stream_item_added(output_index: i64, item_id: &str) -> StreamEvent {
    StreamEvent {
        kind: "response.output_item.added".to_string(),
        output_index: Some(output_index),
        item: Some(OutputItem {
            id: item_id.to_string(),
            kind: "message".to_string(),
            status: "in_progress".to_string(),
            role: "assistant".to_string(),
            content: Vec::new(),
            ..Default::default()
        }),
        ..Default::default()
    }
}

fn stream_part_added(output_index: i64, content_index: i64, item_id: &str) -> StreamEvent {
    StreamEvent {
        kind: "response.content_part.added".to_string(),
        item_id: Some(item_id.to_string()),
        output_index: Some(output_index),
        content_index: Some(content_index),
        part: Some(OutputPart {
            kind: "output_text".to_string(),
            text: String::new(),
            annotations: Vec::new(),
        }),
        ..Default::default()
    }
}

fn stream_text_delta(
    output_index: i64,
    content_index: i64,
    item_id: &str,
    delta: &str,
) -> StreamEvent {
    StreamEvent {
        kind: "response.output_text.delta".to_string(),
        item_id: Some(item_id.to_string()),
        output_index: Some(output_index),
        content_index: Some(content_index),
        delta: Some(delta.to_string()),
        ..Default::default()
    }
}

fn stream_text_done(
    output_index: i64,
    content_index: i64,
    item_id: &str,
    text: &str,
) -> StreamEvent {
    StreamEvent {
        kind: "response.output_text.done".to_string(),
        item_id: Some(item_id.to_string()),
        output_index: Some(output_index),
        content_index: Some(content_index),
        text: Some(text.to_string()),
        ..Default::default()
    }
}

fn stream_part_done(
    output_index: i64,
    content_index: i64,
    item_id: &str,
    text: &str,
) -> StreamEvent {
    StreamEvent {
        kind: "response.content_part.done".to_string(),
        item_id: Some(item_id.to_string()),
        output_index: Some(output_index),
        content_index: Some(content_index),
        part: Some(OutputPart {
            kind: "output_text".to_string(),
            text: text.to_string(),
            annotations: Vec::new(),
        }),
        ..Default::default()
    }
}

fn stream_item_done(output_index: i64, item_id: &str) -> StreamEvent {
    StreamEvent {
        kind: "response.output_item.done".to_string(),
        output_index: Some(output_index),
        item: Some(OutputItem {
            id: item_id.to_string(),
            kind: "message".to_string(),
            status: "completed".to_string(),
            role: "assistant".to_string(),
            content: Vec::new(),
            ..Default::default()
        }),
        ..Default::default()
    }
}

fn stream_image_part_added(output_index: i64, content_index: i64, item_id: &str) -> StreamEvent {
    StreamEvent {
        kind: "response.content_part.added".to_string(),
        item_id: Some(item_id.to_string()),
        output_index: Some(output_index),
        content_index: Some(content_index),
        part: Some(OutputPart {
            kind: "output_image".to_string(),
            text: String::new(),
            annotations: Vec::new(),
        }),
        ..Default::default()
    }
}

fn unknown_descendant(output_index: i64, content_index: i64, item_id: &str) -> StreamEvent {
    StreamEvent {
        kind: "response.output_image.delta".to_string(),
        item_id: Some(item_id.to_string()),
        output_index: Some(output_index),
        content_index: Some(content_index),
        ..Default::default()
    }
}

#[test]
fn unknown_descendant_of_skipped_part_is_absorbed() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("resp_img", "gpt-4o-mini")).unwrap();
    d.feed(&stream_item_added(0, "msg_img")).unwrap();
    assert!(
        d.feed(&stream_image_part_added(0, 0, "msg_img"))
            .unwrap()
            .is_empty()
    );
    assert!(
        d.feed(&unknown_descendant(0, 0, "msg_img"))
            .unwrap()
            .is_empty()
    );
    assert_eq!(d.losses().len(), 1);
    assert_eq!(d.losses()[0].path, "output[0].content[0]");
}

#[test]
fn unknown_descendant_with_wrong_item_is_error() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("resp_img", "gpt-4o-mini")).unwrap();
    d.feed(&stream_item_added(0, "msg_img")).unwrap();
    d.feed(&stream_image_part_added(0, 0, "msg_img")).unwrap();
    let err = d.feed(&unknown_descendant(0, 0, "other")).unwrap_err();
    assert!(err.to_string().contains("does not match"));
    assert_eq!(d.losses().len(), 1);
}

#[test]
fn identity_less_unknown_event_records_one_loss() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("resp_w", "gpt-4o-mini")).unwrap();
    let weird = StreamEvent {
        kind: "response.weird_event".to_string(),
        ..Default::default()
    };
    assert!(d.feed(&weird).unwrap().is_empty());
    assert_eq!(d.losses().len(), 1);
    assert_eq!(d.losses()[0].path, "type");
}

fn stream_completed(id: &str, model: &str, in_tokens: i64, out_tokens: i64) -> StreamEvent {
    StreamEvent {
        kind: "response.completed".to_string(),
        response: Some(Response {
            id: id.to_string(),
            object: "response".to_string(),
            status: "completed".to_string(),
            model: model.to_string(),
            usage: Some(UsageWire {
                input_tokens: in_tokens,
                output_tokens: out_tokens,
                total_tokens: in_tokens + out_tokens,
            }),
            ..Default::default()
        }),
        ..Default::default()
    }
}

#[test]
fn stream_decoder_happy_path() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    let mut got = Vec::new();

    got.extend(d.feed(&stream_created("resp_1", "gpt-4o-mini")).unwrap());
    got.extend(d.feed(&stream_item_added(0, "msg_1")).unwrap());
    got.extend(d.feed(&stream_part_added(0, 0, "msg_1")).unwrap());
    got.extend(d.feed(&stream_text_delta(0, 0, "msg_1", "Hel")).unwrap());
    got.extend(d.feed(&stream_text_delta(0, 0, "msg_1", "lo")).unwrap());
    got.extend(d.feed(&stream_text_done(0, 0, "msg_1", "Hello")).unwrap());
    got.extend(d.feed(&stream_part_done(0, 0, "msg_1", "Hello")).unwrap());
    got.extend(d.feed(&stream_item_done(0, "msg_1")).unwrap());
    got.extend(
        d.feed(&stream_completed("resp_1", "gpt-4o-mini", 3, 5))
            .unwrap(),
    );
    got.extend(d.flush().unwrap());

    let want = vec![
        Event::MessageStart {
            id: "resp_1".to_string(),
            model: "gpt-4o-mini".to_string(),
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

    // Flush without completed event is an error
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("r", "m")).unwrap();
    assert!(d.flush().is_err());

    // Flush twice is an error
    d.feed(&stream_completed("r", "m", 0, 0)).unwrap();
    assert!(d.flush().is_ok());
    assert!(d.flush().is_err());

    // Duplicate created is an error
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("r", "m")).unwrap();
    assert!(d.feed(&stream_created("r", "m")).is_err());

    // Item added before created is an error
    let mut d = StreamDecoder::new(&config);
    assert!(d.feed(&stream_item_added(0, "m")).is_err());

    // Non-consecutive output_index
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("r", "m")).unwrap();
    assert!(d.feed(&stream_item_added(1, "m")).is_err());
}

#[test]
fn stream_encoder_happy_path() {
    let config = Config::default();
    let mut e = StreamEncoder::new(&config);

    let (evs, _) = e
        .apply(&Event::MessageStart {
            id: "resp_1".to_string(),
            model: "gpt-4o-mini".to_string(),
        })
        .unwrap();
    assert_eq!(evs.len(), 1);
    assert_eq!(evs[0].kind, "response.created");

    let (evs, _) = e
        .apply(&Event::ContentBlockStart {
            index: 0,
            block: Block::Text {
                text: String::new(),
            },
        })
        .unwrap();
    assert_eq!(evs.len(), 2);
    assert_eq!(evs[0].kind, "response.output_item.added");
    assert_eq!(evs[1].kind, "response.content_part.added");

    let (evs, _) = e
        .apply(&Event::ContentBlockDelta {
            index: 0,
            delta: Delta::TextDelta {
                text: "Hi".to_string(),
            },
        })
        .unwrap();
    assert_eq!(evs.len(), 1);
    assert_eq!(evs[0].kind, "response.output_text.delta");

    let (evs, _) = e.apply(&Event::ContentBlockStop { index: 0 }).unwrap();
    assert_eq!(evs.len(), 2);
    assert_eq!(evs[0].kind, "response.output_text.done");
    assert_eq!(evs[1].kind, "response.content_part.done");

    let (evs, _) = e
        .apply(&Event::MessageDelta {
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 10,
                output_tokens: 2,
            },
        })
        .unwrap();
    assert_eq!(evs.len(), 2);
    assert_eq!(evs[0].kind, "response.output_item.done");
    assert_eq!(evs[1].kind, "response.completed");

    let (evs, _) = e.apply(&Event::MessageDone {}).unwrap();
    assert!(evs.is_empty());
}
