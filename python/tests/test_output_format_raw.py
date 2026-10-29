import json
import unittest
from dataclasses import replace

from oxa.anthropic.messages import decode_request_json, encode_request_json
from oxa.ir import ResponseFormatJsonSchema, dump_request_json, load_request
from oxa.openai.chatcompletions import decode_request_json as decode_chat_json
from oxa.openai.responses import decode_request_json as decode_responses_json


class OutputFormatRawTest(unittest.TestCase):
    def test_unnamed_schema_can_be_constructed(self):
        self.assertEqual(ResponseFormatJsonSchema(schema={"type": "object"}).name, "")
        self.assertEqual(ResponseFormatJsonSchema("named", {}).name, "named")

    def test_schema_source_survives_canonical_and_anthropic_json(self):
        schema = '{ "type" : "object", "properties" : { "n" : {"type":"string"} } }'
        raw = '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":' + schema + '}}}'
        req, losses = decode_request_json(raw)
        self.assertEqual(losses, [])
        canonical = dump_request_json(req)
        self.assertIn(schema, canonical)
        req = load_request(canonical)
        result, losses = encode_request_json(req)
        self.assertEqual(losses, [])
        self.assertIn(schema, result)
        self.assertEqual(json.loads(result)["output_config"]["format"]["schema"]["type"], "object")

    def test_opaque_schema_allows_large_json_numbers(self):
        schema = '{"type":"object","maximum":123456789012345678901234567890,"default":1e400}'
        raw = '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":' + schema + '}}}'
        req, _ = decode_request_json(raw)
        canonical = dump_request_json(req)
        req = load_request(canonical)
        encoded, _ = encode_request_json(req)
        self.assertIn(schema, encoded)

    def test_openai_to_anthropic_preserves_schema_source(self):
        schema = '{ "type" : "object", "maximum" : 1e400 }'
        cases = [
            (decode_chat_json, '{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":' + schema + '}}}'),
            (decode_responses_json, '{"model":"m","max_output_tokens":64,"input":"hi","text":{"format":{"type":"json_schema","name":"result","schema":' + schema + '}}}'),
        ]
        for decode, raw in cases:
            with self.subTest(decode=decode.__module__):
                req, _ = decode(raw)
                self.assertEqual(req.params.response_format.schema_raw, schema)
                canonical = dump_request_json(req)
                req = load_request(canonical)
                encoded, losses = encode_request_json(req)
                self.assertEqual(len(losses), 1)
                self.assertIn(schema, encoded)

    def test_output_config_container_types(self):
        prefix = '{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":'
        req, losses = decode_request_json(prefix + 'null}')
        self.assertIsNone(req.params.response_format)
        self.assertEqual(losses, [])
        for value in ('[]', '42', '"invalid"', 'false'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                decode_request_json(prefix + value + '}')

    def test_malformed_formats_are_structural_errors(self):
        for fmt in ('null', '{}', '{"type":"json_schema"}', '{"type":"json_schema","schema":null}', '{"type":"json_schema","schema":[]}', '{"type":false}', '{"type":[]}'):
            raw = '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":' + fmt + '}}'
            with self.subTest(fmt=fmt), self.assertRaises(ValueError):
                decode_request_json(raw)

    def test_edited_schema_cannot_reuse_stale_raw(self):
        raw = '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}}'
        req, _ = decode_request_json(raw)
        fmt = replace(req.params.response_format, schema={"type": "object", "title": "changed"})
        req = replace(req, params=replace(req.params, response_format=fmt))
        with self.assertRaises(ValueError):
            encode_request_json(req)
