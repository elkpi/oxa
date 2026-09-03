"""Tests for the modelmap module (spec/03)."""

import unittest

from oxa.modelmap import Table


class ModelMapTest(unittest.TestCase):
    def test_empty_table_is_identity(self) -> None:
        table = Table()
        self.assertEqual(table.map("gpt-4o-mini"), "gpt-4o-mini")

    def test_exact_matches_are_rewritten(self) -> None:
        table = Table()
        table.insert("gpt-4o-mini", "claude-haiku-4-5")
        self.assertEqual(table.map("gpt-4o-mini"), "claude-haiku-4-5")

    def test_misses_fall_back_to_identity(self) -> None:
        table = Table()
        table.insert("gpt-4o-mini", "claude-haiku-4-5")
        self.assertEqual(table.map("gpt-4o"), "gpt-4o")

    def test_init_from_mapping(self) -> None:
        table = Table({"a": "b"})
        self.assertEqual(table.map("a"), "b")
        self.assertEqual(table.map("c"), "c")


if __name__ == "__main__":
    unittest.main()
