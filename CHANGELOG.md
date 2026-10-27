# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] — v2.4.12

### Added

- 函数工具新增可选 `strict` 三态 IR 字段，在 Responses、Chat Completions 与 Anthropic Messages 之间保留缺省、`false` 和 `true`，包括 Responses 嵌套容器及 `additional_tools`。
- 扩充共享向量，覆盖三态、各协议编解码、嵌套工具及六个跨协议方向；结构化输出的 `ResponseFormat.Strict` 不变。

### Changed

- 移除原先针对 Responses 函数工具 strict 的 `unmapped-field` 损失；IR contract 保持 `0.2.0`。

## [v2.4.11] - 2026-10-25

### Added

- Anthropic Messages 思考历史、请求控制与引文保真（spec/01, spec/02, spec/12 N-AN-17/18/19, spec/15 §3.1/3.3/3.8, spec/20 §4.4）：
  - **`RedactedThinkingBlock`（`redacted_thinking`）**:
    - 新增密封 IR 内容块 `RedactedThinkingBlock`，判别值为 `redacted_thinking`，携带不透明加密数据载荷 `data: string`。
    - 支持 Anthropic assistant 思考历史多轮往返保真；禁止出现在 user 消息中（作为结构错误拒绝）。
    - 流式事件中作为原子块（atomic block）支持 `content_block_start` 与 `content_block_stop`，拒绝接收任何 delta。
    - 跨协议转换至 OpenAI Chat Completions 或 Responses 时整块安全省略，并生成单条自描述 `unsupported-semantic` 损失（`detail: "anthropic encrypted thinking replay state; the target face cannot preserve this block"`），绝不泄露加密载荷。
  - **`between_tools` 思考模式**:
    - `thinking_mode` 原生注解扩展接受 `"between_tools"`，支持多步工具调用间的思考控制。
    - 严格校验互斥语义：`thinking: {"type": "between_tools"}` 禁止同时声明 `budget_tokens` 或 `display`，违反时作为结构错误拒绝。
  - **`top_k` 采样过滤参数**:
    - 在 IR `Request.native` 容器中保留原生非负安全整数 `top_k`（`0 <= top_k <= 9007199254740991`，含显式 `0`），Anthropic 同面解码与编码保真回转。
    - 跨面转换至 OpenAI 时安全省略并记录自描述 `unmapped-field` 遗漏损失。
  - **`disable_parallel_tool_use` 并行工具开关**:
    - 在 IR `Request.native` 容器中保留布尔值开关，Anthropic 解码时将 `tool_choice.disable_parallel_tool_use` 保真捕获；同面回转时精准恢复至 wire 上的 `tool_choice` 对象中。
    - 结构语义约束：禁止挂载在 `tool_choice: "none"`（或 `"type": "none"`）上，违反时作为结构错误拒绝。
    - 跨面转换至 OpenAI 时作为原生选项安全省略并记录 `unmapped-field` 损失。
  - **文本块引文 `citations` 与流式增量 `citation_delta`**:
    - 在 `TextBlock` 上定义 `citations: []TextCitation`，流式事件中扩展 `Delta` 联合支持 `citation_delta`（`DeltaTypeCitation`）。
    - 结构化校验涵盖 5 种位置类型：`char_location`、`page_location`、`content_block_location`、`web_search_result_location`、`search_result_location`，未知类型优雅报 `unsupported-semantic` 损失，未知属性报 `unmapped-field` 损失。
    - 引文仅允许挂载在 assistant 文本块与响应流中；禁止出现在 user 文本块或 `tool_result` 文本块中（返回结构错误拒绝）。
    - 请求回显保真：响应引文作为后续多轮对话的 assistant 消息回显时，剥离响应专属的 `file_id` 属性，并在 `file_id` 非空时报告明确的 `unmapped-field` 损失。
    - 跨面转换至 OpenAI Chat Completions 或 Responses 时，引文被安全剥离并记录单条 `unsupported-semantic` 损失。
  - Go、TypeScript、Python、Rust、C++ 全部 5 门语言 100% 对齐实现并全量绿灯通过。
- 新增 17 条共享黄金向量（389 → 406），覆盖思考历史往返、冲突校验、top_k、并行工具控制、文本引文结构化校验、回显剥离、流式引文增量及跨面损失。

### Changed

