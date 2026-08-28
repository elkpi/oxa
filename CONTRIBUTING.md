# Contributing to oxa

Thanks for your interest in contributing.

## Development environment

- Go **1.23 or newer** for the reference implementation (`go/`).
- Standard `git`, `make`, and a POSIX shell.

## Commit policy

Keep commits **fine-grained** so they can be cherry-picked independently:

- Only **tightly related** changes belong in the same commit.
- Unrelated changes **must** be split into separate commits — e.g. a bugfix
  and a refactor go in different commits, and fixes in different modules are
  committed separately.
- Tests may share a commit with implementation **only** when they directly
  verify that implementation.

## Vector-contribution workflow (spec → vectors → implementation)

Every behavior change follows this order, no exceptions:

1. **Spec first.** Update the relevant document(s) under `spec/` and include
   the rationale for the behavior change.
2. **Vectors second.** Update `vectors/` so the golden set matches the new
   spec.
3. **Implementation last.** Update the implementations (starting with `go/`)
   until CI passes against the new vector set.

A PR that changes behavior without a spec update and rationale will be
returned to you.

## No new-language skeletons before v1 freeze

We do **not** accept PRs adding new-language skeletons (Rust, Python, C++, or
anything else) before the v1 spec freeze. Adding a language before the spec
stabilizes multiplies rework across every behavior change. Once v1 is frozen,
per-language directories will be opened with a tracked issue each.

## Reporting bugs

Open an issue with the payload you converted, what you expected, and what you
got. Never include real credentials in payloads.
