"""Responses web_search_call 历史项（spec/11 N-R-16）。"""

from __future__ import annotations

from typing import Any

from oxa.ir.loss import Loss
from oxa.ir.types import WebSearchCallBlock
from oxa.ir.web_search import inspect_web_search


def decode_web_search_item(item: dict[str, Any], path: str) -> tuple[WebSearchCallBlock, list[Loss]]:
    identifier = item.get("id")
    status = item.get("status")
    action = item.get("action")
    losses = inspect_web_search(identifier, status, action, path)
    extra = {key: value for key, value in item.items() if key not in ("type", "id", "status", "action")}
    return WebSearchCallBlock(id=identifier, status=status, action=dict(action), extra=extra), losses
