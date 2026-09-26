"""Codec alignment tests against normative spec/01 examples and ir.schema.json."""

import json
import unittest
from typing import Any

from oxa.ir import (
    CodecError,
    ImageBlock,
    Loss,
    MessageStart,
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
    def test_legacy_request_reads_and_emits_spec2(self) -> None:
        req = load_request(SPEC_REQUEST)
        out = dump_request(req)
        expected = json.loads(SPEC_REQUEST)
        expected["specVersion"] = "0.2.0"
        self.assertEqual(out, expected)

    def test_legacy_response_reads_and_emits_spec2(self) -> None:
        resp = load_response(SPEC_RESPONSE)
        out = dump_response(resp)
        expected = json.loads(SPEC_RESPONSE)
        expected["specVersion"] = "0.2.0"
        self.assertEqual(out, expected)

    def test_legacy_event_stream_reads_and_emits_spec2(self) -> None:
        stream = load_event_stream(SPEC_EVENT_STREAM)
        out = dump_event_stream(stream)
        expected = json.loads(SPEC_EVENT_STREAM)
        expected["specVersion"] = "0.2.0"
        self.assertEqual(out, expected)
        self.assertEqual(len(stream.events), 7)
        self.assertIsInstance(stream.events[0], MessageStart)

    def test_rejects_wrong_spec_version(self) -> None:
        bad = SPEC_RESPONSE.replace('"0.1.0"', '"9.9.9"')
        with self.assertRaises(CodecError):
            load_response(bad)

    def test_spec2_thinking_block_and_delta_shapes_round_trip(self) -> None:
        block_data = {
            "type": "thinking",
            "thinking": "consider",
            "signature": "opaque-token",
        }
        self.assertEqual(dump_block(load_block(block_data)), block_data)

        thinking_delta = {"type": "thinking_delta", "text": "part"}
        signature_delta = {"type": "signature_delta", "signature": "opaque-token"}
        from oxa.ir import dump_delta, load_delta

        self.assertEqual(dump_delta(load_delta(thinking_delta)), thinking_delta)
        self.assertEqual(dump_delta(load_delta(signature_delta)), signature_delta)

    def test_dual_reads_legacy_documents_and_emits_spec2(self) -> None:
        legacy = json.loads(SPEC_REQUEST)
        legacy["messages"][1]["content"].insert(
            0,
            {"type": "thinking", "thinking": "reason", "signature": "sig"},
        )
        legacy["params"]["reasoning_effort"] = "high"

        request = load_request(legacy)
        encoded = dump_request(request)

        self.assertEqual(encoded["specVersion"], "0.2.0")
        self.assertEqual(encoded["messages"][1]["content"][0]["type"], "thinking")
        self.assertEqual(encoded["params"]["reasoning_effort"], "high")

    def test_spec2_usage_details_preserve_absence_and_zero(self) -> None:
        raw = {
            "specVersion": "0.2.0",
            "id": "r",
            "model": "m",
            "content": [],
            "stop_reason": "end_turn",
            "usage": {
                "input_tokens": 4,
                "output_tokens": 2,
                "cache_read_input_tokens": 0,
                "input_tokens_details": {"cached_tokens": 0},
                "output_tokens_details": {"reasoning_tokens": 3},
            },
        }
        response = load_response(raw)
        encoded = dump_response(response)
        self.assertEqual(encoded["specVersion"], "0.2.0")
        self.assertEqual(encoded["usage"]["cache_read_input_tokens"], 0)
        self.assertEqual(encoded["usage"]["input_tokens_details"], {"cached_tokens": 0})
        self.assertEqual(encoded["usage"]["output_tokens_details"], {"reasoning_tokens": 3})

        legacy = load_response(SPEC_RESPONSE)
        self.assertIsNone(legacy.usage.cache_read_input_tokens)
        self.assertIsNone(legacy.usage.input_tokens_details)
        self.assertIsNone(legacy.usage.output_tokens_details)

    def test_spec2_m9_event_stream_round_trips_signature_order(self) -> None:
        raw = {
            "specVersion": "0.2.0",
            "events": [
                {"type": "message_start", "id": "m", "model": "model"},
                {
                    "type": "content_block_start",
                    "index": 0,
                    "block": {"type": "thinking", "thinking": ""},
                },
                {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "thinking_delta", "text": "reason"},
                },
                {
                    "type": "content_block_delta",
                    "index": 0,
                    "delta": {"type": "signature_delta", "signature": "sig"},
                },
                {"type": "content_block_stop", "index": 0},
                {
                    "type": "message_delta",
                    "stop_reason": "end_turn",
                    "usage": {
                        "input_tokens": 1,
                        "output_tokens": 2,
                        "output_tokens_details": {"reasoning_tokens": 1},
                    },
                },
                {"type": "message_done"},
            ],
        }
        self.assertEqual(dump_event_stream(load_event_stream(raw)), raw)

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
