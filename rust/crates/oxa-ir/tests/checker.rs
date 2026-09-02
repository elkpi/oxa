//! Event-stream invariant tests: INV-5 grammar, INV-6 index discipline, and
//! the relational rule that a tool block's input equals the exact
//! concatenation of its fragments. Mirrors the coverage of the Go veccheck
//! regression table.

use oxa_ir::{
    Block, Delta, Event, EventStream, StopReason, Usage, validate_event_stream,
    validate_event_stream_for_encoder,
};

fn text_block_start(index: i64, text: &str) -> Event {
    Event::ContentBlockStart {
        index,
        block: Block::Text {
            text: text.to_string(),
        },
    }
}

fn text_delta(index: i64, text: &str) -> Event {
    Event::ContentBlockDelta {
        index,
        delta: Delta::TextDelta {
            text: text.to_string(),
        },
    }
}

fn tool_start(index: i64, input: &str) -> Event {
    Event::ContentBlockStart {
        index,
        block: Block::ToolUse {
            id: "call_1".to_string(),
            name: "get_weather".to_string(),
            input: input.to_string(),
        },
    }
}

fn input_delta(index: i64, fragment: &str) -> Event {
    Event::ContentBlockDelta {
        index,
        delta: Delta::InputJsonDelta {
            partial_json: fragment.to_string(),
        },
    }
}

fn block_stop(index: i64) -> Event {
    Event::ContentBlockStop { index }
}

fn message_delta(stop: StopReason) -> Event {
    Event::MessageDelta {
        stop_reason: stop,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 0,
            output_tokens: 0,
        },
    }
}

fn start() -> Event {
    Event::MessageStart {
        id: "m".to_string(),
        model: "model".to_string(),
    }
}

fn done() -> Event {
    Event::MessageDone {}
}

fn complete_text_stream() -> Vec<Event> {
    vec![
        start(),
        text_block_start(0, ""),
        text_delta(0, "hello"),
        block_stop(0),
        message_delta(StopReason::EndTurn),
        done(),
    ]
}

fn assert_rejects(events: Vec<Event>, event_index: usize, fragment: &str) {
    let stream = EventStream { events };
    let err = validate_event_stream(&stream).expect_err("stream must violate an invariant");
    assert_eq!(
        err.event, event_index,
        "violation location for {fragment:?}"
    );
    assert!(
        err.message.contains(fragment),
        "violation message {:?} must mention {fragment:?}",
        err.message
    );
}

#[test]
fn accepts_valid_text_and_tool_stream() {
    let events = vec![
        start(),
        text_block_start(0, ""),
        text_delta(0, "hello"),
        block_stop(0),
        tool_start(1, "{\"x\":1"),
        input_delta(1, ""),
        input_delta(1, "{\"x\":1"),
        block_stop(1),
        message_delta(StopReason::ToolUse),
        done(),
    ];
    validate_event_stream(&EventStream { events }).expect("valid stream");
}

#[test]
fn accepts_empty_tool_input_without_fragments() {
    let events = vec![
        start(),
        tool_start(0, ""),
        block_stop(0),
        message_delta(StopReason::ToolUse),
        done(),
    ];
    validate_event_stream(&EventStream { events }).expect("empty input needs no fragments");
}

#[test]
fn rejects_missing_message_start() {
    assert_rejects(
        vec![message_delta(StopReason::EndTurn), done()],
        0,
        "message_start",
    );
}

#[test]
fn rejects_second_open_block() {
    assert_rejects(
        vec![
            start(),
            text_block_start(0, ""),
            text_block_start(1, ""),
            block_stop(0),
            message_delta(StopReason::EndTurn),
            done(),
        ],
        2,
        "open block",
    );
}

#[test]
fn rejects_first_index_not_zero() {
    assert_rejects(
        vec![
            start(),
            text_block_start(1, ""),
            block_stop(1),
            message_delta(StopReason::EndTurn),
            done(),
        ],
        1,
        "index",
    );
}

