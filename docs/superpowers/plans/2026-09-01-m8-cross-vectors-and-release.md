# M8: Cross-Protocol Vectors and Release Readiness — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lock cross-protocol conversion with 12 `protocol-to-protocol` golden vectors, add a test-only cross runner, and bring the public docs (README, CHANGELOG, release checklist, godoc example) to release-readiness accuracy.

**Architecture:** Cross conversion exists only in the test harness as `source.Decode → IR → target.Encode`; no production face-to-face API is added. Vectors stay the behavioral source of truth; the vector schema already reserves `protocol-to-protocol` and forbids `expected_ir` there.

**Tech Stack:** Go 1.23 (module `github.com/elkpi/oxa/go`), existing `go/internal/vectest` harness, `veccheck` CLI.

**Spec:** `docs/superpowers/specs/2026-09-01-m8-cross-vectors-and-release-readiness-design.md`

## Global Constraints

- Source-of-truth order: specification → golden vectors → implementation; behavior changes land in that order.
- Spoke packages import only stdlib, `ir`, and `modelmap` in production code (enforced by `go/internal/deps`). Test-only imports across spokes are permitted.
- Tool inputs and argument fragments are opaque raw JSON text; never parse and re-marshal their payloads.
- Keep commits narrowly cherry-pickable; spec, vectors, implementation, and docs are separate commits (a test may share a commit only with the implementation/vectors it directly verifies).
- Go 1.23 compatibility: no `t.Chdir` (Go 1.24 only).
- Documentation language: English.
- No Git tag, GitHub Release, or remote publish in this milestone; the release checklist defines preconditions only.
- All work happens in the worktree `/home/ping/work/oxa/.claude/worktrees/m7-stream-tool-aggregation`, branch `worktree-m8-cross-and-release`. Go commands run from `go/`.

---

### Task 1: Spec cross-composition semantics + vectors README cross section

**Files:**
- Modify: `spec/02-loss-policy.md` (insert new §6 after §5 "Streaming"; renumber old §6 → §7, old §7 → §8)
- Modify: `vectors/README.md` (layout `<face>` enumeration + new "Cross vectors" section before "## manifest.json")

**Interfaces:**
- Produces: normative loss-concatenation rule (spec/02 §6) that Task 3's vectors encode and the plan's later tasks reference.

- [ ] **Step 1: Insert new §6 into `spec/02-loss-policy.md`**

Insert immediately after the §5 "Streaming" section (its last paragraph ends with "expected losses live in a separate field of the vector."):

```markdown
## 6. Cross-protocol composition

A `protocol-to-protocol` conversion is the composition of the two
single-face conversions through the IR:

    source wire --Decode--> IR --Encode--> target wire

The reported loss list is the source-decode losses followed by the
target-encode losses, in that order. Loss `path` values remain local to
their own stage — decode losses are rooted at the source wire document,
encode losses at the IR document — and MUST NOT be rewritten with stage
prefixes; the `detail` of each loss SHOULD make its stage clear. The
unordered-set comparison rule used by vectors is unaffected by the
concatenation.
```

Renumber: old `## 6. Rules` becomes `## 7. Rules`; old `## 7. Schema agreement` becomes `## 8. Schema agreement`. Existing cross-references in the file (`§3`, `§5`) point to sections before the insertion point and stay valid.

- [ ] **Step 2: Add the cross section to `vectors/README.md`**

In the "Layout and naming" bullet list, change:

```markdown
- `<face>` is one of `chatcompletions`, `responses`, `anthropic`
```

to:

```markdown
- `<face>` is one of `chatcompletions`, `responses`, `anthropic`, or
  `cross`
```

Insert this section immediately before `## manifest.json`:

```markdown
## Cross vectors

`vectors/cross/nonstream/` holds `protocol-to-protocol` vectors: a source
wire document is decoded to the IR and re-encoded to a target face in one
composition (`source.Decode → IR → target.Encode`). Each vector names its
`source` and `target` protocols; `input` is the source wire document and
`expected_output` is the target wire document. `expected_ir` is forbidden
by the vector schema: the intermediate IR is not part of the cross
contract — the per-face vectors already lock each stage separately.

`expected_losses` is the concatenation of the source-decode losses
followed by the target-encode losses (spec/02 §6); it still compares as
an unordered set keyed on `(path, field, reason)`. Cross vectors are
nonstream only.
```

