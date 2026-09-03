"""The single, optional model-renaming injection point defined by spec/03.

oxa libraries carry no built-in model knowledge; callers may supply a Table,
and the table is applied to the model value on both conversion directions.
The model string is otherwise opaque and passes through verbatim.
"""

from __future__ import annotations

from typing import Mapping


class Table:
    """Maps model names to model names (spec/03).

    Lookup is exact-match on the keys; on a miss (or with an empty table) the
    identity fallback applies and the value is returned unchanged.
    """

    __slots__ = ("_entries",)

    def __init__(self, entries: Mapping[str, str] | None = None) -> None:
        self._entries: dict[str, str] = dict(entries) if entries is not None else {}

    def insert(self, from_model: str, to_model: str) -> None:
        self._entries[from_model] = to_model

    def map(self, model: str) -> str:
        return self._entries.get(model, model)
