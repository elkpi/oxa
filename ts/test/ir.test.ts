import assert from "node:assert/strict";
import test from "node:test";

import { OxaError } from "../src/error.js";
import { assertEventSequence, type Event } from "../src/ir/index.js";

test("rejects a delta before its block start", () => {
  const events: readonly Event[] = [
    { type: "message_start", id: "m", model: "model" },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "x" },
    },
  ];

  assert.throws(
    () => assertEventSequence(events),
    (error: unknown) =>
      error instanceof OxaError && error.code === "ir-invariant",
  );
});

test("accepts contiguous text block events", () => {
  const events: readonly Event[] = [
    { type: "message_start", id: "m", model: "model" },
    {
      type: "content_block_start",
      index: 0,
      block: { type: "text", text: "" },
    },
    {
      type: "content_block_delta",
      index: 0,
      delta: { type: "text_delta", text: "x" },
    },
    { type: "content_block_stop", index: 0 },
    {
      type: "message_delta",
      stop_reason: "end_turn",
      usage: { input_tokens: 0n, output_tokens: 0n },
    },
    { type: "message_done" },
  ];

  assert.doesNotThrow(() => assertEventSequence(events));
});
