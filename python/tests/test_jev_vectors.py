"""Runs the shared SystemOne Jev golden vectors through the Python face via
the oxa-vectest harness (vectors/README.md comparison rules)."""

import unittest

from oxa.jev import (
    decode_request_json,
    decode_response_json,
    encode_request,
    encode_response,
)
from oxa.vectest import find_repo_root, load_vectors, run_nonstream_vector


def _run_jev_vectors() -> int:
    root = find_repo_root()
    if root is None:
        return 0
    vectors = load_vectors(root, "jev", "nonstream")
    if not vectors:
        raise AssertionError("no nonstream vectors found for face jev")
    for vector in vectors:
        run_nonstream_vector(
            vector,
            decode_request=lambda wire, raw_text, table: decode_request_json(
                raw_text, table
            ),
            decode_response=lambda wire, raw_text, table: decode_response_json(
                raw_text, table
            ),
            encode_request=lambda req, table: encode_request(req, table),
            encode_response=lambda resp, table: encode_response(resp, table),
            table=None,
        )
    return len(vectors)


class JevVectorsTest(unittest.TestCase):
    def test_runs_every_jev_nonstream_vector(self) -> None:
        root = find_repo_root()
        if root is None:
            self.skipTest("repository root not found (running as standalone package)")
            return
        count = _run_jev_vectors()
        self.assertGreater(count, 0, "jev nonstream vectors must execute")
        self.assertGreaterEqual(count, 15)


if __name__ == "__main__":
    unittest.main()
