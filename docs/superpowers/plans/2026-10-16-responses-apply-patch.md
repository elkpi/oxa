# Responses Apply Patch v2.3.0 Implementation Plan

> **For agentic workers:** 按本文任务边界执行；使用 superpowers:test-driven-development，完成时使用 superpowers:verification-before-completion。工作副本是隔离中间资源；每个语言单元仅发布一个原子 JSON 结果工件，禁止写共享汇总文件。

**Goal:** 五语言完整实现专用 Responses Apply Patch，保持旧转换行为与不透明文本保真，并通过 GitHub CI。

**Architecture:** 保持 hub-and-spoke，扩展 Tool/ToolChoice、两个专用 Block 与三种强类型 Operation；原子项 streaming 只发 start/stop，不增加 Delta。先规范、Schema、共享向量，再实现与跨语言集成。

**Tech Stack:** Go、TypeScript、Rust、Python、C++20、JSON Schema 2020-12、GitHub Actions。

**Spec:** [已确认设计](../specs/2026-10-16-responses-apply-patch-design.md)

## Global Constraints

- 包版本与 Spec 为 2.3.0（未发布），IR contract 为 0.2.0，兼容读取 0.1.0；Go module path 不变。
- 不合并、不打 tag、不发布。GitHub CI 通过 feature 分支 PR 验证。
- 专用 apply_patch 绝不转换成同名 custom/function；原有 custom name=apply_patch 不改语义。
- Operation 必须强类型，Path/Diff 按字符串原样保存，不执行或验证补丁可用性。
- call status 为 in_progress/completed；result status 为 completed/failed；output 缺失/null→缺失，空串不能丢。
- 声明与专用 choice 没有 name；旧工具/choice 规则不得放宽。
- Source-to-IR 的未知 Operation/status 显式 loss；请求关联成对过滤后仍满足 INV-3/4；来源自身错误仍报错。
- 两个不支持目标必须报损、压紧索引，不新增错误 user turn，不在没有实际丢弃调用时随意改 stop reason。
- 每个语言的工作副本仅能修改自己的目录；唯一发布工件含 base SHA、scope、patch 与实际测试证据。只集成通过工件验证的结果。

## Review Focus

1. declare/choice 的 name 缺失是合法专用形状，不能先套用 function/custom 校验。
2. delete 的多余 wire diff 是未知属性 loss，不应解析其值；IR delete 带 diff 则非法。
3. added 的 diff 可不同于 done，后者是最终快照；重复 terminal 不得多产生块。
4. output 缺失/null/空串必须区分，结果失败不能变成顶层 Response failed。
5. 未知语义关联过滤、混合结果、跳过块索引及 stop reason 不能破坏其他保留内容。

---

## W0：隔离和基线

**输入/工件：** v2.2.0 五语言与 205 向量；仓库外 baseline.log。最长 600 秒；完成后复用构建缓存，最终回收本任务临时资源。

- [x] 创建 native worktree，复制已确认但尚未提交的设计稿，不修改主 checkout。
- [x] Go、Python、TS、Rust、C++ 13 项基线全部通过。

## W1：规范、Schema 与向量

**文件：** 新增 spec/14-apply-patch.md；修改 spec/01-intermediate-representation.md、spec/11-mapping-openai-responses.md、spec/20-streaming-semantics.md、spec/README.md、spec/CHANGELOG.md、spec/schema/ir.schema.json；新增 vectors/responses/{nonstream,stream}/apply-patch-*；刷新 vectors/manifest.json；扩展 go/cmd/veccheck。

**接口：**

```text
Tool {type: apply_patch}
ToolChoice {mode: tool, tool_type: apply_patch}
ApplyPatchToolUseBlock {type: apply_patch_tool_use, id, status, operation}
ApplyPatchToolResultBlock {type: apply_patch_tool_result, tool_use_id, status, output?}
Operation = {type:create_file,path,diff} | {type:delete_file,path} | {type:update_file,path,diff}
```

**完成/验证：** Schema 编译、所有向量命名/关系/manifest 均合法。最长 1800 秒；生成器仅在仓库外。

- [ ] 将批准设计的字段约束、响应结果合法位置、原子流式与 loss 规则转为 spec/14。
- [ ] 对齐旧版本政策的冲突文字，不改 IR contract。
- [ ] Schema 添加 Tool/Choice 分支、Operation 三变体和两个 Block；Delta 集合不变。
- [ ] 新向量先运行旧实现，保存因缺少 apply_patch 支持而失败的 RED 证据。
- [ ] 向量包含三操作双向、两状态域、历史/响应结果、空/缺失输出、特殊路径/diff、混合调用、unknown loss、原子流式和目标降级。
- [ ] 用 veccheck 写/检查 manifest；新增零 Delta 块通过语法校验；孤立结果被拒绝。

## W2：Go 参考实现与错误边界

**文件：** 新增 go/ir/apply_patch.go、apply_patch_test.go；修改 go/ir/{request,json,custom}.go；新增 go/openai/responses/apply_patch*.go 和测试；修改 responses/{types,decode,encode,rawjson,streamin,streamout}.go；两个目标的 encode/custom_filter；go/cmd/veccheck 和 go/internal/vectest。

