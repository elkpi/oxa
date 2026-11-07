import unittest

from oxa.ir import CodecError, CompactionBlock, Message, Request, TextBlock, ToolChange, ToolChangeGroup, ToolChangePart, ToolChangeRef
from oxa.ir.native import NativeAnnotation, validate_native_request


class PositionalNativeValidationTest(unittest.TestCase):
    def request(self, native, compaction):
        change = ToolChange(op="add", ref=ToolChangeRef(type="tool_reference", name="read"), native=native)
        content = [CompactionBlock(tool_changes=[change])] if compaction else [TextBlock(text="go")]
        groups = [] if compaction else [ToolChangeGroup(message_index=1, content_index=-1, parts=[ToolChangePart(type="tool_change", change=change)])]
        return Request(model="m", messages=[Message(role="user", content=content)], tool_change_groups=groups)

    def test_change_native_conflicts_and_indices(self):
        cache = NativeAnnotation(source="anthropic", kind="cache_control", value='{"type":"ephemeral"}')
        defer = NativeAnnotation(source="anthropic", kind="defer_loading", value="true")
        future = NativeAnnotation(source="future", kind="future", value="{}")
        invalid = NativeAnnotation(source="anthropic", kind="cache_control", value='{"type":"secret-value"}')
        for compaction in (False, True):
            path = "messages[0].content[0].tool_changes[0]" if compaction else "tool_change_groups[0].parts[0].change"
            with self.subTest(compaction=compaction):
                with self.assertRaises(CodecError) as conflict:
                    validate_native_request(self.request((cache, defer), compaction))
                self.assertIn(path, str(conflict.exception))
                validate_native_request(self.request((cache,), compaction))
                with self.assertRaises(CodecError) as error:
                    validate_native_request(self.request((future, invalid), compaction))
                self.assertIn(f"{path}.native[1]", str(error.exception))
                self.assertNotIn("secret-value", str(error.exception))


if __name__ == "__main__":
    unittest.main()
