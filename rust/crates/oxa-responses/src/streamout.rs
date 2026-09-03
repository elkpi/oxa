//! OpenAI Responses streaming encoder: converts IR events into Responses streaming events (IR → face).

use oxa_ir::{Block, Delta, Event, Loss, LossReason, StopReason};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::loss;
use crate::types::{
    ERROR_CODE_REFUSAL, EVENT_TYPE_RESPONSE_COMPLETED, EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED,
    EVENT_TYPE_RESPONSE_CONTENT_PART_DONE, EVENT_TYPE_RESPONSE_CREATED, EVENT_TYPE_RESPONSE_FAILED,
    EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA, EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE,
    EVENT_TYPE_RESPONSE_INCOMPLETE, EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED,
    EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE, EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA,
    EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE, ErrorWire, INCOMPLETE_REASON_MAX_OUTPUT_TOKENS,
    ITEM_TYPE_FUNCTION_CALL, ITEM_TYPE_MESSAGE, IncompleteWire, OBJECT_RESPONSE, OutputItem,
    OutputPart, PART_TYPE_OUTPUT_TEXT, ROLE_ASSISTANT, Response, STATUS_COMPLETED, STATUS_FAILED,
    STATUS_IN_PROGRESS, STATUS_INCOMPLETE, StreamEvent, UsageWire,
};

#[derive(Copy, Clone, PartialEq, Eq)]
enum StreamOutputItemKind {
    Message,
    FunctionCall,
}

struct StreamOutputItem {
    kind: StreamOutputItemKind,
    id: String,
    output_index: i64,
    content: Vec<OutputPart>,
    next_content_index: i64,
    call_id: String,
    name: String,
}

struct StreamEncodeBlock {
    index: i64,
    kind: StreamOutputItemKind,
    content_index: i64,
    text: String,
    tool_input: String,
    fragments: Vec<String>,
}

/// Incrementally converts an IR event stream into OpenAI Responses streaming events.
pub struct StreamEncoder {
    config: Config,
    id: String,
    model: String,
    started: bool,
    delta: bool,
    done: bool,
    next_block_index: i64,
    next_output_index: i64,
    next_message_item: usize,
    next_function_item: usize,
    active_item: Option<StreamOutputItem>,
    active_block: Option<StreamEncodeBlock>,
    completed: Vec<OutputItem>,
}

impl StreamEncoder {
    /// Creates a new encoder for an IR event stream.
    pub fn new(config: &Config) -> Self {
        StreamEncoder {
            config: config.clone(),
            id: String::new(),
            model: String::new(),
            started: false,
            delta: false,
            done: false,
            next_block_index: 0,
            next_output_index: 0,
            next_message_item: 0,
            next_function_item: 0,
            active_item: None,
            active_block: None,
            completed: Vec::new(),
        }
    }

    /// Pushes one IR event and returns the emitted wire events and losses.
    pub fn apply(&mut self, ev: &Event) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        if self.done || (self.delta && !matches!(ev, Event::MessageDone {})) {
            return Err(Error::new(
                "responses: event applied after stream termination",
            ));
        }

