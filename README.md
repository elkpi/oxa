# oxa

Protocol conversion between the OpenAI and Anthropic APIs, as pure
in-process libraries.

[![CI](https://github.com/elkpi/oxa/actions/workflows/ci.yml/badge.svg)](https://github.com/elkpi/oxa/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

**Latest coordinated release: v2.4.7 (v2.4.8 prepared).** Implements Spec 2.4.8 across Go,
TypeScript, Rust, Python, and C++, and validates all 359 shared golden vectors.

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

| Language   | Directory | Source | Latest published |
| ---------- | --------- | ------ | ---------------- |
| Go         | `go/`     | 2.4.8  | 2.4.7            |
| TypeScript | `ts/`     | 2.4.8  | 2.4.7            |
| Rust       | `rust/`   | 2.4.8  | 2.4.7            |
| Python     | `python/` | 2.4.8  | 2.4.7            |
| C++        | `cpp/`    | 2.4.8  | 2.4.7            |

## Directory overview

```
spec/      Protocol-conversion specification
vectors/   Golden test vectors generated from the spec
go/        Go reference implementation (v2.4.8 prepared)
ts/        TypeScript implementation (v2.4.8 prepared)
docs/      Design docs and the release checklist
rust/      Rust implementation (v2.4.8 prepared)
python/    Python implementation (v2.4.8 prepared)
cpp/       C++ implementation (v2.4.8 prepared)
```

## Current capabilities

The current source tree implements Spec 2.4.8 across Go, TypeScript, Rust,
Python, and C++, converting each protocol face through a shared intermediate
representation (IR). Coordinated release v2.4.8 is prepared.

| Conversion              | Nonstream                                                                  | Streaming                                                   |
| ----------------------- | -------------------------------------------------------------------------- | ----------------------------------------------------------- |
| Chat Completions ↔ IR   | requests and responses (reasoning, usage, structured output formats)       | text + thinking/signature events + `tool_calls` aggregation |
| Responses ↔ IR          | requests and responses (reasoning, usage, structured output formats)       | text + thinking/signature events + function-call aggregation |
| Anthropic Messages ↔ IR | requests and responses (thinking, usage; structured formats reported lost) | text + thinking/signature events + `input_json_delta` aggregation |
| Any face → any face     | two-step composition (below); locked by cross vectors | caller-composed via IR events (`Feed` → `Apply`)            |

Semantic gaps are never silent: every conversion also returns an ordered
loss list describing what could not be carried
([spec/02](spec/02-loss-policy.md)). Responses `developer` messages retain
instruction text and ordering in the IR system field. The current IR cannot
retain the developer/system role distinction, so each developer message
records an explicit `degraded` loss.

## 原始 JSON 与业务运行时边界

接收 provider JSON 时，优先调用 raw JSON 入口，再组合 IR → target face。
Go/Rust 的旧 typed API 无法恢复已经被调用方反序列化丢弃的属性；只有保留完整源输入，
才能报告未知字段并保证 opaque tool input 的原文保真。TypeScript 的 raw 入口复用
`parseJson`，不要先经过会丢数值精度的原生 `JSON.parse`。

| Language | Request / response JSON entrypoints | Stream JSON entrypoint |
| -------- | ----------------------------------- | ---------------------- |
| Go | `DecodeRequestJSON` / `DecodeResponseJSON` | `FeedJSON` |
| TypeScript | `decodeRequestJSON` / `decodeResponseJSON` | `feedJSON` |
| Rust | `decode_request_json` / `decode_response_json` | `feed_json` |
| Python | `decode_request_json` / `decode_response_json` | `feed_json` |
| C++ | existing `std::string_view` decode overloads | `feed(std::string_view)` |

OXA IR 是转换用的受限投影，不是业务运行时的完整模型。它没有运行时 error event、
server/custom-tool 生命周期或 compaction 语义；`MessageDone` 只表示 IR 流结束，
不代表上游请求成功。业务侧应在原始输入上做准入检查，并自行保留失败、取消、会话状态
和 provider 扩展；不要把空 loss 列表当成完整准入或兼容性证明，也不要把未映射字段
盲目透传到目标 API。

## Quick start

### Go

Requires Go 1.23+. See [`go/README.md`](go/README.md) for package details.

```bash
go get github.com/elkpi/oxa/go/v2
```

Any face pair composes through the IR in two steps:

```go
import (
    messages "github.com/elkpi/oxa/go/v2/anthropic/messages"
    "github.com/elkpi/oxa/go/v2/openai/chatcompletions"
)

irReq, losses, err := chatcompletions.DecodeRequest(ccRequest)
if err != nil { /* structural input error */ }
anRequest, encodeLosses, err := messages.EncodeRequest(irReq)
losses = append(losses, encodeLosses...)
```

A complete, compile-verified version of this example lives at
[`go/openai/chatcompletions/example_test.go`](go/openai/chatcompletions/example_test.go)
(it is also visible in the godoc of that package).

### TypeScript

Requires Node.js 20 or later. The ESM-only package has no runtime dependencies.
See [`ts/README.md`](ts/README.md) for complete entry points and streaming usage.

```bash
npm install @elkpi/oxa
```

### Other languages

- **Rust**: workspace in [`rust/`](rust/README.md), published as `elkpi-oxa` on crates.io (`use oxa::...`) alongside modular `oxa-*` crates; latest published version `2.1.1`; its IR contract emits `specVersion: 0.2.0` and dual-reads `0.1.0`, and production crates use `serde` and `serde_json`.
- **Python**: PEP 621 package in [`python/`](python/README.md), published as `elkpi-oxa` on PyPI (`import oxa`), pure Python standard library with zero runtime dependencies.
- **C++**: standard C++20 library in [`cpp/`](cpp/README.md), zero third-party runtime dependencies and exception-free error handling.

## Documentation

- [spec/README.md](spec/README.md) — the specification: reading order and
  source-of-truth rules
- [vectors/README.md](vectors/README.md) — golden vectors and the
  normative comparison rules
- [CHANGELOG.md](CHANGELOG.md) — notable changes
- [docs/release-checklist.md](docs/release-checklist.md) — release
  preconditions

## Downstream verification

The repository keeps executable clean-consumer fixtures under
[`ci/consumers/`](ci/consumers/). They build and install the four non-TypeScript language artifacts outside the
source tree, then run a minimal conversion and streaming consumer. TypeScript
tarball contents and a clean ESM/type-checking consumer are validated by
`npm run test:package` and `npm run test:consumer` in `ts/`. The bounded cross-language stream replay is under
[`ci/reliability/`](ci/reliability/), using the shared corpus in
[`testdata/stream-fragment-corpus.json`](testdata/stream-fragment-corpus.json).

These checks validate packaging and compatibility but do not publish to a
registry. Rust production crates use `serde` and `serde_json`; see
[`rust/README.md`](rust/README.md) for the dependency boundary.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). With v1 frozen, behavior changes still
require the `spec → vectors → implementation` order and an explicit
compatibility review.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