- [ ] **Step 3: Verify docs consistency**

Run: `cd go && go run ./cmd/veccheck -root .. -schema-only` (or `make vectors`) — must stay green (docs-only change).

- [ ] **Step 4: Commit**

```bash
git add spec/02-loss-policy.md vectors/README.md
git commit -m "docs(spec): cross-protocol composition loss semantics"
```

---

### Task 2: vectest cross runner (TDD)

**Files:**
- Modify: `go/internal/vectest/load.go` (add `Endpoint` type + `Vector.Source`/`Vector.Target`)
- Create: `go/internal/vectest/cross.go`
- Create: `go/internal/vectest/cross_test.go` (fake converters only in this task)

**Interfaces:**
- Consumes: existing `Converter` interface (`run.go:22`), `LoadVectors`, `CompareJSON`, `CompareLosses`, `compareLosses`, `Vector.isRequest`.
- Produces: `func RunCross(t *testing.T, source, target Converter)` — Task 3's real-face binding test and the cross vectors depend on it.

- [ ] **Step 1: Write the failing tests** (`go/internal/vectest/cross_test.go`)

```go
package vectest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/elkpi/oxa/go/ir"
)

// fakeCrossConverter is a programmable Converter for harness tests.
type fakeCrossConverter struct {
	face         string
	decodedReq   *ir.Request
	encodedReq   []byte
	decodedResp  *ir.Response
	encodedResp  []byte
	decodeLosses []ir.Loss
	encodeLosses []ir.Loss
	reqCalls     int
	respCalls    int
}

func (f *fakeCrossConverter) Face() string { return f.face }

func (f *fakeCrossConverter) DecodeRequestWire(json.RawMessage) (*ir.Request, []ir.Loss, error) {
	f.reqCalls++
	return f.decodedReq, f.decodeLosses, nil
}

func (f *fakeCrossConverter) DecodeResponseWire(json.RawMessage) (*ir.Response, []ir.Loss, error) {
	f.respCalls++
	return f.decodedResp, f.decodeLosses, nil
}

func (f *fakeCrossConverter) EncodeRequestIR(*ir.Request) ([]byte, []ir.Loss, error) {
	return f.encodedReq, f.encodeLosses, nil
}

func (f *fakeCrossConverter) EncodeResponseIR(*ir.Response) ([]byte, []ir.Loss, error) {
	return f.encodedResp, f.encodeLosses, nil
}

func writeCrossVector(t *testing.T, root, fileName string, v Vector) {
	t.Helper()
	dir := filepath.Join(root, "vectors", "cross", "nonstream")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make cross vector directory: %v", err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal vector: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), raw, 0o644); err != nil {
		t.Fatalf("write vector: %v", err)
	}
}

func crossTestVector(name, source, target string, request bool) Vector {
	v := Vector{
		Name:       "cross.nonstream." + name,
		Mode:       "nonstream",
		Conversion: "protocol-to-protocol",
		Input:      json.RawMessage(`{"kind":"input"}`),
		ExpectedOut: json.RawMessage(`{"kind":"output"}`),
	}
	v.Source.Protocol = source
	v.Target.Protocol = target
	if request {
		v.Tags = []string{"request"}
	} else {
		v.Tags = []string{"response"}
	}
	return v
}

func TestRunCrossComposesMatchedVectors(t *testing.T) {
	root := setupStreamVectorRepo(t)
	alpha := &fakeCrossConverter{
		face:         "alpha",
		decodedReq:   &ir.Request{Model: "m"},
		encodedReq:   []byte(`{"kind":"output"}`),
		decodedResp:  &ir.Response{Model: "m"},
		encodedResp:  []byte(`{"kind":"output"}`),
		decodeLosses: []ir.Loss{{Path: "in", Field: "f", Reason: ir.LossUnmappedField}},
		encodeLosses: []ir.Loss{{Path: "params", Field: "g", Reason: ir.LossUnmappedField}},
	}
	beta := &fakeCrossConverter{face: "beta"}
	writeCrossVector(t, root, "alpha-to-beta-request.json", crossTestVector("alpha-to-beta-request", "alpha", "beta", true))
	writeCrossVector(t, root, "alpha-to-beta-response.json", crossTestVector("alpha-to-beta-response", "alpha", "beta", false))
	// Mismatched pair must be skipped by RunCross(t, alpha, beta).
	writeCrossVector(t, root, "beta-to-alpha-request.json", crossTestVector("beta-to-alpha-request", "beta", "alpha", true))

	RunCross(t, alpha, beta)

	if alpha.reqCalls != 1 || alpha.respCalls != 1 {
		t.Fatalf("alpha decode calls = (%d, %d), want (1, 1)", alpha.reqCalls, alpha.respCalls)
	}
}

func TestCrossVectorsForFiltersByPair(t *testing.T) {
	vectors := []Vector{
		crossTestVector("a-to-b-request", "alpha", "beta", true),
		crossTestVector("b-to-a-request", "beta", "alpha", true),
		crossTestVector("a-to-c-request", "alpha", "gamma", true),
	}
	alpha := &fakeCrossConverter{face: "alpha"}
	beta := &fakeCrossConverter{face: "beta"}
	matched := crossVectorsFor(alpha, beta, vectors)
	if len(matched) != 1 || matched[0].Name != "cross.nonstream.a-to-b-request" {
		t.Fatalf("crossVectorsFor() = %+v, want exactly a-to-b-request", matched)
	}
	if got := crossVectorsFor(alpha, &fakeCrossConverter{face: "delta"}, vectors); len(got) != 0 {
		t.Fatalf("crossVectorsFor() with unknown target = %+v, want empty", got)
	}
}
```

