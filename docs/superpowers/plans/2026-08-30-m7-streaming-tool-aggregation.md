# M7 Streaming Tool-Argument Aggregation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement executable golden-vector coverage and loss-aware, raw-text-preserving M7 streamed function-tool argument conversion for OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages.

**Architecture:** Keep the hub-and-spoke boundary intact: each protocol face converts only typed native stream events to or from `ir.Event`. Decoders buffer each native function-tool unit until its lifecycle closes, then replay one complete IR `ToolUseBlock` and ordered `InputJSONDelta` sequence; text events remain live. Encoders validate the IR block/input-fragment invariant and emit each provider’s native tool stream lifecycle, while the generic vector runner drives face-local adapters without importing a face package.

**Tech Stack:** Go 1.23+, standard library `encoding/json` and fuzzing, `github.com/santhosh-tekuri/jsonschema/v6`, repository Make targets, JSON Schema 2020-12.

**Spec:** `docs/superpowers/specs/2026-08-30-m7-streaming-tool-aggregation-design.md`; implement in source-of-truth order with `spec/20-streaming-semantics.md`, `spec/schema/vector.schema.json`, and `vectors/` taking precedence over Go code.

## Global Constraints

- Maintain the hub-and-spoke dependency rule: a face imports only the standard library, `github.com/elkpi/oxa/go/ir`, and `github.com/elkpi/oxa/go/modelmap`; no face-to-face imports and no new internal-package imports from production face code.
- M7 covers only CC `tool_calls[].index` aggregation, RE `function_call` / `response.function_call_arguments.delta`, and AN `tool_use` / `input_json_delta` aggregation. RE `function_call_output`, reasoning/thinking, provider-hosted tools, and raw-SSE-to-IR remain out of scope and must remain loss-contained unsupported units.
- Treat the payload of `ToolUseBlock.Input` and `InputJSONDelta.PartialJSON` as opaque raw text. Only marshal/unmarshal the outer IR JSON string token; never `json.Valid`, parse, normalize, or re-marshal its payload.
- Preserve every present native argument fragment, including `""`, in encounter order. Invalid/incomplete final argument payloads are valid opaque text, not structural errors.
- For each emitted or accepted tool block, the string payload of `ToolUseBlock.Input` MUST equal the exact concatenation of its `InputJSONDelta.PartialJSON` payloads.
- Preserve INV-5 and INV-6: `message_start`, zero or more complete blocks in contiguous IR indexes, exactly one `message_delta`, and exactly one `message_done`; delta type must match the active block type.
- Stream decoder losses stay in `Losses()` and are never emitted as IR events. Structural input/lifecycle violations return `error`.
- Golden vector names equal their path below `vectors/`, with `/` replaced by `.` and `.json` removed. Update generated `vectors/manifest.json` only through `veccheck -write-manifest`.
- Work units must leave no temporary artifacts, background processes, or worktrees. Run every foreground command with a 120-second timeout; stop a task when its focused verification fails and diagnose before moving on.
- Keep commits narrow and cherry-pickable; specifications, vectors, harness, decoders, encoders, and fuzz tests are separate commits except where a test directly validates the implementation in the same commit.

---

## File Structure and Interfaces

| Path | Responsibility |
| --- | --- |
| `spec/20-streaming-semantics.md` | Replace M6’s M7 deferral with normative decoder buffering, encoder validation, lifecycle/error, and loss rules. |
| `spec/schema/vector.schema.json` | Make parsed native event-stream documents valid vector inputs and remove the unconditional `input_sse` requirement for stream vectors. |
| `vectors/README.md` | Document actual parsed-event stream-vector shapes and executable checker rules; remove claims about nonexistent aggregate comparison. |
| `go/cmd/veccheck/check.go` | Validate IR EventStreams beyond JSON Schema: grammar, indexes, block/delta compatibility, and raw tool-input equality. |
| `go/cmd/veccheck/check_test.go` | Unit-test all manual stream-validation failure modes using temporary vector files or extracted document validators. |
| `go/internal/vectest/stream.go` | Generic executable stream-vector runner and its testable `StreamConverter` interface. |
| `go/internal/vectest/stream_test.go` | Test runner dispatch, ordered event comparison, `Flush`, encoder application, and loss comparison with a fake stream converter. |
| `go/{openai/chatcompletions,openai/responses,anthropic/messages}/vectors_test.go` | Extend each existing vector adapter with typed native event unmarshalling, decoder `Feed`/`Flush`, encoder `Apply`, and canonical event marshalling; call both vector runners. |
| `go/openai/chatcompletions/types.go` | Define typed CC tool-call delta outer shape with pointer fields that preserve omission versus an empty string. |
| `go/openai/chatcompletions/streamin.go` | Decode M7 text and interleaved tool calls with native-index validation and delayed completed-tool replay. |
| `go/openai/chatcompletions/streamout.go` | Encode text/tool IR blocks, validate exact argument aggregation, and report text-after-tool normalization loss. |
| `go/openai/chatcompletions/streaming_test.go` | Cover CC M7 lifecycle, malformed input, encoder output/loss behavior, and fuzz raw argument fragments. |
| `go/openai/responses/types.go` | Model function-call argument stream event fields and marshal their canonical event envelopes. |
| `go/openai/responses/streamin.go` | Decode native function-call item lifecycle and retain `function_call_output` loss containment. |
| `go/openai/responses/streamout.go` | Encode a serial output-item state machine for text items and function-call items. |
| `go/openai/responses/streaming_test.go` | Cover RE function-call aggregation, done validation, skipped output containment, encoder lifecycle, and fuzzing. |
| `go/anthropic/messages/streamin.go` | Retain tool-use blocks, aggregate partial JSON, and use the native start input only when no argument delta exists. |
| `go/anthropic/messages/streamout.go` | Encode canonical `input:{}` tool starts, native partial-json deltas, and final equality validation. |
| `go/anthropic/messages/streaming_test.go` | Cover AN tool aggregation/fallback, unknown-unit behavior, encoder lifecycle, and fuzzing. |
| `vectors/{chatcompletions,responses,anthropic}/stream/*.json` | Source-of-truth M7 parsed-event vectors for both directions and unsupported RE output containment. |
| `vectors/manifest.json` | Generated file list and hashes refreshed after all vector additions. |

