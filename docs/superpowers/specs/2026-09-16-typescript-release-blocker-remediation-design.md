# TypeScript Release-Blocker Remediation Design

## Status

Proposed. This design resolves release-blocking findings against the normative
sources: vectors, schemas, then specifications.

## Goal

Make the TypeScript implementation conform to N-AN-4, streaming int64 usage,
IR INV-2 through INV-4, and Responses N-S-3 loss/lifecycle rules before any
npm publication.

## 1. Lossless Anthropic tool input

Typed Anthropic wire objects gain an optional raw `inputText: JsonText` sidecar
for `tool_use.input`. The JSON decoding boundary supplies it from the source
span when available; `decodeRequest` and `decodeResponse` prefer it after
checking that its parsed value is an object. They copy it directly to
`ToolUseBlock.input`; encoding writes the same token directly. Generic callers
without a sidecar retain the existing canonical lossless-tree fallback, which
is permitted by N-AN-4 and records no loss.

Tests prove key order, whitespace, escaping, and numeric spelling survive a
typed JSON decode -> IR -> Anthropic encode round trip byte-identically.

## 2. Stream usage integers

All native stream usage fields use a lossless integer input type (`bigint` or
integer JSON token), never a JavaScript `number` conversion path. A shared
parser accepts only non-negative integral values in int64 range and returns
`bigint`; absent optional usage stays absent where the face protocol permits
it. Negative, fractional, non-finite, and out-of-range values throw
`OxaError("invalid-input")` before emitting IR. Encoders reject usage outside
the native int64 contract rather than coercing with `Number`.

Tests cover `MAX_SAFE_INTEGER + 1`, int64 limits, negative, fractional, and
out-of-range values for Chat, Responses, and Anthropic streams.

## 3. IR request invariants

A single request validator in `ir/nonstream` enforces INV-2 through INV-4 at
all non-stream decode exits and before non-stream encode. It requires one or
more messages; starts with user; prohibits empty message content; validates
ordered tool use/result pairing and matching IDs. The Responses decoder
normalizes an empty assistant native item to one empty `TextBlock` before
validation, per N-R-2. Violations are structural `OxaError`s, not losses.

Tests exercise each invariant through every face and retain all valid vectors.

## 4. Responses skipped-unit containment

Responses stream state tracks skipped output items and skipped content parts
explicitly. All descendants of one skipped unit are absorbed without more than
one loss. For supported units, every descendant event validates native item,
content, and output identities before any loss is recorded; a mismatch throws
`stream-lifecycle`. The native part union admits unknown types so this behavior
is type-safe and testable.

Tests prove one-loss containment for unsupported parts and structural failure
for mismatched unknown descendants.

## Delivery and verification

Implement in four TDD commits in the order above. Each change gets a focused
red test, face/vector regression run, full TypeScript gates, and independent
review. Then run `npm run release:check`, Windows CI, and a final whole-branch
review. Publishing remains disallowed until all are clean.