#[test]
fn rejects_text_delta_on_tool_block() {
    assert_rejects(
        vec![
            start(),
            tool_start(0, "{}"),
            text_delta(0, "hello"),
            block_stop(0),
            message_delta(StopReason::ToolUse),
            done(),
        ],
        2,
        "delta type",
    );
}

#[test]
fn rejects_input_delta_on_text_block() {
    assert_rejects(
        vec![
            start(),
            text_block_start(0, ""),
            input_delta(0, "{}"),
            block_stop(0),
            message_delta(StopReason::EndTurn),
            done(),
        ],
        2,
        "delta type",
    );
}

#[test]
fn rejects_input_not_equal_to_fragment_concatenation() {
    assert_rejects(
        vec![
            start(),
            tool_start(0, "{\"x\":1}"),
            input_delta(0, "{\"x\":1"),
            input_delta(0, "\"}"),
            block_stop(0),
            message_delta(StopReason::ToolUse),
            done(),
        ],
        4,
        "concatenation",
    );
}

#[test]
fn rejects_delta_on_wrong_index() {
    assert_rejects(
        vec![
            start(),
            text_block_start(0, ""),
            text_delta(1, "hello"),
            block_stop(0),
            message_delta(StopReason::EndTurn),
            done(),
        ],
        2,
        "index",
    );
}

#[test]
fn rejects_stop_on_wrong_index() {
    assert_rejects(
        vec![
            start(),
            text_block_start(0, ""),
            block_stop(1),
            message_delta(StopReason::EndTurn),
            done(),
        ],
        2,
        "index",
    );
}

#[test]
fn rejects_message_done_without_preceding_delta() {
    assert_rejects(vec![start(), done()], 1, "message_delta");
}

#[test]
fn rejects_event_after_message_done() {
    assert_rejects(
        vec![start(), message_delta(StopReason::EndTurn), done(), done()],
        3,
        "after message_done",
    );
}

#[test]
fn rejects_missing_message_delta_at_eof() {
    assert_rejects(vec![start()], 1, "message_delta");
}

#[test]
fn rejects_missing_message_done_at_eof() {
    assert_rejects(
        vec![start(), message_delta(StopReason::EndTurn)],
        2,
        "message_done",
    );
}

#[test]
fn rejects_open_block_at_eof() {
    assert_rejects(vec![start(), text_block_start(0, "")], 2, "not stopped");
}

#[test]
fn rejects_stop_sequence_without_matching_reason() {
    let events = vec![
        start(),
        message_delta_with_sequence(StopReason::EndTurn, Some("END".to_string())),
        done(),
    ];
    let err = validate_event_stream(&EventStream { events })
        .expect_err("stop_sequence requires the stop_sequence reason");
    assert!(err.message.contains("stop_sequence"));
}

fn message_delta_with_sequence(stop: StopReason, seq: Option<String>) -> Event {
    Event::MessageDelta {
        stop_reason: stop,
        stop_sequence: seq,
        usage: Usage {
            input_tokens: 0,
            output_tokens: 0,
        },
    }
}

#[test]
fn accepts_stop_sequence_with_matching_reason() {
    let events = vec![
        start(),
        text_block_start(0, ""),
        block_stop(0),
        message_delta_with_sequence(StopReason::StopSequence, Some("END".to_string())),
        done(),
    ];
    validate_event_stream(&EventStream { events }).expect("conditional stop_sequence is valid");
}

#[test]
fn strict_check_rejects_synthesized_tool_input() {
    // Decoder output must carry explicit fragments; a tool block with a
    // non-empty input and no fragments is encoder shorthand only.
    let events = vec![
        start(),
        tool_start(0, "{\"x\":1e+01}"),
        block_stop(0),
        message_delta(StopReason::ToolUse),
        done(),
    ];
    let stream = EventStream { events };
    assert!(validate_event_stream(&stream).is_err(), "strict mode");
    validate_event_stream_for_encoder(&stream).expect("encoder shorthand accepted");
}

#[test]
fn complete_text_stream_passes() {
    validate_event_stream(&EventStream {
        events: complete_text_stream(),
    })
    .expect("reference stream");
}
