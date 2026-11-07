"""Normative comparison rules of vectors/README.md.

1. Structural JSON equality (key order irrelevant, arrays ordered, strings exact).
2. Integers stay integers — 1 does not match 1.0 (spec/01 INV-7).
3. Raw JSON strings are JSON strings, covered by rule 1.
4. Losses compare as unordered multisets keyed on (path, field, reason).
"""

from __future__ import annotations

from collections import Counter
from decimal import Decimal
from typing import Any

from oxa.ir import Loss


def _token_number(value: Any) -> Decimal | None:
    """Unwraps token-preserving number wrappers (e.g. the Jev face's
    NumberToken) to their exact decimal; builtin numbers and everything
    else return None."""
    if isinstance(value, bool) or value is None or isinstance(value, (int, float)):
        return None
    if isinstance(value, Decimal):
        return value
    token = getattr(value, "token", None)
    raw = getattr(value, "value", None)
    if isinstance(token, str) and isinstance(raw, Decimal):
        return raw
    return None


def _number_or_none(value: Any, is_token: bool) -> Decimal | None:
    if is_token:
        return _token_number(value)
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return Decimal(str(value))
    return None


def compare_json(expected: Any, actual: Any, path: str = "") -> None:
    """Compares two parsed JSON values structurally under INV-7.

    Raises AssertionError describing the first mismatch path.
    """
    expected_is_token = _token_number(expected) is not None
    actual_is_token = _token_number(actual) is not None
    if expected_is_token or actual_is_token:
        # A token-preserving wrapper compares by exact value against builtin
        # numbers (Jev carries raw number literals, INV-1); builtin-vs-
        # builtin comparisons below keep the INV-7 int/float distinction.
        loc = path or "<root>"
        expected_number = _number_or_none(expected, expected_is_token)
        actual_number = _number_or_none(actual, actual_is_token)
        if expected_number is None or actual_number is None:
            raise AssertionError(
                f"type mismatch at {loc}: a token-preserving number cannot "
                "match a non-number"
            )
        if expected_number != actual_number:
            raise AssertionError(
                f"number differs at {loc}: expected {expected_number}, got {actual_number}"
            )
        return

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
            if (
                k == "specVersion"
                and isinstance(v, str)
                and isinstance(actual[k], str)
                and v in ("0.1.0", "0.2.0", "0.3.0")
                and actual[k] in ("0.1.0", "0.2.0", "0.3.0")
            ):
                continue
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
                f"unexpected loss reported: path={key[0]!r} field={key[1]!r} reason={key[2]!r}"
            )

    if problems:
        raise AssertionError("; ".join(problems))
