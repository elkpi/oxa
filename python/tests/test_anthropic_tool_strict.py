import json
import unittest

from oxa.anthropic.messages import decode_request, decode_request_json


class AnthropicToolStrictTest(unittest.TestCase):
    def test_ordinary_tool_rejects_non_boolean_strict(self) -> None:
        schemas = [
            {},
            {
                "type": "object",
                "properties": {"input": {"type": "string"}},
                "required": ["input"],
            },
        ]
        for schema in schemas:
            for value in (None, 0, 1, "true", [], {}):
                request = {
                    "model": "m",
                    "max_tokens": 64,
                    "messages": [{"role": "user", "content": "hi"}],
                    "tools": [{"name": "f", "input_schema": schema, "strict": value}],
                }
                for decode, payload in (
                    (decode_request, request),
                    (decode_request_json, json.dumps(request)),
                ):
                    with self.subTest(schema=schema, value=value, decode=decode.__name__):
                        with self.assertRaisesRegex(ValueError, r"tools\[0\]\.strict"):
                            decode(payload)

    def test_native_tool_keeps_opaque_strict(self) -> None:
        request = {
            "model": "m",
            "max_tokens": 64,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"type": "provider_tool", "name": "f", "strict": {"vendor": True}}],
        }
        result, losses = decode_request_json(json.dumps(request))
        self.assertEqual(json.loads(result.tools[0].definition)["strict"], {"vendor": True})
        self.assertEqual(losses, [])
