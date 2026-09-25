//! Chat Completions streaming decoder: converts chunks into IR events (face → IR).

use oxa_ir::{Block, Delta, Event, Loss, LossReason, StopReason, Usage};

use crate::config::Config;
use crate::decode::{decode_finish_reason, decode_usage};
use crate::error::Error;
use crate::normalize::loss;
use crate::types::{Chunk, TOOL_TYPE_FUNCTION, ToolCallDelta};

#[derive(Default)]
struct StreamToolCall {
    index: usize,
    id: String,
    name: String,
    fragments: Vec<String>,
    skipped: bool,
}

/// Incrementally converts a Chat Completions chunk stream into IR events.
pub struct StreamDecoder {
    config: Config,
    losses: Vec<Loss>,
    started: bool,
    text_open: bool,
    text_index: i64,
    thinking_open: bool,
    thinking_index: i64,
    next_ir_index: i64,
    id: String,
    model: String,
    finish_seen: bool,
    stop: Option<StopReason>,
    usage: Option<Usage>,
    flushed: bool,
    tool_calls: Vec<StreamToolCall>,
}

impl StreamDecoder {
    /// Creates a new decoder for a chunk stream.
    pub fn new(config: &Config) -> Self {
        StreamDecoder {
            config: config.clone(),
            losses: Vec::new(),
            started: false,
            text_open: false,
            text_index: 0,
            thinking_open: false,
            thinking_index: 0,
            next_ir_index: 0,
            id: String::new(),
            model: String::new(),
            finish_seen: false,
            stop: None,
            usage: None,
            flushed: false,
            tool_calls: Vec::new(),
        }
    }

    /// Pushes one wire chunk and returns any IR events it completes.
    pub fn feed(&mut self, chunk: &Chunk) -> Result<Vec<Event>, Error> {
        if self.flushed {
            return Err(Error::new("chatcompletions: chunk fed after stream flush"));
        }
        if let Some(usage) = &chunk.usage {
            self.usage = Some(decode_usage(Some(usage))?);
        }
        if chunk.choices.is_empty() {
            return Ok(Vec::new());
        }

        let choice = &chunk.choices[0];
        if self.started && !choice.delta.role.is_empty() {
            if self.finish_seen {
                return Err(Error::new(
                    "chatcompletions: chunk stream restarted after finish_reason",
                ));
            }
            return Err(Error::new("chatcompletions: chunk stream already started"));
        }

        let mut events = Vec::new();
        if !self.started {
            self.started = true;
            self.id = chunk.id.clone();
            self.model = self.config.map_model(&chunk.model);
            events.push(Event::MessageStart {
                id: self.id.clone(),
                model: self.model.clone(),
            });
        }

        if let Some(calls) = &choice.delta.tool_calls {
            self.record_tool_calls(calls)?;
        }

        if let Some(reasoning) = choice
            .delta
            .reasoning_content
            .as_ref()
            .filter(|reasoning| !reasoning.is_empty())
        {
            if self.text_open {
                events.push(Event::ContentBlockStop {
                    index: self.text_index,
                });
                self.text_open = false;
            }
            if !self.thinking_open {
                self.thinking_open = true;
                self.thinking_index = self.next_ir_index;
                self.next_ir_index += 1;
                events.push(Event::ContentBlockStart {
                    index: self.thinking_index,
                    block: Block::Thinking {
                        thinking: String::new(),
                        signature: None,
                    },
                });
            }
            events.push(Event::ContentBlockDelta {
                index: self.thinking_index,
                delta: Delta::ThinkingDelta {
                    text: reasoning.clone(),
                },
            });
        }

        if let Some(content) = &choice.delta.content {
            if self.thinking_open {
                events.push(Event::ContentBlockStop {
                    index: self.thinking_index,
                });
                self.thinking_open = false;
            }
            if !self.text_open {
                self.text_open = true;
                self.text_index = self.next_ir_index;
                self.next_ir_index += 1;
                events.push(Event::ContentBlockStart {
                    index: self.text_index,
                    block: Block::Text {
                        text: String::new(),
                    },
                });
            }
            events.push(Event::ContentBlockDelta {
                index: self.text_index,
                delta: Delta::TextDelta {
                    text: content.clone(),
                },
            });
        }

        if let Some(finish_reason) = &choice.finish_reason {
            if self.finish_seen {
                return Err(Error::new("chatcompletions: duplicate finish_reason"));
            }
            let (stop, finish_loss) = decode_finish_reason(finish_reason)?;
            if let Some(loss) = finish_loss {
                self.losses.push(loss);
            }
            self.stop = Some(stop);
            self.finish_seen = true;
        }

        Ok(events)
    }

