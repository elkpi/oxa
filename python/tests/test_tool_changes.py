import unittest

from oxa.anthropic.messages import decode_request, encode_request
from oxa.ir.codec import dump_request, load_request


class ToolChangeTests(unittest.TestCase):
    def test_added_tool_definition_reports_decode_loss(self):
        wire = {"model": "m", "max_tokens": 32, "messages": [
            {"role": "user", "content": "go"},
            {"role": "system", "content": [{"type": "tool_addition", "tool": {"type": "tool_definition", "definition": {"name": "f", "input_schema": {"type": "object"}, "cache_control": {"type": "bad"}}}}]},
        ]}
        _, losses = decode_request(wire)
        self.assertEqual([(item.path, item.reason) for item in losses], [("messages[1].content[0].tool.definition.cache_control.type", "unmapped-value")])

    def test_anthropic_add_remove_and_compaction_roundtrip(self):
        compaction = {"type": "compaction", "content": None, "signature": "", "tool_changes": [
            {"type": "tool_removal", "tool": {"type": "tool_reference", "name": "old", "future": True}}
        ]}
        change = {"type": "tool_addition", "tool": {"type": "tool_definition", "definition": {"name": "new", "input_schema": {"type": "object"}}}}
        wire = {"model": "m", "max_tokens": 32, "messages": [
            {"role": "user", "content": [compaction, {"type": "text", "text": "go"}]},
            {"role": "system", "content": [change]},
            {"role": "assistant", "content": "ok"},
        ]}
        req, losses = decode_request(wire)
        self.assertEqual(losses, [])
        self.assertEqual(len(req.tool_change_groups), 1)
        restored = load_request(dump_request(req))
        encoded, losses = encode_request(restored)
        self.assertEqual(losses, [])
        self.assertEqual(encoded["messages"][0]["content"][0], compaction)
        self.assertEqual(encoded["messages"][1]["content"], [change])

    def test_invalid_ir_change_positions_and_references_are_rejected(self):
        invalid_groups = [
            {"message_index": 2, "content_index": -1, "parts": []},
            {"message_index": 0, "content_index": 2, "parts": []},
            {"message_index": 0, "content_index": -1, "parts": [{"type": "tool_change", "change": {"op": "remove", "ref": {"type": "mcp_tool_reference", "name": "x"}}}]},
            {"message_index": 0, "content_index": -1, "parts": [{"type": "tool_change", "change": {"op": "remove", "tool": {"name": "x", "input_schema": {}}}}]},
            {"message_index": 0, "content_index": -1, "parts": [{"type": "text", "text": "x", "change": {"op": "remove", "ref": {"type": "tool_reference", "name": "x"}}}]},
        ]
        for group in invalid_groups:
            with self.subTest(group=group), self.assertRaises(ValueError):
                load_request({"specVersion": "0.3.0", "model": "m", "messages": [{"role": "user", "content": [{"type": "text", "text": "go"}]}], "tool_change_groups": [group]})
