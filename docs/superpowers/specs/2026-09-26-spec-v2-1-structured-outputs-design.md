# Spec 2.1 — Structured Outputs and Response Format (Design Document)

Date: 2026-09-26
Status: APPROVED for planning.

## 1. Problem Statement

Structured generation (`response_format` / Structured Outputs) has become an essential capability for LLM integrations, enabling callers to enforce valid JSON mode (`json_object`) or exact schema conformance (`json_schema` with optional `strict` enforcement).

Currently, oxa treats structured output request fields as unsupported across all faces:
- **OpenAI Chat Completions**: wire `response_format` is dropped with an `unmapped-field` loss (`spec/10 §3`).
- **OpenAI Responses**: wire `text.format` is dropped with an `unmapped-field` loss (`spec/11 §3`).
- **Anthropic Messages**: lacks a top-level native wire parameter for structured outputs; conversions to Anthropic currently drop the preference if simulated elsewhere.

Consequently, any cross-protocol translation (e.g., Chat Completions ↔ Responses) loses structured output constraints, forcing callers to manually unpack, track, and restore formatting parameters outside of oxa.

## 2. Governance and Versioning

Per `spec/README.md`, the post-1.0 evolution rules define:
- **Patch releases (x.y.z)**: wording clarifications with zero schema or behavioral changes.
- **Minor releases (2.x)**: additive optional members on non-sealed types (such as `Params` or `Usage`).
- **Major releases (3.x)**: sealed union or enum extensions (such as new `Block` or `Delta` variants, new stop reasons).

Because `response_format` is an additive optional configuration parameter on `Request.Params` (an open object type), this feature qualifies as a **Minor Release: Spec 2.1.0**.

- **IR Document Contract Version**: remains `"0.2.0"`. `spec/schema/ir.schema.json` retains `enum: ["0.1.0", "0.2.0"]`. Existing vectors require no version bump.
- **Release Coordinate Bump**: language packages and specification bump to `v2.1.0` upon coordinated rollout completion.

## 3. IR Data Model & JSON Schema Design

### 3.1 `ResponseFormat` Union

An explicit, face-neutral `ResponseFormat` discriminant object is introduced into `spec/schema/ir.schema.json` under `#/$defs/responseFormat`:

```json
"responseFormat": {
  "description": "Output formatting preference (spec/01 §3.7.1, since 2.1).",
  "oneOf": [
    {
      "type": "object",
      "properties": {
        "type": { "const": "text" }
      },
      "required": ["type"],
      "additionalProperties": false
    },
    {
      "type": "object",
      "properties": {
        "type": { "const": "json_object" }
      },
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
}
```

### 3.2 `Params` Extension

`Request.Params` in `spec/schema/ir.schema.json` is extended with:

```json
"response_format": {
  "$ref": "#/$defs/responseFormat"
}
```

- When `response_format` is not specified by the caller, `params.response_format` is omitted in canonical serialization.
- `type: "text"` represents explicit unstructured text generation.
- `type: "json_object"` represents schema-free JSON object output (JSON mode).
- `type: "json_schema"` represents strict or guided JSON schema output with a required `name` and verbatim `schema`.

### 3.3 Verbatim Schema Preservation (INV-1)

Following the precedent of `Tool.InputSchema` and `ToolUseBlock.Input` (spec/01 §3.5), `json_schema.schema` is treated as opaque JSON text/object. Converters MUST NOT reorder keys, mutate numbers, normalize types, or strip unknown extensions.

## 4. Spoke Mapping Specifications

### 4.1 OpenAI Chat Completions (`openai/chatcompletions`)

- **Decode (CC → IR)**:
  - `response_format.type == "text"` → `ResponseFormat{Type: "text"}` (or omitted if defaulting).
  - `response_format.type == "json_object"` → `ResponseFormat{Type: "json_object"}`.
  - `response_format.type == "json_schema"` → unwraps nested `json_schema` object:
    - `name` (required string) → `ResponseFormat.Name`
    - `description` (optional string) → `ResponseFormat.Description`
    - `schema` (required object) → `ResponseFormat.Schema` (verbatim per INV-1)
    - `strict` (optional boolean) → `ResponseFormat.Strict`
  - Unrecognized `type` value → parameter dropped with `LOSS_UNMAPPED_VALUE` on `response_format.type`.
  - Non-object or missing required fields (`name`, `schema`) when `type == "json_schema"` → structural error.
