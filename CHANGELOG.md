# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v2.4.5] - 2026-10-21

### Fixed

- Anthropic 原生命名工具声明保真与跨面损失报告（spec/16 / N-NT-1..4）：
  - 修复 Anthropic Messages 解码路径无条件要求工具声明携带 `input_schema`，导致合法的 Anthropic Advisor、服务端工具及客户端内置工具（如 `advisor_20260301`、`bash_20250124` 等）无法通过解码、直接报错且无 fallback losses 的阻断性问题。
  - 在 IR 中扩展来源限定的 `Tool[type=native]` 变体，字段为 `name`、`source`、`definition`，以完全不透明的原始 JSON 对象文本保存声明内容（含嵌套模型、最大调用次数、缓存及厂商私有配置），IR contract 保持 `0.2.0`。
  - 在 Anthropic Messages 解码阶段将带非空且非 function/custom 的 `type` 命名工具识别为 native 工具并完整保留原文；普通 function 工具维持严格对象 `input_schema` 检查，非字符串 `type`（含 null）与非对象声明维持结构错误拒绝。
  - 同协议回转时完整恢复 Anthropic 原生声明配置；跨协议编码至 OpenAI Chat Completions 或 OpenAI Responses 时，逐声明省略并产生精确自描述的 `unsupported-semantic` 损失；指向被省略原生声明的命名 `tool_choice` 联动省略并报损，避免悬空调用。
  - 在 Go、TypeScript、Python、Rust、C++ 五种语言中同步实现上述契约与字段审计规则，并通过 15 条新增共享黄金向量（涵盖 Advisor、内置工具、扩展配置、混合声明、同源往返与跨面报损，向量总数达 345）。
- 文档与版本状态：
  - 发布 Spec 16 原生工具声明规范，更新 Spec 版本至 `2.4.3`；更新 README、规范说明与各语言版本状态矩阵至 `v2.4.5` 准备态，标明最新已发布版本为 `v2.4.3`。

## [v2.4.4] - 2026-10-21

### Fixed

- OpenAI Responses 推理摘要 `summary_text` 解码保真（spec/11 §4 / N-R-13）：
  - 修复 Go 参考实现中 `OutputContent` 的 raw JSON 变体 schema 仅识别 `output_text` 而未包含 `summary_text`，导致 `DecodeRequestJSON` 与 `DecodeResponseJSON` 在原始 JSON 过滤阶段将 `summary[].text` 静默抹除且不报损的问题。
  - 在 Go `types.go` 补齐 `PartTypeSummaryText = "summary_text"` 常量定义，并在 `rawjson.go` 中将 `OutputContent` schema 变体对齐为同时接受 `output_text` 与 `summary_text`。
  - 补充请求侧、响应侧及多块混合场景下保留 `summary_text` 文本且与类型化解码保持一致的回归测试。
  - 对齐 TypeScript、Rust、Python、C++ 的非流式响应解码，使其与 Go 一样保留标准 `summary_text`，并继续兼容 `output_text`。
  - 统一五种语言的摘要边界报损：缺失或未知 `type` 跳过该摘要并产生一条逐项 `unsupported-semantic`，不生成空 ThinkingBlock；已识别摘要中的非空 `annotations` 产生一条 `unmapped-field`，空 annotations 继续免报损。
  - 新增 12 条共享黄金向量，覆盖请求和响应的标准摘要、混合摘要、未知类型、缺失类型、annotations 与未知扩展字段，总向量数为 330；Go 增加 typed/raw 完整 IR 与 loss 一致性测试。
  - 编码器、流式行为和既有 `id` / `status` / `encrypted_content` 等元数据报损策略保持不变。
- 文档与版本状态：
  - 更新项目 README、规范说明与各语言版本状态矩阵至 `v2.4.4` 准备态，标明最新已发布版本为 `v2.4.3`。

## [v2.4.3] - 2026-10-19

### Fixed

