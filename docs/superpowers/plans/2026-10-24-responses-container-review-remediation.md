# Responses Container Review Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the verified Responses container/native-tool data-loss, namespace, validation, and cross-face degradation defects in PR #58 consistently across all five implementations.

**Architecture:** Preserve OpenAI native declarations as lossless JSON text at every raw JSON boundary and never convert them through host-language generic JSON serialization. Replace the single container-kind annotation with one source-scoped JSON annotation holding the complete ordered container path, including each level’s kind, name, and description. Normalize namespaces uniformly for request, response, and stream tool calls; use capability-aware native-option loss collection so targets only report annotations they truly omit.

**Tech Stack:** Go 1.23+, TypeScript/Node lossless JSON types, Python standard JSON and dataclasses, Rust serde_json, C++ oxa JSON layer, shared JSON Schema vectors.

**Spec:** `spec/11-mapping-openai-responses.md`, `spec/15-native-request-options.md`, `spec/16-native-tool-declarations.md`, `spec/01-intermediate-representation.md`

## Global Constraints

- Source-of-truth order is specification → vectors → implementation; update `vectors/manifest.json` with `go run ./cmd/veccheck -root .. -write-manifest` after vector changes.
- Preserve provider native declarations as opaque JSON text. Never parse and re-marshal a declaration through a lossy host representation.
- Do not reveal internal team, customer, domain, or issue identifiers in code, vectors, specs, commits, or release documentation.
- No direct protocol-face imports; preserve hub-and-spoke dependency boundaries.
- Keep behavior identical across Go, TypeScript, Python, Rust, and C++.
- Do not delete existing user changes. Keep commits narrowly cherry-pickable.

## Review Focus

- Native declaration values containing exponent-form numbers, large integers, whitespace, and unknown nested fields must round-trip without loss; add shared raw-fidelity vectors in Task 1.
- Two distinct no-name MCP declarations must not collide; add shared vectors with distinct `server_label` values in Task 1.
- Nested `toolbox` → `namespace` container descriptions and types must round-trip exactly; add shared nested-container vectors in Task 1.
- `namespace` on non-stream Responses output and stream lifecycle snapshots must decode to the same dotted IR call name; add non-stream and stream vectors in Task 1.
- Explicit non-boolean `defer_loading` must be a structural error in every language; add failing input vectors/tests in Task 1 and language-level test coverage in Tasks 3–7.

---

### Task 1: Correct the normative contract and shared vectors

