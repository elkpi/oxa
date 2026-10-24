import assert from "node:assert/strict";
import test from "node:test";

import type { Loss } from "../src/loss.js";
import {
  decodeRequest,
  encodeRequest,
  encodeResponse,
  validateRequest,
  CACHE_CONTROL_LOSS_DETAIL,
  DEFER_LOADING_LOSS_DETAIL,
  NATIVE_KIND_DEFER_LOADING,
  NATIVE_KIND_EAGER_INPUT_STREAMING,
  EAGER_INPUT_STREAMING_LOSS_DETAIL,
  fingerprintLosses,
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
  encodeResponse as encodeAnthropicResponse,
} from "../src/anthropic/messages/index.js";
import {
  encodeRequest as encodeChatCompletions,
  decodeRequestJSON as decodeChatCompletionsJSON,
} from "../src/openai/chatcompletions/index.js";
import {
  encodeRequest as encodeResponses,
  decodeRequestJSON as decodeResponsesJSON,
} from "../src/openai/responses/index.js";

test("rejects conflicting fields on an Anthropic document source", () => {
  const raw = JSON.stringify({
    model: "m",
    max_tokens: 1,
    messages: [
      {
        role: "user",
        content: [
          {
            type: "document",
            source: {
              type: "base64",
              media_type: "application/pdf",
              data: "YQ==",
              url: "",
            },
          },
        ],
      },
    ],
  });
  assert.throws(() => decodeAnthropicJSON(raw), /conflicting source fields/);
});

test("preserves empty Anthropic document metadata", () => {
  const decoded = decodeAnthropicJSON(
    JSON.stringify({
      model: "m",
      max_tokens: 1,
      messages: [
        {
          role: "user",
          content: [
            {
              type: "document",
              source: { type: "file", file_id: "f" },
              title: "",
              context: "",
            },
          ],
        },
      ],
    }),
  );
  const encoded = encodeAnthropic(decoded.value);
  const block = (
    encoded.value.messages as { content: Record<string, unknown>[] }[]
  )[0]!.content[0]!;
  assert.equal(block.title, "");
  assert.equal(block.context, "");
});

test("rejects empty document source fields on Anthropic encode", () => {
  const request: Request = {
    model: "m",
    params: { max_tokens: 1n },
    messages: [
      {
        role: "user",
        content: [
          { type: "document", data: "", media_type: "application/pdf" },
        ],
      },
    ],
  };
  const encoded = encodeAnthropic(request);
  assert.equal(encoded.losses.length, 1);
  assert.equal(encoded.losses[0]?.field, "document");
  assert.equal(encoded.losses[0]?.reason, "unsupported-semantic");
});

test("rejects empty required document source fields on Anthropic decode", () => {
  for (const source of [
    { type: "file", file_id: "" },
    { type: "base64", media_type: "application/pdf", data: "" },
    { type: "base64", media_type: "", data: "YQ==" },
    { type: "url", url: "" },
  ]) {
    assert.throws(
      () =>
        decodeAnthropicJSON(
          JSON.stringify({
            model: "m",
            max_tokens: 1,
            messages: [
              { role: "user", content: [{ type: "document", source }] },
            ],
          }),
        ),
      /must not be empty/,
    );
  }
});

test("reports only the whole-block loss for an unknown document source", () => {
  const decoded = decodeAnthropicJSON(
    JSON.stringify({
      model: "m",
      max_tokens: 1,
      messages: [
        {
          role: "user",
          content: [
            {
              type: "document",
              source: { type: "content", content: "plain text" },
              cache_control: { type: "ephemeral", ttl: "forever" },
            },
          ],
        },
      ],
    }),
  );
  assert.equal(decoded.losses.length, 1);
  assert.equal(decoded.losses[0]?.field, "type");
  assert.equal(decoded.losses[0]?.reason, "unsupported-semantic");
});

