//! OpenAI Responses streaming decoder: converts Responses streaming events into IR events (face → IR).

use oxa_ir::{Block, Delta, Event, Loss, LossReason, Usage};

use crate::config::Config;
use crate::decode::decode_status;
use crate::error::Error;
use crate::normalize::loss;
use crate::types::StreamEvent;

struct StreamFunctionCall {
    call_id: String,
    name: String,
    fragments: Vec<String>,
    arguments_done: bool,
}

/// Incrementally converts an OpenAI Responses event stream into IR events.
pub struct StreamDecoder {
    config: Config,
    losses: Vec<Loss>,
    started: bool,
    terminated: bool,
    flushed: bool,
    next_output_index: i64,
    next_block_index: i64,
    item_open: bool,
    skipped_item: bool,
    item_type: String,
    item_id: String,
    skipped_call_id: String,
    output_index: i64,
    next_content_index: i64,
    function_call: Option<StreamFunctionCall>,
    tool_use_seen: bool,
    block_open: bool,
    skipped_part: bool,
    block_index: i64,
    content_index: i64,
    text_done: bool,
}

impl StreamDecoder {
    /// Creates a new decoder for a Responses event stream.
    pub fn new(config: &Config) -> Self {
        StreamDecoder {
            config: config.clone(),
            losses: Vec::new(),
            started: false,
            terminated: false,
            flushed: false,
            next_output_index: 0,
            next_block_index: 0,
            item_open: false,
            skipped_item: false,
            item_type: String::new(),
            item_id: String::new(),
            skipped_call_id: String::new(),
            output_index: 0,
            next_content_index: 0,
            function_call: None,
            tool_use_seen: false,
            block_open: false,
            skipped_part: false,
            block_index: 0,
            content_index: 0,
            text_done: false,
        }
    }