        match ev {
            Event::MessageStart { id, model } => {
                if self.started {
                    return Err(Error::new("responses: duplicate MessageStart"));
                }
                self.started = true;
                self.id = id.clone();
                self.model = self.config.map_model(model);
                Ok((
                    vec![StreamEvent {
                        kind: EVENT_TYPE_RESPONSE_CREATED.to_string(),
                        response: Some(Response {
                            id: self.id.clone(),
                            object: OBJECT_RESPONSE.to_string(),
                            status: STATUS_IN_PROGRESS.to_string(),
                            model: self.model.clone(),
                            output: Vec::new(),
                            ..Default::default()
                        }),
                        ..Default::default()
                    }],
                    Vec::new(),
                ))
            }
            Event::ContentBlockStart { index, block } => {
                if !self.started || self.active_block.is_some() || self.delta {
                    return Err(Error::new(
                        "responses: ContentBlockStart out of grammar order",
                    ));
                }
                if *index != self.next_block_index {
                    return Err(Error::new(format!(
                        "responses: ContentBlockStart index {}, want {}",
                        index, self.next_block_index
                    )));
                }
                self.next_block_index += 1;
                match block {
                    Block::Text { text } => self.start_text_block(*index, text),
                    Block::ToolUse { id, name, input } => {
                        self.start_function_call_block(*index, id, name, input)
                    }
                    _ => Err(Error::new(
                        "responses: ContentBlockStart carries unsupported block",
                    )),
                }
            }
            Event::ContentBlockDelta { index, delta } => {
                let Some(active_block) = &mut self.active_block else {
                    return Err(Error::new(
                        "responses: ContentBlockDelta out of grammar order",
                    ));
                };
                if *index != active_block.index {
                    return Err(Error::new(
                        "responses: ContentBlockDelta out of grammar order",
                    ));
                }
                let active_item = self.active_item.as_ref().expect("active item");
                match active_block.kind {
                    StreamOutputItemKind::Message => {
                        let Delta::TextDelta { text } = delta else {
                            return Err(Error::new("responses: TextBlock received non-text delta"));
                        };
                        active_block.text.push_str(text);
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA.to_string(),
                                item_id: Some(active_item.id.clone()),
                                output_index: Some(active_item.output_index),
                                content_index: Some(active_block.content_index),
                                delta: Some(text.clone()),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                    StreamOutputItemKind::FunctionCall => {
                        let Delta::InputJsonDelta { partial_json } = delta else {
                            return Err(Error::new(
                                "responses: ToolUseBlock received non-input-json delta",
                            ));
                        };
                        active_block.fragments.push(partial_json.clone());
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA.to_string(),
                                item_id: Some(active_item.id.clone()),
                                output_index: Some(active_item.output_index),
                                delta: Some(partial_json.clone()),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                }
            }
            Event::ContentBlockStop { index } => {
                let Some(active_block) = &self.active_block else {
                    return Err(Error::new(
                        "responses: ContentBlockStop out of grammar order",
                    ));
                };
                if *index != active_block.index {
                    return Err(Error::new(
                        "responses: ContentBlockStop out of grammar order",
                    ));
                }
                match active_block.kind {
                    StreamOutputItemKind::Message => self.stop_text_block(),
                    StreamOutputItemKind::FunctionCall => self.stop_function_call_block(),
                }
            }
            Event::MessageDelta { .. } => {
                if !self.started || self.active_block.is_some() || self.delta {
                    return Err(Error::new("responses: MessageDelta out of grammar order"));
                }
                let mut out = Vec::new();
                if let Some(active) = &self.active_item {
                    if active.kind != StreamOutputItemKind::Message {
                        return Err(Error::new(
                            "responses: MessageDelta with an uncompleted function_call item",
                        ));
                    }
                    out.push(self.close_message_item());
                }
                let (mut terminal, losses) = self.terminal(ev)?;
                if let Some(resp) = &mut terminal.response {
                    resp.output = self.completed.clone();
                }
                self.delta = true;
                out.push(terminal);
                Ok((out, losses))
            }
            Event::MessageDone {} => {
                if !self.delta {
                    return Err(Error::new("responses: MessageDone out of grammar order"));
                }
                self.done = true;
                Ok((Vec::new(), Vec::new()))
            }
        }
    }

    fn start_text_block(
        &mut self,
        index: i64,
        text: &str,
    ) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let mut out = Vec::new();
        if self.active_item.is_none() {
            let (item, added) = self.open_message_item();
            self.active_item = Some(item);
            out.push(added);
        }
        let active = self.active_item.as_mut().unwrap();
        if active.kind != StreamOutputItemKind::Message {
            return Err(Error::new(
                "responses: TextBlock cannot open before the active function_call item completes",
            ));
        }
        let content_index = active.next_content_index;
        active.next_content_index += 1;
        let part = OutputPart {
            kind: PART_TYPE_OUTPUT_TEXT.to_string(),
            text: text.to_string(),
            annotations: Vec::new(),
        };
        active.content.push(part.clone());
        self.active_block = Some(StreamEncodeBlock {
            index,
            kind: StreamOutputItemKind::Message,
            content_index,
            text: text.to_string(),
            tool_input: String::new(),
            fragments: Vec::new(),
        });
        out.push(StreamEvent {
            kind: EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED.to_string(),
            item_id: Some(active.id.clone()),
            output_index: Some(active.output_index),
            content_index: Some(content_index),
            part: Some(part),
            ..Default::default()
        });
        Ok((out, Vec::new()))
    }