test("drops document blocks from Anthropic response encoding", () => {
  const encoded = encodeAnthropicResponse({
    id: "r",
    model: "m",
    content: [
      { type: "text", text: "hi" },
      { type: "document", file_id: "f" },
    ],
    stop_reason: "end_turn",
    usage: { input_tokens: 1n, output_tokens: 1n },
  });
  assert.equal(encoded.losses.length, 1);
  assert.equal(encoded.losses[0]?.field, "type");
  assert.equal(encoded.losses[0]?.reason, "unsupported-semantic");
  const content = encoded.value.content as Record<string, unknown>[];
  assert.equal(content.length, 1);
  assert.equal(content[0]?.type, "text");
});

test("rejects document blocks in IR response encoding", () => {
  assert.throws(
    () =>
      encodeResponse({
        id: "r",
        model: "m",
        content: [{ type: "document", file_id: "f" }],
        stop_reason: "end_turn",
        usage: { input_tokens: 1n, output_tokens: 1n },
      }),
    /document/,
  );
});

test("reports a loss for an IR document with both url and file_id", () => {
  const encoded = encodeAnthropic({
    model: "m",
    params: { max_tokens: 1n },
    messages: [
      {
        role: "user",
        content: [
          {
            type: "document",
            url: "https://example.com/a.pdf",
            file_id: "f",
          },
        ],
      },
    ],
  });
  assert.equal(encoded.losses.length, 1);
  assert.equal(encoded.losses[0]?.field, "document");
  assert.equal(encoded.losses[0]?.reason, "unsupported-semantic");
});

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

test("cache_control omission loss detail is populated for cross encoding, including without omission or empty detail", () => {
  const raw = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 64000,
    system: [
      {
        type: "text",
        text: "S",
        cache_control: { type: "ephemeral" },
      },
    ],
    tools: [
      {
        name: "f",
        description: "d",
        input_schema: { type: "object" },
        cache_control: { type: "ephemeral" },
      },
    ],
    messages: [
      {
        role: "user",
        content: [
          { type: "text", text: "go", cache_control: { type: "ephemeral" } },
        ],
      },
    ],
  });

  const decoded = decodeAnthropicJSON(raw);
  assert.equal(decoded.losses.length, 0);

  const cc = encodeChatCompletions(decoded.value);
  assert.equal(cc.losses.length, 3);
  for (const l of cc.losses) {
    assert.equal(l.field, "cache_control");
    assert.equal(l.reason, "unmapped-field");
    assert.equal(l.detail, CACHE_CONTROL_LOSS_DETAIL);
  }

  const resp = encodeResponses(decoded.value);
  assert.equal(resp.losses.length, 3);
  for (const l of resp.losses) {
    assert.equal(l.detail, CACHE_CONTROL_LOSS_DETAIL);
  }

  // Directly constructed IR with omission omitted
  const reqNoOmission: Request = {
    model: "claude-opus-5-5",
    native: [
      {
        source: "anthropic",
        kind: "cache_control",
        value: JSON.stringify({ type: "ephemeral" }),
      },
    ],
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
  };
  const ccNoOm = encodeChatCompletions(reqNoOmission);
  assert.equal(ccNoOm.losses.length, 1);
  assert.equal(ccNoOm.losses[0]?.field, "cache_control");
  assert.equal(ccNoOm.losses[0]?.reason, "unmapped-field");
  assert.equal(ccNoOm.losses[0]?.detail, CACHE_CONTROL_LOSS_DETAIL);

  // Directly constructed IR with empty detail
  const reqEmptyDetail: Request = {
    model: "claude-opus-5-5",
    native: [
      {
        source: "anthropic",
        kind: "cache_control",
        value: JSON.stringify({ type: "ephemeral" }),
        omission: { field: "cache_control", reason: "unmapped-field" },
      },
    ],
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
  };
  const ccEmptyDetail = encodeChatCompletions(reqEmptyDetail);
  assert.equal(ccEmptyDetail.losses.length, 1);
  assert.equal(ccEmptyDetail.losses[0]?.detail, CACHE_CONTROL_LOSS_DETAIL);
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

