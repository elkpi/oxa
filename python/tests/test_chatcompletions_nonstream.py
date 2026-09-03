"""Nonstream behavior tests beyond golden vectors: structural error boundaries, loss details, and modelmap."""

import unittest

from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    STOP_END_TURN,
    STOP_OTHER,
    STOP_STOP_SEQUENCE,
    Message,
    Request,
    Response,
    TextBlock,
    Usage,
)
from oxa.modelmap import Table
from oxa.openai.chatcompletions import (
    decode_request,
    decode_response,
    encode_request,
    encode_response,
)


class ChatCompletionsNonstreamTest(unittest.TestCase):
    def test_unknown_role_is_a_structural_error(self) -> None:
        wire = {
            "model": "m",
            "messages": [{"role": "chief", "content": "Hello"}],
        }
        with self.assertRaises(ValueError) as cm:
            decode_request(wire)
        self.assertIn("unknown role 'chief'", str(cm.exception))

    def test_request_without_conversation_messages_is_a_structural_error(self) -> None:
        wire = {
            "model": "m",
            "messages": [{"role": "system", "content": "Be concise."}],
        }
        with self.assertRaises(ValueError) as cm:
            decode_request(wire)
        self.assertIn("request carries no conversation messages", str(cm.exception))

    def test_response_without_choices_is_a_structural_error(self) -> None:
        wire = {
            "id": "r",
            "object": "chat.completion",
            "created": 0,
            "model": "m",
            "choices": [],
        }
        with self.assertRaises(ValueError) as cm:
            decode_response(wire)
        self.assertIn("choices array is required and must not be empty", str(cm.exception))

    def test_missing_finish_reason_is_a_structural_error(self) -> None:
        wire = {
            "id": "r",
            "object": "chat.completion",
            "created": 0,
            "model": "m",
            "choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}}],
        }
        with self.assertRaises(ValueError) as cm:
            decode_response(wire)
        self.assertIn("finish_reason is missing", str(cm.exception))

    def test_model_map_applies_on_both_directions(self) -> None:
        table = Table({"gpt-4o-mini": "claude-haiku-4-5"})
        req_wire = {
            "model": "gpt-4o-mini",
            "messages": [{"role": "user", "content": "hi"}],
        }
        req_ir, _ = decode_request(req_wire, table=table)
        self.assertEqual(req_ir.model, "claude-haiku-4-5")

        encoded_req, _ = encode_request(req_ir, table=table)
        self.assertEqual(encoded_req["model"], "claude-haiku-4-5")

    def test_encoding_an_ir_stop_other_is_a_structural_error(self) -> None:
        resp = Response(
            id="r",
            model="m",
            content=[TextBlock(text="hi")],
            stop_reason=STOP_OTHER,
            usage=Usage(input_tokens=0, output_tokens=0),
        )
        with self.assertRaises(ValueError) as cm:
            encode_response(resp)
        self.assertIn("no Chat Completions equivalent", str(cm.exception))

    def test_encoding_a_stop_sequence_reports_the_value_loss(self) -> None:
        resp = Response(
            id="r",
            model="m",
            content=[TextBlock(text="hi")],
            stop_reason=STOP_STOP_SEQUENCE,
            stop_sequence="END",
            usage=Usage(input_tokens=0, output_tokens=0),
        )
        out, losses = encode_response(resp)
        self.assertEqual(out["choices"][0]["finish_reason"], "stop")
        self.assertEqual(len(losses), 1)
        self.assertEqual(losses[0].field, "stop_sequence")
        self.assertEqual(losses[0].reason, LOSS_UNMAPPED_VALUE)

    def test_non_function_tool_call_type_is_a_loss(self) -> None:
        wire = {
            "model": "m",
            "messages": [
                {
                    "role": "assistant",
                    "tool_calls": [{"id": "c1", "type": "bash", "function": {"name": "f"}}],
                },
                {"role": "user", "content": "hi"},
            ],
        }
        _, losses = decode_request(wire)
        self.assertTrue(any(l.reason == LOSS_UNSUPPORTED_SEMANTIC for l in losses))

    def test_unsupported_tool_choice_string_is_a_loss(self) -> None:
        wire = {
            "model": "m",
            "messages": [{"role": "user", "content": "hi"}],
            "tool_choice": "custom_mode",
        }
        _, losses = decode_request(wire)
        self.assertTrue(any(l.field == "tool_choice" for l in losses))


if __name__ == "__main__":
    unittest.main()
