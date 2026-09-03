//! Anthropic Messages streaming decoder: converts wire stream events into IR events (face → IR).

use std::collections::HashSet;

use oxa_ir::{Block, Delta, Event, Loss, LossReason, StopReason, Usage};

use crate::config::Config;
use crate::decode::decode_stop_reason;
use crate::error::Error;
use crate::normalize::loss;
use crate::types::StreamEvent;

/// Incrementally converts an Anthropic Messages event stream into IR events.
pub struct StreamDecoder {
    config: Config,
    losses: Vec<Loss>,
    started: bool,
    next_index: i64,
    next_ir_index: i64,
    open_index: i64,
    open_ir_index: i64,
    block_open: bool,
    open_tool: bool,
    tool_id: String,
    tool_name: String,
    tool_input: String,
    tool_parts: Vec<String>,
    skipped_open: bool,
    skipped: HashSet<i64>,
    delta_seen: bool,
    stop: Option<StopReason>,
    stop_seq: Option<String>,
    usage: Usage,
    stopped: bool,
    flushed: bool,
}

impl StreamDecoder {
    /// Creates a new decoder for an Anthropic Messages stream.
    pub fn new(config: &Config) -> Self {
        StreamDecoder {
            config: config.clone(),
            losses: Vec::new(),
            started: false,
            next_index: 0,
            next_ir_index: 0,
            open_index: 0,
            open_ir_index: 0,
            block_open: false,
            open_tool: false,
            tool_id: String::new(),
            tool_name: String::new(),
            tool_input: String::new(),
            tool_parts: Vec::new(),
            skipped_open: false,
            skipped: HashSet::new(),
            delta_seen: false,
            stop: None,
            stop_seq: None,
            usage: Usage {
                input_tokens: 0,
                output_tokens: 0,
            },
            stopped: false,
            flushed: false,
        }
    }