- Anthropic Messages 原生请求系统块缓存控制回转保真（spec/15 §3.2 / N-AN-14）：
  - 修复 Go 参考实现中 `getBlockCacheControl` 缺少 `ir.SystemBlock` 类型分支，导致 `system[]` 块上的 `cache_control` 在 Anthropic → IR → Anthropic 同协议往返编码时静默丢失的问题。
  - 为 Go 模块补充单个/多个系统块不同缓存配置（TTL 省略、5m、1h）以及直接构造 IR 系统块的往返回归测试。
- 文档与版本状态：
  - 更新项目 README、规范说明与各语言版本状态矩阵至 `v2.4.3` 准备态，标明最新已发布版本为 `v2.4.2`。

## [v2.4.2] - 2026-10-18

### Fixed

- 跨面通用遗漏报损缺省保障（spec/15 §4.1）：
  - 修复直接构造的 IR 或旧版 IR 在省略 `omission` 或其 `detail` 为空时，跨面编码至 Chat Completions / Responses 缺失自描述遗漏报损的问题。
  - 在 Go、TypeScript、Python、Rust、C++ 五门语言的通用遗漏收集 helper 中统一为 `cache_control` 提供缺省自描述 `unmapped-field` 报损与规范 `detail`。
  - 为五门语言参考实现补齐直接构造 IR 与空 detail 场景的显式断言测试。
- 依赖管理：
  - 恢复 `rust/Cargo.lock` 中意外升级的 7 个无关依赖版本，严格保持最小补丁边界。
- 文档状态：
  - 更新项目 README 与各语言版本状态矩阵至 `v2.4.2` 准备态，标明最新已发布版本为 `v2.4.1`。

## [v2.4.1] - 2026-10-18

### Added

- Anthropic `cache_control` 跨面遗漏自描述说明（spec/15 §3.2, §4）：
  - 当携带 `cache_control`（顶层、system 块、消息 content 块、嵌套 tool_result、tools）的 Anthropic 请求跨面编码至无 cache 断点表示的目标面（Chat Completions / Responses）时，`unmapped-field` 遗漏记录填充规范化 `detail`:
    `"anthropic-native cache breakpoint; the target face has no cache_control representation"`。
  - 明确该遗漏属于协议能力缺口（capability gap，目标面 wire 无法表达），而非实现缺失；调用方可据此按意图放行或区分该遗漏。
  - 保持目标面 wire 结构、loss 数量/path/field/reason 及 Anthropic 内部无损回转不变。
- 新增跨协议断点黄金向量 `cross.nonstream.anthropic-to-chatcompletions-cache-breakpoints-request`（总向量数扩充至 318 条，spec 0.2.0 向量达到 168 条）。
- Go、TypeScript、Python、Rust、C++ 五门语言参考实现 100% 保持一致与通过验证。

## [v2.4.0] - 2026-10-18

### Added

- Anthropic Messages 完整请求配置保真与原生回转（spec/15, N-AN-14）：
  - 推理模式保真：`thinking.type: "adaptive"`、`"disabled"` 与 `"enabled"` 完整保留至 IR `NativeAnnotation`，解决以前 adaptive thinking 被静默丢弃的问题。
  - 推理预算保真：`thinking.budget_tokens` 结合通用 effort 进行双向保真与降级覆盖去重；中间调用方修改 effort 后新值在输出时生效（显式编辑优先原则）。
  - 推理显示配置保真：支持 `thinking.display`（`summarized`、`omitted`、`updates`）解析与原生恢复。
  - 显式推理配置保真：支持 `output_config.effort`（`low`、`medium`、`high`、`xhigh`、`max`）解析与映射。
  - Prompt Caching 保真：支持顶层、system 块、消息 content 块及 tools 各层 `cache_control`（`ephemeral`，含 `ttl: "5m"/"1h"`）解析与原生恢复，解决以前 cache_control 被误判为 unmapped-field 的阻断性问题。
  - Context Management 保真：支持官方 Beta 特性 `context_management.edits`（`clear_tool_uses_20250919`、`clear_thinking_20251015`）解析与原生恢复。
