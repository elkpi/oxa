"""Standalone byte-level Server-Sent Events (SSE) frame adapter.

Implements the SSE framing boundary specified in spec/20-streaming-semantics.md §6 (N-S-8).
It knows nothing about JSON, the oxa IR, or any provider face: it decodes and encodes
SSE frames as opaque bytes.
"""

from __future__ import annotations

from dataclasses import dataclass
import io
from typing import BinaryIO, Iterable, Iterator


@dataclass(slots=True)
class Frame:
    """One decoded SSE frame."""

    event: str = ""
    data: bytes = b""


def decode_frames(stream: bytes | BinaryIO | Iterable[bytes]) -> Iterator[Frame]:
    """Yields decoded SSE frames from an input byte stream, bytes, or iterable."""
    if isinstance(stream, bytes):
        reader: BinaryIO = io.BytesIO(stream)
    elif hasattr(stream, "read"):
        reader = stream
    else:
        # iterable of bytes
        chunks: list[bytes] = list(stream)
        reader = io.BytesIO(b"".join(chunks))

    event_name = ""
    have = False
    data_lines: list[bytes] = []

    while True:
        line_raw = reader.readline()
        if not line_raw:
            if have:
                yield Frame(event=event_name, data=b"\n".join(data_lines))
            break

        line = line_raw
        if line.endswith(b"\n"):
            line = line[:-1]
        if line.endswith(b"\r"):
            line = line[:-1]

        if not line:
            if have:
                yield Frame(event=event_name, data=b"\n".join(data_lines))
            event_name = ""
            have = False
            data_lines = []
            continue

        if line.startswith(b":"):
            continue

        if b":" in line:
            name_part, val_part = line.split(b":", 1)
            if val_part.startswith(b" "):
                val_part = val_part[1:]
        else:
            name_part = line
            val_part = b""

        if name_part == b"data":
            have = True
            data_lines.append(val_part)
        elif name_part == b"event":
            have = True
            event_name = val_part.decode("utf-8", errors="replace")


def encode(frame: Frame) -> bytes:
    """Encodes an SSE frame into bytes."""
    buf = bytearray()
    if frame.event:
        buf.extend(b"event: ")
        buf.extend(frame.event.encode("utf-8"))
        buf.extend(b"\n")

    if not frame.data:
        buf.extend(b"data: \n")
    else:
        for line in frame.data.split(b"\n"):
            buf.extend(b"data: ")
            buf.extend(line)
            buf.extend(b"\n")
    buf.extend(b"\n")
    return bytes(buf)