test("defer_loading tool native option round-trip and validation", () => {
  // 1. defer_loading: true round-trip with zero losses
  const raw = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hi" }],
    tools: [
      {
        name: "t1",
        description: "tool 1",
        input_schema: { type: "object" },
        defer_loading: true,
      },
    ],
  });
  const decoded = decodeAnthropicJSON(raw);
  assert.equal(decoded.losses.length, 0);
  assert.equal(decoded.value.tools?.[0]?.native?.[0]?.kind, "defer_loading");
  assert.equal(decoded.value.tools?.[0]?.native?.[0]?.value, "true");
  assert.equal(
    decoded.value.tools?.[0]?.native?.[0]?.omission?.reason,
    "degraded",
  );

  const encoded = encodeAnthropic(decoded.value);
  assert.equal(encoded.losses.length, 0);
  const outTool = (encoded.value as any).tools?.[0];
  assert.equal(outTool?.defer_loading, true);

  // 2. defer_loading: false produces no native annotation
  const rawFalse = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hi" }],
    tools: [
      {
        name: "t1",
        input_schema: { type: "object" },
        defer_loading: false,
      },
    ],
  });
  const decodedFalse = decodeAnthropicJSON(rawFalse);
  assert.equal(decodedFalse.losses.length, 0);
  assert.equal(decodedFalse.value.tools?.[0]?.native, undefined);

  // 3. conflict: defer_loading: true AND cache_control throws
  const rawConflict = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hi" }],
    tools: [
      {
        name: "t1",
        input_schema: { type: "object" },
        defer_loading: true,
        cache_control: { type: "ephemeral" },
      },
    ],
  });
  assert.throws(
    () => decodeAnthropicJSON(rawConflict),
    /cannot specify both defer_loading and cache_control/,
  );

  // 4. non-boolean defer_loading throws
  const rawNonBool = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hi" }],
    tools: [
      {
        name: "t1",
        input_schema: { type: "object" },
        defer_loading: "true",
      },
    ],
  });
  assert.throws(
    () => decodeAnthropicJSON(rawNonBool),
    /tools\[0\]\.defer_loading must be a boolean/,
  );

  // 5. whitespace boolean annotation round-trips
  const reqWhitespace: Request = {
    model: "claude-opus-5-5",
    params: { max_tokens: 16000n },
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
    tools: [
      {
        name: "t1",
        input_schema: { type: "object" },
        native: [
          {
            source: "anthropic",
            kind: NATIVE_KIND_DEFER_LOADING,
            value: " true \n",
          },
        ],
      },
    ],
  };
  validateNativeRequest(reqWhitespace);
  const encWhitespace = encodeAnthropic(reqWhitespace);
  assert.equal(encWhitespace.losses.length, 0);
  assert.equal((encWhitespace.value as any).tools?.[0]?.defer_loading, true);

  // 6. direct IR conflict: Tool with both cache_control and defer_loading
  const reqDirectConflict: Request = {
    model: "claude-opus-5-5",
    params: { max_tokens: 16000n },
    messages: [{ role: "user", content: [{ type: "text", text: "hi" }] }],
    tools: [
      {
        name: "t1",
        input_schema: { type: "object" },
        native: [
          {
            source: "anthropic",
            kind: NATIVE_KIND_DEFER_LOADING,
            value: "true",
          },
          {
            source: "anthropic",
            kind: "cache_control",
            value: JSON.stringify({ type: "ephemeral" }),
          },
        ],
      },
    ],
  };
  assert.throws(
    () => validateNativeRequest(reqDirectConflict),
    /cannot specify both defer_loading and cache_control/,
  );
  assert.throws(
    () => encodeAnthropic(reqDirectConflict),
    /cannot specify both defer_loading and cache_control/,
  );

  // 7. native tool declaration with non-boolean defer_loading is preserved opaquely
  const rawNative = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hi" }],
    tools: [
      {
        type: "future_20990101",
        name: "future",
        defer_loading: { future: true },
      },
    ],
  });
  const decodedNative = decodeAnthropicJSON(rawNative);
  assert.equal(decodedNative.losses.length, 0);
  assert.equal(decodedNative.value.tools?.[0]?.type, "native");

  // 8. cache_control: null + defer_loading: true returns conflict error
  const rawNullCache = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 16000,
    messages: [{ role: "user", content: "hi" }],
    tools: [
      {
        name: "t1",
        input_schema: { type: "object" },
        defer_loading: true,
        cache_control: null,
      },
    ],
  });
  assert.throws(
    () => decodeAnthropicJSON(rawNullCache),
    /cannot specify both defer_loading and cache_control/,
  );

  // 9. public exports check
  assert.equal(NATIVE_KIND_DEFER_LOADING, "defer_loading");
  assert.ok(DEFER_LOADING_LOSS_DETAIL.length > 0);
});