- 通用 IR `Params.ReasoningEffort` 扩展支持 `xhigh` 与 `max`，在三大协议面（Anthropic、Chat Completions、Responses）间对等映射。
- 中立遗漏报损（Omission Metadata）：编码到不支持特性的目标 spoke（Chat Completions / Responses）时，由通用中立 helper 依据预存元数据在编码阶段准确报损。
- 请求专用 Schema 与上下文隔离：通过 `$defs/requestBlock` 确保 native 注解仅存在于请求侧，严格禁止污染响应块和流式事件。
- 新增 36 条原生请求配置黄金向量（总数从 281 条扩充至 317 条，spec 0.2.0 向量达到 167 条）。
- Go、TypeScript、Rust、Python、C++ 五门语言参考实现 100% 验证通过。

### Changed

- 协调版本号升级为 `v2.4.0`（次版本号升级），IR contract 维持 `0.2.0`（双向兼容读取 `0.1.0`）。

## [v2.3.1] - 2026-10-17

### Added

- OpenAI Chat Completions 官方原生 Custom Tools 支持（N-CC-14）：
  - 工具声明：支持 `type: "custom"` 工具规范（`name`、`description`、`format`）。
  - 工具选择：支持 `tool_choice: {type: "custom", custom: {name: "..."}}` 映射至 IR `ToolChoice`。
  - 工具调用：支持 `tool_calls[i]: {id, type: "custom", custom: {name, input}}` 双向保真映射至 `CustomToolUseBlock`。
  - 工具结果：支持 `role: "tool"` 消息承载字符串或 block 列表输出，正确映射至 `CustomToolResultBlock`。
  - 流式增量：`ChoiceDelta.tool_calls` 支持增量发射与解析 `custom.input` 片断，结束于 chunk `finish_reason: "tool_calls"`。
- Anthropic Messages 单属性结构化包装 Custom Tools 支持（N-AN-13）：
  - 工具声明：生成单属性 `input_schema: {type: "object", properties: {input: {type: "string"}}, required: ["input"]}` 包装。
  - 模型调用：文本输入无损包装为 `tool_use.input: {input: "<raw_text>"}`，解码时自动提取识别为 `CustomToolUseBlock`。
  - 工具结果：`tool_result.content` 承载纯文本或 block 列表，与调用 ID 关联解码为 `CustomToolResultBlock`。
  - 流式解包与回放：解码端缓冲 `input_json_delta`，闭合时解包提取原始文本并回放 `CustomToolUseBlock` 与 `CustomToolInputDelta`；编码端将 `CustomToolInputDelta` 包装发射为紧凑型 `input_json_delta`（`{"input":"..."}`）。
- 新增 18 条 Custom Tools 黄金向量（含 2 条跨协议跨模式向量），黄金向量总数由 263 条扩展至 281 条（其中 spec 0.2.0 向量达到 130 条）。
- Go、TypeScript、Python、Rust、C++ 五门语言参考实现 100% 验证通过。

### Changed

- 版本号升级为 `v2.3.1`（修订版本），IR contract 维持 `0.2.0`（双向兼容读取 `0.1.0`）。
- Chat Completions 与 Anthropic Messages 面对 Apply Patch 继续保持规范丢弃降级并记录 `unsupported-semantic` loss。

## [v2.3.0] - 2026-10-16

### Added

