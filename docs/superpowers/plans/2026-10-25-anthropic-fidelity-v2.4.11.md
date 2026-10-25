# Anthropic 思考历史、请求控制与文本引文保真（v2.4.11）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 oxa 中全面支持 Anthropic `redacted_thinking` 历史块、`between_tools` 思考模式、`top_k` 显式采样保留、`disable_parallel_tool_use` 选项保真及五类文本块 `citations`（含流式 `citations_delta`），在 Go、TypeScript、Python、Rust、C++ 五门语言对齐实现，新增跨面与同面共享黄金向量，并升级版本至 v2.4.11。

**Architecture:** 严格遵循“规范 → 共享黄金向量 → 5 门语言实现”的顺序。IR 扩展独立的 `RedactedThinkingBlock` 和 `TextCitation`/`CitationDelta`，通过 Request 宿主扩展 `top_k` 与 `disable_parallel_tool_use` 原生注解；Anthropic 同面编解码 0 损失往返，转至 OpenAI 目标面时对不可表达的加密历史和引文报精确的 `unsupported-semantic` 损失；保持 IR 0.2.0 contract 兼容与纯内存转换隔离。

**Tech Stack:** Go 1.23+, TypeScript 5.6+, Python 3.10–3.13, Rust 2021 (1.80+), C++20 (CMake/CTest), JSON Schema 2020-12, veccheck.

**Spec:** `docs/superpowers/specs/2026-10-25-anthropic-fidelity-v2.4.11-design.md`

## Global Constraints

- 协议到 IR 映射严格遵守规范先行：`spec/` → `vectors/` → `implementation` 顺序。
- IR contract 保持 `0.2.0` 不变，`specVersion` 保持向后兼容读取 `0.1.0`。
- 本次版本发布目标为 `v2.4.11`，规范版本升级为 `2.4.11`。
- `redacted_thinking.data` 是不透明供应商状态，任何时候禁止解密、解码、明文打印或在测试/诊断输出中回显真实载荷。
- 转至 OpenAI 协议时，禁止把 `redacted_thinking.data` 篡改为可见文本或普通 reasoning_content；每个 redacted 块产生且仅产生一条 `unsupported-semantic` 损失，detail 为 `anthropic encrypted thinking replay state; the target face cannot preserve this block`。
- 转至 OpenAI 协议时，禁止近似转换 Anthropic 引文索引；每个携带非空引文的文本块产生且仅产生一条 `unsupported-semantic` 损失，detail 为 `anthropic text citations; the target face cannot preserve source locations and replay metadata`。
- `between_tools`、`top_k` 和 `disable_parallel_tool_use` 保持独立原生注解，同面 0 损失回转，跨面由原生遗漏逻辑生成 `unmapped-field`。
- 每个 Commit 粒度聚焦、独立且可快速 cherry-pick 到其他分支。

## Review Focus

1. `RedactedThinkingBlock` 在流式生命周期中收到非空的 delta（如 `ThinkingDelta` 或 `TextDelta`）：必须作为结构错误拒绝，不可静默吸纳。
2. `TextBlock.citations` 包含未知 citation `type`：在 wire 解码时跳过该项并记录 `unsupported-semantic` 损失；但在 canonical IR 入口必须作为结构错误拒绝，不可借 IR 绕过校验。
3. `thinking: {"type": "between_tools"}` 与 `budget_tokens` 或 `display` 混合出现：属于互斥冲突配置，必须拒绝并返回结构错误。
4. `tool_choice.disable_parallel_tool_use` 出现在 `tool_choice: "none"` 上：必须作为结构错误拒绝。
5. 包含 `file_id` 的位置引文从响应 IR 复用为 assistant 请求历史：必须剔除 `file_id`，非 null 报 `unmapped-field` 损失，null 零损，保留引文本身。

---

### Task 1: 规范定义与 JSON Schema 扩展

