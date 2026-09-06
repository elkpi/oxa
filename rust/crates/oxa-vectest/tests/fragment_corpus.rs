use std::fs;
use std::path::{Path, PathBuf};

use oxa_anthropic::{Config as AnthropicConfig, StreamDecoder as AnthropicDecoder};
use oxa_chatcompletions::{Config as ChatConfig, StreamDecoder as ChatDecoder};
use oxa_ir::{Block, Delta, Event, EventStream};
use oxa_responses::{Config as ResponsesConfig, StreamDecoder as ResponsesDecoder};
use serde::Deserialize;
use serde_json::{Value, json};

#[derive(Debug, Deserialize)]
struct Corpus {
    version: u32,
    cases: Vec<Case>,
}

#[derive(Debug, Deserialize)]
struct Case {
    id: String,
    protocol: String,
    argument: String,
    fragments: Vec<String>,
}

#[test]
fn stream_fragment_corpus() {
    let Some(root) = repository_root() else {
        eprintln!("skipping stream fragment corpus: repository root not found");
        return;
    };
    let path = root.join("testdata/stream-fragment-corpus.json");
    let corpus: Corpus = serde_json::from_slice(
        &fs::read(&path).unwrap_or_else(|err| panic!("read {}: {err}", path.display())),
    )
    .unwrap_or_else(|err| panic!("parse {}: {err}", path.display()));
    assert_eq!(corpus.version, 1);

    for case in &corpus.cases {
        assert_eq!(
            case.fragments.concat(),
            case.argument,
            "{} fragments",
            case.id
        );
        let events = match case.protocol.as_str() {
            "chatcompletions" => decode_chat(case),
            "responses" => decode_responses(case),
            "anthropic" => decode_anthropic(case),
            other => panic!("{} has unsupported protocol {other}", case.id),
        };
        oxa_ir::validate_event_stream(&EventStream {
            events: events.clone(),
        })
        .unwrap_or_else(|violation| panic!("{}: {violation:?}", case.id));

        let tool_blocks: Vec<&str> = events
            .iter()
            .filter_map(|event| match event {
                Event::ContentBlockStart {
                    block: Block::ToolUse { input, .. },
                    ..
                } => Some(input.as_str()),
                _ => None,
            })
            .collect();
        assert_eq!(
            tool_blocks,
            vec![case.argument.as_str()],
            "{} tool input",
            case.id
        );

        let fragments: Vec<&str> = events
            .iter()
            .filter_map(|event| match event {
                Event::ContentBlockDelta {
                    delta: Delta::InputJsonDelta { partial_json },
                    ..
                } => Some(partial_json.as_str()),
                _ => None,
            })
            .collect();
        let expected: Vec<&str> = case.fragments.iter().map(String::as_str).collect();
        assert_eq!(fragments, expected, "{} fragments", case.id);
        assert!(matches!(events.first(), Some(Event::MessageStart { .. })));
        assert!(matches!(events.last(), Some(Event::MessageDone {})));
    }
}

fn repository_root() -> Option<PathBuf> {
    let mut current = Path::new(env!("CARGO_MANIFEST_DIR")).to_path_buf();
    loop {
        if current.join(".git").exists()
            && current
                .join("testdata/stream-fragment-corpus.json")
                .is_file()
        {
            return Some(current);
        }
        if !current.pop() {
            return None;
        }
    }
}

fn decode_chat(case: &Case) -> Vec<Event> {
    let mut decoder = ChatDecoder::new(&ChatConfig::default());
    let mut events = Vec::new();
    feed_chat(
        &mut decoder,
        &mut events,
        json!({
            "id": "chatcmpl-corpus", "object": "chat.completion.chunk", "created": 0,
            "model": "gpt-4o-mini", "choices": [{"index": 0, "delta": {"role": "assistant"}, "finish_reason": null}]
        }),
    );
    for (index, fragment) in case.fragments.iter().enumerate() {
        let mut call = json!({"index": 0, "function": {"arguments": fragment}});
        if index == 0 {
            call["id"] = Value::String("call-corpus".to_string());
            call["type"] = Value::String("function".to_string());
            call["function"] = json!({"name": "corpus_tool", "arguments": fragment});
        }
        feed_chat(
            &mut decoder,
            &mut events,
            json!({
                "id": "chatcmpl-corpus", "object": "chat.completion.chunk", "created": 0,
                "model": "gpt-4o-mini", "choices": [{"index": 0, "delta": {"tool_calls": [call]}, "finish_reason": null}]
            }),
        );
    }
    feed_chat(
        &mut decoder,
        &mut events,
        json!({
            "id": "chatcmpl-corpus", "object": "chat.completion.chunk", "created": 0,
            "model": "gpt-4o-mini", "choices": [{"index": 0, "delta": {}, "finish_reason": "tool_calls"}]
        }),
    );
    events.extend(decoder.flush().unwrap());
    events
}

