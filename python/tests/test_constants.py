"""Unit tests for Python constants and typing exports."""

import oxa.ir.constants as ir_constants
from oxa.anthropic.messages.constants import (
    BLOCK_TYPE_IMAGE as AN_BLOCK_TYPE_IMAGE,
)
from oxa.anthropic.messages.constants import (
    ROLE_USER as AN_ROLE_USER,
)
from oxa.ir import (
    BLOCK_TYPE_IMAGE,
    BLOCK_TYPE_TEXT,
    BLOCK_TYPE_TOOL_RESULT,
    BLOCK_TYPE_TOOL_USE,
    DELTA_TYPE_INPUT_JSON_DELTA,
    DELTA_TYPE_TEXT_DELTA,
    EVENT_TYPE_CONTENT_BLOCK_DELTA,
    EVENT_TYPE_CONTENT_BLOCK_START,
    EVENT_TYPE_CONTENT_BLOCK_STOP,
    EVENT_TYPE_MESSAGE_DELTA,
    EVENT_TYPE_MESSAGE_DONE,
    EVENT_TYPE_MESSAGE_START,
    LOSS_DEGRADED,
    LOSS_UNMAPPED_FIELD,
    LOSS_UNMAPPED_VALUE,
    LOSS_UNSUPPORTED_SEMANTIC,
    ROLE_ASSISTANT,
    ROLE_USER,
    SPEC_VERSION,
    STOP_END_TURN,
    STOP_MAX_TOKENS,
    STOP_OTHER,
    STOP_REFUSAL,
    STOP_STOP_SEQUENCE,
    STOP_TOOL_USE,
    TOOL_CHOICE_ANY,
    TOOL_CHOICE_AUTO,
    TOOL_CHOICE_NONE,
    TOOL_CHOICE_TOOL,
    BlockType,
    DeltaType,
    EventType,
    LossReason,
    Role,
    StopReason,
    ToolChoiceMode,
)
from oxa.openai.chatcompletions.constants import (
    FINISH_REASON_STOP,
)
from oxa.openai.chatcompletions.constants import (
    ROLE_SYSTEM as CC_ROLE_SYSTEM,
)
from oxa.openai.responses.constants import (
    ITEM_TYPE_MESSAGE,
)
from oxa.openai.responses.constants import (
    ROLE_DEVELOPER as RESP_ROLE_DEVELOPER,
)


def test_ir_constants_values() -> None:
    assert SPEC_VERSION == "0.1.0"
    role: Role = ROLE_USER
    assert role == "user"
    assert ROLE_ASSISTANT == "assistant"
    b_type: BlockType = BLOCK_TYPE_TEXT
    assert b_type == "text"
    assert BLOCK_TYPE_IMAGE == "image"
    assert BLOCK_TYPE_TOOL_USE == "tool_use"
    assert BLOCK_TYPE_TOOL_RESULT == "tool_result"
    tc_mode: ToolChoiceMode = TOOL_CHOICE_AUTO
    assert tc_mode == "auto"
    assert TOOL_CHOICE_ANY == "any"
    assert TOOL_CHOICE_TOOL == "tool"
    assert TOOL_CHOICE_NONE == "none"
    stop_reason: StopReason = STOP_END_TURN
    assert stop_reason == "end_turn"
    assert STOP_MAX_TOKENS == "max_tokens"
    assert STOP_STOP_SEQUENCE == "stop_sequence"
    assert STOP_TOOL_USE == "tool_use"
    assert STOP_REFUSAL == "refusal"
    assert STOP_OTHER == "other"
    ev_type: EventType = EVENT_TYPE_MESSAGE_START
    assert ev_type == "message_start"
    assert EVENT_TYPE_CONTENT_BLOCK_START == "content_block_start"
    assert EVENT_TYPE_CONTENT_BLOCK_DELTA == "content_block_delta"
    assert EVENT_TYPE_CONTENT_BLOCK_STOP == "content_block_stop"
    assert EVENT_TYPE_MESSAGE_DELTA == "message_delta"
    assert EVENT_TYPE_MESSAGE_DONE == "message_done"
    d_type: DeltaType = DELTA_TYPE_TEXT_DELTA
    assert d_type == "text_delta"
    assert DELTA_TYPE_INPUT_JSON_DELTA == "input_json_delta"
    l_reason: LossReason = LOSS_UNMAPPED_FIELD
    assert l_reason == "unmapped-field"
    assert LOSS_UNMAPPED_VALUE == "unmapped-value"
    assert LOSS_UNSUPPORTED_SEMANTIC == "unsupported-semantic"
    assert LOSS_DEGRADED == "degraded"


def test_ir_constants_module_reexport() -> None:
    for name in ir_constants.__all__:
        assert hasattr(ir_constants, name)


def test_spoke_constants() -> None:
    assert CC_ROLE_SYSTEM == "system"
    assert FINISH_REASON_STOP == "stop"
    assert RESP_ROLE_DEVELOPER == "developer"
    assert ITEM_TYPE_MESSAGE == "message"
    assert AN_ROLE_USER == "user"
    assert AN_BLOCK_TYPE_IMAGE == "image"