    /// Pushes one wire stream event and returns any completed IR events.
    pub fn feed(&mut self, ev: &StreamEvent) -> Result<Vec<Event>, Error> {
        if self.flushed {
            return Err(Error::new("anthropic: event fed after stream flush"));
        }
        if self.stopped {
            return Err(Error::new("anthropic: event fed after message_stop"));
        }

        match ev.kind.as_str() {
            "message_start" => {
                if self.started {
                    return Err(Error::new("anthropic: duplicate message_start"));
                }
                let Some(msg) = &ev.message else {
                    return Err(Error::new("anthropic: message_start without message"));
                };
                self.started = true;
                Ok(vec![Event::MessageStart {
                    id: msg.id.clone(),
                    model: self.config.map_model(&msg.model),
                }])
            }
            "content_block_start" => {
                if !self.started {
                    return Err(Error::new(
                        "anthropic: content_block_start before message_start",
                    ));
                }
                if self.block_open || self.skipped_open {
                    return Err(Error::new(
                        "anthropic: content_block_start with a block still open",
                    ));
                }
                let index = ev.index.unwrap_or(-1);
                if index != self.next_index {
                    return Err(Error::new(format!(
                        "anthropic: content_block_start index {}, want {}",
                        index, self.next_index
                    )));
                }
                let Some(block) = &ev.content_block else {
                    self.next_index += 1;
                    self.skipped.insert(index);
                    self.skipped_open = true;
                    self.losses.push(loss(
                        format!("content_block_start[{index}].content_block.type"),
                        "content_block.type",
                        LossReason::UnsupportedSemantic,
                        "Anthropic streaming block has no content_block payload; the index is skipped",
                    ));
                    return Ok(Vec::new());
                };
                if block.kind == "tool_use" {
                    if block.id.is_empty() {
                        return Err(Error::new(format!(
                            "anthropic: content_block_start[{index}].content_block.id is required"
                        )));
                    }
                    if block.name.is_empty() {
                        return Err(Error::new(format!(
                            "anthropic: content_block_start[{index}].content_block.name is required"
                        )));
                    }
                    self.next_index += 1;
                    self.block_open = true;
                    self.open_tool = true;
                    self.open_index = index;
                    self.open_ir_index = self.next_ir_index;
                    self.next_ir_index += 1;
                    self.tool_id = block.id.clone();
                    self.tool_name = block.name.clone();
                    self.tool_input = block
                        .input
                        .as_deref()
                        .map(serde_json::value::RawValue::get)
                        .unwrap_or("")
                        .to_string();
                    self.tool_parts.clear();
                    return Ok(Vec::new());
                }
                if block.kind != "text" {
                    self.next_index += 1;
                    self.skipped.insert(index);
                    self.skipped_open = true;
                    self.losses.push(loss(
                        format!("content_block_start[{index}].content_block.type"),
                        "content_block.type",
                        LossReason::UnsupportedSemantic,
                        format!(
                            "Anthropic streaming block type {:?} is not decodable in M7; the index is skipped",
                            block.kind
                        ),
                    ));
                    return Ok(Vec::new());
                }
                self.next_index += 1;
                self.block_open = true;
                self.open_tool = false;
                self.open_index = index;
                self.open_ir_index = self.next_ir_index;
                self.next_ir_index += 1;
                Ok(vec![Event::ContentBlockStart {
                    index: self.open_ir_index,
                    block: Block::Text {
                        text: block.text.clone(),
                    },
                }])
            }
            "content_block_delta" => {
                if !self.started {
                    return Err(Error::new(
                        "anthropic: content_block_delta before message_start",
                    ));
                }
                let Some(delta) = &ev.delta else {
                    return Err(Error::new("anthropic: content_block_delta without delta"));
                };
                let index = ev.index.unwrap_or(-1);
                if self.skipped.contains(&index) {
                    return Ok(Vec::new());
                }
                if !self.block_open || index != self.open_index {
                    return Err(Error::new(format!(
                        "anthropic: content_block_delta index {} does not match the open block",
                        index
                    )));
                }
                if self.open_tool {
                    match delta.kind.as_str() {
                        "text_delta" => {
                            return Err(Error::new("anthropic: text_delta on tool_use block"));
                        }
                        "input_json_delta" => {
                            self.tool_parts
                                .push(delta.partial_json.clone().unwrap_or_default());
                            return Ok(Vec::new());
                        }
                        _ => {}
                    }
                }
                match delta.kind.as_str() {
                    "text_delta" => Ok(vec![Event::ContentBlockDelta {
                        index: self.open_ir_index,
                        delta: Delta::TextDelta {
                            text: delta.text.clone(),
                        },
                    }]),
                    "input_json_delta" => {
                        Err(Error::new("anthropic: input_json_delta on non-tool block"))
                    }
                    other => {
                        self.losses.push(loss(
                            format!("content_block_delta[{index}].delta.type"),
                            "delta.type",
                            LossReason::UnsupportedSemantic,
                            format!("Anthropic delta type {other:?} has no IR equivalent"),
                        ));
                        Ok(Vec::new())
                    }
                }
            }
            "content_block_stop" => {
                if !self.started {
                    return Err(Error::new(
                        "anthropic: content_block_stop before message_start",
                    ));
                }
                let index = ev.index.unwrap_or(-1);
                if self.skipped.remove(&index) {
                    self.skipped_open = false;
                    return Ok(Vec::new());
                }
                if !self.block_open || index != self.open_index {
                    return Err(Error::new(format!(
                        "anthropic: content_block_stop index {} does not match the open block",
                        index
                    )));
                }
                if self.open_tool {
                    let (input, deltas) = if !self.tool_parts.is_empty() {
                        let full_input = self.tool_parts.concat();
                        let deltas = self
                            .tool_parts
                            .iter()
                            .map(|part| Event::ContentBlockDelta {
                                index: self.open_ir_index,
                                delta: Delta::InputJsonDelta {
                                    partial_json: part.clone(),
                                },
                            })
                            .collect::<Vec<_>>();
                        (full_input, deltas)
                    } else {
                        if self.tool_input.is_empty() {
                            return Err(Error::new("anthropic: tool_use input is required"));
                        }
                        let full_input = self.tool_input.clone();
                        let deltas = vec![Event::ContentBlockDelta {
                            index: self.open_ir_index,
                            delta: Delta::InputJsonDelta {
                                partial_json: full_input.clone(),
                            },
                        }];
                        (full_input, deltas)
                    };

                    let mut events = vec![Event::ContentBlockStart {
                        index: self.open_ir_index,
                        block: Block::ToolUse {
                            id: std::mem::take(&mut self.tool_id),
                            name: std::mem::take(&mut self.tool_name),
                            input,
                        },
                    }];
                    events.extend(deltas);
                    events.push(Event::ContentBlockStop {
                        index: self.open_ir_index,
                    });
                    self.block_open = false;
                    self.open_tool = false;
                    self.tool_input.clear();
                    self.tool_parts.clear();
                    return Ok(events);
                }
                self.block_open = false;
                Ok(vec![Event::ContentBlockStop {
                    index: self.open_ir_index,
                }])
            }
            "message_delta" => {
                if !self.started {
                    return Err(Error::new("anthropic: message_delta before message_start"));
                }
                if self.block_open || self.skipped_open {
                    return Err(Error::new(
                        "anthropic: message_delta with a block still open",
                    ));
                }
                let Some(delta) = &ev.delta else {
                    return Err(Error::new("anthropic: message_delta without delta"));
                };
                let (stop, stop_loss) =
                    decode_stop_reason(delta.stop_reason.as_deref().unwrap_or(""))?;
                if let Some(l) = stop_loss {
                    self.losses.push(l);
                }
                self.stop = Some(stop);
                self.stop_seq = delta.stop_sequence.clone();
                if let Some(usage) = &ev.usage {
                    self.usage = Usage {
                        input_tokens: usage.input_tokens,
                        output_tokens: usage.output_tokens,
                    };
                }
                self.delta_seen = true;
                Ok(Vec::new())
            }
            "message_stop" => {
                if !self.started {
                    return Err(Error::new("anthropic: message_stop before message_start"));
                }
                if self.block_open || self.skipped_open {
                    return Err(Error::new(
                        "anthropic: message_stop with a block still open",
                    ));
                }
                if !self.delta_seen {
                    return Err(Error::new(
                        "anthropic: message_stop without a preceding message_delta",
                    ));
                }
                self.stopped = true;
                let stop = self.stop.unwrap_or(StopReason::EndTurn);
                let stop_sequence = if stop == StopReason::StopSequence {
                    self.stop_seq.clone()
                } else {
                    None
                };
                Ok(vec![
                    Event::MessageDelta {
                        stop_reason: stop,
                        stop_sequence,
                        usage: self.usage,
                    },
                    Event::MessageDone {},
                ])
            }
            other => {
                self.losses.push(loss(
                    "type",
                    "type",
                    LossReason::UnsupportedSemantic,
                    format!(
                        "Anthropic stream event type {other:?} is not decoded in this milestone"
                    ),
                ));
                Ok(Vec::new())
            }
        }
    }

    /// Confirms that the stream completed normally with `message_stop`.
    pub fn flush(&mut self) -> Result<Vec<Event>, Error> {
        if self.flushed {
            return Err(Error::new("anthropic: stream flushed twice"));
        }
        if !self.stopped {
            return Err(Error::new("anthropic: stream ended without message_stop"));
        }
        self.flushed = true;
        Ok(Vec::new())
    }

    /// Returns the losses accumulated so far across the stream.
    pub fn losses(&self) -> &[Loss] {
        &self.losses
    }
}
