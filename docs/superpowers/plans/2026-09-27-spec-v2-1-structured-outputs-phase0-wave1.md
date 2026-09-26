# Spec 2.1 Structured Outputs and Response Format Implementation Plan (Phase 0 & Wave 1 Go)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Phase 0 (Specification, JSON Schema, and Golden Vectors) and Wave 1 (Go reference implementation) of Spec 2.1.0, enabling first-class structured generation (`text`, `json_object`, and `json_schema` with `strict` and INV-1 schema preservation) across OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages spokes.

**Architecture:** Hub-and-Spoke protocol conversion via a face-neutral `ResponseFormat` type placed on `Request.Params.ResponseFormat`. Chat Completions and Responses achieve loss-free 0-loss round trips, while Anthropic Messages records a deterministic `unmapped-field` loss without mutating tools or injecting synthetic structures.

**Tech Stack:** Go 1.23+, JSON Schema (Draft 2020-12), `veccheck` vector validation tool.

**Spec:** `docs/superpowers/specs/2026-09-26-spec-v2-1-structured-outputs-design.md`

## Global Constraints

- Hub-and-spoke isolation: Spoke packages (`openai/chatcompletions`, `openai/responses`, `anthropic/messages`) import only standard library, `ir`, and `modelmap` — never another face package.
- Minor version bump: Additive optional property on `Params`; IR document `specVersion` remains `"0.2.0"`.
- INV-1 byte-fidelity: JSON schema bytes in `json_schema.schema` are carried verbatim without key reordering or numeric mutation.
- Deterministic Loss tracking: CC ↔ RE round trips have 0 losses; IR → AN records exactly one `LOSS_UNMAPPED_FIELD` on `params.response_format`.
- Fine-grained commits: Each task must produce a cleanly cherry-pickable commit ending with green tests.

## Review Focus

1. `json_schema` with missing `name` or `schema` → must reject with a structural error (`CodecError` / `error`), not panic or emit malformed wire JSON. (Tested in Task 4 & Task 5).
2. Unknown `response_format.type` (e.g. `"yaml"`) → must drop the format parameter and emit `LOSS_UNMAPPED_VALUE` on `response_format.type`. (Tested in Task 5 & Task 6).
3. `strict: false` vs absent `strict` on `json_schema` → must preserve `*bool` distinction (`false` is not omitted). (Tested in Task 4).
4. Schema JSON text with non-standard formatting (INV-1) → must survive round-trip verbatim without key re-sorting. (Tested in Task 4 & Task 5).
5. Empty / absent `response_format` → `params.set()` must return `false` if no other params exist, omitting `params` completely from IR JSON. (Tested in Task 4).

---

### Task 1: Update Specification and JSON Schema (Phase 0)

**Files:**
- Modify: `spec/schema/ir.schema.json:218-235`
- Modify: `spec/01-intermediate-representation.md:140-160`
- Test: `make vectors`

**Interfaces:**
- Produces: JSON Schema `$defs/responseFormat` and `params.properties.response_format`.

- [ ] **Step 1: Edit `spec/schema/ir.schema.json` to define `responseFormat` and add to `params`**

Add `#/$defs/responseFormat` before `params`:
```json
    "responseFormat": {
      "description": "Output formatting preference (spec/01 §3.7.1, since 2.1).",
      "oneOf": [
        {
          "type": "object",
          "properties": { "type": { "const": "text" } },
          "required": ["type"],
          "additionalProperties": false
        },
        {
          "type": "object",
          "properties": { "type": { "const": "json_object" } },
          "required": ["type"],
          "additionalProperties": false
        },
        {
          "type": "object",
          "properties": {
            "type": { "const": "json_schema" },
            "name": { "type": "string", "minLength": 1 },
            "description": { "type": "string" },
            "schema": {
              "type": "object",
              "description": "JSON Schema object; carried verbatim per INV-1."
            },
            "strict": { "type": "boolean" }
          },
          "required": ["type", "name", "schema"],
          "additionalProperties": false
        }
      ]
    },
```
And add `response_format` to `params.properties`:
```json
        "response_format": {
          "$ref": "#/$defs/responseFormat"
        }
```

