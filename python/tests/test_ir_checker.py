"""Event-stream invariant tests (spec/01 §7): INV-5, INV-6, and INV-1."""

import unittest

from oxa.ir import (
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    Event,
    EventStream,
    InputJsonDelta,
    MessageDelta,
    MessageDone,
    MessageStart,
    STOP_END_TURN,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    TextBlock,
    TextDelta,
    ToolUseBlock,
    Usage,
    Violation,
    validate_event_stream,
    validate_event_stream_for_encoder,
)


def start() -> Event:
    return MessageStart(id="m", model="model")


def done() -> Event:
    return MessageDone()


def text_block_start(index: int, text: str = "") -> Event:
    return ContentBlockStart(index=index, block=TextBlock(text=text))


def text_delta(index: int, text: str) -> Event:
    return ContentBlockDelta(index=index, delta=TextDelta(text=text))


def tool_start(index: int, input_text: str) -> Event:
    return ContentBlockStart(
        index=index,
        block=ToolUseBlock(id="call_1", name="get_weather", input=input_text),
    )


def input_delta(index: int, fragment: str) -> Event:
    return ContentBlockDelta(index=index, delta=InputJsonDelta(partial_json=fragment))


def block_stop(index: int) -> Event:
    return ContentBlockStop(index=index)


def message_delta(stop: str = STOP_END_TURN, seq: str | None = None) -> Event:
    return MessageDelta(
        stop_reason=stop,
        stop_sequence=seq,
        usage=Usage(input_tokens=0, output_tokens=0),
    )


class CheckerTest(unittest.TestCase):
    def assert_rejects(self, events: list[Event], event_index: int, fragment: str) -> None:
        stream = EventStream(events=events)
        with self.assertRaises(Violation) as cm:
            validate_event_stream(stream)
        err = cm.exception
        self.assertEqual(
            err.event,
            event_index,
            f"violation location mismatch, got {err.event}, want {event_index}",
        )
        self.assertIn(fragment, err.message)

    def test_complete_text_stream_passes(self) -> None:
        events = [
            start(),
            text_block_start(0),
            text_delta(0, "hello"),
            block_stop(0),
            message_delta(STOP_END_TURN),
            done(),
        ]
        validate_event_stream(EventStream(events=events))

    def test_accepts_valid_text_and_tool_stream(self) -> None:
        events = [
            start(),
            text_block_start(0),
            text_delta(0, "hello"),
            block_stop(0),
            tool_start(1, '{"x":1'),
            input_delta(1, ""),
            input_delta(1, '{"x":1'),
            block_stop(1),
            message_delta(STOP_TOOL_USE),
            done(),
        ]
        validate_event_stream(EventStream(events=events))

    def test_accepts_empty_tool_input_without_fragments(self) -> None:
        events = [
            start(),
            tool_start(0, ""),
            block_stop(0),
            message_delta(STOP_TOOL_USE),
            done(),
        ]
        validate_event_stream(EventStream(events=events))

    def test_accepts_stop_sequence_with_matching_reason(self) -> None:
        events = [
            start(),
            message_delta(STOP_STOP_SEQUENCE, seq="END"),
            done(),
        ]
        validate_event_stream(EventStream(events=events))

    def test_strict_check_rejects_synthesized_tool_input(self) -> None:
        events = [
            start(),
            tool_start(0, '{"x":1}'),
            block_stop(0),
            message_delta(STOP_TOOL_USE),
            done(),
        ]
        self.assert_rejects(events, 2, "without input_json_delta fragments")
        validate_event_stream_for_encoder(EventStream(events=events))

    def test_rejects_missing_message_start(self) -> None:
        self.assert_rejects(
            [message_delta(STOP_END_TURN), done()],
            0,
            "message_start",
        )

    def test_rejects_second_open_block(self) -> None:
        self.assert_rejects(
            [
                start(),
                text_block_start(0),
                text_block_start(1),
                block_stop(0),
                message_delta(STOP_END_TURN),
                done(),
            ],
            2,
            "open block",
        )

    def test_rejects_first_index_not_zero(self) -> None:
        self.assert_rejects(
            [
                start(),
                text_block_start(1),
                block_stop(1),
                message_delta(STOP_END_TURN),
                done(),
            ],
            1,
            "index",
        )

    def test_rejects_text_delta_on_tool_block(self) -> None:
        self.assert_rejects(
            [
                start(),
                tool_start(0, "{}"),
                text_delta(0, "hello"),
                block_stop(0),
                message_delta(STOP_TOOL_USE),
                done(),
            ],
            2,
            "delta type",
        )

    def test_rejects_input_delta_on_text_block(self) -> None:
        self.assert_rejects(
            [
                start(),
                text_block_start(0),
                input_delta(0, "{}"),
                block_stop(0),
                message_delta(STOP_END_TURN),
                done(),
            ],
            2,
            "delta type",
        )

    def test_rejects_input_not_equal_to_fragment_concatenation(self) -> None:
        self.assert_rejects(
            [
                start(),
                tool_start(0, '{"x":1}'),
                input_delta(0, '{"x":1'),
                input_delta(0, '"}'),
                block_stop(0),
                message_delta(STOP_TOOL_USE),
                done(),
            ],
            4,
            "concatenation",
        )

    def test_rejects_delta_on_wrong_index(self) -> None:
        self.assert_rejects(
            [
                start(),
                text_block_start(0),
                text_delta(1, "hello"),
                block_stop(0),
                message_delta(STOP_END_TURN),
                done(),
            ],
            2,
            "index",
        )

    def test_rejects_stop_on_wrong_index(self) -> None:
        self.assert_rejects(
            [
                start(),
                text_block_start(0),
                block_stop(1),
                message_delta(STOP_END_TURN),
                done(),
            ],
            2,
            "index",
        )

    def test_rejects_message_done_without_preceding_delta(self) -> None:
        self.assert_rejects([start(), done()], 1, "message_delta")

    def test_rejects_event_after_message_done(self) -> None:
        self.assert_rejects(
            [start(), message_delta(STOP_END_TURN), done(), done()],
            3,
            "after message_done",
        )

    def test_rejects_missing_message_delta_at_eof(self) -> None:
        self.assert_rejects([start()], 1, "message_delta")

    def test_rejects_missing_message_done_at_eof(self) -> None:
        self.assert_rejects(
            [start(), message_delta(STOP_END_TURN)],
            2,
            "message_done",
        )

    def test_rejects_open_block_at_eof(self) -> None:
        self.assert_rejects([start(), text_block_start(0)], 2, "not stopped")

    def test_rejects_stop_sequence_without_matching_reason(self) -> None:
        events = [
            start(),
            message_delta(STOP_END_TURN, seq="END"),
            done(),
        ]
        stream = EventStream(events=events)
        with self.assertRaises(Violation) as cm:
            validate_event_stream(stream)
        self.assertIn("stop_sequence", cm.exception.message)


if __name__ == "__main__":
    unittest.main()
