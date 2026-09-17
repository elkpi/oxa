"""Streaming unit tests beyond golden vectors: lifecycle order, error paths, and grammar constraints."""

import unittest

from oxa.anthropic.messages import (
    StreamDecoder as AnthropicStreamDecoder,
    StreamEncoder as AnthropicStreamEncoder,
)
from oxa.ir import (
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    EventStream,
    InputJsonDelta,
    MessageDelta,
    MessageDone,
    MessageStart,
    STOP_END_TURN,
    STOP_TOOL_USE,
    TextBlock,
    TextDelta,
    ToolUseBlock,
    Usage,
)
from oxa.openai.chatcompletions import (
    StreamDecoder as ChatCompletionsStreamDecoder,
    StreamEncoder as ChatCompletionsStreamEncoder,
)
from oxa.openai.responses import (
    StreamDecoder as ResponsesStreamDecoder,
    StreamEncoder as ResponsesStreamEncoder,
)


class StreamUnitTests(unittest.TestCase):
    def test_chatcompletions_decoder_flush_twice_is_error(self) -> None:
        dec = ChatCompletionsStreamDecoder()
        dec.feed(
            {
                "id": "c1",
                "model": "m",
                "choices": [{"index": 0, "delta": {"content": "hi"}, "finish_reason": "stop"}],
            }
        )
        dec.flush()
        with self.assertRaises(ValueError) as cm:
            dec.flush()
        self.assertIn("flushed twice", str(cm.exception))

    def test_anthropic_decoder_feed_after_stop_is_error(self) -> None:
        dec = AnthropicStreamDecoder()
        dec.feed({"type": "message_start", "message": {"id": "m1", "model": "m"}})
        dec.feed({"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}})
        dec.feed({"type": "content_block_stop", "index": 0})
        dec.feed({"type": "message_delta", "delta": {"stop_reason": "end_turn"}, "usage": {"output_tokens": 1}})
        dec.feed({"type": "message_stop"})
        with self.assertRaises(ValueError) as cm:
            dec.feed({"type": "message_stop"})
        self.assertIn("after message_stop", str(cm.exception))

    def _responses_skipped_part_open(self) -> ResponsesStreamDecoder:
        dec = ResponsesStreamDecoder()
        dec.feed(
            {
                "type": "response.created",
                "response": {
                    "id": "r1",
                    "object": "response",
                    "status": "in_progress",
                    "model": "m",
                    "output": [],
                },
            }
        )
        dec.feed(
            {
                "type": "response.output_item.added",
                "output_index": 0,
                "item": {
                    "type": "message",
                    "id": "i1",
                    "role": "assistant",
                    "status": "in_progress",
                    "content": [],
                },
            }
        )
        dec.feed(
            {
                "type": "response.content_part.added",
                "item_id": "i1",
                "output_index": 0,
                "content_index": 0,
                "part": {"type": "output_image"},
            }
        )
        return dec

    def test_responses_unknown_descendant_of_skipped_part_is_absorbed(self) -> None:
        dec = self._responses_skipped_part_open()
        events = dec.feed(
            {
                "type": "response.output_image.delta",
                "item_id": "i1",
                "output_index": 0,
                "content_index": 0,
                "delta": "x",
            }
        )
        self.assertEqual(events, [])
        self.assertEqual(len(dec.losses()), 1)
        self.assertEqual(dec.losses()[0].path, "output[0].content[0]")
        self.assertEqual(dec.losses()[0].reason, "unsupported-semantic")

    def test_responses_unknown_descendant_with_wrong_item_is_error(self) -> None:
        dec = self._responses_skipped_part_open()
        with self.assertRaises(ValueError) as cm:
            dec.feed(
                {
                    "type": "response.output_image.delta",
                    "item_id": "other",
                    "output_index": 0,
                    "content_index": 0,
                    "delta": "x",
                }
            )
        self.assertIn("does not match", str(cm.exception))
        self.assertEqual(len(dec.losses()), 1)

    def test_responses_identity_less_unknown_event_records_one_loss(self) -> None:
        dec = ResponsesStreamDecoder()
        dec.feed(
            {
                "type": "response.created",
                "response": {
                    "id": "r1",
                    "object": "response",
                    "status": "in_progress",
                    "model": "m",
                    "output": [],
                },
            }
        )
        events = dec.feed({"type": "response.weird_event"})
        self.assertEqual(events, [])
        self.assertEqual(len(dec.losses()), 1)
        self.assertEqual(dec.losses()[0].path, "type")

    def test_responses_decoder_unstarted_event_is_error(self) -> None:
        dec = ResponsesStreamDecoder()
        with self.assertRaises(ValueError) as cm:
            dec.feed({"type": "response.output_item.added", "output_index": 0, "item": {"type": "message"}})
        self.assertIn("before response.created", str(cm.exception))

    def test_responses_encoder_apply_after_termination_is_error(self) -> None:
        enc = ResponsesStreamEncoder()
        enc.apply(MessageStart(id="r1", model="m"))
        enc.apply(MessageDelta(stop_reason=STOP_END_TURN, usage=Usage()))
        enc.apply(MessageDone())
        with self.assertRaises(ValueError) as cm:
            enc.apply(MessageStart(id="r2", model="m"))
        self.assertIn("after stream termination", str(cm.exception))


if __name__ == "__main__":
    unittest.main()
