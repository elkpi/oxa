//! Anthropic Messages streaming encoder: converts IR events into wire events (IR → face).

use oxa_ir::{Block, Delta, Event, Loss};
use serde_json::value::RawValue;

use crate::config::Config;
use crate::encode::encode_stop_reason;
use crate::error::Error;
use crate::types::{
    BLOCK_TYPE_TEXT, BLOCK_TYPE_THINKING, BLOCK_TYPE_TOOL_USE, BlockWire,
    DELTA_TYPE_INPUT_JSON_DELTA, DELTA_TYPE_SIGNATURE_DELTA, DELTA_TYPE_TEXT_DELTA,
    DELTA_TYPE_THINKING_DELTA, EVENT_TYPE_CONTENT_BLOCK_DELTA, EVENT_TYPE_CONTENT_BLOCK_START,
    EVENT_TYPE_CONTENT_BLOCK_STOP, EVENT_TYPE_MESSAGE_DELTA, EVENT_TYPE_MESSAGE_START,
    EVENT_TYPE_MESSAGE_STOP, MessageStartWire, ROLE_ASSISTANT, StreamDelta, StreamEvent,
    TYPE_MESSAGE, UsageWire,
};

/// Incrementally converts an IR event stream into Anthropic Messages wire events.
pub struct StreamEncoder {
    config: Config,
    id: String,
    model: String,
    started: bool,
    next_index: i64,
    open_index: i64,
    block_open: bool,
    open_tool: bool,
    open_thinking: bool,
    thinking_input: String,
    thinking_signature: Option<String>,
    thinking_parts: Vec<String>,
    signature_seen: bool,
    tool_input: String,
    tool_parts: Vec<String>,
    delta_seen: bool,
    done: bool,
}

impl StreamEncoder {
    /// Creates a new encoder for an event stream.
    pub fn new(config: &Config) -> Self {
        StreamEncoder {
            config: config.clone(),
            id: String::new(),
            model: String::new(),
            started: false,
            next_index: 0,
            open_index: 0,
            block_open: false,
            open_tool: false,
            open_thinking: false,
            thinking_input: String::new(),
            thinking_signature: None,
            thinking_parts: Vec::new(),
            signature_seen: false,
            tool_input: String::new(),
            tool_parts: Vec::new(),
            delta_seen: false,
            done: false,
        }
    }

    /// Pushes one IR event and returns the wire events and losses it produces.
    pub fn apply(&mut self, ev: &Event) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        if self.done {
            return Err(Error::new("anthropic: event applied after MessageDone"));
        }

