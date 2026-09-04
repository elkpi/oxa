# Release checklist

This document defines the preconditions for tagging and publishing an oxa
release. It does not perform one: tagging, GitHub Releases, and any
announcement are separate, explicitly authorized actions.

## Preconditions (all REQUIRED before a tag)

1. **CI green on the release commit across all four languages**:
   - **Go**: Go 1.23 and 1.24 `-race` test jobs, `gofmt`, `go vet`, `golangci-lint`, the vector validation job (`veccheck`), and the manifest check.
   - **Rust**: `cargo fmt --all -- --check`, `cargo clippy --workspace --all-targets -- -D warnings`, and `cargo test --workspace`.
   - **Python**: unittest discovery suite across supported Python versions (3.10, 3.11, 3.12, 3.13).
   - **C++**: CMake build and CTest suite under default and `-fno-exceptions` configurations across supported platforms.
2. **Vectors pinned**: `cd go && go run ./cmd/veccheck -root .. -check-manifest` passes with no drift.
3. **Module and package coordinates final**:
   - `go/go.mod` declares `module github.com/elkpi/oxa/go` with no placeholder. CI's tag-only `release-guard` job re-checks this at tag time.
   - For Go submodule consumers, publish matching subpath tag `go/vX.Y.Z` alongside repository tag `vX.Y.Z`.
   - Python `pyproject.toml`, Rust `Cargo.toml`, and C++ `CMakeLists.txt` project versions match the target release version.
4. **Specification frozen**: `spec/README.md` states the release spec version; every shipped-scope document is marked `ready`.
5. **Changelog dated**: the `CHANGELOG.md` `[Unreleased]` section is renamed to the release version and date (Keep a Changelog format).
6. **README accurate**: status, support matrix, and quick start match what is being released without pre-v1 or roadmap placeholder language.
7. **Tag and notes**: the tag follows `vX.Y.Z`, points at the release commit on `main`, and its release notes are generated from the matching changelog section.

## Sequence

1. Land the final release-preparation PR (changelog date, spec version if
   needed, README status).
2. Wait for CI on `main`.
3. Tag and publish. Each of these steps is a manual, explicitly
   authorized action — CI and routine PRs never tag or publish.
