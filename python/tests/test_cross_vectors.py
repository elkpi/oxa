"""Runs all shared cross-protocol vectors against the Python implementation."""

import unittest

from oxa.anthropic.messages import (
    decode_request as an_decode_request,
    decode_response as an_decode_response,
    encode_request as an_encode_request,
    encode_response as an_encode_response,
)
from oxa.openai.chatcompletions import (
    decode_request as cc_decode_request,
    decode_response as cc_decode_response,
    encode_request as cc_encode_request,
    encode_response as cc_encode_response,
)
from oxa.openai.responses import (
    decode_request as resp_decode_request,
    decode_response as resp_decode_response,
    encode_request as resp_encode_request,
    encode_response as resp_encode_response,
)
from oxa.vectest import (
    find_repo_root,
    load_cross_vectors,
    run_cross_vector,
)

DECODERS = {
    "chatcompletions": (cc_decode_request, cc_decode_response),
    "openai/chatcompletions": (cc_decode_request, cc_decode_response),
    "anthropic": (an_decode_request, an_decode_response),
    "anthropic/messages": (an_decode_request, an_decode_response),
    "responses": (resp_decode_request, resp_decode_response),
    "openai/responses": (resp_decode_request, resp_decode_response),
}

ENCODERS = {
    "chatcompletions": (cc_encode_request, cc_encode_response),
    "openai/chatcompletions": (cc_encode_request, cc_encode_response),
    "anthropic": (an_encode_request, an_encode_response),
    "anthropic/messages": (an_encode_request, an_encode_response),
    "responses": (resp_encode_request, resp_encode_response),
    "openai/responses": (resp_encode_request, resp_encode_response),
}


class CrossVectorsTest(unittest.TestCase):
    def test_all_cross_vectors(self) -> None:
        root = find_repo_root()
        if root is None:
            self.skipTest("repository root not found (running as standalone package)")
            return

        vectors = load_cross_vectors(root)
        self.assertEqual(len(vectors), 12, "expected 12 cross nonstream vectors")

        for vector in vectors:
            with self.subTest(vector=vector.name):
                src_proto = vector.source.protocol
                tgt_proto = vector.target.protocol

                self.assertIn(src_proto, DECODERS, f"unknown source protocol {src_proto}")
                self.assertIn(tgt_proto, ENCODERS, f"unknown target protocol {tgt_proto}")

                src_dec_req, src_dec_resp = DECODERS[src_proto]
                tgt_enc_req, tgt_enc_resp = ENCODERS[tgt_proto]

                run_cross_vector(
                    vector,
                    source_decode_request=src_dec_req,
                    source_decode_response=src_dec_resp,
                    target_encode_request=tgt_enc_req,
                    target_encode_response=tgt_enc_resp,
                )


if __name__ == "__main__":
    unittest.main()
