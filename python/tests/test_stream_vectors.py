"""Runs all shared golden vectors for streaming across all faces against the Python implementation."""

import unittest

from oxa.anthropic.messages import (
    StreamDecoder as AnthropicStreamDecoder,
    StreamEncoder as AnthropicStreamEncoder,
)
from oxa.openai.chatcompletions import (
    StreamDecoder as ChatCompletionsStreamDecoder,
    StreamEncoder as ChatCompletionsStreamEncoder,
)
from oxa.openai.responses import (
    StreamDecoder as ResponsesStreamDecoder,
    StreamEncoder as ResponsesStreamEncoder,
)
from oxa.vectest import (
    find_repo_root,
    load_vectors,
    run_stream_vector,
)

FACES = {
    "chatcompletions": (
        lambda table: ChatCompletionsStreamDecoder(table=table),
        lambda table: ChatCompletionsStreamEncoder(table=table),
    ),
    "anthropic": (
        lambda table: AnthropicStreamDecoder(table=table),
        lambda table: AnthropicStreamEncoder(table=table),
    ),
    "responses": (
        lambda table: ResponsesStreamDecoder(table=table),
        lambda table: ResponsesStreamEncoder(table=table),
    ),
}


class StreamVectorsTest(unittest.TestCase):
    def test_all_stream_vectors(self) -> None:
        root = find_repo_root()
        if root is None:
            self.skipTest("repository root not found (running as standalone package)")
            return

        total_vectors = 0
        for face, (dec_factory, enc_factory) in FACES.items():
            vectors = load_vectors(root, face, "stream")
            self.assertGreater(len(vectors), 0, f"expected stream vectors for face {face}")
            total_vectors += len(vectors)

            for vector in vectors:
                with self.subTest(vector=vector.name):
                    run_stream_vector(
                        vector,
                        decoder_factory=dec_factory,
                        encoder_factory=enc_factory,
                    )

        self.assertEqual(total_vectors, 8, "expected 8 total stream vectors across all faces")


if __name__ == "__main__":
    unittest.main()
