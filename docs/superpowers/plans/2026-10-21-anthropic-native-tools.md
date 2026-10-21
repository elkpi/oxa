# Anthropic 原生工具声明 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复合法 Anthropic 原生工具声明导致解码失败的问题，准备五语言 2.4.5。

**Architecture:** 继续 face → IR → face，新增来源限定的 native Tool 变体，以字符串保存完整原始声明对象。Anthropic 恢复同来源声明；其他目标面省略并记录损失。独立于 NativeAnnotation，不扩展响应或流式。

**Tech Stack:** Go、TypeScript、Python、Rust、C++，JSON Schema，共享黄金向量。

**Spec:** `docs/superpowers/specs/2026-10-21-anthropic-native-tools-design.md`、`spec/16-native-tool-declarations.md`。

## Global Constraints

- 规范 → 黄金向量 → 实现；IR contract 标记保持 0.2.0。
- native 工具仅有 type=native、name、source、definition；原始 definition 为 JSON 对象文本，内部 type/name 必须合法且与投影 name 一致。
- Anthropic 非空且非 function/custom 的声明 type 是 provider-native，不据此判断执行位置；不做模型能力和 type 版本白名单。
- 原有 function/custom/apply_patch 语义不变；普通缺 schema 请求仍失败。
- HTTP header、Advisor 内容块、响应和流式不在范围；不引入网络依赖。
- 不 commit、push 或发布。临时日志在仓库外；工作单元及其验收由外部 manifest 记录。

## Review Focus

1. 混合工具中原生声明含未知配置或非规范数值拼写：保留完整声明，不能字段审计删除后再保存。
2. 普通 function 缺 input_schema 或 discriminator/name 类型错误：仍须结构失败，不能全局放宽校验。
3. 强制 native tool_choice 或 any 的全部声明被丢弃：输出不得包含悬空 choice。
4. 手工构造 IR 的空来源、非对象 definition、不同 name、function 类型携带 native 字段：结构验证一致。
5. 原生声明内 cache_control 与 model：声明整体保留，不重复生成 native annotation，不把嵌套 model 交给顶层 modelmap。

---

### Task 1: 锁定契约

**Files:** `spec/16-native-tool-declarations.md`；修改 `spec/01-intermediate-representation.md`、`spec/10-mapping-openai-chat-completions.md`、`spec/11-mapping-openai-responses.md`、`spec/12-mapping-anthropic-messages.md`、`spec/schema/ir.schema.json`、`spec/README.md`。

**Interfaces:** Tool 新增可选 Source/Definition，Type=native；ToolChoice 增加 tool_type=native。definition 为 JSON 原始对象文本字符串。原生声明缺失能力的 loss 采用 spec/16 固定文本。

- [ ] 定义 named-native 声明、保真边界、跨面 loss、choice 省略和严格错误规则。
- [ ] 扩展 schema 联合体；确认原有变体拒绝 source/definition。
- [ ] 运行 JSON 解析检查并检查设计覆盖。

### Task 2: 黄金向量与 RED

**Files:** 新增 `vectors/anthropic/nonstream/native-tool-*.json`、`vectors/cross/nonstream/anthropic-*-native-tools.json`；更新 `vectors/manifest.json`。

**Interfaces:** 沿用现有 vector 格式；definition 字符串精确对应原始 wire tool 对象文本。

- [ ] 新增 Advisor/客户端内置/未来类型 to-ir、from-ir 及混合声明向量。
- [ ] 新增两个 OpenAI 目标、同来源回转、强制 choice 及全部声明丢弃的向量。
- [ ] 更新 manifest；`go -C go run ./cmd/veccheck -root .. -write-manifest` 通过结构检查。
- [ ] `go -C go test -count=1 ./anthropic/messages ./internal/vectest` 必须因新 native 行为失败，记录 RED。

### Task 3: Go 参考实现

**Files:** `go/ir/{request.go,json.go,custom.go,native_tool.go,native_tool_test.go}`；`go/anthropic/messages/{types.go,rawjson.go,decode.go,encode.go,native_tool.go,native_tool_test.go}`；`go/openai/{chatcompletions,responses}/encode.go`。