- **Encode (IR → CC)**:
  - `params.response_format == nil` → omit `response_format`.
  - `type: "text"` → emit `{"type": "text"}`.
  - `type: "json_object"` → emit `{"type": "json_object"}`.
  - `type: "json_schema"` → wraps into nested `{"type": "json_schema", "json_schema": {"name": ..., ...}}`.
  - **Loss**: 0 (lossless round trip).

### 4.2 OpenAI Responses (`openai/responses`)

- **Decode (RE → IR)**:
  - Inspects `text.format` in request.
  - Maps `text.format` (flat format object) into `params.response_format`.
  - Unmapped field loss for `text.format` is removed. (`text.verbosity` remains an unmapped field if present).
- **Encode (IR → RE)**:
  - If `params.response_format` is present:
    - Renders as flat `text.format` object within request `text`.
  - **Loss**: 0 (lossless round trip between CC and Responses).

### 4.3 Anthropic Messages (`anthropic/messages`)

- **Decode (AN → IR)**:
  - Anthropic has no top-level wire format parameter. No changes.
- **Encode (IR → AN)**:
  - Anthropic Messages API does not natively support a request-level `response_format` parameter.
  - Consistent with oxa's core contract (pure conversion without proxy injection or payload synthesis):
    - Converters MUST NOT inject synthetic dummy tools or mutate `tools` / `tool_choice`.
    - Exactly one `LOSS_UNMAPPED_FIELD` is recorded on `params.response_format`:
      - `path`: `"params.response_format"`
      - `field`: `"response_format"`
      - `reason`: `LOSS_UNMAPPED_FIELD`
      - `detail`: `"Anthropic Messages has no native response_format request parameter; structured output preference is dropped."`

### 4.4 Streaming Semantics (spec/20)

`response_format` is purely a request-side parameter. Streaming events emitted by providers (`content_block_delta`, `response.output_text.delta`, Chat Completions chunks) follow existing text delta contracts. No new delta variants or stream lifecycle transitions are required.

## 5. Golden Vectors Strategy

Golden vectors under `vectors/` will be extended with:
1. `chatcompletions.to-ir.request-response-format-json-object`
2. `chatcompletions.to-ir.request-response-format-json-schema`
3. `chatcompletions.from-ir.request-response-format-json-object`
4. `chatcompletions.from-ir.request-response-format-json-schema`
5. `responses.to-ir.request-text-format-json-schema`
6. `responses.from-ir.request-text-format-json-schema`
7. `anthropic.from-ir.request-response-format-dropped` (verifying exact `unmapped-field` loss)
8. `cross.chatcompletions-to-responses.request-response-format-json-schema` (0 loss CC ↔ RE)
9. `cross.chatcompletions-to-anthropic.request-response-format-json-schema` (1 loss on AN encode)

`veccheck -write-manifest` will regenerate `vectors/manifest.json`.

## 6. Implementation Rollout Sequence

- **Phase 0**: Specification & Schema Freeze + Vectors Generation (`vectors/manifest.json`).
- **Wave 1**: Go reference implementation (`github.com/elkpi/oxa/go/v2`).
- **Wave 2**: TypeScript implementation (`@elkpi/oxa`).
- **Wave 3**: Rust workspace (`elkpi-oxa`).
- **Wave 4**: Python package (`elkpi-oxa`).
- **Wave 5**: C++ library (`oxa`).
- **Wave 6**: Coordinated release `v2.1.0`.

## 7. Approved Decisions Summary

- **Decision 1 (Location)**: `response_format` is housed in `Request.Params.ResponseFormat` as an additive optional field (Minor Release 2.1.0).
- **Decision 2 (IR Structure)**: Flattened, face-neutral discriminant union (`text`, `json_object`, `json_schema` with `name`, `description`, `schema`, `strict`).
- **Decision 3 (Anthropic Mapping)**: Pure conversion with single `LOSS_UNMAPPED_FIELD`; no synthetic tool injection.
- **Decision 4 (INV-1 Preservation)**: Schema objects are carried verbatim without JSON manipulation.
