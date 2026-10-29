"""校验并保留响应格式 schema 的原始 JSON 文本。"""

from __future__ import annotations

import json
from typing import Any


def _same_value(left: Any, right: Any) -> bool:
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(_same_value(left[key], right[key]) for key in left)
    if isinstance(left, list):
        return len(left) == len(right) and all(_same_value(a, b) for a, b in zip(left, right))
    return left == right


def _reject_constant(_: str) -> None:
    raise ValueError("invalid JSON constant")


def serialize_json_with_schema(value: dict[str, Any], path: tuple[str, ...], schema: dict[str, Any], raw: str | None) -> str:
    if raw is None:
        return json.dumps(value, ensure_ascii=False, allow_nan=False, separators=(",", ":"))
    from oxa._wire import RawDocument

    try:
        parsed = json.loads(raw, parse_constant=_reject_constant)
    except (TypeError, json.JSONDecodeError) as exc:
        raise ValueError("response format schema_raw must be a JSON object") from exc
    if not isinstance(parsed, dict) or not _same_value(parsed, schema):
        raise ValueError("response format schema_raw does not match schema")
    root = dict(value)
    current = root
    for key in path[:-1]:
        child = current.get(key)
        if not isinstance(child, dict):
            raise ValueError("response format schema not found in encoded JSON")
        current[key] = dict(child)
        current = current[key]
    if path[-1] not in current:
        raise ValueError("response format schema not found in encoded JSON")
    current[path[-1]] = {}
    text = json.dumps(root, ensure_ascii=False, allow_nan=False, separators=(",", ":"))
    token = RawDocument(text).tokens.get(path)
    if token is None:
        raise ValueError("response format schema not found in encoded JSON")
    start, end = token
    return text[:start] + raw + text[end:]
