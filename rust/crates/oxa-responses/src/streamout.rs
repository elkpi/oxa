//! OpenAI Responses streaming encoder: converts IR events into Responses streaming events (IR → face).

use oxa_ir::{Block, Delta, Event, Loss, LossReason, StopReason};

use crate::config::Config;
use crate::error::Error;
use crate::normalize::loss;
use crate::types::{
    ERROR_CODE_REFUSAL, EVENT_TYPE_RESPONSE_COMPLETED, EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED,
    EVENT_TYPE_RESPONSE_CONTENT_PART_DONE, EVENT_TYPE_RESPONSE_CREATED,
    EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DELTA, EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DONE,
    EVENT_TYPE_RESPONSE_FAILED, EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA,
    EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE, EVENT_TYPE_RESPONSE_INCOMPLETE,
    EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED, EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE,
    EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA, EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE,
    EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_ADDED,
    EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_DONE,
    EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DELTA, ErrorWire,
    INCOMPLETE_REASON_MAX_OUTPUT_TOKENS, ITEM_TYPE_CUSTOM_TOOL_CALL, ITEM_TYPE_FUNCTION_CALL,
    ITEM_TYPE_MESSAGE, ITEM_TYPE_REASONING, IncompleteWire, InputTokenDetailsWire, OBJECT_RESPONSE,
    OutputItem, OutputPart, OutputTokenDetailsWire, OutputValue, PART_TYPE_OUTPUT_TEXT,
    ROLE_ASSISTANT, Response, STATUS_COMPLETED, STATUS_FAILED, STATUS_IN_PROGRESS,
    STATUS_INCOMPLETE, StreamEvent, UsageWire,
};

