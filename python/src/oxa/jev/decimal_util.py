"""Exact decimal helpers for the Jev face (spec/17 §4.1).

Range and integer validation must not round JSON number tokens through
IEEE 754 doubles: 1.0000000000000001 is greater than 1 and 1.5 is not a
score level, even though both collapse to the same float. Parsing the
aggregated model output with ``parse_float=Decimal`` keeps every token
exact, and ``decimal`` comparisons are exact for any magnitude — no
expansion of adversarial powers of ten into memory.
"""

from decimal import Decimal


def decimal_is_zero(value: Decimal) -> bool:
    return value == 0


def decimal_in_unit_interval(value: Decimal) -> bool:
    """Reports whether the value is exactly within [0,1] (noul range)."""
    return 0 <= value <= 1


def decimal_equals_one(value: Decimal) -> bool:
    """Reports whether the value equals exactly 1 in decimal terms."""
    return value == 1


def score_token_within_levels(value: Decimal, levels: int) -> bool:
    """Validates a score value against the discrete integer 1..N levels."""
    if value != value.to_integral_value():
        return False
    return 1 <= value <= levels