    fn record_tool_calls(&mut self, calls: &[ToolCallDelta]) -> Result<(), Error> {
        for call in calls {
            if call.index > self.tool_calls.len() {
                return Err(Error::new(format!(
                    "chatcompletions: tool_calls index {} is not the next consecutive native index",
                    call.index
                )));
            }
            if call.index == self.tool_calls.len() {
                self.tool_calls.push(StreamToolCall {
                    index: call.index,
                    ..Default::default()
                });
            }
            let record = &mut self.tool_calls[call.index];
            if let Some(id) = &call.id
                && !id.is_empty()
            {
                if !record.id.is_empty() && record.id != *id {
                    return Err(Error::new(format!(
                        "chatcompletions: tool_calls[{}] has conflicting IDs {:?} and {:?}",
                        call.index, record.id, id
                    )));
                }
                record.id = id.clone();
            }
            if let Some(kind) = &call.kind
                && kind != TOOL_TYPE_FUNCTION
                && !record.skipped
            {
                self.losses.push(loss(
                    format!("choices[0].delta.tool_calls[{}]", call.index),
                    "type",
                    LossReason::UnsupportedSemantic,
                    format!(
                        "Chat Completions streamed tool type {:?} has no IR equivalent",
                        kind
                    ),
                ));
                record.skipped = true;
            }
            if record.skipped {
                continue;
            }
            if let Some(function) = &call.function {
                if let Some(name) = &function.name {
                    record.name.push_str(name);
                }
                if let Some(arguments) = &function.arguments {
                    record.fragments.push(arguments.clone());
                }
            }
        }
        Ok(())
    }

    /// Closes the stream and returns terminal stop and message events.
    pub fn flush(&mut self) -> Result<Vec<Event>, Error> {
        if self.flushed {
            return Err(Error::new("chatcompletions: stream flushed twice"));
        }
        let Some(stop) = self.stop else {
            return Err(Error::new(
                "chatcompletions: stream ended without finish_reason",
            ));
        };
        self.flushed = true;

        let mut events = Vec::new();
        if self.thinking_open {
            events.push(Event::ContentBlockStop {
                index: self.thinking_index,
            });
            self.thinking_open = false;
        }
        if self.text_open {
            events.push(Event::ContentBlockStop {
                index: self.text_index,
            });
            self.text_open = false;
        }

        for call in &self.tool_calls {
            if call.skipped {
                continue;
            }
            if call.id.is_empty() {
                return Err(Error::new(format!(
                    "chatcompletions: tool_calls[{}] is missing final ID",
                    call.index
                )));
            }
            if call.name.is_empty() {
                return Err(Error::new(format!(
                    "chatcompletions: tool_calls[{}] is missing final function name",
                    call.index
                )));
            }
            let index = self.next_ir_index;
            self.next_ir_index += 1;
            let full_input = call.fragments.concat();
            events.push(Event::ContentBlockStart {
                index,
                block: Block::ToolUse {
                    id: call.id.clone(),
                    name: call.name.clone(),
                    input: full_input,
                },
            });
            for fragment in &call.fragments {
                events.push(Event::ContentBlockDelta {
                    index,
                    delta: Delta::InputJsonDelta {
                        partial_json: fragment.clone(),
                    },
                });
            }
            events.push(Event::ContentBlockStop { index });
        }

        let usage = self.usage.unwrap_or_default();

        events.push(Event::MessageDelta {
            stop_reason: stop,
            stop_sequence: None,
            usage,
        });
        events.push(Event::MessageDone {});

        Ok(events)
    }

    /// Returns the accumulated losses across the stream.
    pub fn losses(&self) -> &[Loss] {
        &self.losses
    }
}
