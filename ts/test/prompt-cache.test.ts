import assert from "node:assert/strict";
import test from "node:test";

import { parseJson, stringifyJson } from "../src/json/index.js";
import type { JsonObject } from "../src/json/index.js";
import { validateNativeRequest } from "../src/ir/native.js";
import type { Request } from "../src/ir/index.js";
import {
  decodeRequestJSON as decodeCCJSON,
  encodeRequest as encodeCC,
} from "../src/openai/chatcompletions/index.js";
import {
  decodeRequest as decodeResponses,
  decodeRequestJSON as decodeResponsesJSON,
  encodeRequest as encodeResponses,
} from "../src/openai/responses/index.js";

const baseIR: Request = {
  model: "gpt-4o",
  messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
};

function wireText(wire: JsonObject): string {
  return stringifyJson(parseJson(stringifyJson(wire)) as JsonObject);
}

test("responses encode keeps system breakpoints on input items, not instructions", () => {
  const request: Request = {
    ...baseIR,
    system: [
      {
        type: "text",
        text: "policy",
        native: [
          {
            source: "openai",
            kind: "prompt_cache_breakpoint",
            value: '{"mode":"explicit"}',
          },
        ],
      },
    ],
  };
  const { value: wire, losses } = encodeResponses(request);
  assert.deepEqual(losses, []);
  const text = wireText(wire);
  assert.ok(!text.includes('"instructions"'), `got ${text}`);
  assert.ok(text.includes('"prompt_cache_breakpoint"'), `got ${text}`);
});

test("responses decode preserves explicit null prompt_cache_options", () => {
  const decoded = decodeResponsesJSON(
    '{"model":"m","input":"hi","prompt_cache_options":null}',
  );
  assert.equal(decoded.value.native?.length, 1);
  assert.equal(decoded.value.native?.[0]?.value, "null");
  const { value: wire } = encodeResponses(decoded.value);
  const text = wireText(wire);
  assert.ok(text.includes('"prompt_cache_options":null'), `got ${text}`);
});

test("chatcompletions decode rejects invalid prompt_cache_options values", () => {
  for (const value of [
    '{"mode":"bad"}',
    '{"ttl":"1h"}',
    '{"mode":123}',
    '{"ttl":30}',
  ]) {
    assert.throws(
      () =>
        decodeCCJSON(
          `{"model":"m","messages":[{"role":"user","content":"hi"}],"prompt_cache_options":${value}}`,
        ),
      `expected rejection for ${value}`,
    );
  }
});

test("responses decode rejects invalid prompt_cache_options values", () => {
  for (const value of [
    '{"mode":"bad"}',
    '{"ttl":"1h"}',
    '{"mode":123}',
    '{"ttl":30}',
  ]) {
    assert.throws(
      () =>
        decodeResponsesJSON(
          `{"model":"m","input":"hi","prompt_cache_options":${value}}`,
        ),
      `expected rejection for ${value}`,
    );
  }
});

test("both faces reject invalid prompt_cache_breakpoint values", () => {
  const cc = (value: string) =>
    decodeCCJSON(
      `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi","prompt_cache_breakpoint":${value}}]}]}`,
    );
  const resp = (value: string) =>
    decodeResponsesJSON(
      `{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"hi","prompt_cache_breakpoint":${value}}]}]}`,
    );
  for (const value of ['{"mode":"bad"}', '{"ttl":"wrong"}', '{"mode":1}']) {
    assert.throws(() => cc(value), `expected rejection for ${value}`);
    assert.throws(() => resp(value), `expected rejection for ${value}`);
  }
  // mode is optional per spec/15 §3.7.
  assert.doesNotThrow(() => cc("{}"));
  assert.doesNotThrow(() => resp('{"ttl":"30m"}'));
});

test("image blocks restore prompt_cache_breakpoint on encode", () => {
  const withImage: Request = {
    ...baseIR,
    messages: [
      {
        role: "user",
        content: [
          {
            type: "image",
            url: "https://example.com/cat.png",
            native: [
              {
                source: "openai",
                kind: "prompt_cache_breakpoint",
                value: '{"mode":"explicit"}',
              },
            ],
          },
        ],
      },
    ],
  };
  const { value: ccWire } = encodeCC(withImage);
  const ccText = wireText(ccWire);
  assert.ok(ccText.includes('"prompt_cache_breakpoint"'), `got ${ccText}`);
  const { value: respWire } = encodeResponses(withImage);
  const respText = wireText(respWire);
  assert.ok(respText.includes('"prompt_cache_breakpoint"'), `got ${respText}`);
});

test("ir validation rejects prompt_cache_breakpoint on unsupported blocks and null values", () => {
  const thinking: Request = {
    ...baseIR,
    messages: [
      {
        role: "user",
        content: [
          {
            type: "thinking",
            thinking: "hmm",
            native: [
              {
                source: "openai",
                kind: "prompt_cache_breakpoint",
                value: '{"mode":"explicit"}',
              },
            ],
          },
        ],
      },
    ],
  };
  assert.throws(() => validateNativeRequest(thinking));
  const nullValue: Request = {
    ...baseIR,
    messages: [
      {
        role: "user",
        content: [
          {
            type: "text",
            text: "hi",
            native: [
              {
                source: "openai",
                kind: "prompt_cache_breakpoint",
                value: "null",
              },
            ],
          },
        ],
      },
    ],
  };
  assert.throws(() => validateNativeRequest(nullValue));
});
