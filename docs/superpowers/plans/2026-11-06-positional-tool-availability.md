# Positional Tool Availability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement mid-conversation positional tool availability (`tool_addition`, `tool_removal`, `compaction.tool_changes`, Responses `additional_tools`) across IR 0.3.0 and all 5 language spokes (Go, TypeScript, Python, Rust, C++) under specification 3.0.0.

**Architecture:** Extend IR Request with ordered `tool_change_groups` pointing to normalized message/content boundaries and a first-block `compaction` block carrying `tool_changes`. Anthropic decodes mixed system blocks into groups and encodes groups at valid system boundaries as system messages; Responses decodes `additional_tools` into groups at exact input positions without hoisting to top-level `Tools`; Chat Completions records bounded `unsupported-semantic` losses per change without mutating request `tools`.

**Tech Stack:** Go 1.23+, TypeScript (Node 24+), Python 3.10+, Rust (edition 2021), C++20 (CMake), JSON Schema draft 2020-12.

**Spec:** `docs/superpowers/specs/2026-11-06-positional-tool-availability-design.md`

## Global Constraints

- Specification version is **3.0.0**; IR contract version (`specVersion`) is **0.3.0**; implementation package version is **v3.0.0** across all 5 languages.
- `Request.Tools` represents only tools available at conversation start; mid-conversation changes MUST NOT be appended or hoisted to `Request.Tools`.
- Message roles remain strictly `user` | `assistant` in IR. No system or developer role is added to `Message`.
- `compaction` block is only permitted as the first block of `messages[0]` in request history and MUST NOT appear in system messages; empty string `content` is a structural error; `content: null` is preserved as failed compaction.
- Responses `additional_tools` requires `role: "developer"` and an array `tools`; empty `tools: []` is preserved as an empty group without losses and does not split assistant runs.
- Cross-face encoding to Chat Completions records one `unsupported-semantic` loss per change/part and does not mutate `tools`.
- Cross-face encoding to Anthropic records `unsupported-semantic` losses for groups placed on invalid boundaries without shifting group positions.
- All golden vectors and all 5 language test suites MUST pass cleanly in CI.

## Review Focus

1. `compaction` with `content: null`: Decodes to a compaction block with null content and roundtrips without becoming omitted or empty string.
2. `additional_tools` placed between two `function_call` items in a single Responses assistant run: Merges into one IR assistant message with `content_index` positioned between the two `ToolUseBlock`s.
3. Anthropic system message with both text and `tool_addition`: Preserves both parts in original order within `tool_change_groups` without hoisting text to `Request.System`.
4. Empty `additional_tools: []`: Decodes to a `ToolChangeGroup` with empty `parts`, emitting no `unmapped-field` loss, and serializes back to `{"type": "additional_tools", "role": "developer", "tools": []}`.
5. Invalid Anthropic group placement (e.g. inside an assistant message or following a system message): Anthropic encoder drops each part with `unsupported-semantic` loss rather than moving it to a nearby boundary.

---

### Task 1: Specification & Schema Updates

**Files:**
- Modify: `spec/schema/ir.schema.json`
- Modify: `spec/01-intermediate-representation.md`
- Modify: `spec/02-loss-policy.md`
- Modify: `spec/10-mapping-openai-chat-completions.md`
- Modify: `spec/11-mapping-openai-responses.md`
- Modify: `spec/12-mapping-anthropic-messages.md`
- Modify: `spec/README.md`
- Modify: `spec/CHANGELOG.md`

**Interfaces:**
- Produces: `specVersion: "0.3.0"` enum in `ir.schema.json`, schema definition for `compaction` block, schema definition for `tool_change_groups`, updated mapping rules N-R-15, N-AN-15, N-AN-22.

- [ ] **Step 1: Write failing schema validation test for IR 0.3.0 and compaction/tool_change_groups**

