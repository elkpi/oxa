import json
import unittest
from oxa.anthropic import messages as anthropic
from oxa.openai import responses


class ReviewRegressionTest(unittest.TestCase):
    def test_display_like_unknown_key_cannot_replace_tool_source(self):
        base = {"id":"m1","model":"m","stop_reason":"tool_use","content":[{"type":"tool_use","id":"a","name":"f","input":{"x":1}}]}
        for first in [True, False]:
            extra = {"content[0].input":{"x":999}}
            wire = {**extra, **base} if first else {**base, **extra}
            value, _ = anthropic.decode_response_json(json.dumps(wire))
            self.assertEqual(json.loads(value.content[0].input), {"x":1})

    def test_known_raw_shape_violations_never_return_ir(self):
        invalid = [
            {"type":"text","text":{}},
            42,
            {"type":"tool_use","id":"a","name":"f","input":'{"x":1}'},
        ]
        for block in invalid:
            raw = json.dumps({"id":"m1","model":"m","stop_reason":"end_turn","content":[block]})
            with self.assertRaises(ValueError):
                anthropic.decode_response_json(raw)

    def test_known_dropped_null_and_false_are_reported(self):
        raw = '{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"hi"}],"metadata":null,"tool_choice":{"type":"auto","disable_parallel_tool_use":false}}'
        req, losses = anthropic.decode_request_json(raw)
        self.assertEqual([loss.path for loss in losses], ["metadata"])
        par_ann = next((a for a in (req.native or []) if a.kind == "disable_parallel_tool_use"), None)
        self.assertIsNotNone(par_ann)
        self.assertEqual(par_ann.value, "false")
        raw = '{"id":"m1","model":"m","stop_reason":"tool_use","content":[{"type":"tool_use","id":"a","name":"f","input":{},"cache_control":{"type":"ephemeral"}}]}'
        _, losses = anthropic.decode_response_json(raw)
        self.assertEqual([loss.path for loss in losses], ["content[0].cache_control"])

    def test_unknown_event_and_semantic_loss_order(self):
        decoder = anthropic.StreamDecoder()
        self.assertEqual(decoder.feed_json('{"type":"new_event","extension":null}'), [])
        self.assertEqual(len(decoder.losses()), 1)
        decoder.feed_json('{"type":"message_start","message":{"id":"m1","model":"m","content":[]}}')
        decoder.feed_json('{"type":"message_delta","delta":{"stop_reason":"new_reason","extension":false},"usage":{"input_tokens":1,"output_tokens":1}}')
        self.assertEqual([loss.reason for loss in decoder.losses()[1:]], ["unmapped-value", "unmapped-field"])

    def test_skipped_unit_descendants_do_not_add_child_losses(self):
        decoder = anthropic.StreamDecoder()
        decoder.feed_json('{"type":"message_start","message":{"id":"m1","model":"m","content":[]}}')
        decoder.feed_json('{"type":"content_block_start","index":0,"content_block":{"type":"image","extra":null,"source":{"type":"url","url":"example","extra":{}}}}')
        decoder.feed_json('{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi","extra":false}}')
        self.assertEqual(len(decoder.losses()), 1)
