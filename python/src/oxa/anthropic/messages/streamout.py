"""Anthropic Messages streaming encoder: converts IR events into wire stream events (IR → face)."""

from __future__ import annotations

import json
from typing import Any

from oxa.anthropic.messages.constants import (
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_THINKING,
    BLOCK_TYPE_REDACTED_THINKING,
    BLOCK_TYPE_TOOL_USE,
    DELTA_TYPE_INPUT_JSON_DELTA,
    DELTA_TYPE_SIGNATURE_DELTA,
    DELTA_TYPE_TEXT_DELTA,
    DELTA_TYPE_THINKING_DELTA,
    DELTA_TYPE_CITATIONS,
    EVENT_TYPE_CONTENT_BLOCK_DELTA,
    EVENT_TYPE_CONTENT_BLOCK_START,
    EVENT_TYPE_CONTENT_BLOCK_STOP,
    EVENT_TYPE_MESSAGE_DELTA,
    EVENT_TYPE_MESSAGE_START,
    EVENT_TYPE_MESSAGE_STOP,
    ROLE_ASSISTANT,
    TYPE_MESSAGE,
)
from oxa.anthropic.messages.encode import encode_stop_reason
from oxa.anthropic.messages.normalize import loss
from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNSUPPORTED_SEMANTIC,
    STOP_END_TURN,
    STOP_TOOL_USE,
    ApplyPatchToolResultBlock,
    ApplyPatchToolUseBlock,
    ContentBlockDelta,
    ContentBlockStart,
    ContentBlockStop,
    CustomToolInputDelta,
    CustomToolUseBlock,
    Event,
    InputJsonDelta,
    Loss,
    MessageDelta,
    MessageDone,
    MessageStart,
    SignatureDelta,
    TextBlock,
    TextDelta,
    ThinkingBlock,
    ThinkingDelta,
    RedactedThinkingBlock,
    CitationDelta,
    ToolUseBlock,
)
from oxa.modelmap import Table


