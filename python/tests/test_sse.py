"""Tests for the oxa.sse frame adapter (spec/20 §6)."""

import unittest

from oxa.sse import Frame, decode_frames, encode


class SseTest(unittest.TestCase):
    def test_decode_lf(self) -> None:
        input_data = b"data: hello\n\ndata: world\n\n"
        frames = list(decode_frames(input_data))
        self.assertEqual(len(frames), 2)
        self.assertEqual(frames[0].event, "")
        self.assertEqual(frames[0].data, b"hello")
        self.assertEqual(frames[1].event, "")
        self.assertEqual(frames[1].data, b"world")

    def test_decode_crlf_and_mixed(self) -> None:
        input_data = b"data: a\r\n\r\ndata: b\n\r\n"
        frames = list(decode_frames(input_data))
        self.assertEqual(len(frames), 2)
        self.assertEqual(frames[0].data, b"a")
        self.assertEqual(frames[1].data, b"b")

    def test_decode_multiline_data(self) -> None:
        input_data = b"data: line1\ndata: line2\n\n"
        frames = list(decode_frames(input_data))
        self.assertEqual(len(frames), 1)
        self.assertEqual(frames[0].data, b"line1\nline2")

    def test_decode_event_field_last_wins(self) -> None:
        input_data = b"event: first\nevent: second\ndata: x\n\n"
        frames = list(decode_frames(input_data))
        self.assertEqual(len(frames), 1)
        self.assertEqual(frames[0].event, "second")
        self.assertEqual(frames[0].data, b"x")

    def test_decode_comments_and_unknown_fields(self) -> None:
        input_data = b": keepalive\nid: 42\nretry: 1000\nbogus: v\ndata: x\n\n"
        frames = list(decode_frames(input_data))
        self.assertEqual(len(frames), 1)
        self.assertEqual(frames[0].event, "")
        self.assertEqual(frames[0].data, b"x")

    def test_decode_empty_data(self) -> None:
        frames = list(decode_frames(b"data:\n\n"))
        self.assertEqual(len(frames), 1)
        self.assertEqual(frames[0].data, b"")

        frames2 = list(decode_frames(b"data: \n\n"))
        self.assertEqual(len(frames2), 1)
        self.assertEqual(frames2[0].data, b"")

    def test_decode_trailing_frame_without_blank_line(self) -> None:
        frames = list(decode_frames(b"data: trailing"))
        self.assertEqual(len(frames), 1)
        self.assertEqual(frames[0].data, b"trailing")

    def test_encode_and_round_trip(self) -> None:
        frame = Frame(event="message_delta", data=b'{"text": "hello"}\n{"text": "world"}')
        encoded = encode(frame)
        self.assertEqual(
            encoded,
            b'event: message_delta\ndata: {"text": "hello"}\ndata: {"text": "world"}\n\n',
        )
        round_tripped = list(decode_frames(encoded))
        self.assertEqual(len(round_tripped), 1)
        self.assertEqual(round_tripped[0].event, "message_delta")
        self.assertEqual(round_tripped[0].data, b'{"text": "hello"}\n{"text": "world"}')

    def test_encode_empty(self) -> None:
        frame = Frame(event="", data=b"")
        encoded = encode(frame)
        self.assertEqual(encoded, b"data: \n\n")


if __name__ == "__main__":
    unittest.main()