- OpenAI Responses Apply Patch 端到端实现（N-AP-1 ~ N-AP-5）：支持 `apply_patch` 专用声明、选择、调用与结果项。
- 引入强类型 `ApplyPatchOperation` 判别联合体（`create_file`、`delete_file`、`update_file`），严格禁止对 `delete_file` 附带 diff，保证 `path` 与 `diff` 不透明文本保真。
- 引入 `ApplyPatchToolUseBlock` 与 `ApplyPatchToolResultBlock`，解耦调用状态（`in_progress` / `completed`）与结果状态（`completed` / `failed`）。
- 实现同轮次请求项成对关联校验（N-AP-3, INV-3）与成对语义过滤；未知操作类型或未知状态成对丢弃并记录对应 loss，杜绝悬垂引用。
- 实现 Responses Apply Patch 原子项流式生命周期（N-AP-4），added 时暂存快照，done 时验证并回放单组 block start/stop，严禁虚假增量。
- 实现 Chat Completions 与 Anthropic Messages 面对 Apply Patch 的规范降级（N-AP-5），记录 `unsupported-semantic` loss 并正确降级 `stop_reason` 至 `end_turn`。
- 新增 58 条 Apply Patch 黄金向量，全仓库向量总数达到 263 条，覆盖 Go、TypeScript、Python、Rust、C++ 五门参考语言。

### Changed

- 版本号升级为 `v2.3.0`（次版本迭代），IR contract 维持 `0.2.0`（双向兼容读取 `0.1.0`）。
- Go、TypeScript、Python、Rust、C++ 五语言实现包版本统一升级为 `2.3.0`。

## [v2.2.0] - 2026-10-16

### Added

- Responses Custom Tools：独立 IR 调用、结果和文本 Delta；支持 text/Lark/regex format、custom tool_choice、call_id 关联及任意文本输入保真。
- 字符串与 text/image 内容列表结果保持区别；不能表达的文件等内容显式报告 loss；流式支持 delta/done、快照一致性校验及无 delta 回退。
- 新增 20 个共享向量，总数 205；目标 profile 暂不支持 custom 时显式降级，不将文本伪装成 function arguments。
- 计划在后续版本（如 v2.3.0）补齐 Chat Completions 与 Anthropic Messages 的 Custom Tools 支持。

### Changed

- 规范版本升级为 `2.2.0`，IR contract 保持 `0.2.0`（兼容读取 `0.1.0`）；遵循大的架构重构升级高位版本、同一框架下的补充扩展升级中间版本、BUG与小feature升级小版本的版本准则。
- Go、TypeScript、Rust、Python、C++ 协调准备版本为 `2.2.0`。

## [v2.1.2] - 2026-10-15

### Fixed

- 五种语言的 Responses 请求解码支持 `developer` 消息，包括字符串和文本 content parts；`instructions` 在前，`system`/`developer` 按共同输入顺序进入 IR system 区域，保持高于 user 的指令优先级。
- 现有 IR 无法区分 developer/system，每条 developer 消息显式记录 `input[i].role` 的 `degraded` loss；转换到 Chat Completions 或 Anthropic 时保留指令内容和该 loss。
- 仅含指令的 Responses 请求补充一条空 user 消息以满足 IR 约束，并记录额外的 `input` degradation，不再因缺少普通会话消息而拒绝转换。
- 新增 12 条共享 vectors，向量总数从 173 增至 185；IR schema 与 contract version 不变。

### Changed

- Go、TypeScript、Rust、Python、C++ 发行版本统一准备为 `2.1.2`；Spec 保持 `2.1.0`，IR contract 保持 `0.2.0`，Go module path 仍为 `github.com/elkpi/oxa/go/v2`。

## [v2.1.1] - 2026-10-14

### Fixed

- 五种语言对原始 provider JSON 的未知字段按属性存在记录 `unmapped-field`，包含嵌套属性、`null` 和 `store:false`；opaque payload 与整体 unsupported unit 不重复递归报损。
- Anthropic 请求的 `tool_use.input` 保留原始空白、键序、转义和数字拼写，避免 Go 大整数精度变化，以及 Rust/Python/C++ 的重新序列化。
- Python 按完整对象/数组路径定位 tool input，修复字段顺序变化时两个工具调用共享错误参数的问题。
- 共享套件增加到 173 vectors，并校正两条 cross vectors 的原始参数文本期望；IR contract 仍为 `0.2.0`，Spec 仍为 `2.1.0`。
- README 正确区分已发布的 v2.1.0 与未发布的 v2.1.1 源码。

