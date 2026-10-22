# Anthropic 会话中间 System 消息与 v2.4.6 发布实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 规范化支持 Anthropic Messages 会话中间 `role: "system"` 消息，折叠至 IR `Request.System` 并报告 `degraded` 损失，跨 Go/TS/Python/Rust/C++ 五门语言对齐实现，新增共享黄金向量并升级版本至 v2.4.6。

**Architecture:** 依据 Anthropic 官方 GA 契约与规范制定 N-AN-15 规则；在各语言 Anthropic 解码器中校验 placement 与 content，将文本抽取追加至 `Request.System[]`，保留 `cache_control` 原生注解，生成精确 loss 记录；保持 IR 0.2.0 contract 及 `messages` 消息角色封闭集不变。

**Tech Stack:** Go 1.23+, TypeScript 5+, Python 3.12+, Rust 2021, C++20, JSON Schema 2020-12, veccheck.

**Spec:** `docs/superpowers/specs/2026-10-22-anthropic-mid-conversation-system-design.md`

## Global Constraints

- 协议到 IR 映射严格遵守规范先行：`spec/` → `vectors/` → `implementation` 顺序。
- IR contract 保持 `0.2.0` 不变，`Message.Role` 封闭集保持 `user` | `assistant` 不变。
- 会话中间 system 消息必须满足官方位置约束（不可为 `messages[0]`，必须跟随 `user` 或 server tool 结束的 `assistant`，不可后接 `user`），违规必须报结构错误。
- 会话中间 system 消息内容必须为纯文本字符串或仅含 `type: "text"` 的 content block 数组，非文本块必须报结构错误。
- 每一条折叠的 system 消息产生且仅产生一条 `degraded` 损失，path 为 `messages[i].role`，field 为 `role`，detail 为固定常量：`Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved.`。
- 每一个 Commit 粒度聚焦、独立且可 cherry-pick。

## Review Focus

1. `messages[0]` 出现 `role: "system"`：必须拒绝并返回明确结构错误，不可错误静默折叠。
2. 连续多个 `role: "system"` 消息：必须按出现顺序全部抽取追加至 `Request.System[]`，并分别逐条在各自下标报 `degraded` 损失。
3. `role: "system"` 后直接跟随 `role: "user"`：违反 placement 约束，必须拒绝并报错。
4. system 消息内容中含有 `image`、`tool_use` 或未知块类型：必须拒绝并报错，不可漏过非文本数据。
5. 携带 `cache_control` 的 system 文本块：其 `NativeAnnotation` 必须完整保留在生成的 `ir.SystemBlock` 上。

---

### Task 1: 规范定义 (spec/12 与 spec/README)

**Files:**
- Modify: `spec/12-mapping-anthropic-messages.md:20-65,140-270`
- Modify: `spec/README.md:10-18`

**Interfaces:**
- Consumes: `docs/superpowers/specs/2026-10-22-anthropic-mid-conversation-system-design.md`
- Produces: `spec/12-mapping-anthropic-messages.md` 中的 N-AN-15 规则与损失目录条目，spec 版本更新至 `2.4.6`。

- [ ] **Step 1: 编写规范补丁与修订**

更新 `spec/12-mapping-anthropic-messages.md`：
- §1 Overview：说明支持 mid-conversation `role: "system"` 消息。
- §2 Wire Objects：Message 角色说明增加 mid-conversation `system`。
- §3 Request Field Mapping 表：增加 `messages[].role = "system"` 映射至 `Request.System[]`，标注 N-AN-15。
- §7 Normalization Rules：新增 **N-AN-15 (mid-conversation system messages)**：详细定义 placement 校验规则、content 文本校验规则、追加至 `Request.System[]` 顺序、`cache_control` 保留以及 `messages[i].role` 上的 `degraded` 损失。
- §8 Loss Catalog 表：增加 `messages[i].role` 条目。
更新 `spec/README.md`：当前 spec 版本标记为 `2.4.6`。

- [ ] **Step 2: 验证规范一致性**

Run: `go -C go run ./cmd/veccheck -root .. -check-manifest`
Expected: PASS (schemas compile against 2020-12, 346 vectors OK)

- [ ] **Step 3: Commit 规范改动**

```bash
git add spec/12-mapping-anthropic-messages.md spec/README.md
git commit -m "spec: 定义 Anthropic 会话中间 system 消息折叠与降级损失规范 (N-AN-15)"
```

---

### Task 2: 共享黄金向量补充与 Manifest 刷新

