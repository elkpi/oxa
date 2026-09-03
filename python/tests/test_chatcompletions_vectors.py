"""Runs all shared golden vectors for Chat Completions nonstream against the Python implementation."""

import unittest

from oxa.openai.chatcompletions import (
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


class ChatCompletionsVectorsTest(unittest.TestCase):
    def test_all_nonstream_vectors(self) -> None:
        root = find_repo_root()
        if root is None:
            self.skipTest("repository root not found (running as standalone package)")
            return

        vectors = load_vectors(root, "chatcompletions", "nonstream")
        self.assertEqual(len(vectors), 34, "expected 34 nonstream chatcompletions vectors")

        for vector in vectors:
            with self.subTest(vector=vector.name):
                run_nonstream_vector(
                    vector,
                    decode_request=decode_request,
                    decode_response=decode_response,
                    encode_request=encode_request,
                    encode_response=encode_response,
                )


if __name__ == "__main__":
    unittest.main()
