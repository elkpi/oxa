# TypeScript Release-Blocker Remediation Review Report

## Scope

Fixed-point review of `3b7d18f..934a596` (Tasks 1–4 plus one style commit),
performed in-session against the normative anchors N-AN-4, INV-1 through
INV-4, N-S-3, int64 usage bounds, package exports, and repository style.
Per-task implementer reports, per-task independent reviews, and fix-round
diffs live in `.superpowers/sdd/2026-09-16-typescript-release-blocker-remediation/`.

## Normative verification

### N-AN-4 — typed Anthropic raw tool input

`decodeContent` resolves `tool_use.input` text as `block.inputText` →
`sourceTextOf(input)` → `jsonText(stringifyJson(input))`
(`ts/src/anthropic/messages/nonstream.ts:294-302`); a supplied sidecar that
does not parse to an object is a face `type-violation`. `encodeContent`
re-attaches the raw token via `withSourceText`
(`ts/src/anthropic/messages/nonstream.ts:356`), and `stringifyJson` emits a
registered raw object token before its canonical branch
(`ts/src/json/stringify.ts:21`). Source spans live in a module-private
`WeakMap` (`ts/src/json/value.ts:11`). **PASS.**

### INV-1 — opaque raw JSON text

Tool inputs and `input_json_delta.partial_json` stay `jsonText` fragments
throughout; `ResponsesStreamDecoder` never re-parses or re-marshals fragments
(`ts/src/openai/responses/stream.ts:375,383`). **PASS.**

### INV-2 through INV-4 — request invariants

`validateRequest` enforces non-empty messages, user-first (INV-2), non-empty
content, ordered tool_use/tool_result pairing with exact ID match (INV-3/4),
rejects orphan and split results, and rejects nested `tool_result` recursively
(`ts/src/ir/nonstream.ts:118-191`). It runs at IR decode-return, IR
encode-entry, and every face nonstream boundary. **PASS.**

### Stream usage int64

`parseUsageInteger` accepts `bigint` or integer `JsonNumber` only within
`0n..9_223_372_036_854_775_807n`, throwing `OxaError("invalid-input")`
otherwise (`ts/src/ir/usage.ts`). `isInteger` is defined as
`!/[.eE]/.test(token)` (`ts/src/json/parse.ts:148`), so `BigInt(value.token)`
only ever receives plain integer literals. No `Number(bigint)` or
`BigInt(number)` paths exist in usage code. **PASS.**

### N-S-3 — Responses skipped-unit containment

Unsupported items and content parts each record exactly one
`unsupported-semantic` loss (`ts/src/openai/responses/stream.ts:180,246`).
`#requireSkippedDescendant` validates output index, item id, and content index
before absorbing any descendant (`stream.ts:526-543`);
`#requireUnknownEventIdentity` makes identity-bearing unknown events
structural failures while identity-less standalone events keep a single loss
(`stream.ts:504-524`). Part state clears only at its matching
`content_part.done`; item state at `output_item.done`. **PASS.**

### Package exports and architecture

`ts/src/index.ts` exports error/loss/modelmap namespaces plus `json`, `ir`,
`sse`, `stream`, and the three faces, matching every `exports` subpath in
`ts/package.json`. No cross-face imports exist in `ts/src` (also enforced by
the 8-test architecture suite). **PASS.**

## Gate evidence

- `npm run release:check` (ts/): vectors and manifest (131 checks), generated
  declarations, format, types, Node tests, Web runtime, package contents,
  clean ESM/TypeScript consumer, Go tests/build/vet/format, module path —
  **all passed**; "release checks passed; no package was published".
- `make vectors && make test && make lint && make fmt` (root): 130 vectors,
  130 checks, OK; Go suite green; vet and gofmt clean.
- Focused suites during Tasks 1–4: 123/123 node tests at branch head
  (re-verified at `934a596`), architecture 8/8, `go run ./cmd/veccheck -root
  .. -check-manifest` 131 checks OK.

## Findings

| Severity | Finding | Disposition |
| --- | --- | --- |
| P2 | `fromValue(number)` produces `isInteger: true` tokens containing `e` for values ≥ 1e21 (`ts/src/json/value.ts:48`); `BigInt(token)` would throw a raw SyntaxError if such a value reached `parseUsageInteger`. Unreachable today: usage accepts only `bigint` and `parseJson` products. | Recorded; no release impact. Tighten if usage ever accepts in-memory numbers. |

No P0 or P1 findings.

## Publish decision

**Approved to proceed to Task 6** (push branch, GitHub Actions Linux +
Windows evidence, then npm publication via the tag-triggered release
workflow). Publication remains conditional on clean CI and a clean
`release:check` re-run against the pushed commit.

## Release evidence (Task 6)

- Branch `typescript-support` pushed through `4421901`; `main`
  fast-forwarded to the same commit.
- Windows CI defect fixed en route: generated-file header path separators
  normalized (`24e5287`) and LF checkout enforced via `.gitattributes`
  (`e442000`); `typescript` and `typescript-windows` jobs green
  (run `35170033484`).
- Tag `ts/v1.0.0` pushed at `4421901`; `release-npm` run `35172533308`
  succeeded after two publish-side corrections:
  `npm publish --access public` first hit `EOTP` (classic Publish token under
  2FA; replaced with a Granular Access Token in the `NPM_TOKEN` secret) and
  then `E422` provenance verification (missing `repository.url`, fixed in
  `4421901`).
- Verified: `npm view @elkpi/oxa version --registry https://registry.npmjs.org`
  → `1.0.0`, tarball
  `https://registry.npmjs.org/@elkpi/oxa/-/oxa-1.0.0.tgz` (66.2 kB, 144 files,
  provenance attested).

## Follow-up (out of scope, tracked)

The remediation vectors (three `usage-int64`, `skipped-part-loss`,
`raw-json-tool-input`, plus Task 3 vector corrections) currently fail the
rust, python, and cpp CI jobs: python/cpp tests hardcode vector totals
(30→31 nonstream anthropic, 8→12 stream), and rust/python disagree with the
vector's expected skipped-part loss path (`output[i].content[j]` vs `type`).
These implementations predate the tightened vectors and need a separate
cross-language alignment plan.
