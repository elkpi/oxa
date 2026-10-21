import json
import unittest

from oxa.anthropic.messages.decode import decode_request, decode_request_json
from oxa.anthropic.messages.encode import encode_request
from oxa.ir.codec import dump_request, load_request
from oxa.ir.types import Request as IrRequest, ToolChoice as IrToolChoice
from oxa.openai.chatcompletions.encode import encode_request as encode_cc_request
from oxa.openai.responses.encode import encode_request as encode_re_request


class NativeToolsTest(unittest.TestCase):
    def test_native_tool_declaration_fidelity(self) -> None:
        definition = '{ "type" : "advisor_20260301", "name":"advisor", "model":"opus", "max_uses":2, "vendor":{"n":1e+03}, "cache_control":{"type":"ephemeral"} }'
        raw = f'{{"model":"m","max_tokens":32,"tools":[{definition}],"messages":[{{"role":"user","content":"hi"}}]}}'
        req, losses = decode_request_json(raw)
        self.assertEqual(len(losses), 0)
        self.assertEqual(len(req.tools or []), 1)
        tool = req.tools[0]
        self.assertEqual(tool.type, "native")
        self.assertEqual(tool.name, "advisor")
        self.assertEqual(tool.source, "anthropic")
        self.assertEqual(tool.definition, definition)

        encoded_ir = dump_request(req)
        restored = load_request(encoded_ir)
        self.assertEqual(restored.tools[0].definition, definition)

        wire, encode_losses = encode_request(restored)
        self.assertEqual(len(encode_losses), 0)
        tool_wire = wire["tools"][0]
        self.assertEqual(tool_wire["model"], "opus")
        self.assertEqual(tool_wire["max_uses"], 2)
        self.assertEqual(tool_wire["vendor"], {"n": 1000})

    def test_malformed_native_declarations_rejected(self) -> None:
        invalid = [
            '{"name":"function"}',
            '{"name":"function","input_schema":[]}',
            '{"type":null,"name":"function","input_schema":{}}',
            '{"type":7,"name":"function","input_schema":{}}',
            '{"type":"advisor_20260301","name":""}',
            '{"type":"advisor_20260301","name":7}',
        ]
        for t in invalid:
            with self.subTest(tool=t):
                raw = f'{{"model":"m","max_tokens":32,"tools":[{t}],"messages":[{{"role":"user","content":"hi"}}]}}'
                with self.assertRaises(Exception):
                    decode_request_json(raw)

    def test_native_tool_choice_without_explicit_tool_type(self) -> None:
        raw = '{"model":"m","max_tokens":32,"tools":[{"type":"advisor_20260301","name":"advisor","model":"opus"}],"tool_choice":{"type":"tool","name":"advisor"},"messages":[{"role":"user","content":"hi"}]}'
        req, _ = decode_request_json(raw)
        choice = IrToolChoice(mode="tool", name="advisor")
        req_with_plain_choice = IrRequest(
            model=req.model,
            system=req.system,
            messages=req.messages,
            tools=req.tools,
            tool_choice=choice,
            params=req.params,
        )

        cc, cc_losses = encode_cc_request(req_with_plain_choice)
        self.assertNotIn("tools", cc)
        self.assertNotIn("tool_choice", cc)
        self.assertEqual(len(cc_losses), 2)

        re, re_losses = encode_re_request(req_with_plain_choice)
        self.assertNotIn("tools", re)
        self.assertNotIn("tool_choice", re)
        self.assertEqual(len(re_losses), 2)

    def test_invalid_native_ir_rejected(self) -> None:
        valid = {
            "type": "native",
            "name": "advisor",
            "source": "anthropic",
            "definition": '{"type":"advisor_20260301","name":"advisor"}',
        }
        invalid_ir = [
            {**valid, "source": ""},
            {**valid, "definition": '{"type":"advisor_20260301","name":"other"}'},
            {**valid, "definition": "[]"},
            {**valid, "input_schema": {}},
        ]
        for t in invalid_ir:
            with self.subTest(tool=t):
                with self.assertRaises(Exception):
                    load_request({
                        "specVersion": "0.2.0",
                        "model": "m",
                        "messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}],
                        "tools": [t],
                    })


if __name__ == "__main__":
    unittest.main()
