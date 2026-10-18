"""Tests for v2.4.0 Anthropic request fidelity and native options across IR."""

import dataclasses
import json
import unittest

from oxa.anthropic.messages import (
    decode_request as an_decode_request,
    encode_request as an_encode_request,
)
from oxa.ir import (
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    Block,
    CodecError,
    Loss,
    Message,
    Params,
    Request,
    Response,
    TextBlock,
    Usage,
    dump_block,
    dump_request,
    dump_response,
    load_request,
)
from oxa.ir.native import (
    NATIVE_KIND_CACHE_CONTROL,
    NATIVE_KIND_CONTEXT_MANAGEMENT,
    NATIVE_KIND_THINKING_BUDGET,
    NATIVE_KIND_THINKING_MODE,
    NATIVE_SOURCE_ANTHROPIC,
    NativeAnnotation,
    NativeOmission,
    NativeProjection,
    omitted_native_request_losses,
    validate_native_request,
)
from oxa.openai.chatcompletions import (
    encode_request as cc_encode_request,
)
from oxa.openai.responses import (
    encode_request as resp_encode_request,
)


class NativeRequestTest(unittest.TestCase):
    def test_native_portable_round_trip(self) -> None:
        req = Request(
            model="claude-3-7-sonnet-20250219",
            messages=[
                Message(
                    role="user",
                    content=[
                        TextBlock(
                            text="Hello",
                            native=(
                                NativeAnnotation(
                                    source=NATIVE_SOURCE_ANTHROPIC,
                                    kind=NATIVE_KIND_CACHE_CONTROL,
                                    value=json.dumps({"type": "ephemeral", "ttl": "1h"}),
                                    omission=NativeOmission(
                                        field="cache_control",
                                        reason=LOSS_UNMAPPED_FIELD,
                                    ),
                                ),
                            ),
                        )
                    ],
                )
            ],
            system=[
                TextBlock(
                    text="System prompt",
                    native=(
                        NativeAnnotation(
                            source=NATIVE_SOURCE_ANTHROPIC,
                            kind=NATIVE_KIND_CACHE_CONTROL,
                            value=json.dumps({"type": "ephemeral"}),
                            omission=NativeOmission(
                                field="cache_control",
                                reason=LOSS_UNMAPPED_FIELD,
                            ),
                        ),
                    ),
                )
            ],
            params=Params(reasoning_effort="xhigh"),
            native=(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_THINKING_MODE,
                    value=json.dumps("adaptive"),
                    omission=NativeOmission(
                        field="type",
                        reason=LOSS_UNMAPPED_FIELD,
                    ),
                ),
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_CONTEXT_MANAGEMENT,
                    value=json.dumps({"edits": [{"type": "clear_thinking_20251015", "keep": "all"}]}),
                    omission=NativeOmission(
                        field="context_management",
                        reason=LOSS_UNSUPPORTED_SEMANTIC,
                    ),
                ),
            ),
        )

        dumped = dump_request(req)
        json_str = json.dumps(dumped)
        loaded = load_request(json.loads(json_str))

        self.assertEqual(len(loaded.native), 2)
        self.assertEqual(loaded.native[0].value, json.dumps("adaptive"))
        self.assertIsNotNone(loaded.params)
        self.assertEqual(loaded.params.reasoning_effort, "xhigh")
        self.assertEqual(len(loaded.system), 1)
        self.assertEqual(len(loaded.system[0].native), 1)
        self.assertEqual(len(loaded.messages[0].content[0].native), 1)

    def test_validate_native_request(self) -> None:
        # Duplicate native kind on Request
        duplicate = Request(
            model="model",
            messages=[Message(role="user", content=[TextBlock(text="hi")])],
            native=(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_THINKING_MODE,
                    value=json.dumps("adaptive"),
                ),
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_THINKING_MODE,
                    value=json.dumps("enabled"),
                ),
            ),
        )
        with self.assertRaises((CodecError, ValueError)):
            validate_native_request(duplicate)

        # Invalid JSON value
        bad_json = Request(
            model="model",
            messages=[Message(role="user", content=[TextBlock(text="hi")])],
            native=(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_THINKING_MODE,
                    value="{invalid json",
                ),
            ),
        )
        with self.assertRaises((CodecError, ValueError)):
            validate_native_request(bad_json)

        # Host violation: thinking_mode on ContentBlock
        host_violation = Request(
            model="model",
            messages=[
                Message(
                    role="user",
                    content=[
                        TextBlock(
                            text="hi",
                            native=(
                                NativeAnnotation(
                                    source=NATIVE_SOURCE_ANTHROPIC,
                                    kind=NATIVE_KIND_THINKING_MODE,
                                    value=json.dumps("adaptive"),
                                ),
                            ),
                        )
                    ],
                )
            ],
        )
        with self.assertRaises((CodecError, ValueError)):
            validate_native_request(host_violation)

    def test_native_thinking_combinations(self) -> None:
        # 1. Adaptive thinking
        wire = {
            "model": "claude-3-7-sonnet-20250219",
            "max_tokens": 4096,
            "messages": [{"role": "user", "content": "Hi"}],
            "thinking": {"type": "adaptive"},
        }
        req, losses = an_decode_request(wire)
        self.assertEqual(len(losses), 0)
        self.assertIsNone(req.params.reasoning_effort)
        self.assertEqual(len(req.native), 1)
        self.assertEqual(req.native[0].kind, NATIVE_KIND_THINKING_MODE)

        # Encode back to Anthropic: thinking preserved without loss
        an_wire, an_losses = an_encode_request(req)
        self.assertEqual(len(an_losses), 0)
        self.assertEqual(an_wire.get("thinking"), {"type": "adaptive"})

        # Encode to ChatCompletions: type unmapped-field reported
        cc_wire, cc_losses = cc_encode_request(req)
        type_losses = [l for l in cc_losses if l.field == "type"]
        self.assertEqual(len(type_losses), 1)
        self.assertEqual(type_losses[0].reason, LOSS_UNMAPPED_FIELD)

        # 2. Enabled thinking with exact budget 6000
        wire_budget = {
            "model": "claude-3-7-sonnet-20250219",
            "max_tokens": 8000,
            "messages": [{"role": "user", "content": "Solve math"}],
            "thinking": {"type": "enabled", "budget_tokens": 6000},
        }
        req_b, losses_b = an_decode_request(wire_budget)
        self.assertEqual(len(losses_b), 0)
        self.assertIsNotNone(req_b.params)
        self.assertEqual(req_b.params.reasoning_effort, "medium")

        # Encode back to Anthropic: exact 6000 restored!
        an_wire_b, an_losses_b = an_encode_request(req_b)
        self.assertEqual(len(an_losses_b), 0)
        self.assertEqual(an_wire_b.get("thinking"), {"type": "enabled", "budget_tokens": 6000})

        # Encode to Responses: effort is medium, budget degraded loss reported
        resp_wire_b, resp_losses_b = resp_encode_request(req_b)
        self.assertEqual(resp_wire_b.get("reasoning"), {"effort": "medium"})
        budget_losses = [l for l in resp_losses_b if l.field == "budget_tokens"]
        self.assertEqual(len(budget_losses), 1)
        self.assertEqual(budget_losses[0].reason, LOSS_DEGRADED)

    def test_native_edited_effort_wins(self) -> None:
        wire = {
            "model": "claude-3-7-sonnet-20250219",
            "max_tokens": 8000,
            "messages": [{"role": "user", "content": "Solve math"}],
            "thinking": {"type": "enabled", "budget_tokens": 6000},
        }
        req, _ = an_decode_request(wire)
        self.assertEqual(req.params.reasoning_effort, "medium")

        # Edit effort to low
        new_params = dataclasses.replace(req.params, reasoning_effort="low")
        req_edited = dataclasses.replace(req, params=new_params)

        # Output to Anthropic: new effort wins, outputs budget 2048 with degraded loss
        an_wire, an_losses = an_encode_request(req_edited)
        self.assertEqual(an_wire.get("thinking"), {"type": "enabled", "budget_tokens": 2048})
        degraded = [l for l in an_losses if l.field == "reasoning_effort"]
        self.assertEqual(len(degraded), 1)
        self.assertEqual(degraded[0].reason, LOSS_DEGRADED)

    def test_native_unknown_enum_drops_unit(self) -> None:
        wire = {
            "model": "claude-3-7-sonnet-20250219",
            "max_tokens": 1000,
            "messages": [
                {
                    "role": "user",
                    "content": [
                        {
                            "type": "text",
                            "text": "Hello",
                            "cache_control": {"type": "unknown_future_cache"},
                        }
                    ],
                }
            ],
        }
        req, losses = an_decode_request(wire)
        unmapped = [l for l in losses if l.field == "type" and l.reason == LOSS_UNMAPPED_VALUE]
        self.assertEqual(len(unmapped), 1)
        # Block does NOT receive native cache_control
        self.assertEqual(len(req.messages[0].content[0].native), 0)

    def test_native_request_only(self) -> None:
        # A block carrying native annotation must not be allowed in Response
        blk = TextBlock(
            text="resp",
            native=(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_CACHE_CONTROL,
                    value=json.dumps({"type": "ephemeral"}),
                ),
            ),
        )
        resp = Response(
            id="msg_123",
            model="model",
            content=[blk],
            stop_reason="end_turn",
            usage=Usage(input_tokens=10, output_tokens=10),
        )
        with self.assertRaises(CodecError):
            dump_response(resp)

    def test_native_omission_unknown_source(self) -> None:
        req = Request(
            model="model",
            messages=[Message(role="user", content=[TextBlock(text="hi")])],
            native=(
                NativeAnnotation(
                    source="unsupported_vendor",
                    kind="custom_feature",
                    value=json.dumps("value"),
                ),
            ),
        )
        losses = omitted_native_request_losses(req, "chatcompletions")
        self.assertEqual(len(losses), 1)
        self.assertEqual(losses[0].field, "source")
        self.assertEqual(losses[0].reason, LOSS_UNSUPPORTED_SEMANTIC)


if __name__ == "__main__":
    unittest.main()
