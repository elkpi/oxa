"""Nonstream behavior tests beyond golden vectors: structural error boundaries, loss details, and defaults."""

import unittest

from oxa.anthropic.messages import (
    decode_request,
    decode_response,
    encode_request,
    encode_response,
)
from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_VALUE,
    ROLE_USER,
    STOP_OTHER,
    Message,
    Request,
    Response,
    TextBlock,
    Usage,
)


class AnthropicNonstreamTest(unittest.TestCase):
    def test_max_tokens_must_be_positive(self) -> None:
        wire = {
            "model": "claude-sonnet-4-5",
            "max_tokens": 0,
            "messages": [{"role": "user", "content": "Hello"}],
        }
        with self.assertRaises(ValueError) as cm:
            decode_request(wire)
        self.assertIn("max_tokens is required and must be positive", str(cm.exception))

    def test_unknown_role_is_a_structural_error(self) -> None:
        wire = {
            "model": "claude-sonnet-4-5",
            "max_tokens": 100,
            "messages": [{"role": "chief", "content": "Hello"}],
        }
        with self.assertRaises(ValueError) as cm:
            decode_request(wire)
        self.assertIn("unknown role 'chief'", str(cm.exception))

    def test_request_without_messages_is_a_structural_error(self) -> None:
        wire = {
            "model": "claude-sonnet-4-5",
            "max_tokens": 100,
            "messages": [],
        }
        with self.assertRaises(ValueError) as cm:
            decode_request(wire)
        self.assertIn("request carries no messages", str(cm.exception))

    def test_missing_stop_reason_is_a_structural_error(self) -> None:
        wire = {
            "id": "msg_1",
            "type": "message",
            "role": "assistant",
            "model": "m",
            "content": [{"type": "text", "text": "hi"}],
            "stop_reason": "",
            "usage": {"input_tokens": 0, "output_tokens": 0},
        }
        with self.assertRaises(ValueError) as cm:
            decode_response(wire)
        self.assertIn("stop_reason is missing", str(cm.exception))

    def test_missing_max_tokens_defaults_to_4096_with_degraded_loss(self) -> None:
        req = Request(
            model="claude-sonnet-4-5",
            messages=[Message(role=ROLE_USER, content=[TextBlock(text="hi")])],
        )
        out, losses = encode_request(req)
        self.assertEqual(out["max_tokens"], 4096)
        self.assertTrue(any(l.reason == LOSS_DEGRADED and l.field == "max_tokens" for l in losses))

    def test_single_text_message_renders_the_string_shorthand(self) -> None:
        req = Request(
            model="claude-sonnet-4-5",
            messages=[Message(role=ROLE_USER, content=[TextBlock(text="Hello world")])],
        )
        out, _ = encode_request(req)
        self.assertEqual(out["messages"][0]["content"], "Hello world")

    def test_encoding_ir_stop_other_is_a_structural_error(self) -> None:
        resp = Response(
            id="r",
            model="m",
            content=[TextBlock(text="hi")],
            stop_reason=STOP_OTHER,
            usage=Usage(input_tokens=0, output_tokens=0),
        )
        with self.assertRaises(ValueError) as cm:
            encode_response(resp)
        self.assertIn("no Anthropic equivalent", str(cm.exception))

    def test_unknown_stop_reason_maps_to_other_with_a_loss(self) -> None:
        wire = {
            "id": "msg_1",
            "type": "message",
            "role": "assistant",
            "model": "m",
            "content": [{"type": "text", "text": "hi"}],
            "stop_reason": "custom_stop",
            "usage": {"input_tokens": 0, "output_tokens": 0},
        }
        resp, losses = decode_response(wire)
        self.assertEqual(resp.stop_reason, STOP_OTHER)
        self.assertTrue(any(l.reason == LOSS_UNMAPPED_VALUE and l.field == "stop_reason" for l in losses))


if __name__ == "__main__":
    unittest.main()
