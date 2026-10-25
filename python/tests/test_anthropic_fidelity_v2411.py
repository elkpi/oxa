import json
import unittest

from oxa.anthropic.messages import (
    decode_request,
    decode_request_json,
    decode_response_json,
    encode_request,
    encode_response,
)
from oxa.ir import (
    Message,
    RedactedThinkingBlock,
    Request,
    TextBlock,
    TextCitation,
)
from oxa.openai.chatcompletions import encode_request as encode_cc
from oxa.openai.responses import encode_request as encode_re


class AnthropicFidelityV2411Test(unittest.TestCase):
    def test_redacted_thinking_roundtrip(self) -> None:
        raw = json.dumps({
            "model": "claude-opus-5-5",
            "max_tokens": 4096,
            "messages": [
                {"role": "user", "content": "Prior prompt."},
                {
                    "role": "assistant",
                    "content": [
                        {"type": "redacted_thinking", "data": "OPAQUE_ENCRYPTED_DATA"},
                        {"type": "text", "text": "Follow up."},
                    ],
                },
                {"role": "user", "content": "Continue."},
            ],
        })
        req, losses = decode_request_json(raw)
        self.assertEqual(len(losses), 0)
        self.assertEqual(len(req.messages[1].content), 2)
        redacted = req.messages[1].content[0]
        self.assertIsInstance(redacted, RedactedThinkingBlock)
        self.assertEqual(redacted.data, "OPAQUE_ENCRYPTED_DATA")

        encoded, enc_losses = encode_request(req)
        self.assertEqual(len(enc_losses), 0)
        encoded_str = json.dumps(encoded)
        self.assertIn('"type": "redacted_thinking"', encoded_str)
        self.assertIn('"data": "OPAQUE_ENCRYPTED_DATA"', encoded_str)

    def test_between_tools_roundtrip_and_conflict(self) -> None:
        raw = json.dumps({
            "model": "claude-sonnet-5-5",
            "max_tokens": 4096,
            "thinking": {"type": "between_tools"},
            "messages": [{"role": "user", "content": "Hello"}],
        })
        req, losses = decode_request_json(raw)
        self.assertEqual(len(losses), 0)
        mode_ann = next((a for a in (req.native or []) if a.kind == "thinking_mode"), None)
        self.assertIsNotNone(mode_ann)
        self.assertEqual(mode_ann.value, '"between_tools"')

        encoded, enc_losses = encode_request(req)
        self.assertEqual(len(enc_losses), 0)
        self.assertEqual(encoded.get("thinking", {}).get("type"), "between_tools")

        # Conflict with budget
        with self.assertRaises(ValueError):
            decode_request_json(json.dumps({
                "model": "claude-sonnet-5-5",
                "max_tokens": 4096,
                "thinking": {"type": "between_tools", "budget_tokens": 2048},
                "messages": [{"role": "user", "content": "Hello"}],
            }))

    def test_top_k_roundtrip(self) -> None:
        for k in [0, 40, 100]:
            raw = json.dumps({
                "model": "claude-opus-4-6",
                "max_tokens": 1024,
                "top_k": k,
                "messages": [{"role": "user", "content": "Hello"}],
            })
            req, losses = decode_request_json(raw)
            self.assertEqual(len(losses), 0)
            top_k_ann = next((a for a in (req.native or []) if a.kind == "top_k"), None)
            self.assertIsNotNone(top_k_ann)
            self.assertEqual(top_k_ann.value, str(k))

            encoded, enc_losses = encode_request(req)
            self.assertEqual(len(enc_losses), 0)
            self.assertEqual(encoded.get("top_k"), k)

    def test_disable_parallel_tool_use_roundtrip_and_none(self) -> None:
        for flag in [True, False]:
            raw = json.dumps({
                "model": "claude-sonnet-4-5",
                "max_tokens": 1024,
                "messages": [{"role": "user", "content": "Hello"}],
                "tools": [{"name": "tool1", "input_schema": {"type": "object"}}],
                "tool_choice": {"type": "auto", "disable_parallel_tool_use": flag},
            })
            req, losses = decode_request_json(raw)
            self.assertEqual(len(losses), 0)
            par_ann = next((a for a in (req.native or []) if a.kind == "disable_parallel_tool_use"), None)
            self.assertIsNotNone(par_ann)
            self.assertEqual(par_ann.value, "true" if flag else "false")

            encoded, enc_losses = encode_request(req)
            self.assertEqual(len(enc_losses), 0)
            self.assertEqual(encoded.get("tool_choice", {}).get("disable_parallel_tool_use"), flag)

        with self.assertRaises(ValueError):
            decode_request_json(json.dumps({
                "model": "claude-sonnet-4-5",
                "max_tokens": 1024,
                "messages": [{"role": "user", "content": "Hello"}],
                "tools": [{"name": "tool1", "input_schema": {"type": "object"}}],
                "tool_choice": {"type": "none", "disable_parallel_tool_use": True},
            }))

    def test_text_citations_roundtrip(self) -> None:
        raw = json.dumps({
            "id": "msg_cite",
            "type": "message",
            "role": "assistant",
            "model": "claude-sonnet-4-6",
            "content": [
                {
                    "type": "text",
                    "text": "Summary statement.",
                    "citations": [
                        {
                            "type": "char_location",
                            "cited_text": "source quote",
                            "document_index": 0,
                            "document_title": "Document 1",
                            "start_char_index": 0,
                            "end_char_index": 12,
                            "file_id": "file_doc_1",
                        }
                    ],
                }
            ],
            "stop_reason": "end_turn",
            "usage": {"input_tokens": 10, "output_tokens": 10},
        })
        resp, losses = decode_response_json(raw)
        self.assertEqual(len(losses), 0)
        txt = resp.content[0]
        self.assertIsInstance(txt, TextBlock)
        self.assertEqual(len(txt.citations or []), 1)
        self.assertEqual(txt.citations[0].source, "anthropic")

        encoded, enc_losses = encode_response(resp)
        self.assertEqual(len(enc_losses), 0)
        enc_str = json.dumps(encoded)
        self.assertIn('"char_location"', enc_str)
        self.assertIn('"file_doc_1"', enc_str)

    def test_cross_face_losses(self) -> None:
        req = Request(
            model="claude-opus-5-5",
            messages=[
                Message(role="user", content=[TextBlock(text="Question.")]),
                Message(
                    role="assistant",
                    content=[
                        RedactedThinkingBlock(data="OPAQUE_TEST"),
                        TextBlock(text="Answer."),
                    ],
                ),
                Message(role="user", content=[TextBlock(text="Followup.")]),
            ],
        )
        _, cc_losses = encode_cc(req)
        self.assertTrue(
            any(
                l.reason == "unsupported-semantic" and "encrypted thinking" in (l.detail or "")
                for l in cc_losses
            )
        )
        _, re_losses = encode_re(req)
        self.assertTrue(
            any(
                l.reason == "unsupported-semantic" and "encrypted thinking" in (l.detail or "")
                for l in re_losses
            )
        )


if __name__ == "__main__":
    unittest.main()