The generic harness must add this exact public-to-`internal/vectest` interface in `go/internal/vectest/stream.go`:

```go
type StreamConverter interface {
    Face() string
    DecodeNativeEvent(json.RawMessage) ([]ir.Event, error)
    FlushDecoder() ([]ir.Event, error)
    DecoderLosses() []ir.Loss
    ApplyIREvent(ir.Event) ([]json.RawMessage, []ir.Loss, error)
}

func RunStream(t *testing.T, conv StreamConverter)
```

The `DecodeNativeEvent` method receives one element from `input.events`; it must unmarshal that native event, pass it into the face decoder, marshal any face-returned events with `ir.MarshalEvent`, and return the original typed `ir.Event` sequence. `ApplyIREvent` receives one canonical event parsed by `ir.UnmarshalEvent`, calls the face encoder, marshals every native event with the face type’s `MarshalJSON`, and returns ordered canonical native event JSON. `FlushDecoder` must supply decoder terminal output, and `DecoderLosses` must return the full loss list only after all native events and `Flush` have run.

## Work Units

### Task 1: Specify M7 behavior and parsed-event vector format

**Input range:** `docs/superpowers/specs/2026-08-30-m7-streaming-tool-aggregation-design.md`, `spec/20-streaming-semantics.md`, `spec/schema/vector.schema.json`, and `vectors/README.md`.

**Output artifact:** A normative M7 streaming section, an executable stream-vector schema, and vector documentation that agree on parsed native event documents.

**Completion criteria:** The mapping is explicit for all three supported tool protocols, makes the raw input invariant and structural/loss boundaries normative, and schema validation accepts `input.events` stream vectors without `input_sse`.

**Verification:** `cd go && go run ./cmd/veccheck -root ..` succeeds after a minimal valid parsed-event stream vector exists in Task 2. Before Task 2, validate JSON syntax with `python3 -m json.tool spec/schema/vector.schema.json >/dev/null`.

**Timeout/cancellation:** 120 seconds for JSON validation. Stop if changing the schema would make nonstream vectors invalid; inspect and correct the conditional instead of weakening unrelated constraints.

**Cleanup:** No generated files or temporary JSON documents remain.

**Files:**
- Modify: `spec/20-streaming-semantics.md`
- Modify: `spec/schema/vector.schema.json`
- Modify: `vectors/README.md`
- Test: schema validation through `go/cmd/veccheck` after Task 2

**Interfaces:**
- Consumes: approved design sections 2–8.
- Produces: a vector contract where `mode:"stream"` is `input: {"events": [...]}`; `to-ir` requires an `expected_ir` `ir.EventStream`, and `from-ir` takes an `ir.EventStream` in `input` and requires `expected_output: {"events": [...]}`.

- [ ] **Step 1: Write the failing schema fixtures in Task 2 before changing the schema**

Create one `mode:"stream"`, `conversion:"to-ir"` fixture with `input.events` and no `input_sse`, plus one `conversion:"from-ir"` fixture with an IR EventStream input. They must be rejected by the current schema because the old conditional requires `input_sse`.

- [ ] **Step 2: Run the vector checker to verify the old schema rejects the new shape**

Run: `cd go && go run ./cmd/veccheck -root ..`

Expected: FAIL with a `vector.schema.json` validation error identifying the missing `input_sse` field for the newly added stream fixture.

- [ ] **Step 3: Replace the M7 deferral in `spec/20-streaming-semantics.md` with the selected normative profile**

Write RFC-2119 rules that state all of the following exactly:

```text
A decoder MUST buffer each native function-tool unit until its native closure.
A decoder MUST replay ToolUseBlock, all InputJSONDelta fragments, and ContentBlockStop together only after that closure.
The ToolUseBlock input string MUST equal the exact concatenation of its fragments.
A decoder MUST preserve empty fragments and MUST NOT parse the payload as JSON.
A native lifecycle/identity disagreement is an error; an unsupported native unit is one loss plus descendant absorption.
```

Document face-specific closure boundaries: CC `Flush` after finish reason; RE `response.output_item.done`; AN `content_block_stop`. Define CC index ordering/interleaving, RE optional argument-done validation, AN no-delta fallback, and the encoder synthesis/validation rules from the approved design.

- [ ] **Step 4: Change the stream schema condition without weakening nonstream validation**

Remove only the `then: {"required": ["input_sse"]}` branch that applies to every `mode:"stream"` vector. Keep `input_sse` optional for byte-framing adapter vectors. Add conditional requirements for stream directions:

```json
{
  "if": {"properties": {"mode": {"const": "stream"}, "conversion": {"const": "to-ir"}}, "required": ["mode", "conversion"]},
  "then": {"required": ["input", "expected_ir"]}
}
```

and a matching `from-ir` conditional requiring `input` and `expected_output`. Do not require `input.events` in JSON Schema if schema composition would duplicate protocol-specific native event grammar; `veccheck` and the executable runner will check the object envelope and typed events.

- [ ] **Step 5: Correct `vectors/README.md` stream sections**

State that protocol-face stream vectors contain parsed native `events`, not SSE frames; show both stream document shapes; state full event arrays compare in order. List actual validations only: vector/IR schemas, INV-5/INV-6, block/delta compatibility, tool input/fragment equality, exact JSON-string payload equality, ordered native events, and unordered expected-loss set comparison. Remove any assertion that stream text fragments are compared against a generated nonstream aggregate.

- [ ] **Step 6: Re-run validation after Task 2 vectors exist**

Run: `cd go && go run ./cmd/veccheck -root ..`

Expected: PASS for schema and existing/manual validation, except for failures deliberately introduced by Task 3 before its implementation.

