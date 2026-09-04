import unittest

import oxa


class SmokeTest(unittest.TestCase):
    def test_version(self) -> None:
        self.assertEqual(oxa.__version__, "1.0.0")


if __name__ == "__main__":
    unittest.main()