Note: `setupStreamVectorRepo` (stream_test.go) creates a fake repo root with a `.git` dir and chdirs into it; reuse it unchanged.

- [ ] **Step 2: Watch the tests fail**

Run: `cd go && go test ./internal/vectest -run 'TestRunCross|TestCrossVectorsFor'`
Expected: compile failure — `undefined: RunCross` (and `crossVectorsFor`).

- [ ] **Step 3: Add `Endpoint` to `load.go`**

Add the type and two `Vector` fields:

```go
// Endpoint names one side of a vector (vector.schema.json $defs/endpoint).
type Endpoint struct {
	Protocol string `json:"protocol"`
}
```

and inside `Vector` (after `Conversion`):

```go
	Source         Endpoint        `json:"source"`
	Target         Endpoint        `json:"target"`
```

Existing single-face vectors carry only `source`; the missing `target` unmarshals to the zero value and is ignored by `Run`/`RunStream`.

- [ ] **Step 4: Implement `cross.go`**

```go
package vectest

import (
	"testing"

	"github.com/elkpi/oxa/go/ir"
)

// RunCross executes every nonstream cross-protocol vector whose source and
// target endpoints match the two converters' Face() protocol names. Each
// vector composes source decode -> IR -> target encode; the target wire
// output compares structurally and the reported losses are the decode
// losses followed by the encode losses, compared as an unordered set. A
// matched pair with no vectors fails the test.
func RunCross(t *testing.T, source, target Converter) {
	t.Helper()
	root := FindRepoRoot(".")
	if root == "" {
		t.Skip("repo root not found; vector tests skipped (dependency mode)")
	}
	vectors, err := LoadVectors(root, "cross", "nonstream")
	if err != nil {
		t.Fatalf("load cross vectors: %v", err)
	}
	matched := crossVectorsFor(source, target, vectors)
	if len(matched) == 0 {
		t.Fatalf("no cross vectors found for pair %s -> %s", source.Face(), target.Face())
	}
	for _, v := range matched {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			runCrossVector(t, source, target, v)
		})
	}
	t.Logf("vectest: executed %d cross vectors for %s -> %s", len(matched), source.Face(), target.Face())
}

// crossVectorsFor selects the vectors whose endpoints match the pair.
func crossVectorsFor(source, target Converter, vectors []Vector) []Vector {
	var out []Vector
	for _, v := range vectors {
		if v.Source.Protocol == source.Face() && v.Target.Protocol == target.Face() {
			out = append(out, v)
		}
	}
	return out
}

func runCrossVector(t *testing.T, source, target Converter, v Vector) {
	t.Helper()
	if v.Conversion != "protocol-to-protocol" {
		t.Fatalf("cross vector %s has conversion %q, want protocol-to-protocol", v.Name, v.Conversion)
	}
	var out []byte
	var decodeLosses, encodeLosses []ir.Loss
	var err error
	if v.isRequest() {
		var req *ir.Request
		req, decodeLosses, err = source.DecodeRequestWire(v.Input)
		if err == nil {
			out, encodeLosses, err = target.EncodeRequestIR(req)
		}
	} else {
		var resp *ir.Response
		resp, decodeLosses, err = source.DecodeResponseWire(v.Input)
		if err == nil {
			out, encodeLosses, err = target.EncodeResponseIR(resp)
		}
	}
	if err != nil {
		t.Fatalf("cross conversion failed: %v", err)
	}
	if err := CompareJSON(v.ExpectedOut, out); err != nil {
		t.Errorf("expected_output mismatch: %v\nexpected: %s\nactual:   %s", err, v.ExpectedOut, out)
	}
	losses := append(append([]ir.Loss{}, decodeLosses...), encodeLosses...)
	compareLosses(t, v, losses)
}
```