**接口：** ApplyPatchOperation 封闭接口，ApplyPatchCreateFileOperation/ApplyPatchDeleteFileOperation/ApplyPatchUpdateFileOperation 三种变体；两个 Block 的关联字段为 ID/ToolUseID；Result.Output 为 *string；调用与结果 Status 使用不同 string enum。公开 Decode/Encode/Feed/Apply 入口签名保持。

**完成/验证：** go test -count=1 ./...、go vet ./...、gofmt、向量及原始字段审计全部通过。最长 3600 秒；不留后台任务。

- [ ] 先写并运行真实回归（示例断言）：

```go
func TestApplyPatchDeclaration(t *testing.T) {
    req, losses, err := DecodeRequestJSON([]byte(`{"model":"m","input":"go","tools":[{"type":"apply_patch"}],"tool_choice":{"type":"apply_patch"}}`))
    if err != nil || len(losses) != 0 || len(req.Tools) != 1 || req.Tools[0].Type != ir.ToolTypeApplyPatch {
        t.Fatalf("request=%+v losses=%+v err=%v", req, losses, err)
    }
}
```

- [ ] 实现 IR codec 与结构校验；不解析 diff 内容。
- [ ] 实现声明、选择、三个操作和结果的双向映射，共享 Operation 解析，不复制五份校验规则。
- [ ] 请求使用 source-local 关联预检与过滤；保持 function/custom/apply_patch 的共享顺序。
- [ ] response.output 同时支持专用调用与结果；合成 apc_N/apco_N id，不改 call_id。
- [ ] 流式缓存 added，到 done 发原子 start/stop，验证 terminal 快照；禁止 Delta；不要求初始 diff 等于最终 diff。
- [ ] 两目标对专用声明/选择/调用/结果报损，并正确处理混合会话、空 turn、索引、终止与 usage。
- [ ] 补齐缺字段/类型/null、未来 enum、错关联、unknown audit、重复 lifecycle、初末快照和 special-string 回归，观察 RED→GREEN。
- [ ] Go 测试通过后固定源 commit，作为语言 port 的只读输入；规范、向量、实现提交分开。

## W3：四语言独立实现

**固定分块：** TS→ts/**；Rust→rust/**；Python→python/**；C++→cpp/**。每块最多 128 个文件，2700 秒，600 秒无进展停止。每个 worker 使用自己的隔离 worktree，不能改其他目录；中间构建文件不发布。

**唯一发布工件：** 对应 ts-result.json、rust-result.json、python-result.json、cpp-result.json；先 .partial 后原子 rename。Schema 在仓库外 manifest 中，必须含 unit、base、scope、patch、tests、worktree。发布前在私有副本实际测试，不将 git apply --check 冒充功能测试。

**测试覆盖：** 全部共享向量和 W2 的边界输入；旧向量不改、不跳过。每个 worker 先记录新向量在旧语言实现上的失败，再实现并验证。

- [ ] TS：ir 类型/codec/checker/nonstream/generated、Responses、两个目标、审计、向量 runner。验证 npm test、generate:check、lint、fmt；原子结果块不能遗漏 stream codec/归一化入口。
- [ ] Rust：oxa-ir/responses/目标/vectest 的 serde、枚举穷尽分支、optional 输出与原子流式；cargo test --workspace、fmt、clippy -D warnings。
- [ ] Python：typed dataclass Operation union、IR codec/checker、Responses 与 wire audit、目标、向量 runner；unittest 全量执行，核实 worktree 根与向量计数。
- [ ] C++：IR variant/codec/checker、Responses codec/audit、目标及向量 runner；CMake/CTest 全量与 -fno-exceptions；不得读取 move 后的容器来决定 stop reason。

## W4：集成、版本与独立审查

**范围/工件：** 所有已经验证的语言结果，版本文件与发行文档；verification.log。最长 1800 秒。只有本单元写最终汇总；结果有效且 worker 未退出则停止 worker；验证 patch 已保留后回收各临时 worktree/分支。

- [ ] 检查每份结果的 JSON schema、base SHA、路径/文件数/patch 大小/哈希、测试记录；失败不应用。
- [ ] 在主 worktree 应用已验证 patch，重复执行对应语言测试。
- [ ] 更新五语言包及锁文件/版本测试为 2.3.0，更新 README/CHANGELOG 为未发布；IR 仍 0.2.0。
- [ ] 五语言全量、生成类型、架构、常量一致性、manifest、格式、消费者门禁通过。
- [ ] 一个独立 fresh-context 全分支审查覆盖批准设计、实际改动和验收证据；确认问题以回归测试修复，再跑全量。
- [ ] 提交实现/版本/文档的细粒度 commits；不把不相关清理混入。

## W5：GitHub CI

**范围/工件：** elkpi/oxa 的 feature commit 与 PR、同 SHA 的 CI 作业；github-ci.json。最长 1800 秒，有界等待、终态全覆盖；结束停止 watch。

- [ ] 推送 feature 分支，创建用于用户 review 的 PR，正文说明版本、兼容边界和未包含的其他协议补齐。
- [ ] GitHub CI 验证 Go race 矩阵、TS Ubuntu/Windows/Web/包消费者、Rust、Python 矩阵、C++ 三平台与无异常、vectors/manifest/consumers/reliability。
- [ ] 失败读日志、定位、加回归修复，推送并等待同新 SHA 的结果，不改测试来绕过门禁。
- [ ] 所有必需 job success 后输出 PR、SHA、范围、验证与限制。保留工作中的 feature worktree，不合并、打 tag 或发布。
