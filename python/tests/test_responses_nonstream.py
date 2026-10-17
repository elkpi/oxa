"""Nonstream behavior tests beyond golden vectors: refusal errors, raw json arguments, reordering."""

import json
import unittest

from oxa.anthropic.messages import decode_request as an_decode_request
from oxa.anthropic.messages import encode_request as an_encode_request
from oxa.anthropic.messages import encode_response as an_encode_response
from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNSUPPORTED_SEMANTIC,
    ROLE_ASSISTANT,
    ROLE_USER,
    STOP_OTHER,
    STOP_REFUSAL,
    CustomToolFormatGrammar,
    CustomToolResultBlock,
    CustomToolUseBlock,
    Message,
    Params,
    Request,
    Response,
    TextBlock,
    Tool,
    ToolChoice,
    ToolResultBlock,
    ToolUseBlock,
    Usage,
)
from oxa.openai.chatcompletions import decode_request as cc_decode_request
from oxa.openai.chatcompletions import encode_request as cc_encode_request
from oxa.openai.chatcompletions import encode_response as cc_encode_response
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

    def test_custom_tools_supported_in_cc_and_anthropic(self) -> None:
        req = Request(
            model="m",
            messages=[Message(role=ROLE_USER, content=[CustomToolResultBlock(tool_use_id="c", output="")])],
            tools=[Tool(name="exec", type="custom")],
            tool_choice=ToolChoice(mode="any"),
            params=Params(max_tokens=1024),
        )
        for encoder in (cc_encode_request, an_encode_request):
            wire, losses = encoder(req)
            self.assertIn("tools", wire)
            self.assertIn("tool_choice", wire)
            self.assertEqual(len(losses), 0)

        response = Response(id="r", model="m", content=[CustomToolUseBlock(id="c", name="exec", input="not JSON")], stop_reason="tool_use", usage=Usage())
        for encoder, expected in ((cc_encode_response, "tool_calls"), (an_encode_response, "tool_use")):
            wire, losses = encoder(response)
            self.assertEqual(wire["choices"][0]["finish_reason"] if encoder is cc_encode_response else wire["stop_reason"], expected)
            self.assertEqual(len(losses), 0)

        patch_req = Request(
            model="m",
            messages=[Message(role=ROLE_USER, content=[TextBlock(text="hi")])],
            tools=[Tool(name="apply_patch", type="apply_patch")],
            tool_choice=ToolChoice(mode="any"),
        )
        for encoder in (cc_encode_request, an_encode_request):
            wire, losses = encoder(patch_req)
            self.assertNotIn("tools", wire)
            self.assertNotIn("tool_choice", wire)
            self.assertTrue(any(l.reason == LOSS_UNSUPPORTED_SEMANTIC for l in losses))

    def test_conflicting_function_declaration_not_promoted_to_custom(self) -> None:
        wire = {
            "model": "claude-3-5-sonnet-20241022",
            "max_tokens": 1024,
            "tools": [
                {
                    "name": "echo",
                    "input_schema": {
                        "type": "object",
                        "properties": {"input": {"type": "string"}, "extra": {"type": "string"}},
                        "required": ["input"],
                    },
                }
            ],
            "messages": [
                {"role": "user", "content": "hi"},
                {
                    "role": "assistant",
                    "content": [
                        {"type": "tool_use", "id": "call_1", "name": "echo", "input": {"input": "hello"}}
                    ],
                },
                {
                    "role": "user",
                    "content": [{"type": "tool_result", "tool_use_id": "call_1", "content": "ok"}],
                },
            ],
        }
        req, losses = an_decode_request(wire)
        self.assertEqual(len(losses), 0)
        self.assertEqual(req.tools[0].type, "function")
        self.assertIsInstance(req.messages[1].content[0], ToolUseBlock)
        self.assertIsInstance(req.messages[2].content[0], ToolResultBlock)

    def test_custom_tool_result_array_preserved(self) -> None:
        req = Request(
            model="m",
            messages=[
                Message(role=ROLE_USER, content=[TextBlock(text="hi")]),
                Message(role=ROLE_ASSISTANT, content=[CustomToolUseBlock(id="c", name="run", input="cmd")]),
                Message(
                    role=ROLE_USER,
                    content=[
                        CustomToolResultBlock(
                            tool_use_id="c",
                            output=[TextBlock(text="line 1"), TextBlock(text="line 2")],
                        )
                    ],
                ),
            ],
        )
        wire, losses = cc_encode_request(req)
        self.assertEqual(len(losses), 0)
        self.assertIsInstance(wire["messages"][2]["content"], list)
        self.assertEqual(len(wire["messages"][2]["content"]), 2)
        decoded, decode_losses = cc_decode_request(wire)
        self.assertEqual(len(decode_losses), 0)
        result = decoded.messages[2].content[0]
        self.assertIsInstance(result, CustomToolResultBlock)
        self.assertIsInstance(result.output, list)
        self.assertEqual(len(result.output), 2)

    def test_grammar_format_loss_on_anthropic_encode(self) -> None:
        req = Request(
            model="m",
            messages=[Message(role=ROLE_USER, content=[TextBlock(text="hi")])],
            tools=[
                Tool(
                    name="calc",
                    type="custom",
                    format=CustomToolFormatGrammar(syntax="regex", definition="[0-9]+"),
                )
            ],
            params=Params(max_tokens=1024),
        )
        _, losses = an_encode_request(req)
        self.assertTrue(
            any(l.path == "tools[0].format" and l.reason == LOSS_UNSUPPORTED_SEMANTIC for l in losses)
        )

    def test_chatcompletions_preserves_missing_format_and_reports_unknown_format(self) -> None:
        wire_missing = {
            "model": "m",
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"type": "custom", "custom": {"name": "c"}}],
        }
        req_missing, losses_missing = cc_decode_request(wire_missing)
        self.assertEqual(len(losses_missing), 0)
        self.assertIsNone(req_missing.tools[0].format)

        wire_unknown = {
            "model": "m",
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"type": "custom", "custom": {"name": "c", "format": {"type": "unknown_fmt"}}}],
        }
        req_unknown, losses_unknown = cc_decode_request(wire_unknown)
        self.assertTrue(any(l.reason == LOSS_UNSUPPORTED_SEMANTIC for l in losses_unknown))
        self.assertIsNone(req_unknown.tools[0].format)

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
