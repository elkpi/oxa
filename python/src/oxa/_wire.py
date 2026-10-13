from __future__ import annotations

import json
from functools import wraps
from typing import Any

from oxa.ir import Loss
from oxa._wire_fields import PROFILES


def _reject_constant(value: str) -> None:
    raise ValueError(f"invalid JSON constant: {value}")


class RawDocument:
    def __init__(self, text: str):
        self.text = text
        self.decoder = json.JSONDecoder(parse_constant=_reject_constant)
        self.data = json.loads(text, parse_constant=_reject_constant)
        self.tokens: dict[str, tuple[int, int]] = {}
        self._locate(self._space(0), "")

    def _space(self, position: int) -> int:
        while position < len(self.text) and self.text[position] in " \t\r\n":
            position += 1
        return position

    def _locate(self, position: int, path: str) -> int:
        start = position
        token = self.text[position]
        if token == "{":
            position = self._space(position + 1)
            while self.text[position] != "}":
                key, position = self.decoder.raw_decode(self.text, position)
                position = self._space(position)
                position = self._space(position + 1)
                position = self._locate(position, _join(path, key))
                position = self._space(position)
                if self.text[position] == "}":
                    break
                position = self._space(position + 1)
            end = position + 1
        elif token == "[":
            position = self._space(position + 1)
            index = 0
            while self.text[position] != "]":
                position = self._locate(position, f"{path}[{index}]")
                index += 1
                position = self._space(position)
                if self.text[position] == "]":
                    break
                position = self._space(position + 1)
            end = position + 1
        else:
            _, end = self.decoder.raw_decode(self.text, position)
        self.tokens[path] = (start, end)
        return end

    def field(self, path: str) -> str | None:
        token = self.tokens.get(path)
        return None if token is None else self.text[token[0]:token[1]]


def _join(path: str, field: str) -> str:
    return field if not path else f"{path}.{field}"


def raw_field(raw: str | RawDocument, path: str) -> str | None:
    if not raw:
        return None
    document = raw if isinstance(raw, RawDocument) else RawDocument(raw)
    return document.field(path)


def _audit(wire: dict[str, Any], face: str, kind: str, document: RawDocument | None):
    profile = PROFILES[face]
    losses = []
    positions = {path: token[0] for path, token in document.tokens.items()} if document else {}

    def visit(value, shape, path):
        if path not in positions:
            positions[path] = len(positions)
        if not shape:
            return
        if shape.endswith("[]"):
            if isinstance(value, list):
                for index, child in enumerate(value):
                    visit(child, shape[:-2], f"{path}[{index}]")
            return
        if not isinstance(value, dict):
            return
        fields = profile[shape]
        if "$tag" in fields:
            tag = value.get(fields["$tag"])
            target = fields["$cases"].get(tag, "") if tag is not None else fields["$default"]
            if not target:
                return
            return visit(value, target, path)
        for key, child in value.items():
            child_path = _join(path, key)
            if child_path not in positions:
                positions[child_path] = len(positions)
            if key in fields:
                visit(child, fields[key], child_path)
            else:
                losses.append(Loss(path=child_path, field=key, reason="unmapped-field"))

    visit(wire, kind, "")
    return losses, positions


def _source_path(path, positions):
    if path in positions:
        return path
    if _join("response", path) in positions:
        return _join("response", path)
    if path.startswith("output[") and "]" in path:
        tail = path.split("]", 1)[1]
        if "item" + tail in positions:
            return "item" + tail
        if tail.startswith(".content["):
            part = "part" + tail.split("]", 1)[1]
            if part in positions:
                return part
    if ".content_block" in path:
        block = "content_block" + path.split(".content_block", 1)[1]
        if block in positions:
            return block
    return path


def _merge(existing, audit, positions):
    seen = {(loss.path, loss.field, loss.reason) for loss in existing}
    combined = list(existing) + [loss for loss in audit if (loss.path, loss.field, loss.reason) not in seen]

    def order(loss):
        path = _source_path(loss.path, positions)
        if loss.reason == "unsupported-semantic":
            return positions.get(path, float("inf"))
        return positions.get(_join(path, loss.field), positions.get(path, float("inf")))

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
            audited, positions = _audit(wire, face, "event", document)
            before = len(self._losses)
            events = function(self, wire, document or "")
            self._losses[before:] = _merge(self._losses[before:], audited, positions)
            return events
        return feed
    return decorate
