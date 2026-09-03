"""Codec alignment tests against normative spec/01 examples and ir.schema.json."""

import json
import unittest
from typing import Any

from oxa.ir import (
    CodecError,
    EventStream,
    ImageBlock,
    Loss,
    MessageStart,
    Request,
    Response,
    TextBlock,
    ToolResultBlock,
    ToolUseBlock,
    dump_block,
    dump_event_stream,
    dump_loss,
    dump_request,
    dump_response,
    load_block,
    load_event_stream,
    load_loss,
    load_request,
    load_response,
)

SPEC_REQUEST = """{
  "specVersion": "0.1.0",
  "model": "claude-sonnet-4-5",
  "system": [{ "type": "text", "text": "You are a concise assistant." }],
  "messages": [
    { "role": "user", "content": [
      { "type": "text", "text": "What is the weather in Paris?" }
    ]},
    { "role": "assistant", "content": [
      { "type": "text", "text": "Let me check." },
      { "type": "tool_use", "id": "toolu_01", "name": "get_weather",
        "input": "{\\"city\\":\\"Paris\\"}" }
    ]},
    { "role": "user", "content": [
      { "type": "tool_result", "tool_use_id": "toolu_01", "content": [
        { "type": "text", "text": "18 C, clear" }
      ] }
    ]}
  ],
  "tools": [
    { "name": "get_weather", "description": "Current weather for a city",
      "input_schema": { "type": "object",
        "properties": { "city": { "type": "string" } },
        "required": ["city"] } }
  ],
  "tool_choice": { "mode": "auto" },
  "params": { "temperature": 0.7, "max_tokens": 1024 }
}"""

SPEC_RESPONSE = """{
  "specVersion": "0.1.0",
  "id": "msg_017Y2hvcv",
  "model": "claude-sonnet-4-5",
  "content": [
    { "type": "text", "text": "It is 18 C and clear in Paris." }
  ],
  "stop_reason": "end_turn",
  "usage": { "input_tokens": 120, "output_tokens": 12 }
}"""

SPEC_EVENT_STREAM = """{
  "specVersion": "0.1.0",
  "events": [
    { "type": "message_start", "id": "msg_017Y2hvcv", "model": "claude-sonnet-4-5" },
    { "type": "content_block_start", "index": 0,
      "block": { "type": "text", "text": "" } },
    { "type": "content_block_delta", "index": 0,
      "delta": { "type": "text_delta", "text": "It is 18 C" } },
    { "type": "content_block_delta", "index": 0,
      "delta": { "type": "text_delta", "text": " and clear in Paris." } },
    { "type": "content_block_stop", "index": 0 },
    { "type": "message_delta", "stop_reason": "end_turn",
      "usage": { "input_tokens": 120, "output_tokens": 12 } },
    { "type": "message_done" }
  ]
}"""


class CodecTest(unittest.TestCase):
    def test_spec01_request_round_trips(self) -> None:
        req = load_request(SPEC_REQUEST)
        out = dump_request(req)
        self.assertEqual(out, json.loads(SPEC_REQUEST))

    def test_spec01_response_round_trips(self) -> None:
        resp = load_response(SPEC_RESPONSE)
        out = dump_response(resp)
        self.assertEqual(out, json.loads(SPEC_RESPONSE))

    def test_spec01_event_stream_round_trips(self) -> None:
        stream = load_event_stream(SPEC_EVENT_STREAM)
        out = dump_event_stream(stream)
        self.assertEqual(out, json.loads(SPEC_EVENT_STREAM))
        self.assertEqual(len(stream.events), 7)
        self.assertIsInstance(stream.events[0], MessageStart)

    def test_rejects_wrong_spec_version(self) -> None:
        bad = SPEC_RESPONSE.replace('"0.1.0"', '"9.9.9"')
        with self.assertRaises(CodecError):
            load_response(bad)

    def test_block_discriminant_shapes_are_pinned(self) -> None:
        cases: list[tuple[dict[str, Any], type]] = [
            ({"type": "text", "text": "hi"}, TextBlock),
            ({"type": "image", "url": "https://example.com/cat.png"}, ImageBlock),
            ({"type": "tool_use", "id": "call_1", "name": "f", "input": "{}"}, ToolUseBlock),
            (
                {
                    "type": "tool_result",
                    "tool_use_id": "call_1",
                    "content": [{"type": "text", "text": "ok"}],
                },
                ToolResultBlock,
            ),
        ]
        for data, expected_type in cases:
            block = load_block(data)
            self.assertIsInstance(block, expected_type)
            dumped = dump_block(block)
            self.assertEqual(dumped, data)

    def test_absent_and_zero_are_distinct_in_params(self) -> None:
        raw = """{
            "specVersion": "0.1.0",
            "model": "m",
            "messages": [{ "role": "user", "content": [{ "type": "text", "text": "hi" }] }],
            "params": { "max_tokens": 0 }
        }"""
        req = load_request(raw)
        out = dump_request(req)
        self.assertIn("params", out)
        self.assertEqual(out["params"].get("max_tokens"), 0)
        self.assertNotIn("temperature", out["params"])

    def test_loss_round_trip(self) -> None:
        loss = Loss(
            path="messages[0].content[1]",
            field="cache_control",
            reason="unmapped-field",
            detail="detail message",
        )
        data = dump_loss(loss)
        loaded = load_loss(data)
        self.assertEqual(loaded, loss)


if __name__ == "__main__":
    unittest.main()
