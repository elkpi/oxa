"""Loss records reporting conversion fidelity costs (spec/02)."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True, slots=True)
class Loss:
    """Reports a fidelity cost of a conversion (spec/02 §2).

    Losses are first-class output, never silently dropped, and never turned into errors.
    """

    path: str
    field: str
    reason: str
    detail: str = ""

    def to_dict(self) -> dict[str, Any]:
        out: dict[str, Any] = {
            "path": self.path,
            "field": self.field,
            "reason": self.reason,
        }
        if self.detail:
            out["detail"] = self.detail
        return out

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> Loss:
        return cls(
            path=str(data.get("path", "")),
            field=str(data["field"]),
            reason=str(data["reason"]),
            detail=str(data.get("detail", "")),
        )
