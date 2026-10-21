# Responses 摘要审查修正实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 v2.4.4 PR #53 中修正摘要未知类型与 annotations 的报损，并以共享向量约束五种语言的请求、响应解码一致性。

**Architecture:** 保留 hub-and-spoke 和现有 raw JSON 审计器。摘要转换器识别 `summary_text` 与兼容的 `output_text`；未知或缺失类型整块跳过并记录一条 `unsupported-semantic`，已识别摘要中的非空 annotations 记录一条 `unmapped-field`。不修改编码器、普通 message 的类型词表或既有 id/status/encrypted_content 的报损策略。

**Tech Stack:** Go、TypeScript、Rust、Python、C++；现有共享向量测试器。

**Spec:** `spec/11-mapping-openai-responses.md` N-R-13 与 loss catalog；`spec/02-loss-policy.md`；`spec/20-streaming-semantics.md`（流式行为保持不变）。

## Global Constraints

- 按规范 → 黄金向量 → 实现的顺序修改并分别提交；向量变化后刷新 `vectors/manifest.json`。
- IR contract 保持 `0.2.0`，实现版本保持已经准备的 `2.4.4`。
- 保留原有 `output_text` 向量；不添加协议间直接依赖，不解析工具 JSON。
- 当前会话串行执行；不启动子代理、不创建第二个 worktree，不合并 PR 或发布版本。
- 工作单元输入、工件与验证命令见下文；每个语言单元最多 15 分钟，集成单元最多 20 分钟。单条命令最多 120 秒，构建最多 600 秒。超时、输入发生外部变更或测试无法解释时停止该单元并记录缺口。
- 临时产物只放在忽略的构建目录或仓库外。保留本计划作为审计工件；完成后清理额外临时文件和后台任务，当前 worktree 留待 PR 审查。

## Review Focus

- `summary_text` 与 `output_text` 混合时保留摘要顺序。
- 缺少 `type` 与未知类型不得变为空 ThinkingBlock 且零 loss。
- 非空 annotations 丢弃时必须有对应路径的 loss；空 annotations 继续免报损。
- 不支持的摘要整体报损，不能同时重复报告其 annotations 或子字段。
- 新识别摘要中的普通未知扩展字段仍应被 raw 审计器报告。

## 工作单元 S：规范与计划

**输入范围：** 本计划、`spec/11-mapping-openai-responses.md`。
**工件：** 上述两个文件的独立规范提交。
**完成条件：** 明确请求/响应摘要支持词表、未知/缺失类型的单条 loss、annotations 及未知字段规则，不改变编码与流式输出。
**验证：** `git diff --check`；逐条对应 Review Focus。
**时限/清理：** 10 分钟；无临时文件。

- [x] 更新 InputItem 描述、请求映射、N-R-13 和 loss catalog。
- [x] 检查说明与既有空 summary 规则兼容，提交规范。

## 工作单元 V：共享黄金向量

**输入范围：** `vectors/responses/nonstream/`、`vectors/manifest.json`。
**工件：** 12 个新向量及生成的 manifest 的独立提交。
**完成条件：** 请求与响应分别覆盖标准摘要、混合摘要、未知类型、缺失类型、非空 annotations、未知扩展字段；预期 loss 的路径、数量、顺序固定。
**验证：** `go run ./cmd/veccheck -root .. -write-manifest`、`-check-manifest`；在修改实现前运行 Responses 向量测试，记录失败。
**时限/清理：** 10 分钟；生成文件仅 manifest，无临时生成器残留。

- [ ] 逐一写入 12 个命名匹配路径的向量，保留原有向量。
- [ ] 刷新和校验 manifest。
- [ ] 运行 Go 向量测试确认未知类型/annotations 场景为 RED，提交向量。

## 工作单元 G：Go

**输入范围：** `go/openai/responses/decode.go`、`wire_test.go` 及共享向量。
**工件：** Go 修正与直接验证的 typed/raw 一致性测试提交。
**完成条件：** 请求和响应共用摘要转换逻辑；未知类型跳过并报单条 loss，非空 annotations 报损；新增与既有向量全部通过。
**验证：** `go test -count=1 ./openai/responses`；`go test -count=1 ./...`；`go vet ./...`；`gofmt -l .`。
**时限/清理：** 15 分钟；无仓库外工件。