- [ ] **Step 7: Commit specification and schema only**

```bash
git add spec/20-streaming-semantics.md spec/schema/vector.schema.json vectors/README.md
git commit -m "docs(spec): define M7 streaming tool aggregation"
```

### Task 2: Add source-of-truth M7 stream vectors and refresh the manifest

**Input range:** M7 design, updated schema, existing vector naming/format, and stream native/IR JSON codecs.

**Output artifact:** Complete `vectors/<face>/stream/*.json` M7 cases plus a generated, matching `vectors/manifest.json`.

**Completion criteria:** Every vector validates, names match paths, all M7 matrix behaviors are represented in both directions for each face, and `-check-manifest` succeeds.

**Verification:** `cd go && go run ./cmd/veccheck -root ..` followed by `cd go && go run ./cmd/veccheck -root .. -check-manifest`.

**Timeout/cancellation:** 120 seconds for each checker invocation. Stop and repair a vector if it fails schema or manual IR validation; do not change the implementation to accept an invalid vector.

**Cleanup:** `-write-manifest` is the only permitted generator; do not retain hand-written temporary manifests.

**Files:**
- Create: `vectors/chatcompletions/stream/m7-tool-calls-to-ir.json`
- Create: `vectors/chatcompletions/stream/m7-tool-calls-from-ir.json`
- Create: `vectors/responses/stream/m7-function-call-to-ir.json`
- Create: `vectors/responses/stream/m7-function-call-from-ir.json`
- Create: `vectors/responses/stream/m7-function-call-output-loss.json`
- Create: `vectors/anthropic/stream/m7-tool-use-to-ir.json`
- Create: `vectors/anthropic/stream/m7-tool-use-from-ir.json`
- Create: `vectors/anthropic/stream/m7-tool-use-start-input-fallback-to-ir.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Consumes: Task 1 stream vector schema and `ir.MarshalEventStream` wire shape.
- Produces: `input.events` native event documents that face adapters in Task 4 unmarshal one-by-one; `expected_ir`/`input` IR streams that `ir.UnmarshalEventStream` parses.

- [ ] **Step 1: Create a failing CC `to-ir` stream vector**

Use one tool-only response with two native `tool_calls` whose indexes interleave `0, 1, 0, 1`; include one `function.arguments:""` fragment, fragmented names, escapes, whitespace, Unicode, and a noncanonical numeric spelling such as `1e+01`. Expected IR must replay two compact tool blocks in index order only after the native finish chunk, with each tool input exactly equal to its own ordered fragments. Include no empty `TextBlock`.

- [ ] **Step 2: Create a CC `from-ir` vector**

Use an IR stream with text, two tool blocks, and text after a tool block. Each ToolUse block must use an outer JSON string input whose payload matches its explicit `input_json_delta` fragments; one block must contain an empty fragment. Expected native chunks must use consecutive tool indexes and function start fields; expect exactly one `degraded` loss because CC normalizes later text before tool calls.

- [ ] **Step 3: Create RE vectors for retained calls and retained skip behavior**

For `m7-function-call-to-ir.json`, represent `response.created`, `response.output_item.added` with `item.type:"function_call"`, initial arguments, multiple argument deltas including `""`, an optional matching `response.function_call_arguments.done`, `response.output_item.done`, and terminal completed response. Expected IR contains one replayed tool block and exact deltas.

For `m7-function-call-from-ir.json`, use text → tool → text and expect: a completed first message item, a `function_call` item with empty initial arguments, argument deltas, arguments-done, item-done, a new message item, and terminal response with all ordered output items.

For `m7-function-call-output-loss.json`, use a `function_call_output` added/delta-or-done/item-done lifecycle and verify exactly one `unsupported-semantic` expected loss; no IR content block is emitted for the skipped unit.

- [ ] **Step 4: Create AN vectors for partial JSON and the start-input fallback**

For `m7-tool-use-to-ir.json`, use a `tool_use` native start containing `input:{}`, native partial-json deltas including `""`, and a stop. Expected IR starts only when native block stop arrives and carries exact raw fragments.

For `m7-tool-use-start-input-fallback-to-ir.json`, use a tool-use start with a nontrivial raw object input and no `input_json_delta`; expected IR must have one synthesized `input_json_delta` equal to the exact raw start input.

For `m7-tool-use-from-ir.json`, expect a canonical native `content_block_start` with `input:{}`, every IR argument fragment as native `input_json_delta`, then native `content_block_stop`; include opaque escaping, Unicode, and number spellings.

- [ ] **Step 5: Run the checker and fix vectors, not code**

Run: `cd go && go run ./cmd/veccheck -root ..`

Expected: PASS after Task 3 is complete. Before Task 3, expected failures are limited to the manual stream invariants that do not yet exist; any schema/name/IR-schema failure must be fixed in the vector.

- [ ] **Step 6: Generate and verify the manifest**

Run:

```bash
cd go && go run ./cmd/veccheck -root .. -write-manifest
cd go && go run ./cmd/veccheck -root .. -check-manifest
```

Expected: both commands succeed and every new M7 stream file occurs exactly once in `vectors/manifest.json`.

- [ ] **Step 7: Commit vectors and generated manifest together**

```bash
git add vectors/chatcompletions/stream vectors/responses/stream vectors/anthropic/stream vectors/manifest.json
git commit -m "test(vectors): add M7 stream tool cases"
```

### Task 3: Validate stream IR invariants in `veccheck`

**Input range:** `go/cmd/veccheck/check.go`, `go/ir/event.go`, `go/ir/json.go`, stream vectors from Task 2, and IR schema definitions.

**Output artifact:** Checker-side manual event-stream invariant validator with unit tests that reject invalid schema-valid relational states.

**Completion criteria:** `veccheck` rejects malformed grammar, noncontiguous indexes, delta/block mismatches, input fragment mismatches, and invalid raw string envelopes with deterministic field/path diagnostics.

**Verification:** `cd go && go test -count=1 ./cmd/veccheck` and `cd go && go run ./cmd/veccheck -root ..`.

**Timeout/cancellation:** 120 seconds per command. Stop if a validator begins accepting `ir.Event` values unavailable to `ir.UnmarshalEvent`; share existing codec behavior instead of duplicating parsing.

**Cleanup:** Tests use `t.TempDir()` and no checked-in temporary vectors.

**Files:**
- Modify: `go/cmd/veccheck/check.go`
- Create: `go/cmd/veccheck/check_test.go`

**Interfaces:**
- Consumes: `ir.UnmarshalEventStream([]byte) (*ir.EventStream, error)` and Task 1’s stream shape.
- Produces: `validateEventStream(es *ir.EventStream) error`, called for `expected_ir` on stream `to-ir` vectors and `input` on stream `from-ir` vectors.

- [ ] **Step 1: Write table-driven failing tests for the relational invariants**

Add `TestValidateEventStream` cases whose documents individually violate:

```text
missing message_start
content delta without a start
second open block before the first stop
index 1 used for the first IR block
TextDelta applied to ToolUseBlock
InputJSONDelta applied to TextBlock
ToolUseBlock input "{}" with fragment "{\"x\":1}"
ToolUseBlock input that is not a JSON string token
message_done without an immediately preceding message_delta
an event after message_done
```

Also add one valid stream containing text and a tool block with an empty input fragment. Assert exact acceptance/rejection, and use a substring assertion for stable error context rather than full wording.

- [ ] **Step 2: Run the new tests before implementation**

Run: `cd go && go test -count=1 ./cmd/veccheck -run '^TestValidateEventStream$'`

Expected: FAIL because `validateEventStream` does not yet exist.

- [ ] **Step 3: Implement `validateEventStream` using IR typed events**

Parse only through `ir.UnmarshalEventStream`, then maintain:

```go
type openBlock struct {
    index int
    kind  string // "text" or "tool_use"
    input string // ToolUseBlock string payload for tool blocks
    parts []string
}
```

Accept exactly one `MessageStart` first; require each next `ContentBlockStart.Index` to equal `nextIndex`; permit only one open block; accept `TextDelta` only on `text`; accept `InputJSONDelta` only on `tool_use`; unwrap `ToolUseBlock.Input` with `json.Unmarshal` into a Go `string`; compare `strings.Join(parts, "")` with that string at matching stop; require `MessageDelta` only after all blocks; require `MessageDone` immediately afterward and as final event. Return a path-bearing `fmt.Errorf` for every violation.

- [ ] **Step 4: Wire validation into `checkVectorFile`**

When `doc.Mode == "stream"`, continue schema validation and then parse/validate only the IR side selected by `conversion`: `expected_ir` for `to-ir`, `input` for `from-ir`. Keep the existing nonstream Request/Response schema workflow unchanged. Report both schema errors and manual invariant errors where both are observable.

- [ ] **Step 5: Run focused and repository checks**

Run:

```bash
cd go && go test -count=1 ./cmd/veccheck
go run ./cmd/veccheck -root ..
```

Expected: PASS. The Task 2 vectors exercise a valid text/tool stream and the checker reports no errors.

- [ ] **Step 6: Commit checker behavior and tests**

```bash
git add go/cmd/veccheck/check.go go/cmd/veccheck/check_test.go
git commit -m "feat(go): validate stream vector invariants"
```

### Task 4: Make parsed stream vectors executable in the Go harness

**Input range:** `go/internal/vectest/{load.go,run.go,compare.go}`, each face’s `vectors_test.go`, and Task 2 vectors.

**Output artifact:** Generic `RunStream` harness driven by face-local typed event adapters, with direct harness tests.

**Completion criteria:** All three face packages discover nonempty stream vectors; the harness compares full ordered IR/native output arrays and expected losses; no production spoke imports `internal/vectest`.

**Verification:** `cd go && go test -count=1 ./internal/vectest ./openai/chatcompletions ./openai/responses ./anthropic/messages`.

**Timeout/cancellation:** 120 seconds. Stop when an adapter’s wire JSON differs from its public event codec; fix canonical event marshalling rather than comparing Go structs.

**Cleanup:** Fake converters are test-local; no temporary vectors outside `t.TempDir()`.

**Files:**
- Create: `go/internal/vectest/stream.go`
- Create: `go/internal/vectest/stream_test.go`
- Modify: `go/openai/chatcompletions/vectors_test.go`
- Modify: `go/openai/responses/vectors_test.go`
- Modify: `go/anthropic/messages/vectors_test.go`

**Interfaces:**
- Consumes: the exact `StreamConverter` interface defined in File Structure.
- Produces: `vectest.RunStream(t, conv)`; each existing `vectorConverter` implements both its preexisting nonstream `Converter` surface and the new stream surface.

- [ ] **Step 1: Write generic runner tests with a fake converter**

Test a `to-ir` vector where `DecodeNativeEvent` returns one event per input item and `FlushDecoder` returns terminal events. Assert the runner compares full `expected_ir` and calls `DecoderLosses` after flush.

Test a `from-ir` vector where `ApplyIREvent` returns one JSON object per input event. Assert its ordered concatenation is compared against `expected_output.events` and returned losses are accumulated before unordered expected-loss comparison.

Test a converter failure and assert the subtest fails with the conversion error. Use a helper process or extracted non-`testing.T` runner function if needed; do not silently swallow errors.

- [ ] **Step 2: Run runner tests before implementation**

Run: `cd go && go test -count=1 ./internal/vectest -run '^TestRunStream'`

Expected: FAIL because `RunStream` and `StreamConverter` do not exist.

- [ ] **Step 3: Implement `RunStream` and its two direction helpers**

Load `LoadVectors(root, conv.Face(), "stream")`; fail if repository root is found but no stream vectors exist. For `to-ir`, unmarshal `v.Input` into `struct{ Events []json.RawMessage }`, feed every raw native event in order, append `FlushDecoder` events, marshal all events with `ir.MarshalEventStream`, compare to `v.ExpectedIR`, then compare `DecoderLosses()`.

For `from-ir`, parse `ir.UnmarshalEventStream(v.Input)`, call `ApplyIREvent` for every IR event in order, append all returned raw native JSON in exact order, marshal `struct{ Events []json.RawMessage }`, compare to `v.ExpectedOut`, then compare all encoder losses. Reject missing `events` envelopes and unknown conversion directions with `t.Fatalf`.

- [ ] **Step 4: Extend CC’s test-only adapter**

In `go/openai/chatcompletions/vectors_test.go`, define the methods on `vectorConverter` using `StreamChunk` (the current native stream type): unmarshal each raw object to one chunk, call a decoder retained by the adapter, return typed IR events, flush it, expose its losses, unmarshal IR events for an encoder, apply each event, and marshal each returned chunk with `json.Marshal`. Make `TestVectors` call both:

```go
vectest.Run(t, vectorConverter{})
vectest.RunStream(t, newVectorStreamConverter())
```

Use a fresh adapter value per `RunStream` invocation so state cannot cross vectors.

- [ ] **Step 5: Extend RE and AN adapters with their own native event codec**

For Responses, unmarshal/marshal `StreamEvent` using its `UnmarshalJSON`/`MarshalJSON` behavior and route to `NewStreamDecoder` / `NewStreamEncoder`. For Anthropic, use `StreamEvent` and the same face-local public streaming methods. Do not construct provider JSON by hand inside `vectest`.

- [ ] **Step 6: Run the harness and face vector tests**

Run:

```bash
cd go && go test -count=1 ./internal/vectest
go test -count=1 ./openai/chatcompletions ./openai/responses ./anthropic/messages
```

Expected: harness tests pass. Face stream-vector tests may still fail until the relevant Tasks 5–10 implementations are complete; failures must identify actual event-array mismatches rather than skipped vectors.

- [ ] **Step 7: Commit harness and test adapters**

```bash
git add go/internal/vectest/stream.go go/internal/vectest/stream_test.go \
  go/openai/chatcompletions/vectors_test.go go/openai/responses/vectors_test.go go/anthropic/messages/vectors_test.go
