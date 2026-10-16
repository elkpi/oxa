"""Nonstream behavior tests beyond golden vectors: refusal errors, raw json arguments, reordering."""

import json
import unittest

from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNSUPPORTED_SEMANTIC,
    ROLE_ASSISTANT,
    ROLE_USER,
    STOP_OTHER,
    STOP_REFUSAL,
    CustomToolResultBlock,
    CustomToolUseBlock,
    Message,
    Request,
    Response,
    TextBlock,
    ToolResultBlock,
    ToolUseBlock,
    Tool,
    ToolChoice,
    Usage,
)
from oxa.openai.chatcompletions import encode_request as cc_encode_request, encode_response as cc_encode_response
from oxa.anthropic.messages import encode_request as an_encode_request, encode_response as an_encode_response
from oxa.openai.responses import (
    decode_request,
    decode_request_json,
    decode_response,
    encode_request,
    encode_response,
)


class ResponsesNonstreamTest(unittest.TestCase):
    def test_text_format_rejects_non_boolean_strict(self) -> None:
        wire = {
            "model": "gpt-4o",
            "input": "json",
            "text": {
                "format": {
                    "type": "json_schema",
                    "name": "result",
                    "schema": {"type": "object"},
                    "strict": "false",
                }
            },
        }
        with self.assertRaises(ValueError):
            decode_request(wire)

    def test_text_format_rejects_non_string_description(self) -> None:
        wire = {
            "model": "gpt-4o",
            "input": "json",
            "text": {
                "format": {
                    "type": "json_schema",
                    "name": "result",
                    "description": 17,
                    "schema": {"type": "object"},
                }
            },
        }
        with self.assertRaises(ValueError):
            decode_request(wire)

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

    def test_custom_tool_required_fields_are_not_defaulted(self) -> None:
        with self.assertRaises(ValueError):
            decode_request({"model": "m", "input": [{"type": "custom_tool_call", "call_id": "c", "name": "exec"}]})
        with self.assertRaises(ValueError):
            decode_request({"model": "m", "input": [{"type": "custom_tool_call_output", "call_id": "c"}]})
        with self.assertRaises(ValueError):
            decode_response({"id": "r", "model": "m", "status": "completed", "output": [{"type": "custom_tool_call", "call_id": "c", "name": "exec"}]})

    def test_custom_output_audits_supported_parts_but_not_unsupported_descendants(self) -> None:
        wire = {
            "model": "m",
            "tools": [{"type": "custom", "name": "exec", "format": {"type": "grammar", "syntax": "regex", "definition": ".*", "extra": False}}],
            "input": [
                {"role": "user", "content": "go"},
                {"type": "custom_tool_call", "call_id": "c", "name": "exec", "input": "not JSON\r\n"},
                {"type": "custom_tool_call_output", "call_id": "c", "output": [
                    {"type": "input_text", "text": "x", "image_url": None},
                    {"type": "input_file", "file_id": "f", "text": "opaque"},
                ]},
            ],
        }
        _, losses = decode_request_json(json.dumps(wire))
        self.assertTrue(any(l.reason == LOSS_UNMAPPED_FIELD and l.path == "tools[0].format.extra" for l in losses))
        self.assertTrue(any(l.reason == LOSS_UNMAPPED_FIELD and l.path == "input[2].output[0].image_url" for l in losses))
        unsupported = [l for l in losses if l.reason == LOSS_UNSUPPORTED_SEMANTIC and l.path == "input[2].output[1]"]
        self.assertEqual(len(unsupported), 1)
        self.assertFalse(any(l.path.startswith("input[2].output[1].") for l in losses))

    def test_custom_tools_downgrade_explicitly_for_unimplemented_targets(self) -> None:
        req = Request(
            model="m",
            messages=[Message(role=ROLE_USER, content=[CustomToolResultBlock(tool_use_id="c", output="")])],
            tools=[Tool(name="exec", type="custom")],
            tool_choice=ToolChoice(mode="any"),
        )
        for encoder in (cc_encode_request, an_encode_request):
            wire, losses = encoder(req)
            self.assertNotIn("tools", wire)
            self.assertNotIn("tool_choice", wire)
            self.assertTrue(any(l.reason == LOSS_UNSUPPORTED_SEMANTIC for l in losses))
            self.assertEqual(wire["messages"][0].get("content", ""), "")

        response = Response(id="r", model="m", content=[CustomToolUseBlock(id="c", name="exec", input="not JSON")], stop_reason="tool_use", usage=Usage())
        for encoder, expected in ((cc_encode_response, "stop"), (an_encode_response, "end_turn")):
            wire, losses = encoder(response)
            self.assertEqual(wire["choices"][0]["finish_reason"] if encoder is cc_encode_response else wire["stop_reason"], expected)
            self.assertTrue(any(l.reason == LOSS_DEGRADED for l in losses))

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
