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

    def test_non_positive_budget_is_a_structural_error(self) -> None:
        for budget in (0, -1, -100):
            wire = {
                "model": "claude-sonnet-4-5",
                "max_tokens": 100,
                "messages": [{"role": "user", "content": "Hello"}],
                "thinking": {"type": "enabled", "budget_tokens": budget},
            }
            with self.assertRaises(ValueError) as cm:
                decode_request(wire)
            self.assertIn("budget_tokens", str(cm.exception))
        wire_missing = {
            "model": "claude-sonnet-4-5",
            "max_tokens": 100,
            "messages": [{"role": "user", "content": "Hello"}],
            "thinking": {"type": "enabled"},
        }
        with self.assertRaises(ValueError) as cm:
            decode_request(wire_missing)
        self.assertIn("budget_tokens", str(cm.exception))

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
        self.assertTrue(
            any(l.reason == LOSS_UNMAPPED_VALUE and l.field == "stop_reason" for l in losses)
        )

    def test_mid_conversation_system_valid(self) -> None:
        wire = {
            "model": "claude-opus-5-5",
            "max_tokens": 1024,
            "system": [{"type": "text", "text": "Base."}],
            "messages": [
                {"role": "user", "content": "Hi"},
                {"role": "assistant", "content": "Hello"},
                {"role": "user", "content": "Summarize"},
                {"role": "system", "content": "Terse mode."},
                {
                    "role": "system",
                    "content": [
                        {
                            "type": "text",
                            "text": "Under 20 words.",
                            "cache_control": {"type": "ephemeral"},
                        }
                    ],
                },
            ],
        }
        req, losses = decode_request(wire)
        self.assertEqual(len(req.messages), 3)
        self.assertEqual(len(req.system), 3)
        self.assertEqual(req.system[0].text, "Base.")
        self.assertEqual(req.system[1].text, "Terse mode.")
        self.assertEqual(req.system[2].text, "Under 20 words.")
        self.assertEqual(len(req.system[2].native), 1)
        self.assertEqual(req.system[2].native[0].kind, "cache_control")

        self.assertEqual(len(losses), 2)
        self.assertEqual(losses[0].path, "messages[3].role")
        self.assertEqual(losses[0].field, "role")
        self.assertEqual(losses[0].reason, LOSS_DEGRADED)
        self.assertIn("carried as request-level system content", losses[0].detail)
        self.assertEqual(losses[1].path, "messages[4].role")
        self.assertEqual(losses[1].field, "role")
        self.assertEqual(losses[1].reason, LOSS_DEGRADED)

    def test_mid_conversation_system_loss_order(self) -> None:
        wire = {
            "model": "claude-opus-5-5",
            "max_tokens": 1024,
            "messages": [
                {"role": "user", "content": "Hi"},
                {
                    "role": "system",
                    "content": [
                        {"type": "text", "text": "x", "cache_control": {"type": "ephemeral", "ttl": "10m"}}
                    ],
                },
            ],
        }
        req, losses = decode_request(wire)
        self.assertEqual(len(req.system), 1)
        self.assertEqual(len(losses), 2)
        self.assertEqual(losses[0].path, "messages[1].role")
        self.assertEqual(losses[0].reason, LOSS_DEGRADED)
        self.assertEqual(losses[1].path, "messages[1].content[0].cache_control.ttl")
        self.assertEqual(losses[1].reason, LOSS_UNMAPPED_VALUE)

    def test_mid_conversation_system_placement_errors(self) -> None:
        # at index 0
        with self.assertRaises(ValueError) as cm:
            decode_request({
                "model": "claude-opus-5-5",
                "max_tokens": 1024,
                "messages": [
                    {"role": "system", "content": "Init"},
                    {"role": "user", "content": "Hi"},
                ],
            })
        self.assertIn("messages[0]: system message cannot appear at messages[0]", str(cm.exception))

        # followed by user
        with self.assertRaises(ValueError) as cm:
            decode_request({
                "model": "claude-opus-5-5",
                "max_tokens": 1024,
                "messages": [
                    {"role": "user", "content": "Hi"},
                    {"role": "system", "content": "Mid"},
                    {"role": "user", "content": "Another"},
                ],
            })
        self.assertIn("system message must be followed by an assistant turn or be the last message", str(cm.exception))

        # preceded by assistant
        with self.assertRaises(ValueError) as cm:
            decode_request({
                "model": "claude-opus-5-5",
                "max_tokens": 1024,
                "messages": [
                    {"role": "user", "content": "Hi"},
                    {"role": "assistant", "content": "Answer"},
                    {"role": "system", "content": "Mid"},
                ],
            })
        self.assertIn("system message must follow a user turn", str(cm.exception))

    def test_mid_conversation_system_content_errors(self) -> None:
        with self.assertRaises(ValueError) as cm:
            decode_request({
                "model": "claude-opus-5-5",
                "max_tokens": 1024,
                "messages": [
                    {"role": "user", "content": "Hi"},
                    {
                        "role": "system",
                        "content": [
                            {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "abc"}}
                        ],
                    },
                ],
            })
        self.assertIn("system content must be text", str(cm.exception))


if __name__ == "__main__":
    unittest.main()