git commit -m "feat(go): run stream vectors in Go harness"
```

### Task 5: Decode and encode Chat Completions streamed tool calls

**Input range:** `spec/10-mapping-openai-chat-completions.md`, Task 1 norms, `go/openai/chatcompletions/{types.go,streamin.go,streamout.go,streaming_test.go}`, and CC vectors.

**Output artifact:** M7 CC typed tool delta model, aggregate decoder, encoder, direct tests, and fuzz target.

**Completion criteria:** Tool-only and text/tool CC streams round-trip through typed events; interleaved native indexes become ordered complete IR tool blocks; invalid native IDs/indexes/lifecycle return errors; encoder validates raw equality and produces one degradation loss only for text after a tool.

**Verification:** `cd go && go test -count=1 ./openai/chatcompletions` and `cd go && go test -count=1 -run '^FuzzStreamToolArguments$' ./openai/chatcompletions`.

**Timeout/cancellation:** 120 seconds per command. Stop if a proposed fix parses tool argument payloads; replace it with outer-string wrapping/unwrapping only.

**Cleanup:** Keep Go’s fuzz seed corpus only if it is a deterministic test input under the face package’s `testdata/fuzz/`; delete crash artifacts not chosen as seeds.

**Files:**
- Modify: `go/openai/chatcompletions/types.go`
- Modify: `go/openai/chatcompletions/streamin.go`
- Modify: `go/openai/chatcompletions/streamout.go`
- Modify: `go/openai/chatcompletions/streaming_test.go`

**Interfaces:**
- Consumes: existing `NewStreamDecoder`, `(*StreamDecoder).Feed(*StreamChunk)`, `Flush()`, `Losses()`, and existing stream encoder `Apply(ir.Event)` API.
- Produces: `ToolCallDelta` / `FunctionDelta` wire types; decoder acceptance of `ToolUseBlock` / `InputJSONDelta`; encoder native `tool_calls` deltas.

- [ ] **Step 1: Add failing CC decoder tests**

Add a test that feeds `role`, two interleaved native calls (`index:0`, `index:1`, then both again), distinct name fragments, arguments `"{\"a\":"`, `""`, `"1}"`, finish reason, and `Flush`. Assert no tool event precedes flush; assert compact IR indexes; assert call order is 0 then 1; assert fragments retain encounter order per call and input exactness.

Add error tests for first index 1, a new index that skips a number, conflicting nonempty IDs, missing final ID/name, an unsupported `type:"custom"` unit followed by descendants (one loss only), and tool-only stream with no text block.

- [ ] **Step 2: Run CC decoder tests before implementation**

Run: `cd go && go test -count=1 ./openai/chatcompletions -run '^TestStreamDecoder.*Tool'`

Expected: FAIL because M6 reports `tool_calls` as unsupported or creates a text block prematurely.

- [ ] **Step 3: Make CC tool call deltas typed and presence-aware**

Replace `DeltaPayload.ToolCalls json.RawMessage` with a slice whose element has these exact fields and tags:

```go
type ToolCallDelta struct {
    Index    int            `json:"index"`
    ID       *string        `json:"id,omitempty"`
    Type     *string        `json:"type,omitempty"`
    Function *FunctionDelta `json:"function,omitempty"`
}