**Files:**
- Modify: `spec/11-mapping-openai-responses.md:300-336`
- Modify: `spec/15-native-request-options.md:79-90`
- Modify: `spec/16-native-tool-declarations.md:16-55`
- Modify: `spec/CHANGELOG.md`
- Create/Modify: `vectors/responses/nonstream/*.json`
- Create/Modify: `vectors/responses/stream/*.json`
- Create/Modify: `vectors/cross/nonstream/*.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Consumes: current `ir.Tool.Native` contract and vector schema.
- Produces: one exact cross-language contract for native source text, a complete `openai/container_path` annotation payload, native declaration identity without accidental collision, response/stream namespace normalization, and target-aware `defer_loading` loss behavior.

- [ ] **Step 1: Write failing shared vectors for every verified defect**

Include vectors for:

```json
{
  "type": "mcp",
  "server_label": "repo",
  "server_url": "https://example.invalid/mcp",
  "require_approval": "never"
}
```

and a second MCP declaration with a different `server_label`; native declarations containing `2e+0` and large integers; `web_search_preview_2025_03_11`; nested `toolbox`/`namespace` descriptions; namespace-bearing response and stream calls; malformed `"defer_loading":"true"`; Responses→Anthropic flattened container with a required `degraded` loss while preserving `defer_loading`.

- [ ] **Step 2: Run vector validation to verify the new vectors are initially unsupported or fail against the fixed contract**

Run: `cd go && go run ./cmd/veccheck -root .. -check-manifest`

Expected: schema-valid vectors, but implementation test suites fail until Tasks 2–7 are complete.

- [ ] **Step 3: Update the specs**

Specify:

- `container` annotation `value` is one JSON object with an ordered `path` array of `{type,name,description?}` entries, not a leaf-only type string.
- Each source container description is preserved and nested container encoding reconstructs the complete path.
- Every provider-defined non-container discriminator is a native declaration; preserve multiple no-name declarations without global name collision. For an absent `name`, choose a stable in-memory identity from `server_label` when available, otherwise type; no-name native declarations are not rejected merely because another no-name declaration shares the same discriminator.
- Native raw declaration text is exact on raw JSON paths; typed entry points use canonical lossless JSON serialization.
- `namespace` applies to Responses request items, response items, and streaming item snapshots/lifecycle validation.
- Responses `defer_loading` must be boolean. `false`/absent create no annotation; any other value is structural error.
- Container grouping is degraded only when the target cannot encode it. `defer_loading` is represented by both Responses and Anthropic, so it must not generate an omission loss in either direction.

- [ ] **Step 4: Regenerate and validate the manifest**

Run: `cd go && go run ./cmd/veccheck -root .. -write-manifest && go run ./cmd/veccheck -root .. -check-manifest`

Expected: all vectors and manifest checks pass.

- [ ] **Step 5: Commit**

```bash
git add spec/ vectors/
git commit -m "fix(spec): define lossless Responses native containers and declarations"
```

### Task 2: Align shared IR-native behavior in all language cores

**Files:**
- Modify: `go/ir/native.go`, `go/ir/native_tool.go`, `go/ir/*_test.go`
- Modify: `ts/src/ir/native.ts`, `ts/src/ir/nonstream.ts`, `ts/test/native-*.test.ts`
- Modify: `python/src/oxa/ir/native.py`, `python/src/oxa/ir/codec.py`, `python/tests/test_native_*.py`
- Modify: `rust/crates/oxa-ir/src/native.rs`, `rust/crates/oxa-ir/src/request.rs`, `rust/crates/oxa-ir/tests/*.rs`
- Modify: `cpp/include/oxa/ir.hpp`, `cpp/src/native.cpp`, `cpp/src/ir.cpp`, `cpp/tests/test_native_*.cpp`

**Interfaces:**
- Consumes: Task 1’s revised annotation and native declaration contract.
- Produces: `source: "openai"`, `kind: "container"` and `kind: "defer_loading"` validation/loss collection that exactly matches target capability; native tool identity validation that supports absent declaration names without collision.

- [ ] **Step 1: Add failing core tests per language**

Each test must assert that:

```json
{"type":"mcp","server_label":"repo"}
```

and a second MCP server are accepted together; an OpenAI container annotation uses a JSON `path` object; source `openai` container emits degraded only for non-Responses/non-Anthropic targets; and `defer_loading` emits no loss to Responses or Anthropic.

- [ ] **Step 2: Run the narrow core tests to verify failure**

Run the relevant language core test command before modifying its implementation.

Expected: failure due to current leaf-only container data, generic native source handling, or duplicate no-name identity.

- [ ] **Step 3: Implement the minimal shared IR changes**

- Decode container annotation values as valid JSON objects, validate their ordered path entries, and retain unknown future fields only where the native contract permits.
- Make native declaration identity accept absent payload `name`; do not globally de-duplicate no-name native declarations.
- Make omission collection use `(source, kind, target)` capability rules, including recovery of OpenAI `defer_loading` on Anthropic.

- [ ] **Step 4: Run each language’s core tests**

Expected: all new core tests pass with no regression to existing native request tests.

- [ ] **Step 5: Commit per language or coherent IR change**

```bash
git commit -m "fix(ir): align OpenAI native annotation capability handling"
```

### Task 3: Fix Go Responses raw fidelity, containers, namespace, and cross-face projection

**Files:**
- Modify: `go/openai/responses/rawjson.go`
- Modify: `go/openai/responses/types.go`
- Modify: `go/openai/responses/container.go`
- Modify: `go/openai/responses/decode.go`
- Modify: `go/openai/responses/encode.go`
- Modify: `go/openai/responses/streamin.go`, `go/openai/responses/streamout.go`, related types
- Modify: `go/anthropic/messages/encode.go`, `go/anthropic/messages/request_native.go`
- Test: `go/openai/responses/container_test.go`, `go/openai/responses/*_test.go`, `go/anthropic/messages/request_native_test.go`

**Interfaces:**
- Consumes: Task 1 vectors and Task 2 IR contract.
- Produces: exact raw native declarations in `ir.Tool.Definition`, complete container-path annotation capture/reconstruction, namespace-aware non-stream and stream calls, structural `defer_loading` validation, and accurate Anthropic cross-face losses.

- [ ] **Step 1: Write focused failing Go tests**

Add tests for raw MCP config fidelity, two MCP declarations, nested container path/descriptions, non-stream response namespace, stream namespace snapshots, invalid `defer_loading`, and Responses→Anthropic container/defer behavior.

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `cd go && go test -count=1 ./openai/responses ./anthropic/messages -run 'Test.*(Container|Native|Namespace|Defer)'`

Expected: failure on the reviewed reproduction cases.

- [ ] **Step 3: Implement Go raw fidelity and container reconstruction**

- Add an explicit opaque branch to raw filtering/auditing so native declarations retain their complete raw object instead of reducing to `type`.
- Make `ToolDef` retain source raw text independently from encode-only raw output where necessary.
- Record and consume complete container paths. Only group compatible ordinary/custom children; never absorb native/apply-patch/foreign-source tools solely because their names share a prefix.
- Preserve container descriptions and nesting on same-face encode.

- [ ] **Step 4: Implement namespace and target-aware cross-face behavior**

Normalize `namespace + name` for response output and every function/custom streaming lifecycle identity; split dotted names on corresponding encoders. Validate `defer_loading` before annotation creation. Accept both supported source annotations in Anthropic’s deferred-loading projection while emitting exactly one container degradation loss.

- [ ] **Step 5: Verify Go**

Run:

```bash
cd go
go test -count=1 ./...
go vet ./...
gofmt -l .
```

Expected: no failures and no output from `gofmt -l`.

- [ ] **Step 6: Commit**

```bash
git add go/
git commit -m "fix(go): preserve Responses native tools and container paths"
```

### Task 4: Fix TypeScript lossless JSON and namespace behavior

**Files:**
- Modify: `ts/src/openai/responses/nonstream.ts`
- Modify: `ts/src/openai/responses/stream.ts`
- Modify: `ts/src/openai/responses/audit.ts`
- Modify: `ts/src/anthropic/messages/native.ts`, `ts/src/anthropic/messages/nonstream.ts`
- Modify: `ts/src/ir/native.ts`, `ts/src/ir/nonstream.ts`
- Test: relevant `ts/test/*responses*`, `ts/test/native-request.test.ts`, vectors tests

**Interfaces:**
- Consumes: Task 1/2 contracts.
- Produces: native raw declaration handling exclusively via the project lossless JSON API, response/stream namespace symmetry, and target-aware native annotation losses.

- [ ] **Step 1: Write failing TypeScript tests**

Test encode of a direct native definition containing `2e+0` and a large integer; expect exact numeric tokens, not `{}` or internal `JsonNumber`. Test `EncodeResponse`/`DecodeResponse` for `repo.read`, namespace-bearing raw response calls, stream item snapshots, and nonboolean `defer_loading` rejection.

- [ ] **Step 2: Run focused tests to verify failure**

Run: `cd ts && npm test -- --test-name-pattern='native|namespace|container'`

Expected: failure before implementation.

- [ ] **Step 3: Replace native JSON conversions and finish capability paths**

Use `sourceTextOf`, `parseJson`, and `stringifyJson` rather than `JSON.stringify`/`JSON.parse` for native declarations. Add `namespace` to output auditing and all output/stream decode paths. Update container-path grouping so descriptions and nested containers round-trip and foreign native tools cannot become functions.

- [ ] **Step 4: Verify TypeScript**

Run:

```bash
cd ts
npm run fmt
npm test
```

Expected: Prettier reports no issues; all tests pass.

- [ ] **Step 5: Commit**

```bash
git add ts/
git commit -m "fix(ts): preserve native JSON and namespace response identity"
```

### Task 5: Fix Python Responses fidelity and capability behavior

**Files:**
- Modify: `python/src/oxa/openai/responses/decode.py`
- Modify: `python/src/oxa/openai/responses/encode.py`
- Modify: `python/src/oxa/openai/responses/normalize.py`
- Modify: `python/src/oxa/_wire.py`, `python/src/oxa/_wire_fields.py`
- Modify: `python/src/oxa/anthropic/messages/encode.py`
- Test: `python/tests/test_responses_nonstream.py`, `python/tests/test_native_request.py`, vector tests

**Interfaces:**
- Consumes: Tasks 1–2 contract.
- Produces: raw declaration source retention, complete container paths, type validation, namespace symmetry, and correct Anthropic crossing behavior.

- [ ] **Step 1: Write failing Python tests for all Python-specific reviewed regressions**

Cover malformed `defer_loading`, MCP multiplicity, raw exponent text fidelity on raw entrypoint, nested containers/descriptions, and Responses→Anthropic tool annotations.

- [ ] **Step 2: Run focused tests to verify failure**

Run: `cd python && .venv/bin/pytest tests/test_responses_nonstream.py tests/test_native_request.py -q`

Expected: reviewed regressions fail.

- [ ] **Step 3: Implement contract-compliant Python handling**

Use raw JSON slices where `RawDocument` supplies them; do not canonicalize raw native declaration text. Update audit schemas, container path representation, target-aware loss behavior, and both normal and streaming namespace paths.

- [ ] **Step 4: Verify Python**

Run: `cd python && .venv/bin/pytest`

Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add python/
git commit -m "fix(python): preserve Responses native declarations and containers"
```

### Task 6: Fix Rust Responses fidelity and capability behavior

**Files:**
- Modify: `rust/crates/oxa-responses/src/audit.rs`
- Modify: `rust/crates/oxa-responses/src/types.rs`
- Modify: `rust/crates/oxa-responses/src/decode.rs`
- Modify: `rust/crates/oxa-responses/src/encode.rs`
- Modify: `rust/crates/oxa-responses/src/normalize.rs`
- Modify: `rust/crates/oxa-ir/src/native.rs`, `rust/crates/oxa-ir/src/request.rs`
- Test: `rust/crates/oxa-responses/tests/*.rs`, `rust/crates/oxa-ir/tests/*.rs`

**Interfaces:**
- Consumes: Tasks 1–2 contract.
- Produces: raw `serde_json::value::RawValue` preservation for native tools, non-whitelist provider-defined discriminator handling, target-aware annotations, nested container reconstruction, namespace symmetry.

- [ ] **Step 1: Add failing Rust integration tests**

Include exact raw MCP/file-search configs, `web_search_preview_2025_03_11`, duplicate no-name declarations, nested containers, malformed defer loading, response/stream namespace round-trips, and cross-face losses.

- [ ] **Step 2: Run narrow Rust tests to verify failure**

Run: `cd rust && cargo test -p oxa-responses --test vectors --test nonstream`

Expected: new tests/vector cases fail before implementation.

- [ ] **Step 3: Implement raw/native and container fixes**

Keep native wire tool source text separately from typed fields; `audit::clean_json` must preserve complete native object raw text. Remove provider discriminator whitelists; preserve every non-container discriminator as native. Reconstruct only compatible ordinary/custom container descendants and maintain full path metadata.

- [ ] **Step 4: Verify Rust**

Run:

```bash
cd rust
cargo fmt --all -- --check
cargo test
```

Expected: formatter and full workspace tests pass.

- [ ] **Step 5: Commit**

```bash
git add rust/
git commit -m "fix(rust): preserve Responses native declarations and containers"
```

### Task 7: Fix C++ Responses fidelity and capability behavior

**Files:**
- Modify: `cpp/src/openai/responses.cpp`
- Modify: `cpp/src/openai/responses_audit.hpp`
- Modify: `cpp/src/wire_audit.hpp`
- Modify: `cpp/src/ir.cpp`, `cpp/src/native.cpp`, `cpp/include/oxa/ir.hpp`
- Test: `cpp/tests/test_responses.cpp`, `cpp/tests/test_vectors.cpp`, `cpp/tests/test_native_*.cpp`

**Interfaces:**
- Consumes: Tasks 1–2 contract.
- Produces: exact native source text, complete container path metadata, no discriminator whitelist, namespace behavior in all call directions, and correct annotation degradation.

- [ ] **Step 1: Write failing C++ tests**

Add direct conversion tests for native config preservation, duplicate MCP declarations, malformed defer loading, nested descriptions, namespace response/stream identities, and cross-face `degraded` loss behavior.

- [ ] **Step 2: Build and run focused tests to verify failure**

Run:

```bash
cd cpp
cmake -B build -S .
cmake --build build --target test_responses test_vectors
ctest --test-dir build -R 'test_(responses|vectors)' --output-on-failure
```

Expected: new coverage fails before implementation.

- [ ] **Step 3: Implement C++ contract-compliant handling**

Use the oxa JSON source/serialize APIs without reducing native declarations to type-only objects. Make grouping inspect source/type and tool variant; preserve recursive metadata and descriptions. Extend stream and non-stream namespace normalization and audit profiles.

- [ ] **Step 4: Verify C++ and clean artifacts**

Run:

```bash
cd cpp
cmake -B build -S .
cmake --build build
ctest --test-dir build --output-on-failure
rm -rf build
```

Expected: 100% tests pass; no build artifacts remain.

- [ ] **Step 5: Commit**

```bash
git add cpp/
git commit -m "fix(cpp): preserve Responses native declarations and containers"
```

### Task 8: Cross-language verification and PR update

**Files:**
- Modify only if required: `CHANGELOG.md`, `README.md`, language package version metadata
- No code changes expected outside previous tasks.

**Interfaces:**
- Consumes: all corrected implementations and regenerated vectors.
- Produces: green validation evidence and an updated PR branch.

- [ ] **Step 1: Run all repository checks**

```bash
make vectors
make lint
make fmt
make check-modulepath
cd go && go test -count=1 ./...
cd ../ts && npm run fmt && npm test
cd ../python && .venv/bin/pytest
cd ../rust && cargo fmt --all -- --check && cargo test
cd ../cpp && cmake -B build -S . && cmake --build build && ctest --test-dir build --output-on-failure && rm -rf build
```

Expected: all commands exit 0.

- [ ] **Step 2: Audit privacy and status**

Run: `git log main..HEAD -p | grep -iE 'ocic|ai switch|internal-domain-placeholder'`

Expected: no output except language keywords if an intentionally broad token is used; refine the matcher before claiming privacy compliance.

- [ ] **Step 3: Commit final vector/manifest or documentation synchronization if needed**

```bash
git add vectors/ README.md CHANGELOG.md spec/
git commit -m "test(vectors): cover Responses native fidelity regressions"
```

- [ ] **Step 4: Push the branch and confirm PR checks**

Run:

```bash
git push

gh pr checks 58 --watch
```

Expected: all required checks pass.

## Self-Review

- **Spec coverage:** Tasks 1–2 define and enforce raw declaration fidelity, nested containers, no-name native identity, strict `defer_loading`, stream namespace behavior, and target-aware losses. Tasks 3–7 implement every part in each language. Task 8 validates all integration boundaries.
- **Step scan:** Every task begins with a concrete failing test/vector, then a narrow failing command, a specific implementation change, full verification, and a focused commit.
- **Type consistency:** `NativeAnnotation{source:"openai",kind:"container"}` receives one JSON path-object value in every implementation; native raw text remains a string; target is passed through omission APIs consistently.
- **Review Focus:** The five risky input classes are all assigned to Task 1 vectors and reinforced by language tests in Tasks 3–7.
- **Proportion:** The plan specifies behavior and interfaces but leaves implementation internals to the established language idioms.
