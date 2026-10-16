# Responses Custom Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在五种语言中支持 Responses Custom Tools 的声明、选择、调用、结果和流式输入，保留任意文本并显式报告无法表达的语义。

**Architecture:** 最小扩展 Tool/ToolChoice；独立 custom_tool_use/custom_tool_result/custom_tool_input_delta；沿用 M7 聚合与现有 opaque 字符串机制。先规范、共享向量，再 Go 与其余语言，最后统一集成验证。

**Tech Stack:** Go、TypeScript、Rust、Python、C++20、JSON Schema 2020-12。

**Spec:** [已批准设计](../specs/2026-10-16-responses-custom-tools-design.md)

## Global Constraints

- 新 IR 输出版本 0.3.0；继续读取 0.1.0、0.2.0；下一规范主版本 3.0.0，未发布。
- 不提交、不发布、不迁移 Go module path、不加入 Apply Patch。
- 不解析或执行 custom 输入和 grammar，不将其包装为 function arguments。
- 结果 output 为 string 或 text/image 列表，无 is_error；文件明确 loss。
- 接口与 JSON 名称以设计为准；语言实现只修改自己的目录。
- 每个并行 worker 仅写自己唯一 patch 工件；集成阶段验证路径、应用补丁、运行测试才算完成。

## Review Focus

1. 空字符串和空 delta 不能被当成字段缺失；归属 WU-02 至 WU-06 的新增 custom 测试。
2. function/custom 混合顺序不能被按类别重排；归属共享 mixed-request 向量。
3. done/item.done 完整快照不能重复拼接；归属流式向量与各语言失败用例。
4. output 全是不支持文件时必须保留关联与空列表；归属 output-loss 向量。
5. 丢弃 custom 后不得遗留错误 choice、tool_use 停止原因或跳号索引；归属不支持目标的编码回归。

---

### WU-00: 隔离与基线

- [x] 创建原生 worktree，确认没有既存用户修改。
- [x] 执行 make test、make test-python、npm test、cargo test --workspace。
- [x] 构建 C++，从 worktree 根运行 13 个测试二进制；外部构建目录的 CTest 失败确认为启动目录问题。

### WU-01: 规范与共享向量

