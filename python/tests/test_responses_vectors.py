"""Runs all shared golden vectors for OpenAI Responses nonstream against the Python implementation."""

import unittest

from oxa.openai.responses import (
    decode_request,
    decode_response,
    encode_request,
    encode_response,
)
from oxa.vectest import (
    find_repo_root,
    load_vectors,
    run_nonstream_vector,
)


class ResponsesVectorsTest(unittest.TestCase):
    def test_all_nonstream_vectors(self) -> None:
        root = find_repo_root()
        if root is None:
            self.skipTest("repository root not found (running as standalone package)")
            return

        vectors = load_vectors(root, "responses", "nonstream")
        self.assertEqual(len(vectors), 178, "expected 178 nonstream responses vectors")

        for vector in vectors:
            with self.subTest(vector=vector.name):
                run_nonstream_vector(
                    vector,
                    decode_request=decode_request,
                    decode_response=decode_response,
                    encode_request=encode_request,
                    encode_response=encode_response,
                )

    def test_annotations_type_and_multi_part_preservation(self) -> None:
        from oxa.ir import TextBlock, TextCitation, Response as IrResponse, STOP_END_TURN, Usage
        # Invalid annotations container
        with self.assertRaises(TypeError):
            decode_response({"id": "r", "model": "m", "status": "completed", "output": [{"type": "message", "content": [{"type": "output_text", "text": "t", "annotations": False}]}], "usage": {"input_tokens": 0, "output_tokens": 0}})

        # Multiple text blocks preserve distinct parts and citations
        resp = IrResponse(
            id="r_multi",
            model="m",
            stop_reason=STOP_END_TURN,
            usage=Usage(input_tokens=0, output_tokens=0),
            content=[
                TextBlock(text="AAAA"),
                TextBlock(text="Docs", citations=(TextCitation(source="openai", value='{"end_index":4,"start_index":0,"title":"Docs","type":"url_citation","url":"https://example.test"}'),))
            ]
        )
        wire, losses = encode_response(resp)
        self.assertEqual(len(losses), 0)
        parts = wire["output"][0]["content"]
        self.assertEqual(len(parts), 2)
        self.assertEqual(parts[0]["text"], "AAAA")
        self.assertEqual(parts[0]["annotations"], [])
        self.assertEqual(parts[1]["text"], "Docs")
        self.assertEqual(len(parts[1]["annotations"]), 1)
        self.assertEqual(parts[1]["annotations"][0]["end_index"], 4)


if __name__ == "__main__":
    unittest.main()
