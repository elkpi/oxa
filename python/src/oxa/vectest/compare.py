"""Normative comparison rules of vectors/README.md.

1. Structural JSON equality (key order irrelevant, arrays ordered, strings exact).
2. Integers stay integers — 1 does not match 1.0 (spec/01 INV-7).
3. Raw JSON strings are JSON strings, covered by rule 1.
4. Losses compare as unordered multisets keyed on (path, field, reason).
"""

from __future__ import annotations

from collections import Counter
from typing import Any

from oxa.ir import Loss


def compare_json(expected: Any, actual: Any, path: str = "") -> None:
    """Compares two parsed JSON values structurally under INV-7.

    Raises AssertionError describing the first mismatch path.
    """
    if isinstance(expected, bool) or isinstance(actual, bool):
        if not (isinstance(expected, bool) and isinstance(actual, bool)):
            loc = path or "<root>"
            raise AssertionError(
                f"type mismatch at {loc}: expected {type(expected).__name__}, got {type(actual).__name__}"
            )
        if expected != actual:
            loc = path or "<root>"
            raise AssertionError(f"bool differs at {loc}: expected {expected}, got {actual}")
        return

    if isinstance(expected, int) or isinstance(actual, int):
        if not (isinstance(expected, int) and isinstance(actual, int)):
            loc = path or "<root>"
            raise AssertionError(
                f"type mismatch at {loc}: expected {type(expected).__name__}, got {type(actual).__name__}"
            )
        if expected != actual:
            loc = path or "<root>"
            raise AssertionError(f"int differs at {loc}: expected {expected}, got {actual}")
        return

    if isinstance(expected, float) or isinstance(actual, float):
        if not (isinstance(expected, float) and isinstance(actual, float)):
            loc = path or "<root>"
            raise AssertionError(
                f"type mismatch at {loc}: expected {type(expected).__name__}, got {type(actual).__name__}"
            )
        if expected != actual:
            loc = path or "<root>"
            raise AssertionError(f"float differs at {loc}: expected {expected}, got {actual}")
        return

    if isinstance(expected, str) or isinstance(actual, str):
        if not (isinstance(expected, str) and isinstance(actual, str)):
            loc = path or "<root>"
            raise AssertionError(
                f"type mismatch at {loc}: expected {type(expected).__name__}, got {type(actual).__name__}"
            )
        if expected != actual:
            loc = path or "<root>"
            raise AssertionError(f"string differs at {loc}: expected {expected!r}, got {actual!r}")
        return

    if expected is None or actual is None:
        if expected is not actual:
            loc = path or "<root>"
            raise AssertionError(f"null differs at {loc}: expected {expected}, got {actual}")
        return

    if isinstance(expected, list) or isinstance(actual, list):
        if not (isinstance(expected, list) and isinstance(actual, list)):
            loc = path or "<root>"
            raise AssertionError(
                f"type mismatch at {loc}: expected {type(expected).__name__}, got {type(actual).__name__}"
            )
        if len(expected) != len(actual):
            loc = path or "<root>"
            raise AssertionError(
                f"array length mismatch at {loc}: expected {len(expected)}, got {len(actual)}"
            )
        for i, (exp_item, act_item) in enumerate(zip(expected, actual)):
            compare_json(exp_item, act_item, f"{path}[{i}]")
        return

    if isinstance(expected, dict) or isinstance(actual, dict):
        if not (isinstance(expected, dict) and isinstance(actual, dict)):
            loc = path or "<root>"
            raise AssertionError(
                f"type mismatch at {loc}: expected {type(expected).__name__}, got {type(actual).__name__}"
            )
        for k, v in expected.items():
            if k not in actual:
                loc = path or "<root>"
                raise AssertionError(f"missing key {k!r} at {loc}")
            subpath = f"{path}.{k}" if path else str(k)
            compare_json(v, actual[k], subpath)
        for k in actual:
            if k not in expected:
                loc = path or "<root>"
                raise AssertionError(f"unexpected key {k!r} at {loc}")
        return

    loc = path or "<root>"
    raise AssertionError(f"unhandled JSON type at {loc}: {type(expected)}")


def compare_losses(expected: list[Loss], reported: list[Loss]) -> None:
    """Compares losses as an unordered multiset keyed on (path, field, reason).

    detail is informational and not part of the matching key.
    """
    exp_counts: Counter[tuple[str, str, str]] = Counter(
        (loss.path, loss.field, loss.reason) for loss in expected
    )
    rep_counts: Counter[tuple[str, str, str]] = Counter(
        (loss.path, loss.field, loss.reason) for loss in reported
    )

    problems: list[str] = []
    for key, count in exp_counts.items():
        if rep_counts.get(key, 0) < count:
            problems.append(
                f"expected loss not reported (or reported fewer times): "
                f"path={key[0]!r} field={key[1]!r} reason={key[2]!r}"
            )
    for key, count in rep_counts.items():
        if exp_counts.get(key, 0) < count:
            problems.append(
                f"unexpected loss reported: "
                f"path={key[0]!r} field={key[1]!r} reason={key[2]!r}"
            )

    if problems:
        raise AssertionError("; ".join(problems))