In `go/cmd/veccheck/check_test.go`:
```go
func TestIRSchema030(t *testing.T) {
    doc := `{"specVersion":"0.3.0","model":"m","messages":[{"role":"user","content":[{"type":"compaction","content":"summary","tool_changes":[{"op":"remove","tool":{"type":"tool_reference","name":"calc"}}]}]},{"role":"assistant","content":[{"type":"text","text":"ok"}]}],"tool_change_groups":[{"message_index":1,"content_index":-1,"parts":[{"op":"add","tool":{"type":"tool_definition","definition":{"name":"calc","input_schema":{"type":"object"}}}}]}]}`
    // validate against ir.schema.json
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go && go test -count=1 ./cmd/veccheck -run TestIRSchema030`
Expected: FAIL with schema validation error (0.3.0 not permitted or unknown properties).

- [ ] **Step 3: Update `spec/schema/ir.schema.json` and specification markdown files**

Add `"0.3.0"` to `specVersion` enums in `spec/schema/ir.schema.json`.
Add `compaction` to `Block` definitions:
```json
{
  "type": "object",
  "required": ["type"],
  "properties": {
    "type": { "const": "compaction" },
    "content": { "type": ["string", "null"] },
    "encrypted_content": { "type": "string" },
    "signature": { "type": "string" },
    "tool_changes": {
      "type": "array",
      "items": { "$ref": "#/$defs/ToolChange" }
    },
    "native": { "type": "array", "items": { "$ref": "#/$defs/NativeAnnotation" } }
  }
}
```
Add `tool_change_groups` to `Request` properties in `ir.schema.json`.
Update `spec/01-intermediate-representation.md` for `CompactionBlock` and `ToolChangeGroup`.
Update `spec/02-loss-policy.md` loss directory table with the 7 new loss details.
Update `spec/11-mapping-openai-responses.md` N-R-15 to remove hoisting and define positional `additional_tools`.
Update `spec/12-mapping-anthropic-messages.md` N-AN-15 to preserve mixed system messages as groups and define N-AN-22 for compaction and tool changes.
Update `spec/README.md` and `spec/CHANGELOG.md` to version 3.0.0.

- [ ] **Step 4: Run veccheck schema test to verify it passes**

Run: `cd go && go test -count=1 ./cmd/veccheck -run TestIRSchema030`
Expected: PASS

- [ ] **Step 5: Commit specification and schema updates**

```bash
git add spec/
git commit -m "spec: bump to 3.0.0 and IR 0.3.0 with positional tool availability"
```

---

### Task 2: Golden Vectors & Manifest

