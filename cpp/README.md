# oxa — C++ Reference Implementation

Pure, in-process protocol-conversion library for OpenAI Chat Completions,
OpenAI Responses, and Anthropic Messages in C++20.

Like the Go, Rust, and Python implementations, oxa in C++ is not a proxy, HTTP
client, router, retry layer, authentication service, or model capability
database. It provides deterministic, pure conversions between protocol wire
structures and oxa's shared Intermediate Representation (IR).

## Architecture

- **Hub-and-spoke**: Spoke namespaces (`oxa::openai::chatcompletions`,
  `oxa::openai::responses`, `oxa::anthropic::messages`) implement only
  `face → IR` and `IR → face`. No direct face-to-face conversions.
- **Zero third-party runtime dependencies**: Standard C++20 STL only
  (`<string>`, `<vector>`, `<variant>`, `<optional>`, `<memory>`, etc.).
- **Exception-free error handling**: Uses `oxa::Status` and `oxa::StatusOr<T>`
  (Abseil style) with `-fno-exceptions` support across the entire library.
- **Dedicated lightweight JSON**: Self-contained `oxa::json::Value` with source
  span tracking, guaranteed raw JSON string preservation (spec/01 INV-1),
  integer/float distinction (INV-7), and duplicate object key rejection.
- **Opaque tool inputs (INV-1)**: Tool call arguments and streaming delta
  fragments remain unparsed raw JSON text.
- **Shared Golden Vectors**: Conforms to the exact same shared `vectors/` golden
  suite (45 Chat Completions + 41 Anthropic + 58 Responses + 20 cross-protocol
  + 21 stream = 185 golden vectors) as Go, TypeScript, Rust, and Python.

## Building and Testing

Requirements: CMake 3.20+ and a C++20 compliant compiler (GCC 11+, Clang 14+,
AppleClang 14+, or MSVC 19.30+).

```bash
# Configure CMake
cmake -B cpp/build -S cpp

# Build library and test suite
cmake --build cpp/build --parallel

# Run all tests
ctest --test-dir cpp/build --output-on-failure
```

## Installation and Downstream Consumption

oxa provides standard CMake package configuration targets (`oxa::oxa`):

```bash
# Install to desired prefix
cmake --install cpp/build --prefix /path/to/prefix
```

Downstream CMake projects can consume oxa via `find_package`:

```cmake
find_package(oxa CONFIG REQUIRED)
target_link_libraries(your_app PRIVATE oxa::oxa)
```

## Vectors Location Convention

Test harnesses locate the shared `vectors/` suite by walking up parent
directories from `cpp/` until finding a directory containing both `vectors/`
and `.git/`. Tests skip cleanly if the repository root is absent (e.g. when
building a standalone package archive).

## 原始 JSON 入口

各 face 的 request/response converter 保留既有 `std::string_view` overload；
stream decoder 新增 `feed(std::string_view)`。解析后的 `json::Value` 也执行字段审计；
有源文本时按节点 span 合并 losses，并保留 Anthropic tool input 的 `raw_slice()`。
没有 source span 的程序内构造对象只保证确定性与 canonical encoding。

`ir::dump_request` 返回 `json::Value`，不提供错误返回值；对于直接构造的 IR，调用方应先调用 `ir::validate_tool_strict`。三个 face 的 request encoder 会执行该校验。对非法的非 function 工具，`dump_request` 保留 `strict` 而不是静默删除，因此结果不符合 IR schema，`ir::load_request` 会拒绝它。
