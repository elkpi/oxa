import assert from "node:assert/strict";
import test from "node:test";

import type { Loss } from "../src/loss.js";
import {
  decodeRequest,
  encodeRequest,
  encodeResponse,
  validateRequest,
  type Request,
  type TextBlock,
  type NativeAnnotation,
} from "../src/ir/index.js";
import {
  validateNativeRequest,
  omittedNativeRequestLosses,
} from "../src/ir/native.js";
import {
  decodeRequestJSON as decodeAnthropicJSON,
  decodeResponseJSON as decodeAnthropicResponseJSON,
  encodeRequest as encodeAnthropic,
} from "../src/anthropic/messages/index.js";
import {
  encodeRequest as encodeChatCompletions,
  decodeRequestJSON as decodeChatCompletionsJSON,
} from "../src/openai/chatcompletions/index.js";
import {
  encodeRequest as encodeResponses,
  decodeRequestJSON as decodeResponsesJSON,
} from "../src/openai/responses/index.js";

test("native portable round trip through IR codec", () => {
  const req: Request = {
    model: "test-model",
    native: [
      {
        source: "anthropic",
        kind: "thinking_mode",
        value: '"adaptive"',
        omission: { field: "type", reason: "unmapped-field" },
      },
      {
        source: "anthropic",
        kind: "context_management",
        value: '{"edits":[{"keep":"all","type":"clear_thinking_20251015"}]}',
        omission: {
          field: "context_management",
          reason: "unsupported-semantic",
        },
      },
    ],
    system: [
      {
        type: "text",
        text: "System prompt",
        native: [
          {
            source: "anthropic",
            kind: "cache_control",
            value: '{"type":"ephemeral"}',
            omission: { field: "cache_control", reason: "unmapped-field" },
          },
        ],
      },
    ],
    messages: [
      {
        role: "user",
        content: [
          {
            type: "text",
            text: "Hello",
            native: [
              {
                source: "anthropic",
                kind: "cache_control",
                value: '{"ttl":"1h","type":"ephemeral"}',
                omission: { field: "cache_control", reason: "unmapped-field" },
              },
            ],
          },
        ],
      },
    ],
    tools: [
      {
        name: "lookup",
        input_schema: { type: "object" },
        native: [
          {
            source: "anthropic",
            kind: "cache_control",
            value: '{"type":"ephemeral"}',
            omission: { field: "cache_control", reason: "unmapped-field" },
          },
        ],
      },
    ],
    params: {
      reasoning_effort: "xhigh",
    },
  };

  const encoded = encodeRequest(req);
  const decoded = decodeRequest(encoded);

  assert.equal(decoded.native?.length, 2);
  assert.equal(decoded.native?.[0]?.value, '"adaptive"');
  assert.equal(decoded.system?.[0]?.native?.length, 1);
  assert.equal(decoded.system?.[0]?.native?.[0]?.kind, "cache_control");
  const textBlk = decoded.messages[0]?.content[0] as TextBlock;
  assert.equal(textBlk.native?.length, 1);
  assert.equal(textBlk.native?.[0]?.kind, "cache_control");
  assert.equal(decoded.tools?.[0]?.native?.length, 1);
  assert.equal(decoded.params?.reasoning_effort, "xhigh");
});

test("validateNativeRequest detects structural errors", () => {
  // Duplicate known kind
  const dupReq: Request = {
    model: "m",
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
    native: [
      { source: "anthropic", kind: "thinking_mode", value: '"adaptive"' },
      { source: "anthropic", kind: "thinking_mode", value: '"enabled"' },
    ],
  };
  assert.throws(() => validateNativeRequest(dupReq));

  // Invalid JSON value
  const badJsonReq: Request = {
    model: "m",
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
    native: [
      { source: "anthropic", kind: "cache_control", value: "{invalid json}" },
    ],
  };
  assert.throws(() => validateNativeRequest(badJsonReq));

  // Unauthorized host (thinking_mode on SystemBlock)
  const badHostReq: Request = {
    model: "m",
    system: [
      {
        type: "text",
        text: "sys",
        native: [
          { source: "anthropic", kind: "thinking_mode", value: '"adaptive"' },
        ],
      },
    ],
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
  };
  assert.throws(() => validateNativeRequest(badHostReq));
});

test("omittedNativeRequestLosses computes expected losses and does not mutate request", () => {
  const req: Request = {
    model: "m",
    native: [
      {
        source: "future_provider",
        kind: "future_feature",
        value: '{"foo":"bar"}',
      },
    ],
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
  };

  const losses = omittedNativeRequestLosses(req, "responses");
  assert.equal(losses.length, 1);
  assert.equal(losses[0]?.reason, "unsupported-semantic");
  assert.equal(losses[0]?.field, "source");
  assert.equal(losses[0]?.path, "native[0]");

  // Verify not mutated
  assert.equal(req.native?.length, 1);
  assert.equal(req.native?.[0]?.source, "future_provider");
});