**Files:**
- Create: `vectors/anthropic/nonstream/mid-conversation-system-to-ir.json`
- Modify: `vectors/manifest.json`

**Interfaces:**
- Consumes: Task 1 规范 N-AN-15 定义
- Produces: 347 个向量的黄金测试集，供五门语言 runner 执行

- [ ] **Step 1: 编写黄金向量文件 `vectors/anthropic/nonstream/mid-conversation-system-to-ir.json`**

向量内容包含：
- `input`:
  - `model`: `"claude-opus-5-5"`
  - `max_tokens`: `1024`
  - `system`: `[{"type": "text", "text": "Top-level system prompt."}]`
  - `messages`:
    - `{"role": "user", "content": "What is the capital of France?"}`
    - `{"role": "assistant", "content": "Paris."}`
    - `{"role": "user", "content": "Now summarize."}`
    - `{"role": "system", "content": "Terse mode enabled - keep responses under 40 words."}`
- `expected_ir`:
  - `system`: 包含两块，前一块为顶层 prompt，后一块为 mid-conversation 文本。
  - `messages`: 仅保留前 3 个 user/assistant turn。
- `expected_losses`:
  - 单条 `degraded` 损失：`path: "messages[3].role", field: "role", reason: "degraded", detail: "Anthropic mid-conversation system message is carried as request-level system content; mid-conversation position is not preserved."`

- [ ] **Step 2: 重新生成并校验 manifest**

Run: `go -C go run ./cmd/veccheck -root .. -write-manifest && go -C go run ./cmd/veccheck -root .. -check-manifest`
Expected: `347 vectors, 348 checks, OK`

- [ ] **Step 3: Commit 黄金向量**

```bash
git add vectors/anthropic/nonstream/mid-conversation-system-to-ir.json vectors/manifest.json
git commit -m "vectors: 新增 Anthropic 会话中间 system 消息折叠黄金向量 (347)"
```

---

### Task 3: Go 参考实现与单元测试

**Files:**
- Modify: `go/anthropic/messages/types.go:23`
- Modify: `go/anthropic/messages/decode.go:149-168`
- Test: `go/anthropic/messages/mid_system_test.go`

**Interfaces:**
- Consumes: Task 1 N-AN-15 规范与 Task 2 向量
- Produces: Go `DecodeRequest` 与 `DecodeRequestJSON` 支持会话中间 system 消息

- [ ] **Step 1: 编写 Go 回归测试 `go/anthropic/messages/mid_system_test.go`**

覆盖：
1. 正常字符串 content 的 mid-conversation system 消息；
2. 块数组 content 且带 `cache_control` 的 system 消息；
3. 连续两条 system 消息分别折叠并报 loss；
4. `messages[0]` 为 system 时报错；
5. system 消息后直接跟随 user 消息时报错；
6. system 消息中包含非 text 块时报错。

- [ ] **Step 2: 运行测试验证失败**

Run: `go -C go test -count=1 ./anthropic/messages -run TestMidConversationSystem`
Expected: FAIL (unknown role "system")

- [ ] **Step 3: 在 Go 中实现 N-AN-15 规则**

在 `go/anthropic/messages/types.go` 定义 `RoleSystem = "system"`。
在 `go/anthropic/messages/decode.go` 实现：
- placement 预校验与运行期校验：检查 `messages[0]`、前驱与后继角色。
- content 提取：支持 string、`[]BlockWire`、`[]SystemBlockWire`、`[]any`，校验 `type == "text"`，提取文本与 `cache_control`（保存至 `ir.SystemBlock.Native`）。
- 记录 `ir.LossDegraded` 并将生成的 `SystemBlock` 追加至 `req.System`。

- [ ] **Step 4: 运行 Go 测试套件验证通过**

Run: `go -C go test -count=1 ./...`
Expected: 所有 package 测试通过，347 个向量全部通过。

- [ ] **Step 5: Commit Go 实现**

```bash
git add go/anthropic/messages/types.go go/anthropic/messages/decode.go go/anthropic/messages/mid_system_test.go
git commit -m "fix(go): 支持 Anthropic 会话中间 system 消息折叠与校验"
```

---

### Task 4: TypeScript 实现与单元测试

**Files:**
- Modify: `ts/src/anthropic/messages/nonstream.ts:195-225`
- Test: `ts/test/anthropic-nonstream.test.ts`

