oxa
===

Protocol conversion between the OpenAI and Anthropic APIs, as pure
in-process libraries.

[![CI](https://github.com/elkpi/oxa/actions/workflows/ci.yml/badge.svg)](https://github.com/elkpi/oxa/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

**Status: v1.0.0 released.** The specification, golden vectors,
and all four reference implementations (Go, Rust, Python, and C++) pass the
identical 125 golden vectors across nonstream, cross-protocol, and stream suites.

## What is oxa?

oxa is a collection of **pure protocol-conversion libraries** that translate
between OpenAI APIs (Chat Completions and Responses) and the Anthropic
Messages API. Each library takes structured request/response payloads in one
dialect and returns equivalent structured payloads in another — nothing more.

## What oxa is NOT

oxa is explicitly **not** any of the following:

- **Not a proxy.** There is no server, no listening socket, no HTTP
  forwarding.
- **Not an HTTP forwarding service.** oxa never re-issues your requests to an
  upstream provider.
- **Not a router.** oxa does not pick providers, balance load, fail over, or
  manage keys.

This is the key difference from projects like LiteLLM or one-api, which sit
between your application and the providers as gateways. oxa has no runtime
position in your network path at all: it is a set of functions you call to
convert payloads, and you keep full ownership of transport, retries, and
credentials.

## The three deliverable layers

oxa is built as three mutually reinforcing layers:

1. **`spec/` — a written protocol-conversion specification.** Precise,
   testable rules for how each field, content block, and event maps between
   the OpenAI and Anthropic dialects.
2. **`vectors/` — golden test vectors.** Machine-readable input/expected-
   output pairs generated directly from the spec.
3. **Per-language implementations.** Starting with `go/` (the reference
   implementation), each library is validated by CI against the exact same
   vector set.

These layers **lock each other**: CI fails if an implementation drifts from
the vectors, if the vectors drift from the spec, or if the spec is changed
without a corresponding vector update.

## Language matrix

| Language | Directory | State |
|----------|-----------|-------|
| Go | `go/` | Usable (`v1.0.0`) |
| Rust | `rust/` | Usable (`v1.0.0`) |
| Python | `python/` | Usable (`v1.0.0`) |
| C++ | `cpp/` | Usable (`v1.0.0`) |

## Directory overview

```
spec/      Protocol-conversion specification
vectors/   Golden test vectors generated from the spec
go/        Go reference implementation (v1.0.0)
docs/      Design docs and the release checklist
rust/      Rust implementation (v1.0.0)
python/    Python implementation (v1.0.0)
cpp/       C++ implementation (v1.0.0)
```

## Current capabilities

The multi-language implementations convert between each protocol face and a
shared intermediate representation (IR):

| Conversion | Nonstream | Streaming |
|------------|-----------|-----------|
| Chat Completions ↔ IR | requests and responses | text events + `tool_calls` argument aggregation |
| Responses ↔ IR | requests and responses | text events + function-call argument aggregation |
| Anthropic Messages ↔ IR | requests and responses | text events + `input_json_delta` aggregation |
| Any face → any face | two-step composition (below); locked by cross vectors | caller-composed via IR events (`Feed` → `Apply`) |

Semantic gaps are never silent: every conversion also returns an ordered
loss list describing what could not be carried
([spec/02](spec/02-loss-policy.md)).

## Quick start

### Go

Requires Go 1.23+. See [`go/README.md`](go/README.md) for package details.

```bash
go get github.com/elkpi/oxa/go
```

Any face pair composes through the IR in two steps:

```go
import (
    messages "github.com/elkpi/oxa/go/anthropic/messages"
    "github.com/elkpi/oxa/go/openai/chatcompletions"
)

irReq, losses, err := chatcompletions.DecodeRequest(ccRequest)
if err != nil { /* structural input error */ }
anRequest, encodeLosses, err := messages.EncodeRequest(irReq)
losses = append(losses, encodeLosses...)
```

A complete, compile-verified version of this example lives at
[`go/openai/chatcompletions/example_test.go`](go/openai/chatcompletions/example_test.go)
(it is also visible in the godoc of that package).

### Other languages

- **Rust**: workspace in [`rust/`](rust/README.md), pure in-process conversion crates targeting spec 0.1.0 baseline.
- **Python**: PEP 621 package in [`python/`](python/README.md), pure Python standard library with zero runtime dependencies.
- **C++**: standard C++20 library in [`cpp/`](cpp/README.md), zero third-party runtime dependencies and exception-free error handling.

## Documentation

- [spec/README.md](spec/README.md) — the specification: reading order and
  source-of-truth rules
- [vectors/README.md](vectors/README.md) — golden vectors and the
  normative comparison rules
- [CHANGELOG.md](CHANGELOG.md) — notable changes
- [docs/release-checklist.md](docs/release-checklist.md) — release
  preconditions

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Note that we do not accept
new-language skeleton PRs before the v1 spec freeze.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
