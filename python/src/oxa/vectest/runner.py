"""Vector execution harness applying normative comparison rules."""

from __future__ import annotations

from typing import Any, Callable

from oxa.ir import (
    Loss,
    Request,
    Response,
    dump_request,
    dump_response,
    load_request,
    load_response,
)
from oxa.modelmap import Table
from oxa.vectest.compare import compare_json, compare_losses
from oxa.vectest.load import Vector


def run_nonstream_vector(
    vector: Vector,
    decode_request: Callable[[Any, str, Table | None], tuple[Request, list[Loss]]],
    decode_response: Callable[[Any, str, Table | None], tuple[Response, list[Loss]]],
    encode_request: Callable[[Request, Table | None], tuple[Any, list[Loss]]],
    encode_response: Callable[[Response, Table | None], tuple[Any, list[Loss]]],
    table: Table | None = None,
) -> None:
    """Executes a nonstream vector through the appropriate converter direction."""
    if vector.conversion == "to-ir":
        if vector.is_request():
            req, losses = decode_request(vector.input, vector.input_raw, table)
            actual_ir = dump_request(req)
        else:
            resp, losses = decode_response(vector.input, vector.input_raw, table)
            actual_ir = dump_response(resp)

        assert vector.expected_ir is not None, f"vector {vector.name}: expected_ir is missing"
        compare_json(vector.expected_ir, actual_ir)
        compare_losses(vector.expected_losses, losses)

    elif vector.conversion == "from-ir":
        if vector.is_request():
            ir_req = load_request(vector.input)
            actual_wire, losses = encode_request(ir_req, table)
        else:
            ir_resp = load_response(vector.input)
            actual_wire, losses = encode_response(ir_resp, table)

        assert vector.expected_output is not None, f"vector {vector.name}: expected_output is missing"
        compare_json(vector.expected_output, actual_wire)
        compare_losses(vector.expected_losses, losses)

    else:
        raise ValueError(f"unknown nonstream conversion: {vector.conversion}")


def run_cross_vector(
    vector: Vector,
    source_decode_request: Callable[[Any, str, Table | None], tuple[Request, list[Loss]]],
    source_decode_response: Callable[[Any, str, Table | None], tuple[Response, list[Loss]]],
    target_encode_request: Callable[[Request, Table | None], tuple[Any, list[Loss]]],
    target_encode_response: Callable[[Response, Table | None], tuple[Any, list[Loss]]],
    table: Table | None = None,
) -> None:
    """Executes a cross-protocol vector: source decode -> IR -> target encode."""
    if vector.is_request():
        ir_req, dec_losses = source_decode_request(vector.input, vector.input_raw, table)
        actual_wire, enc_losses = target_encode_request(ir_req, table)
    else:
        ir_resp, dec_losses = source_decode_response(vector.input, vector.input_raw, table)
        actual_wire, enc_losses = target_encode_response(ir_resp, table)

    all_losses = dec_losses + enc_losses
    assert vector.expected_output is not None, f"vector {vector.name}: expected_output is missing"
    compare_json(vector.expected_output, actual_wire)
    compare_losses(vector.expected_losses, all_losses)