**Files:**
- Modify: `vectors/responses/nonstream/tool-additional-tools-to-ir.json`
- Modify: `vectors/responses/nonstream/tool-additional-tools-empty-to-ir.json`
- Modify: `vectors/responses/nonstream/tool-additional-tools-transparent-runs-to-ir.json`
- Modify: `vectors/responses/nonstream/tool-additional-tools-nested-strict-to-ir.json`
- Modify: `vectors/cross/nonstream/responses-to-chatcompletions-additional-tools-request.json`
- Modify: `vectors/cross/nonstream/responses-to-anthropic-additional-tools-request.json`
- Create: `vectors/anthropic/nonstream/tool-addition-to-ir.json`
- Create: `vectors/anthropic/nonstream/tool-addition-from-ir.json`
- Create: `vectors/anthropic/nonstream/tool-removal-to-ir.json`
- Create: `vectors/anthropic/nonstream/tool-removal-from-ir.json`
- Create: `vectors/anthropic/nonstream/compaction-roundtrip-to-ir.json`
- Create: `vectors/anthropic/nonstream/compaction-roundtrip-from-ir.json`
- Create: `vectors/cross/nonstream/anthropic-to-chatcompletions-tool-change-request.json`
- Create: `vectors/cross/nonstream/anthropic-to-responses-tool-change-request.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Consumes: `spec/schema/ir.schema.json` 0.3.0
- Produces: 556 validated vectors with updated `manifest.json`.

- [ ] **Step 1: Write and update golden vector JSON files**

Update existing 4 Responses vectors and 2 cross vectors to reflect non-hoisting in `expected_ir`:
`expected_ir.tools` stays empty; `expected_ir.tool_change_groups` captures the item.
Create new vectors covering:
- Anthropic mid-conversation system `tool_addition` (definition and reference) to/from IR.
- Anthropic mid-conversation system `tool_removal` (name, server_name) to/from IR.
- Anthropic `compaction` block with `content`, `encrypted_content`, `signature`, and `tool_changes` to/from IR.
- Cross-protocol Anthropic -> Chat Completions (emits `unsupported-semantic` loss for tool change without mutating tools).
- Cross-protocol Anthropic -> Responses (emits `additional_tools` for definition additions, losses for removals).

- [ ] **Step 2: Run veccheck to regenerate manifest and validate**

Run: `cd go && go run ./cmd/veccheck -root .. -write-manifest`
Expected: Writes updated `vectors/manifest.json`.
Run: `cd go && go run ./cmd/veccheck -root .. -check-manifest`
Expected: PASS with 0 schema or manifest errors.

- [ ] **Step 3: Commit vectors and manifest**

```bash
git add vectors/
git commit -m "vectors: add positional tool availability vectors and update manifest"
```

---

### Task 3: Go Implementation

**Files:**
- Modify: `go/ir/block.go`
- Modify: `go/ir/request.go`
- Modify: `go/ir/json.go`
- Modify: `go/ir/codec.go`
- Modify: `go/anthropic/messages/types.go`
- Modify: `go/anthropic/messages/decode.go`
- Modify: `go/anthropic/messages/encode.go`
- Modify: `go/anthropic/messages/rawjson.go`
- Modify: `go/openai/responses/types.go`
- Modify: `go/openai/responses/decode.go`
- Modify: `go/openai/responses/encode.go`
- Modify: `go/openai/chatcompletions/encode.go`
- Test: `go/anthropic/messages/tool_change_test.go`
- Test: `go/openai/responses/additional_tools_test.go`

**Interfaces:**
- Produces in `go/ir`:
```go
type CompactionBlock struct {
    Content          *string
    EncryptedContent string
    Signature        string
    ToolChanges      []ToolChange
    Native           []NativeAnnotation
    Extra            map[string]json.RawMessage
}

type ToolChangeGroup struct {
    MessageIndex int
    ContentIndex int
    ID           string
    Parts        []ToolChangePart
    Extra        map[string]json.RawMessage
}

type ToolChangePart struct {
    Type   string // "text" | "tool_change"
    Text   string
    Change *ToolChange
}

type ToolChange struct {
    Op     string // "add" | "remove"
    Tool   *Tool
    Ref    *ToolChangeRef
    Native []NativeAnnotation
    Extra  map[string]json.RawMessage
}

type ToolChangeRef struct {
    Type       string // "tool_reference" | "mcp_tool_reference" | "mcp_toolset_reference"
    Name       string
    ServerName string
}
```

- [ ] **Step 1: Write failing Go tests in `go/anthropic/messages` and `go/openai/responses`**

Tests covering:
- `TestAnthropicToolAdditionAndRemovalDecodeEncode`
- `TestAnthropicCompactionRoundtrip`
- `TestResponsesAdditionalToolsPositionPreserved`
- `TestResponsesEmptyAdditionalToolsNoLoss`
- `TestChatCompletionsPositionalLossesNoToolMutation`

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd go && go test -count=1 ./anthropic/messages ./openai/responses ./openai/chatcompletions`
Expected: FAIL (compilation error or assertion failures).

- [ ] **Step 3: Implement IR types and json encoding/decoding in `go/ir`**

Update `go/ir/block.go`, `go/ir/request.go`, `go/ir/json.go`, `go/ir/codec.go` to handle `CompactionBlock`, `ToolChangeGroup`, `ToolChangePart`, `ToolChange`, `ToolChangeRef` and support IR version `0.3.0`.

- [ ] **Step 4: Implement Anthropic Messages decode & encode**