- [ ] **Step 2: Update `spec/01-intermediate-representation.md`**

Add Section §3.7.1 describing `ResponseFormat`, its variants (`text`, `json_object`, `json_schema`), INV-1 verbatim schema semantics, and omission when unspecified.

- [ ] **Step 3: Run schema validation to verify syntax**

Run: `go run ./go/cmd/veccheck -root . -schema-only`
Expected: `schemas OK: vector, ir, loss compile against 2020-12 (spec_version [0.1.0 0.2.0])`

- [ ] **Step 4: Commit**

```bash
git add spec/schema/ir.schema.json spec/01-intermediate-representation.md
git commit -m "spec(ir): define responseFormat union and add to Params schema (spec 2.1)"
```

---

### Task 2: Update Spoke Mapping Specifications (Phase 0)

**Files:**
- Modify: `spec/10-mapping-openai-chat-completions.md:65-75`
- Modify: `spec/11-mapping-openai-responses.md:75-85`
- Modify: `spec/12-mapping-anthropic-messages.md:65-75`
- Modify: `spec/02-loss-policy.md`

**Interfaces:**
- Documents: Normative spoke mapping rules N-CC-13, N-R-14, N-AN-12.

- [ ] **Step 1: Update `spec/10-mapping-openai-chat-completions.md`**

Replace `| response_format | — | unmapped-field loss |` with normative mapping:
`| response_format | Params.ResponseFormat | N-CC-13 (since 2.1); 1:1 unwrapped json_schema |`.
Document rule N-CC-13: decoding `type: "text"`, `"json_object"`, `"json_schema"`, unknown value dropping, and encode re-wrapping.

- [ ] **Step 2: Update `spec/11-mapping-openai-responses.md`**

Replace `| text.verbosity, text.format | — | unmapped-field loss |` with:
`| text.format | Params.ResponseFormat | N-R-14 (since 2.1); 1:1 flat format object |`.
`text.verbosity` remains an unmapped-field loss.

- [ ] **Step 3: Update `spec/12-mapping-anthropic-messages.md`**

Document rule N-AN-12: when `Params.ResponseFormat` is present on encode, converters record `LOSS_UNMAPPED_FIELD` on `params.response_format` and do not synthesize dummy tools.

- [ ] **Step 4: Update `spec/02-loss-policy.md`**

Add `params.response_format` to the Loss Catalog under `unmapped-field`.

- [ ] **Step 5: Commit**

```bash
git add spec/10-mapping-openai-chat-completions.md spec/11-mapping-openai-responses.md spec/12-mapping-anthropic-messages.md spec/02-loss-policy.md
git commit -m "spec: document response_format spoke mapping and loss rules (spec 2.1)"
```

---

### Task 3: Author Golden Vectors & Update Manifest (Phase 0)

