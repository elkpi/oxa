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

test("rejects package self-reference imports between faces", () => {
  assert.deepEqual(
    forbiddenImports(
      "src/openai/chatcompletions/bad.ts",
      'import { decodeRequest } from "@elkpi/oxa/anthropic/messages";',
    ),
    [
      "src/openai/chatcompletions/bad.ts imports anthropic/messages from another protocol face",
    ],
  );
});

test("ignores import-looking comments and string literals", () => {
  assert.deepEqual(
    forbiddenImports(
      "src/openai/chatcompletions/good.ts",
      '// import "@elkpi/oxa/anthropic/messages";\nconst note = "import ../../anthropic/messages/index.js";',
    ),
    [],
  );
});

test("rejects dynamic imports across architecture boundaries", () => {
  assert.deepEqual(
    forbiddenImports(
      "src/openai/chatcompletions/bad.ts",
      'const messages = import("@elkpi/oxa/anthropic/messages");',
    ),
    [
      "src/openai/chatcompletions/bad.ts imports anthropic/messages from another protocol face",
    ],
  );
  assert.deepEqual(
    forbiddenImports("src/sse/bad.ts", 'const ir = import("../ir/index.js");'),
    ["src/sse/bad.ts imports ir from the opaque SSE adapter"],
  );
});

test("rejects package self-references in import types", () => {
  assert.deepEqual(
    forbiddenImports(
      "src/openai/responses/bad.ts",
      'type MessagesRequest = import("@elkpi/oxa/anthropic/messages").Request;',
    ),
    [
      "src/openai/responses/bad.ts imports anthropic/messages from another protocol face",
    ],
  );
  assert.deepEqual(
    forbiddenImports(
      "src\\sse\\bad.ts",
      'type ChatEvent = import("@elkpi/oxa/openai/chatcompletions").Event;',
    ),
    [
      "src\\sse\\bad.ts imports openai/chatcompletions from the opaque SSE adapter",
    ],
  );
});

test("classifies simulated Windows source paths", () => {
  assert.deepEqual(
    forbiddenImports(
      "src\\openai\\chatcompletions\\bad.ts",
      'import "../../anthropic/messages/index.js";',
    ),
    [
      "src\\openai\\chatcompletions\\bad.ts imports anthropic/messages from another protocol face",
    ],
  );
  assert.deepEqual(
    forbiddenImports("src\\sse\\bad.ts", 'import "../ir/index.js";'),
    ["src\\sse\\bad.ts imports ir from the opaque SSE adapter"],
  );
});
