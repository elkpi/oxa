"""Nonstream behavior tests beyond golden vectors: refusal errors, raw json arguments, reordering."""

import unittest

from oxa.ir import (
    LOSS_DEGRADED,
    ROLE_ASSISTANT,
    ROLE_USER,
    STOP_OTHER,
    STOP_REFUSAL,
    Message,
    Request,
    Response,
    TextBlock,
    ToolResultBlock,
    ToolUseBlock,
    Usage,
)
from oxa.openai.responses import (
    decode_request,
    decode_response,
    encode_request,
    encode_response,
)


class ResponsesNonstreamTest(unittest.TestCase):
    def test_decodes_function_call_arguments_without_normalizing_their_json_text(self) -> None:
        wire = {
            "model": "gpt-4o-mini",
            "input": [
                {
                    "type": "function_call",
                    "call_id": "call_1",
                    "name": "weather",
                    "arguments": '{"temperature":1e+01}',
                }
            ],
        }
        req, losses = decode_request(wire)
        self.assertEqual(len(losses), 0)
        self.assertEqual(len(req.messages), 1)
        self.assertEqual(req.messages[0].role, ROLE_ASSISTANT)
        first_block = req.messages[0].content[0]
        self.assertIsInstance(first_block, ToolUseBlock)
        assert isinstance(first_block, ToolUseBlock)
        self.assertEqual(first_block.input, '{"temperature":1e+01}')

    def test_encodes_tool_results_before_normal_user_content_and_reports_reordering(self) -> None:
        req = Request(
            model="gpt-4o-mini",
            messages=[
                Message(
                    role=ROLE_USER,
                    content=[
                        TextBlock(text="Use the tool result."),
                        ToolResultBlock(
                            tool_use_id="call_1",
                            content=[TextBlock(text="Sunny")],
                        ),
                    ],
                )
            ],
        )
        out, losses = encode_request(req)
        self.assertTrue(any(l.reason == LOSS_DEGRADED and l.field == "ordering" for l in losses))
        self.assertEqual(len(out["input"]), 2)
        self.assertEqual(out["input"][0]["type"], "function_call_output")
        self.assertEqual(out["input"][1]["role"], "user")

    def test_encodes_refusal_with_the_required_empty_error_message(self) -> None:
        resp = Response(
            id="resp_1",
            model="gpt-4o-mini",
            content=[TextBlock(text="I cannot fulfill this request.")],
            stop_reason=STOP_REFUSAL,
            usage=Usage(input_tokens=10, output_tokens=5),
        )
        out, losses = encode_response(resp)
        self.assertEqual(out["status"], "failed")
        self.assertEqual(out["error"]["code"], "refusal")
        self.assertEqual(out["error"]["message"], "")

    def test_encoding_ir_stop_other_is_a_structural_error(self) -> None:
        resp = Response(
            id="resp_1",
            model="gpt-4o-mini",
            content=[TextBlock(text="hi")],
            stop_reason=STOP_OTHER,
            usage=Usage(input_tokens=0, output_tokens=0),
        )
        with self.assertRaises(ValueError) as cm:
            encode_response(resp)
        self.assertIn("no Responses equivalent", str(cm.exception))


if __name__ == "__main__":
    unittest.main()