### Added

- Go、TypeScript、Rust、Python 的 raw JSON request/response 入口和流事件入口；C++ 保留已有 raw nonstream overload，并新增 raw streaming overload。

## [v2.1.0] - 2026-09-27

### Added

- Spec 2.1.0 structured-output support across Go, TypeScript, Rust, Python, and C++.
  - Adds `Params.ResponseFormat` with `text`, `json_object`, and `json_schema` formats while retaining IR document `specVersion: "0.2.0"`.
  - Maps Chat Completions `response_format` and Responses `text.format` through IR without loss; Anthropic encoding records one `unmapped-field` loss without synthesizing tools.
  - Expands the shared golden suite from 154 to 163 vectors; all five language implementations pass the new cases.

## [v2.0.0] - 2026-09-26

### Added

- **Spec 2.0.0 Coordinated Milestone Release**: Reasoning Content and Usage Granularity across all five reference implementations (Go, TypeScript, Rust, Python, C++), verified against all 154 golden vectors.
  - IR contract version 0.2.0: first-class `ThinkingBlock`, `ThinkingDelta`, and `SignatureDelta` sealed variants. Dual-read compatibility accepting both `0.1.0` and `0.2.0`.
  - Request reasoning effort: closed enum `minimal`, `low`, `medium`, `high` mapped across all faces.
  - Granular usage: cache read/creation input tokens and token detail breakdowns (`input_tokens_details.cached_tokens`, `output_tokens_details.reasoning_tokens`), preserving absent vs. zero.
  - M9 streaming profile (N-S-11): streaming reasoning deltas, opaque provider signature deltas, and lifecycle closures across OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages spokes.
  - Go (`github.com/elkpi/oxa/go/v2`): module path upgraded to `v2` with full semantic import versioning support.
  - TypeScript (`@elkpi/oxa`): package updated to 2.0.0 with typed root and subpath exports.
  - Rust (`elkpi-oxa`): workspace and 7 modular crates updated to 2.0.0.
  - Python (`elkpi-oxa`): package updated to 2.0.0.
  - C++ (`oxa`): CMake package updated to 2.0.0 with standard and `-fno-exceptions` support.

- Python (Wave 4): Spec 2.0 reasoning content and usage granularity support.
  - Implemented `ThinkingBlock`, `ThinkingDelta`, and `SignatureDelta` across all spokes and IR codec/checker.
  - Added request `reasoning_effort` mapping and dual-read support for IR specVersion `0.1.0` and `0.2.0`.
  - Added granular usage accounting (`cache_read_input_tokens`, `cache_creation_input_tokens`, `input_tokens_details`, `output_tokens_details`).
  - Implemented M9 streaming reasoning and signature lifecycle events across Chat Completions, Responses, and Anthropic Messages spokes.
  - Verified against all 154 golden vectors.

- C++ (Wave 5): Spec 2.0 reasoning content and usage granularity support under C++20 with `-fno-exceptions`.
  - Implemented `ThinkingBlock`, `ThinkingDelta`, and `SignatureDelta` across all spokes and IR codec/checker.
  - Added request `reasoning_effort` mapping, dual-read `specVersion` support, and granular usage details.
  - Implemented M9 streaming reasoning deltas and lifecycle closures across Chat Completions, Responses, and Anthropic Messages spokes.
  - Verified against all 154 golden vectors across standard and `-fno-exceptions` builds.

- Multi-language convergence: all five implementations (Go, TypeScript, Rust, Python, and C++) now validate the full 154 Spec 2.0 golden vector suite.

## [v1.0.1] - 2026-09-17

### Added

