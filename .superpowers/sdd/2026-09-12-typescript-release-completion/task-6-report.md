# Task 6 report — TypeScript package and release-preparation gates

## Summary

Prepared the ESM-only `@elkpi/oxa` v1 package in commit `f171d9b`
(`feat(ts): prepare v1 package release`).

- Added explicit JavaScript and declaration entry points for the package root,
  common modules, async stream adapters, and all three completed protocol
  faces.
- Restricted the npm tarball to the compiled public-module closure, declaration
  files and maps, package metadata, README, Apache-2.0 license, and notice.
  Internal vector-test and generated-schema helpers, sources, tests, and build
  tooling are excluded.
- Added a `prepack` gate that checks generated declarations and builds the
  distributable output.
- Added a real `npm pack --json --dry-run` content/metadata test.
- Added a temporary clean consumer that packs and installs the real tarball,
  type-checks imports from every public entry point, and imports the same entry
  points at ESM runtime.
- Added non-preview TypeScript v1 documentation and updated the root language
  matrix and downstream-verification documentation.
- Added Linux and Windows CI package/consumer gates plus discoverable Make
  targets.
- Added `npm run release:check`, which performs all release-readiness checks
  and contains no publishing command.

## TDD evidence

- Package RED: `npm run test:package` failed with
  `packed package is missing LICENSE` before package assets, exports, types,
  prepack, and the public-file allowlist were configured.
- Consumer RED: after installing the original tarball into a temporary clean
  project, TypeScript reported TS2307 for `@elkpi/oxa` and every intended
  subpath because the package had no entry-point metadata.
- GREEN: `npm run test:package` passed after package configuration.
- GREEN: `npm run test:consumer` installed one local tarball, type-checked all
  public entries, and printed both `clean ESM consumer import passed` and
  `clean TypeScript consumer typecheck passed`.

## Clean-state release verification

After commit `f171d9b`, `git status --short` produced no output. From that
clean worktree, `npm run release:check` exited zero and reported:

- vector schemas/manifest: 125 vectors, 126 checks;
- generated declarations, Prettier, and production type-check: passed;
- Node: 64 passed, 0 failed;
- Web Runtime smoke: passed;
- npm dry-run contents and clean installed ESM/TypeScript consumer: passed;
- Go uncached tests, build, vet, gofmt, and module-path check: passed;
- final message: `release checks passed; no package was published`.

The available local runtime was Node 24.18.0. CI remains pinned to Node 20.x on
both Ubuntu and Windows and runs the new package and consumer gates.

## Files and scope

- `ts/package.json`, `ts/package-lock.json`
- `ts/README.md`, `ts/LICENSE`, `ts/NOTICE`
- `ts/scripts/test-package.mjs`, `ts/scripts/test-consumer.mjs`
- `ts/scripts/release-check.mjs`
- root `README.md`, `Makefile`, and `.github/workflows/ci.yml`

No specification, vector, converter, IR, or stream behavior changed. No npm
publish command was run.
