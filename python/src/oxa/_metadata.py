"""Request metadata mapping (spec 2.5.0)."""

from __future__ import annotations

from typing import Any

from oxa.ir import LOSS_UNMAPPED_FIELD, Loss


def string_metadata(value: Any, face: str) -> dict[str, str] | None:
    if value is None:
        return None
    if not isinstance(value, dict):
        raise ValueError(f"{face}: metadata must be an object")
    out: dict[str, str] = {}
    for key, item in value.items():
        if not isinstance(item, str):
            raise ValueError(f"{face}: metadata.{key} must be a string")
        out[key] = item
    return out or None


def anthropic_metadata(value: Any) -> tuple[dict[str, str] | None, list[Loss]]:
    if value is None:
        return None, []
    if not isinstance(value, dict):
        raise ValueError("anthropic: metadata must be an object")
    losses: list[Loss] = []
    user_id: str | None = None
    for key, item in value.items():
        if key != "user_id":
            losses.append(
                Loss(
                    path=f"metadata.{key}",
                    field=key,
                    reason=LOSS_UNMAPPED_FIELD,
                    detail="Anthropic request metadata only preserves user_id; this key is dropped.",
                )
            )
            continue
        if not isinstance(item, str):
            raise ValueError("anthropic: metadata.user_id must be a string")
        user_id = item
    if user_id is None:
        return None, losses
    return {"user_id": user_id}, losses


def encode_anthropic_metadata(
    metadata: dict[str, str] | None,
) -> tuple[dict[str, str] | None, list[Loss]]:
    if not metadata:
        return None, []
    losses: list[Loss] = []
    user_id: str | None = None
    for key in sorted(metadata):
        if key == "user_id":
            user_id = metadata[key]
            continue
        losses.append(
            Loss(
                path=f"metadata.{key}",
                field=key,
                reason=LOSS_UNMAPPED_FIELD,
                detail="Anthropic request metadata only preserves user_id; this IR metadata key is dropped.",
            )
        )
    if user_id is None:
        return None, losses
    return {"user_id": user_id}, losses
