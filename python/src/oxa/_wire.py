from __future__ import annotations

import json
import re
from functools import wraps
from typing import Any

from oxa.ir import Loss
from oxa._wire_fields import PROFILES


def _reject_constant(value: str) -> None:
    raise ValueError(f"invalid JSON constant: {value}")


_PATH_PART_RE = re.compile(r'([^\.\[\]]+)|\[(\d+)\]')


def parse_path(path: str) -> tuple[str | int, ...]:
    if not path:
        return ()
    parts: list[str | int] = []
    for match in _PATH_PART_RE.finditer(path):
        key, idx = match.groups()
        if key is not None:
            parts.append(key)
        else:
            parts.append(int(idx))
    return tuple(parts)


def format_path(path: tuple[str | int, ...]) -> str:
    res: list[str] = []
    for part in path:
        if isinstance(part, int):
            res.append(f"[{part}]")
        else:
            if res:
                res.append(f".{part}")
            else:
                res.append(part)
    return "".join(res)


class RawDocument:
    def __init__(self, text: str):
        self.text = text
        self.decoder = json.JSONDecoder(parse_constant=_reject_constant)
        self.data = json.loads(text, parse_constant=_reject_constant)
        self.tokens: dict[tuple[str | int, ...], tuple[int, int]] = {}
        self.key_positions: dict[tuple[str | int, ...], int] = {}
        self._locate(self._space(0), ())

    def _space(self, position: int) -> int:
        while position < len(self.text) and self.text[position] in " \t\r\n":
            position += 1
        return position

    def _locate(self, position: int, path: tuple[str | int, ...]) -> int:
        start = position
        token = self.text[position]
        if token == "{":
            position = self._space(position + 1)
            while self.text[position] != "}":
                key_start = position
                key, position = self.decoder.raw_decode(self.text, position)
                child_path = path + (key,)
                self.key_positions[child_path] = key_start
                position = self._space(position)
                position = self._space(position + 1)  # colon
                position = self._locate(position, child_path)
                position = self._space(position)
                if self.text[position] == "}":
                    break
                position = self._space(position + 1)  # comma
            end = position + 1
        elif token == "[":
            position = self._space(position + 1)
            index = 0
            while self.text[position] != "]":
                child_path = path + (index,)
                position = self._locate(position, child_path)
                index += 1
                position = self._space(position)
                if self.text[position] == "]":
                    break
                position = self._space(position + 1)  # comma
            end = position + 1
        else:
            _, end = self.decoder.raw_decode(self.text, position)
        self.tokens[path] = (start, end)
        return end

    def field(self, path: str | tuple[str | int, ...]) -> str | None:
        p = path if isinstance(path, tuple) else parse_path(path)
        token = self.tokens.get(p)
        return None if token is None else self.text[token[0]:token[1]]


def raw_field(raw: str | RawDocument, path: str) -> str | None:
    if not raw:
        return None
    document = raw if isinstance(raw, RawDocument) else RawDocument(raw)
    return document.field(path)


def _audit(wire: dict[str, Any], face: str, kind: str, document: RawDocument | None):
    profile = PROFILES[face]
    losses: list[Loss] = []
    positions: dict[tuple[str | int, ...], int] = {}
    if document:
        positions.update(document.key_positions)

    def visit(value: Any, shape: str, path: tuple[str | int, ...]):
        if path not in positions:
            positions[path] = len(positions)
        if not shape:
            return
        if shape.endswith("[]"):
            if isinstance(value, list):
                for index, child in enumerate(value):
                    visit(child, shape[:-2], path + (index,))
            return
        if not isinstance(value, dict):
            return
        fields = profile.get(shape)
        if not fields:
            return
        if "$tag" in fields:
            tag = value.get(fields["$tag"])
            target = fields["$cases"].get(tag, "") if tag is not None else fields.get("$default", "")
            if not target:
                return
            return visit(value, target, path)
        for key, child in value.items():
            child_path = path + (key,)
            if child_path not in positions:
                positions[child_path] = len(positions)
            if key in fields:
                visit(child, fields[key], child_path)
            else:
                losses.append(Loss(path=format_path(child_path), field=key, reason="unmapped-field"))

    visit(wire, kind, ())
    return losses, positions


def _source_path_tuple(p_tuple: tuple[str | int, ...], positions: dict[tuple[str | int, ...], int]) -> tuple[str | int, ...]:
    if p_tuple in positions:
        return p_tuple
    resp = ("response", *p_tuple)
    if resp in positions:
        return resp
    delta = ("delta", *p_tuple)
    if delta in positions:
        return delta
    if len(p_tuple) >= 2 and p_tuple[0] == "output" and isinstance(p_tuple[1], int):
        tail = p_tuple[2:]
        item = ("item", *tail)
        if item in positions:
            return item
        if len(tail) >= 2 and tail[0] == "content" and isinstance(tail[1], int):
            part = ("part", *tail[2:])
            if part in positions:
                return part
    if "content_block" in p_tuple:
        idx = p_tuple.index("content_block")
        block = p_tuple[idx:]
        if block in positions:
            return block
    return p_tuple


def _merge(existing: list[Loss], audit: list[Loss], positions: dict[tuple[str | int, ...], int]) -> list[Loss]:
    seen = {(loss.path, loss.field, loss.reason) for loss in existing}
    combined = list(existing) + [loss for loss in audit if (loss.path, loss.field, loss.reason) not in seen]

    def order(loss: Loss) -> int:
        p_tuple = parse_path(loss.path)
        if loss.reason != "unsupported-semantic":
            f_tuple = p_tuple if (p_tuple and p_tuple[-1] == loss.field) else p_tuple + (loss.field,)
            if f_tuple in positions:
                return positions[f_tuple]
        if p_tuple in positions:
            return positions[p_tuple]
        sp_tuple = _source_path_tuple(p_tuple, positions)
        return positions.get(sp_tuple, 10**9)

    return sorted(combined, key=order)


def audit_decode(face: str, kind: str):
    def decorate(function):
        @wraps(function)
        def convert(wire, raw_text="", table=None):
            if not isinstance(wire, dict):
                raise ValueError(f"{face}: {kind} must be an object")
            document = raw_text if isinstance(raw_text, RawDocument) else RawDocument(raw_text) if raw_text else None
            if document is not None and document.data != wire:
                raise ValueError(f"{face}: raw text does not match the input object")
            audited, positions = _audit(wire, face, kind, document)
            value, losses = function(wire, document or "", table)
            return value, _merge(losses, audited, positions)
        return convert
    return decorate


def audit_feed(face: str):
    def decorate(function):
        @wraps(function)
        def feed(self, wire, raw_text=""):
            if not isinstance(wire, dict):
                raise ValueError(f"{face}: stream event must be an object")
            document = raw_text if isinstance(raw_text, RawDocument) else RawDocument(raw_text) if raw_text else None
            if document is not None and document.data != wire:
                raise ValueError(f"{face}: raw text does not match the input object")
            skipped_indexes = getattr(self, "_skipped", ())
            idx = wire.get("index")
            is_skipped = idx in skipped_indexes if idx is not None else False
            if not is_skipped and getattr(self, "_skipped_item", False):
                is_skipped = True
            audited, positions = _audit(wire, face, "event", document)
            if is_skipped:
                audited = [l for l in audited if l.path in ("type", "index", "item_id", "output_index", "content_index")]
            before = len(self._losses)
            events = function(self, wire, document or "")
            self._losses[before:] = _merge(self._losses[before:], audited, positions)
            return events
        return feed
    return decorate