test("omittedNativeRequestLosses handles thinking budget projection", () => {
  // Valid unedited budget projection: degraded budget_tokens loss only, no duplicate type loss
  const req1: Request = {
    model: "m",
    params: { reasoning_effort: "medium" },
    native: [
      {
        source: "anthropic",
        kind: "thinking_mode",
        value: '"enabled"',
        omission: { field: "type", reason: "unmapped-field" },
      },
      {
        source: "anthropic",
        kind: "thinking_budget",
        value: "8192",
        omission: {
          field: "budget_tokens",
          reason: "degraded",
          detail: "budget approximated",
        },
        projection: { origin: "budget", effort: "medium" },
      },
    ],
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
  };

  const losses1 = omittedNativeRequestLosses(req1, "responses");
  assert.equal(losses1.length, 1);
  assert.equal(losses1[0]?.field, "budget_tokens");
  assert.equal(losses1[0]?.reason, "degraded");

  // Edited budget projection: stale budget degraded suppressed, mode type loss reported
  const req2: Request = {
    model: "m",
    params: { reasoning_effort: "low" },
    native: [
      {
        source: "anthropic",
        kind: "thinking_mode",
        value: '"enabled"',
        omission: { field: "type", reason: "unmapped-field" },
      },
      {
        source: "anthropic",
        kind: "thinking_budget",
        value: "8192",
        omission: {
          field: "budget_tokens",
          reason: "degraded",
          detail: "budget approximated",
        },
        projection: { origin: "budget", effort: "medium" },
      },
    ],
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
  };

  const losses2 = omittedNativeRequestLosses(req2, "responses");
  assert.equal(
    losses2.some(
      (l: Loss) => l.field === "budget_tokens" && l.reason === "degraded",
    ),
    false,
  );
  assert.equal(
    losses2.some(
      (l: Loss) => l.field === "type" && l.reason === "unmapped-field",
    ),
    true,
  );
});

test("Anthropic thinking and effort combinations", () => {
  const raw = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hello" }],
    thinking: { type: "adaptive" },
    output_config: { effort: "xhigh" },
  });

  const decoded = decodeAnthropicJSON(raw);
  assert.equal(decoded.losses.length, 0);
  assert.equal(decoded.value.params?.reasoning_effort, "xhigh");

  const encoded = encodeAnthropic(decoded.value);
  assert.equal(encoded.losses.length, 0);
  const outThinking = (encoded.value as any).thinking;
  const outOutputConfig = (encoded.value as any).output_config;
  assert.equal(outThinking?.type, "adaptive");
  assert.equal(outOutputConfig?.effort, "xhigh");
});

test("explicit effort only encodes back to output_config.effort", () => {
  const raw = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hi" }],
    output_config: { effort: "low" },
  });
  const decoded = decodeAnthropicJSON(raw);
  assert.equal(decoded.losses.length, 0);
  const encoded = encodeAnthropic(decoded.value);
  assert.equal(encoded.losses.length, 0);
  const outThinking = (encoded.value as any).thinking;
  const outOutputConfig = (encoded.value as any).output_config;
  assert.equal(outThinking, undefined);
  assert.equal(outOutputConfig?.effort, "low");
});

test("context management recursive audit strips unknown fields", () => {
  const raw = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hi" }],
    context_management: {
      edits: [
        {
          type: "clear_tool_uses_20250919",
          future_policy: true,
          trigger: {
            type: "input_tokens",
            value: 30000,
            future_nested: false,
          },
        },
      ],
    },
  });
  const decoded = decodeAnthropicJSON(raw);
  assert.equal(
    decoded.losses.some(
      (l) => l.field === "future_policy" && l.reason === "unmapped-field",
    ),
    true,
  );
  assert.equal(
    decoded.losses.some(
      (l) => l.field === "future_nested" && l.reason === "unmapped-field",
    ),
    true,
  );
  const encoded = encodeAnthropic(decoded.value);
  const edits = (encoded.value as any).context_management?.edits;
  assert.equal(edits?.length, 1);
  assert.equal(edits[0]?.future_policy, undefined);
  assert.equal(edits[0]?.trigger?.future_nested, undefined);
});

test("response native isolation does not pollute response blocks", () => {
  const raw = JSON.stringify({
    id: "r1",
    model: "claude-opus-5-5",
    stop_reason: "end_turn",
    content: [
      { type: "text", text: "hi", cache_control: { type: "ephemeral" } },
    ],
    usage: { input_tokens: 1, output_tokens: 1 },
  });
  const decoded = decodeAnthropicResponseJSON(raw);
  assert.equal(
    decoded.losses.some(
      (l) => l.field === "cache_control" && l.reason === "unmapped-field",
    ),
    true,
  );
  const block = decoded.value.content[0] as any;
  assert.equal(block.native, undefined);
  // Must encode cleanly through IR response encoder without throwing
  assert.doesNotThrow(() => {
    encodeResponse(decoded.value);
  });
});

test("custom tool result with nested native preserves cache_control", () => {
  const raw = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    tools: [
      {
        name: "f",
        input_schema: {
          type: "object",
          properties: { input: { type: "string" } },
          required: ["input"],
        },
      },
    ],
    messages: [
      { role: "user", content: "hi" },
      {
        role: "assistant",
        content: [
          { type: "tool_use", id: "c1", name: "f", input: { input: "run" } },
        ],
      },
      {
        role: "user",
        content: [
          {
            type: "tool_result",
            tool_use_id: "c1",
            content: [
              {
                type: "text",
                text: "result",
                cache_control: { type: "ephemeral" },
              },
            ],
          },
        ],
      },
    ],
  });
  const decoded = decodeAnthropicJSON(raw);
  const irDoc = encodeRequest(decoded.value);
  const reloaded = decodeRequest(irDoc);
  const encoded = encodeAnthropic(reloaded);
  const userContent = (encoded.value as any).messages[2]?.content;
  assert.equal(Array.isArray(userContent), true);
  const toolResult = userContent[0];
  assert.equal(toolResult.type, "tool_result");
  const innerContent = toolResult.content;
  assert.equal(Array.isArray(innerContent), true);
  assert.equal(innerContent[0]?.cache_control?.type, "ephemeral");
});