- Spec 版本、五种语言实现版本和状态矩阵统一更新至 `2.4.11`；IR contract 保持 `0.2.0`。
- `spec/schema/ir.schema.json` 扩充 `redactedThinkingBlock`、`textCitation` 与 `citationDelta` 定义。

## [v2.4.10] - 2026-10-24

### Added

- Anthropic Messages: 支持每工具 `eager_input_streaming` 原生三态保真（spec/12、spec/15 §3.6）：
  - 显式 `true` 记录为 `NativeAnnotation`（`source: "anthropic"`, `kind: "eager_input_streaming"`），跨面至 Chat Completions / Responses 时产生 `degraded` 损失（detail: `"target does not preserve eager tool input streaming semantics"`）。
  - 显式 `false` 记录为 `NativeAnnotation`，跨面至默认缓冲流式的目标面时享受双重保障零损（0 loss）放行。
  - wire 缺省时不生成注解，同面编码时严格按原样省略。
  - 非布尔类型输入校验为不可恢复结构类型错误；32 个工具声明携带该参数时彻底杜绝 loss 放大告警（0 解码损失）。
- OpenAI Chat Completions: 标准请求控制（`store`、`stream`、`stream_options`、`max_completion_tokens`）保真与解构（spec/10、spec/15 §3.7）：
  - `max_completion_tokens` 直接映射为通用 IR `Params.MaxTokens` 并通过原生注解保留拼写形态，同面回转精准恢复；与 `max_tokens` 同时出现时作为结构错误拒绝。
  - `store`（模型蒸馏数据留存）解码为原生注解，OpenAI 家族内无损保留；跨异构平台时 `store: true` 严格报 `unsupported-semantic` 损失，`store: false` 零损放行。
  - `stream` 传输标记解码为原生注解，非流式解码器安全接纳，跨面 0 loss，同面精确还原。
  - `stream_options`（`include_usage`、`include_obfuscation`）强类型结构校验，未知扩展子属性精确产生 `unmapped-field` 报损，跨面降级为 `degraded` 损失。
- 安全可观测性辅助工具（spec/15 §7）：
  - 在 IR 模块新增轻量只读损失指纹结构 `LossFingerprint` 与工具函数 `FingerprintLosses`，严格剔除包含潜在敏感数据的 `Detail`，支持按需上限截断，全 5 门语言 100% 同步提供。
- 新增 8 条共享黄金向量（381 → 389），覆盖工具细粒度流式三态回转与跨面降级、Token 限制映射与拼写记忆、标准请求控制回转与跨面转换。

### Changed

- Spec 版本、五种语言实现版本和状态矩阵统一更新至 `2.4.10`；IR contract 保持 `0.2.0`。

## [v2.4.9] - 2026-10-24

### Added

- OpenAI Responses 层级工具容器与原生工具支持（spec/11 N-R-15、spec/15 §3.4、spec/16 N-NT-5）：
  - 支持 `namespace` 与 `toolbox` 层级容器解码，子工具递归扁平化为点分工具名称（如 `editor.submit_patch`、`repo.read`），并通过 `NativeAnnotation` 保留容器类型以供高保真回写重组。
  - 支持 `input[]` 中 `additional_tools` 声明项的解包并提升至请求顶层 `Tools`。
  - 支持 `function_call` 与 `custom_tool_call` 的 `namespace` 属性解析、命名空间规范化与编码拆分。
  - 支持 `tool_choice` 对命名空间工具（包含嵌套与点分名称）的强类型选择与拆分回写。
  - 容器空数组、缺失/非法名称、展平后名称冲突严格校验为结构错误。
  - Responses 原生支持 `defer_loading`（仅限 `true`）无损回转，仅跨面至 Chat Completions 时产生 `degraded` 遗漏损失。
  - 扩展平台内置原生工具声明支持（`web_search`、`file_search`、`code_interpreter`、`computer`、`mcp`、`tool_search` 等），映射为 `type: "native"`、`source: "openai"`，原始 JSON 声明文本严格保真，同源回写，跨面安全省略并记录 `unsupported-semantic` 损失。
  - Go、TypeScript、Python、Rust、C++ 全部 5 门语言 100% 同步实现并全量绿灯通过。
- 新增 13 条共享黄金向量（359 → 372），覆盖命名空间、工具箱、动态注入、命名空间工具调用/选择、OpenAI 原生内置工具声明及其跨协议映射。

