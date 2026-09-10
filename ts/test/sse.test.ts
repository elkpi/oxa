import assert from "node:assert/strict";
import test from "node:test";

import { sse } from "../src/index.js";
import { SseDecoder, encodeSse } from "../src/sse/index.js";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

test("frames UTF-8 data split inside a code point", () => {
  const bytes = encoder.encode("data: 你\n\n");
  const stream = new SseDecoder();
  assert.deepEqual(stream.feed(bytes.slice(0, 7)), []);
  assert.deepEqual(stream.feed(bytes.slice(7)), [{ data: "你" }]);
});

test("keeps DONE opaque while supporting CRLF and multiline data", () => {
  const stream = new SseDecoder();
  assert.deepEqual(
    stream.feed(
      encoder.encode(
        "event: delta\r\ndata: first\r\ndata: second\r\n\r\ndata: [DONE]\n\n",
      ),
    ),
    [{ event: "delta", data: "first\nsecond" }, { data: "[DONE]" }],
  );
});

test("encodes opaque frames", () => {
  assert.equal(
    decoder.decode(encodeSse({ event: "delta", data: "a\nb" })),
    "event: delta\ndata: a\ndata: b\n\n",
  );
});

test("flushes a trailing frame and exposes SSE at the package root", () => {
  const stream = new sse.SseDecoder();
  assert.deepEqual(stream.feed(encoder.encode("data: tail")), []);
  assert.deepEqual(stream.flush(), [{ data: "tail" }]);
});
