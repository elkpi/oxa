import unittest

from oxa.ir import CodecError, dump_request, load_request


class ToolStrictTest(unittest.TestCase):
    def test_canonical_round_trip_preserves_tristate(self) -> None:
        tools = [
            {"name": "omitted", "input_schema": {}},
            {"name": "non_strict", "input_schema": {}, "strict": False},
            {"name": "strict", "input_schema": {}, "strict": True},
        ]
        raw = {"specVersion": "0.2.0", "model": "m", "messages": [], "tools": tools}
        self.assertEqual(dump_request(load_request(raw))["tools"], tools)

    def test_non_function_strict_is_rejected(self) -> None:
        tools = [
            {"type": "custom", "name": "x", "strict": False},
            {"type": "apply_patch", "strict": True},
            {
                "type": "native",
                "name": "x",
                "source": "s",
                "definition": '{"type":"provider_tool","name":"x"}',
                "strict": False,
            },
        ]
        for tool in tools:
            with self.subTest(tool=tool), self.assertRaises(CodecError):
                load_request(
                    {"specVersion": "0.2.0", "model": "m", "messages": [], "tools": [tool]}
                )


if __name__ == "__main__":
    unittest.main()
