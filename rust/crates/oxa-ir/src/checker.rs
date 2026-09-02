//! Event-stream invariant checking (spec/01 §7): INV-5 grammar, INV-6 index
//! discipline, and the relational rule that a tool block's `input` equals
//! the exact concatenation of its `input_json_delta.partial_json` fragments
//! (INV-1: compared as opaque strings, never parsed).

use crate::event::{Delta, Event, EventStream};
use crate::request::Block;
use crate::response::StopReason;

/// A rule violation found by the event-stream validators.
#[derive(Clone, Debug, PartialEq)]
pub struct Violation {
    /// Index of the offending event (or the stream length for an EOF
    /// violation such as a missing terminal event).
    pub event: usize,
    pub message: String,
}

fn violate(event: usize, message: impl Into<String>) -> Violation {
    Violation {
        event,
        message: message.into(),
    }
}

/// Validates a decoder-produced stream (strict): a tool block with a
/// non-empty input must carry explicit input fragments.
pub fn validate_event_stream(stream: &EventStream) -> Result<(), Violation> {
    validate_with(stream, false)
}

/// Validates an encoder-input stream (lenient): accepts the documented
/// encoder shorthand where a tool block carries its full input and no
/// fragments (the encoder synthesizes one full delta).
pub fn validate_event_stream_for_encoder(stream: &EventStream) -> Result<(), Violation> {
    validate_with(stream, true)
}

struct OpenTool {
    input: String,
    fragments: String,
    fragment_count: usize,
}

enum OpenBlock {
    Text,
    Tool(OpenTool),
}

#[derive(Clone, Copy, PartialEq)]
enum Phase {
    NeedStart,
    Blocks,
    NeedMessageDone,
    Done,
}

fn validate_with(stream: &EventStream, allow_synthesized: bool) -> Result<(), Violation> {
    let mut phase = Phase::NeedStart;
    let mut next_index: i64 = 0;
    // The currently open block: (index, kind). INV-5 allows at most one.
    let mut open: Option<(i64, OpenBlock)> = None;

    for (i, event) in stream.events.iter().enumerate() {
        match event {
            Event::MessageStart { .. } => {
                if phase != Phase::NeedStart {
                    return Err(violate(i, "duplicate message_start"));
                }
                phase = Phase::Blocks;
            }
            Event::ContentBlockStart { index, block } => {
                if phase != Phase::Blocks {
                    return Err(violate(
                        i,
                        "content_block_start outside the block region (no message_start, or a message_delta already seen)",
                    ));
                }
                if open.is_some() {
                    return Err(violate(i, "content_block_start with an open block"));
                }
                if *index != next_index {
                    return Err(violate(
                        i,
                        format!("content_block_start index {index}, want {next_index}"),
                    ));
                }
                next_index += 1;
                open = Some(match block {
                    Block::Text { .. } => (*index, OpenBlock::Text),
                    Block::ToolUse { input, .. } => (
                        *index,
                        OpenBlock::Tool(OpenTool {
                            input: input.clone(),
                            fragments: String::new(),
                            fragment_count: 0,
                        }),
                    ),
                    other => {
                        return Err(violate(
                            i,
                            format!(
                                "content_block_start carries {} block; streams carry text and tool_use only",
                                block_kind(other)
                            ),
                        ));
                    }
                });
            }
            Event::ContentBlockDelta { index, delta } => {
                let Some((open_index, kind)) = open.as_mut() else {
                    return Err(violate(i, "content_block_delta without an open block"));
                };
                if *index != *open_index {
                    return Err(violate(
                        i,
                        format!(
                            "content_block_delta index {index} does not match the open block {open_index}"
                        ),
                    ));
                }
                match (kind, delta) {
                    (OpenBlock::Text, Delta::TextDelta { .. }) => {}
                    (OpenBlock::Tool(tool), Delta::InputJsonDelta { partial_json }) => {
                        tool.fragments.push_str(partial_json);
                        tool.fragment_count += 1;
                    }
                    _ => {
                        return Err(violate(
                            i,
                            format!(
                                "delta type {} does not match the open block kind",
                                delta_kind(delta)
                            ),
                        ));
                    }
                }
            }
            Event::ContentBlockStop { index } => {
                let Some((open_index, kind)) = open.take() else {
                    return Err(violate(i, "content_block_stop without an open block"));
                };
                if *index != open_index {
                    return Err(violate(
                        i,
                        format!(
                            "content_block_stop index {index} does not match the open block {open_index}"
                        ),
                    ));
                }
                if let OpenBlock::Tool(tool) = kind {
                    validate_tool_input(i, &tool, allow_synthesized)?;
                }
            }
            Event::MessageDelta {
                stop_reason,
                stop_sequence,
                ..
            } => {
                if phase != Phase::Blocks {
                    return Err(violate(
                        i,
                        "message_delta outside the block region (missing message_start or out of grammar order)",
                    ));
                }
                if open.is_some() {
                    return Err(violate(i, "message_delta with an open block"));
                }
                if stop_sequence.is_some() && *stop_reason != StopReason::StopSequence {
                    return Err(violate(
                        i,
                        "stop_sequence is only permitted when stop_reason is stop_sequence",
                    ));
                }
                phase = Phase::NeedMessageDone;
            }
            Event::MessageDone {} => {
                if phase == Phase::Done {
                    return Err(violate(i, "event after message_done"));
                }
                if phase != Phase::NeedMessageDone {
                    return Err(violate(
                        i,
                        "message_done without an immediately preceding message_delta",
                    ));
                }
                phase = Phase::Done;
            }
        }
    }

    if phase == Phase::Done {
        return Ok(());
    }
    if let Some((index, _)) = open {
        return Err(violate(
            stream.events.len(),
            format!("block index {index} is not stopped"),
        ));
    }
    if phase == Phase::NeedMessageDone {
        return Err(violate(
            stream.events.len(),
            format!(
                "events: missing message_done (stream ends after {} events)",
                stream.events.len()
            ),
        ));
    }
    Err(violate(
        stream.events.len(),
        "events: missing message_delta",
    ))
}

fn validate_tool_input(
    i: usize,
    tool: &OpenTool,
    allow_synthesized: bool,
) -> Result<(), Violation> {
    if tool.fragment_count == 0 {
        if tool.input.is_empty() || allow_synthesized {
            return Ok(());
        }
        return Err(violate(
            i,
            "tool block input without input_json_delta fragments; only encoder shorthand may synthesize them",
        ));
    }
    if tool.input != tool.fragments {
        return Err(violate(
            i,
            format!(
                "tool block input does not equal the concatenation of its {} fragments (INV-1 exact text)",
                tool.fragment_count
            ),
        ));
    }
    Ok(())
}

fn block_kind(block: &Block) -> &'static str {
    match block {
        Block::Text { .. } => "text",
        Block::Image { .. } => "image",
        Block::ToolUse { .. } => "tool_use",
        Block::ToolResult { .. } => "tool_result",
    }
}

fn delta_kind(delta: &Delta) -> &'static str {
    match delta {
        Delta::TextDelta { .. } => "text_delta",
        Delta::InputJsonDelta { .. } => "input_json_delta",
    }
}
