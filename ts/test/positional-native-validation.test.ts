import assert from "node:assert/strict";
import test from "node:test";
import {
  validateNativeRequest,
  type Request,
  type NativeAnnotation,
} from "../src/ir/index.js";

const cache: NativeAnnotation = {
  source: "anthropic",
  kind: "cache_control",
  value: '{"type":"ephemeral"}',
};
const defer: NativeAnnotation = {
  source: "anthropic",
  kind: "defer_loading",
  value: "true",
};
const invalid: NativeAnnotation = {
  source: "anthropic",
  kind: "cache_control",
  value: '{"type":"secret-value"}',
};
const future: NativeAnnotation = {
  source: "future",
  kind: "future",
  value: "{}",
};

function request(
  native: readonly NativeAnnotation[],
  compaction: boolean,
): Request {
  const change = {
    op: "add" as const,
    ref: { type: "tool_reference" as const, name: "read" },
    native,
  };
  return {
    model: "m",
    messages: [
      {
        role: "user",
        content: compaction
          ? [{ type: "compaction", tool_changes: [change] }]
          : [{ type: "text", text: "go" }],
      },
    ],
    tool_change_groups: compaction
      ? []
      : [
          {
            message_index: 1,
            content_index: -1,
            parts: [{ type: "tool_change", change }],
          },
        ],
  };
}

for (const compaction of [false, true]) {
  const base = compaction
    ? "messages[0].content[0].tool_changes[0]"
    : "tool_change_groups[0].parts[0].change";
  test(`change native conflict at ${base}`, () => {
    assert.throws(
      () => validateNativeRequest(request([cache, defer], compaction)),
      (error: unknown) =>
        error instanceof Error && error.message.includes(base),
    );
    assert.doesNotThrow(() =>
      validateNativeRequest(request([cache], compaction)),
    );
  });
  test(`change native identifies invalid index at ${base}`, () => {
    assert.throws(
      () => validateNativeRequest(request([future, invalid], compaction)),
      (error: unknown) =>
        error instanceof Error &&
        error.message.includes(`${base}.native[1]`) &&
        !error.message.includes("secret-value"),
    );
  });
}