**Files:**
- Modify: `spec/01-intermediate-representation.md`
- Modify: `spec/02-loss-policy.md`
- Modify: `spec/12-mapping-anthropic-messages.md`
- Modify: `spec/15-native-request-options.md`
- Modify: `spec/20-streaming-semantics.md`
- Modify: `spec/schema/ir.schema.json`
- Modify: `spec/schema/vector.schema.json`
- Modify: `spec/README.md`
- Modify: `spec/CHANGELOG.md`

**Interfaces:**
- Consumes: `docs/superpowers/specs/2026-10-25-anthropic-fidelity-v2.4.11-design.md`
- Produces: 扩展后的 IR 规范，包含 `redacted_thinking` 块定义、`TextCitation` 数组与 `citation_delta` 定义、`NativeKindTopK` 与 `NativeKindDisableParallelToolUse` 规范，spec 版本标记更新至 `2.4.11`。

- [ ] **Step 1: 编写规范修订**

更新 `spec/01-intermediate-representation.md`：
- §3.4 Content blocks：增加 `RedactedThinkingBlock`，字段 `data`（必填字符串）、`native`（可选原生注解数组，仅限 Request）；在 `TextBlock` 中增加可选 `citations`（`TextCitation[]`），定义 `TextCitation{source, value}`。
- §5.2 Delta：增加 `CitationDelta{citation}`，对应 discriminator `citation_delta`；明确 `RedactedThinkingBlock` 不接受 delta。
- §4.1 Response：更新 block 联合体包含 `RedactedThinkingBlock`，明确 response 块不包含 `native`。

更新 `spec/12-mapping-anthropic-messages.md`：
- §2 Wire Objects：增加 `redacted_thinking` 块类型；`TextBlock` 增加 `citations`；`ToolChoice` 明确 `disable_parallel_tool_use` 布尔；`Request` 增加 `top_k`。
- §3 Request Field Mapping：增加 `top_k` 映射至 `NativeAnnotation`（`top_k`）；增加 `thinking` 的 `type: "between_tools"` 映射；增加 `disable_parallel_tool_use` 映射至 `NativeAnnotation`；增加 assistant 消息 `redacted_thinking` 块映射；增加 assistant 消息 `text.citations` 映射。
- §4 Response Field Mapping：增加 response `redacted_thinking` 块映射；增加 response `text.citations` 映射。
- §7 Normalization Rules：新增 N-AN-17（redacted_thinking）、N-AN-18（text citations）、N-AN-19（between_tools & top_k & tool_choice parallel control）。
- §8 Loss Catalog：补充相应 loss 条目。

更新 `spec/15-native-request-options.md`：
- §2 已知种类表：在 Request 宿主上增加 `top_k`、`disable_parallel_tool_use`，并在 `thinking_mode` 中明确增加 `between_tools`。
- §3 Anthropic 配置闭合定义：增加 `between_tools`、`top_k`（安全正整数/0）、`disable_parallel_tool_use`（布尔）的校验与跨面遗漏规则。

更新 `spec/20-streaming-semantics.md`：
- §5 Anthropic 映射：增加 `content_block_start` 携带 `redacted_thinking` 的映射；增加 `citations_delta` 映射至 `CitationDelta`。
- §3/§4 OpenAI 映射：定义当遇到 `RedactedThinkingBlock` 和 `CitationDelta` 时的跨面单次报损规则。

更新 `spec/schema/ir.schema.json` 与 `spec/schema/vector.schema.json`：
- `ir.schema.json`: 在 `$defs` 中新增 `redactedThinkingBlock`、`textCitation`、`citationDelta`，将 `redactedThinkingBlock` 加入 content blocks 联合体，在 `textBlock` 中增加可选 `citations`，将 `citationDelta` 加入 `contentBlockDelta.delta` 联合体。
- `vector.schema.json`: 确保新 delta 和 block 能够被 vector 校验器正常识别。

