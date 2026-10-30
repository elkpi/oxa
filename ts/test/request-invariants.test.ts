import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequest as decodeAnthropicRequest,
  encodeRequest as encodeAnthropicRequest,
} from "../src/anthropic/messages/index.js";
import { OxaError } from "../src/error.js";
import {
  decodeRequest as decodeIrRequest,
  encodeRequest as encodeIrRequest,
  type Block,
  type Request,
} from "../src/ir/index.js";
import { integer, jsonText, type JsonObject } from "../src/json/index.js";
import {
  decodeRequest as decodeChatRequest,
  encodeRequest as encodeChatRequest,
} from "../src/openai/chatcompletions/index.js";
import {
  decodeRequest as decodeResponsesRequest,
  encodeRequest as encodeResponsesRequest,
  decodeResponse as decodeResponsesResponse,
  ResponsesStreamDecoder,
} from "../src/openai/responses/index.js";

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
  {
    name: "a nested orphan tool result",
    request: {
      model: "model",
      messages: [
        { role: "user", content: [text] },
        { role: "assistant", content: [callA] },
        {
          role: "user",
          content: [
            {
              type: "tool_result",
              tool_use_id: "call_a",
              content: [result("nested_call")],
            },
          ],
        },
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

test("face decoders reject invalid source request conversations", () => {
  assertInvalidRequest(
    () =>
      decodeChatRequest({
        model: "model",
        messages: [{ role: "assistant", content: "hello" }],
      }),
    "Chat Completions decode",
  );
  assertInvalidRequest(
    () =>
      decodeResponsesRequest({
        model: "model",
        input: [{ role: "assistant", content: "hello" }],
      }),
    "Responses decode",
  );
  assertInvalidRequest(
    () =>
      decodeAnthropicRequest({
        model: "model",
        max_tokens: integer(1n),
        messages: [{ role: "assistant", content: "hello" }],
      }),
    "Anthropic decode",
  );
});

test("Responses apply_patch invalid request association", () => {
  assert.throws(
    () =>
      decodeResponsesRequest({
        model: "m",
        input: [
          { role: "user", content: "hi" },
          {
            type: "apply_patch_call",
            call_id: "same",
            status: "completed",
            operation: { type: "future" },
          },
          {
            type: "apply_patch_call",
            call_id: "same",
            status: "completed",
            operation: { type: "future" },
          },
          {
            type: "apply_patch_call_output",
            call_id: "same",
            status: "completed",
          },
          {
            type: "apply_patch_call_output",
            call_id: "same",
            status: "completed",
          },
        ],
      }),
    /INV-3/,
  );
  assert.throws(
    () =>
      decodeResponsesRequest({
        model: "m",
        input: [
          { role: "user", content: "hi" },
          {
            type: "apply_patch_call",
            call_id: "a",
            status: "completed",
            operation: { type: "future" },
          },
          {
            type: "apply_patch_call_output",
            call_id: "b",
            status: "future",
          },
        ],
      }),
    /INV-3/,
  );
  assert.throws(
    () =>
      decodeResponsesRequest({
        model: "m",
        input: [
          { role: "user", content: "hi" },
          {
            type: "apply_patch_call_output",
            call_id: "c",
            status: "completed",
          },
        ],
      }),
    /INV-3/,
  );
});

test("Responses apply_patch response validation rejects missing fields", () => {
  const badItems = [
    {
      id: "apc",
      type: "apply_patch_call",
      call_id: "c",
      status: "completed",
      operation: { type: "delete_file" }, // missing path
    },
    {
      id: "apc",
      type: "apply_patch_call",
      call_id: "c",
      status: "completed",
      operation: { type: "delete_file", path: null }, // null path
    },
    {
      id: "apc",
      type: "apply_patch_call",
      call_id: "c",
      status: "completed",
      operation: { type: "create_file", path: "a" }, // missing diff
    },
    {
      id: "apc",
      type: "apply_patch_call",
      status: "completed",
      operation: { type: "delete_file", path: "a" }, // missing call_id
    },
    {
      type: "apply_patch_call",
      call_id: "c",
      status: "completed",
      operation: { type: "delete_file", path: "a" }, // missing item id
    },
    {
      id: "apc",
      type: "apply_patch_call",
      call_id: "c",
      status: "completed", // missing operation
    },
    {
      id: "apc",
      type: "apply_patch_call",
      call_id: "c",
      operation: { type: "delete_file", path: "a" }, // missing status
    },
  ];
  for (const item of badItems) {
    assert.throws(
      () =>
        decodeResponsesResponse({
          id: "r",
          model: "m",
          status: "completed",
          output: [item as any],
        }),
      OxaError,
    );
  }
});

test("Responses stream decoder skips unknown apply_patch op and status with loss", () => {
  for (const scenario of [
    { op: { type: "future", extra: true }, status: "completed" },
    { op: { type: "delete_file", path: "a" }, status: "future" },
    { op: { type: "delete_file", path: "a", diff: 42 }, status: "completed" },
  ]) {
    const d = new ResponsesStreamDecoder();
    d.Feed({
      type: "response.created",
      response: {
        id: "r",
        object: "response",
        model: "m",
        status: "in_progress",
        output: [],
      },
    });
    d.Feed({
      type: "response.output_item.added",
      output_index: 0,
      item: {
        id: "apc",
        type: "apply_patch_call",
        call_id: "c",
        status: "in_progress",
        operation: scenario.op as any,
      },
    });
    const doneEvents = d.Feed({
      type: "response.output_item.done",
      output_index: 0,
      item: {
        id: "apc",
        type: "apply_patch_call",
        call_id: "c",
        status: scenario.status as any,
        operation: scenario.op as any,
      },
    });
    if (scenario.status === "future" || scenario.op.type === "future") {
      assert.equal(doneEvents.length, 0);
    }
    d.Feed({
      type: "response.completed",
      response: {
        id: "r",
        object: "response",
        model: "m",
        status: "completed",
        output: [
          {
            id: "apc",
            type: "apply_patch_call",
            call_id: "c",
            status: scenario.status as any,
            operation: scenario.op as any,
          },
        ],
      },
    });
    d.Flush();
    assert.ok(d.Losses().length >= 1);
  }
});

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
    case "web_search_call":
      return {
        type: block.type,
        id: block.id,
        status: block.status,
        action: block.action,
        ...(block.extra === undefined ? {} : { extra: block.extra }),
      };
    case "text":
      return { type: "text", text: block.text };
    case "thinking":
      return {
        type: "thinking",
        thinking: block.thinking,
        ...(block.signature === undefined
          ? {}
          : { signature: block.signature }),
      };
    case "image":
      return {
        type: "image",
        ...(block.media_type === undefined
          ? {}
          : { media_type: block.media_type }),
        ...(block.data === undefined ? {} : { data: block.data }),
        ...(block.url === undefined ? {} : { url: block.url }),
      };
    case "document":
      return {
        type: "document",
        ...(block.media_type === undefined
          ? {}
          : { media_type: block.media_type }),
        ...(block.data === undefined ? {} : { data: block.data }),
        ...(block.url === undefined ? {} : { url: block.url }),
        ...(block.file_id === undefined ? {} : { file_id: block.file_id }),
      };
    case "custom_tool_use":
      return {
        type: block.type,
        id: block.id,
        name: block.name,
        input: block.input,
      };
    case "custom_tool_result":
      return {
        type: block.type,
        tool_use_id: block.tool_use_id,
        output:
          typeof block.output === "string"
            ? block.output
            : block.output.map(blockDocument),
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
    case "apply_patch_tool_use":
      return {
        type: block.type,
        id: block.id,
        status: block.status,
        operation: { ...block.operation },
      };
    case "apply_patch_tool_result":
      return {
        type: block.type,
        tool_use_id: block.tool_use_id,
        status: block.status,
        ...(block.output !== undefined ? { output: block.output } : {}),
      };
    case "redacted_thinking":
      return {
        type: "redacted_thinking",
        data: block.data,
      };
  }
}
