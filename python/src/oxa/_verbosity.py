from typing import Any

from oxa.ir import LOSS_UNMAPPED_VALUE, Loss


def decode_verbosity(value: Any, path: str, losses: list[Loss]) -> str | None:
    if value is None:
        return None
    if not isinstance(value, str):
        raise ValueError(f"{path} must be a string or null")
    if value not in ("low", "medium", "high"):
        losses.append(Loss(path=path, field="verbosity", reason=LOSS_UNMAPPED_VALUE))
        return None
    return value