- [ ] **Step 5: Watch the tests pass, then run the package**

Run: `cd go && go test ./internal/vectest && go vet ./internal/vectest`
Expected: PASS, no vet findings.

- [ ] **Step 6: Commit**

```bash
git add go/internal/vectest/load.go go/internal/vectest/cross.go go/internal/vectest/cross_test.go
git commit -m "feat(go): cross-protocol vector runner"
```

---

### Task 3: Twelve cross vectors + real-face bindings + manifest

**Files:**
- Create: `vectors/cross/nonstream/<src>-to-<tgt>-<request|response>.json` (12 files, names listed in Step 5)
- Modify: `go/internal/vectest/cross_test.go` (append real-face adapters + `TestCrossVectors`)
- Modify: `vectors/manifest.json` (regenerated)

**Interfaces:**
- Consumes: `RunCross` from Task 2; the three faces' `DecodeRequest/EncodeRequest/DecodeResponse/EncodeResponse`; spec/02 §6 loss concatenation.

**Scenario (all request vectors, semantically identical across faces):**

- system prompt: `"You are a helpful weather assistant."`
- turn 1 — user: `"What's the weather in Paris?"`
- turn 2 — assistant tool call: `get_weather`, arguments exactly `{"city":"Paris"}`, call id `call_1` (Chat Completions / Responses) or `toolu_1` (Anthropic)
- turn 3 — tool result: JSON string `{"temp_c":21,"sky":"sunny"}` (Chat Completions `role:"tool"` message with `tool_call_id`; Responses `function_call_output` item; Anthropic `tool_result` block)
- tools: one function `get_weather` with string parameter `city`, description `"Get current weather for a city."`
- params: `temperature: 0.5`, `max_tokens: 512`
- one face-specific unpassable field per source face (loss exercise):
  - Chat Completions request: `"frequency_penalty": 0.5`
  - Responses request: `"previous_response_id": "resp_prev_9"`
  - Anthropic request: `"metadata": {"user_id": "user-99"}`

**Scenario (all response vectors, semantically identical across faces):**

- output: one text block `"Checking the weather now."` followed by one tool call `get_weather` with arguments `{"city":"Paris"}` (same ids as above)
- usage: input 10, output 25
- stop: Chat Completions `finish_reason: "tool_calls"`; Anthropic `stop_reason: "tool_use"`; Responses renders its terminal status per spec/11 (decode its documented equivalent; if spec/11 maps a completed response containing a function_call to a different IR stop reason, follow spec/11 — the cross vector locks the composed behavior, and any resulting unmapped-value loss is expected).

- [ ] **Step 1: Read the mapping tables**

Read `spec/10-mapping-openai-chat-completions.md`, `spec/11-mapping-openai-responses.md`, `spec/12-mapping-anthropic-messages.md` sections for: system/instructions, tools definition, tool calls/results, params (temperature/max_tokens), stop reasons, and each face's loss catalog. Note the exact wire field names each source input must use and the exact envelope defaults each target renders (from-ir defaults in vectors/README.md).

