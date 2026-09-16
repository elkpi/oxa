import assert from "node:assert/strict";
import test from "node:test";

import { encodeRequest as encodeAnthropicRequest } from "../src/anthropic/messages/index.js";
import { OxaError } from "../src/error.js";
import {
  decodeRequest as decodeIrRequest,
  encodeRequest as encodeIrRequest,
  type Block,
  type Request,
} from "../src/ir/index.js";
import { jsonText, type JsonObject } from "../src/json/index.js";
import { encodeRequest as encodeChatRequest } from "../src/openai/chatcompletions/index.js";
import { encodeRequest as encodeResponsesRequest } from "../src/openai/responses/index.js";

const text = { type: "text", text: "hello" } as const;
const callA = {
  type: "tool_use",
  id: "call_a",
  name: "lookup",
  input: jsonText("{}"),
} as const;
const callB = {
  type: "tool_use",
  id: "call_b",
  name: "lookup",
  input: jsonText("{}"),
} as const;
const result = (toolUseId: string): Block => ({
  type: "tool_result",
  tool_use_id: toolUseId,
  content: [],
});

const invalidRequests: readonly {
  readonly name: string;
  readonly request: Request;
}[] = [
  { name: "empty messages", request: { model: "model", messages: [] } },
  {
    name: "an assistant first message",
    request: {
      model: "model",
      messages: [{ role: "assistant", content: [text] }],
    },
  },
  {
    name: "empty message content",
    request: {
      model: "model",
      messages: [{ role: "user", content: [] }],
    },
  },
  {
    name: "an orphan tool result",
    request: {
      model: "model",
      messages: [{ role: "user", content: [result("call_a")] }],
    },
  },
  {
    name: "tool calls without a following user message",
    request: {
      model: "model",
      messages: [
        { role: "user", content: [text] },
        { role: "assistant", content: [callA] },
      ],
    },
  },
  {
    name: "a mismatched tool result ID",
    request: {
      model: "model",
      messages: [
        { role: "user", content: [text] },
        { role: "assistant", content: [callA] },
        { role: "user", content: [result("call_b")] },
      ],
    },
  },
  {
    name: "tool results in reversed order",
    request: {
      model: "model",
      messages: [
        { role: "user", content: [text] },
        { role: "assistant", content: [callA, callB] },
        {
          role: "user",
          content: [result("call_b"), result("call_a")],
        },
      ],
    },
  },
  {
    name: "tool results split over two user messages",
    request: {
      model: "model",
      messages: [
        { role: "user", content: [text] },
        { role: "assistant", content: [callA, callB] },
        { role: "user", content: [result("call_a")] },
        { role: "user", content: [result("call_b")] },
      ],
    },
  },
];

const faceEncoders: readonly {
  readonly name: string;
  readonly encode: (request: Request) => unknown;
}[] = [
  { name: "Chat Completions", encode: encodeChatRequest },
  { name: "Responses", encode: encodeResponsesRequest },
  { name: "Anthropic Messages", encode: encodeAnthropicRequest },
];

for (const { name, request } of invalidRequests) {
  test(`IR request codec rejects ${name}`, () => {
    assertInvalidRequest(() => encodeIrRequest(request), "IR encode");
    assertInvalidRequest(
      () => decodeIrRequest(requestDocument(request)),
      "IR decode",
    );
  });

  for (const face of faceEncoders) {
    test(`${face.name} request encoder rejects ${name}`, () => {
      assertInvalidRequest(() => face.encode(request), face.name);
    });
  }
}

function assertInvalidRequest(run: () => unknown, context: string): void {
  assert.throws(run, (error: unknown) => {
    assert.ok(error instanceof OxaError, `${context}: expected OxaError`);
    assert.equal(error.code, "invalid-input", `${context}: error code`);
    assert.deepEqual(
      (error as OxaError & { readonly losses?: readonly unknown[] }).losses ??
        [],
      [],
      `${context}: structural errors must not carry losses`,
    );
    return true;
  });
}

function requestDocument(request: Request): JsonObject {
  return {
    specVersion: "0.1.0",
    model: request.model,
    messages: request.messages.map((message) => ({
      role: message.role,
      content: message.content.map(blockDocument),
    })),
  };
}

function blockDocument(block: Block): JsonObject {
  switch (block.type) {
    case "text":
      return { type: "text", text: block.text };
    case "image":
      return {
        type: "image",
        ...(block.media_type === undefined
          ? {}
          : { media_type: block.media_type }),
        ...(block.data === undefined ? {} : { data: block.data }),
        ...(block.url === undefined ? {} : { url: block.url }),
      };
    case "tool_use":
      return {
        type: "tool_use",
        id: block.id,
        name: block.name,
        input: block.input,
      };
    case "tool_result":
      return {
        type: "tool_result",
        tool_use_id: block.tool_use_id,
        content: block.content.map(blockDocument),
      };
  }
}
