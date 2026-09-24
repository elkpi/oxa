import assert from "node:assert/strict";
import test from "node:test";

import {
  decodeEventStream,
  type Event,
} from "../src/ir/index.js";
import {
  fromValue,
  isJsonArray,
  isJsonNumber,
  parseJson,
  type JsonObject,
} from "../src/json/index.js";
import type { Loss } from "../src/loss.js";
import {
  AnthropicStreamDecoder,
  AnthropicStreamEncoder,
} from "../src/anthropic/messages/index.js";
import {
  ChatCompletionsStreamDecoder,
  ChatCompletionsStreamEncoder,
} from "../src/openai/chatcompletions/index.js";
import {
  ResponsesStreamDecoder,
  ResponsesStreamEncoder,
} from "../src/openai/responses/index.js";
import {
  compareJson,
  compareLosses,
  compareStreams,
  findRepoRoot,
  loadVectors,
} from "../src/vectest/index.js";
/**
 * Vector JSON is parsed losslessly, so every number is a JsonNumber token.
 * The typed native event shapes require plain numbers on stream position
 * fields, while usage fields intentionally keep JsonNumber for int64
 * fidelity; hydrate only the position fields.
 */
const NATIVE_NUMBER_FIELDS = new Set([
  "index",
  "output_index",
  "content_index",
  "created",
  "sequence_number",
]);

function hydratePositions(value: unknown): unknown {
  if (isJsonNumber(value)) return value;
  if (isJsonArray(value)) {
    let changed = false;
    const result = value.map((child) => {
      const hydrated = hydratePositions(child);
      if (hydrated !== child) changed = true;
      return hydrated;
    });
    return changed ? result : value;
  }
  if (value !== null && typeof value === "object") {
    let changed = false;
    const result: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value)) {
      if (key === "input") {
        result[key] = child;
      } else if (
        NATIVE_NUMBER_FIELDS.has(key) &&
        isJsonNumber(child) &&
        child.isInteger
      ) {
        result[key] = Number(child.token);
        changed = true;
      } else {
        const hydrated = hydratePositions(child);
        result[key] = hydrated;
        if (hydrated !== child) changed = true;
      }
    }
    // Preserve parsed identities when no native position needs hydration.
    return changed ? result : value;
  }
  return value;
}

interface StreamFaceHarness {
  decodeFeed(event: unknown): readonly Event[];
  decodeFlush(): readonly Event[];
  decodeLosses(): readonly Loss[];
  encodeApply(event: Event): { value: readonly unknown[]; losses: readonly Loss[] };
}

const harnesses: Record<string, () => StreamFaceHarness> = {
  chatcompletions: () => {
    const decoder = new ChatCompletionsStreamDecoder();
    const encoder = new ChatCompletionsStreamEncoder();
    return {
      decodeFeed: (event) => decoder.Feed(event as never),
      decodeFlush: () => decoder.Flush(),
      decodeLosses: () => decoder.Losses(),
      encodeApply: (event) => encoder.Apply(event),
    };
  },
  responses: () => {
    const decoder = new ResponsesStreamDecoder();
    const encoder = new ResponsesStreamEncoder();
    return {
      decodeFeed: (event) => decoder.Feed(event as never),
      decodeFlush: () => decoder.Flush(),
      decodeLosses: () => decoder.Losses(),
      encodeApply: (event) => encoder.Apply(event),
    };
  },
  anthropic: () => {
    const decoder = new AnthropicStreamDecoder();
    const encoder = new AnthropicStreamEncoder();
    return {
      decodeFeed: (event) => decoder.Feed(event as never),
      decodeFlush: () => decoder.Flush(),
      decodeLosses: () => decoder.Losses(),
      encodeApply: (event) => encoder.Apply(event),
    };
  },
};

function faceOf(name: string): string {
  const face = name.split(".")[0]!;
  assert.ok(
    face in harnesses,
    `${name}: no stream harness for face ${face}`,
  );
  return face;
}

function withVectorName(name: string, run: () => void): void {
  try {
    run();
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    assert.fail(`${name}: ${detail}`);
  }
}

test("hydrates nested native indexes without rewriting opaque tool input", () => {
  const input = parseJson('{"index":1,"nested":[{"content_index":2}]}') as JsonObject;
  const nativeEvent = {
    choices: [
      {
        index: parseJson("0"),
        delta: { tool_calls: [{ index: parseJson("0") }] },
        input,
      },
    ],
  };

  const hydrated = hydratePositions(nativeEvent) as {
    readonly choices: readonly [
      {
        readonly index: unknown;
        readonly delta: { readonly tool_calls: readonly [{ readonly index: unknown }] };
        readonly input: unknown;
      },
    ];
  };

  assert.equal(hydrated.choices[0].index, 0);
  assert.equal(hydrated.choices[0].delta.tool_calls[0].index, 0);
  assert.equal(hydrated.choices[0].input, input);
  assert.ok(isJsonNumber(input.index));
});

test("runs every stream golden vector through the public converters", () => {
  const root = findRepoRoot(process.cwd());
  assert.ok(root !== undefined);
  const vectors = loadVectors(root).filter(
    ({ document }) => document.mode === "stream",
  );
  assert.ok(vectors.length > 0);

  for (const vector of vectors) {
    withVectorName(vector.name, () => {
      const harness = harnesses[faceOf(vector.name)]!();
    const input = vector.document.input as JsonObject;
    const expectedLosses = vector.document
      .expected_losses as unknown as Loss[];
    const conversion = vector.document.conversion as string;

    if (conversion === "to-ir") {
      const nativeEvents = (
        input.events as readonly unknown[]
      ).map(hydratePositions);
      const actual: Event[] = [];
      for (const event of nativeEvents)
        actual.push(...harness.decodeFeed(event));
      actual.push(...harness.decodeFlush());
      const expectedIr = vector.document.expected_ir as JsonObject;
      const difference = compareStreams(
        decodeEventStream(expectedIr),
        { events: actual },
      );
      assert.equal(difference, undefined, vector.name);
      assert.deepEqual(
        compareLosses(expectedLosses, harness.decodeLosses()),
        undefined,
        vector.name,
      );
    } else if (conversion === "from-ir") {
      const events = decodeEventStream(input).events;
      const actual: unknown[] = [];
      const losses: Loss[] = [];
      for (const event of events) {
        const result = harness.encodeApply(event);
        actual.push(...result.value);
        losses.push(...result.losses);
      }
      const expectedOutput = vector.document.expected_output as JsonObject;
      const actualOutput = {
        events: fromValue(actual as never),
      } as unknown as JsonObject;
      assert.deepEqual(
        compareJson(expectedOutput, actualOutput),
        undefined,
        vector.name,
      );
      assert.deepEqual(
        compareLosses(expectedLosses, losses),
        undefined,
        vector.name,
      );
    } else {
      assert.fail(`${vector.name}: unknown conversion ${String(conversion)}`);
    }
    });
  }
});

test("runs every M9 reasoning stream vector through both directions", () => {
  const root = findRepoRoot(process.cwd());
  assert.ok(root !== undefined);
  const m9 = loadVectors(root).filter(({ name }) => name.includes(".m9-"));
  assert.equal(m9.length, 6);
  for (const vector of m9) {
    assert.equal(vector.document.spec_version, "0.2.0", vector.name);
    const conversion = vector.document.conversion as string;
    assert.match(conversion, /^to-ir$|^from-ir$/, vector.name);
  }
});