**Interfaces:**
- Consumes: Task 1 规范与 Task 2 向量
- Produces: TypeScript `decodeRequest` 支持会话中间 system 消息

- [ ] **Step 1: 编写 TypeScript 回归测试**

在 `ts/test/anthropic-nonstream.test.ts` 增加对 mid-conversation system 消息成功解析、loss 记录及非法 placement/content 拒绝的测试。

- [ ] **Step 2: 运行测试验证失败**

Run: `npm --prefix ts test`
Expected: FAIL (由于新黄金向量 `mid-conversation-system-to-ir` 暂未在 TS 中通过)

- [ ] **Step 3: 在 TypeScript 中实现 N-AN-15 规则**

在 `ts/src/anthropic/messages/nonstream.ts` 中扩展 `decodeRequest`：
- 识别 `role === "system"`；
- 校验 placement（不可为第 0 项、前驱必须为 user/server tool assistant、后继不可为 user）；
- 抽取 text 并解析 `cache_control`，追加至 `req.system`；
- 追加 `degraded` 损失 `messages[${i}].role`。

- [ ] **Step 4: 运行 TypeScript 测试套件验证通过**

Run: `npm --prefix ts test`
Expected: 220+ tests pass (0 failures).

- [ ] **Step 5: Commit TypeScript 实现**

```bash
git add ts/src/anthropic/messages/nonstream.ts ts/test/anthropic-nonstream.test.ts
git commit -m "fix(ts): 支持 Anthropic 会话中间 system 消息折叠与校验"
```

---

### Task 5: Python 实现与单元测试

**Files:**
- Modify: `python/src/oxa/anthropic/messages/constants.py`
- Modify: `python/src/oxa/anthropic/messages/decode.py`
- Test: `python/tests/test_anthropic_nonstream.py`

**Interfaces:**
- Consumes: Task 1 规范与 Task 2 向量
- Produces: Python `decode_request` 支持会话中间 system 消息

- [ ] **Step 1: 编写 Python 回归测试**

在 `python/tests/test_anthropic_nonstream.py` 增加针对 mid-conversation system 消息正常与异常路径的测试函数。

- [ ] **Step 2: 运行测试验证失败**

Run: `/home/ping/work/oxa/python/.venv/bin/pytest python`
Expected: FAIL (vectors test fails on `mid-conversation-system-to-ir`)

- [ ] **Step 3: 在 Python 中实现 N-AN-15 规则**

在 `constants.py` 增加 `AN_ROLE_SYSTEM = "system"`。
在 `decode.py` 中：
- 校验 system placement；
- 解码 system content 为 `SystemBlock`，保留 `cache_control`；
- 追加至 `req.system` 并记录 `degraded` loss。

- [ ] **Step 4: 运行 Python 测试套件验证通过**

Run: `/home/ping/work/oxa/python/.venv/bin/pytest python`
Expected: 全部测试通过 (147 passed).

- [ ] **Step 5: Commit Python 实现**

```bash
git add python/src/oxa/anthropic/messages/constants.py python/src/oxa/anthropic/messages/decode.py python/tests/test_anthropic_nonstream.py
git commit -m "fix(python): 支持 Anthropic 会话中间 system 消息折叠与校验"
```

---

### Task 6: Rust 实现与单元测试

**Files:**
- Modify: `rust/crates/oxa-anthropic/src/types.rs`
- Modify: `rust/crates/oxa-anthropic/src/decode.rs`
- Test: `rust/crates/oxa-anthropic/tests/nonstream.rs`

**Interfaces:**
- Consumes: Task 1 规范与 Task 2 向量
- Produces: Rust `decode_request` 支持会话中间 system 消息

- [ ] **Step 1: 编写 Rust 回归测试**

在 `rust/crates/oxa-anthropic/tests/nonstream.rs` 增加对 mid-conversation system 消息的单元测试与非法位置/内容的拒绝测试。

- [ ] **Step 2: 运行测试验证失败**

Run: `cargo test --manifest-path rust/Cargo.toml`
Expected: FAIL (vector test fails on `mid-conversation-system-to-ir`)

- [ ] **Step 3: 在 Rust 中实现 N-AN-15 规则**

在 `types.rs` 增加 `pub const ROLE_SYSTEM: &str = "system";`。
在 `decode.rs` 中：
- 校验 placement 规则；
- 解码 system blocks 并保留 `cache_control`；
- 追加至 `req.system` 并记录 `degraded` loss。

- [ ] **Step 4: 运行 Rust 测试套件验证通过**

