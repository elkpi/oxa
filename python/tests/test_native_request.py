"""Tests for v2.4.0 Anthropic request fidelity and native options across IR."""

import dataclasses
import json
import unittest

from oxa.anthropic.messages import (
    decode_request as an_decode_request,
    decode_response as an_decode_response,
    encode_request as an_encode_request,
)
from oxa.openai.chatcompletions import (
    decode_request as cc_decode_request,
    encode_request as cc_encode_request,
)
from oxa.openai.responses import (
    encode_request as r_encode_request,
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
    Tool,
    Usage,
    dump_block,
    dump_request,
    dump_response,
    load_request,
    fingerprint_losses,
    NATIVE_KIND_EAGER_INPUT_STREAMING,
    EAGER_INPUT_STREAMING_LOSS_DETAIL,
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

    def test_explicit_effort_only_encodes_to_output_config(self) -> None:
        wire = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "output_config": {"effort": "low"},
        }
        req, losses = an_decode_request(wire)
        self.assertEqual(len(losses), 0)
        self.assertEqual(req.params.reasoning_effort, "low")
        self.assertEqual(len(req.native), 1)
        self.assertEqual(req.native[0].projection.origin, "explicit")

        encoded, enc_losses = an_encode_request(req)
        self.assertEqual(len(enc_losses), 0)
        self.assertNotIn("thinking", encoded)
        self.assertEqual(encoded.get("output_config"), {"effort": "low"})

    def test_context_management_recursive_audit(self) -> None:
        wire = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "context_management": {
                "edits": [
                    {
                        "type": "clear_tool_uses_20250919",
                        "future_policy": True,
                        "trigger": {
                            "type": "input_tokens",
                            "value": 30000,
                            "future_nested": False,
                        },
                    }
                ]
            },
        }
        req, losses = an_decode_request(wire)
        self.assertTrue(any(l.field == "future_policy" and l.reason == LOSS_UNMAPPED_FIELD for l in losses))
        self.assertTrue(any(l.field == "future_nested" and l.reason == LOSS_UNMAPPED_FIELD for l in losses))

        encoded, enc_losses = an_encode_request(req)
        edits = encoded.get("context_management", {}).get("edits", [])
        self.assertEqual(len(edits), 1)
        self.assertNotIn("future_policy", edits[0])
        self.assertNotIn("future_nested", edits[0].get("trigger", {}))

    def test_response_native_isolation(self) -> None:
        wire = {
            "id": "r1",
            "model": "claude-opus-5-5",
            "stop_reason": "end_turn",
            "content": [{"type": "text", "text": "hi", "cache_control": {"type": "ephemeral"}}],
            "usage": {"input_tokens": 1, "output_tokens": 1},
        }
        resp, losses = an_decode_response(wire)
        self.assertTrue(any(l.field == "cache_control" and l.reason == LOSS_UNMAPPED_FIELD for l in losses))
        self.assertEqual(len(getattr(resp.content[0], "native", ())), 0)
        # Must dump without throwing CodecError
        dumped = dump_response(resp)
        self.assertEqual(dumped["id"], "r1")

    def test_custom_tool_result_nested_cache_control(self) -> None:
        wire = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "tools": [
                {
                    "name": "f",
                    "input_schema": {
                        "type": "object",
                        "properties": {"input": {"type": "string"}},
                        "required": ["input"],
                    },
                }
            ],
            "messages": [
                {"role": "user", "content": "hi"},
                {
                    "role": "assistant",
                    "content": [{"type": "tool_use", "id": "c1", "name": "f", "input": {"input": "run"}}],
                },
                {
                    "role": "user",
                    "content": [
                        {
                            "type": "tool_result",
                            "tool_use_id": "c1",
                            "content": [
                                {
                                    "type": "text",
                                    "text": "result",
                                    "cache_control": {"type": "ephemeral"},
                                }
                            ],
                        }
                    ],
                },
            ],
        }
        req, _ = an_decode_request(wire)
        ir_json = dump_request(req)
        reloaded = load_request(ir_json)
        encoded, _ = an_encode_request(reloaded)
        user_content = encoded["messages"][2]["content"]
        self.assertIsInstance(user_content, list)
        tool_result = user_content[0]
        self.assertEqual(tool_result["type"], "tool_result")
        inner_content = tool_result["content"]
        self.assertIsInstance(inner_content, list)
        self.assertEqual(inner_content[0].get("cache_control"), {"type": "ephemeral"})

    def test_cache_control_omission_loss_detail_cross_encoding(self) -> None:
        from oxa.ir import CACHE_CONTROL_LOSS_DETAIL

        wire = {
            "model": "claude-opus-5-5",
            "max_tokens": 64000,
            "system": [{"type": "text", "text": "S", "cache_control": {"type": "ephemeral"}}],
            "tools": [{"name": "f", "description": "d", "input_schema": {"type": "object"}, "cache_control": {"type": "ephemeral"}}],
            "messages": [{"role": "user", "content": [{"type": "text", "text": "go", "cache_control": {"type": "ephemeral"}}]}],
        }
        req, dec_losses = an_decode_request(wire)
        self.assertEqual(len(dec_losses), 0)

        _, cc_losses = cc_encode_request(req)
        self.assertEqual(len(cc_losses), 3)
        for l in cc_losses:
            self.assertEqual(l.field, "cache_control")
            self.assertEqual(l.reason, LOSS_UNMAPPED_FIELD)
            self.assertEqual(l.detail, CACHE_CONTROL_LOSS_DETAIL)

        _, resp_losses = resp_encode_request(req)
        self.assertEqual(len(resp_losses), 3)
        for l in resp_losses:
            self.assertEqual(l.detail, CACHE_CONTROL_LOSS_DETAIL)

        # Directly constructed IR without omission
        req_no_om = Request(
            model="claude-opus-5-5",
            native=(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_CACHE_CONTROL,
                    value=json.dumps({"type": "ephemeral"}),
                ),
            ),
            messages=[Message(role="user", content=[TextBlock(text="hi")])],
        )
        _, cc_no_om_losses = cc_encode_request(req_no_om)
        self.assertEqual(len(cc_no_om_losses), 1)
        self.assertEqual(cc_no_om_losses[0].field, "cache_control")
        self.assertEqual(cc_no_om_losses[0].reason, LOSS_UNMAPPED_FIELD)
        self.assertEqual(cc_no_om_losses[0].detail, CACHE_CONTROL_LOSS_DETAIL)

        # Directly constructed IR with empty detail
        req_empty_detail = Request(
            model="claude-opus-5-5",
            native=(
                NativeAnnotation(
                    source=NATIVE_SOURCE_ANTHROPIC,
                    kind=NATIVE_KIND_CACHE_CONTROL,
                    value=json.dumps({"type": "ephemeral"}),
                    omission=NativeOmission(field="cache_control", reason=LOSS_UNMAPPED_FIELD, detail=""),
                ),
            ),
            messages=[Message(role="user", content=[TextBlock(text="hi")])],
        )
        _, cc_empty_losses = cc_encode_request(req_empty_detail)
        self.assertEqual(len(cc_empty_losses), 1)
        self.assertEqual(cc_empty_losses[0].detail, CACHE_CONTROL_LOSS_DETAIL)

    def test_defer_loading_tool_native_option(self) -> None:
        from oxa.ir import NATIVE_KIND_DEFER_LOADING

        # 1. defer_loading: true round-trip with zero decode losses
        wire = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [
                {
                    "name": "t1",
                    "description": "tool 1",
                    "input_schema": {"type": "object"},
                    "defer_loading": True,
                }
            ],
        }
        req, losses = an_decode_request(wire)
        self.assertEqual(len(losses), 0)
        self.assertEqual(len(req.tools), 1)
        self.assertEqual(len(req.tools[0].native), 1)
        ann = req.tools[0].native[0]
        self.assertEqual(ann.kind, NATIVE_KIND_DEFER_LOADING)
        self.assertEqual(ann.value, "true")
        self.assertIsNotNone(ann.omission)
        self.assertEqual(ann.omission.reason, LOSS_DEGRADED)

        encoded, el = an_encode_request(req)
        self.assertEqual(len(el), 0)
        self.assertEqual(len(encoded["tools"]), 1)
        self.assertEqual(encoded["tools"][0].get("defer_loading"), True)

        # 2. defer_loading: false produces no annotation
        wire_false = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [
                {
                    "name": "t1",
                    "input_schema": {"type": "object"},
                    "defer_loading": False,
                }
            ],
        }
        req_false, losses_false = an_decode_request(wire_false)
        self.assertEqual(len(losses_false), 0)
        self.assertEqual(len(req_false.tools[0].native), 0)

        # 3. conflict: defer_loading: true AND cache_control
        wire_conflict = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [
                {
                    "name": "t1",
                    "input_schema": {"type": "object"},
                    "defer_loading": True,
                    "cache_control": {"type": "ephemeral"},
                }
            ],
        }
        with self.assertRaises(ValueError) as cm:
            an_decode_request(wire_conflict)
        self.assertIn("cannot specify both defer_loading and cache_control", str(cm.exception))

        # 4. non-boolean defer_loading
        wire_non_bool = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [
                {
                    "name": "t1",
                    "input_schema": {"type": "object"},
                    "defer_loading": "true",
                }
            ],
        }
        with self.assertRaises(ValueError) as cm:
            an_decode_request(wire_non_bool)
        self.assertIn("tools[0].defer_loading must be a boolean", str(cm.exception))

        # 5. whitespace boolean annotation round-trips
        req_whitespace = Request(
            model="claude-opus-5-5",
            params=Params(max_tokens=16000),
            messages=[Message(role="user", content=[TextBlock(text="hi")])],
            tools=[
                Tool(
                    name="t1",
                    input_schema={"type": "object"},
                    native=(NativeAnnotation(source="anthropic", kind=NATIVE_KIND_DEFER_LOADING, value=" true \n"),),
                )
            ],
        )
        validate_native_request(req_whitespace)
        enc_whitespace, el_whitespace = an_encode_request(req_whitespace)
        self.assertEqual(len(el_whitespace), 0)
        self.assertEqual(enc_whitespace["tools"][0].get("defer_loading"), True)

        # 6. direct IR conflict: Tool with both cache_control and defer_loading
        req_direct_conflict = Request(
            model="claude-opus-5-5",
            params=Params(max_tokens=16000),
            messages=[Message(role="user", content=[TextBlock(text="hi")])],
            tools=[
                Tool(
                    name="t1",
                    input_schema={"type": "object"},
                    native=(
                        NativeAnnotation(source="anthropic", kind=NATIVE_KIND_DEFER_LOADING, value="true"),
                        NativeAnnotation(source="anthropic", kind="cache_control", value=json.dumps({"type": "ephemeral"})),
                    ),
                )
            ],
        )
        with self.assertRaises(Exception) as cm:
            validate_native_request(req_direct_conflict)
        self.assertIn("cannot specify both defer_loading and cache_control", str(cm.exception))
        with self.assertRaises(Exception) as cm:
            an_encode_request(req_direct_conflict)
        self.assertIn("cannot specify both defer_loading and cache_control", str(cm.exception))

        # 7. native tool declaration with non-boolean defer_loading is preserved opaquely
        wire_native = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [
                {
                    "type": "future_20990101",
                    "name": "future",
                    "defer_loading": {"future": True},
                }
            ],
        }
        req_native, losses_native = an_decode_request(wire_native)
        self.assertEqual(len(losses_native), 0)
        self.assertEqual(len(req_native.tools), 1)
        self.assertEqual(req_native.tools[0].type, "native")

        # 8. cache_control: null + defer_loading: true returns conflict error
        wire_null_cache = {
            "model": "claude-opus-5-5",
            "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [
                {
                    "name": "t1",
                    "input_schema": {"type": "object"},
                    "defer_loading": True,
                    "cache_control": None,
                }
            ],
        }
        with self.assertRaises(ValueError) as cm:
            an_decode_request(wire_null_cache)
        self.assertIn("cannot specify both defer_loading and cache_control", str(cm.exception))

    def test_fingerprint_losses(self) -> None:
        losses = [
            Loss(path="tools[0]", field="eager_input_streaming", reason=LOSS_DEGRADED, detail="sensitive1"),
            Loss(path="native[1]", field="store", reason=LOSS_UNSUPPORTED_SEMANTIC, detail="sensitive2"),
            Loss(path="stream_options", field="extra", reason=LOSS_UNMAPPED_FIELD, detail="sensitive3"),
        ]
        fps_all = fingerprint_losses(losses, 0)
        self.assertEqual(len(fps_all), 3)
        self.assertEqual(fps_all[0].path, "tools[0]")
        self.assertEqual(fps_all[0].field, "eager_input_streaming")
        self.assertEqual(fps_all[0].reason, LOSS_DEGRADED)
        self.assertFalse(hasattr(fps_all[0], "detail"))

        fps_trunc = fingerprint_losses(losses, 2)
        self.assertEqual(len(fps_trunc), 2)
        self.assertEqual(fps_trunc[1].field, "store")

    def test_eager_input_streaming_32_tools_and_validation(self) -> None:
        # 1. 32 tools carrying eager_input_streaming: true produce 0 decode losses
        tools = [
            {"name": f"tool_{i}", "input_schema": {"type": "object"}, "eager_input_streaming": True}
            for i in range(32)
        ]
        wire32 = {
            "model": "claude-opus-5-5",
            "max_tokens": 1024,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": tools,
        }
        req32, losses32 = an_decode_request(wire32)
        self.assertEqual(len(losses32), 0)
        self.assertEqual(len(req32.tools or ()), 32)
        self.assertEqual(req32.tools[0].native[0].kind, NATIVE_KIND_EAGER_INPUT_STREAMING)

        # 2. Non-boolean fails
        for bad in ["true", 1, {"a": "b"}]:
            wire_bad = {
                "model": "claude-opus-5-5",
                "max_tokens": 1024,
                "messages": [{"role": "user", "content": "hi"}],
                "tools": [{"name": "t1", "input_schema": {"type": "object"}, "eager_input_streaming": bad}],
            }
            with self.assertRaises(ValueError) as cm:
                an_decode_request(wire_bad)
            self.assertIn("tools[0].eager_input_streaming must be a boolean", str(cm.exception))

        # 3. Round-trip
        encoded, _ = an_encode_request(req32)
        self.assertEqual(encoded["tools"][0]["eager_input_streaming"], True)

    def test_chatcompletions_controls_conflict_and_audit(self) -> None:
        wire_conflict = {
            "model": "gpt-5",
            "messages": [{"role": "user", "content": "hi"}],
            "max_tokens": 100,
            "max_completion_tokens": 200,
        }
        with self.assertRaises(ValueError) as cm:
            cc_decode_request(wire_conflict)
        self.assertIn("cannot specify both max_tokens and max_completion_tokens", str(cm.exception))

        wire_unknown_so = {
            "model": "gpt-5",
            "messages": [{"role": "user", "content": "hi"}],
            "stream_options": {"include_usage": True, "extra_prop": "val"},
        }
        req_unknown, losses = cc_decode_request(wire_unknown_so)
        self.assertTrue(any(l.path == "stream_options.extra_prop" and l.reason == LOSS_UNMAPPED_FIELD for l in losses))
        so_ann = next((n for n in (req_unknown.native or ()) if n.kind == "stream_options"), None)
        self.assertIsNotNone(so_ann)
        self.assertNotIn("extra_prop", so_ann.value)

        # max_completion_tokens <= 0
        for bad in [0, -1]:
            with self.assertRaises(ValueError) as cm:
                cc_decode_request({"model": "gpt-5", "messages": [{"role": "user", "content": "hi"}], "max_completion_tokens": bad})
            self.assertIn("max_completion_tokens must be a positive integer", str(cm.exception))

        # null values
        wire_nulls = {"model": "gpt-5", "messages": [{"role": "user", "content": "hi"}], "store": None, "stream": None, "stream_options": None}
        req_nulls, ls_nulls = cc_decode_request(wire_nulls)
        self.assertEqual(len(ls_nulls), 0)
        enc_nulls, _ = cc_encode_request(req_nulls)
        self.assertIsNone(enc_nulls.get("store"))
        self.assertIn("store", enc_nulls)
        self.assertIsNone(enc_nulls.get("stream"))
        self.assertIn("stream", enc_nulls)
        self.assertIsNone(enc_nulls.get("stream_options"))
        self.assertIn("stream_options", enc_nulls)

        # store: false to Responses
        wire_sf = {"model": "gpt-5", "messages": [{"role": "user", "content": "hi"}], "store": False}
        req_sf, _ = cc_decode_request(wire_sf)
        enc_resp, resp_ls = r_encode_request(req_sf)
        self.assertEqual(enc_resp.get("store"), False)
        self.assertEqual(len(resp_ls), 0)

        # Foreign openai native option does not leak into Anthropic
        from oxa.ir import NativeAnnotation
        req_foreign = Request(
            model="claude-opus-5-5",
            params=Params(max_tokens=100),
            messages=[Message(role="user", content=[TextBlock(text="hi")])],
            native=(
                NativeAnnotation(source="openai", kind="thinking_mode", value='"disabled"'),
                NativeAnnotation(source="openai", kind="context_management", value='{"edits":[{"type":"clear_tool_uses_20250919"}]}'),
            ),
        )
        enc_foreign, for_ls = an_encode_request(req_foreign)
        self.assertNotIn("thinking", enc_foreign)
        self.assertNotIn("context_management", enc_foreign)
        self.assertEqual(len(for_ls), 2)


if __name__ == "__main__":
    unittest.main()