    fn start_function_call_block(
        &mut self,
        index: i64,
        id: &str,
        name: &str,
        input: &str,
    ) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        if id.is_empty() || name.is_empty() {
            return Err(Error::new(
                "responses: ToolUseBlock requires nonempty ID and name",
            ));
        }
        let mut out = Vec::new();
        if let Some(active) = &self.active_item {
            if active.kind != StreamOutputItemKind::Message {
                return Err(Error::new(
                    "responses: ToolUseBlock cannot open before the active function_call item completes",
                ));
            }
            out.push(self.close_message_item());
        }
        let (item, added) = self.open_function_call_item(id, name);
        self.active_item = Some(item);
        self.active_block = Some(StreamEncodeBlock {
            index,
            kind: StreamOutputItemKind::FunctionCall,
            content_index: 0,
            text: String::new(),
            tool_input: input.to_string(),
            fragments: Vec::new(),
        });
        out.push(added);
        Ok((out, Vec::new()))
    }

    fn stop_text_block(&mut self) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let Some(active_item) = &mut self.active_item else {
            return Err(Error::new(
                "responses: text block without an active message item",
            ));
        };
        if active_item.kind != StreamOutputItemKind::Message {
            return Err(Error::new(
                "responses: text block without an active message item",
            ));
        }
        let block = self.active_block.take().expect("active block");
        let content_idx = block.content_index as usize;
        active_item.content[content_idx].text = block.text.clone();
        let part = active_item.content[content_idx].clone();
        let item_id = active_item.id.clone();
        let output_index = active_item.output_index;
        Ok((
            vec![
                StreamEvent {
                    kind: EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE.to_string(),
                    item_id: Some(item_id.clone()),
                    output_index: Some(output_index),
                    content_index: Some(block.content_index),
                    text: Some(block.text),
                    ..Default::default()
                },
                StreamEvent {
                    kind: EVENT_TYPE_RESPONSE_CONTENT_PART_DONE.to_string(),
                    item_id: Some(item_id),
                    output_index: Some(output_index),
                    content_index: Some(block.content_index),
                    part: Some(part),
                    ..Default::default()
                },
            ],
            Vec::new(),
        ))
    }

    fn stop_function_call_block(&mut self) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let Some(active_item) = &mut self.active_item else {
            return Err(Error::new(
                "responses: tool block without an active function_call item",
            ));
        };
        if active_item.kind != StreamOutputItemKind::FunctionCall {
            return Err(Error::new(
                "responses: tool block without an active function_call item",
            ));
        }
        let mut block = self.active_block.take().expect("active block");
        let mut out = Vec::new();
        if block.fragments.is_empty() {
            block.fragments.push(block.tool_input.clone());
            out.push(StreamEvent {
                kind: EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA.to_string(),
                item_id: Some(active_item.id.clone()),
                output_index: Some(active_item.output_index),
                delta: Some(block.tool_input.clone()),
                ..Default::default()
            });
        }
        let arguments = block.fragments.concat();
        if arguments != block.tool_input {
            return Err(Error::new(
                "responses: ToolUseBlock input does not equal concatenated InputJSONDelta fragments",
            ));
        }
        let completed = OutputItem {
            id: active_item.id.clone(),
            kind: ITEM_TYPE_FUNCTION_CALL.to_string(),
            status: STATUS_COMPLETED.to_string(),
            call_id: active_item.call_id.clone(),
            name: active_item.name.clone(),
            arguments: arguments.clone(),
            ..Default::default()
        };
        out.push(StreamEvent {
            kind: EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE.to_string(),
            item_id: Some(active_item.id.clone()),
            output_index: Some(active_item.output_index),
            call_id: Some(active_item.call_id.clone()),
            name: Some(active_item.name.clone()),
            arguments: Some(arguments),
            ..Default::default()
        });
        out.push(StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE.to_string(),
            output_index: Some(active_item.output_index),
            item: Some(completed.clone()),
            ..Default::default()
        });
        self.completed.push(completed);
        self.active_item = None;
        Ok((out, Vec::new()))
    }

    fn open_message_item(&mut self) -> (StreamOutputItem, StreamEvent) {
        let id = stream_generated_item_id("msg", self.next_message_item);
        self.next_message_item += 1;
        let output_index = self.next_output_index;
        self.next_output_index += 1;
        let item = StreamOutputItem {
            kind: StreamOutputItemKind::Message,
            id: id.clone(),
            output_index,
            content: Vec::new(),
            next_content_index: 0,
            call_id: String::new(),
            name: String::new(),
        };
        let event = StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED.to_string(),
            output_index: Some(output_index),
            item: Some(OutputItem {
                id,
                kind: ITEM_TYPE_MESSAGE.to_string(),
                status: STATUS_IN_PROGRESS.to_string(),
                role: ROLE_ASSISTANT.to_string(),
                ..Default::default()
            }),
            ..Default::default()
        };
        (item, event)
    }

    fn open_function_call_item(
        &mut self,
        call_id: &str,
        name: &str,
    ) -> (StreamOutputItem, StreamEvent) {
        let id = stream_generated_item_id("fc", self.next_function_item);
        self.next_function_item += 1;
        let output_index = self.next_output_index;
        self.next_output_index += 1;
        let item = StreamOutputItem {
            kind: StreamOutputItemKind::FunctionCall,
            id: id.clone(),
            output_index,
            content: Vec::new(),
            next_content_index: 0,
            call_id: call_id.to_string(),
            name: name.to_string(),
        };
        let event = StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED.to_string(),
            output_index: Some(output_index),
            item: Some(OutputItem {
                id,
                kind: ITEM_TYPE_FUNCTION_CALL.to_string(),
                status: STATUS_IN_PROGRESS.to_string(),
                call_id: call_id.to_string(),
                name: name.to_string(),
                arguments: String::new(),
                ..Default::default()
            }),
            ..Default::default()
        };
        (item, event)
    }

    fn close_message_item(&mut self) -> StreamEvent {
        let item = self
            .active_item
            .take()
            .expect("active message item to close");
        let completed = OutputItem {
            id: item.id,
            kind: ITEM_TYPE_MESSAGE.to_string(),
            status: STATUS_COMPLETED.to_string(),
            role: ROLE_ASSISTANT.to_string(),
            content: item.content,
            ..Default::default()
        };
        let event = StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE.to_string(),
            output_index: Some(item.output_index),
            item: Some(completed.clone()),
            ..Default::default()
        };
        self.completed.push(completed);
        event
    }

    fn terminal(&self, delta: &Event) -> Result<(StreamEvent, Vec<Loss>), Error> {
        let Event::MessageDelta {
            stop_reason, usage, ..
        } = delta
        else {
            return Err(Error::new("expected MessageDelta"));
        };
        let mut response = Response {
            id: self.id.clone(),
            object: OBJECT_RESPONSE.to_string(),
            model: self.model.clone(),
            output: Vec::new(),
            usage: Some(UsageWire {
                input_tokens: usage.input_tokens,
                output_tokens: usage.output_tokens,
                total_tokens: usage.input_tokens + usage.output_tokens,
            }),
            ..Default::default()
        };
        match stop_reason {
            StopReason::EndTurn | StopReason::ToolUse => {
                response.status = STATUS_COMPLETED.to_string();
                Ok((
                    StreamEvent {
                        kind: EVENT_TYPE_RESPONSE_COMPLETED.to_string(),
                        response: Some(response),
                        ..Default::default()
                    },
                    Vec::new(),
                ))
            }
            StopReason::MaxTokens => {
                response.status = STATUS_INCOMPLETE.to_string();
                response.incomplete_details = Some(IncompleteWire {
                    reason: INCOMPLETE_REASON_MAX_OUTPUT_TOKENS.to_string(),
                });
                Ok((
                    StreamEvent {
                        kind: EVENT_TYPE_RESPONSE_INCOMPLETE.to_string(),
                        response: Some(response),
                        ..Default::default()
                    },
                    Vec::new(),
                ))
            }
            StopReason::Refusal => {
                response.status = STATUS_FAILED.to_string();
                response.error = Some(ErrorWire {
                    code: ERROR_CODE_REFUSAL.to_string(),
                    message: String::new(),
                });
                Ok((
                    StreamEvent {
                        kind: EVENT_TYPE_RESPONSE_FAILED.to_string(),
                        response: Some(response),
                        ..Default::default()
                    },
                    Vec::new(),
                ))
            }
            StopReason::StopSequence => {
                response.status = STATUS_COMPLETED.to_string();
                Ok((
                    StreamEvent {
                        kind: EVENT_TYPE_RESPONSE_COMPLETED.to_string(),
                        response: Some(response),
                        ..Default::default()
                    },
                    vec![loss(
                        "status",
                        "stop_sequence",
                        LossReason::UnmappedValue,
                        "Responses status carries no stop-sequence identity; the matched IR stop sequence is lost",
                    )],
                ))
            }
            StopReason::Other => Err(Error::new(
                "responses: stop reason \"other\" has no Responses equivalent",
            )),
        }
    }
}

fn stream_generated_item_id(prefix: &str, ordinal: usize) -> String {
    format!("{prefix}_abc{:03}", 123 + 333 * ordinal)
}