Run: `cargo test --manifest-path rust/Cargo.toml`
Expected: 所有 crate 和 vector 测试全部通过。

- [ ] **Step 5: Commit Rust 实现**

```bash
git add rust/crates/oxa-anthropic/src/types.rs rust/crates/oxa-anthropic/src/decode.rs rust/crates/oxa-anthropic/tests/nonstream.rs
git commit -m "fix(rust): 支持 Anthropic 会话中间 system 消息折叠与校验"
```

---

### Task 7: C++ 实现与单元测试

**Files:**
- Modify: `cpp/src/anthropic/messages.cpp`
- Test: `cpp/tests/test_anthropic.cpp`

**Interfaces:**
- Consumes: Task 1 规范与 Task 2 向量
- Produces: C++ `decode_request` 支持会话中间 system 消息

- [ ] **Step 1: 编写 C++ 回归测试**

在 `cpp/tests/test_anthropic.cpp` 增加对 mid-conversation system 消息及各种边界情况的断言。

- [ ] **Step 2: 运行测试验证失败**

Run: `cmake --build cpp/build --parallel && ctest --test-dir cpp/build --output-on-failure`
Expected: FAIL (vector test fails on `mid-conversation-system-to-ir`)

- [ ] **Step 3: 在 C++ 中实现 N-AN-15 规则**

在 `cpp/src/anthropic/messages.cpp` 中：
- 识别 `role == "system"`；
- 校验 placement；
- 解析 content 为 `ir::SystemBlock`，保留 `cache_control`；
- 追加至 `req.system`，报告 `degraded` loss。

- [ ] **Step 4: 运行 C++ 测试套件验证通过**

Run: `cmake --build cpp/build --parallel && ctest --test-dir cpp/build --output-on-failure`
Expected: 15/15 tests passed.

- [ ] **Step 5: Commit C++ 实现**

```bash
git add cpp/src/anthropic/messages.cpp cpp/tests/test_anthropic.cpp
git commit -m "fix(cpp): 支持 Anthropic 会话中间 system 消息折叠与校验"
```

---

### Task 8: 全局版本号升级 (v2.4.6) 与发布元数据准备

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `go/doc.go`, `go/doc_test.go`
- Modify: `python/pyproject.toml`, `python/src/oxa/__init__.py`, `python/tests/test_smoke.py`
- Modify: `rust/Cargo.toml`, `rust/Cargo.lock`, `rust/README.md`, `rust/crates/oxa-ir/tests/codec.rs`
- Modify: `ts/package.json`, `ts/package-lock.json`, `ts/scripts/test-package.mjs`
- Modify: `spec/README.md`

**Interfaces:**
- Consumes: Tasks 1-7 完成的五语言统一实现
- Produces: 完备就绪的 v2.4.6 发布元数据文件

- [ ] **Step 1: 更新各语言工程配置及版本号到 2.4.6**

批量更新所有版本标识文件。
在 `CHANGELOG.md` 中记录 v2.4.6 版本条目：
- 修复 Anthropic 会话中间 `role: "system"` 消息在各语言解码器中被拒绝的问题。
- 引入 N-AN-15 规范规则：严格校验 placement 与文本 content，折叠至 `Request.System` 并记录 `degraded` 损失。
- 新增黄金向量，总向量数达到 347。
- Go、TypeScript、Python、Rust、C++ 全部同步对齐。

- [ ] **Step 2: 全量执行 5 门语言的完整验证**

1. Go: `go -C go test -count=1 ./...`
2. TS: `npm --prefix ts test`
3. Python: `/home/ping/work/oxa/python/.venv/bin/pytest python`
4. Rust: `cargo test --manifest-path rust/Cargo.toml`
5. C++: `cmake --build cpp/build --parallel && ctest --test-dir cpp/build --output-on-failure`
6. Manifest: `go -C go run ./cmd/veccheck -root .. -check-manifest`
Expected: 6 项检查全部 100% PASS。

- [ ] **Step 3: Commit 发布准备改动**

```bash
git add CHANGELOG.md README.md go/doc.go go/doc_test.go python/pyproject.toml python/src/oxa/__init__.py python/tests/test_smoke.py rust/Cargo.toml rust/Cargo.lock rust/README.md rust/crates/oxa-ir/tests/codec.rs ts/package.json ts/package-lock.json ts/scripts/test-package.mjs spec/README.md
git commit -m "chore(release): 准备 v2.4.6 版本发布文件与元数据"
```