### Changed

- Spec 版本、五种语言实现版本和状态矩阵统一更新至 `2.4.9`；IR contract 保持 `0.2.0`。

## [v2.4.8] - 2026-10-23

### Added

- Anthropic Messages `document` 内容块的 typed IR 支持（spec/01、spec/12 N-AN-16、spec/15 §3.5）：
  - 新增请求侧 sealed `DocumentBlock`，以扁平 `media_type` + `data`、`url` 或 `file_id` 三选一建模 `base64`、`url`、`file` source；base64 payload 始终作为不透明文本保留，不解码、不重写。
  - Anthropic Messages 解码/编码支持三种 source 的无损回转，并支持嵌套于 `tool_result.content[]`；缺 source、缺必填字段、非法 citations/title/context 类型均为结构错误，未知 document source 整块产生 `unsupported-semantic` 损失。
  - `title`、`context`、`citations` 作为 document 级 `NativeAnnotation` 保留，`cache_control` 继续走块级原生注解；OpenAI Chat Completions 与 Responses 无 document 语义时整块省略并产生一条 `unsupported-semantic` 损失，不额外重复报告块内注解。
  - Go、TypeScript、Python、Rust、C++ 五种实现同步支持 canonical IR codec、native 校验、原始 JSON 审计、同面回转及跨面损失。
- 新增 7 条共享黄金向量（352 → 359），覆盖三种 document source 的双向映射、嵌套 tool result、未知 source 和两条跨面降级；原 `loss-unknown-block-to-ir` 向量改用真正未知的 `mystery_block`。

### Changed

- Spec 版本、五种实现版本和状态矩阵更新至 `2.4.8`；IR contract 保持 `0.2.0`。
- C++ CMake package version 从此前遗漏更新的 `2.4.5` 对齐至 `2.4.8`。

## [v2.4.7] - 2026-10-22

### Fixed

- Anthropic Messages 工具 GA 属性 `defer_loading` 保真与跨面降级 (spec/15 §3.4, N-NAT-2/3, N-AN-14)：
  - 修复 Anthropic Messages 解码器因审计白名单未包含 `defer_loading` 导致将合法延迟装载工具属性误判为致命 `unmapped-field` 损失（`path: "tools[N].defer_loading"`），阻塞 Claude Code 等使用 Tool Search 的客户端请求走 OXA 路径的问题。
  - 依据 spec/15 Native Request Options 规范制定扩展契约：
    - 在通用 IR 的 `Tool.native` 原生选项容器中建模 `kind: "defer_loading"`，Anthropic 解码时将 `defer_loading: true` 保真捕获，**无解码损失（0 decode loss）**。
    - 严格校验 `defer_loading` 类型（必须为布尔值，非布尔值返回结构解析错误）及 API 冲突语义（同一工具不可同时携带 `defer_loading: true` 与 `cache_control`，冲突时返回明确结构错误）。
    - 针对默认行为 `defer_loading: false` 或缺省，保持 API 缺省语义，不生成附加项与损失。
    - 原生回转保真：编码回 Anthropic 时依据 `Tool.native` 完整恢复 wire 上的 `"defer_loading": true`。
    - 跨协议中立降级：编码至无 Tool Search / 延迟装载支持的目标面（OpenAI Chat Completions、OpenAI Responses）时，由于工具定义本身已完整下发至目标上下文，仅按需装载优化退化，通过既有原生遗漏收集管线自动发射自描述的 `degraded` 损失（`field: "defer_loading", reason: "degraded"`）。
  - 在 Go、TypeScript、Python、Rust、C++ 全部五种语言中同步对齐上述审计白名单、校验、保真与跨面降级实现。
  - 新增 4 个共享黄金向量（向量总数 348 → 352）：
    - `anthropic.nonstream.native-tool-defer-loading-to-ir`
    - `anthropic.nonstream.native-tool-defer-loading-from-ir`
    - `cross.nonstream.anthropic-to-chatcompletions-tool-defer-loading`
    - `cross.nonstream.anthropic-to-responses-tool-defer-loading`
- 版本与规范状态：
  - 更新 Spec 版本至 `2.4.7`，IR contract 保持 `0.2.0`；
  - 更新 README、规范说明与各语言版本状态矩阵至 `v2.4.7` 准备态。

