# oxa Rust implementation

The Rust workspace package version is **2.0.0**. Rust emits IR documents
with `specVersion: 0.2.0` and accepts both `0.1.0` and `0.2.0`; implementation
and IR contract versions are independent version axes.

It conforms to the **same shared `vectors/` golden set** as every other
oxa implementation — Rust gets no vector set of its own, and CI runs all 154
Spec 2.0 vectors against it.

## Layout

`rust/` is an independent Cargo workspace (mirroring `go/` as an
independent Go module):

```
rust/Cargo.toml          workspace root
rust/crates/elkpi-oxa/   umbrella crate (`use oxa::...`) re-exporting all faces
rust/crates/oxa-ir/      the IR types, document codec, and invariant checker
                         (spec/01); implemented
rust/crates/oxa-vectest/         the dev-only vector harness (dev-dependency
                                 of face crates); implemented
rust/crates/oxa-modelmap/        the optional model-renaming table
                                 (spec/03); implemented
rust/crates/oxa-chatcompletions/ the Chat Completions face; implemented
rust/crates/oxa-responses/       the OpenAI Responses face; implemented
rust/crates/oxa-anthropic/       the Anthropic Messages face; implemented
rust/crates/oxa-sse/             the byte-level SSE frame adapter
                                 (spec/20 §6); implemented
```

Production crates use the runtime dependencies `serde` and `serde_json`.
Test-only code may add dev-dependencies; the hub-and-spoke dependency rule from
spec/00 §4 applies unchanged: face crates must not import each other, only
`oxa-ir` and `oxa-modelmap`.

## Vectors location convention

Test code must **not** hard-code a path to the vectors. Instead, starting
from this implementation directory, **walk up parent directories** until you
find a directory containing both `vectors/` and `.git/` — that is the
repository root, and `vectors/` beneath it is the golden set. **Skip the
vector tests** (with a clear message) if no such root is found, so the crate
can still build and test outside the monorepo.

## Status

- [x] `oxa-ir` — Spec 2.0 IR types, `specVersion` 0.2.0 emission with 0.1.0
      dual-read, INV-5/INV-6 event-stream checker, and loss records (spec/02)
- [x] Spec 2.0 reasoning blocks/signatures, request reasoning effort, and
      granular usage across all three protocol faces
- [x] vector harness (`oxa-vectest`) and the walk-up repo-root discovery
- [x] Chat Completions face (nonstream and streaming)
- [x] Anthropic Messages face (nonstream and streaming)
- [x] Responses face (nonstream and streaming)
- [x] cross-protocol vectors through the composition
- [x] `oxa-sse` — byte-level SSE frame adapter (spec/20 §6)
- [x] streaming text (M6), tool-argument aggregation (M7), and reasoning blocks
      with signatures (M9) across all three faces