更新 `spec/README.md` 与 `spec/CHANGELOG.md`：
- 将当前规范版本推进至 `2.4.11`。

- [ ] **Step 2: 验证 Schema 语法与现有向量兼容性**

Run: `go -C go run ./cmd/veccheck -root .. -check-manifest`
Expected: PASS (schemas compile against 2020-12, 389 vectors OK)

- [ ] **Step 3: Commit 规范改动**

```bash
git add spec/
git commit -m "spec: 定义 Anthropic 思考历史、请求控制与文本引文保真规范 (2.4.11)"
```

---

### Task 2: 共享黄金向量集补充与 Manifest 刷新

**Files:**
- Create: `vectors/anthropic/nonstream/redacted-thinking-to-ir.json`
- Create: `vectors/anthropic/nonstream/redacted-thinking-from-ir.json`
- Create: `vectors/anthropic/nonstream/native-thinking-between-tools-request-to-ir.json`
- Create: `vectors/anthropic/nonstream/native-thinking-between-tools-request-from-ir.json`
- Create: `vectors/anthropic/nonstream/native-top-k-request-to-ir.json`
- Create: `vectors/anthropic/nonstream/native-top-k-request-from-ir.json`
- Create: `vectors/anthropic/nonstream/native-disable-parallel-tool-use-request-to-ir.json`
- Create: `vectors/anthropic/nonstream/native-disable-parallel-tool-use-request-from-ir.json`
- Create: `vectors/anthropic/nonstream/text-citations-response-to-ir.json`
- Create: `vectors/anthropic/nonstream/text-citations-response-from-ir.json`
- Create: `vectors/anthropic/stream/redacted-thinking-to-ir.json`
- Create: `vectors/anthropic/stream/redacted-thinking-from-ir.json`
- Create: `vectors/anthropic/stream/citations-delta-to-ir.json`
- Create: `vectors/anthropic/stream/citations-delta-from-ir.json`
- Create: `vectors/cross/anthropic-redacted-thinking-to-chatcompletions.json`
- Create: `vectors/cross/anthropic-redacted-thinking-to-responses.json`
- Create: `vectors/cross/anthropic-text-citations-to-chatcompletions.json`
- Create: `vectors/cross/anthropic-text-citations-to-responses.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Consumes: Task 1 规范与 Schema 定义
- Produces: 扩展后的共享黄金向量集（约 407 个向量），包含完整的 Anthropic 同面往返、流式增量与跨面 OpenAI 报损行为。

- [ ] **Step 1: 编写新增的黄金向量文件**

1. `redacted-thinking-to-ir.json` & `from-ir.json`:
   - 验证 assistant history 中包含 `[{"type": "thinking", ...}, {"type": "redacted_thinking", "data": "OPAQUE_TEST_PAYLOAD"}, {"type": "text", ...}]` 的 0 loss 往返。
2. `native-thinking-between-tools-request-to-ir.json` & `from-ir.json`:
   - 验证 `thinking: {"type": "between_tools"}` 解码生成 `NativeAnnotation{Kind: "thinking_mode", Value: "\"between_tools\""}`，编码完整恢复。
3. `native-top-k-request-to-ir.json` & `from-ir.json`:
   - 验证 `top_k: 40` 解码生成 `NativeAnnotation{Kind: "top_k", Value: "40"}`，编码完整恢复。
4. `native-disable-parallel-tool-use-request-to-ir.json` & `from-ir.json`:
   - 验证 `tool_choice: {"type": "auto", "disable_parallel_tool_use": true}` 解码生成对应 native 注解，编码完整恢复。
5. `text-citations-response-to-ir.json` & `from-ir.json`:
   - 验证 response text 携带 `char_location`、`page_location`、`content_block_location`、`web_search_result_location`、`search_result_location` 的 0 loss 往返。
6. 流式向量：
   - `redacted-thinking`: `content_block_start` 直接携带 `redacted_thinking`，紧随 `content_block_stop`。
   - `citations-delta`: `content_block_delta` 携带 `citations_delta` 累加至 TextBlock。
7. 跨面 cross 向量：
   - 验证 Anthropic 输入转换至 Chat Completions / Responses 时，生成对应的 `unsupported-semantic` 损失记录。

- [ ] **Step 2: 重新生成并校验 manifest**

Run: `go -C go run ./cmd/veccheck -root .. -write-manifest && go -C go run ./cmd/veccheck -root .. -check-manifest`
Expected: PASS (所有向量 schema 校验通过，manifest 哈希一致)

- [ ] **Step 3: Commit 黄金向量**

```bash
git add vectors/
git commit -m "vectors: 新增 Anthropic 思考历史、请求控制与引文测试向量"
```

---

### Task 3: Go 参考实现与单元测试

**Files:**
- Modify: `go/ir/block.go`
- Modify: `go/ir/event.go`
- Modify: `go/ir/json.go`
- Modify: `go/ir/native.go`
- Modify: `go/anthropic/messages/types.go`
- Modify: `go/anthropic/messages/rawjson.go`
- Modify: `go/anthropic/messages/request_native.go`
- Modify: `go/anthropic/messages/decode.go`
- Modify: `go/anthropic/messages/encode.go`
- Modify: `go/anthropic/messages/streamin.go`
- Modify: `go/anthropic/messages/streamout.go`
- Modify: `go/openai/chatcompletions/encode.go`
- Modify: `go/openai/chatcompletions/streamout.go`
- Modify: `go/openai/responses/encode.go`
- Modify: `go/openai/responses/streamout.go`
- Modify: `go/internal/vectest/stream.go`
- Test: `go/anthropic/messages/fidelity_v2411_test.go`
- Test: `go/ir/native_test.go`
- Test: `go/ir/json_test.go`

**Interfaces:**
- Consumes: Task 1 规范与 Task 2 向量
- Produces: Go `go/v2` 完整实现五项能力，通过全部 Go 单测和向量测试。

- [ ] **Step 1: 编写 Go 单元测试 `go/anthropic/messages/fidelity_v2411_test.go`**

编写包含以下场景的失败测试：
- `redacted_thinking` 请求历史/响应 round trip
- `between_tools` round trip
- `top_k` round trip (包含 `0`)
- `disable_parallel_tool_use` (true/false) round trip
- 五类 citations 响应与 assistant 历史 round trip
- `between_tools` 与 `budget_tokens` 冲突检测
- `tool_choice: "none"` 携带 `disable_parallel_tool_use` 报错检测
- `redacted_thinking` 流式收到非法 delta 报错检测
- 跨面转换至 CC / RE 时产生精确 `unsupported-semantic`

- [ ] **Step 2: 运行测试验证失败**

Run: `go -C go test -run TestFidelityV2411 ./anthropic/messages`
Expected: FAIL (编译错误或测试失败，尚未实现对应类型和字段)

- [ ] **Step 3: 实现 Go IR 与编解码层**

1. `go/ir`:
   - 定义 `RedactedThinkingBlock`、`TextCitation`、更新 `TextBlock`、定义 `CitationDelta`。
   - `json.go`: 实现 `RedactedThinkingBlock` 和 `CitationDelta` 的 canonical JSON 编解码。
   - `native.go`: 扩展 `NativeKindTopK`、`NativeKindDisableParallelToolUse`、`NativeKindThinkingMode` ("between_tools") 校验。
2. `go/anthropic/messages`:
   - `types.go`: 增加 `BlockTypeRedactedThinking`、`DeltaTypeCitations`，在 `BlockWire` 增加 `Data` 与 `Citations`，在 `ToolChoiceWire` 增加 `HasDisableParallelToolUse`，在 `Request` 增加 `TopK`。
   - `request_native.go`: 编解码 `top_k`、`disable_parallel_tool_use`、`between_tools`。
   - `decode.go` & `encode.go`: 1:1 映射 `redacted_thinking` 和 `citations`。
   - `streamin.go` & `streamout.go`: 流式处理 `redacted_thinking`（无 delta）与 `citations_delta`。
3. `go/openai/chatcompletions` & `go/openai/responses`:
   - 非流式与流式遇 `RedactedThinkingBlock` 和 `TextCitation` 记录 `unsupported-semantic` 损失。
4. `go/internal/vectest/stream.go`:
   - 更新流式向量比较工具支持 `CitationDelta` 累加与归一化。

- [ ] **Step 4: 运行 Go 全量测试**

Run: `go -C go test -count=1 -race ./...`
Expected: PASS (所有包单元测试及向量测试 100% 通过)

- [ ] **Step 5: Commit Go 参考实现**

```bash
git add go/
git commit -m "feat(go): 实现 Anthropic 思考历史、请求控制与文本引文保真"
```

---

### Task 4: TypeScript 语言实现与验证套件

**Files:**
- Modify: `ts/src/ir/types.ts`
- Modify: `ts/src/ir/constants.ts`
- Modify: `ts/src/ir/codec.ts`
- Modify: `ts/src/ir/checker.ts`
- Modify: `ts/src/ir/native.ts`
- Modify: `ts/src/anthropic/messages/types.ts`
- Modify: `ts/src/anthropic/messages/audit.ts`
- Modify: `ts/src/anthropic/messages/rawjson.ts`
- Modify: `ts/src/anthropic/messages/native.ts`
- Modify: `ts/src/anthropic/messages/nonstream.ts`
- Modify: `ts/src/anthropic/messages/stream.ts`
- Modify: `ts/src/openai/chatcompletions/nonstream.ts`
- Modify: `ts/src/openai/chatcompletions/stream.ts`
- Modify: `ts/src/openai/responses/nonstream.ts`
- Modify: `ts/src/openai/responses/stream.ts`
- Modify: `ts/src/vectest/stream.ts`
- Modify: `ts/src/generated/ir-schema.ts`
- Test: `ts/test/anthropic-fidelity-v2411.test.ts`

**Interfaces:**
- Consumes: Task 1 Schema/规范 与 Task 2 向量
- Produces: TypeScript `@elkpi/oxa` 完整实现五项能力，通过全量 node/web/package 检查。

- [ ] **Step 1: 编写 TypeScript 失败测试 `ts/test/anthropic-fidelity-v2411.test.ts`**

覆盖：
- `redacted_thinking` 非流式与流式
- `between_tools`、`top_k`、`disable_parallel_tool_use`
- 五类 citations 响应与 assistant 历史 round trip
- 跨面转换至 CC / RE 时的 unsupported-semantic 损失

- [ ] **Step 2: 刷新生成的 Schema 类型定义并确认测试失败**

Run: `cd ts && npm run generate && node --test dist-test/test/anthropic-fidelity-v2411.test.js`
Expected: FAIL

- [ ] **Step 3: 实现 TypeScript IR 与编解码层**

- 在 `ts/src/ir` 中增加 `RedactedThinkingBlock`、`TextCitation`、`CitationDelta`，更新 `checker.ts` 与 `codec.ts`。
- 在 `ts/src/anthropic/messages` 中实现类型、审计、native 注解、非流式与流式转换。
- 在 `ts/src/openai` 中实现省略报损。
- 更新 `ts/src/vectest/stream.ts` 支持 `citation_delta`。

- [ ] **Step 4: 运行 TypeScript 完整测试套件**

Run: `cd ts && npm run check && npm run test:architecture && npm run test:node && npm run test:web && npm run test:package && npm run test:consumer`
Expected: PASS (所有 7 项构建与验证脚本全部成功)

- [ ] **Step 5: Commit TypeScript 实现**

```bash
git add ts/
git commit -m "feat(ts): 对齐实现 Anthropic 思考历史、请求控制与文本引文保真"
```

---

### Task 5: Python 语言实现与单元测试

**Files:**
- Modify: `python/src/oxa/ir/types.py`
- Modify: `python/src/oxa/ir/constants.py`
- Modify: `python/src/oxa/ir/codec.py`
- Modify: `python/src/oxa/ir/checker.py`
- Modify: `python/src/oxa/ir/native.py`
- Modify: `python/src/oxa/anthropic/messages/constants.py`
- Modify: `python/src/oxa/anthropic/messages/decode.py`
- Modify: `python/src/oxa/anthropic/messages/encode.py`
- Modify: `python/src/oxa/anthropic/messages/native.py`
- Modify: `python/src/oxa/anthropic/messages/rawjson.py`
- Modify: `python/src/oxa/anthropic/messages/streamin.py`
- Modify: `python/src/oxa/anthropic/messages/streamout.py`
- Modify: `python/src/oxa/openai/chatcompletions/encode.py`
- Modify: `python/src/oxa/openai/chatcompletions/streamout.py`
- Modify: `python/src/oxa/openai/responses/encode.py`
- Modify: `python/src/oxa/openai/responses/streamout.py`
- Modify: `python/src/oxa/vectest/compare.py`
- Test: `python/tests/test_anthropic_fidelity_v2411.py`

**Interfaces:**
- Consumes: Task 1 规范与 Task 2 向量
- Produces: Python `oxa` 完整实现五项能力，通过全量单元测试与向量测试。

- [ ] **Step 1: 编写 Python 失败测试 `python/tests/test_anthropic_fidelity_v2411.py`**

- [ ] **Step 2: 运行测试验证失败**

Run: `cd python && PYTHONPATH=src python3 -m unittest tests/test_anthropic_fidelity_v2411.py`
Expected: FAIL

- [ ] **Step 3: 实现 Python IR 与编解码层**

- 在 `python/src/oxa/ir` 中增加 `RedactedThinkingBlock`、`TextCitation`、`CitationDelta`，更新 `codec.py`、`checker.py` 与 `native.py`。
- 在 `python/src/oxa/anthropic/messages` 中实现五项特性的 decode/encode 及流式转换。
- 在 `python/src/oxa/openai` 中实现对 redacted block 与 citations 的省略报损。
- 更新 `python/src/oxa/vectest/compare.py` 流式归一化逻辑。

- [ ] **Step 4: 运行 Python 全量测试**

Run: `cd python && PYTHONPATH=src python3 -m unittest discover -s tests`
Expected: PASS (所有单元测试及向量测试全绿)

- [ ] **Step 5: Commit Python 实现**

```bash
git add python/
git commit -m "feat(python): 对齐实现 Anthropic 思考历史、请求控制与文本引文保真"
```

---

### Task 6: Rust 语言实现与工作区测试

**Files:**
- Modify: `rust/crates/oxa-ir/src/request.rs`
- Modify: `rust/crates/oxa-ir/src/response.rs`
- Modify: `rust/crates/oxa-ir/src/event.rs`
- Modify: `rust/crates/oxa-ir/src/codec.rs`
- Modify: `rust/crates/oxa-ir/src/checker.rs`
- Modify: `rust/crates/oxa-ir/src/native.rs`
- Modify: `rust/crates/oxa-anthropic/src/types.rs`
- Modify: `rust/crates/oxa-anthropic/src/decode.rs`
- Modify: `rust/crates/oxa-anthropic/src/encode.rs`
- Modify: `rust/crates/oxa-anthropic/src/native.rs`
- Modify: `rust/crates/oxa-anthropic/src/rawjson.rs`
- Modify: `rust/crates/oxa-anthropic/src/streamin.rs`
- Modify: `rust/crates/oxa-anthropic/src/streamout.rs`
- Modify: `rust/crates/oxa-chatcompletions/src/encode.rs`
- Modify: `rust/crates/oxa-chatcompletions/src/streamout.rs`
- Modify: `rust/crates/oxa-responses/src/encode.rs`
- Modify: `rust/crates/oxa-responses/src/streamout.rs`
- Modify: `rust/crates/oxa-vectest/src/stream.rs`
- Test: `rust/crates/oxa-anthropic/tests/fidelity_v2411.rs`

**Interfaces:**
- Consumes: Task 1 规范与 Task 2 向量
- Produces: Rust `oxa-*` crates 完整实现五项能力，通过 clippy、fmt 与 workspace 测试。

- [ ] **Step 1: 编写 Rust 失败测试 `rust/crates/oxa-anthropic/tests/fidelity_v2411.rs`**

- [ ] **Step 2: 运行测试验证失败**

Run: `cargo test --manifest-path rust/Cargo.toml --test fidelity_v2411`
Expected: FAIL (编译报错，enum 变体与类型尚未添加)

- [ ] **Step 3: 实现 Rust IR 与各 Spoke 编解码层**

- 在 `oxa-ir` 的 `Block` enum 中增加 `RedactedThinking(RedactedThinkingBlock)`，定义 `TextCitation`，更新 `TextBlock` 结构体；在 `Delta` enum 中增加 `Citation(CitationDelta)`。
- 更新 `oxa-ir` 的 codec、checker 与 native。
- 在 `oxa-anthropic` 中实现 wire 类型、非流式与流式编解码。
- 在 `oxa-chatcompletions` 与 `oxa-responses` 中补齐 enum 穷尽匹配分支并报告 unsupported-semantic。
- 更新 `oxa-vectest` 流式比较逻辑。

- [ ] **Step 4: 运行 Rust 工作区检查与全量测试**

Run: `cd rust && cargo fmt --all -- --check && cargo clippy --workspace --all-targets -- -D warnings && cargo test --workspace`
Expected: PASS (所有 7 个 crates 测试无告警全绿)

- [ ] **Step 5: Commit Rust 实现**

```bash
git add rust/
git commit -m "feat(rust): 对齐实现 Anthropic 思考历史、请求控制与文本引文保真"
```

---

### Task 7: C++ 语言实现与 CTest 测试

**Files:**
- Modify: `cpp/include/oxa/ir.hpp`
- Modify: `cpp/src/ir.cpp`
- Modify: `cpp/src/json.cpp`
- Modify: `cpp/src/native.cpp`
- Modify: `cpp/include/oxa/anthropic/messages.hpp`
- Modify: `cpp/src/anthropic/messages.cpp`
- Modify: `cpp/src/anthropic/messages_native.hpp`
- Modify: `cpp/src/anthropic/messages_audit.hpp`
- Modify: `cpp/src/openai/chatcompletions.cpp`
- Modify: `cpp/src/openai/chatcompletions_audit.hpp`
- Modify: `cpp/src/openai/responses.cpp`
- Modify: `cpp/src/openai/responses_audit.hpp`
- Modify: `cpp/src/vectest.cpp`
- Test: `cpp/tests/test_fidelity_v2411.cpp`

**Interfaces:**
- Consumes: Task 1 规范与 Task 2 向量
- Produces: C++20 `oxa` 库完整实现五项能力，通过全量 CTest 向量与单测。

- [ ] **Step 1: 编写 C++ 失败测试 `cpp/tests/test_fidelity_v2411.cpp`**

- [ ] **Step 2: 编译并验证测试失败**

Run: `cmake -B cpp/build -S cpp -DCMAKE_BUILD_TYPE=Debug && cmake --build cpp/build --config Debug`
Expected: FAIL (编译报错，缺失类型与结构体字段)

- [ ] **Step 3: 实现 C++ IR 与各 Spoke 编解码层**

- 在 `cpp/include/oxa/ir.hpp` 中扩展 `Block` 与 `Delta` 的 `std::variant`，定义 `RedactedThinkingBlock`、`TextCitation`、`CitationDelta`。
- 在 `cpp/src/json.cpp`、`ir.cpp`、`native.cpp` 中实现序列化、反序列化与 native 校验。
- 在 `cpp/src/anthropic` 中实现非流式和流式解析。
- 在 `cpp/src/openai` 中处理新变体并报 unsupported-semantic。
- 更新 `cpp/src/vectest.cpp` 中的流式聚合比对逻辑。

- [ ] **Step 4: 运行 CTest 全量测试**

Run: `cmake --build cpp/build --config Debug --parallel && ctest --test-dir cpp/build -C Debug --output-on-failure`
Expected: PASS (所有 C++ 测试 100% 通过)

- [ ] **Step 5: Commit C++ 实现**

```bash
git add cpp/
git commit -m "feat(cpp): 对齐实现 Anthropic 思考历史、请求控制与文本引文保真"
```

---

### Task 8: 常量一致性检查与版本元数据同步 (v2.4.11)

**Files:**
- Modify: `scripts/check-constants.py`
- Modify: `CHANGELOG.md`
- Modify: `ts/package.json`
- Modify: `python/pyproject.toml`
- Modify: `rust/Cargo.toml`
- Modify: `rust/crates/elkpi-oxa/Cargo.toml`
- Modify: `rust/crates/oxa-ir/Cargo.toml`
- Modify: `rust/crates/oxa-anthropic/Cargo.toml`
- Modify: `rust/crates/oxa-chatcompletions/Cargo.toml`
- Modify: `rust/crates/oxa-responses/Cargo.toml`
- Modify: `rust/crates/oxa-modelmap/Cargo.toml`
- Modify: `rust/crates/oxa-sse/Cargo.toml`
- Modify: `rust/crates/oxa-vectest/Cargo.toml`
- Modify: `cpp/CMakeLists.txt`

**Interfaces:**
- Consumes: Task 1–7 全部完成的实现
- Produces: 5 门语言常量校验 100% 对齐，各包清单统一升级为 `2.4.11`，CHANGELOG 记录完整特性与迁移说明。

- [ ] **Step 1: 更新常量同步脚本 `scripts/check-constants.py`**

加入新增常量：
- `BlockTypeRedactedThinking` / `BLOCK_TYPE_REDACTED_THINKING` (`"redacted_thinking"`)
- `DeltaTypeCitation` / `DELTA_TYPE_CITATION` (`"citation_delta"`)
- `NativeKindTopK` (`"top_k"`)
- `NativeKindDisableParallelToolUse` (`"disable_parallel_tool_use"`)

- [ ] **Step 2: 执行跨语言常量检查**

Run: `python3 scripts/check-constants.py`
Expected: PASS (Go, TS, Python, Rust, C++ 常量全量一致)

- [ ] **Step 3: 更新版本元数据与 CHANGELOG**

- 将版本号更新至 `2.4.11`：`ts/package.json`（并 `npm install` 刷新 `package-lock.json`）、`python/pyproject.toml`、`rust/Cargo.toml` 及全部子 crate `Cargo.toml`、`cpp/CMakeLists.txt`。
- 在 `CHANGELOG.md` 顶部增加 `v2.4.11` 发版记录，详细列出新支持的 5 项 Anthropic 特性、跨面损失行为、及旧版本消费者匹配 exhaustive enum/union 时的迁移提示。

- [ ] **Step 4: 执行全库端到端最终校验**

Run:
```bash
make test
make lint
make check-constants
make vectors
make check-modulepath
```
Expected: PASS (所有 5 门语言测试与代码质量检查 100% 全绿)

- [ ] **Step 5: Commit 版本与元数据更新**

```bash
git add scripts/check-constants.py CHANGELOG.md ts/package.json ts/package-lock.json python/pyproject.toml rust/ cpp/CMakeLists.txt
git commit -m "chore: 升级多语言版本至 v2.4.11 并记录变更日志"
```
