from oxa.openai.chatcompletions import StreamDecoder, decode_request

request, losses = decode_request(
    {
        "model": "gpt-4o-mini",
        "messages": [{"role": "user", "content": "hello"}],
    }
)
assert request.model == "gpt-4o-mini"
assert len(request.messages) == 1
assert losses == []

decoder = StreamDecoder()
events = []
base = {
    "id": "stream-1",
    "object": "chat.completion.chunk",
    "created": 0,
    "model": "gpt-4o-mini",
}
events.extend(
    decoder.feed(
        {
            **base,
            "choices": [
                {
                    "index": 0,
                    "delta": {"role": "assistant"},
                    "finish_reason": None,
                }
            ],
        }
    )
)
events.extend(
    decoder.feed(
        {
            **base,
            "choices": [
                {
                    "index": 0,
                    "delta": {
                        "tool_calls": [
                            {
                                "index": 0,
                                "id": "call-1",
                                "type": "function",
                                "function": {
                                    "name": "lookup",
                                    "arguments": '{"q"',
                                },
                            }
                        ]
                    },
                    "finish_reason": None,
                }
            ],
        }
    )
)
events.extend(
    decoder.feed(
        {
            **base,
            "choices": [
                {
                    "index": 0,
                    "delta": {
                        "tool_calls": [
                            {"index": 0, "function": {"arguments": ":1}"}}
                        ]
                    },
                    "finish_reason": None,
                }
            ],
        }
    )
)
events.extend(
    decoder.feed(
        {
            **base,
            "choices": [
                {
                    "index": 0,
                    "delta": {},
                    "finish_reason": "tool_calls",
                }
            ],
        }
    )
)
events.extend(decoder.flush())
assert events

from oxa.ir import Message as IrMessage, Request as IrRequest, TextBlock as IrTextBlock
from oxa.ir.native import (
    NATIVE_KIND_THINKING_MODE,
    NATIVE_SOURCE_ANTHROPIC,
    NativeAnnotation,
    validate_native_request,
)

ir_req = IrRequest(
    model="m",
    messages=[IrMessage(role="user", content=[IrTextBlock(text="hi")])],
    native=(
        NativeAnnotation(
            source=NATIVE_SOURCE_ANTHROPIC,
            kind=NATIVE_KIND_THINKING_MODE,
            value='"adaptive"',
        ),
    ),
)
validate_native_request(ir_req)

print("python consumer: OK")