- Multi-language constant convergence: synchronized and type-safe IR and spoke protocol constants across Python (`oxa.ir.constants`), TypeScript (`@elkpi/oxa/ir`), C++ (`oxa::*`), Go (`ir.*`), and Rust (`oxa_ir::*`).
- Automated CI constant-drift verification (`scripts/check-constants.py`) ensuring 100% coverage of closed schema enums against `spec/schema/*.json`.
- TypeScript: ESM-only `@elkpi/oxa` package at version `1.0.1`, with typed root and focused subpath exports for the IR, JSON, SSE, stream adapters, and all three protocol faces. It has no runtime dependencies and supports Node.js 20+ and Web runtimes.
- TypeScript packaging gates: exact npm tarball-content validation plus a clean installed consumer that type-checks and imports every public ESM entry point.
- Rust: `elkpi-oxa` umbrella crate with `[lib] name = "oxa"` re-exporting all protocol faces and IR under `use oxa::...`.

### Changed

- Python: distribution package renamed to `elkpi-oxa` on PyPI (`pip install elkpi-oxa`); in-code imports remain `import oxa`.
- Downstream packaging: verified a clean consumer for the `@elkpi/oxa` npm tarball, and `elkpi-oxa` on both Python (wheel & sdist) and Rust.
- Release version 1.0.1 synchronized across Python, TypeScript, Rust, C++, and Go module tags.

## [v1.0.0] - 2026-09-04

### Added

- **oxa 1.0.0 Milestone Release**: At the original 2026-09-04 milestone, all four
  then-shipped reference implementations (Go, Rust, Python, C++)
  implement the complete pure in-process protocol-conversion specification across OpenAI Chat
  Completions, OpenAI Responses, and Anthropic Messages, verified against the identical 125 golden vectors.
  - Go, Python, and C++ have no third-party runtime dependencies; Rust production crates use `serde` and `serde_json`.
  - Hub-and-spoke intermediate representation (IR) with pure face ↔ IR converters.
  - Full non-streaming, streaming (M7 tool-argument aggregation), and cross-protocol conversion support.
  - Opaque tool input handling preserving byte-for-byte fidelity (INV-1).
  - Explicit loss tracking for unmapped fields and semantics (spec/02).
  - Multi-OS continuous integration across Linux (Ubuntu default and `-fno-exceptions`), macOS, and Windows.
  - Downstream packaging support: CMake `install()` and `find_package(oxa CONFIG REQUIRED)` exports for C++,
    workspace package versioning for Rust crates, and PEP 621 package metadata for Python.

## [v0.3.0] - 2026-09-04

### Added

- C++ implementation (`cpp/`): C++20 library (`oxa`) targeting the frozen spec
  baseline with zero third-party runtime dependencies and exception-free error handling.
  - `oxa::status`: Abseil-style `Status`, `StatusOr<T>`, and `Conversion<T>` types
    supporting `-fno-exceptions`.
  - `oxa::json`: lightweight self-contained JSON value type with source span
    tracking, byte-for-byte slice extraction (INV-1), float/int preservation (INV-7),
    and duplicate key rejection.
  - `oxa::ir`: neutral request, response, block, event, and loss types; document
    codec with `specVersion: "0.1.0"`; event-stream grammar (INV-5) and block
    indexing (INV-6) invariant validator.
  - `oxa::modelmap`: model renaming table with identity fallback (spec/03).
  - `oxa::openai::chatcompletions`: nonstream request/response converters, streaming
    `StreamDecoder` with tool call argument aggregation, and `StreamEncoder`.
  - `oxa::anthropic::messages`: nonstream request/response converters, streaming
    `StreamDecoder`, and `StreamEncoder`.
  - `oxa::openai::responses`: nonstream request/response converters, streaming
    `StreamDecoder` with function call argument aggregation, and `StreamEncoder`.
  - `oxa::sse`: standalone byte-level Server-Sent Events frame decoder and encoder
    (spec/20 §6).
  - `oxa::vectest`: test harness running all shared nonstream (105), cross-protocol
    (12), and stream (8) golden vectors against each C++ face with full INV-1
    lexical precision.
  - CI: CMake build and CTest execution on Ubuntu.

