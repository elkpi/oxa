import assert from "node:assert/strict";
import test from "node:test";

import {
  findArchitectureViolations,
  forbiddenImports,
} from "./architecture-support.js";

test("rejects direct face-to-face imports", () => {
  assert.deepEqual(
    forbiddenImports(
      "src/openai/chatcompletions/bad.ts",
      'import "../../anthropic/messages/index.js";',
    ),
    [
      "src/openai/chatcompletions/bad.ts imports anthropic/messages from another protocol face",
    ],
  );
});

test("rejects SSE imports of IR or protocol faces", () => {
  assert.deepEqual(
    forbiddenImports(
      "src/sse/bad.ts",
      [
        'import type { Event } from "../ir/index.js";',
        'export { decodeRequest } from "../openai/responses/index.js";',
      ].join("\n"),
    ),
    [
      "src/sse/bad.ts imports ir from the opaque SSE adapter",
      "src/sse/bad.ts imports openai/responses from the opaque SSE adapter",
    ],
  );
});

test("the TypeScript source tree obeys spoke and SSE boundaries", async () => {
  assert.deepEqual(await findArchitectureViolations(), []);
});
