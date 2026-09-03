import unittest

import oxa


class SmokeTest(unittest.TestCase):
    def test_version(self) -> None:
        self.assertEqual(oxa.__version__, "0.2.0")


if __name__ == "__main__":
    unittest.main()
