"""Vector execution harness applying normative comparison rules."""

from __future__ import annotations

import json
from typing import Any, Callable

from oxa.ir import (
    Event,
    EventStream,
    Loss,
    Request,
    Response,
    dump_event_stream,
    dump_request,
    dump_response,
    load_event_stream,
    load_request,
    load_response,
    validate_event_stream,
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


def _extract_events_raw(input_raw: str) -> list[str]:
    events_marker = '"events"'
    pos = input_raw.find(events_marker)
    if pos == -1:
        return []
    bracket = input_raw.find("[", pos + len(events_marker))
    if bracket == -1:
        return []
    curr = bracket + 1
    decoder = json.JSONDecoder()
    raw_events: list[str] = []
    while curr < len(input_raw):
        while curr < len(input_raw) and input_raw[curr].isspace():
            curr += 1
        if curr >= len(input_raw) or input_raw[curr] == "]":
            break
        if input_raw[curr] == ",":
            curr += 1
            continue
        try:
            _, end = decoder.raw_decode(input_raw, curr)
            raw_events.append(input_raw[curr:end])
            curr = end
        except Exception:
            break
    return raw_events


def run_stream_vector(
    vector: Vector,
    decoder_factory: Callable[[Table | None], Any],
    encoder_factory: Callable[[Table | None], Any],
    table: Table | None = None,
) -> None:
    """Executes a streaming vector in either direction."""
    if vector.conversion == "to-ir":
        decoder = decoder_factory(table)
        input_events = vector.input.get("events", []) if isinstance(vector.input, dict) else []
        raw_events = _extract_events_raw(vector.input_raw)
        ir_events: list[Event] = []
        for i, ev in enumerate(input_events):
            raw_str = raw_events[i] if i < len(raw_events) else ""
            produced = decoder.feed(ev, raw_str)
            ir_events.extend(produced)
        flushed = decoder.flush()
        ir_events.extend(flushed)

        losses = decoder.losses()
        stream = EventStream(events=ir_events)
        validate_event_stream(stream)

        actual_ir = dump_event_stream(stream)
        assert vector.expected_ir is not None, f"vector {vector.name}: expected_ir is missing"
        compare_json(vector.expected_ir, actual_ir)
        compare_losses(vector.expected_losses, losses)

    elif vector.conversion == "from-ir":
        encoder = encoder_factory(table)
        ir_stream = load_event_stream(vector.input)
        wire_events: list[dict[str, Any]] = []
        all_losses: list[Loss] = []

        for ev in ir_stream.events:
            produced, losses = encoder.apply(ev)
            wire_events.extend(produced)
            all_losses.extend(losses)

        actual_wire = {"events": wire_events}
        assert vector.expected_output is not None, f"vector {vector.name}: expected_output is missing"
        compare_json(vector.expected_output, actual_wire)
        compare_losses(vector.expected_losses, all_losses)

    else:
        raise ValueError(f"unknown stream conversion: {vector.conversion}")