    /// Pushes one Responses streaming event and returns any completed IR events.
    pub fn feed(&mut self, ev: &StreamEvent) -> Result<Vec<Event>, Error> {
        if self.flushed {
            return Err(Error::new("responses: event fed after stream flush"));
        }
        if self.terminated {
            return Err(Error::new("responses: event fed after terminal response"));
        }

        match ev.kind.as_str() {
            "response.created" => {
                if self.started {
                    return Err(Error::new("responses: duplicate response.created"));
                }
                let Some(response) = &ev.response else {
                    return Err(Error::new("responses: response.created without response"));
                };
                self.started = true;
                Ok(vec![Event::MessageStart {
                    id: response.id.clone(),
                    model: self.config.map_model(&response.model),
                }])
            }
            "response.output_item.added" => {
                self.require_started("response.output_item.added")?;
                if self.item_open {
                    return Err(Error::new(
                        "responses: response.output_item.added with an item still open",
                    ));
                }
                let output_index = ev.output_index.unwrap_or(-1);
                if output_index != self.next_output_index {
                    return Err(Error::new(format!(
                        "responses: output_item.added output_index {}, want {}",
                        output_index, self.next_output_index
                    )));
                }
                let Some(item) = &ev.item else {
                    return Err(Error::new(
                        "responses: response.output_item.added without item",
                    ));
                };
                self.next_output_index += 1;
                self.item_open = true;
                self.item_type = item.kind.clone();
                self.output_index = output_index;
                self.item_id = item.id.clone();
                self.skipped_call_id.clear();
                self.next_content_index = 0;
                self.function_call = None;

                if item.kind == "message" && item.role == "assistant" {
                    return Ok(Vec::new());
                }
                if item.kind == "function_call" {
                    if item.id.is_empty() || item.call_id.is_empty() || item.name.is_empty() {
                        return Err(Error::new(
                            "responses: function_call item requires id, call_id, and name",
                        ));
                    }
                    self.function_call = Some(StreamFunctionCall {
                        call_id: item.call_id.clone(),
                        name: item.name.clone(),
                        fragments: vec![item.arguments.clone()],
                        arguments_done: false,
                    });
                    return Ok(Vec::new());
                }

                self.skipped_item = true;
                if item.kind == "function_call_output" {
                    self.skipped_call_id = item.call_id.clone();
                }
                self.losses
                    .push(unsupported_item_loss(output_index, &item.kind));
                Ok(Vec::new())
            }
            "response.content_part.added" => {
                self.require_active_item(ev, "response.content_part.added")?;
                if self.function_call.is_some() {
                    return Err(Error::new(
                        "responses: response.content_part.added on function_call item",
                    ));
                }
                if self.block_open || self.skipped_part {
                    return Err(Error::new(
                        "responses: response.content_part.added with a part still open",
                    ));
                }
                let content_index = ev.content_index.unwrap_or(-1);
                if content_index != self.next_content_index {
                    return Err(Error::new(format!(
                        "responses: content_part.added content_index {}, want {}",
                        content_index, self.next_content_index
                    )));
                }
                self.next_content_index += 1;
                self.content_index = content_index;
                let Some(part) = &ev.part else {
                    return Err(Error::new(
                        "responses: response.content_part.added without part",
                    ));
                };
                if self.skipped_item {
                    self.skipped_part = true;
                    return Ok(Vec::new());
                }
                if part.kind != "output_text" {
                    self.skipped_part = true;
                    self.losses.push(loss(
                        format!(
                            "output[{}].content[{}]",
                            ev.output_index.unwrap_or(-1),
                            content_index
                        ),
                        "type",
                        LossReason::UnsupportedSemantic,
                        format!(
                            "Responses streaming content type {:?} is not decoded in the Responses stream profile",
                            part.kind
                        ),
                    ));
                    return Ok(Vec::new());
                }
                self.block_open = true;
                self.block_index = self.next_block_index;
                self.next_block_index += 1;
                self.text_done = false;
                Ok(vec![Event::ContentBlockStart {
                    index: self.block_index,
                    block: Block::Text {
                        text: part.text.clone(),
                    },
                }])
            }
            "response.function_call_arguments.delta" => {
                if self.skipped_item {
                    self.require_active_item(ev, "response.function_call_arguments.delta")?;
                    return Ok(Vec::new());
                }
                self.require_function_call(ev, "response.function_call_arguments.delta")?;
                let fc = self.function_call.as_mut().unwrap();
                if fc.arguments_done {
                    return Err(Error::new(
                        "responses: response.function_call_arguments.delta after arguments.done",
                    ));
                }
                fc.fragments.push(ev.delta.clone().unwrap_or_default());
                Ok(Vec::new())
            }
            "response.function_call_arguments.done" => {
                if self.skipped_item {
                    self.require_active_item(ev, "response.function_call_arguments.done")?;
                    return Ok(Vec::new());
                }
                self.require_function_call(ev, "response.function_call_arguments.done")?;
                let fc = self.function_call.as_mut().unwrap();
                if fc.arguments_done {
                    return Err(Error::new(
                        "responses: duplicate response.function_call_arguments.done",
                    ));
                }
                let call_id = ev.call_id.as_deref().unwrap_or("");
                let name = ev.name.as_deref().unwrap_or("");
                let arguments = ev.arguments.as_deref().unwrap_or("");
                if call_id != fc.call_id || name != fc.name || arguments != fc.fragments.concat() {
                    return Err(Error::new(
                        "responses: response.function_call_arguments.done does not match the active function call",
                    ));
                }
                fc.arguments_done = true;
                Ok(Vec::new())
            }
            "response.output_text.delta" => {
                self.require_active_item(ev, "response.output_text.delta")?;
                if self.function_call.is_some() {
                    return Err(Error::new(
                        "responses: response.output_text.delta on function_call item",
                    ));
                }
                let content_index = ev.content_index.unwrap_or(-1);
                if self.skipped_item || self.skipped_part {
                    if content_index != self.content_index {
                        return Err(Error::new(format!(
                            "responses: output_text.delta content_index {} does not match the skipped part",
                            content_index
                        )));
                    }
                    return Ok(Vec::new());
                }
                if !self.block_open || content_index != self.content_index {
                    return Err(Error::new(
                        "responses: output_text.delta does not match the open content part",
                    ));
                }
                if self.text_done {
                    return Err(Error::new(
                        "responses: output_text.delta after output_text.done",
                    ));
                }
                Ok(vec![Event::ContentBlockDelta {
                    index: self.block_index,
                    delta: Delta::TextDelta {
                        text: ev.delta.clone().unwrap_or_default(),
                    },
                }])
            }
            "response.output_text.done" => {
                self.require_active_item(ev, "response.output_text.done")?;
                if self.function_call.is_some() {
                    return Err(Error::new(
                        "responses: response.output_text.done on function_call item",
                    ));
                }
                let content_index = ev.content_index.unwrap_or(-1);
                if self.skipped_item || self.skipped_part {
                    if content_index != self.content_index {
                        return Err(Error::new(format!(
                            "responses: output_text.done content_index {} does not match the skipped part",
                            content_index
                        )));
                    }
                    return Ok(Vec::new());
                }
                if !self.block_open || content_index != self.content_index {
                    return Err(Error::new(
                        "responses: output_text.done does not match the open content part",
                    ));
                }
                if self.text_done {
                    return Err(Error::new("responses: duplicate output_text.done"));
                }
                self.text_done = true;
                Ok(Vec::new())
            }
            "response.content_part.done" => {
                self.require_active_item(ev, "response.content_part.done")?;
                if self.function_call.is_some() {
                    return Err(Error::new(
                        "responses: response.content_part.done on function_call item",
                    ));
                }
                if ev.part.is_none() {
                    return Err(Error::new(
                        "responses: response.content_part.done without part",
                    ));
                }
                let content_index = ev.content_index.unwrap_or(-1);
                if self.skipped_item || self.skipped_part {
                    if content_index != self.content_index {
                        return Err(Error::new(format!(
                            "responses: content_part.done content_index {} does not match the skipped part",
                            content_index
                        )));
                    }
                    self.skipped_part = false;
                    return Ok(Vec::new());
                }
                if !self.block_open || content_index != self.content_index {
                    return Err(Error::new(
                        "responses: content_part.done does not match the open content part",
                    ));
                }
                if !self.text_done {
                    return Err(Error::new(
                        "responses: content_part.done before output_text.done",
                    ));
                }
                self.block_open = false;
                Ok(vec![Event::ContentBlockStop {
                    index: self.block_index,
                }])
            }
            "response.output_item.done" => {
                self.require_started("response.output_item.done")?;
                let output_index = ev.output_index.unwrap_or(-1);
                if !self.item_open || output_index != self.output_index {
                    return Err(Error::new(
                        "responses: response.output_item.done does not match the open item",
                    ));
                }
                let Some(item) = &ev.item else {
                    return Err(Error::new(
                        "responses: response.output_item.done does not match the open item",
                    ));
                };
                if item.id != self.item_id || item.kind != self.item_type {
                    return Err(Error::new(
                        "responses: response.output_item.done does not match the open item",
                    ));
                }
                if self.skipped_item
                    && self.item_type == "function_call_output"
                    && item.call_id != self.skipped_call_id
                {
                    return Err(Error::new(
                        "responses: response.output_item.done does not match the active function_call_output",
                    ));
                }
                if self.block_open || self.skipped_part {
                    return Err(Error::new(
                        "responses: response.output_item.done with a content part still open",
                    ));
                }
                let mut events = Vec::new();
                if let Some(fc) = self.function_call.take() {
                    let joined = fc.fragments.concat();
                    if item.call_id != fc.call_id
                        || item.name != fc.name
                        || item.arguments != joined
                    {
                        return Err(Error::new(
                            "responses: response.output_item.done does not match the active function call",
                        ));
                    }
                    let index = self.next_block_index;
                    self.next_block_index += 1;
                    events.push(Event::ContentBlockStart {
                        index,
                        block: Block::ToolUse {
                            id: fc.call_id,
                            name: fc.name,
                            input: joined,
                        },
                    });
                    for fragment in fc.fragments {
                        events.push(Event::ContentBlockDelta {
                            index,
                            delta: Delta::InputJsonDelta {
                                partial_json: fragment,
                            },
                        });
                    }
                    events.push(Event::ContentBlockStop { index });
                    self.tool_use_seen = true;
                }
                self.item_open = false;
                self.item_type.clear();
                self.item_id.clear();
                self.skipped_call_id.clear();
                self.skipped_item = false;
                self.function_call = None;
                Ok(events)
            }
            "response.completed" | "response.incomplete" | "response.failed" => {
                self.require_started(&ev.kind)?;
                if self.item_open || self.block_open || self.skipped_part {
                    return Err(Error::new(format!(
                        "responses: {} before output lifecycle completed",
                        ev.kind
                    )));
                }
                let Some(response) = &ev.response else {
                    return Err(Error::new(format!(
                        "responses: {} without response",
                        ev.kind
                    )));
                };
                let (stop, status_losses) = decode_status(response, self.tool_use_seen)?;
                self.losses.extend(status_losses);
                self.terminated = true;
                let usage = response
                    .usage
                    .map(|u| Usage {
                        input_tokens: u.input_tokens,
                        output_tokens: u.output_tokens,
                    })
                    .unwrap_or(Usage {
                        input_tokens: 0,
                        output_tokens: 0,
                    });
                Ok(vec![
                    Event::MessageDelta {
                        stop_reason: stop,
                        stop_sequence: None,
                        usage,
                    },
                    Event::MessageDone {},
                ])
            }
            _ => {
                self.losses.push(loss(
                    "type",
                    "type",
                    LossReason::UnsupportedSemantic,
                    format!(
                        "Responses stream event type {:?} is not decoded in the Responses stream profile",
                        ev.kind
                    ),
                ));
                Ok(Vec::new())
            }
        }
    }

