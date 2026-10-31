"""Runs all shared golden vectors for Anthropic Messages nonstream against the Python implementation."""

import unittest

from oxa.anthropic.messages import (
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


class AnthropicVectorsTest(unittest.TestCase):
    def test_rejects_conflicting_document_source_fields(self) -> None:
        request = {
            "model": "m",
            "max_tokens": 1,
            "messages": [
                {
                    "role": "user",
                    "content": [
                        {
                            "type": "document",
                            "source": {
                                "type": "base64",
                                "media_type": "application/pdf",
                                "data": "YQ==",
                                "url": "",
                            },
                        }
                    ],
                }
            ],
        }
        with self.assertRaisesRegex(ValueError, "conflicting source fields"):
            decode_request(request)

    def test_document_missing_or_empty_source_type_is_error(self) -> None:
        for source in ({}, {"type": ""}):
            request = {
                "model": "m",
                "max_tokens": 1,
                "messages": [
                    {
                        "role": "user",
                        "content": [{"type": "document", "source": source}],
                    }
                ],
            }
            with self.assertRaisesRegex(ValueError, "source.type is required"):
                decode_request(request)

    def test_document_null_citations_is_error(self) -> None:
        request = {
            "model": "m",
            "max_tokens": 1,
            "messages": [
                {
                    "role": "user",
                    "content": [
                        {
                            "type": "document",
                            "source": {"type": "file", "file_id": "f"},
                            "citations": None,
                        }
                    ],
                }
            ],
        }
        with self.assertRaisesRegex(ValueError, "citations must be a JSON object"):
            decode_request(request)

    def test_preserves_empty_document_metadata(self) -> None:
        request = {
            "model": "m",
            "max_tokens": 1,
            "messages": [
                {
                    "role": "user",
                    "content": [
                        {
                            "type": "document",
                            "source": {"type": "file", "file_id": "f"},
                            "title": "",
                            "context": "",
                        }
                    ],
                }
            ],
        }
        decoded, _ = decode_request(request)
        encoded, _ = encode_request(decoded)
        block = encoded["messages"][0]["content"][0]
        self.assertEqual(block["title"], "")
        self.assertEqual(block["context"], "")

    def test_all_nonstream_vectors(self) -> None:
        root = find_repo_root()
        if root is None:
            self.skipTest("repository root not found (running as standalone package)")
            return

        vectors = load_vectors(root, "anthropic", "nonstream")
        self.assertEqual(len(vectors), 122, "expected 122 nonstream anthropic vectors")

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
