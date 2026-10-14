import assert from "node:assert/strict";
import test from "node:test";
import * as chat from "../src/openai/chatcompletions/index.js";
import * as responses from "../src/openai/responses/index.js";
import * as anthropic from "../src/anthropic/messages/index.js";

const requests = [
  [
    chat.decodeRequestJSON,
    '{"model":"m","messages":[{"role":"user","content":"hi"}]',
  ],
  [responses.decodeRequestJSON, '{"model":"m","input":"hi"'],
  [
    anthropic.decodeRequestJSON,
    '{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"hi"}]',
  ],
] as const;

test("raw request entrypoints audit false, null and other opaque extension values", () => {
  for (const [decode, prefix] of requests) {
    for (const value of [
      "false",
      "null",
      "0",
      '""',
      "[]",
      "{}",
      '{"nested":true}',
    ]) {
      const result = decode(`${prefix},"unknown_extension":${value}}`);
      assert.deepEqual(
        result.losses.map(({ path, field, reason }) => ({
          path,
          field,
          reason,
        })),
        [
          {
            path: "unknown_extension",
            field: "unknown_extension",
            reason: "unmapped-field",
          },
        ],
      );
    }
  }
});

test("raw request losses preserve nested source order and numeric property names", () => {
  const result = responses.decodeRequestJSON(
    '{"model":"m","input":"hi","reasoning":{"effort":"low","nested_extension":null},"store":false,"2":null,"1":null}',
  );
  assert.deepEqual(
    result.losses.map((loss) => loss.path),
    ["reasoning.nested_extension", "store", "2", "1"],
  );
});

test("raw unknown prototype property names are ordinary properties", () => {
  for (const [decode, prefix] of requests) {
    const result = decode(
      `${prefix},"__proto__":null,"constructor":false,"toString":0}`,
    );
    assert.deepEqual(
      result.losses.map((loss) => loss.path),
      ["__proto__", "constructor", "toString"],
    );
  }
});

test("raw stream indexes are decoded without changing opaque tool input", () => {
  const decoder = new anthropic.AnthropicStreamDecoder();
  decoder.feedJSON(
    '{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}',
  );
  const input = '{ "z": 9007199254740993, "a": 1e+01 }';
  decoder.feedJSON(
    '{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"f","input":' +
      input +
      '},"unknown_extension":null}',
  );
  const events = decoder.feedJSON('{"type":"content_block_stop","index":0}');
  assert.equal(events[0]?.type, "content_block_start");
  const start = events[0];
  assert.ok(
    start?.type === "content_block_start" && start.block.type === "tool_use",
  );
  assert.equal(start.block.input, input);
  decoder.feedJSON(
    '{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":1,"output_tokens":1}}',
  );
  assert.equal(decoder.feedJSON('{"type":"message_stop"}').length, 2);
  assert.equal(decoder.Flush().length, 0);
  assert.equal(decoder.Losses().length, 1);
});

test("an unknown native event remains a single whole-event loss", () => {
  const decoder = new anthropic.AnthropicStreamDecoder();
  assert.deepEqual(
    decoder.feedJSON(
      '{"type":"error","error":{"type":"overloaded_error"},"extra":null}',
    ),
    [],
  );
  assert.equal(decoder.Losses().length, 1);
  assert.equal(decoder.Losses()[0]?.reason, "unsupported-semantic");
});

test("raw response entrypoints report unknown fields", () => {
  const fixtures = [
    [
      chat.decodeResponseJSON,
      '{"id":"r1","model":"m","object":"chat.completion","created":0,"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}',
    ],
    [
      responses.decodeResponseJSON,
      '{"id":"r1","model":"m","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":1,"output_tokens":1}',
    ],
    [
      anthropic.decodeResponseJSON,
      '{"id":"r1","model":"m","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}',
    ],
  ] as const;
  for (const [decode, prefix] of fixtures) {
    const result = decode(`${prefix},"unknown_extension":null}`);
    assert.equal(result.losses.length, 1);
    assert.equal(result.losses[0]?.path, "unknown_extension");
    assert.throws(() => decode("{"));
    assert.throws(() => decode("[]"));
  }
});

test("raw Responses stream indexes maintain the native lifecycle", () => {
  const decoder = new responses.ResponsesStreamDecoder();
  decoder.feedJSON(
    '{"type":"response.created","response":{"id":"r1","object":"response","status":"in_progress","model":"m","output":[]}}',
  );
  decoder.feedJSON(
    '{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg1","role":"assistant","content":[]}}',
  );
  decoder.feedJSON(
    '{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg1","role":"assistant","content":[]}}',
  );
  assert.equal(
    decoder.feedJSON(
      '{"type":"response.completed","response":{"id":"r1","object":"response","status":"completed","model":"m","output":[],"usage":{"input_tokens":0,"output_tokens":0}}}',
    ).length,
    2,
  );
  assert.equal(decoder.Flush().length, 0);
});
