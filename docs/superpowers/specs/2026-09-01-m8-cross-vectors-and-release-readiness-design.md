# M8 Design: Cross-Protocol Vectors and Release Readiness

Date: 2026-09-01
Status: approved (user-confirmed in session)
Milestone: M8 — end-to-end cross vectors + release readiness

## 1. Goals

1. **Cross-protocol golden vectors.** Twelve `protocol-to-protocol` vectors
   under `vectors/cross/nonstream/`: six ordered face pairs × request /
   response. The pairs are CC→Responses, CC→Anthropic, Responses→CC,
   Responses→Anthropic, Anthropic→CC, Anthropic→Responses.
2. **Test-only composition runner.** A `vectest` runner that executes
   `source.Decode → IR → target.Encode` and compares against the vector.
   The composition exists ONLY in the test harness; no production
   face-to-face converter is added (spec/00 architecture constraint).
3. **Accurate public README.** Replace the stale "Nothing here works yet"
   status and "Coming soon" quick start with a correct pre-v1 description,
   support matrix, and a compile-verified usage snippet.
4. **Compile-verified Go example.** A godoc `Example` function exercising
   the cross-face composition through public APIs, keeping the README story
   honest.
5. **Release-readiness documentation.** Fill the root `CHANGELOG.md`
   `[Unreleased]` section and add a release checklist; document (but do not
   perform) release actions.

## 2. Non-goals

- No production direct face-to-face conversion API.
- No cross streaming vectors (M8 covers nonstream only; per user decision).
- No Git tag, GitHub Release, or remote publish. The release checklist
  defines preconditions only; any actual release requires separate explicit
  authorization.
- No new spec documents; the only spec change is a small normative section
  on cross-composition loss semantics (§5 below).

## 3. Vector layout and schema use

Files: `vectors/cross/nonstream/<src>-to-<tgt>-<request|response>.json`, e.g.
`vectors/cross/nonstream/chatcompletions-to-anthropic-request.json`.

- `name` follows the existing dotted-path rule, e.g.
  `cross.nonstream.chatcompletions-to-anthropic-request`.
- `conversion: "protocol-to-protocol"`; `source` and `target` endpoints name
  the two protocols.
- `input` is the source-face wire document; `expected_output` is the
  target-face wire document. `expected_ir` is FORBIDDEN by the existing
  vector schema conditional (`required: [target, expected_output], not:
  [expected_ir]`), so no schema change is needed.
- `expected_losses` is the ordered concatenation of the source decode losses
  followed by the target encode losses (§5). Comparison stays the existing
  unordered-set rule.
- `veccheck` needs no changes: `listVectorFiles` already recurses into
  `vectors/cross/`, the schema validates the conditional requirements, and
  the IR-side manual check correctly skips `protocol-to-protocol` (its
  `input` is a wire document, not an IR document).

## 4. Scenario design

All twelve vectors share one semantically equivalent scenario so the pairs
differ only in face rendering:

- **Request scenario:** a `system`/`instructions` prompt, a short multi-turn
  text exchange, one tool definition, one assistant tool call with its tool
  result, and sampling params (temperature, max_tokens, stop). Each source
  face additionally carries one face-specific field that cannot survive the
  trip (Chat Completions `logprobs`, Responses `previous_response_id`,
  Anthropic `metadata.user_id`), so every vector exercises loss merging.
- **Response scenario:** assistant text plus one tool call in the output,
  usage totals, and a `tool_use` stop reason where the source expresses it.

Vectors are produced by running the real converters on hand-written source
inputs, then each intermediate IR document and the expected output is
reviewed against spec/10–12 before being frozen as golden. Face-level
to-ir/from-ir vectors already lock each stage, so the cross vectors
primarily lock the composition itself: loss concatenation order, envelope
rendering on the target side, and IR-as-hub correctness.

## 5. Spec change: cross-composition loss semantics (spec/02)

A new normative section in `spec/02-loss-policy.md`, after the streaming
section, defining:

- A cross-protocol conversion is the composition
  `source.Decode → IR → target.Encode`; the reported loss list is the
  source-decode losses followed by the target-encode losses, in that order.
- Loss `path` values remain local to their own stage (source wire document
  for decode losses, IR document for encode losses); they are not rewritten
  with prefixes. The `detail` of a loss SHOULD make its stage clear.
