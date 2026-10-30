import unittest

from oxa.openai.responses import decode_request_json


class WebSearchCallTest(unittest.TestCase):
    def test_invalid_official_actions_are_structural_errors(self):
        import json

        invalid = [
            None,
            {},
            {"type": "future"},
            {"type": "find_in_page", "url": "https://example.com"},
            {"type": "find_in_page", "url": None, "pattern": "q"},
            {"type": "search", "query": None},
            {"type": "search", "queries": None},
            {"type": "search", "sources": [{"type": "url"}]},
        ]
        for action in invalid:
            with self.subTest(action=action):
                raw = json.dumps({"model": "m", "input": [
                    {"type": "web_search_call", "id": "ws", "status": "completed", "action": action},
                    {"role": "user", "content": "continue"},
                ]})
                with self.assertRaises(ValueError):
                    decode_request_json(raw)

    def test_unknown_top_level_field_is_retained(self):
        raw = '{"model":"m","input":[{"role":"user","content":"hi"},{"type":"web_search_call","id":"ws","status":"completed","action":{"type":"search"},"role":true}]}'
        request, losses = decode_request_json(raw)
        self.assertEqual(request.messages[1].content[0].extra, {"role": True})
        self.assertEqual([(loss.path, loss.reason) for loss in losses], [("input[1].role", "unmapped-field")])

    def test_assistant_first_history(self):
        raw = '{"model":"m","input":[{"type":"web_search_call","id":"ws","status":"completed","action":{"type":"search","query":"q"}},{"role":"assistant","content":"answer"},{"role":"user","content":"continue"}]}'
        request, losses = decode_request_json(raw)
        self.assertEqual(losses, [])
        self.assertEqual(request.messages[0].role, "assistant")
