# Release checklist

This document defines the preconditions for tagging and publishing an oxa
release. It does not perform one: tagging, GitHub Releases, and any
announcement are separate, explicitly authorized actions.

## Preconditions (all REQUIRED before a tag)

1. **CI green on the release commit across all five languages**:
   - **Go**: Go 1.23 and 1.24 `-race` test jobs, `gofmt`, `go vet`, `golangci-lint`, the vector validation job (`veccheck`), and the manifest check.
   - **TypeScript**: Node 20 production type-check, architecture tests, Node tests, Web Runtime smoke, npm package-content validation, and a clean installed ESM/type-checking consumer on Ubuntu and Windows.
   - **Rust**: `cargo fmt --all -- --check`, `cargo clippy --workspace --all-targets -- -D warnings`, and `cargo test --workspace`.
   - **Python**: unittest discovery suite across supported Python versions (3.10, 3.11, 3.12, 3.13).
   - **C++**: CMake build and CTest suite across supported platforms (Ubuntu, macOS, Windows) under default configuration, plus Linux Ubuntu `-fno-exceptions` configuration.
   - **Post-v1 gates**: the four-language `consumers` and `reliability` jobs and the TypeScript package/consumer gates are green.
2. **Vectors pinned**: `cd go && go run ./cmd/veccheck -root .. -check-manifest` passes with no drift.
3. **Packaging and downstream consumer verification**:
   - **Go**: for Go submodule consumers, publish matching subpath tag `go/vX.Y.Z` alongside repository tag `vX.Y.Z`; the `consumers` job verifies module resolution in an isolated consumer context.
   - **TypeScript**: `npm run test:package` verifies the `@elkpi/oxa` tarball allowlist and public exports; `npm run test:consumer` installs that tarball into a temporary project, type-checks every public entry point, and imports them under ESM.
   - **Rust**: internal inter-crate dependencies declare workspace version constraints; the `consumers` job checks every public crate's package file list and builds an external consumer from package-shaped extracted sources. Run full `cargo package --locked` after the dependent crates are available in the target registry.
   - **Python**: the `consumers` job builds valid wheel/sdist artifacts and installs each in a clean environment without `PYTHONPATH`.
   - **C++**: the `consumers` job verifies CMake package installation (`cmake --install`) and export (`find_package(oxa CONFIG REQUIRED)`) against an external downstream consumer project.
4. **Module and package coordinates final**:
   - `go/go.mod` declares `module github.com/elkpi/oxa/go` with no placeholder. CI's tag-only `release-guard` job re-checks this at tag time.
   - `ts/package.json` declares npm package `@elkpi/oxa`; its version, Python `pyproject.toml`, Rust `Cargo.toml`, and C++ `CMakeLists.txt` project versions match the target release version.
5. **Specification frozen**: `spec/README.md` states the release spec version; every shipped-scope document is marked `ready`.
6. **Changelog dated**: the `CHANGELOG.md` `[Unreleased]` section is renamed to the release version and date (Keep a Changelog format).
7. **README accurate**: status, support matrix, and quick start match what is being released without pre-v1 or roadmap placeholder language.
8. **Tag and notes**: the tag follows `vX.Y.Z`, points at the release commit on `main`, and its release notes are generated from the matching changelog section.

## Sequence

1. Land the final release-preparation PR (changelog date, spec version if
   needed, README status).
2. Wait for CI on `main`.
3. Tag and publish. Each of these steps is a manual, explicitly
   authorized action — CI and routine PRs never tag or publish.

## Post-v1 continuous verification

The `consumers` job is the clean-install contract for the original four
implementations: it constructs temporary consumers outside the source tree and
exercises the Go module, Rust package contents, Python wheel/sdist, and
installed CMake package. The TypeScript CI job separately exercises the packed
`@elkpi/oxa` artifact through `test:package` and `test:consumer`.

The `reliability` job replays `testdata/stream-fragment-corpus.json` through
Go, Rust, Python, and C++ with a bounded runtime. TypeScript runs all 125 shared
golden vectors in its Node test gate; it is not currently part of that bounded
four-language corpus replay.

None of these CI verification jobs uploads to npm, PyPI, crates.io, Conan, vcpkg, or another
registry.

## Automated Registry Releases

Registry releases are automated via tag-triggered GitHub Actions workflows. Pushing a version tag on `main` initiates pre-publish validation gates and publishing:

| Registry | Target Package | Trigger Tag | Workflow | Pre-Publish Gate | Credentials |
|---|---|---|---|---|---|
| **npm** | `@elkpi/oxa` | `ts/vX.Y.Z` | `.github/workflows/release-npm.yml` | `npm run release:check` (13 gates) | `NPM_TOKEN` (Granular Token) |
| **PyPI** | `elkpi-oxa` | `py/vX.Y.Z` | `.github/workflows/release-pypi.yml` | `python -m unittest discover` | PyPI Trusted Publishing (OIDC) or `PYPI_API_TOKEN` / `PYPI_TOKEN` |
| **crates.io** | 7 crates (`oxa-*` + `elkpi-oxa`) | `rust/vX.Y.Z` | `.github/workflows/release-crates.yml` | `cargo clippy` + `cargo test` | `CARGO_REGISTRY_TOKEN` |

Each workflow validates that the tag matches the package version declared in source (`ts/package.json`, `python/pyproject.toml`, or `rust/Cargo.toml`) before proceeding. Workflows can also be triggered manually via `workflow_dispatch`. Local/manual publishing can be performed idempotently via `scripts/publish-registries.sh`.
