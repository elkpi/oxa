import unittest
from oxa.openai.responses import (
    StreamDecoder,
    decode_request,
    decode_response,
)
from oxa.openai.chatcompletions import StreamEncoder as CCStreamEncoder
from oxa.anthropic.messages import StreamEncoder as AntStreamEncoder
from oxa.ir import (
    MessageStart,
    ContentBlockStart,
    ContentBlockDelta,
    ContentBlockStop,
    MessageDelta,
    MessageDone,
    ApplyPatchToolResultBlock,
    ApplyPatchToolUseBlock,
    ApplyPatchDeleteFileOperation,
    CustomToolInputDelta,
    Usage,
)


class TestApplyPatchEdge(unittest.TestCase):
    def test_apply_patch_response_validation_rejects_missing_fields(self) -> None:
        bad_items = [
            {
                "id": "apc",
                "type": "apply_patch_call",
                "call_id": "c",
                "status": "completed",
                "operation": {"type": "delete_file"},  # missing path
            },
            {
                "id": "apc",
                "type": "apply_patch_call",
                "call_id": "c",
                "status": "completed",
                "operation": {"type": "delete_file", "path": None},  # null path
            },
            {
                "id": "apc",
                "type": "apply_patch_call",
                "call_id": "c",
                "status": "completed",
                "operation": {"type": "create_file", "path": "a"},  # missing diff
            },
            {
                "id": "apc",
                "type": "apply_patch_call",
                "status": "completed",
                "operation": {"type": "delete_file", "path": "a"},  # missing call_id
            },
            {
                "type": "apply_patch_call",
                "call_id": "c",
                "status": "completed",
                "operation": {"type": "delete_file", "path": "a"},  # missing item id
            },
            {
                "id": "apc",
                "type": "apply_patch_call",
                "call_id": "c",
                "status": "completed",  # missing operation
            },
            {
                "id": "apc",
                "type": "apply_patch_call",
                "call_id": "c",
                "operation": {"type": "delete_file", "path": "a"},  # missing status
            },
        ]
        for item in bad_items:
            with self.subTest(item=item):
                with self.assertRaises(ValueError):
                    decode_response({
                        "id": "r",
                        "model": "m",
                        "status": "completed",
                        "output": [item],
                    })

    def test_apply_patch_request_association(self) -> None:
        # duplicate call_id in same turn
        with self.assertRaises(ValueError):
            decode_request({
                "model": "m",
                "input": [
                    {"role": "user", "content": "hi"},
                    {"type": "apply_patch_call", "call_id": "same", "status": "completed", "operation": {"type": "future"}},
                    {"type": "apply_patch_call", "call_id": "same", "status": "completed", "operation": {"type": "future"}},
                    {"type": "apply_patch_call_output", "call_id": "same", "status": "completed"},
                    {"type": "apply_patch_call_output", "call_id": "same", "status": "completed"},
                ],
            })

        # mismatched call_id in same turn
        with self.assertRaises(ValueError):
            decode_request({
                "model": "m",
                "input": [
                    {"role": "user", "content": "hi"},
                    {"type": "apply_patch_call", "call_id": "a", "status": "completed", "operation": {"type": "future"}},
                    {"type": "apply_patch_call_output", "call_id": "b", "status": "future"},
                ],
            })

        # orphaned result
        with self.assertRaises(ValueError):
            decode_request({
                "model": "m",
                "input": [
                    {"role": "user", "content": "hi"},
                    {"type": "apply_patch_call_output", "call_id": "c", "status": "completed"},
                ],
            })

    def test_apply_patch_streaming_unknown_semantics(self) -> None:
        for scenario in [
            {"op": {"type": "future", "extra": True}, "status": "completed"},
            {"op": {"type": "delete_file", "path": "a"}, "status": "future"},
            {"op": {"type": "delete_file", "path": "a", "diff": 42}, "status": "completed"},
        ]:
            d = StreamDecoder()
            d.feed({"type": "response.created", "response": {"id": "r", "model": "m", "status": "in_progress", "output": []}})
            d.feed({
                "type": "response.output_item.added",
                "output_index": 0,
                "item": {
                    "id": "apc",
                    "type": "apply_patch_call",
                    "call_id": "c",
                    "status": "in_progress",
                    "operation": scenario["op"],
                },
            })
            done_events = d.feed({
                "type": "response.output_item.done",
                "output_index": 0,
                "item": {
                    "id": "apc",
                    "type": "apply_patch_call",
                    "call_id": "c",
                    "status": scenario["status"],
                    "operation": scenario["op"],
                },
            })
            if scenario["status"] == "future" or scenario["op"]["type"] == "future":
                self.assertEqual(len(done_events), 0)
            d.feed({
                "type": "response.completed",
                "response": {
                    "id": "r",
                    "model": "m",
                    "status": "completed",
                    "output": [
                        {
                            "id": "apc",
                            "type": "apply_patch_call",
                            "call_id": "c",
                            "status": scenario["status"],
                            "operation": scenario["op"],
                        }
                    ],
                },
            })
            d.flush()
            self.assertGreaterEqual(len(d.losses()), 1)

    def test_apply_patch_stream_degradation(self) -> None:
        events = [
            MessageStart(id="r", model="m"),
            ContentBlockStart(index=0, block=ApplyPatchToolResultBlock(tool_use_id="c", status="completed")),
            ContentBlockStop(index=0),
            MessageDelta(stop_reason="end_turn", usage=Usage(input_tokens=1, output_tokens=2)),
            MessageDone(),
        ]
        for cls in [CCStreamEncoder, AntStreamEncoder]:
            enc = cls()
            for ev in events:
                enc.apply(ev)

        # Delta on atomic block must fail
        delta_events = [
            MessageStart(id="r", model="m"),
            ContentBlockStart(index=0, block=ApplyPatchToolUseBlock(id="c", status="completed", operation=ApplyPatchDeleteFileOperation(path="a"))),
            ContentBlockDelta(index=0, delta=CustomToolInputDelta(text='""')),
        ]
        for cls in [CCStreamEncoder, AntStreamEncoder]:
            enc = cls()
            with self.assertRaises(ValueError):
                for ev in delta_events:
                    enc.apply(ev)


if __name__ == "__main__":
    unittest.main()