**Interfaces:** `ir.ToolTypeNative`、Tool.Source/Definition；`ir.ValidateNativeTool(Tool) error`；`ir.IsNativeToolChoice(*Request) bool`。ToolWire 原始声明载体及 JSON 编解码不得擦除配置。

- [ ] 编写 Advisor 最小复现、原始 payload/IR round-trip、混合工具、modelmap 不修改嵌套模型的测试。
- [ ] 编写普通 schema 错误、无效 IR definition 和 named choice 的回归断言，先运行确认失败。
- [ ] 实现 native variant、IR JSON、wire 保真与 raw audit 边界，三个 encoder 的省略及 choice 保护。
- [ ] 运行完整 Go tests、vet 和 gofmt 检查；共享向量全部 GREEN。

### Task 4: TypeScript

**Files:** `ts/src/ir/{types.ts,codec.ts,nonstream.ts,constants.ts}`；`ts/src/anthropic/messages/{nonstream.ts,audit.ts}`；`ts/src/openai/{chatcompletions,responses}/nonstream.ts`；`ts/test/native-tools.test.ts`。实际文件位置以对应导出定位为准，不创建并行替代转换器。

**Interfaces:** 与 Task 1 相同 JSON 契约；使用现有 RawDocument/原始 JSON 工具取得并回写声明原文。

- [ ] 新增针对 Review Focus 1–5 的回归测试，确认 RED。
- [ ] 扩展工具 union/codec/三面请求转换及审计。
- [ ] 运行 typecheck、Node tests 和共享向量。

### Task 5: Python

**Files:** `python/src/oxa/ir/{types.py,codec.py,constants.py}`；`python/src/oxa/anthropic/messages/{decode.py,encode.py}`；`python/src/oxa/_wire*.py`；两个 OpenAI 请求 encoder；`python/tests/test_native_tools.py`。

**Interfaces:** 相同 Tool JSON 契约，RawDocument 原文和 RawJSON 回写模式不变。

- [ ] 新增原文、错误、choice、嵌套配置回归测试并确认 RED。
- [ ] 实现 variant、codec、来源声明保留和三个 encoder 分支。
- [ ] 运行 `PYTHONPATH=python/src python3 -m unittest discover -s python/tests`。

### Task 6: Rust

**Files:** `rust/crates/oxa-ir/src/{request.rs,codec.rs}`；`rust/crates/oxa-anthropic/src/{types.rs,rawjson.rs,audit.rs,decode.rs,encode.rs}`；两 OpenAI crate encoder；对应 tests。

**Interfaces:** ToolType::Native 及可选 source/definition；共享 JSON/loss 契约。

- [ ] 先运行新向量确认 RED，补充 malformed 和原文保真测试。
- [ ] 实现 serde/validator、raw cleaner 的 opaque 声明边界和请求映射。
- [ ] 运行 workspace tests、fmt、clippy。

### Task 7: C++

**Files:** `cpp/include/oxa/ir.hpp`；`cpp/src/ir*.cpp`；`cpp/src/anthropic/{messages.cpp,messages_audit.hpp}`；两个 OpenAI encoder；`cpp/tests/test_native_tools.cpp`、测试注册处。

**Interfaces:** 相同 Tool variant/JSON，现有 json::Value 原文边界和 no-exceptions Status 模式。

- [ ] 新向量 RED；新增原文、choice 和 malformed IR 回归测试。
- [ ] 实现字段、codec、审计 opaque profile 和三个请求 encoder。
- [ ] CMake build/CTest 通过；验证 no-exceptions 构建。

### Task 8: 验收与 2.4.5 准备

**Files:** 根及 spec CHANGELOG/README、`ts/package*.json`、`python/pyproject.toml`、`rust/Cargo.toml`/lock、`cpp/CMakeLists.txt` 及既有版本记录。

**Interfaces:** 已验证的五语言实现和共享向量；不消费未验证的部分结果。

- [ ] 检查所有新增向量、loss 顺序和源下标；核查 Review Focus 五项均有测试。
- [ ] 执行五语言完整验证及 vector manifest 检查；记录实际命令/结果，明确不可运行的平台矩阵。
- [ ] 更新 2.4.5 版本及文档计数，注明 native 是新增穷尽匹配分支，未实现 Advisor 内容块。
- [ ] 自审当前 diff，运行 `git diff --check`，确认无临时文件和后台进程；不执行 commit/tag/publish。