fn feed_chat(decoder: &mut ChatDecoder, events: &mut Vec<Event>, value: Value) {
    let chunk = serde_json::from_value(value).unwrap();
    events.extend(decoder.feed(&chunk).unwrap());
}

fn decode_responses(case: &Case) -> Vec<Event> {
    let mut decoder = ResponsesDecoder::new(&ResponsesConfig::default());
    let mut events = Vec::new();
    feed_responses(
        &mut decoder,
        &mut events,
        json!({
            "type": "response.created",
            "response": {"id": "resp-corpus", "object": "response", "status": "in_progress", "model": "gpt-4o-mini", "output": []}
        }),
    );
    feed_responses(
        &mut decoder,
        &mut events,
        json!({
            "type": "response.output_item.added", "output_index": 0,
            "item": {"type": "function_call", "id": "fc-corpus", "call_id": "call-corpus", "name": "corpus_tool", "status": "in_progress", "arguments": case.fragments[0]}
        }),
    );
    for fragment in case.fragments.iter().skip(1) {
        feed_responses(
            &mut decoder,
            &mut events,
            json!({"type": "response.function_call_arguments.delta", "item_id": "fc-corpus", "output_index": 0, "delta": fragment}),
        );
    }
    feed_responses(
        &mut decoder,
        &mut events,
        json!({"type": "response.function_call_arguments.done", "item_id": "fc-corpus", "output_index": 0, "call_id": "call-corpus", "name": "corpus_tool", "arguments": case.argument}),
    );
    feed_responses(
        &mut decoder,
        &mut events,
        json!({
            "type": "response.output_item.done", "output_index": 0,
            "item": {"type": "function_call", "id": "fc-corpus", "call_id": "call-corpus", "name": "corpus_tool", "status": "completed", "arguments": case.argument}
        }),
    );
    feed_responses(
        &mut decoder,
        &mut events,
        json!({
            "type": "response.completed",
            "response": {"id": "resp-corpus", "object": "response", "status": "completed", "model": "gpt-4o-mini", "output": [], "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
        }),
    );
    events.extend(decoder.flush().unwrap());
    events
}

fn feed_responses(decoder: &mut ResponsesDecoder, events: &mut Vec<Event>, value: Value) {
    let event = serde_json::from_value(value).unwrap();
    events.extend(decoder.feed(&event).unwrap());
}

fn decode_anthropic(case: &Case) -> Vec<Event> {
    let mut decoder = AnthropicDecoder::new(&AnthropicConfig::default());
    let mut events = Vec::new();
    feed_anthropic(
        &mut decoder,
        &mut events,
        json!({
            "type": "message_start",
            "message": {"id": "msg-corpus", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5", "content": [], "stop_reason": null, "usage": {"input_tokens": 0, "output_tokens": 0}}
        }),
    );
    feed_anthropic(
        &mut decoder,
        &mut events,
        json!({
            "type": "content_block_start", "index": 0,
            "content_block": {"type": "tool_use", "id": "toolu-corpus", "name": "corpus_tool", "input": {}}
        }),
    );
    for fragment in &case.fragments {
        feed_anthropic(
            &mut decoder,
            &mut events,
            json!({"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": fragment}}),
        );
    }
    feed_anthropic(
        &mut decoder,
        &mut events,
        json!({"type": "content_block_stop", "index": 0}),
    );
    feed_anthropic(
        &mut decoder,
        &mut events,
        json!({"type": "message_delta", "delta": {"stop_reason": "tool_use"}, "usage": {"input_tokens": 1, "output_tokens": 1}}),
    );
    feed_anthropic(&mut decoder, &mut events, json!({"type": "message_stop"}));
    events.extend(decoder.flush().unwrap());
    events
}

fn feed_anthropic(decoder: &mut AnthropicDecoder, events: &mut Vec<Event>, value: Value) {
    let event = serde_json::from_value(value).unwrap();
    events.extend(decoder.feed(&event).unwrap());
}