## [v2.4.6] - 2026-10-22

### Fixed

- PR #55 审查修订：
  - 修订 spec/12 N-AN-15：明确本版不识别 server-tool-result 例外（`assistant` 前驱一律结构错误，识别该例外推迟到后续版本）；文档化空 content 块数组的 system 消息行为（不产生 IR 文本块，仅记录 degraded loss）；规范固化 role 的 degraded loss 先于该消息 content 派生损失的输出顺序。
  - 修复 Go、Rust typed 解码路径的报损顺序：`messages[i].role` 的 degraded loss 现先于该消息 content 派生的损失（如非法 `cache_control` 的 unmapped-value），与 spec/02 源 encounter order 一致；TypeScript、Python、C++ 经审计层按源位置排序本就正确，补回归测试钉住。
  - 移除 Go 解码器中恒为 false 的 server tool 例外占位函数，对齐规范修订。
  - 新增共享黄金向量 `anthropic.nonstream.mid-conversation-system-blocks-to-ir`，锁定块数组 content 的 `cache_control` 原生注解保留语义（向量总数 347 → 348）。
- Anthropic Messages 会话中间 `role: "system"` 消息支持与降级折叠规范 (N-AN-15)：
  - 修复 Anthropic Messages 解码器将 `messages[]` 中合法的会话中间 `role: "system"` 消息直接作为未知角色拒绝（`unknown role "system"`），导致 Claude Code 等官方客户端请求无法走 OXA 路径的问题。
  - 依据 Anthropic 官方 GA 规范制定 N-AN-15 规则：
    - 严格校验 placement 约束（不可为 `messages[0]`，首个指令必须使用顶层 `system`；前驱必须为 `user` 角色；后继不可为 `user` 角色）；违规返回明确结构错误。
    - 严格校验 content 约束（仅允许纯文本字符串或 `type: "text"` 块数组，非文本块返回结构错误）。
    - 将会话中间 system 消息文本追加至 IR `Request.System[]`，并保留文本块上的 `cache_control` 原生注解。
    - 由于 IR 请求级 `system` 位于消息序列前缀、无法保留指令中间生效位置，逐条折叠消息记录一条自描述的 `degraded` 损失（`path: "messages[i].role", field: "role"`）。
  - 在 Go、TypeScript、Python、Rust、C++ 五种语言中同步实现上述校验与折叠契约，并通过新增共享黄金向量 `anthropic.nonstream.mid-conversation-system-to-ir`（向量总数达到 347）。
- 版本与规范状态：
  - 更新 Spec 版本至 `2.4.6`，IR contract 保持 `0.2.0`；
  - 更新 README、规范说明与各语言版本状态矩阵至 `v2.4.6` 准备态，标明最新已发布版本为 `v2.4.3`。

## [v2.4.5] - 2026-10-21

### Fixed

- PR #54 审查修正（原生工具声明与 custom 判定）：
  - 修正 TypeScript、Python、Rust、C++ 将 Anthropic 显式 `type:"custom"` 且携带真实 JSON Schema 的普通用户工具误判为自由文本 custom 工具的问题：此前 `input_schema` 被无损失丢弃，回写时替换为单属性包装 schema。现按 spec/16 §3 沿用 schema-wrapper 判定，新增共享黄金向量锁定该行为（向量总数 345 → 346）。
  - 修复 Rust typed 解码路径（`serde_json::from_str` + `decode_request`）丢弃 Advisor 声明中 `model`、`max_uses` 等配置且不报损失的问题；现通过 raw source 捕获完整声明原文。
  - 修复 Python 同协议回写原生声明时浮点字面量被静默取整的问题（如 `9007199254740993.0` → `9007199254740992.0`）；双精度无法精确表示的整数字面量现以精确整数恢复（spec/16 数值恢复语义澄清）。
  - 修复 Go 普通 `ToolWire` 的公开字段修改被 `Raw` 原文回写静默覆盖的问题；原文回写现限定在原生声明边界。
  - 为 TypeScript、Python、Rust、C++ 的 Anthropic 编码入口补齐原生声明身份校验（definition 必须为对象、native type、name 与外层一致），堵住悬空 `tool_choice` 与空工具对象输出；Rust IR 反序列化同步执行 N-NT-1 校验。
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
