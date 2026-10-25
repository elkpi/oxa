import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeRequestJSON as decodeAnthropicJSON,
  decodeResponseJSON as decodeAnthropicResponseJSON,
  encodeRequest as encodeAnthropic,
  encodeResponse as encodeAnthropicResponse,
} from "../src/anthropic/messages/index.js";
import { encodeRequest as encodeChatCompletions } from "../src/openai/chatcompletions/index.js";
import { encodeRequest as encodeResponses } from "../src/openai/responses/index.js";
import type {
  Request,
  TextBlock,
  RedactedThinkingBlock,
} from "../src/ir/index.js";

test("v2.4.11: Anthropic redacted_thinking round trip", () => {
  const raw = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 4096,
    messages: [
      { role: "user", content: "Prior context." },
      {
        role: "assistant",
        content: [
          { type: "redacted_thinking", data: "OPAQUE_ENCRYPTED_TEST_PAYLOAD" },
          { type: "text", text: "Answer." },
        ],
      },
      { role: "user", content: "Proceed." },
    ],
  });

  const { value: irReq, losses: decLosses } = decodeAnthropicJSON(raw);
  assert.equal(decLosses.length, 0);

  const asstContent = irReq.messages[1]!.content;
  assert.equal(asstContent.length, 2);
  const redacted = asstContent[0] as RedactedThinkingBlock;
  assert.equal(redacted.type, "redacted_thinking");
  assert.equal(redacted.data, "OPAQUE_ENCRYPTED_TEST_PAYLOAD");

  const { value: encoded, losses: encLosses } = encodeAnthropic(irReq);
  assert.equal(encLosses.length, 0);
  const encodedStr = JSON.stringify(encoded);
  assert.ok(encodedStr.includes('"type":"redacted_thinking"'));
  assert.ok(encodedStr.includes('"data":"OPAQUE_ENCRYPTED_TEST_PAYLOAD"'));
});

test("v2.4.11: Anthropic thinking between_tools round trip and conflict", () => {
  const raw = JSON.stringify({
    model: "claude-sonnet-5-5",
    max_tokens: 4096,
    thinking: { type: "between_tools" },
    messages: [{ role: "user", content: "Hi" }],
  });

  const { value: irReq, losses: decLosses } = decodeAnthropicJSON(raw);
  assert.equal(decLosses.length, 0);
  const modeAnn = irReq.native?.find((a: any) => a.kind === "thinking_mode");
  assert.ok(modeAnn);
  assert.equal(modeAnn?.value, '"between_tools"');

  const { value: encoded, losses: encLosses } = encodeAnthropic(irReq);
  assert.equal(encLosses.length, 0);
  assert.equal((encoded.thinking as any)?.type, "between_tools");

  // Conflict with budget
  assert.throws(() => {
    decodeAnthropicJSON(
      JSON.stringify({
        model: "claude-sonnet-5-5",
        max_tokens: 4096,
        thinking: { type: "between_tools", budget_tokens: 2048 },
        messages: [{ role: "user", content: "Hi" }],
      }),
    );
  }, /between_tools/);
});

test("v2.4.11: Anthropic top_k round trip", () => {
  for (const k of [0, 40, 100]) {
    const raw = JSON.stringify({
      model: "claude-opus-4-6",
      max_tokens: 1024,
      top_k: k,
      messages: [{ role: "user", content: "Hi" }],
    });

    const { value: irReq, losses: decLosses } = decodeAnthropicJSON(raw);
    assert.equal(decLosses.length, 0);
    const topKAnn = irReq.native?.find((a: any) => a.kind === "top_k");
    assert.ok(topKAnn);
    assert.equal(topKAnn?.value, String(k));

    const { value: encoded, losses: encLosses } = encodeAnthropic(irReq);
    assert.equal(encLosses.length, 0);
    assert.equal(Number((encoded as any).top_k.token), k);
  }
});

test("v2.4.11: Anthropic disable_parallel_tool_use round trip and none conflict", () => {
  for (const flag of [true, false]) {
    const raw = JSON.stringify({
      model: "claude-sonnet-4-5",
      max_tokens: 1024,
      messages: [{ role: "user", content: "Hi" }],
      tools: [{ name: "tool1", input_schema: { type: "object" } }],
      tool_choice: { type: "auto", disable_parallel_tool_use: flag },
    });

    const { value: irReq, losses: decLosses } = decodeAnthropicJSON(raw);
    assert.equal(decLosses.length, 0);
    const parAnn = irReq.native?.find(
      (a: any) => a.kind === "disable_parallel_tool_use",
    );
    assert.ok(parAnn);
    assert.equal(parAnn?.value, String(flag));

    const { value: encoded, losses: encLosses } = encodeAnthropic(irReq);
    assert.equal(encLosses.length, 0);
    assert.equal((encoded.tool_choice as any)?.disable_parallel_tool_use, flag);
  }

  assert.throws(() => {
    decodeAnthropicJSON(
      JSON.stringify({
        model: "claude-sonnet-4-5",
        max_tokens: 1024,
        messages: [{ role: "user", content: "Hi" }],
        tools: [{ name: "tool1", input_schema: { type: "object" } }],
        tool_choice: { type: "none", disable_parallel_tool_use: true },
      }),
    );
  }, /disable_parallel_tool_use/);
});

