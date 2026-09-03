"""Tests for the oxa.vectest package."""

import unittest

from oxa.ir import Loss
from oxa.vectest import (
    compare_json,
    compare_losses,
    find_repo_root,
    load_vectors,
)


class VectestHarnessTest(unittest.TestCase):
    def test_find_repo_root(self) -> None:
        root = find_repo_root()
        self.assertIsNotNone(root)
        assert root is not None
        self.assertTrue((root / "vectors").is_dir())
        self.assertTrue((root / ".git").is_dir())

    def test_load_vectors_loads_and_sorts(self) -> None:
        root = find_repo_root()
        self.assertIsNotNone(root)
        assert root is not None
        vectors = load_vectors(root, "chatcompletions", "nonstream")
        self.assertGreater(len(vectors), 0)
        names = [v.name for v in vectors]
        self.assertEqual(names, sorted(names))

    def test_compare_json_structural_equality(self) -> None:
        # Key order insensitive
        compare_json({"a": 1, "b": "two"}, {"b": "two", "a": 1})

        # Arrays are ordered
        compare_json([1, 2, 3], [1, 2, 3])
        with self.assertRaises(AssertionError):
            compare_json([1, 2, 3], [1, 3, 2])

        # Integers stay integers (INV-7)
        with self.assertRaises(AssertionError):
            compare_json({"x": 1}, {"x": 1.0})

        # Bool vs int
        with self.assertRaises(AssertionError):
            compare_json({"x": True}, {"x": 1})

    def test_compare_losses_multiset(self) -> None:
        l1 = Loss(path="p", field="f", reason="r", detail="d1")
        l2 = Loss(path="p", field="f", reason="r", detail="d2")
        # Same key, two instances
        compare_losses([l1, l2], [l2, l1])

        # Missing instance
        with self.assertRaises(AssertionError):
            compare_losses([l1, l2], [l1])

        # Unexpected instance
        with self.assertRaises(AssertionError):
            compare_losses([l1], [l1, l2])


if __name__ == "__main__":
    unittest.main()