        match ev {
            Event::MessageStart { id, model } => {
                if self.started {
                    return Err(Error::new("anthropic: duplicate MessageStart"));
                }
                self.started = true;
                self.id = id.clone();
                self.model = self.config.map_model(model);
                Ok((
                    vec![StreamEvent {
                        kind: EVENT_TYPE_MESSAGE_START.to_string(),
                        message: Some(MessageStartWire {
                            id: self.id.clone(),
                            kind: TYPE_MESSAGE.to_string(),
                            role: ROLE_ASSISTANT.to_string(),
                            model: self.model.clone(),
                            content: Vec::new(),
                            stop_reason: None,
                            usage: Some(UsageWire {
                                input_tokens: 0,
                                output_tokens: 0,
                                ..UsageWire::default()
                            }),
                        }),
                        ..Default::default()
                    }],
                    Vec::new(),
                ))
            }
            Event::ContentBlockStart { index, block } => {
                if !self.started || self.block_open {
                    return Err(Error::new(
                        "anthropic: ContentBlockStart out of grammar order",
                    ));
                }
                if *index != self.next_index {
                    return Err(Error::new(format!(
                        "anthropic: ContentBlockStart index {}, want {}",
                        index, self.next_index
                    )));
                }
                match block {
                    Block::Text { text } => {
                        self.next_index += 1;
                        self.block_open = true;
                        self.open_tool = false;
                        self.open_thinking = false;
                        self.open_index = *index;
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_CONTENT_BLOCK_START.to_string(),
                                index: Some(*index),
                                content_block: Some(BlockWire {
                                    kind: BLOCK_TYPE_TEXT.to_string(),
                                    text: text.clone(),
                                    ..Default::default()
                                }),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                    Block::Thinking {
                        thinking,
                        signature,
                    } => {
                        self.next_index += 1;
                        self.block_open = true;
                        self.open_tool = false;
                        self.open_thinking = true;
                        self.open_index = *index;
                        self.thinking_input = thinking.clone();
                        self.thinking_signature = signature.clone();
                        self.thinking_parts.clear();
                        self.signature_seen = false;
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_CONTENT_BLOCK_START.to_string(),
                                index: Some(*index),
                                content_block: Some(BlockWire {
                                    kind: BLOCK_TYPE_THINKING.to_string(),
                                    thinking: None,
                                    ..Default::default()
                                }),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                    Block::ToolUse { id, name, input } => {
                        if id.is_empty() {
                            return Err(Error::new(
                                "anthropic: ContentBlockStart tool_use id is required",
                            ));
                        }
                        if name.is_empty() {
                            return Err(Error::new(
                                "anthropic: ContentBlockStart tool_use name is required",
                            ));
                        }
                        self.next_index += 1;
                        self.block_open = true;
                        self.open_tool = true;
                        self.open_thinking = false;
                        self.open_index = *index;
                        self.tool_input = input.clone();
                        self.tool_parts.clear();
                        let empty_object = RawValue::from_string("{}".to_string())
                            .expect("empty JSON object is valid");
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_CONTENT_BLOCK_START.to_string(),
                                index: Some(*index),
                                content_block: Some(BlockWire {
                                    kind: BLOCK_TYPE_TOOL_USE.to_string(),
                                    id: id.clone(),
                                    name: name.clone(),
                                    input: Some(empty_object),
                                    ..Default::default()
                                }),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                    _ => Err(Error::new(
                        "anthropic: ContentBlockStart carries an unsupported block",
                    )),
                }
            }
            Event::ContentBlockDelta { index, delta } => {
                if !self.block_open || *index != self.open_index {
                    return Err(Error::new(
                        "anthropic: ContentBlockDelta out of grammar order",
                    ));
                }
                match delta {
                    Delta::ThinkingDelta { text } => {
                        if !self.open_thinking {
                            return Err(Error::new(
                                "anthropic: ThinkingDelta on non-thinking block",
                            ));
                        }
                        if self.signature_seen {
                            return Err(Error::new(
                                "anthropic: thinking_delta after signature_delta",
                            ));
                        }
                        self.thinking_parts.push(text.clone());
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_CONTENT_BLOCK_DELTA.to_string(),
                                index: Some(*index),
                                delta: Some(StreamDelta {
                                    kind: DELTA_TYPE_THINKING_DELTA.to_string(),
                                    thinking: Some(text.clone()),
                                    ..Default::default()
                                }),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                    Delta::SignatureDelta { signature } => {
                        if !self.open_thinking {
                            return Err(Error::new(
                                "anthropic: SignatureDelta on non-thinking block",
                            ));
                        }
                        if self.signature_seen {
                            return Err(Error::new("anthropic: duplicate signature_delta"));
                        }
                        self.signature_seen = true;
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_CONTENT_BLOCK_DELTA.to_string(),
                                index: Some(*index),
                                delta: Some(StreamDelta {
                                    kind: DELTA_TYPE_SIGNATURE_DELTA.to_string(),
                                    signature: Some(signature.clone()),
                                    ..Default::default()
                                }),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                    Delta::TextDelta { text } => {
                        if self.open_tool || self.open_thinking {
                            return Err(Error::new("anthropic: TextDelta on non-text block"));
                        }
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_CONTENT_BLOCK_DELTA.to_string(),
                                index: Some(*index),
                                delta: Some(StreamDelta {
                                    kind: DELTA_TYPE_TEXT_DELTA.to_string(),
                                    text: text.clone(),
                                    ..Default::default()
                                }),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                    Delta::InputJsonDelta { partial_json } => {
                        if !self.open_tool {
                            return Err(Error::new("anthropic: InputJSONDelta on text block"));
                        }
                        self.tool_parts.push(partial_json.clone());
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_CONTENT_BLOCK_DELTA.to_string(),
                                index: Some(*index),
                                delta: Some(StreamDelta {
                                    kind: DELTA_TYPE_INPUT_JSON_DELTA.to_string(),
                                    partial_json: Some(partial_json.clone()),
                                    ..Default::default()
                                }),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                }
            }
            Event::ContentBlockStop { index } => {
                if !self.block_open || *index != self.open_index {
                    return Err(Error::new(
                        "anthropic: ContentBlockStop out of grammar order",
                    ));
                }
                if self.open_thinking {
                    let mut events = Vec::new();
                    if self.thinking_parts.is_empty() && !self.thinking_input.is_empty() {
                        events.push(StreamEvent {
                            kind: EVENT_TYPE_CONTENT_BLOCK_DELTA.to_string(),
                            index: Some(*index),
                            delta: Some(StreamDelta {
                                kind: DELTA_TYPE_THINKING_DELTA.to_string(),
                                thinking: Some(self.thinking_input.clone()),
                                ..Default::default()
                            }),
                            ..Default::default()
                        });
                    }
                    if !self.signature_seen
                        && self
                            .thinking_signature
                            .as_deref()
                            .is_some_and(|signature| !signature.is_empty())
                    {
                        events.push(StreamEvent {
                            kind: EVENT_TYPE_CONTENT_BLOCK_DELTA.to_string(),
                            index: Some(*index),
                            delta: Some(StreamDelta {
                                kind: DELTA_TYPE_SIGNATURE_DELTA.to_string(),
                                signature: self.thinking_signature.clone(),
                                ..Default::default()
                            }),
                            ..Default::default()
                        });
                    }
                    events.push(StreamEvent {
                        kind: EVENT_TYPE_CONTENT_BLOCK_STOP.to_string(),
                        index: Some(*index),
                        ..Default::default()
                    });
                    self.block_open = false;
                    self.open_thinking = false;
                    self.thinking_input.clear();
                    self.thinking_signature = None;
                    self.thinking_parts.clear();
                    self.signature_seen = false;
                    return Ok((events, Vec::new()));
                }
                let mut events = Vec::new();
                if self.open_tool {
                    if self.tool_parts.is_empty() {
                        events.push(StreamEvent {
                            kind: EVENT_TYPE_CONTENT_BLOCK_DELTA.to_string(),
                            index: Some(*index),
                            delta: Some(StreamDelta {
                                kind: DELTA_TYPE_INPUT_JSON_DELTA.to_string(),
                                partial_json: Some(self.tool_input.clone()),
                                ..Default::default()
                            }),
                            ..Default::default()
                        });
                    } else if self.tool_parts.concat() != self.tool_input {
                        return Err(Error::new(
                            "anthropic: tool input fragments do not match ToolUseBlock input",
                        ));
                    }
                }
                events.push(StreamEvent {
                    kind: EVENT_TYPE_CONTENT_BLOCK_STOP.to_string(),
                    index: Some(*index),
                    ..Default::default()
                });
                self.block_open = false;
                self.open_tool = false;
                self.open_thinking = false;
                self.tool_input.clear();
                self.tool_parts.clear();
                Ok((events, Vec::new()))
            }
            Event::MessageDelta {
                stop_reason,
                stop_sequence,
                usage,
            } => {
                if !self.started || self.block_open || self.delta_seen {
                    return Err(Error::new("anthropic: MessageDelta out of grammar order"));
                }
                let (reason, seq) = encode_stop_reason(*stop_reason, stop_sequence.as_deref())?;
                self.delta_seen = true;
                Ok((
                    vec![StreamEvent {
                        kind: EVENT_TYPE_MESSAGE_DELTA.to_string(),
                        delta: Some(StreamDelta {
                            stop_reason: Some(reason.to_string()),
                            stop_sequence: seq,
                            ..Default::default()
                        }),
                        usage: Some(UsageWire {
                            input_tokens: usage.input_tokens,
                            output_tokens: usage.output_tokens,
                            cache_creation_input_tokens: usage.cache_creation_input_tokens,
                            cache_read_input_tokens: usage.cache_read_input_tokens,
                        }),
                        ..Default::default()
                    }],
                    Vec::new(),
                ))
            }
            Event::MessageDone {} => {
                if !self.delta_seen {
                    return Err(Error::new("anthropic: MessageDone out of grammar order"));
                }
                self.done = true;
                Ok((
                    vec![StreamEvent {
                        kind: EVENT_TYPE_MESSAGE_STOP.to_string(),
                        ..Default::default()
                    }],
                    Vec::new(),
                ))
            }
        }
    }
}
