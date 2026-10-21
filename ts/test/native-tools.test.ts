import test from "node:test";
import assert from "node:assert/strict";
import { decodeRequestJSON, encodeRequest } from "../src/anthropic/messages/nonstream.js";
import { encodeRequest as encodeCCRequest } from "../src/openai/chatcompletions/nonstream.js";
import { encodeRequest as encodeRERequest } from "../src/openai/responses/nonstream.js";
import { decodeRequest as decodeIRRequest } from "../src/ir/nonstream.js";

import { stringifyJson } from "../src/json/index.js";

test("preserves native tool declaration fidelity", () => {
  const definition = `{ "type" : "advisor_20260301", "name":"advisor", "model":"opus", "max_uses":2, "vendor":{"n":1e+03}, "cache_control":{"type":"ephemeral"} }`;
  const raw = `{"model":"m","max_tokens":32,"tools":[${definition}],"messages":[{"role":"user","content":"hi"}]}`;
  const decoded = decodeRequestJSON(raw);
  assert.equal(decoded.losses.length, 0);
  assert.equal(decoded.value.tools?.length, 1);
  const tool = decoded.value.tools![0]!;
  assert.equal(tool.type, "native");
  assert.equal(tool.name, "advisor");
  assert.equal(tool.source, "anthropic");
  assert.equal(tool.definition, definition);

  const encoded = encodeRequest(decoded.value);
  assert.equal(encoded.losses.length, 0);
  const toolStr = stringifyJson((encoded.value.tools as any[])[0]);
  assert.ok(toolStr.includes('"model":"opus"'));
  assert.ok(toolStr.includes('"max_uses":2'));
  assert.ok(toolStr.includes('1e+03'));
});

test("rejects malformed native and function tool declarations", () => {
  const invalid = [
    `{"name":"function"}`,
    `{"name":"function","input_schema":[]}`,
    `{"type":null,"name":"function","input_schema":{}}`,
    `{"type":7,"name":"function","input_schema":{}}`,
    `{"type":"advisor_20260301","name":""}`,
    `{"type":"advisor_20260301","name":7}`,
  ];
  for (const tool of invalid) {
    const raw = `{"model":"m","max_tokens":32,"tools":[${tool}],"messages":[{"role":"user","content":"hi"}]}`;
    assert.throws(() => decodeRequestJSON(raw));
  }
});

test("omits native tool choice without explicit tool_type across target protocols", () => {
  const raw = `{"model":"m","max_tokens":32,"tools":[{"type":"advisor_20260301","name":"advisor","model":"opus"}],"tool_choice":{"type":"tool","name":"advisor"},"messages":[{"role":"user","content":"hi"}]}`;
  const decoded = decodeRequestJSON(raw);
  const req = {
    ...decoded.value,
    tool_choice: { mode: "tool" as const, name: "advisor" },
  };

  const cc = encodeCCRequest(req);
  assert.equal(cc.value.tools, undefined);
  assert.equal(cc.value.tool_choice, undefined);
  assert.equal(cc.losses.length, 2);

  const re = encodeRERequest(req);
  assert.equal(re.value.tools, undefined);
  assert.equal(re.value.tool_choice, undefined);
  assert.equal(re.losses.length, 2);
});

test("rejects invalid native tool in IR decode", () => {
  const valid = {
    type: "native",
    name: "advisor",
    source: "anthropic",
    definition: '{"type":"advisor_20260301","name":"advisor"}',
  };
  const invalidIR = [
    { ...valid, source: "" },
    { ...valid, definition: '{"type":"advisor_20260301","name":"other"}' },
    { ...valid, definition: "[]" },
    { ...valid, input_schema: {} },
  ];
  for (const tool of invalidIR) {
    assert.throws(() =>
      decodeIRRequest({
        specVersion: "0.2.0",
        model: "m",
        messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
        tools: [tool as any],
      }),
    );
  }
});