- [ ] **Step 2: Hand-write the six source inputs**

Create `/tmp/oxa-m8-cross/` with: `cc-request.json`, `cc-response.json`, `re-request.json`, `re-response.json`, `an-request.json`, `an-response.json` — the scenario above in each face's native wire shape, matching the field names confirmed in Step 1.

- [ ] **Step 3: Generate actual outputs with a throwaway program**

Create `go/internal/tmpcrossgen/main.go` (temporary, never committed): a small program that, for each of the six ordered pairs and both directions, loads the source input, runs `Decode*` → `Encode*` on the wire structs, and writes to `/tmp/oxa-m8-cross/gen/`: the intermediate IR (`<pair>-<kind>.ir.json`), the target output (`<pair>-<kind>.out.json`), and the concatenated losses (`<pair>-<kind>.losses.json`, decode losses first). Run it with `go run ./internal/tmpcrossgen` from `go/`.

- [ ] **Step 4: Review every generated artifact against the spec**

For each of the 12 generations verify, fixing the source input (not the expectation) if the input itself was wrong:

- the intermediate IR validates against `spec/schema/ir.schema.json` and matches the single-face to-ir vectors' established shapes (system placement, tool result blocks, params, stop reason);
- the target output matches the face's from-ir rendering defaults (envelope synthesis, derived `total_tokens`, Anthropic content as blocks, Responses item ids `msg_abc123`/`fc_abc123`);
- every loss is justified by the source face's loss catalog (or the target's encode-side catalog) with the right reason code; nothing unmapped is silently dropped.

- [ ] **Step 5: Freeze the twelve vectors**

Write the files (name = `cross.nonstream.` + file stem; `spec_version: "0.1.0"`; `mode: "nonstream"`; `conversion: "protocol-to-protocol"`; `source`/`target` endpoints; `tags` beginning with `request` or `response`):

```
vectors/cross/nonstream/chatcompletions-to-anthropic-request.json
vectors/cross/nonstream/chatcompletions-to-anthropic-response.json
vectors/cross/nonstream/chatcompletions-to-responses-request.json
vectors/cross/nonstream/chatcompletions-to-responses-response.json
vectors/cross/nonstream/responses-to-anthropic-request.json
vectors/cross/nonstream/responses-to-anthropic-response.json
vectors/cross/nonstream/responses-to-chatcompletions-request.json
vectors/cross/nonstream/responses-to-chatcompletions-response.json
vectors/cross/nonstream/anthropic-to-chatcompletions-request.json
vectors/cross/nonstream/anthropic-to-chatcompletions-response.json
vectors/cross/nonstream/anthropic-to-responses-request.json
vectors/cross/nonstream/anthropic-to-responses-response.json
```

`input` comes from the Step 2 file; `expected_output` and `expected_losses` from the reviewed Step 3 artifacts.

- [ ] **Step 6: Append the real-face bindings to `cross_test.go`**

