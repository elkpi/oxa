"""The SystemOne Jev face of the oxa conversion library: the structured
decision protocol (`state + questions → typed answers`) projected onto the
IR conversation model (spec/17). Nonstream only — Jev has no streaming mode.
Semantic unmappables are losses, never errors; errors are reserved for
structural type violations of known fields (spec/02 §4)."""

from oxa.jev.decode import decode_request, decode_request_json, decode_response, decode_response_json
from oxa.jev.encode import encode_request, encode_request_json, encode_response, encode_response_json

__all__ = [
    "decode_request",
    "decode_request_json",
    "decode_response",
    "decode_response_json",
    "encode_request",
    "encode_request_json",
    "encode_response",
    "encode_response_json",
]
