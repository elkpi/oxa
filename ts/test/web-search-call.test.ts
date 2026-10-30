import assert from "node:assert/strict";
import test from "node:test";
import { decodeRequestJSON } from "../src/openai/responses/nonstream.js";

function request(action: unknown): string {
  return JSON.stringify({
    model: "m",
    input: [
      { type: "web_search_call", id: "ws", status: "completed", action },
      { role: "assistant", content: "answer" },
      { role: "user", content: "continue" },
    ],
  });
}

test("web_search_call validates official action fields", () => {
  for (const action of [
    null,
    {},
    { type: "future" },
    { type: "find_in_page", url: "https://example.com" },
    { type: "find_in_page", url: null, pattern: "q" },
    { type: "search", query: null },
    { type: "search", queries: null },
    { type: "search", sources: [{ type: "url" }] },
  ]) {
    assert.throws(() => decodeRequestJSON(request(action)));
  }
});

test("unknown top-level item fields survive in extra", () => {
  const raw = JSON.stringify({
    model: "m",
    input: [
      { role: "user", content: "hi" },
      {
        type: "web_search_call",
        id: "ws",
        status: "completed",
        action: { type: "search" },
        role: true,
      },
    ],
  });
  const result = decodeRequestJSON(raw);
  const block = result.value.messages[1]?.content[0];
  assert.equal(block?.type, "web_search_call");
  if (block?.type === "web_search_call") assert.equal(block.extra?.role, true);
  assert.deepEqual(
    result.losses.map((item) => [item.path, item.reason]),
    [["input[1].role", "unmapped-field"]],
  );
});

test("assistant-first web search history is accepted", () => {
  const result = decodeRequestJSON(request({ type: "search", query: "q" }));
  assert.equal(result.value.messages[0]?.role, "assistant");
  assert.deepEqual(result.losses, []);
});
