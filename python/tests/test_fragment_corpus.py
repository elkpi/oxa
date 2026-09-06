import json
import unittest
from pathlib import Path
from typing import Any

from oxa.anthropic.messages import StreamDecoder as AnthropicStreamDecoder
from oxa.ir import (
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    EventStream,
    InputJsonDelta,
    MessageDone,
    MessageStart,
    ToolUseBlock,
    validate_event_stream,
)
from oxa.openai.chatcompletions import StreamDecoder as ChatCompletionsStreamDecoder
from oxa.openai.responses import StreamDecoder as ResponsesStreamDecoder


class FragmentCorpusTest(unittest.TestCase):
    def test_stream_fragment_corpus(self) -> None:
        root = Path(__file__).resolve().parents[2]
        corpus_path = root / "testdata" / "stream-fragment-corpus.json"
        if not corpus_path.is_file():
            self.skipTest("repository root with testdata/stream-fragment-corpus.json not found")
        corpus = json.loads(corpus_path.read_text())
        self.assertEqual(corpus["version"], 1)
        for case in corpus["cases"]:
            with self.subTest(case=case["id"]):
                self.assertIn(case["protocol"], {"chatcompletions", "responses", "anthropic"})
                self.assertEqual("".join(case["fragments"]), case["argument"])
                if case["protocol"] == "chatcompletions":
                    events = self._decode_chat(case)
                elif case["protocol"] == "responses":
                    events = self._decode_responses(case)
                else:
                    events = self._decode_anthropic(case)
                validate_event_stream(EventStream(events))
                tool_blocks = [
                    event.block
                    for event in events
                    if isinstance(event, ContentBlockStart)
                    and isinstance(event.block, ToolUseBlock)
                ]
                self.assertEqual(len(tool_blocks), 1)
                self.assertEqual(tool_blocks[0].input, case["argument"])
                fragments = [
                    event.delta.partial_json
                    for event in events
                    if isinstance(event, ContentBlockDelta)
                    and isinstance(event.delta, InputJsonDelta)
                ]
                self.assertEqual(fragments, case["fragments"])
                self.assertIsInstance(events[0], MessageStart)
                self.assertIsInstance(events[-1], MessageDone)

    def _decode_chat(self, case: dict[str, Any]) -> list[Any]:
        decoder = ChatCompletionsStreamDecoder()
        events: list[Any] = []
        base = {
            "id": "chatcmpl-corpus",
            "object": "chat.completion.chunk",
            "created": 0,
            "model": "gpt-4o-mini",
        }

        def feed(delta: dict[str, Any], finish_reason: str | None = None) -> None:
            events.extend(
                decoder.feed(
                    {
                        **base,
                        "choices": [
                            {
                                "index": 0,
                                "delta": delta,
                                "finish_reason": finish_reason,
                            }
                        ],
                    }
                )
            )

        feed({"role": "assistant"})
        for index, fragment in enumerate(case["fragments"]):
            call: dict[str, Any] = {
                "index": 0,
                "function": {"arguments": fragment},
            }
            if index == 0:
                call.update(
                    {
                        "id": "call-corpus",
                        "type": "function",
                        "function": {"name": "corpus_tool", "arguments": fragment},
                    }
                )
            feed({"tool_calls": [call]})
        feed({}, "tool_calls")
        events.extend(decoder.flush())
        return events

    def _decode_responses(self, case: dict[str, Any]) -> list[Any]:
        decoder = ResponsesStreamDecoder()
        events: list[Any] = []

        def feed(event: dict[str, Any]) -> None:
            events.extend(decoder.feed(event))

        feed(
            {
                "type": "response.created",
                "response": {
                    "id": "resp-corpus",
                    "object": "response",
                    "status": "in_progress",
                    "model": "gpt-4o-mini",
                    "output": [],
                },
            }
        )
        feed(
            {
                "type": "response.output_item.added",
                "output_index": 0,
                "item": {
                    "type": "function_call",
                    "id": "fc-corpus",
                    "call_id": "call-corpus",
                    "name": "corpus_tool",
                    "status": "in_progress",
                    "arguments": case["fragments"][0],
                },
            }
        )
        for fragment in case["fragments"][1:]:
            feed(
                {
                    "type": "response.function_call_arguments.delta",
                    "item_id": "fc-corpus",
                    "output_index": 0,
                    "delta": fragment,
                }
            )
        feed(
            {
                "type": "response.function_call_arguments.done",
                "item_id": "fc-corpus",
                "output_index": 0,
                "call_id": "call-corpus",
                "name": "corpus_tool",
                "arguments": case["argument"],
            }
        )
        feed(
            {
                "type": "response.output_item.done",
                "output_index": 0,
                "item": {
                    "type": "function_call",
                    "id": "fc-corpus",
                    "call_id": "call-corpus",
                    "name": "corpus_tool",
                    "status": "completed",
                    "arguments": case["argument"],
                },
            }
        )
        feed(
            {
                "type": "response.completed",
                "response": {
                    "id": "resp-corpus",
                    "object": "response",
                    "status": "completed",
                    "model": "gpt-4o-mini",
                    "output": [],
                    "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
                },
            }
        )
        events.extend(decoder.flush())
        return events

    def _decode_anthropic(self, case: dict[str, Any]) -> list[Any]:
        decoder = AnthropicStreamDecoder()
        events: list[Any] = []

        def feed(event: dict[str, Any]) -> None:
            events.extend(decoder.feed(event))

        feed(
            {
                "type": "message_start",
                "message": {
                    "id": "msg-corpus",
                    "type": "message",
                    "role": "assistant",
                    "model": "claude-sonnet-4-5",
                    "content": [],
                    "stop_reason": None,
                    "usage": {"input_tokens": 0, "output_tokens": 0},
                },
            }
        )
        feed(
            {
                "type": "content_block_start",
                "index": 0,
                "content_block": {
                    "type": "tool_use",
                    "id": "toolu-corpus",
                    "name": "corpus_tool",
                    "input": {},
                },
            }
        )
        for fragment in case["fragments"]:
            feed(
                {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "input_json_delta", "partial_json": fragment},
                }
            )
        feed({"type": "content_block_stop", "index": 0})
        feed(
            {
                "type": "message_delta",
                "delta": {"stop_reason": "tool_use"},
                "usage": {"input_tokens": 1, "output_tokens": 1},
            }
        )
        feed({"type": "message_stop"})
        events.extend(decoder.flush())
        return events


if __name__ == "__main__":
    unittest.main()
