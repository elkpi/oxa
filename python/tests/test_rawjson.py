import json
import unittest

from oxa.anthropic import messages as anthropic
from oxa.openai import chatcompletions as chat
from oxa.openai import responses


class RawJSONTest(unittest.TestCase):
    def test_response_tool_inputs_follow_structure_not_id_position(self):
        raw = '{"id":"m1","type":"message","role":"assistant","model":"m","stop_reason":"tool_use","content":[{"type":"tool_use","input":{"x":1},"id":"t1","name":"f"},{"type":"tool_use","input":{"x":2},"id":"t2","name":"f"}],"usage":{"input_tokens":1,"output_tokens":1}}'
        response, losses = anthropic.decode_response(json.loads(raw), raw_text=raw)
        self.assertEqual([(block.id, block.input) for block in response.content], [("t1", '{"x":1}'), ("t2", '{"x":2}')])
        self.assertEqual(losses, [])

    def test_unknown_properties_follow_presence_and_source_order(self):
        raw = '{"model":"m","input":"hi","store":false,"previous_response_id":"r1","reasoning":{"effort":"low","unknown_extension":null}}'
        request, losses = responses.decode_request(json.loads(raw), raw_text=raw)
        self.assertEqual(len(request.messages), 1)
        self.assertEqual([loss.path for loss in losses], ["store", "previous_response_id", "reasoning.unknown_extension"])

    def test_json_entrypoints_and_opaque_schema(self):
        fixtures = [
            (chat, {"model": "m", "messages": [{"role": "user", "content": "hi"}]}),
            (responses, {"model": "m", "input": "hi"}),
            (anthropic, {"model": "m", "max_tokens": 32, "messages": [{"role": "user", "content": "hi"}]}),
        ]
        for module, base in fixtures:
            for value in [False, None, 0, "", [], {}, {"nested": True}]:
                raw = json.dumps({**base, "unknown_extension": value})
                request, losses = module.decode_request_json(raw)
                self.assertEqual(len(request.messages), 1)
                self.assertEqual([loss.path for loss in losses], ["unknown_extension"])
            with self.assertRaises(ValueError):
                module.decode_request_json('[]')
            with self.assertRaises(ValueError):
                module.decode_request_json('{')

    def test_raw_stream_events_keep_lifecycle_and_loss_channel(self):
        for module, events in [
            (chat, [{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":None}],"unknown_extension":None},{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}]),
            (responses, [{"type":"response.created","response":{"id":"r1","model":"m","status":"in_progress","output":[]},"unknown_extension":None},{"type":"response.completed","response":{"id":"r1","model":"m","status":"completed","output":[]}}]),
            (anthropic, [{"type":"message_start","message":{"id":"m1","model":"m","content":[]},"unknown_extension":None},{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}},{"type":"message_stop"}]),
        ]:
            decoder = module.StreamDecoder()
            produced = []
            for event in events:
                produced.extend(decoder.feed_json(json.dumps(event)))
            produced.extend(decoder.flush())
            self.assertGreaterEqual(len(produced), 3)
            self.assertEqual([loss.path for loss in decoder.losses()], ["unknown_extension"])

    def test_raw_response_entrypoints_and_known_type_errors(self):
        fixtures = [
            (chat, {"id":"r1","model":"m","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}),
            (responses, {"id":"r1","model":"m","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}),
            (anthropic, {"id":"r1","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"hi"}]}),
        ]
        for module, wire in fixtures:
            _, losses = module.decode_response_json(json.dumps({**wire, "unknown_extension":None}))
            self.assertEqual([loss.path for loss in losses], ["unknown_extension"])
            with self.assertRaises(ValueError):
                module.decode_response_json("[]")
        with self.assertRaises(ValueError):
            anthropic.decode_request_json('{"model":"m","max_tokens":32,"messages":[{"role":"invalid","content":"hi"}]}')

    def test_raw_request_tool_input_preserves_lexical_form(self):
        raw_input = '{ "z": 9007199254740993, "a": 1e+01 }'
        raw = '{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"invoke"},{"role":"assistant","content":[{"input":' + raw_input + ',"type":"tool_use","id":"t1","name":"f"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]}'
        request, losses = anthropic.decode_request(json.loads(raw), raw_text=raw)
        self.assertEqual(request.messages[1].content[0].input, raw_input)
        self.assertEqual(losses, [])