**Files:** spec/13-custom-tools.md、spec/01-intermediate-representation.md、spec/11-mapping-openai-responses.md、spec/20-streaming-semantics.md、spec/README.md、spec/schema/ir.schema.json、vectors/responses/**、vectors/manifest.json、go/cmd/veccheck/check.go。

**Interfaces:** 产生设计中锁定的 IR JSON 形状，所有语言消费同一 schema/向量。

- [ ] 更新规范与 schema，保持旧 function 工具形状，添加条件字段与受限 output union。
- [ ] 新建双向声明/调用/结果向量和双向流式向量。核心断言：

```json
{"type":"custom_tool_use","id":"call_custom","name":"code_exec","input":"print(\"你好\")\r\nnot JSON"}
```

```json
{"type":"custom_tool_result","tool_use_id":"call_custom","output":[{"type":"text","text":"你好"}]}
```

- [ ] 运行 `go run ./cmd/veccheck -root .. -write-manifest`；新增类型未实现时先确认测试失败，再扩展语法校验。
- [ ] 检查 manifest、命名、输入/结果匹配与 schema，发布只读契约供语言 worker 使用。

### WU-02: Go IR、Responses 和降级

**Files:** go/ir/{request,block,event,json}.go、新增 custom 测试；go/openai/responses/{types,decode,encode,rawjson,streamin,streamout}.go；go/anthropic/messages、go/openai/chatcompletions 的编码路径；go/internal/vectest/compare.go。

**Interfaces:** 新增 ir.CustomToolFormat、ir.CustomToolUseBlock、ir.CustomToolResultBlock、ir.CustomToolInputDelta；现有 Decode/Encode/Feed/Apply 签名不变。

- [ ] 编写并运行失败测试：

```go
func TestCustomTextInput(t *testing.T) {
    r, losses, err := DecodeResponseJSON([]byte(`{"id":"r","model":"m","status":"completed","output":[{"type":"custom_tool_call","call_id":"c","name":"run","input":"not JSON\r\n"}]}`))
    if err != nil || len(losses) != 0 || r.StopReason != ir.StopToolUse { t.Fatalf("%+v %v", losses, err) }
    b, ok := r.Content[0].(ir.CustomToolUseBlock)
    if !ok || string(b.Input) != `"not JSON\r\n"` { t.Fatalf("%+v", r.Content) }
}
```

- [ ] 实现 IR codec、工具条件字段、受限输出、版本读取和 delta 对应关系。
- [ ] 实现 Responses 声明/选择/混合请求/响应/内容列表与未知字段审计。
- [ ] 实现 M7 聚合：delta、done、item.done 校验、缺少 delta 回退、合成编码事件。
- [ ] 明确拒绝错配 Delta、错误 identity、快照不一致和未闭合流；不支持目标显式 loss。
- [ ] `go test -count=1 ./...`、`go vet ./...`、`gofmt -l .`、manifest 检查。

### WU-03: TypeScript

**Files:** ts/src/ir/**、ts/src/generated/**、ts/src/openai/responses/**、ts/src/stream/**、ts/src/json/**、ts/test/**。

**Interfaces:** JSON 形状同 WU-01；保留现有公开调用入口。

- [ ] 读取新增向量并先运行现有测试，确认 custom 输入/版本不被支持。
- [ ] 修改 IR 类型、codec/checker、Responses 全路径、两个不支持目标和旧版本比较；生成类型以 schema 为准。
- [ ] 增加空文本、混合调用、快照不匹配、未知字段测试。
- [ ] 发布唯一 `/tmp/oxa-custom-tools.weNMAt/ts.patch`。
- [ ] 集成后执行 `npm run generate && npm test && npm run lint && npm run generate:check`。

### WU-04: Rust

**Files:** rust/crates/oxa-ir/**、oxa-responses/**、oxa-chatcompletions/**、oxa-anthropic/**、oxa-vectest/**。

**Interfaces:** 新枚举变体与受限输出 union，既有函数签名保留。

- [ ] 先以共享向量和新单测观察失败。
- [ ] 修改 IR serde/checker、Responses、审计与降级，以及兼容版本比较。
- [ ] 添加空输入、done 快照冲突、不可表达结果测试。
- [ ] 发布唯一 `/tmp/oxa-custom-tools.weNMAt/rust.patch`。
- [ ] 集成后执行 `cargo test --workspace`、`cargo fmt --all --check`、`cargo clippy --workspace --all-targets -- -D warnings`。

### WU-05: Python

**Files:** python/src/oxa/**、python/tests/**。

**Interfaces:** Python 数据类型对应相同 JSON 契约，保留入口。

- [ ] 共享新向量在旧实现失败；增加纯文本/空片段回归。
- [ ] 实现 IR codec/checker、Responses 非流式/流式和未知字段审计；更新降级与版本。
- [ ] 发布唯一 `/tmp/oxa-custom-tools.weNMAt/python.patch`。
- [ ] 集成后执行 `make test-python`，确认全部共享向量通过。

### WU-06: C++

**Files:** cpp/include/oxa/ir.hpp、cpp/src/ir.cpp、cpp/include/oxa/openai/responses.hpp、cpp/src/openai/responses*、cpp/src/{anthropic,openai} 编码路径、cpp/tests/**。

**Interfaces:** std::variant 新增变体；字符串/内容列表 output union；StatusOr 入口保持。

- [ ] 共享向量观察失败；添加空文本、错配生命周期和内容列表测试。
- [ ] 修改 IR codec/checker、Responses 和 audit、目标降级；不引入外部依赖。
- [ ] 发布唯一 `/tmp/oxa-custom-tools.weNMAt/cpp.patch`。
- [ ] 集成后 CMake build；从 worktree 根运行全部 13 项测试，验证无异常编译选项下兼容性。

### WU-07: 集成与最终验证

**Files:** 各单元已验证工件；README/CHANGELOG 的未发布说明。

- [ ] 校验各 patch 仅涉及所属语言，git apply --check 成功才应用；验证后记录 SHA-256。
- [ ] 五语言新旧向量全部通过；修复测试发现的问题而非屏蔽向量。
- [ ] 执行跨模块约束检查、manifest、格式与静态检查、git diff --check。
- [ ] 独立审查重点覆盖文本保真、重复快照、关联、降级和未知字段；修复经验证的问题。
- [ ] 完成清单记录全部验证结果；清理临时工件和后台任务，保留有改动 worktree；最终报告明确覆盖范围与未完成项。
