import unittest

import oxa


class SmokeTest(unittest.TestCase):
    def test_version(self) -> None:
        self.assertEqual(oxa.__version__, "2.4.1")


if __name__ == "__main__":
    unittest.main()