type FunctionDelta struct {
    Name      *string `json:"name,omitempty"`
    Arguments *string `json:"arguments,omitempty"`
}
```

Use pointer values to distinguish absent from present-empty `name` / `arguments`. Preserve `index:0` through a non-omitempty `Index` field.

- [ ] **Step 4: Implement per-native-index aggregation in the decoder**

Add a decoder-private record keyed by native call index containing native index, ID, assembled name, `[]string` argument fragments, and `skipped` state. Validate new indexes are consecutive from zero; validate repeated IDs; append every non-nil argument pointer, including empty. Treat supplied non-`function` type as one unsupported unit while retaining index state and absorbing future fragments for it.

Open text only upon non-nil content. On `Flush`, require the existing CC terminal/finish lifecycle, close live text, then iterate native indexes in ascending order. For every retained call, require nonempty ID and name, wrap `strings.Join(fragments, "")` with `json.Marshal`, and replay one ToolUse `ContentBlockStart`, one InputJSONDelta per fragment (outer string marshalled), and `ContentBlockStop`; allocate `nextIRIndex` rather than using native indexes. Finish with the existing message delta/done policy.

- [ ] **Step 5: Add failing CC encoder tests, then implement encoder state**

Add tests for ToolUse start → multiple `InputJSONDelta` (including empty) → stop native chunks; ToolUse with no deltas must synthesize exactly one full `arguments` delta; mismatched aggregate must error; `TextDelta` on a tool block must error; and text after a tool must produce exactly one `ir.LossDegraded` at terminal completion.

Extend `streamout.go` with active block state containing block kind, IR index, ToolUse input string, accumulated fragments, and CC native call index. Emit ID/type/function-name start fields, every raw argument delta, and synthesize only when no deltas arrived. Verify exact equality before marking tool closed. Buffer/normalize text as needed to preserve the CC text-before-tools wire shape while emitting the required single degradation loss.

- [ ] **Step 6: Add and run CC raw-fragment fuzzing**

Add `FuzzStreamToolArguments` with seeds covering `""`, `{}`, escaped quotes/backslashes, Unicode, whitespace, `1e+01`, nested text, and safe UTF-8 split boundaries. For each fuzz input, choose only rune-safe split offsets, feed a complete native call plus finish/flush, unwrap IR outer string tokens, and assert:

```go
strings.Join(decodedFragments, "") == decodedToolInput
```

Run: `cd go && go test -count=1 ./openai/chatcompletions`

Expected: PASS, including Task 2 CC stream vectors via `TestVectors`.

- [ ] **Step 7: Commit CC implementation and direct tests**

```bash
git add go/openai/chatcompletions/types.go go/openai/chatcompletions/streamin.go \
  go/openai/chatcompletions/streamout.go go/openai/chatcompletions/streaming_test.go
