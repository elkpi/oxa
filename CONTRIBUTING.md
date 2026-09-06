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

## Contributions after v1 is frozen

With v1 frozen, behavior changes still require the `spec → vectors →
implementation` order and an explicit compatibility review. A new language or
public package must include a tracked consumer smoke test, the documented
runtime dependencies, and a plan for the shared vector suite. New protocol
semantics must first be proposed against the versioning rules in `spec/README.md`;
contributors must not silently extend a sealed IR union or enum.

## Reporting bugs

Open an issue with the payload you converted, what you expected, and what you
got. Never include real credentials in payloads.
