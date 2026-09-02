# oxa Rust implementation

The Rust implementation tracks the frozen spec **0.0.2** baseline and
targets spec version **0.1.0** (the Rust milestone in the project's
versioning cadence: 0.0.x Go, 0.1.0 Rust, 0.2.0 Python, 0.3.0 C++, 1.0.0
once every supported language implements the same spec against the same
vectors).

It conforms to the **same shared `vectors/` golden set** as every other
oxa implementation — Rust gets no vector set of its own, and CI runs the
identical vectors against it.

## Layout

`rust/` is an independent Cargo workspace (mirroring `go/` as an
independent Go module):

```
rust/Cargo.toml          workspace root
rust/crates/oxa-ir/      the IR types, document codec, and invariant checker
                         (spec/01); implemented
rust/crates/oxa-vectest/         the dev-only vector harness (dev-dependency
                                 of face crates); implemented
rust/crates/oxa-modelmap/        the optional model-renaming table
                                 (spec/03); implemented
rust/crates/oxa-chatcompletions/ the Chat Completions face (nonstream);
                                 implemented
rust/crates/oxa-responses/       planned
rust/crates/oxa-anthropic/       the Anthropic Messages face (nonstream);
                                 implemented
rust/crates/oxa-sse/             planned
```

Production crates depend on `serde` and `serde_json` only. Test-only code
may add dev-dependencies; the hub-and-spoke dependency rule from spec/00 §4
applies unchanged: face crates must not import each other, only `oxa-ir`
and `oxa-modelmap`.

## Vectors location convention

Test code must **not** hard-code a path to the vectors. Instead, starting
from this implementation directory, **walk up parent directories** until you
find a directory containing both `vectors/` and `.git/` — that is the
repository root, and `vectors/` beneath it is the golden set. **Skip the
vector tests** (with a clear message) if no such root is found, so the crate
can still build and test outside the monorepo.

## Status

- [x] `oxa-ir` — IR types, `specVersion` document codec, INV-5/INV-6
      event-stream checker, loss records (spec/02)
- [x] vector harness (`oxa-vectest`) and the walk-up repo-root discovery
- [x] Chat Completions face (nonstream)
- [x] Anthropic Messages face (nonstream)
- [ ] Responses face (nonstream)
- [ ] cross-protocol vectors through the composition
- [ ] streaming (text profile, then tool-argument aggregation)