In `go/anthropic/messages/decode.go`:
- Accept `tool_addition` and `tool_removal` in mid-conversation system messages.
- Accept `compaction` block as `messages[0].content[0]`.
- Populate `req.ToolChangeGroups` for system messages containing tool changes.
In `go/anthropic/messages/encode.go`:
- Emit `compaction` blocks verbatim.
- Emit `tool_change_groups` at valid system message boundaries as `role: "system"` messages.
- Emit `unsupported-semantic` loss for groups at invalid boundaries.

- [ ] **Step 5: Implement OpenAI Responses decode & encode**

In `go/openai/responses/decode.go`:
- Do not hoist `additional_tools` to `req.Tools`.
- Construct `ToolChangeGroup` at the current `MessageIndex` and `ContentIndex`.
- Support empty `tools: []` without loss.
In `go/openai/responses/encode.go`:
- Render `ToolChangeGroup` back to `type: "additional_tools"` item at the appropriate position.
- Emit losses for removal operations and unsupported tool refs.

- [ ] **Step 6: Implement OpenAI Chat Completions encode**

In `go/openai/chatcompletions/encode.go`:
- Do not mutate request `tools`.
- For each `ToolChangeGroup`, emit `unsupported-semantic` loss per part (or group if empty).
- For `CompactionBlock`, emit `unsupported-semantic` loss.

- [ ] **Step 7: Run Go unit and vector tests to verify they pass**

Run: `cd go && go test -count=1 ./...`
Expected: PASS across all packages.

- [ ] **Step 8: Commit Go implementation**

```bash
git add go/
git commit -m "go: implement positional tool availability in v3.0.0"
```

---

### Task 4: TypeScript Implementation

**Files:**
- Modify: `ts/src/ir/index.ts`
- Modify: `ts/src/ir/codec.ts`
- Modify: `ts/src/anthropic/messages.ts`
- Modify: `ts/src/responses/index.ts`
- Modify: `ts/src/chatcompletions/index.ts`
- Modify: `ts/package.json`
- Test: `ts/test/tool-change.test.ts`
- Modify: `ts/test/responses-additional-tools.test.ts`

**Interfaces:**
- Produces: TypeScript interfaces `CompactionBlock`, `ToolChangeGroup`, `ToolChangePart`, `ToolChange`, `ToolChangeRef` in `@elkpi/oxa`.

- [ ] **Step 1: Write failing TypeScript tests**

Add `ts/test/tool-change.test.ts` testing Anthropic tool changes and Responses `additional_tools` positioning.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ts && npm test`
Expected: FAIL.

- [ ] **Step 3: Implement IR types and codecs in `ts/src/ir`**

Update `types.ts`, `codec.ts`, and bump package version to `3.0.0`.

- [ ] **Step 4: Implement Anthropic, Responses, and ChatCompletions spokes in `ts/src`**

Port the decode/encode changes matching Go logic and loss details.

- [ ] **Step 5: Run tests to verify TypeScript passes**

Run: `cd ts && npm test`
Expected: PASS (all unit & vector tests).

- [ ] **Step 6: Commit TypeScript implementation**

```bash
git add ts/
git commit -m "ts: implement positional tool availability in v3.0.0"
```

---

### Task 5: Python Implementation

**Files:**
- Modify: `python/src/oxa/ir/types.py`
- Modify: `python/src/oxa/ir/json_codec.py`
- Modify: `python/src/oxa/anthropic/messages.py`
- Modify: `python/src/oxa/openai/responses.py`
- Modify: `python/src/oxa/openai/chatcompletions.py`
- Modify: `python/pyproject.toml`
- Modify: `python/tests/test_anthropic_vectors.py`
- Test: `python/tests/test_tool_change.py`

**Interfaces:**
- Produces: Python dataclasses `CompactionBlock`, `ToolChangeGroup`, `ToolChangePart`, `ToolChange`, `ToolChangeRef`.

- [ ] **Step 1: Write failing Python tests**

Add `python/tests/test_tool_change.py`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd python && PYTHONPATH=src python3 -m unittest discover -s tests`
Expected: FAIL.

