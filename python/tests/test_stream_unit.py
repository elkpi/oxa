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
