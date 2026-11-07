"""Raw JSON number token preservation for the Jev face (spec/17 §7.5/§7.6).

``json.loads`` rounds number literals through IEEE 754 doubles and renders
them back in a normalized form, so a verbatim carry (INV-1 discipline) would
rewrite ``1.0000000000000001`` as ``1.0`` and turn ``1e400`` into the
non-JSON ``Infinity``. Raw entrypoints therefore parse numbers into a token
holder: ``token`` preserves the source spelling and ``value`` is the exact
decimal for range validation.
"""

from decimal import Decimal


class NumberToken:
    """A JSON number kept as its source text plus its exact decimal value."""

    __slots__ = ("token", "value")

    def __init__(self, token: str) -> None:
        self.token = token
        self.value = Decimal(token)

    def __eq__(self, other: object) -> bool:
        if isinstance(other, NumberToken):
            return self.value == other.value
        if isinstance(other, bool) or not isinstance(other, (int, float, Decimal)):
            return NotImplemented
        return self.value == Decimal(str(other))

    def __hash__(self) -> int:
        return hash(self.value)

    def __repr__(self) -> str:
        return f"NumberToken({self.token!r})"