**Files:**
- Create: `vectors/chatcompletions/to-ir/request-response-format-json-object.json`
- Create: `vectors/chatcompletions/to-ir/request-response-format-json-schema.json`
- Create: `vectors/chatcompletions/from-ir/request-response-format-json-object.json`
- Create: `vectors/chatcompletions/from-ir/request-response-format-json-schema.json`
- Create: `vectors/responses/to-ir/request-text-format-json-schema.json`
- Create: `vectors/responses/from-ir/request-text-format-json-schema.json`
- Create: `vectors/anthropic/from-ir/request-response-format-dropped.json`
- Create: `vectors/cross/request-chatcompletions-to-responses-response-format.json`
- Create: `vectors/cross/request-chatcompletions-to-anthropic-response-format.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Produces: 9 new golden test vectors bringing total vector count to 163.

- [ ] **Step 1: Write `chatcompletions` to-ir and from-ir vectors**

Create the 4 CC vector JSON files following vector schema:
`request-response-format-json-object.json` with `{"type": "json_object"}`.
`request-response-format-json-schema.json` with `{"type": "json_schema", "json_schema": {"name": "user_info", "schema": {"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}, "strict": true}}`.

- [ ] **Step 2: Write `responses` to-ir and from-ir vectors**

Create `request-text-format-json-schema.json` with `text: {"format": {"type": "json_schema", "name": "user_info", "schema": {...}, "strict": true}}`.

- [ ] **Step 3: Write `anthropic` from-ir vector**

Create `request-response-format-dropped.json` converting an IR request with `response_format` to Anthropic wire format, asserting 1 expected loss with reason `unmapped-field` at path `params.response_format`.

- [ ] **Step 4: Write `cross` protocol vectors**

Create `request-chatcompletions-to-responses-response-format.json` (0 expected loss).
Create `request-chatcompletions-to-anthropic-response-format.json` (1 expected loss).

- [ ] **Step 5: Regenerate manifest and validate vectors**

Run: `cd go && go run ./cmd/veccheck -root .. -write-manifest`
Expected: `163 vectors, 163 checks, OK`

- [ ] **Step 6: Commit**

```bash
git add vectors/
git commit -m "vectors: add Spec 2.1 structured outputs golden vectors"
```

---

### Task 4: Implement IR ResponseFormat in Go (`go/v2/ir`)

**Files:**
- Modify: `go/ir/request.go`
- Modify: `go/ir/json.go`
- Modify: `go/ir/json_test.go`

**Interfaces:**
- Consumes: Spec 2.1 IR types.
- Produces:
  ```go
  type ResponseFormat struct {
      Type        string // text | json_object | json_schema
      Name        string // required iff Type is json_schema
      Description string // optional
      Schema      []byte // verbatim JSON Schema bytes (INV-1)
      Strict      *bool  // optional
  }
  const (
      ResponseFormatText       = "text"
      ResponseFormatJSONObject = "json_object"
      ResponseFormatJSONSchema = "json_schema"
  )
  ```

- [ ] **Step 1: Write failing unit test in `go/ir/json_test.go`**

Test marshal and unmarshal of `Params{ResponseFormat: &ResponseFormat{Type: ResponseFormatJSONSchema, Name: "result", Schema: []byte(`{"type":"object"}`), Strict: boolPtr(true)}}`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go && go test -v ./ir -run TestResponseFormatCodec`
Expected: FAIL (compilation error, `ResponseFormat` undefined)

- [ ] **Step 3: Add `ResponseFormat` struct and constants to `go/ir/request.go`**

Add `ResponseFormat` struct, constants, and update `Params.set()`:
```go
func (p Params) set() bool {
    return p.Temperature != nil || p.TopP != nil || p.MaxTokens != nil ||
        len(p.StopSequences) > 0 || p.ReasoningEffort != "" || p.ResponseFormat != nil
}
```

- [ ] **Step 4: Update `wireParams` and unmarshaling in `go/ir/json.go`**

Add `wireResponseFormat` struct and map between `ResponseFormat` and `wireResponseFormat` in `MarshalRequest` and `UnmarshalRequest`.

- [ ] **Step 5: Run tests to verify pass**

Run: `cd go && go test -v ./ir`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go/ir/
git commit -m "feat(go/ir): add ResponseFormat type and JSON codec support"
```

---

### Task 5: Implement Chat Completions Spoke Support (`go/v2/openai/chatcompletions`)

**Files:**
- Modify: `go/openai/chatcompletions/types.go`
- Modify: `go/openai/chatcompletions/decode.go`
- Modify: `go/openai/chatcompletions/encode.go`
- Modify: `go/openai/chatcompletions/wire_test.go`

**Interfaces:**
- Consumes: `ir.ResponseFormat`, `ir.Params`.
- Produces: Wire decode/encode for `response_format` in Chat Completions.

- [ ] **Step 1: Write failing test in `wire_test.go`**

Add tests for decoding and encoding `json_object` and `json_schema` with `strict: true`.

- [ ] **Step 2: Run test to verify failure**

Run: `cd go && go test -v ./openai/chatcompletions -run TestResponseFormat`
Expected: FAIL

- [ ] **Step 3: Update `types.go` and `decode.go`**

Define `ResponseFormatWire` and `JSONSchemaWire` in `types.go`.
In `decode.go`:
```go
if wire.ResponseFormat != nil {
    rf, rfLosses, err := decodeResponseFormat(wire.ResponseFormat)
    if err != nil {
        return nil, nil, err
    }
    losses = append(losses, rfLosses...)
    req.Params.ResponseFormat = rf
}
```
Implement `decodeResponseFormat(any) (*ir.ResponseFormat, []ir.Loss, error)`.

- [ ] **Step 4: Update `encode.go`**

In `encode.go`, when `req.Params.ResponseFormat != nil`, format `ResponseFormatWire` and assign to `wire.ResponseFormat`.

- [ ] **Step 5: Run test to verify pass**

Run: `cd go && go test -v ./openai/chatcompletions`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go/openai/chatcompletions/
git commit -m "feat(go/chatcompletions): decode and encode response_format"
```

---

### Task 6: Implement Responses Spoke Support (`go/v2/openai/responses`)

**Files:**
- Modify: `go/openai/responses/types.go`
- Modify: `go/openai/responses/decode.go`
- Modify: `go/openai/responses/encode.go`
- Modify: `go/openai/responses/wire_test.go`

**Interfaces:**
- Consumes: `ir.ResponseFormat`, `ir.Params`.
- Produces: Wire decode/encode for `text.format` in Responses.

- [ ] **Step 1: Write failing test in `wire_test.go`**

Test decode and encode of `text.format` with `json_schema`.

- [ ] **Step 2: Run test to verify failure**

Run: `cd go && go test -v ./openai/responses -run TestTextFormat`
Expected: FAIL

- [ ] **Step 3: Update `decode.go` in `openai/responses`**

Remove `text.format` from unmapped fields loop.
If `wire.Text != nil && wire.Text.Format != nil`:
Decode `wire.Text.Format` into `req.Params.ResponseFormat`.

- [ ] **Step 4: Update `encode.go` in `openai/responses`**

If `req.Params.ResponseFormat != nil`:
Create `wire.Text` if nil, and set `wire.Text.Format` to formatted `TextFormatWire`.

- [ ] **Step 5: Run tests to verify pass**

Run: `cd go && go test -v ./openai/responses`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go/openai/responses/
git commit -m "feat(go/responses): decode and encode text.format"
```

---

### Task 7: Implement Anthropic Spoke Loss Handling (`go/v2/anthropic/messages`)

**Files:**
- Modify: `go/anthropic/messages/encode.go`
- Modify: `go/anthropic/messages/wire_test.go`

**Interfaces:**
- Consumes: `ir.Params.ResponseFormat`.
- Produces: Emits `LOSS_UNMAPPED_FIELD` on `params.response_format`.

- [ ] **Step 1: Write failing test in `wire_test.go`**

Assert that encoding an IR request with `Params{ResponseFormat: &ir.ResponseFormat{Type: ir.ResponseFormatJSONObject}}` records an `unmapped-field` loss on `params.response_format`.

- [ ] **Step 2: Run test to verify failure**

Run: `cd go && go test -v ./anthropic/messages -run TestResponseFormatDropped`
Expected: FAIL

- [ ] **Step 3: Update `go/anthropic/messages/encode.go`**

Add:
```go
if req.Params.ResponseFormat != nil {
    losses = append(losses, ir.Loss{
        Path:   "params.response_format",
        Field:  "response_format",
        Reason: ir.LossUnmappedField,
        Detail: "Anthropic Messages has no native response_format request parameter; structured output preference is dropped.",
    })
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `cd go && go test -v ./anthropic/messages`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go/anthropic/messages/
git commit -m "feat(go/anthropic): report unmapped-field loss for params.response_format"
```

---

### Task 8: Full Go Verification & Vector Conformance (Wave 1 Completion)

**Files:**
- Test: All vectors against Go implementation.
- Check: Constant convergence across schemas and Go.

- [ ] **Step 1: Run veccheck on all 163 vectors**

Run: `cd go && go run ./cmd/veccheck -root .. -check-manifest`
Expected: `163 vectors, 163 checks, OK`

- [ ] **Step 2: Run full Go test suite with race detector**

Run: `cd go && go test -race -count=1 ./...`
Expected: All tests PASS

- [ ] **Step 3: Run repository-wide checks**

Run: `make test && make lint && make fmt && make vectors && make check-modulepath`
Expected: All clean and green.

- [ ] **Step 4: Commit**

```bash
git commit --allow-empty -m "chore(go): complete Phase 0 and Wave 1 Go reference implementation for Spec 2.1"
```