## [v0.2.0] - 2026-09-03

### Added

- Python implementation (`python/`): PEP 621 package (`oxa`) targeting the
  frozen spec baseline with zero runtime dependencies.
  - `oxa.ir`: neutral request, response, block, event, and loss types; document
    codec with `specVersion: "0.1.0"`; event-stream grammar (INV-5), block
    indexing (INV-6), and tool-argument concatenation (INV-1) invariant checker.
  - `oxa.modelmap`: model renaming table with identity fallback (spec/03).
  - `oxa.openai.chatcompletions`: nonstream request/response converters, streaming
    `StreamDecoder` with tool call argument aggregation, and `StreamEncoder`.
  - `oxa.anthropic.messages`: nonstream request/response converters, streaming
    `StreamDecoder`, and `StreamEncoder`.
  - `oxa.openai.responses`: nonstream request/response converters, streaming
    `StreamDecoder` with function call argument aggregation, and `StreamEncoder`.
  - `oxa.sse`: standalone byte-level Server-Sent Events frame decoder and encoder with zero
    external dependencies (spec/20 §6).
  - `oxa.vectest`: test harness running all shared nonstream, cross-protocol,
    and stream golden vectors against each Python face with full INV-1 lexical
    precision.
  - CI: matrix testing across Python 3.10, 3.11, 3.12, and 3.13.

## [v0.1.0] - 2026-09-03

### Added

- Rust implementation (`rust/`): Cargo workspace targeting the frozen spec
  baseline with zero cross-face dependencies.
  - `oxa-ir`: neutral request, response, block, event, and loss types;
    document codec with `specVersion: "0.1.0"`; event-stream grammar (INV-5)
    and block indexing (INV-6) invariant checker.
  - `oxa-modelmap`: model renaming table with identity fallback.
  - `oxa-chatcompletions`: nonstream request/response converters, streaming
    `StreamDecoder` with tool call argument aggregation, and `StreamEncoder`.
  - `oxa-anthropic`: nonstream request/response converters, streaming
    `StreamDecoder`, and `StreamEncoder`.
  - `oxa-responses`: nonstream request/response converters, streaming
    `StreamDecoder` with function call argument aggregation, and `StreamEncoder`.
  - `oxa-sse`: byte-level Server-Sent Events frame decoder and encoder with zero
    external dependencies (spec/20 §6).
  - `oxa-vectest`: test harness running all shared nonstream, cross-protocol,
    and stream golden vectors against each Rust face with full INV-1 lexical
    precision.

## [v0.0.1] - 2026-09-02

### Added

- Written protocol-conversion specification (`spec/`): hub-and-spoke IR,
  loss policy, model handling, per-face mappings for Chat Completions,
  Responses, and Anthropic Messages, and streaming semantics.
- Golden vector set (`vectors/`): per-face nonstream and stream vectors,
  cross-protocol (`protocol-to-protocol`) nonstream vectors, validated by
  `cmd/veccheck` and pinned by `manifest.json`.
- Go reference implementation (`go/`): face-neutral `ir` package; three
  protocol faces (`openai/chatcompletions`, `openai/responses`,
  `anthropic/messages`) with nonstream request/response conversion and
  incremental stream decoders/encoders; opaque SSE byte framing (`sse`);
  optional model-name mapping (`modelmap`).
- Streaming tool-argument aggregation: Chat Completions `tool_calls`
  index aggregation, Responses function-call argument deltas, and
  Anthropic `input_json_delta`, preserving tool inputs as opaque raw JSON.
- Public documentation: project README with support matrix and quick
  start, a compile-verified cross-face godoc example, and a release
  checklist.
