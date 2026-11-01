"""Regression tests for prompt_cache_options / prompt_cache_breakpoint (spec/15 §3.7)."""

import json
import unittest

from oxa.ir import (
    NativeAnnotation,
    Request,
    TextBlock,
    ThinkingBlock,
    ImageBlock,
    Message,
)
from oxa.ir.native import validate_native_request
from oxa.openai.chatcompletions import decode_request as cc_decode_request
from oxa.openai.chatcompletions import encode_request as cc_encode_request
from oxa.openai.responses import decode_request as resp_decode_request
from oxa.openai.responses import encode_request as resp_encode_request


def cc_request(**overrides):
    wire = {
        "model": "m",
        "messages": [{"role": "user", "content": "hi"}],
    }
    wire.update(overrides)
    return wire


def resp_request(**overrides):
    wire = {
        "model": "m",
        "input": "hi",
    }
    wire.update(overrides)
    return wire


class TestPromptCacheOptionsValidation(unittest.TestCase):
    def test_chatcompletions_rejects_invalid_values(self):
        for value in (
            {"mode": "bad"},
            {"ttl": "1h"},
            {"mode": 123},
            {"ttl": 30},
        ):
            with self.assertRaises(Exception):
                cc_decode_request(cc_request(prompt_cache_options=value))

    def test_responses_rejects_invalid_values(self):
        for value in (
            {"mode": "bad"},
            {"ttl": "1h"},
            {"mode": 123},
            {"ttl": 30},
        ):
            with self.assertRaises(Exception):
                resp_decode_request(resp_request(prompt_cache_options=value))

    def test_breakpoint_mode_is_optional(self):
        for value in ({}, {"ttl": "30m"}):
            req, _ = cc_decode_request(
                {
                    "model": "m",
                    "messages": [
                        {
                            "role": "user",
                            "content": [
                                {
                                    "type": "text",
                                    "text": "hi",
                                    "prompt_cache_breakpoint": value,
                                }
                            ],
                        }
                    ],
                }
            )
            self.assertEqual(len(req.messages[0].content[0].native), 1)

    def test_both_faces_reject_invalid_breakpoint_values(self):
        for value in (
            {"mode": "bad"},
            {"ttl": "wrong"},
            {"mode": 1},
        ):
            with self.assertRaises(Exception):
                cc_decode_request(
                    {
                        "model": "m",
                        "messages": [
                            {
                                "role": "user",
                                "content": [
                                    {
                                        "type": "text",
                                        "text": "hi",
                                        "prompt_cache_breakpoint": value,
                                    }
                                ],
                            }
                        ],
                    }
                )
            with self.assertRaises(Exception):
                resp_decode_request(
                    {
                        "model": "m",
                        "input": [
                            {
                                "role": "user",
                                "content": [
                                    {
                                        "type": "input_text",
                                        "text": "hi",
                                        "prompt_cache_breakpoint": value,
                                    }
                                ],
                            }
                        ],
                    }
                )


class TestResponsesSystemBreakpoint(unittest.TestCase):
    def test_system_breakpoint_emits_items_not_instructions(self):
        req, _ = cc_decode_request(
            {
                "model": "gpt-4o",
                "messages": [
                    {
                        "role": "system",
                        "content": [
                            {
                                "type": "text",
                                "text": "policy",
                                "prompt_cache_breakpoint": {"mode": "explicit"},
                            }
                        ],
                    },
                    {"role": "user", "content": "hi"},
                ],
            }
        )
        wire, losses = resp_encode_request(req)
        self.assertEqual(losses, [])
        self.assertNotIn("instructions", wire)
        items = wire["input"]
        self.assertEqual(items[0]["role"], "system")
        self.assertEqual(
            items[0]["content"],
            [
                {
                    "type": "input_text",
                    "text": "policy",
                    "prompt_cache_breakpoint": {"mode": "explicit"},
                }
            ],
        )


class TestImageBreakpointRestore(unittest.TestCase):
    IR_IMAGE = ImageBlock(
        url="https://example.com/cat.png",
        native=(
            NativeAnnotation(
                source="openai",
                kind="prompt_cache_breakpoint",
                value='{"mode":"explicit"}',
            ),
        ),
    )

    def test_chatcompletions(self):
        req = Request(
            model="gpt-4o",
            messages=(Message(role="user", content=(self.IR_IMAGE,)),),
        )
        wire, _ = cc_encode_request(req)
        parts = wire["messages"][0]["content"]
        self.assertIsInstance(parts, list)
        self.assertIn("prompt_cache_breakpoint", parts[0])

    def test_responses(self):
        req = Request(
            model="gpt-4o",
            messages=(Message(role="user", content=(self.IR_IMAGE,)),),
        )
        wire, _ = resp_encode_request(req)
        items = wire["input"]
        parts = items[0]["content"]
        self.assertIn("prompt_cache_breakpoint", parts[0])


class TestIRValidation(unittest.TestCase):
    def test_breakpoint_null_value_rejected(self):
        req = Request(
            model="m",
            messages=(
                Message(
                    role="user",
                    content=(
                        TextBlock(
                            text="hi",
                            native=(
                                NativeAnnotation(
                                    source="openai",
                                    kind="prompt_cache_breakpoint",
                                    value="null",
                                ),
                            )
                        ),
                    ),
                ),
            ),
        )
        with self.assertRaises(Exception):
            validate_native_request(req)

    def test_breakpoint_on_thinking_rejected(self):
        req = Request(
            model="m",
            messages=(
                Message(
                    role="user",
                    content=(
                        ThinkingBlock(
                            thinking="hmm",
                            native=(
                                NativeAnnotation(
                                    source="openai",
                                    kind="prompt_cache_breakpoint",
                                    value='{"mode":"explicit"}',
                                ),
                            ),
                        ),
                    ),
                ),
            ),
        )
        with self.assertRaises(Exception):
            validate_native_request(req)


if __name__ == "__main__":
    unittest.main()
