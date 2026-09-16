import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequest,
  decodeResponse,
  encodeRequest,
  encodeResponse,
} from "../src/openai/responses/index.js";
import { runNonstreamVectors } from "./nonstream-vector-runner.js";

test("runs every Responses non-stream vector", () => {
  assert.ok(
    runNonstreamVectors("responses", {
      decodeRequest,
      decodeResponse,
      encodeRequest,
      encodeResponse,
    }) > 0,
  );
});

test("decodes empty assistant content as one empty text block", () => {
  const decoded = decodeRequest({
    model: "gpt-5",
    input: [
      { type: "message", role: "user", content: "hello" },
      { type: "message", role: "assistant", content: [] },
    ],
  });

  assert.deepEqual(decoded.value.messages, [
    { role: "user", content: [{ type: "text", text: "hello" }] },
    { role: "assistant", content: [{ type: "text", text: "" }] },
  ]);
  assert.deepEqual(decoded.losses, []);
});

test("decodes empty user content as one empty text block", () => {
  const decoded = decodeRequest({
    model: "gpt-5",
    input: [{ type: "message", role: "user", content: [] }],
  });

  assert.deepEqual(decoded.value.messages, [
    { role: "user", content: [{ type: "text", text: "" }] },
  ]);
  assert.deepEqual(decoded.losses, []);
});