- [ ] **Step 3: Implement Python IR types, spokes, and update vector count**

Port changes to `python/src/oxa/` and update vector count in `test_anthropic_vectors.py`.

- [ ] **Step 4: Run tests to verify Python passes**

Run: `cd python && PYTHONPATH=src python3 -m unittest discover -s tests`
Expected: PASS.

- [ ] **Step 5: Commit Python implementation**

```bash
git add python/
git commit -m "python: implement positional tool availability in v3.0.0"
```

---

### Task 6: Rust Implementation

**Files:**
- Modify: `rust/crates/oxa-ir/src/lib.rs`
- Modify: `rust/crates/oxa-anthropic/src/nonstream.rs`
- Modify: `rust/crates/oxa-responses/src/nonstream.rs`
- Modify: `rust/crates/oxa-chatcompletions/src/nonstream.rs`
- Modify: `rust/crates/*/Cargo.toml`
- Test: `rust/crates/oxa-anthropic/tests/tool_change.rs`

**Interfaces:**
- Produces: Rust structs `CompactionBlock`, `ToolChangeGroup`, `ToolChangePart`, `ToolChange`, `ToolChangeRef` in `oxa_ir`.

- [ ] **Step 1: Write failing Rust test**

Add test in `rust/crates/oxa-anthropic/tests/tool_change.rs`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd rust && cargo test -p oxa-anthropic --test tool_change`
Expected: FAIL.

- [ ] **Step 3: Implement Rust crates and version bump to 3.0.0**

Update `oxa-ir`, `oxa-anthropic`, `oxa-responses`, `oxa-chatcompletions`.

- [ ] **Step 4: Run tests to verify Rust passes**

Run: `cd rust && cargo test`
Expected: PASS across all workspace crates.

- [ ] **Step 5: Commit Rust implementation**

```bash
git add rust/
git commit -m "rust: implement positional tool availability in v3.0.0"
```

---

### Task 7: C++ Implementation

**Files:**
- Modify: `cpp/include/oxa/ir.hpp`
- Modify: `cpp/src/ir.cpp`
- Modify: `cpp/src/anthropic/messages.cpp`
- Modify: `cpp/src/openai/responses.cpp`
- Modify: `cpp/src/openai/chatcompletions.cpp`
- Modify: `cpp/CMakeLists.txt`
- Test: `cpp/tests/test_tool_change.cpp`

**Interfaces:**
- Produces: C++ structs `compaction_block`, `tool_change_group`, `tool_change_part`, `tool_change`, `tool_change_ref`.

- [ ] **Step 1: Write failing C++ test**

Add `cpp/tests/test_tool_change.cpp` and register in `cpp/CMakeLists.txt`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cmake -B cpp/build -S cpp && cmake --build cpp/build --target test_tool_change`
Expected: FAIL.

- [ ] **Step 3: Implement C++ structs and spokes**

Update `ir.hpp`, `ir.cpp`, `anthropic/messages.cpp`, `openai/responses.cpp`, `openai/chatcompletions.cpp`.

- [ ] **Step 4: Run ctest to verify C++ passes**

Run: `ctest --test-dir cpp/build --output-on-failure`
Expected: PASS.

- [ ] **Step 5: Commit C++ implementation**

```bash
git add cpp/
git commit -m "cpp: implement positional tool availability in v3.0.0"
```

---

### Task 8: Verification & Release Polish

**Files:**
- None (runs repo-wide checks)

- [ ] **Step 1: Run repo-wide vector check**

Run: `make vectors` (or `cd go && go run ./cmd/veccheck -root .. -check-manifest`)
Expected: PASS with 0 errors.

- [ ] **Step 2: Run all language test suites**

Run Go, TypeScript, Python, Rust, and C++ test suites.
Expected: 100% passing.

- [ ] **Step 3: Final branch check**

Verify `git status` is clean and diff matches specification 3.0.0.