- [ ] 添加 typed/raw 未知摘要、annotations 一致性断言，先运行确认 RED。
- [ ] 添加 `decodeReasoningSummary(parts []OutputContent, path string) ([]ir.Block, []ir.Loss)`，用于请求和响应摘要分支。
- [ ] 运行验证并提交 Go 修正。

## 工作单元 T：TypeScript

**输入范围：** `ts/src/openai/responses/nonstream.ts`、既有 Responses 测试和共享向量。
**工件：** TypeScript 独立修正提交。
**完成条件：** 请求未知摘要不再静默忽略；响应支持 summary_text；双方对 annotations 报损；非空但全未知的响应 summary 不额外添加空数组 loss。
**验证：** `npm --prefix ts ci`（仅缺少依赖时）、`npm --prefix ts test`、`npm --prefix ts run check`、`npm --prefix ts run fmt`。
**时限/清理：** 15 分钟；构建输出与 node_modules 保持忽略，不进入提交。

- [ ] 运行现有测试器确认新向量 RED。
- [ ] 修改请求/响应摘要分支；维持真正空 summary 的既有报损。
- [ ] 格式化、运行验证并提交。

## 工作单元 R：Rust

**输入范围：** `rust/crates/oxa-responses/src/{normalize,decode}.rs` 与共享向量。
**工件：** Rust 独立修正提交。
**完成条件：** 请求/响应对支持类型和 annotations 的处理符合新向量，既有空数组行为不变。
**验证：** `cargo test --manifest-path rust/Cargo.toml --workspace`；`cargo fmt --manifest-path rust/Cargo.toml --all -- --check`；`cargo clippy --manifest-path rust/Cargo.toml --workspace --all-targets -- -D warnings`。
**时限/清理：** 15 分钟；不更新依赖，target 目录保持忽略。

- [ ] 运行 Responses 向量测试确认 RED。
- [ ] 补充响应 summary_text 和两方向 annotations 报损。
- [ ] 运行验证并提交。

## 工作单元 P：Python

**输入范围：** `python/src/oxa/openai/responses/{normalize,decode}.py` 与共享向量。
**工件：** Python 独立修正提交。
**完成条件：** 请求/响应处理符合新向量；不改变其他协议审计 profile。
**验证：** `PYTHONPATH=python/src python3 -m unittest discover -s python/tests`。
**时限/清理：** 15 分钟；不安装全局依赖，缓存目录保持忽略。

- [ ] 运行 Responses 向量测试确认 RED。
- [ ] 补充响应 summary_text 和两方向 annotations 报损。
- [ ] 运行完整测试并提交。

## 工作单元 C：C++

**输入范围：** `cpp/src/openai/responses.cpp` 与共享向量。
**工件：** C++ 独立修正提交。
**完成条件：** 响应支持 summary_text，两方向 annotations 报损；已有未知类型单条报损保持不变。
**验证：** `cmake -S cpp -B cpp/build -DCMAKE_BUILD_TYPE=Debug`；`cmake --build cpp/build -j2`；`ctest --test-dir cpp/build --output-on-failure`。
**时限/清理：** 15 分钟；build 目录保持忽略。

- [ ] 配置、构建并运行新向量，确认 RED。
- [ ] 修正摘要转换分支，运行验证并提交。

## 工作单元 A：集成、审查与 PR 更新

**输入范围：** 已通过验证的上述提交、`CHANGELOG.md`、`README.md`、`spec/README.md`、PR #53。
**工件：** 文档汇总提交、推送后的 PR #53 及 CI 结果。
**完成条件：** 向量总数与文档一致；五种语言测试通过；工作区干净；提交均推送至原 PR；CI 无失败。
**验证：** Go race 测试、五种语言全套测试、格式/静态检查、manifest 检查、`git diff --check`、`gh pr checks 53`。
**时限/清理：** 20 分钟；CI 采用一次有上限的 watch，不密集轮询；无后台任务遗留。若 CI 排队超时，明确报告待完成项，不取消他人的远端任务。

- [ ] 更新 changelog、向量计数与 PR 描述，纠正“其他语言已全部支持响应 summary_text”的说法。
- [ ] 当前会话检查完整差异，执行集成验证。
- [ ] 分粒度提交、推送原分支，更新 PR #53 并确认 CI。