git commit -m "feat(go): convert streamed Chat Completions tool arguments"
```

### Task 6: Decode and encode Responses streamed function calls

**Input range:** `spec/11-mapping-openai-responses.md`, Task 1 norms, `go/openai/responses/{types.go,streamin.go,streamout.go,streaming_test.go}`, and RE vectors.

**Output artifact:** M7 Responses `function_call` lifecycle support with optional argument-done validation, output-item encoder state machine, direct tests, and fuzz target.

**Completion criteria:** A native function-call’s initial arguments and deltas replay exactly on item completion; optional done validates rather than duplicates; output lifecycle remains loss-contained; encoder creates ordered output items across text → tool → text.

**Verification:** `cd go && go test -count=1 ./openai/responses` and `cd go && go test -count=1 -run '^FuzzStreamFunctionCallArguments$' ./openai/responses`.

**Timeout/cancellation:** 120 seconds. Stop if `function_call_output` starts emitting IR blocks; it remains out of scope and must have one absorbed loss.

**Cleanup:** Remove noncanonical fuzz crash artifacts; retain only reviewed deterministic seeds.

**Files:**
- Modify: `go/openai/responses/types.go`
- Modify: `go/openai/responses/streamin.go`
- Modify: `go/openai/responses/streamout.go`
- Modify: `go/openai/responses/streaming_test.go`

**Interfaces:**
- Consumes: `StreamEvent.MarshalJSON`, `NewStreamDecoder`/`Feed`/`Flush`/`Losses`, `NewStreamEncoder`/`Apply`.
- Produces: `StreamEvent` field support and canonical envelopes for `response.function_call_arguments.delta` and `.done`; one serial output-item encoder state machine.

- [ ] **Step 1: Add failing RE decoder and wire-codec tests**

Test `StreamEvent` JSON round-trips for both argument event types. The delta event must preserve `item_id`, `output_index`, and `delta`; done must preserve `item_id`, `output_index`, `call_id`, `name`, and `arguments`.

Add a decoder test for a function-call item added with initial arguments `""`, deltas including `""`, a valid done, item done, and completed response. Assert IR replay occurs at item done, no duplicate final delta exists, and exact raw equality holds. Add failures for mismatched item ID/index/call ID/name/final arguments and an unclosed active call at terminal response. Add a `function_call_output` unit plus descendants test asserting exactly one loss and no tool block.

- [ ] **Step 2: Run RE tests before implementation**

Run: `cd go && go test -count=1 ./openai/responses -run '^TestStream.*FunctionCall'`

Expected: FAIL because current M6 skips `function_call` and lacks canonical argument event fields.

- [ ] **Step 3: Extend `StreamEvent` and `MarshalJSON`**

Add `CallID`, `Name`, and `Arguments` string fields to `StreamEvent` with appropriate `json` tags. Add explicit `MarshalJSON` cases for:

```text
response.function_call_arguments.delta
response.function_call_arguments.done
```

Use distinct structs for canonical required fields so native envelope output does not rely on accidental `omitempty`. Maintain existing event cases byte-structure compatible.

- [ ] **Step 4: Implement retained function-call decoding**

Track the active native output item type and identifiers. On `response.output_item.added(function_call)`, require an open response, retain `item.ID`, `output_index`, `call_id`, and `name`, and append `item.Arguments` as the first fragment even when empty. On each arguments delta, require active retained item identity and append `Delta`. On optional arguments done, require exact identity/call/name/final joined text and produce no IR event. On matching item done, replay ToolUse start, every input delta, and stop using the next compact IR index.

Keep current assistant-message text behavior. Preserve `function_call_output` and unknown types as one loss at item start plus absorbed descendants/closure. At terminal response, error if any retained native item remains open.

- [ ] **Step 5: Add failing RE encoder tests and implement serial output-item state**

Test text → tool → text output exact event order and terminal response output ordering. Test one tool with zero IR deltas synthesizes a full argument delta before done. Test tool aggregate mismatch and mismatched delta type/index errors. Test function-call item identifiers are deterministic and reuse `ToolUseBlock.ID` as `call_id`.

Replace the single-message-only encoder state with an active item record (`message` or `function_call`) and a slice of completed `OutputItem`. Close an active message item before starting a tool; create a `function_call` item with deterministic ID, name/call ID, and initial empty arguments; emit argument deltas; on tool stop validate or synthesize, emit arguments-done then item-done, append completed item; reopen a fresh message for later text. Require all blocks closed before terminal response and include completed items in its `Response.Output`.

- [ ] **Step 6: Add and run RE fuzzing**

Add `FuzzStreamFunctionCallArguments` using arbitrary rune-safe fragment splits and seeds for empty, escaped, Unicode, whitespace, nested, and numeric-spelling arguments. Decode an otherwise valid function-call lifecycle, unwrap output tokens, and assert exact joined fragments equal ToolUse input.

Run: `cd go && go test -count=1 ./openai/responses`

Expected: PASS, including all RE stream vectors and existing M6 regressions.

- [ ] **Step 7: Commit RE implementation and direct tests**

```bash
git add go/openai/responses/types.go go/openai/responses/streamin.go \
  go/openai/responses/streamout.go go/openai/responses/streaming_test.go