- The unordered-set comparison rule for vectors is unaffected.

`vectors/README.md` gains a matching "Cross vectors" subsection (layout,
composition, loss concatenation) and the `<face>` layout enumeration gains
`cross`.

## 6. Harness design (go/internal/vectest)

New file `go/internal/vectest/cross.go`. The existing `Converter` interface
already carries exactly the cross surface needed (`Face()` plus the four
wire↔IR methods), so no new interface is introduced:

```go
// RunCross executes every nonstream cross-protocol vector whose source and
// target endpoints match the two converters' Face() protocol names. It
// fails the test if a matched pair has no vectors.
func RunCross(t *testing.T, source, target Converter)
```

- `Vector` gains `Source`/`Target` endpoint fields (`{"protocol": ...}`);
  existing single-face vectors carry only `source`, which unmarshals
  harmlessly.
- Loading reuses `LoadVectors(root, "cross", "nonstream")`.
- A matched pair with zero vectors is a test failure (guards against a
  typo'd protocol silently skipping a pair).
- Per vector: request vectors compose
  `source.DecodeRequestWire → target.EncodeRequestIR`, response vectors
  compose `source.DecodeResponseWire → target.EncodeResponseIR`; reported
  losses are the decode losses followed by the encode losses; the output
  and the combined loss set compare through the existing `CompareJSON` /
  `CompareLosses` rules.
- Comparison: structural JSON equality for `expected_output`; losses from
  both stages concatenated then compared as the usual unordered set.
- The real face bindings and the integration test live in
  `go/internal/vectest/cross_test.go`, which imports the three face
  packages (test-only; the `internal/deps` guard inspects non-test imports
  only, so hub-and-spoke stays intact).

TDD: the runner is built against fake converters first (watch it fail, watch
it pass), then the real face bindings are added together with the vectors
they verify.

## 7. Go example

New file `go/openai/chatcompletions/example_test.go`
(package `chatcompletions_test`): a deterministic `Example` that converts a
Chat Completions request to an Anthropic Messages request through the IR
using only public APIs, printing the key parts of the result under
`// Output:`. Test-only imports across spokes are permitted; production
spoke imports remain unchanged. README's quick start shows the same
composition inline.

## 8. README, CHANGELOG, release checklist

- **README.md:** status becomes "early development, pre-v1" with the Go
  reference implementation usable; a support matrix (nonstream
  request/response for all three faces; streaming text and M7 tool-argument
  aggregation; cross vectors nonstream); a two-step quick start
  (`DecodeRequest` → `EncodeRequest`); badges updated (license + CI); links
  to spec, vectors, changelog, release checklist.
- **CHANGELOG.md:** fill `[Unreleased]` with the project's notable entries
  so far (spec + vectors + Go reference implementation: IR, three faces,
  nonstream conversion, streaming text, M7 streaming tool aggregation,
  cross vectors, SSE adapter, modelmap, veccheck).
- **docs/release-checklist.md:** the precondition list for tagging a
  release (CI green including race matrix, vectors + manifest check,
  module path not a placeholder — enforced by the tag-only `release-guard`
  CI job — CHANGELOG dated, spec frozen at the release version, tag and
  release-notes procedure). The document defines preconditions; it does not
  perform a release.

## 9. Verification

- `make fmt && make lint && make test && make vectors && make
  check-modulepath` from the repo root.
- `cd go && go test -count=1 ./...` and `go run ./cmd/veccheck -root ..
  -check-manifest` after the manifest refresh.
- The twelve cross vectors pass for all six ordered pairs through the new
  runner; a deliberately wrong protocol in any binding fails the suite
  (zero-vector guard verified once by hand).

## 10. Commits (cherry-pickable units)

1. `docs(spec): cross-protocol composition loss semantics` — spec/02 §,
   vectors/README cross subsection.
2. `feat(go): cross-protocol vector runner` — vectest `cross.go` +
   Endpoint fields + fake-stage tests.
3. `test(vectors): add cross-protocol nonstream vectors` — 12 vectors +
   real-face binding test + manifest refresh.
4. `feat(go): cross-face conversion example` — example_test.go.
5. `docs: accurate README status, support matrix, quick start`.
6. `docs: changelog entries and release checklist`.

Specification, vectors, implementation, and docs stay in separate commits;
the vectors commit includes the binding test because the test directly
verifies those vectors.