test("fingerprintLosses extracts safe subset and limits output", () => {
  const losses: Loss[] = [
    { path: "tools[0]", field: "eager_input_streaming", reason: "degraded", detail: "sensitive detail 1" },
    { path: "native[1]", field: "store", reason: "unsupported-semantic", detail: "sensitive detail 2" },
    { path: "stream_options", field: "extra", reason: "unmapped-field", detail: "sensitive detail 3" },
  ];

  const all = fingerprintLosses(losses, 0);
  assert.equal(all.length, 3);
  assert.deepEqual(all[0], {
    path: "tools[0]",
    field: "eager_input_streaming",
    reason: "degraded",
  });
  assert.equal((all[0] as any).detail, undefined);

  const truncated = fingerprintLosses(losses, 2);
  assert.equal(truncated.length, 2);
  assert.equal(truncated[1]?.field, "store");
});

test("eager_input_streaming tool native option round-trip and validation", () => {
  // 1. 32 tools carrying eager_input_streaming: true produce 0 losses
  const tools = Array.from({ length: 32 }, (_, i) => ({
    name: `tool_${i}`,
    input_schema: { type: "object" },
    eager_input_streaming: true,
  }));
  const raw32 = JSON.stringify({
    model: "claude-opus-5-5",
    max_tokens: 1024,
    messages: [{ role: "user", content: "hi" }],
    tools,
  });
  const decoded = decodeAnthropicJSON(raw32);
  assert.equal(decoded.losses.length, 0);
  assert.equal(decoded.value.tools?.length, 32);
  assert.equal(
    decoded.value.tools?.[0]?.native?.[0]?.kind,
    NATIVE_KIND_EAGER_INPUT_STREAMING,
  );

  // 2. Non-boolean eager_input_streaming throws
  for (const bad of ["true", 1, null, {}]) {
    const rawBad = JSON.stringify({
      model: "claude-opus-5-5",
      max_tokens: 1024,
      messages: [{ role: "user", content: "hi" }],
      tools: [{ name: "t1", input_schema: { type: "object" }, eager_input_streaming: bad }],
    });
    assert.throws(
      () => decodeAnthropicJSON(rawBad),
      /tools\[0\]\.eager_input_streaming must be a boolean/,
    );
  }

  // 3. Round-trip
  const encoded = encodeAnthropic(decoded.value);
  assert.equal((encoded.value as any).tools?.[0]?.eager_input_streaming, true);
});

test("chatcompletions request controls conflict and validation", () => {
  // Conflict
  const rawConflict = JSON.stringify({
    model: "gpt-5",
    messages: [{ role: "user", content: "hi" }],
    max_tokens: 100,
    max_completion_tokens: 200,
  });
  assert.throws(
    () => decodeChatCompletionsJSON(rawConflict),
    /cannot specify both max_tokens and max_completion_tokens/,
  );

  // stream_options unknown child loss
  const rawUnknown = JSON.stringify({
    model: "gpt-5",
    messages: [{ role: "user", content: "hi" }],
    stream_options: { include_usage: true, unknown_field: "foo" },
  });
  const res = decodeChatCompletionsJSON(rawUnknown);
  assert.ok(
    res.losses.some(
      (l) => l.path === "stream_options.unknown_field" && l.reason === "unmapped-field",
    ),
  );
});
