oxa
===

**Status: pre-alpha.** Nothing here works yet — see the badges below.

[![Status: pre-alpha](https://img.shields.io/badge/status-pre--alpha-red)]()
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)]()

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
| Go | `go/` | Reference implementation — in progress |
| Rust | `rust/` | Roadmap, post-v1 |
| Python | `python/` | Roadmap, post-v1 (after Rust) |
| C++ | `cpp/` | Roadmap, post-v1 (after Python) |

## Directory overview

```
spec/      Protocol-conversion specification
vectors/   Golden test vectors generated from the spec
go/        Go reference implementation
rust/      Rust implementation (post-v1)
python/    Python implementation (post-v1)
cpp/       C++ implementation (post-v1)
```

## Quick start

Coming soon — the Go reference implementation is not merged yet. Once it
lands, this section will show how to import the converter and translate a
request in a few lines of code.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Note that we do not accept
new-language skeleton PRs before the v1 spec freeze.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