git commit -m "feat(go): convert streamed Responses function arguments"
```

### Task 7: Decode and encode Anthropic streamed tool use

**Input range:** `spec/12-mapping-anthropic-messages.md`, Task 1 norms, `go/anthropic/messages/{types.go,streamin.go,streamout.go,streaming_test.go}`, and AN vectors.

**Output artifact:** M7 AN tool-use aggregation / start-input fallback, tool-use encoder lifecycle, direct tests, and fuzz target.

**Completion criteria:** AN partial JSON fragments are delayed until block stop then replayed exactly; `input:{}` start is ignored when real partial deltas exist; a non-delta start input becomes one synthetic IR delta; unsupported native blocks still have loss containment.

**Verification:** `cd go && go test -count=1 ./anthropic/messages` and `cd go && go test -count=1 -run '^FuzzStreamToolUseArguments$' ./anthropic/messages`.

**Timeout/cancellation:** 120 seconds. Stop if partial JSON is passed to `requireJSONObject` or otherwise structurally inspected; only start input conversion may unwrap/wrap its outer representation.

**Cleanup:** Retain only deterministic test seeds; no generated fuzz corpus artifacts.

**Files:**
- Modify: `go/anthropic/messages/streamin.go`
- Modify: `go/anthropic/messages/streamout.go`
- Modify: `go/anthropic/messages/streaming_test.go`

**Interfaces:**
- Consumes: existing `BlockWire.Input json.RawMessage`, `StreamDelta.PartialJSON`, existing nonstream `inputToIRString` / `inputFromIRString` outer-envelope helpers where applicable.
- Produces: decoder support for `tool_use` and `input_json_delta`; encoder support for `ToolUseBlock` and `InputJSONDelta`.

- [ ] **Step 1: Add failing AN decoder tests**

Test a native `tool_use` start with `input:{}`, partial-json deltas including empty, block stop, message delta, and message stop. Assert no IR tool event until native block stop and exact raw fragment/input equality afterward.

Test a start with raw `input:{"x":1e+01}` and no partial delta. Assert exactly one synthetic `InputJSONDelta` contains the complete raw start input. Add errors for missing tool ID/name, wrong native index, partial-json delta on a text/unknown block, and open tool at message stop. Confirm `server_tool_use` plus descendants creates one unsupported loss and never changes IR index allocation.

- [ ] **Step 2: Run AN tests before implementation**

Run: `cd go && go test -count=1 ./anthropic/messages -run '^TestStreamDecoder.*Tool'`

Expected: FAIL because M6 marks tool-use and input-json deltas unsupported.

- [ ] **Step 3: Implement retained tool-use decode state**

On `content_block_start(type:"tool_use")`, require ID/name, retain native index and raw start input, and do not emit an IR block. On a matching `input_json_delta`, append `PartialJSON` exactly, including empty; do not use the start input. On matching block stop, choose final input/fragments:

```text
has partial fragments: final input = exact fragment join; replay original fragments
no partial fragments: final input = raw native start input; replay one synthetic fragment
```

Wrap final input as only an IR outer string token. Emit a new compact IR tool block atomically. Preserve existing text and unknown-unit lifecycle/compaction behavior, and require no retained units at terminal completion.

- [ ] **Step 4: Add failing AN encoder tests and implement tool block output**

Test tool start emits `content_block_start` with canonical `input:{}`; every InputJSONDelta maps one-for-one to `content_block_delta(type:"input_json_delta")`; stop verifies exact equality then emits `content_block_stop`. Test zero input deltas synthesize exactly one full native partial-json delta. Test `TextDelta` on tool and `InputJSONDelta` on text fail.

Extend `streamout.go` active-block state to remember ToolUse input and fragments. Use `inputFromIRString` only for nonstream helpers if it parses an outer IR string token; never use it to parse partial fragments. For native tool start, always output literal empty object `json.RawMessage("{}")` regardless of final opaque input.

- [ ] **Step 5: Add and run AN fuzzing**

Add `FuzzStreamToolUseArguments` that feeds tool-use starts with `{}` and rune-safe partial-json fragment splits. Seed empty, escaping, Unicode, whitespace, `1e+01`, and nested values. At `content_block_stop`, unwrap IR string tokens and assert fragment join equals final ToolUse input.

Run: `cd go && go test -count=1 ./anthropic/messages`

Expected: PASS, including all AN stream vectors and existing skipped-index regression tests.

- [ ] **Step 6: Commit AN implementation and direct tests**

```bash
git add go/anthropic/messages/streamin.go go/anthropic/messages/streamout.go \
  go/anthropic/messages/streaming_test.go