Adapters (one per face; each method unmarshals into the face's wire type, then calls the public conversion function — same pattern as the face-local `vectors_test.go` adapters):

```go
type crossChatCompletions struct{}

func (crossChatCompletions) Face() string { return "chatcompletions" }

func (crossChatCompletions) DecodeRequestWire(w json.RawMessage) (*ir.Request, []ir.Loss, error) {
	var wire chatcompletions.Request
	if err := json.Unmarshal(w, &wire); err != nil {
		return nil, nil, err
	}
	return chatcompletions.DecodeRequest(&wire)
}

func (crossChatCompletions) DecodeResponseWire(w json.RawMessage) (*ir.Response, []ir.Loss, error) {
	var wire chatcompletions.Response
	if err := json.Unmarshal(w, &wire); err != nil {
		return nil, nil, err
	}
	return chatcompletions.DecodeResponse(&wire)
}

func (crossChatCompletions) EncodeRequestIR(req *ir.Request) ([]byte, []ir.Loss, error) {
	out, losses, err := chatcompletions.EncodeRequest(req)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(out)
	return raw, losses, err
}

func (crossChatCompletions) EncodeResponseIR(resp *ir.Response) ([]byte, []ir.Loss, error) {
	out, losses, err := chatcompletions.EncodeResponse(resp)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(out)
	return raw, losses, err
}
```

…and the analogous `crossResponses` (package `responses`, types `Request`/`Response`) and `crossAnthropic` (package `messages`) adapters, plus:

```go
// TestCrossVectors runs the six ordered face pairs over the
// cross/nonstream vectors through the real implementations.
func TestCrossVectors(t *testing.T) {
	faces := map[string]Converter{
		"chatcompletions": crossChatCompletions{},
		"responses":       crossResponses{},
		"anthropic":       crossAnthropic{},
	}
	for _, pair := range [][2]string{
		{"chatcompletions", "responses"},
		{"chatcompletions", "anthropic"},
		{"responses", "chatcompletions"},
		{"responses", "anthropic"},
		{"anthropic", "chatcompletions"},
		{"anthropic", "responses"},
	} {
		t.Run(pair[0]+"-to-"+pair[1], func(t *testing.T) {
			RunCross(t, faces[pair[0]], faces[pair[1]])
		})
	}
}
```

(Imports: `encoding/json`, `testing`, the three face packages, and `github.com/elkpi/oxa/go/ir`. If a face's encode entry returns a typed wire pointer whose JSON differs from the vector's expected shape, fix the vector, never the converter — vectors are the source of truth.)

- [ ] **Step 7: Run and sanity-check**

Run: `cd go && go test ./internal/vectest -run TestCrossVectors -count=1`
Expected: PASS for all six pairs. Then corrupt one byte of one `expected_output` value, re-run, confirm the failure names the vector, restore the byte, re-run green (proves the comparison actually bites).

- [ ] **Step 8: Clean up and pin the manifest**

Delete `go/internal/tmpcrossgen/`. Then:

```bash
cd go && go run ./cmd/veccheck -root .. -write-manifest
go run ./cmd/veccheck -root .. -check-manifest
```

Expected: manifest written (12 new vectors), check passes.

- [ ] **Step 9: Commit**

```bash
git add vectors/cross vectors/manifest.json go/internal/vectest/cross_test.go
git commit -m "test(vectors): add cross-protocol nonstream vectors"
```

---

### Task 4: Compile-verified cross-face godoc example

**Files:**
- Create: `go/openai/chatcompletions/example_test.go`

**Interfaces:**
- Consumes: `chatcompletions.Request/DecodeRequest`, `messages.Request/EncodeRequest` (public APIs only).
- Produces: `Example` the README quick start mirrors (Task 5).

- [ ] **Step 1: Write the example**

```go
package chatcompletions_test

import (
	"encoding/json"
	"fmt"
	"log"

	messages "github.com/elkpi/oxa/go/anthropic/messages"
	"github.com/elkpi/oxa/go/openai/chatcompletions"
)

// This example converts a Chat Completions request into an Anthropic
// Messages request by routing through the face-neutral intermediate
// representation. The same two-step pattern composes every pair of
// supported protocols.
func Example() {
	ccReq := &chatcompletions.Request{
		Model: "gpt-4o-mini",
		Messages: []chatcompletions.Message{
			{Role: "user", Content: "Translate 'good morning' to French."},
		},
	}

	irReq, losses, err := chatcompletions.DecodeRequest(ccReq)
	if err != nil {
		log.Fatal(err)
	}

	anReq, encodeLosses, err := messages.EncodeRequest(irReq)
	if err != nil {
		log.Fatal(err)
	}

	losses = append(losses, encodeLosses...)
	out, _ := json.Marshal(anReq)
	fmt.Printf("%s\nlosses: %d\n", out, len(losses))
	// Output:
	// {"model":"gpt-4o-mini","messages":[{"role":"user","content":"Translate 'good morning' to French."}]}
	// losses: 0
}
```

- [ ] **Step 2: Run and correct the pinned output**

Run: `cd go && go test ./openai/chatcompletions -run Example -count=1 -v`
If the marshaled request or loss count differs (e.g. an extra envelope field), the converter is the source of truth: fix the pinned `// Output:` to the actual deterministic output only after verifying the difference is correct per spec/12 (do not weaken the converter or the example).

- [ ] **Step 3: Commit**

```bash
git add go/openai/chatcompletions/example_test.go
git commit -m "feat(go): cross-face conversion example"
```

---

### Task 5: README status, support matrix, quick start

**Files:**
- Modify: `README.md` (rewrite status block, add capabilities matrix + quick start + documentation links; keep the existing "What is oxa?", "What oxa is NOT", "three deliverable layers", "Language matrix", "Directory overview", "Contributing", "License" sections, lightly updated)

- [ ] **Step 1: Rewrite the top of README.md**

Replace the title block and badges with:

```markdown
oxa
===

Protocol conversion between the OpenAI and Anthropic APIs, as pure
in-process libraries.

[![CI](https://github.com/elkpi/oxa/actions/workflows/ci.yml/badge.svg)](https://github.com/elkpi/oxa/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

**Status: early development, pre-v1.** The specification, golden vectors,
and the Go reference implementation are usable today; interfaces may still
change before v1.
```

- [ ] **Step 2: Insert "Current capabilities" and "Quick start"**

After the "three deliverable layers" section, insert:

````markdown
## Current capabilities

The Go reference implementation converts between each protocol face and a
shared intermediate representation (IR):

| Conversion | Nonstream | Streaming |
|------------|-----------|-----------|
| Chat Completions ↔ IR | requests and responses | text events + `tool_calls` argument aggregation |
| Responses ↔ IR | requests and responses | text events + function-call argument aggregation |
| Anthropic Messages ↔ IR | requests and responses | text events + `input_json_delta` aggregation |
| Any face → any face | two-step composition (below); locked by cross vectors | not yet |

Semantic gaps are never silent: every conversion also returns an ordered
loss list describing what could not be carried
([spec/02](spec/02-loss-policy.md)).

## Quick start

Requires Go 1.23+.

```bash
go get github.com/elkpi/oxa/go
```

```go
import (
    messages "github.com/elkpi/oxa/go/anthropic/messages"
    "github.com/elkpi/oxa/go/openai/chatcompletions"
)

// Any face pair composes through the IR in two steps:
irReq, losses, err := chatcompletions.DecodeRequest(ccRequest)
if err != nil { /* structural input error */ }
anRequest, encodeLosses, err := messages.EncodeRequest(irReq)
losses = append(losses, encodeLosses...)
```

A complete, compile-verified version of this example lives at
[`go/openai/chatcompletions/example_test.go`](go/openai/chatcompletions/example_test.go)
(it is also visible in the godoc of that package).
````

- [ ] **Step 3: Update "Directory overview" and add "Documentation"**

In "Directory overview", add a `docs/` line (`docs/       Design docs and the release checklist`). Replace the "Quick start" placeholder section (already replaced above) and add before "Contributing":

```markdown
## Documentation

- [spec/README.md](spec/README.md) — the specification: reading order and
  source-of-truth rules
- [vectors/README.md](vectors/README.md) — golden vectors and the
  normative comparison rules
- [CHANGELOG.md](CHANGELOG.md) — notable changes
- [docs/release-checklist.md](docs/release-checklist.md) — release
  preconditions
```

Also delete the stale badge line `[![Status: pre-alpha](...)]()` and the sentence "**Status: pre-alpha.** Nothing here works yet — see the badges below."

- [ ] **Step 4: Verify links**

Run: `grep -o '](basedir[^)]*)' README.md` is not applicable — instead manually confirm each relative link target exists (`spec/02-loss-policy.md`, `spec/README.md`, `vectors/README.md`, `CHANGELOG.md`, `docs/release-checklist.md`, `go/openai/chatcompletions/example_test.go`, `CONTRIBUTING.md`, `LICENSE`, `NOTICE`). `docs/release-checklist.md` arrives in Task 6 — write Task 6 before pushing, or verify after both tasks.

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "docs: accurate README status, support matrix, quick start"
```

---

### Task 6: CHANGELOG entries + release checklist

**Files:**
- Modify: `CHANGELOG.md`
- Create: `docs/release-checklist.md`

- [ ] **Step 1: Fill `[Unreleased]` in CHANGELOG.md**

Replace the bare `## [Unreleased]` with:

```markdown
## [Unreleased]

### Added

- Written protocol-conversion specification (`spec/`): hub-and-spoke IR,
  loss policy, model handling, per-face mappings for Chat Completions,
  Responses, and Anthropic Messages, and streaming semantics.
- Golden vector set (`vectors/`): per-face nonstream and stream vectors,
  cross-protocol (`protocol-to-protocol`) nonstream vectors, validated by
  `cmd/veccheck` and pinned by `manifest.json`.
- Go reference implementation (`go/`): face-neutral `ir` package; three
  protocol faces (`openai/chatcompletions`, `openai/responses`,
  `anthropic/messages`) with nonstream request/response conversion and
  incremental stream decoders/encoders; opaque SSE byte framing (`sse`);
  optional model-name mapping (`modelmap`).
- Streaming tool-argument aggregation: Chat Completions `tool_calls`
  index aggregation, Responses function-call argument deltas, and
  Anthropic `input_json_delta`, preserving tool inputs as opaque raw JSON.
- Public documentation: project README with support matrix and quick
  start, a compile-verified cross-face godoc example, and a release
  checklist.
```

- [ ] **Step 2: Create `docs/release-checklist.md`**

```markdown
# Release checklist

This document defines the preconditions for tagging and publishing an oxa
release. It does not perform one: tagging, GitHub Releases, and any
announcement are separate, explicitly authorized actions.

## Preconditions (all REQUIRED before a tag)

1. **CI green on the release commit**, including the Go 1.23 and 1.24
   `-race` test jobs, `gofmt`, `go vet`, `golangci-lint`, the vector
   validation job, and the manifest check.
2. **Vectors pinned**: `cd go && go run ./cmd/veccheck -root ..
   -check-manifest` passes with no drift.
3. **Module path final**: `go/go.mod` declares
   `module github.com/elkpi/oxa/go` with no placeholder. CI's tag-only
   `release-guard` job re-checks this at tag time.
4. **Specification frozen**: `spec/README.md` states the release spec
   version; every shipped-scope document is marked `ready`.
5. **Changelog dated**: the `CHANGELOG.md` `[Unreleased]` section is
   renamed to the release version and date (Keep a Changelog format).
6. **README accurate**: status, support matrix, and quick start match
   what is being released.
7. **Tag and notes**: the tag follows `vX.Y.Z`, points at the release
   commit on `main`, and its release notes are generated from the matching
   changelog section.

## Sequence

1. Land the final release-preparation PR (changelog date, spec version if
   needed, README status).
2. Wait for CI on `main`.
3. Tag and publish. Each of these steps is a manual, explicitly
   authorized action — CI and routine PRs never tag or publish.
```

- [ ] **Step 3: Verify**

Run: `cd go && go test ./... -count=1` (full suite still green) and confirm `docs/release-checklist.md` link resolves from README.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md docs/release-checklist.md
git commit -m "docs: changelog entries and release checklist"
```

---

### Task 7: Full verification and PR

- [ ] **Step 1: Full local gate**

From the repo root:

```bash
make fmt && make lint && make test && make vectors && make check-modulepath
cd go && go test -count=1 ./...
go vet ./...
go run ./cmd/veccheck -root .. -check-manifest
```

Expected: all green, `make fmt` lists nothing, manifest check passes.

- [ ] **Step 2: Push and open the PR**

```bash
git push -u origin worktree-m8-cross-and-release
gh pr create --repo elkpi/oxa \
  --base main --head worktree-m8-cross-and-release \
  --title "M8: cross-protocol vectors and release readiness" \
  --body "<summary of the six commits: cross vectors + runner, spec loss-composition section, example, README, CHANGELOG/checklist>"
```

Report the PR URL; merging and any release action remain the user's decision.

---

## Self-review notes

- Spec coverage: design §1 goals map to Tasks 1–6 (vectors §3–4 → Task 3; §5 → Task 1; §6 → Task 2; §7 → Task 4; §8 → Tasks 5–6); Task 7 verifies §9.
- Placeholders: none — every code and doc block is verbatim; the only run-dependent value (Example's pinned output) has an explicit verification-and-correct step.
- Type consistency: `RunCross`/`Converter` signatures match `run.go:22`; adapters use the same unmarshal→call pattern as the face-local `vectors_test.go`.