    fn require_started(&self, event_type: &str) -> Result<(), Error> {
        if !self.started {
            return Err(Error::new(format!(
                "responses: {event_type} before response.created"
            )));
        }
        Ok(())
    }

    fn require_active_item(&self, ev: &StreamEvent, event_type: &str) -> Result<(), Error> {
        self.require_started(event_type)?;
        let output_index = ev.output_index.unwrap_or(-1);
        let item_id = ev.item_id.as_deref().unwrap_or("");
        if !self.item_open || output_index != self.output_index || item_id != self.item_id {
            return Err(Error::new(format!(
                "responses: {event_type} does not match the open output item"
            )));
        }
        Ok(())
    }

    fn require_function_call(&self, ev: &StreamEvent, event_type: &str) -> Result<(), Error> {
        self.require_active_item(ev, event_type)?;
        if self.function_call.is_none() {
            return Err(Error::new(format!(
                "responses: {event_type} without an active function_call item"
            )));
        }
        Ok(())
    }

    /// Confirms that the stream completed with a terminal response event.
    pub fn flush(&mut self) -> Result<Vec<Event>, Error> {
        if self.flushed {
            return Err(Error::new("responses: stream flushed twice"));
        }
        if !self.terminated {
            return Err(Error::new(
                "responses: stream ended without a terminal response event",
            ));
        }
        self.flushed = true;
        Ok(Vec::new())
    }

    /// Returns the accumulated losses across the stream.
    pub fn losses(&self) -> &[Loss] {
        &self.losses
    }
}

fn unsupported_item_loss(output_index: i64, item_type: &str) -> Loss {
    let detail = if item_type == "function_call_output" {
        "N-S-10: Responses function_call_output has no supported IR block mapping; response.output_item.done completes and is absorbed for this item-only lifecycle vector".to_string()
    } else {
        format!("Responses streaming output item type {item_type:?} is not decoded")
    };
    loss(
        format!("output[{output_index}]"),
        "type",
        LossReason::UnsupportedSemantic,
        detail,
    )
}
