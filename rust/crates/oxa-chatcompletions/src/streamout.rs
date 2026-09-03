//! Chat Completions streaming encoder: converts IR events into chunks (IR → face).

use oxa_ir::{Block, Delta, Event, Loss, LossReason};

use crate::config::Config;
use crate::encode::encode_finish_reason;
use crate::error::Error;
use crate::normalize::loss;
use crate::types::{
    ChoiceDelta, Chunk, DeltaPayload, FunctionDelta, OBJECT_CHAT_COMPLETION_CHUNK, ROLE_ASSISTANT,
    TOOL_TYPE_FUNCTION, ToolCallDelta, UsageWire,
};

#[derive(Copy, Clone, PartialEq, Eq)]
enum StreamBlockKind {
    Text,
    Tool,
}

struct StreamEncodeBlock {
    kind: StreamBlockKind,
    index: i64,
    tool_id: String,
    tool_name: String,
    tool_input: String,
    fragments: Vec<String>,
    native_index: usize,
    tool_started: bool,
}

/// Incrementally converts an IR event stream into Chat Completions chunks.
pub struct StreamEncoder {
    config: Config,
    id: String,
    model: String,
    started: bool,
    active: Option<StreamEncodeBlock>,
    next_ir_index: i64,
    next_native_tool: usize,
    tool_seen: bool,
    ordering_degrade: bool,
    pending_tools: Vec<Chunk>,
    finished: bool,
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
            active: None,
            next_ir_index: 0,
            next_native_tool: 0,
            tool_seen: false,
            ordering_degrade: false,
            pending_tools: Vec::new(),
            finished: false,
            done: false,
        }
    }

    /// Pushes one IR event and returns the chunks and losses it produces.
    pub fn apply(&mut self, ev: &Event) -> Result<(Vec<Chunk>, Vec<Loss>), Error> {
        if self.done || (self.finished && !matches!(ev, Event::MessageDone {})) {
            return Err(Error::new(
                "chatcompletions: event applied after stream termination",
            ));
        }

        match ev {
            Event::MessageStart { id, model } => {
                if self.started {
                    return Err(Error::new("chatcompletions: duplicate MessageStart"));
                }
                self.started = true;
                self.id = id.clone();
                self.model = self.config.map_model(model);
                Ok((
                    vec![self.chunk(DeltaPayload {
                        role: ROLE_ASSISTANT.to_string(),
                        ..Default::default()
                    })],
                    Vec::new(),
                ))
            }
            Event::ContentBlockStart { index, block } => {
                if !self.started || self.active.is_some() {
                    return Err(Error::new(
                        "chatcompletions: ContentBlockStart out of grammar order",
                    ));
                }
                if *index != self.next_ir_index {
                    return Err(Error::new(format!(
                        "chatcompletions: ContentBlockStart index {}, want {}",
                        index, self.next_ir_index
                    )));
                }
                self.next_ir_index += 1;
                match block {
                    Block::Text { .. } => {
                        if self.tool_seen {
                            self.ordering_degrade = true;
                        }
                        self.active = Some(StreamEncodeBlock {
                            kind: StreamBlockKind::Text,
                            index: *index,
                            tool_id: String::new(),
                            tool_name: String::new(),
                            tool_input: String::new(),
                            fragments: Vec::new(),
                            native_index: 0,
                            tool_started: false,
                        });
                        Ok((Vec::new(), Vec::new()))
                    }
                    Block::ToolUse { id, name, input } => {
                        if id.is_empty() || name.is_empty() {
                            return Err(Error::new(
                                "chatcompletions: ToolUseBlock requires nonempty ID and name",
                            ));
                        }
                        let native_index = self.next_native_tool;
                        self.next_native_tool += 1;
                        self.tool_seen = true;
                        self.active = Some(StreamEncodeBlock {
                            kind: StreamBlockKind::Tool,
                            index: *index,
                            tool_id: id.clone(),
                            tool_name: name.clone(),
                            tool_input: input.clone(),
                            fragments: Vec::new(),
                            native_index,
                            tool_started: false,
                        });
                        Ok((Vec::new(), Vec::new()))
                    }
                    _ => Err(Error::new(
                        "chatcompletions: ContentBlockStart carries unsupported block",
                    )),
                }
            }
            Event::ContentBlockDelta { index, delta } => {
                let Some(active) = &mut self.active else {
                    return Err(Error::new(
                        "chatcompletions: ContentBlockDelta out of grammar order",
                    ));
                };
                if *index != active.index {
                    return Err(Error::new(
                        "chatcompletions: ContentBlockDelta out of grammar order",
                    ));
                }
                match active.kind {
                    StreamBlockKind::Text => {
                        let Delta::TextDelta { text } = delta else {
                            return Err(Error::new(
                                "chatcompletions: TextBlock received non-text delta",
                            ));
                        };
                        Ok((
                            vec![self.chunk(DeltaPayload {
                                content: Some(text.clone()),
                                ..Default::default()
                            })],
                            Vec::new(),
                        ))
                    }
                    StreamBlockKind::Tool => {
                        let Delta::InputJsonDelta { partial_json } = delta else {
                            return Err(Error::new(
                                "chatcompletions: ToolUseBlock received non-input-json delta",
                            ));
                        };
                        active.fragments.push(partial_json.clone());
                        let chunk =
                            make_tool_argument_chunk(&self.id, &self.model, active, partial_json);
                        self.pending_tools.push(chunk);
                        Ok((Vec::new(), Vec::new()))
                    }
                }
            }
            Event::ContentBlockStop { index } => {
                let Some(active) = self.active.take() else {
                    return Err(Error::new(
                        "chatcompletions: ContentBlockStop out of grammar order",
                    ));
                };
                if *index != active.index {
                    return Err(Error::new(
                        "chatcompletions: ContentBlockStop out of grammar order",
                    ));
                }
                if active.kind == StreamBlockKind::Tool {
                    let mut act = active;
                    if act.fragments.is_empty() {
                        // Synthesize native argument delta
                        let full = act.tool_input.clone();
                        act.fragments.push(full.clone());
                        let chunk =
                            make_tool_argument_chunk(&self.id, &self.model, &mut act, &full);
                        self.pending_tools.push(chunk);
                    }
                    if act.fragments.concat() != act.tool_input {
                        return Err(Error::new(
                            "chatcompletions: ToolUseBlock input does not equal concatenated InputJSONDelta fragments",
                        ));
                    }
                }
                Ok((Vec::new(), Vec::new()))
            }
            Event::MessageDelta {
                stop_reason, usage, ..
            } => {
                if !self.started || self.active.is_some() {
                    return Err(Error::new(
                        "chatcompletions: MessageDelta out of grammar order",
                    ));
                }
                let (finish, finish_loss) = encode_finish_reason(*stop_reason)?;
                let mut losses = Vec::new();
                if let Some(l) = finish_loss {
                    losses.push(l);
                }
                if self.ordering_degrade {
                    losses.push(loss(
                        "events",
                        "ordering",
                        LossReason::Degraded,
                        "N-S-10: the text block after a tool block is normalized ahead of the tool calls; IR source order is not preserved",
                    ));
                }
                self.finished = true;
                let mut chunks = std::mem::take(&mut self.pending_tools);
                chunks.push(Chunk {
                    id: self.id.clone(),
                    object: OBJECT_CHAT_COMPLETION_CHUNK.to_string(),
                    created: 0,
                    model: self.model.clone(),
                    choices: vec![ChoiceDelta {
                        index: 0,
                        delta: DeltaPayload::default(),
                        finish_reason: Some(finish.to_string()),
                    }],
                    usage: Some(UsageWire {
                        prompt_tokens: usage.input_tokens,
                        completion_tokens: usage.output_tokens,
                        total_tokens: usage.input_tokens + usage.output_tokens,
                    }),
                });
                Ok((chunks, losses))
            }
            Event::MessageDone {} => {
                if !self.finished {
                    return Err(Error::new(
                        "chatcompletions: MessageDone out of grammar order",
                    ));
                }
                self.done = true;
                Ok((Vec::new(), Vec::new()))
            }
        }
    }

    fn chunk(&self, delta: DeltaPayload) -> Chunk {
        Chunk {
            id: self.id.clone(),
            object: OBJECT_CHAT_COMPLETION_CHUNK.to_string(),
            created: 0,
            model: self.model.clone(),
            choices: vec![ChoiceDelta {
                index: 0,
                delta,
                finish_reason: None,
            }],
            usage: None,
        }
    }
}

fn make_tool_argument_chunk(
    id: &str,
    model: &str,
    block: &mut StreamEncodeBlock,
    arguments: &str,
) -> Chunk {
    let function = FunctionDelta {
        arguments: Some(arguments.to_string()),
        name: if !block.tool_started {
            Some(block.tool_name.clone())
        } else {
            None
        },
    };
    let call = ToolCallDelta {
        index: block.native_index,
        id: if !block.tool_started {
            Some(block.tool_id.clone())
        } else {
            None
        },
        kind: if !block.tool_started {
            Some(TOOL_TYPE_FUNCTION.to_string())
        } else {
            None
        },
        function: Some(function),
    };
    block.tool_started = true;
    Chunk {
        id: id.to_string(),
        object: OBJECT_CHAT_COMPLETION_CHUNK.to_string(),
        created: 0,
        model: model.to_string(),
        choices: vec![ChoiceDelta {
            index: 0,
            delta: DeltaPayload {
                tool_calls: Some(vec![call]),
                ..Default::default()
            },
            finish_reason: None,
        }],
        usage: None,
    }
}