git commit -m "feat(go): convert streamed Anthropic tool arguments"
```

### Task 8: Execute cross-face M7 gates and record final vector manifest

**Input range:** Every change in Tasks 1–7, `Makefile`, and generated manifest.

**Output artifact:** Green M7 verification results and final generated `vectors/manifest.json` committed separately if implementation did not alter vectors after Task 2.

**Completion criteria:** Formatting, lint, tests, vector checks, module-path checks, and uncached Go test/vet all pass; no M6 placeholder text remains in M7 paths; working tree is clean after commits.

**Verification:** All listed commands must pass. GitHub Actions Linux `-race` is the authoritative race result if the local environment reports the known ThreadSanitizer VMA limitation.

**Timeout/cancellation:** 120 seconds for each Make/Go check; 600 seconds for full `go test -count=1 ./...`. Stop on the first failing command, retain its output, correct only the implicated work unit, and restart its focused suite before rerunning the full gate.

**Cleanup:** Remove any Go fuzz crash files not intentionally committed as testdata; confirm no `.partial`, coverage, or background task artifacts are left.

**Files:**
- Modify if regenerated: `vectors/manifest.json`
- Test: all `go/**/*_test.go`, `vectors/`, and repository Make targets

**Interfaces:**
- Consumes: all Tasks 1–7.
- Produces: M7 completion evidence only; it must not begin M8 documentation, cross-protocol vectors, release preparation, or unsupported feature work.

- [ ] **Step 1: Add explicit regressions for formerly M6-only unsupported labels**

Search `go/openai/chatcompletions`, `go/openai/responses`, and `go/anthropic/messages` for `M6`, `streaming tool`, `tool_calls`, `function_call`, and `input_json_delta`. Replace only stale M6 unsupported assertions for the three M7 supported units. Keep explicit M7 out-of-scope RE `function_call_output` and AN/CC unsupported semantic behavior covered by focused tests.

- [ ] **Step 2: Refresh generated manifest after final vector content is stable**

Run:

```bash
cd go && go run ./cmd/veccheck -root .. -write-manifest
go run ./cmd/veccheck -root .. -check-manifest
```

Expected: PASS and no hand-edited hash/order drift.

- [ ] **Step 3: Run focused package suites and fuzz seed execution**

Run:

```bash
cd go && go test -count=1 ./cmd/veccheck ./internal/vectest
go test -count=1 ./openai/chatcompletions ./openai/responses ./anthropic/messages
go test -count=1 -run '^FuzzStream' ./openai/chatcompletions ./openai/responses ./anthropic/messages
```

Expected: PASS.

- [ ] **Step 4: Run repository gates**

Run:

```bash
make fmt
make lint
make test
make vectors
make check-modulepath
cd go && go test -count=1 ./...
go vet ./...
go run ./cmd/veccheck -root .. -check-manifest
```

Expected: every command exits zero. If local race tests fail only with `ThreadSanitizer: unsupported VMA range`, record that environmental limitation verbatim and rely on GitHub Actions for `-race`; all non-race commands remain mandatory locally.

- [ ] **Step 5: Review final diff and commit any regenerated manifest separately**

Run:

```bash
git diff --check
git status --short
git diff -- vectors/manifest.json
```

Expected: no whitespace errors; only expected generated manifest change remains, if any. If it changed after the Task 2 vectors commit, commit it separately:

```bash
git add vectors/manifest.json
git commit -m "chore(vectors): refresh manifest"
```

Do not create an empty commit.

## Plan Self-Review

### Specification coverage

- Design sections 1–3 (goals, buffering decision, raw fidelity): Tasks 1, 5, 6, and 7 specify delayed replay, opaque outer-string handling, empty fragments, and fuzz properties.
- Design section 4 (IR grammar, structural versus loss boundary, CC degraded loss): Tasks 1 and 3 establish normative/checker rules; Tasks 5–7 establish face-level errors/losses and CC normalization behavior.
- Design section 5 (CC typed deltas, index interleaving, decode/encode): Task 5 with source-of-truth CC vectors in Task 2.
- Design section 6 (RE lifecycle, optional done, output-item state, excluded output): Task 6 and three RE vectors in Task 2.
- Design section 7 (AN partial JSON and fallback): Task 7 and AN vector matrix in Task 2.
- Design section 8 (schema, parsed event shapes, manual checks, generic runner): Tasks 1–4.
- Design section 9 (both-direction matrix, raw fidelity, fuzz): Tasks 2 and 5–7.
- Design section 10 (source order, narrow commits, final gate): all task ordering and Task 8.

No M8 item (cross-protocol vectors, public README alignment, examples, or release work) is included.

### Placeholder scan

The plan contains no `TBD`, `TODO`, “implement later”, “fill in”, or unbounded “handle edge cases” instructions. Every work unit identifies inputs, output, completion condition, verification, timeout/cancellation, cleanup, files, interfaces, concrete test cases, implementation state, and commit command.

### Type consistency review

- The generic runner uses exactly `StreamConverter`, `DecodeNativeEvent`, `FlushDecoder`, `DecoderLosses`, `ApplyIREvent`, and `RunStream` throughout Tasks 4–8.
- All directions use existing `ir.UnmarshalEventStream` / `ir.MarshalEventStream` and `ir.UnmarshalEvent` / `ir.MarshalEvent` names.
- Tool fragments use `ir.InputJSONDelta` and tool blocks use `ir.ToolUseBlock` consistently.
- CC wire additions are `ToolCallDelta` and `FunctionDelta`; RE native stream data remains `StreamEvent`; AN uses its existing `StreamEvent`, `BlockWire`, and `StreamDelta` shapes.
- Manual checker helper is consistently named `validateEventStream` and only validates the stream IR side selected by vector conversion direction.