class StreamEncoder:
    """Incrementally converts an IR event stream into Anthropic Messages wire events."""

    __slots__ = (
        "_table",
        "_id",
        "_model",
        "_started",
        "_open_index",
        "_open_tool",
        "_open_custom",
        "_open_apply_patch",
        "_open_redacted",
        "_custom_input",
        "_custom_parts",
        "_tool_seen",
        "_native_index",
        "_next_native_index",
        "_open_thinking",
        "_thinking_input",
        "_thinking_signature",
        "_thinking_parts",
        "_signature_seen",
        "_tool_input",
        "_tool_parts",
        "_delta_seen",
        "_done",
    )

    def __init__(self, table: Table | None = None) -> None:
        self._table = table
        self._id = ""
        self._model = ""
        self._started = False
        self._open_index = -1
        self._open_tool = False
        self._open_custom = False
        self._open_apply_patch = False
        self._open_redacted = False
        self._custom_input = ""
        self._custom_parts: list[str] = []
        self._tool_seen = False
        self._native_index = -1
        self._next_native_index = 0
        self._open_thinking = False
        self._thinking_input = ""
        self._thinking_signature: str | None = None
        self._thinking_parts: list[str] = []
        self._signature_seen = False
        self._tool_input = ""
        self._tool_parts: list[str] = []
        self._delta_seen = False
        self._done = False

    def apply(self, ev: Event) -> tuple[list[dict[str, Any]], list[Loss]]:
        """Pushes one IR event and returns the emitted wire events and losses."""
        if self._done:
            raise ValueError("anthropic: event applied after message_done")

        if isinstance(ev, MessageStart):
            if self._started:
                raise ValueError("anthropic: duplicate MessageStart")
            self._started = True
            self._id = ev.id
            self._model = self._table.map(ev.model) if self._table else ev.model
            return [
                {
                    "type": EVENT_TYPE_MESSAGE_START,
                    "message": {
                        "id": self._id,
                        "type": TYPE_MESSAGE,
                        "role": ROLE_ASSISTANT,
                        "model": self._model,
                        "content": [],
                        "stop_reason": None,
                        "usage": {"input_tokens": 0, "output_tokens": 0},
                    },
                }
            ], []

        if isinstance(ev, ContentBlockStart):
            if not self._started or self._open_index >= 0:
                raise ValueError("anthropic: ContentBlockStart out of grammar order")

            if isinstance(ev.block, TextBlock):
                self._open_index = ev.index
                self._native_index = self._next_native_index
                self._next_native_index += 1
                self._open_tool = False
                content_block = {"type": BLOCK_TYPE_TEXT}
                if ev.block.text:
                    content_block["text"] = ev.block.text
                if ev.block.citations:
                    content_block["citations"] = [json.loads(c.value) for c in ev.block.citations]
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_START,
                        "index": self._native_index,
                        "content_block": content_block,
                    }
                ], []

            if isinstance(ev.block, ThinkingBlock):
                self._open_index = ev.index
                self._native_index = self._next_native_index
                self._next_native_index += 1
                self._open_tool = False
                self._open_thinking = True
                self._thinking_input = ev.block.thinking
                self._thinking_signature = ev.block.signature
                self._thinking_parts.clear()
                self._signature_seen = False
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_START,
                        "index": self._native_index,
                        "content_block": {"type": BLOCK_TYPE_THINKING},
                    }
                ], []

            if isinstance(ev.block, RedactedThinkingBlock):
                self._open_index = ev.index
                self._native_index = self._next_native_index
                self._next_native_index += 1
                self._open_redacted = True
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_START,
                        "index": self._native_index,
                        "content_block": {
                            "type": BLOCK_TYPE_REDACTED_THINKING,
                            "data": ev.block.data,
                        },
                    }
                ], []

            if isinstance(ev.block, CustomToolUseBlock):
                if not ev.block.id or not ev.block.name:
                    raise ValueError("anthropic: CustomToolUseBlock requires nonempty ID and name")
                self._open_index = ev.index
                self._native_index = self._next_native_index
                self._next_native_index += 1
                self._open_custom = True
                self._tool_seen = True
                self._custom_input = ev.block.input
                self._custom_parts.clear()
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_START,
                        "index": self._native_index,
                        "content_block": {
                            "type": BLOCK_TYPE_TOOL_USE,
                            "id": ev.block.id,
                            "name": ev.block.name,
                            "input": {},
                        },
                    }
                ], []

            if isinstance(ev.block, (ApplyPatchToolUseBlock, ApplyPatchToolResultBlock)):
                self._open_index = ev.index
                self._native_index = -1
                self._open_apply_patch = True
                return [], []

            if isinstance(ev.block, ToolUseBlock):
                if not ev.block.id or not ev.block.name:
                    raise ValueError("anthropic: ToolUseBlock requires nonempty ID and name")
                self._open_index = ev.index
                self._native_index = self._next_native_index
                self._next_native_index += 1
                self._open_tool = True
                self._tool_seen = True
                self._tool_input = ev.block.input
                self._tool_parts.clear()
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_START,
                        "index": self._native_index,
                        "content_block": {
                            "type": BLOCK_TYPE_TOOL_USE,
                            "id": ev.block.id,
                            "name": ev.block.name,
                            "input": {},
                        },
                    }
                ], []

            raise ValueError("anthropic: ContentBlockStart carries unsupported block")

        if isinstance(ev, ContentBlockDelta):
            if self._open_index != ev.index:
                raise ValueError("anthropic: ContentBlockDelta out of grammar order")

            if self._open_apply_patch:
                raise ValueError("anthropic: apply patch block does not accept deltas")

            if self._open_redacted:
                raise ValueError("anthropic: redacted_thinking block does not accept deltas")

            if self._open_custom:
                if not isinstance(ev.delta, CustomToolInputDelta):
                    raise ValueError("anthropic: CustomToolUseBlock received non-custom-input delta")
                self._custom_parts.append(ev.delta.text)
                return [], []

            if self._open_tool:
                if not isinstance(ev.delta, InputJsonDelta):
                    raise ValueError("anthropic: ToolUseBlock received non-input-json delta")
                self._tool_parts.append(ev.delta.partial_json)
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                        "index": self._native_index,
                        "delta": {
                            "type": DELTA_TYPE_INPUT_JSON_DELTA,
                            "partial_json": ev.delta.partial_json,
                        },
                    }
                ], []

            if self._open_thinking:
                if isinstance(ev.delta, ThinkingDelta):
                    if self._signature_seen:
                        raise ValueError("anthropic: thinking_delta after signature_delta")
                    self._thinking_parts.append(ev.delta.text)
                    return [
                        {
                            "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                            "index": self._native_index,
                            "delta": {
                                "type": DELTA_TYPE_THINKING_DELTA,
                                "thinking": ev.delta.text,
                            },
                        }
                    ], []
                if isinstance(ev.delta, SignatureDelta):
                    if self._signature_seen:
                        raise ValueError("anthropic: duplicate signature_delta")
                    self._signature_seen = True
                    return [
                        {
                            "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                            "index": self._native_index,
                            "delta": {
                                "type": DELTA_TYPE_SIGNATURE_DELTA,
                                "signature": ev.delta.signature,
                            },
                        }
                    ], []
                raise ValueError("anthropic: ThinkingBlock received non-thinking delta")

            if isinstance(ev.delta, CitationDelta):
                try:
                    cite_obj = json.loads(ev.delta.citation.value)
                except Exception as e:
                    raise ValueError(f"anthropic: citation value is not valid JSON: {e}") from e
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                        "index": self._native_index,
                        "delta": {
                            "type": DELTA_TYPE_CITATIONS,
                            "citation": cite_obj,
                        },
                    }
                ], []

            if not isinstance(ev.delta, TextDelta):
                raise ValueError("anthropic: TextBlock received non-text delta")
            return [
                {
                    "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                    "index": self._native_index,
                    "delta": {
                        "type": DELTA_TYPE_TEXT_DELTA,
                        "text": ev.delta.text,
                    },
                }
            ], []

        if isinstance(ev, ContentBlockStop):
            if self._open_index != ev.index:
                raise ValueError("anthropic: ContentBlockStop out of grammar order")

            if self._open_redacted:
                self._open_redacted = False
                self._open_index = -1
                idx = self._native_index
                self._native_index = -1
                return [
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_STOP,
                        "index": idx,
                    }
                ], []

            events: list[dict[str, Any]] = []
            if self._open_thinking:
                if not self._thinking_parts and self._thinking_input:
                    events.append(
                        {
                            "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                            "index": self._native_index,
                            "delta": {
                                "type": DELTA_TYPE_THINKING_DELTA,
                                "thinking": self._thinking_input,
                            },
                        }
                    )
                if not self._signature_seen and self._thinking_signature:
                    events.append(
                        {
                            "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                            "index": self._native_index,
                            "delta": {
                                "type": DELTA_TYPE_SIGNATURE_DELTA,
                                "signature": self._thinking_signature,
                            },
                        }
                    )
                self._open_thinking = False
                self._thinking_input = ""
                self._thinking_signature = None
                self._thinking_parts.clear()
                self._signature_seen = False

            if self._open_apply_patch:
                self._open_apply_patch = False
                self._open_index = -1
                self._native_index = -1
                return [], [loss(f"events[{ev.index}].block", "type", LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tool has no Anthropic Messages streaming mapping")]

            if self._open_custom:
                if not self._custom_parts:
                    full_input = self._custom_input
                else:
                    full_input = "".join(self._custom_parts)
                    if full_input != self._custom_input:
                        raise ValueError(
                            "anthropic: CustomToolUseBlock input does not equal custom input deltas"
                        )
                wrapped = json.dumps(
                    {"input": full_input},
                    separators=(",", ":"),
                    ensure_ascii=False,
                )
                events.append(
                    {
                        "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                        "index": self._native_index,
                        "delta": {
                            "type": DELTA_TYPE_INPUT_JSON_DELTA,
                            "partial_json": wrapped,
                        },
                    }
                )
                self._open_custom = False
                self._custom_input = ""
                self._custom_parts.clear()

            if self._open_tool:
                if not self._tool_parts:
                    events.append(
                        {
                            "type": EVENT_TYPE_CONTENT_BLOCK_DELTA,
                            "index": self._native_index,
                            "delta": {
                                "type": DELTA_TYPE_INPUT_JSON_DELTA,
                                "partial_json": self._tool_input,
                            },
                        }
                    )
                else:
                    if "".join(self._tool_parts) != self._tool_input:
                        raise ValueError(
                            "anthropic: ToolUseBlock input does not equal concatenated InputJSONDelta fragments"
                        )
                self._open_tool = False
                self._tool_input = ""
                self._tool_parts.clear()

            self._open_index = -1
            events.append(
                {
                    "type": EVENT_TYPE_CONTENT_BLOCK_STOP,
                    "index": self._native_index,
                }
            )
            self._native_index = -1
            return events, []

        if isinstance(ev, MessageDelta):
            if not self._started or self._open_index >= 0:
                raise ValueError("anthropic: MessageDelta out of grammar order")
            stop_reason = ev.stop_reason
            losses: list[Loss] = []
            if stop_reason == STOP_TOOL_USE and not self._tool_seen:
                stop_reason = STOP_END_TURN
                losses.append(loss("stop_reason", "stop_reason", LOSS_DEGRADED, "tool_use was degraded because no native function call was retained"))
            reason, seq = encode_stop_reason(stop_reason, ev.stop_sequence)
            self._delta_seen = True
            delta_dict: dict[str, Any] = {"stop_reason": reason}
            if seq is not None:
                delta_dict["stop_sequence"] = seq
            usage = {
                "input_tokens": ev.usage.input_tokens,
                "output_tokens": ev.usage.output_tokens,
            }
            if ev.usage.cache_read_input_tokens is not None:
                usage["cache_read_input_tokens"] = ev.usage.cache_read_input_tokens
            if ev.usage.cache_creation_input_tokens is not None:
                usage["cache_creation_input_tokens"] = ev.usage.cache_creation_input_tokens
            return [
                {
                    "type": EVENT_TYPE_MESSAGE_DELTA,
                    "delta": delta_dict,
                    "usage": usage,
                }
            ], losses

        if isinstance(ev, MessageDone):
            if not self._delta_seen:
                raise ValueError("anthropic: MessageDone out of grammar order")
            self._done = True
            return [{"type": EVENT_TYPE_MESSAGE_STOP}], []

        raise ValueError(f"anthropic: unknown event type {type(ev)}")