#[derive(Copy, Clone, PartialEq, Eq)]
enum StreamOutputItemKind {
    Message,
    Reasoning,
    FunctionCall,
    CustomToolCall,
    ApplyPatchCall,
    ApplyPatchResult,
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
    next_custom_item: usize,
    next_apply_patch_call_item: usize,
    next_apply_patch_result_item: usize,
    next_reasoning_item: usize,
    active_item: Option<StreamOutputItem>,
    active_block: Option<StreamEncodeBlock>,
    active_apply_patch_call: Option<Block>,
    active_apply_patch_result: Option<Block>,
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
            next_custom_item: 0,
            next_apply_patch_call_item: 0,
            next_apply_patch_result_item: 0,
            next_reasoning_item: 0,
            active_item: None,
            active_block: None,
            active_apply_patch_call: None,
            active_apply_patch_result: None,
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
                    Block::Text { text, .. } => self.start_text_block(*index, text),
                    Block::Thinking {
                        thinking,
                        signature,
                        ..
                    } => self.start_thinking_block(*index, thinking, signature.as_deref()),
                    Block::ToolUse {
                        id, name, input, ..
                    } => self.start_function_call_block(*index, id, name, input),
                    Block::CustomToolUse {
                        id, name, input, ..
                    } => self.start_custom_tool_call_block(*index, id, name, input),
                    Block::ApplyPatchToolUse { .. } => {
                        self.start_apply_patch_call_block(*index, block)
                    }
                    Block::ApplyPatchToolResult { .. } => {
                        self.start_apply_patch_result_block(*index, block)
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
                    StreamOutputItemKind::Reasoning => match delta {
                        Delta::ThinkingDelta { text } => {
                            active_block.text.push_str(text);
                            Ok((
                                vec![StreamEvent {
                                    kind: EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DELTA
                                        .to_string(),
                                    item_id: Some(active_item.id.clone()),
                                    output_index: Some(active_item.output_index),
                                    content_index: Some(active_block.content_index),
                                    delta: Some(text.clone()),
                                    ..Default::default()
                                }],
                                Vec::new(),
                            ))
                        }
                        Delta::SignatureDelta { .. } => Ok((
                            Vec::new(),
                            vec![loss(
                                format!("events[{index}].delta.signature"),
                                "signature",
                                LossReason::UnmappedField,
                                "Responses carries no signature delta in streaming reasoning",
                            )],
                        )),
                        _ => Err(Error::new(
                            "responses: ThinkingBlock received non-thinking delta",
                        )),
                    },
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
                    StreamOutputItemKind::CustomToolCall => {
                        let Delta::CustomToolInputDelta { text } = delta else {
                            return Err(Error::new(
                                "responses: CustomToolUseBlock received non-custom-input delta",
                            ));
                        };
                        active_block.fragments.push(text.clone());
                        Ok((
                            vec![StreamEvent {
                                kind: EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DELTA.to_string(),
                                item_id: Some(active_item.id.clone()),
                                output_index: Some(active_item.output_index),
                                delta: Some(text.clone()),
                                ..Default::default()
                            }],
                            Vec::new(),
                        ))
                    }
                    StreamOutputItemKind::ApplyPatchCall
                    | StreamOutputItemKind::ApplyPatchResult => Err(Error::new(
                        "responses: apply patch item does not accept deltas",
                    )),
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
                    StreamOutputItemKind::Reasoning => self.stop_thinking_block(),
                    StreamOutputItemKind::FunctionCall => self.stop_function_call_block(),
                    StreamOutputItemKind::CustomToolCall => self.stop_custom_tool_call_block(),
                    StreamOutputItemKind::ApplyPatchCall => self.stop_apply_patch_call_block(),
                    StreamOutputItemKind::ApplyPatchResult => self.stop_apply_patch_result_block(),
                }
            }
            Event::MessageDelta { .. } => {
                if !self.started || self.active_block.is_some() || self.delta {
                    return Err(Error::new("responses: MessageDelta out of grammar order"));
                }
                let mut out = Vec::new();
                if let Some(active) = &self.active_item {
                    match active.kind {
                        StreamOutputItemKind::Message => out.push(self.close_message_item()),
                        StreamOutputItemKind::Reasoning => out.push(self.close_reasoning_item()),
                        StreamOutputItemKind::FunctionCall
                        | StreamOutputItemKind::CustomToolCall
                        | StreamOutputItemKind::ApplyPatchCall
                        | StreamOutputItemKind::ApplyPatchResult => {
                            return Err(Error::new(
                                "responses: MessageDelta with an uncompleted tool-call item",
                            ));
                        }
                    }
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
        if let Some(active) = &self.active_item {
            match active.kind {
                StreamOutputItemKind::Message => {}
                StreamOutputItemKind::Reasoning => out.push(self.close_reasoning_item()),
                StreamOutputItemKind::FunctionCall
                | StreamOutputItemKind::CustomToolCall
                | StreamOutputItemKind::ApplyPatchCall
                | StreamOutputItemKind::ApplyPatchResult => {
                    return Err(Error::new(
                        "responses: TextBlock cannot open before the active tool item completes",
                    ));
                }
            }
        }
        if self.active_item.is_none() {
            let (item, added) = self.open_message_item();
            self.active_item = Some(item);
            out.push(added);
        }
        let active = self.active_item.as_mut().expect("message item opened");
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

    fn start_thinking_block(
        &mut self,
        index: i64,
        thinking: &str,
        signature: Option<&str>,
    ) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let mut out = Vec::new();
        if let Some(active) = &self.active_item {
            match active.kind {
                StreamOutputItemKind::Message => out.push(self.close_message_item()),
                // Consecutive thinking blocks merge into one reasoning item,
                // mirroring the Go reference encoder.
                StreamOutputItemKind::Reasoning => {}
                StreamOutputItemKind::FunctionCall
                | StreamOutputItemKind::CustomToolCall
                | StreamOutputItemKind::ApplyPatchCall
                | StreamOutputItemKind::ApplyPatchResult => {
                    return Err(Error::new(
                        "responses: ThinkingBlock cannot open before the active tool item completes",
                    ));
                }
            }
        }
        if self.active_item.is_none() {
            let (item, added) = self.open_reasoning_item();
            self.active_item = Some(item);
            out.push(added);
        }
        let active = self.active_item.as_mut().expect("reasoning item active");
        let content_index = active.next_content_index;
        active.next_content_index += 1;
        let part = OutputPart {
            kind: PART_TYPE_OUTPUT_TEXT.to_string(),
            text: thinking.to_string(),
            annotations: Vec::new(),
        };
        active.content.push(part.clone());
        self.active_block = Some(StreamEncodeBlock {
            index,
            kind: StreamOutputItemKind::Reasoning,
            content_index,
            text: thinking.to_string(),
            tool_input: String::new(),
            fragments: Vec::new(),
        });
        out.push(StreamEvent {
            kind: EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_ADDED.to_string(),
            item_id: Some(active.id.clone()),
            output_index: Some(active.output_index),
            content_index: Some(content_index),
            part: Some(part),
            ..Default::default()
        });
        let losses = signature
            .filter(|signature| !signature.is_empty())
            .map(|_| {
                vec![loss(
                    format!("events[{index}].block.signature"),
                    "signature",
                    LossReason::UnmappedField,
                    "Responses carries no signature in reasoning summary",
                )]
            })
            .unwrap_or_default();
        Ok((out, losses))
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
            match active.kind {
                StreamOutputItemKind::Message => out.push(self.close_message_item()),
                StreamOutputItemKind::Reasoning => out.push(self.close_reasoning_item()),
                StreamOutputItemKind::FunctionCall
                | StreamOutputItemKind::CustomToolCall
                | StreamOutputItemKind::ApplyPatchCall
                | StreamOutputItemKind::ApplyPatchResult => {
                    return Err(Error::new(
                        "responses: ToolUseBlock cannot open before the active tool item completes",
                    ));
                }
            }
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

    fn stop_thinking_block(&mut self) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        if self
            .active_item
            .as_ref()
            .is_none_or(|item| item.kind != StreamOutputItemKind::Reasoning)
        {
            return Err(Error::new(
                "responses: thinking block without an active reasoning item",
            ));
        }
        let block = self.active_block.take().expect("active block");
        let content_index = usize::try_from(block.content_index)
            .map_err(|_| Error::new("responses: negative reasoning content index"))?;
        let active_item = self.active_item.as_mut().expect("reasoning item");
        active_item.content[content_index].text = block.text;
        let part = active_item.content[content_index].clone();
        let item_id = active_item.id.clone();
        let output_index = active_item.output_index;
        // The shared reasoning item stays open across consecutive thinking
        // blocks; close_reasoning_item emits output_item.done at the next
        // non-thinking block or at MessageDelta.
        Ok((
            vec![StreamEvent {
                kind: EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_DONE.to_string(),
                item_id: Some(item_id),
                output_index: Some(output_index),
                content_index: Some(block.content_index),
                part: Some(part),
                ..Default::default()
            }],
            Vec::new(),
        ))
    }

    fn start_custom_tool_call_block(
        &mut self,
        index: i64,
        id: &str,
        name: &str,
        input: &str,
    ) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        if id.is_empty() || name.is_empty() {
            return Err(Error::new(
                "responses: CustomToolUseBlock requires nonempty ID and name",
            ));
        }
        let mut out = Vec::new();
        if let Some(active) = &self.active_item {
            match active.kind {
                StreamOutputItemKind::Message => out.push(self.close_message_item()),
                StreamOutputItemKind::Reasoning => out.push(self.close_reasoning_item()),
                _ => {
                    return Err(Error::new(
                        "responses: custom tool call cannot open before active tool call completes",
                    ));
                }
            }
        }
        let output_index = self.next_output_index;
        self.next_output_index += 1;
        let item_id = format!("ctc_{}", self.next_custom_item);
        self.next_custom_item += 1;
        self.active_item = Some(StreamOutputItem {
            kind: StreamOutputItemKind::CustomToolCall,
            id: item_id.clone(),
            output_index,
            content: Vec::new(),
            next_content_index: 0,
            call_id: id.to_string(),
            name: name.to_string(),
        });
        self.active_block = Some(StreamEncodeBlock {
            index,
            kind: StreamOutputItemKind::CustomToolCall,
            content_index: 0,
            text: String::new(),
            tool_input: input.to_string(),
            fragments: Vec::new(),
        });
        out.push(StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED.to_string(),
            output_index: Some(output_index),
            item: Some(OutputItem {
                id: item_id,
                kind: ITEM_TYPE_CUSTOM_TOOL_CALL.to_string(),
                call_id: id.to_string(),
                name: name.to_string(),
                input: Some(String::new()),
                ..OutputItem::default()
            }),
            ..Default::default()
        });
        Ok((out, Vec::new()))
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

    fn stop_custom_tool_call_block(&mut self) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let Some(active_item) = &mut self.active_item else {
            return Err(Error::new(
                "responses: custom tool block without an active item",
            ));
        };
        if active_item.kind != StreamOutputItemKind::CustomToolCall {
            return Err(Error::new(
                "responses: custom tool block without an active custom item",
            ));
        }
        let mut block = self.active_block.take().expect("active block");
        let mut out = Vec::new();
        if block.fragments.is_empty() {
            block.fragments.push(block.tool_input.clone());
            out.push(StreamEvent {
                kind: EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DELTA.to_string(),
                item_id: Some(active_item.id.clone()),
                output_index: Some(active_item.output_index),
                delta: Some(block.tool_input.clone()),
                ..Default::default()
            });
        }
        let input = block.fragments.concat();
        if input != block.tool_input {
            return Err(Error::new(
                "responses: CustomToolUseBlock input does not equal concatenated custom input deltas",
            ));
        }
        let completed = OutputItem {
            id: active_item.id.clone(),
            kind: ITEM_TYPE_CUSTOM_TOOL_CALL.to_string(),
            call_id: active_item.call_id.clone(),
            name: active_item.name.clone(),
            input: Some(input.clone()),
            ..OutputItem::default()
        };
        out.push(StreamEvent {
            kind: EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DONE.to_string(),
            item_id: Some(active_item.id.clone()),
            output_index: Some(active_item.output_index),
            input: Some(input),
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

    fn start_apply_patch_call_block(
        &mut self,
        index: i64,
        block: &Block,
    ) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let Block::ApplyPatchToolUse { id, operation, .. } = block else {
            unreachable!()
        };
        if id.is_empty() {
            return Err(Error::new(
                "responses: apply_patch_call requires nonempty id",
            ));
        }
        let mut out = Vec::new();
        if let Some(active) = &self.active_item {
            match active.kind {
                StreamOutputItemKind::Message => out.push(self.close_message_item()),
                StreamOutputItemKind::Reasoning => out.push(self.close_reasoning_item()),
                _ => {
                    return Err(Error::new(
                        "responses: apply_patch_call cannot open before active item completes",
                    ));
                }
            }
        }
        let output_index = self.next_output_index;
        self.next_output_index += 1;
        let item_id = format!("apc_{}", self.next_apply_patch_call_item);
        self.next_apply_patch_call_item += 1;
        self.active_item = Some(StreamOutputItem {
            kind: StreamOutputItemKind::ApplyPatchCall,
            id: item_id.clone(),
            output_index,
            content: Vec::new(),
            next_content_index: 0,
            call_id: id.clone(),
            name: String::new(),
        });
        self.active_block = Some(StreamEncodeBlock {
            index,
            kind: StreamOutputItemKind::ApplyPatchCall,
            content_index: 0,
            text: String::new(),
            tool_input: String::new(),
            fragments: Vec::new(),
        });
        self.active_apply_patch_call = Some(block.clone());
        let op_wire = crate::normalize::encode_apply_patch_operation(operation);
        out.push(StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED.to_string(),
            output_index: Some(output_index),
            item: Some(OutputItem {
                id: item_id,
                kind: "apply_patch_call".to_string(),
                call_id: id.clone(),
                status: "in_progress".to_string(),
                operation: Some(op_wire),
                ..OutputItem::default()
            }),
            ..Default::default()
        });
        Ok((out, Vec::new()))
    }

    fn stop_apply_patch_call_block(&mut self) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let Some(active_item) = &mut self.active_item else {
            return Err(Error::new(
                "responses: apply_patch_call block without active item",
            ));
        };
        let Some(Block::ApplyPatchToolUse {
            id,
            status,
            operation,
            ..
        }) = self.active_apply_patch_call.take()
        else {
            return Err(Error::new("responses: apply_patch_call missing block"));
        };
        self.active_block = None;
        let status_str = match status {
            oxa_ir::ApplyPatchCallStatus::InProgress => "in_progress",
            oxa_ir::ApplyPatchCallStatus::Completed => "completed",
        };
        let op_wire = crate::normalize::encode_apply_patch_operation(&operation);
        let completed = OutputItem {
            id: active_item.id.clone(),
            kind: "apply_patch_call".to_string(),
            call_id: id,
            status: status_str.to_string(),
            operation: Some(op_wire),
            ..OutputItem::default()
        };
        let out = vec![StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE.to_string(),
            output_index: Some(active_item.output_index),
            item: Some(completed.clone()),
            ..Default::default()
        }];
        self.completed.push(completed);
        self.active_item = None;
        Ok((out, Vec::new()))
    }

    fn start_apply_patch_result_block(
        &mut self,
        index: i64,
        block: &Block,
    ) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let Block::ApplyPatchToolResult {
            tool_use_id,
            status,
            output,
            ..
        } = block
        else {
            unreachable!()
        };
        if tool_use_id.is_empty() {
            return Err(Error::new(
                "responses: apply_patch_call_output requires nonempty tool_use_id",
            ));
        }
        let mut out = Vec::new();
        if let Some(active) = &self.active_item {
            match active.kind {
                StreamOutputItemKind::Message => out.push(self.close_message_item()),
                StreamOutputItemKind::Reasoning => out.push(self.close_reasoning_item()),
                _ => {
                    return Err(Error::new(
                        "responses: apply_patch_call_output cannot open before active item completes",
                    ));
                }
            }
        }
        let output_index = self.next_output_index;
        self.next_output_index += 1;
        let item_id = format!("apco_{}", self.next_apply_patch_result_item);
        self.next_apply_patch_result_item += 1;
        self.active_item = Some(StreamOutputItem {
            kind: StreamOutputItemKind::ApplyPatchResult,
            id: item_id.clone(),
            output_index,
            content: Vec::new(),
            next_content_index: 0,
            call_id: tool_use_id.clone(),
            name: String::new(),
        });
        self.active_block = Some(StreamEncodeBlock {
            index,
            kind: StreamOutputItemKind::ApplyPatchResult,
            content_index: 0,
            text: String::new(),
            tool_input: String::new(),
            fragments: Vec::new(),
        });
        self.active_apply_patch_result = Some(block.clone());
        let status_str = match status {
            oxa_ir::ApplyPatchResultStatus::Completed => "completed",
            oxa_ir::ApplyPatchResultStatus::Failed => "failed",
        };
        out.push(StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED.to_string(),
            output_index: Some(output_index),
            item: Some(OutputItem {
                id: item_id,
                kind: "apply_patch_call_output".to_string(),
                call_id: tool_use_id.clone(),
                status: status_str.to_string(),
                output: output.as_ref().map(|s| OutputValue::Text(s.clone())),
                ..OutputItem::default()
            }),
            ..Default::default()
        });
        Ok((out, Vec::new()))
    }

    fn stop_apply_patch_result_block(&mut self) -> Result<(Vec<StreamEvent>, Vec<Loss>), Error> {
        let Some(active_item) = &mut self.active_item else {
            return Err(Error::new(
                "responses: apply_patch_call_output block without active item",
            ));
        };
        let Some(Block::ApplyPatchToolResult {
            tool_use_id,
            status,
            output,
            ..
        }) = self.active_apply_patch_result.take()
        else {
            return Err(Error::new(
                "responses: apply_patch_call_output missing block",
            ));
        };
        self.active_block = None;
        let status_str = match status {
            oxa_ir::ApplyPatchResultStatus::Completed => "completed",
            oxa_ir::ApplyPatchResultStatus::Failed => "failed",
        };
        let completed = OutputItem {
            id: active_item.id.clone(),
            kind: "apply_patch_call_output".to_string(),
            call_id: tool_use_id,
            status: status_str.to_string(),
            output: output.map(OutputValue::Text),
            ..OutputItem::default()
        };
        let out = vec![StreamEvent {
            kind: EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE.to_string(),
            output_index: Some(active_item.output_index),
            item: Some(completed.clone()),
            ..Default::default()
        }];
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

    fn open_reasoning_item(&mut self) -> (StreamOutputItem, StreamEvent) {
        let id = stream_generated_item_id("rs", self.next_reasoning_item);
        self.next_reasoning_item += 1;
        let output_index = self.next_output_index;
        self.next_output_index += 1;
        let item = StreamOutputItem {
            kind: StreamOutputItemKind::Reasoning,
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
                kind: ITEM_TYPE_REASONING.to_string(),
                status: STATUS_IN_PROGRESS.to_string(),
                ..Default::default()
            }),
            ..Default::default()
        };
        (item, event)
    }

    fn close_reasoning_item(&mut self) -> StreamEvent {
        let item = self
            .active_item
            .take()
            .expect("active reasoning item to close");
        let completed = OutputItem {
            id: item.id,
            kind: ITEM_TYPE_REASONING.to_string(),
            status: STATUS_COMPLETED.to_string(),
            summary: item.content,
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
                input_token_details: usage.input_tokens_details.map(|details| {
                    InputTokenDetailsWire {
                        cached_tokens: details.cached_tokens,
                    }
                }),
                output_token_details: usage.output_tokens_details.map(|details| {
                    OutputTokenDetailsWire {
                        reasoning_tokens: details.reasoning_tokens,
                    }
                }),
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
