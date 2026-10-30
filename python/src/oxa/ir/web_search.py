"""校验服务端搜索历史块，不改写 action。"""

from __future__ import annotations

from typing import Any

from oxa.ir.loss import Loss


def validate_web_search_extra(extra: object) -> None:
    if not isinstance(extra, dict):
        raise ValueError("web_search_call extra must be an object")
    if any(key in extra for key in ("type", "id", "status", "action")):
        raise ValueError("web_search_call extra cannot shadow known item fields")


def inspect_web_search(identifier: object, status: object, action: object, path: str = "web_search_call") -> list[Loss]:
    if not isinstance(identifier, str) or not identifier or not isinstance(status, str) or not status:
        raise ValueError(f"{path}: web_search_call requires non-empty id and status")
    if not isinstance(action, dict):
        raise ValueError(f"{path}: action must be an object")
    kind = action.get("type")
    if kind == "search":
        applicable = {"query", "queries", "sources"}
    elif kind == "open_page":
        applicable = {"url"}
    elif kind == "find_in_page":
        applicable = {"url", "pattern"}
        if "url" not in action or "pattern" not in action:
            raise ValueError(f"{path}: find_in_page requires url and pattern")
    else:
        raise ValueError(f"{path}: unknown or absent action.type")
    losses: list[Loss] = []
    for key, value in action.items():
        if key == "type":
            continue
        if key not in applicable:
            losses.append(Loss(path=f"{path}.action.{key}", field=key, reason="unmapped-field"))
            continue
        if kind == "open_page" and key == "url" and value is None:
            continue
        if key == "queries":
            if not isinstance(value, list) or any(not isinstance(entry, str) for entry in value):
                raise ValueError(f"{path}.action.queries must be an array of strings")
        elif key == "sources":
            if not isinstance(value, list):
                raise ValueError(f"{path}.action.sources must be an array")
            for index, source in enumerate(value):
                if not isinstance(source, dict) or source.get("type") != "url" or not isinstance(source.get("url"), str):
                    raise ValueError(f"{path}.action.sources[{index}] requires type url and string url")
                for field in source:
                    if field not in ("type", "url"):
                        losses.append(Loss(path=f"{path}.action.sources[{index}].{field}", field=field, reason="unmapped-field"))
        elif not isinstance(value, str):
            raise ValueError(f"{path}.action.{key} must be a string")
    return losses