test("v2.4.11: Anthropic text citations round trip", () => {
  const raw = JSON.stringify({
    id: "msg_cite",
    type: "message",
    role: "assistant",
    model: "claude-sonnet-4-6",
    content: [
      {
        type: "text",
        text: "Summary statement.",
        citations: [
          {
            type: "char_location",
            cited_text: "source quote",
            document_index: 0,
            document_title: "Document 1",
            start_char_index: 0,
            end_char_index: 12,
            file_id: "file_doc_1",
          },
        ],
      },
    ],
    stop_reason: "end_turn",
    usage: { input_tokens: 10, output_tokens: 10 },
  });

  const { value: irResp, losses: decLosses } = decodeAnthropicResponseJSON(raw);
  assert.equal(decLosses.length, 0);
  const txt = irResp.content[0] as TextBlock;
  assert.equal(txt.type, "text");
  assert.equal(txt.citations?.length, 1);
  assert.equal(txt.citations?.[0]!.source, "anthropic");

  const { value: encoded, losses: encLosses } = encodeAnthropicResponse(irResp);
  assert.equal(encLosses.length, 0);
  const encStr = JSON.stringify(encoded);
  assert.ok(encStr.includes('"char_location"'));
  assert.ok(encStr.includes('"file_doc_1"'));
});

test("v2.4.11: Cross-face conversion drops redacted_thinking with unsupported-semantic loss", () => {
  const req: Request = {
    model: "claude-opus-5-5",
    messages: [
      { role: "user", content: [{ type: "text", text: "Question." }] },
      {
        role: "assistant",
        content: [
          { type: "redacted_thinking", data: "OPAQUE_TEST" },
          { type: "text", text: "Answer." },
        ],
      },
      { role: "user", content: [{ type: "text", text: "Followup." }] },
    ],
  };

  const { losses: ccLosses } = encodeChatCompletions(req);
  assert.ok(
    ccLosses.some(
      (l) =>
        l.reason === "unsupported-semantic" &&
        l.detail?.includes("encrypted thinking"),
    ),
  );

  const { losses: reLosses } = encodeResponses(req);
  assert.ok(
    reLosses.some(
      (l) =>
        l.reason === "unsupported-semantic" &&
        l.detail?.includes("encrypted thinking"),
    ),
  );
});

test("v2.4.11: RedactedThinking data validation and empty string preservation", () => {
  const decoded = decodeAnthropicJSON(
    JSON.stringify({
      model: "claude-opus-5-5",
      max_tokens: 1024,
      messages: [
        { role: "user", content: "Hi" },
        {
          role: "assistant",
          content: [
            { type: "redacted_thinking", data: "" },
            { type: "text", text: "Ok" },
          ],
        },
      ],
    }),
  );
  const encoded = encodeAnthropic(decoded.value);
  assert.equal(((encoded.value as any).messages[1] as any).content[0].data, "");

  assert.throws(
    () =>
      decodeAnthropicJSON(
        JSON.stringify({
          model: "claude-opus-5-5",
          max_tokens: 1024,
          messages: [
            { role: "user", content: "Hi" },
            {
              role: "assistant",
              content: [
                { type: "redacted_thinking", data: null },
                { type: "text", text: "Ok" },
              ],
            },
          ],
        }),
      ),
    /data is required/,
  );
});

test("v2.4.11: Disable parallel tool use null rejection and encode conflict", () => {
  assert.throws(
    () =>
      decodeAnthropicJSON(
        JSON.stringify({
          model: "claude-sonnet-4-5",
          max_tokens: 1024,
          messages: [{ role: "user", content: "Hi" }],
          tools: [{ name: "t1", input_schema: { type: "object" } }],
          tool_choice: { type: "auto", disable_parallel_tool_use: null },
        }),
      ),
    /boolean/,
  );

  const reqConflict: Request = {
    model: "claude-sonnet-4-5",
    messages: [{ role: "user", content: [{ type: "text", text: "Hi" }] }],
    tool_choice: { mode: "none" },
    native: [
      {
        source: "anthropic",
        kind: "disable_parallel_tool_use",
        value: "true",
      },
    ],
  };
  assert.throws(
    () => encodeAnthropic(reqConflict),
    /disable_parallel_tool_use/,
  );
});

test("v2.4.11: between_tools encode conflict with budget", () => {
  const reqConflict: Request = {
    model: "claude-sonnet-5-5",
    messages: [{ role: "user", content: [{ type: "text", text: "Hi" }] }],
    native: [
      { source: "anthropic", kind: "thinking_mode", value: '"between_tools"' },
      { source: "anthropic", kind: "thinking_budget", value: "2048" },
    ],
  };
  assert.throws(() => encodeAnthropic(reqConflict), /between_tools/);
});

test("v2.4.11: omittedNativeRequestLosses default fallback for top_k and disable_parallel_tool_use", async () => {
  const req: Request = {
    model: "claude-opus-4-6",
    messages: [{ role: "user", content: [{ type: "text", text: "Hi" }] }],
    tool_choice: { mode: "auto" },
    native: [
      { source: "anthropic", kind: "top_k", value: "40" },
      { source: "anthropic", kind: "disable_parallel_tool_use", value: "true" },
    ],
  };
  const { omittedNativeRequestLosses } = await import("../src/ir/index.js");
  const losses = omittedNativeRequestLosses(req, "chatcompletions");
  assert.ok(
    losses.some(
      (l: any) => l.field === "top_k" && l.reason === "unmapped-field",
    ),
  );
  assert.ok(
    losses.some(
      (l: any) =>
        l.field === "disable_parallel_tool_use" &&
        l.reason === "unmapped-field",
    ),
  );
});
