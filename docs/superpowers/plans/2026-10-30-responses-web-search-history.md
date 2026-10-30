# Responses web_search_call 历史项实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans 在当前会话按工作单元执行；不启动实现子代理。用户要求不在本地构建或测试，测试结果以本 PR 的 GitHub CI 为准。

**Goal:** 在 v2.4.13 中保留 Responses 非流式请求/响应的 web_search_call，在其他协议目标侧明确报损。
**Architecture:** 独立 WebSearchCallBlock{id,status,action}；Responses assistant run 归并为 thinking、text、calls，服务端搜索不参与客户端工具配对，也不改变 stop_reason。流式不新增支持，保持整项报损及合法生命周期。
**Tech Stack:** Go、TypeScript、Python、Rust、C++；共享 JSON Schema 与 golden vectors。
**Spec:** spec/01 §3.4、INV-2；spec/11 N-R-16；官方 response_function_web_search_param.py。

## Global Constraints

- IR contract 仍为 0.2.0；穷尽匹配需要处理新增块。
- 用户已确认放宽 INV-2：只允许含 web_search_call 的 assistant 首轮历史。
- 按官方 SDK：search 可含 query、queries、sources；open_page.url 允许缺省或 null；find_in_page 的 url、pattern 必须为字符串。
- id/status 为非空字符串；未知 action.type 为结构错误；未知字段保留于 action 并逐字段报 unmapped-field；item 顶层扩展字段保留在可选 `extra` 对象并报损，同源编码恢复。用户已确认该公开 IR 扩展。
- 不合成 function 或 Anthropic server_tool_use；不改变已有 Anthropic 服务端搜索处理。
- 不运行本地测试、构建、依赖安装或打包；允许小型文本生成、格式化和 git diff 校验。统一推送后观察 CI，不合并或发布。

## Review Focus

- 首轮截断历史及混合 function/apply_patch 配对不会丢弃或误认搜索为客户端调用。
- 官方 nullable url、sources、find 必填字段和未知状态字符串在五语言一致。
- 非法直接构造 IR 在 encode 入口也失败；未知 action 字段不被二次过滤。
- 流式跳过块后仍能关闭 block、继续后续块，且只报损一次。
- 请求/响应跨面编码逐块损失，搜索以外的文本和工具不受影响。

## 工作单元

每个单元顺序执行，超过 30 分钟无进展或输入发生冲突时停止该单元；不创建后台实现任务或额外 worktree。临时脚本位于 /tmp 独立目录，结束时删除指定文件；保留现有 worktree 供 PR 反馈。

### 1. 规范和向量
- 输入：spec/、vectors/、CHANGELOG.md。
- 工件：N-R-16、WebSearchCallBlock JSON Schema、请求/响应双向及跨面/流式向量。
- 完成条件：共享向量覆盖三类 action、官方边界、未知字段、首轮历史和目标侧 loss；manifest 同步。
- 验证：GitHub CI vectors、manifest。

### 2. Go / TypeScript / Python 实现及测试
- 输入：go/、ts/、python/；按已有未提交实现继续，移除 Go sentinel 临时设计。
- 工件：公共块、canonical codec、Responses 映射、其他 face 编码 loss、生命周期和回归测试。
- 完成条件：三语言在同一向量集通过；静态自查不能替代 CI。
- 验证：GitHub CI Go 1.23/1.24、typescript、python 3.10–3.13、consumers。

### 3. Rust 实现及测试
- 输入：rust/。
- 工件：Block 枚举变体、验证、codec、映射、审计、所有穷尽分支和回归测试。
- 完成条件：workspace 测试、fmt、Clippy 与 consumers CI 通过。
- 验证：GitHub CI rust、consumers。

### 4. C++ 实现及测试
- 输入：cpp/。
- 工件：variant、codec、验证、Responses/跨面/流式及测试。
- 完成条件：Linux/macOS/Windows 与 no-exceptions CI 通过。
- 验证：GitHub CI cpp 各配置、consumers。

### 5. 汇总与提交验证
- 输入：以上全部最终修改、PR #62。
- 工件：细粒度提交、更新 PR、固定 SHA 的 CI 结果。
- 完成条件：git diff --check、所有必要 CI 和 CodeQL 终态成功；失败必须修复后再验证。
- 验证：gh pr checks 62、gh run view；单次远端观察最长 20 分钟，无无限轮询。

## 已作裁定

- 因用户明确要求，本地 RED/GREEN 验证改由 GitHub CI 执行；不声称未运行的本地测试通过。
- 首轮历史例外由用户单独确认；不为满足 INV-2 合成空 user 消息。
- 之前给出的 action 可选字段说明不完整，以官方 SDK 的 sources、nullable open_page 和 find 必填字段为准。
