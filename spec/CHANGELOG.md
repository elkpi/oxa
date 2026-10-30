# Spec changelog

Version history of the oxa specification itself, independent of the
implementations. The spec follows [Semantic Versioning](https://semver.org/);
precedence between spec, vectors, and schemas is defined in
[README.md](README.md#source-of-truth-precedence).

## 2.4.13 — 未发布

- 更新 N-AN-12，映射 Anthropic `output_config.format` JSON Schema 并保留 schema 原文；未知类型与嵌套字段按规范报损。
- 通用 IR 的 JSON Schema 名称可选；OpenAI 编码要求名称，缺失时省略格式并记录损失，不合成名称。Anthropic 对无法承载的 OpenAI 外层字段分别报损。
- 明确原文 sidecar、过期数据校验和保真 JSON 文本接口的要求；IR contract 保持 `0.2.0`。
- 新增 `WebSearchCallBlock`，非流式映射 Responses `web_search_call` 历史项（N-R-16）。其他协议编码时省略并报 `unsupported-semantic`；流式仍整项报损。穷尽匹配内容块的消费者必须处理新变体。IR contract 保持 `0.2.0`。

## 2.4.12 - 2026-10-27

- `Tool.strict` 是仅适用于 function 的可选布尔值，区分缺省、显式 `false` 和 `true`；不检查或改写不透明的 `input_schema`。
- 三种协议的函数工具 `strict` 布尔值在 IR 和 wire 之间无损映射，Responses 容器与 `additional_tools` 包含在内；OpenAI wire 的 `null` 归一为缺省，编码时省略，不保留原始拼写。
- Anthropic 普通工具的 `strict` 若存在必须为布尔值；携带 `strict` 时，即使 schema 与 custom wrapper 相同也不得识别为 custom。
- IR contract 保持 `0.2.0`。

## 2.4.11 - 2026-10-25

### Added

- [01 — Intermediate Representation](01-intermediate-representation.md)：
  - §3.4 扩展块联合体，新增 `RedactedThinkingBlock`（`redacted_thinking`，包含必填不透明 `data`）。
  - §3.4 扩展 `TextBlock`，新增可选 `citations`（`TextCitation[]`），定义 `TextCitation{source, value}`。
  - §5.2 扩展 Delta 联合体，新增 `CitationDelta`（`citation_delta`，包含 `citation`）。
- [12 — Mapping Anthropic Messages](12-mapping-anthropic-messages.md)：
  - 新增 N-AN-17 规则：支持 `redacted_thinking` 在 assistant 历史、响应与流式中的 1:1 双向映射；跨面转 OpenAI 时省略并报单次 `unsupported-semantic` 损失，不暴露载荷。
  - 新增 N-AN-18 规则：支持五类文本 `citations`（`char_location`、`page_location`、`content_block_location`、`web_search_result_location`、`search_result_location`）解析、审计与回转；流式支持 `citations_delta`；跨面转 OpenAI 时省略引文并报单次 `unsupported-semantic` 损失；响应引文复用为 assistant 历史时对非空 `file_id` 报 `unmapped-field` 损失。
  - 新增 N-AN-19 规则：支持 `thinking.type = "between_tools"` 独立原生模式，禁止与 `budget_tokens`/`display` 冲突联用；支持顶层显式非负安全整数 `top_k`（含 `0`）原生注解保真；支持 `tool_choice` 的 `disable_parallel_tool_use` 布尔原生注解保真。
- [15 — Native Request Options](15-native-request-options.md)：
  - §2 宿主表在 `Request` 上增加 `top_k`、`disable_parallel_tool_use`，并在 `thinking_mode` 增加 `between_tools`。
  - §3.1 增加 `between_tools` 模式与互斥约束。
  - §3.7 规范定义 `top_k` 非负安全整数校验与跨面遗漏规则。
  - §3.8 规范定义 `disable_parallel_tool_use` 布尔校验、宿主约束与跨面遗漏规则。
- [20 — Streaming Semantics](20-streaming-semantics.md)：
  - §11.3 规范定义 `RedactedThinkingBlock` 流式 start/stop 生命周期（不接受任何 delta）与 OpenAI 跨面单次报损。
  - §11.4 规范定义 `CitationDelta` 在打开的 `TextBlock` 中的增量聚合规则与 OpenAI 跨面单次报损。
- [schema/ir.schema.json](schema/ir.schema.json)：
  - 增加 `redactedThinkingBlock`、`requestRedactedThinkingBlock`、`textCitation`、`citationDelta` 定义，并在联合体中闭合。

## 2.4.10 - 2026-10-24

### Added

- [12 — Mapping Anthropic Messages](12-mapping-anthropic-messages.md)：
  - 支持 `tools[].eager_input_streaming` 三态解析与回转（`true`/`false`/缺失）。
  - 非布尔类型输入校验为不可恢复结构类型错误。
- [10 — Mapping OpenAI Chat Completions](10-mapping-openai-chat-completions.md)：
  - 支持 `max_completion_tokens` 映射至 `Params.MaxTokens` 并通过原生注解保留拼写形态，与 `max_tokens` 冲突互斥报错。
  - 支持 `store`、`stream`、`stream_options` 标准请求控制参数解码为原生注解。
  - 支持 `stream_options`（`include_usage`、`include_obfuscation`）结构校验与未知子字段审计报损。
- [15 — Native Request Options](15-native-request-options.md)：
  - §2 表格新增 `Tool` 宿主 `eager_input_streaming` 原生选项，以及 `Request` 宿主 `max_completion_tokens`、`store`、`stream`、`stream_options` 原生选项。
  - §3.6 规范定义 `eager_input_streaming` 解析保真、正交性、原生回转以及跨面（OpenAI CC/Responses）时 `true` 报 `degraded` 损失、`false` 报 0 损失的双重过滤契约。
  - §3.7 规范定义 `max_completion_tokens` 拼写保真、`store` 跨面报损（`store:true` 报 `unsupported-semantic`，`store:false` 零损）、`stream` 传输标记零损、`stream_options` 降级报损。
  - §7 规范定义轻量且 PII 安全的损失指纹辅助工具 `FingerprintLosses`。

## 2.4.9 - 2026-10-24

### Added

- [11 — Mapping OpenAI Responses](11-mapping-openai-responses.md)：
  - 新增 N-R-15 规范规则，支持层级工具容器（`namespace`、`toolbox`）递归平铺为点分工具名称，并通过原生注解保留容器来源以支持编码时高保真重组。
  - 支持 `input[]` 中 `additional_tools` 声明项的解包并提升至请求顶层 `Tools`。
  - 支持 `function_call` 与 `custom_tool_call` 的 `namespace` 属性解析、命名规范化与编码拆分。
  - 支持 `tool_choice` 针对命名空间工具的解析与编码回写。
  - 容器展开后的重名冲突、空容器、缺失或非法命名空间名称严格校验为结构错误。
  - 跨面转换至不支持工具容器的协议（Chat Completions、Anthropic）时，记录容器层级降级 `degraded` 损失。
- [15 — Native Request Options](15-native-request-options.md)：
  - 明确 Responses 协议原生支持 `defer_loading`（仅限 `true`），支持双向无损回转；仅在跨面至 Chat Completions 时产生 `degraded` 遗漏损失。
- [16 — Native Tool Declarations](16-native-tool-declarations.md)：
  - 扩展原生工具声明来源至 `openai`（N-NT-5），覆盖 Responses `tools[]` 中的 `web_search`、`file_search`、`code_interpreter`、`computer`、`mcp`、`tool_search` 等平台原生/内置工具声明。
  - 原始声明 JSON 文本严格保真，同源无损回转，跨源省略并记录 `unsupported-semantic` 损失。

## 2.4.8 - 2026-10-23

### Added

- [01 — Intermediate Representation](01-intermediate-representation.md)：请求侧 sealed `Block` 联合新增 `DocumentBlock`；定义 `data` / `url` / `file_id` 三选一来源、base64 原文保留以及穷尽匹配消费者的迁移要求。
- [12 — Mapping Anthropic Messages](12-mapping-anthropic-messages.md)：新增 N-AN-16，规范 Anthropic `document` 的 source 映射、结构校验、tool_result 递归位置、原生回转及无 document 目标面的整块降级损失。
- [15 — Native Request Options](15-native-request-options.md)：新增 document `title`、`context`、`citations` 原生注解宿主、校验和跨面遗漏规则。
- IR JSON Schema 的 request block union 新增 `document` 变体；IR contract 维持 `0.2.0`。

## 2.4.7 - 2026-10-22

### Added

- [15 — Native Request Options](15-native-request-options.md)：
  - §2 表格新增 `Tool` 宿主支持的 `defer_loading`（仅限 `true`）原生选项。
  - §3.4 与 §6 规范定义 `defer_loading` 解析保真（允许合规 JSON 空白）、`false`/缺省默认行为、与 `cache_control` 的互斥性约束（解码与 IR 校验双向防御）、原生回转以及跨面至 OpenAI（Chat Completions / Responses）时的 `degraded` 降级遗漏损失。
- [12 — Mapping Anthropic Messages](12-mapping-anthropic-messages.md)：
  - 增加 `tools[].defer_loading` 的解码保真与跨面降级损失映射。
- [13 — Custom Tools](13-custom-tools.md)：
  - 明确 `defer_loading` 已由 `spec/15` NativeAnnotation 显式建模，移出未知字段审计列表。

## 2.4.2 - 2026-10-18

### Changed

- [15 — Native Request Options](15-native-request-options.md)：
  - §4.1 规范明确当 `cache_control` 的 `omission` 缺省或其 `detail` 为空时（如直接构造或旧版 IR），跨面编码至目标面时统一缺省补齐 §3.2 规定的规范化 `detail` 常量并报 `unmapped-field` 遗漏。

## 2.4.1 - 2026-10-18

### Changed

- [15 — Native Request Options](15-native-request-options.md)：
  - §3.2 规范定义 `cache_control` 跨面至不支持 cache 断点的目标面时的规范化 `detail` 常量文本。
  - §4 显式声明 Chat Completions 与 Responses 均无 cache 断点表示，跨面编码必然产生自描述的 `unmapped-field` 遗漏。
  - §4.1 修订遗漏元数据遍历规则，将 `detail` 与 `Field`、`Reason` 并列由附加项元数据提供。

## 2.4.0 - 2026-10-18

### Added

- [15 — Native Request Options](15-native-request-options.md) 定义通用 IR 请求原生配置受限保留机制（N-NAT-1～6）：Request、SystemBlock、Tool、及 Request content blocks 携带可选 `native: NativeAnnotation[]`。
- Anthropic Messages 请求原生配置保真（N-AN-14）：`thinking.type`（`adaptive` / `disabled` / `enabled`）、`thinking.budget_tokens`、`thinking.display`、`output_config.effort`、各层 `cache_control` 及 `context_management.edits` 在 Decode 时保留至 IR，并在 Encode 原生回转时完整恢复。
- 通用 IR `Params.ReasoningEffort` 扩展支持 `xhigh` 与 `max`，在 Anthropic、Chat Completions 与 Responses 间对等映射。
- 跨协议遗漏报损：当包含原生配置的请求转换为不支持对应特性的协议（Chat Completions / Responses）时，在 Encode 阶段按规范固定顺序和去重规则准确报损。
- 显式编辑优先原则：通用 IR effort 改变后旧预算不能覆盖新值，只恢复有效配置。
- IR contract 保持 `0.2.0`（兼容读取 `0.1.0`）。

## 2.3.1 - 2026-10-18

### Added

- OpenAI Chat Completions 原生 Custom Tools 支持（N-CC-14）：嵌套 `custom: {name, description?, format}` 声明、`custom: {name}` choice、`tool_calls[type=custom].custom.{name, input}` 调用、`role: "tool"` 结果及流式双向映射。
- Anthropic Messages 结构化包装 Custom Tools 支持（N-AN-13）：单属性 `input_schema: {"type":"object","properties":{"input":{"type":"string"}}}` 声明、`tool_use.input: {"input": "..."}` 调用、`tool_result` 结果及流式缓冲/解包回放双向映射。
- 消除 Responses ⇄ Chat Completions / Anthropic 转换中的 Custom Tools 降级损耗，达成跨三大协议面对等互通。
- IR contract 保持 `0.2.0`（兼容读取 `0.1.0`）。

## 2.3.0 - 2026-10-16

### Added

- [14 — Apply Patch](14-apply-patch.md) 定义专用的 `apply_patch` Tool/ToolChoice 声明、三种扁平 Operation 联合体、`ApplyPatchToolUseBlock` 与 `ApplyPatchToolResultBlock`。
- 请求端成对关联校验（N-AP-3, INV-3）与成对语义过滤；未知操作类型或未知状态成对丢弃并记录对应 loss，杜绝悬垂引用。
- Responses 原子项流式生命周期（N-AP-4），added 时暂存快照，done 时验证并回放单组 block start/stop，活跃期间严禁任何增量。
- Chat Completions 与 Anthropic Messages 面对 Apply Patch 的规范降级（N-AP-5），记录 `unsupported-semantic` loss 并正确降级 `stop_reason` 至 `end_turn`。
- 新增 58 条 Apply Patch 黄金向量，全仓库向量总数达到 263 条；IR contract 保持 `0.2.0`（兼容读取 `0.1.0`）。

## 2.2.0 - 2026-10-16

### Added

- [13 — Custom Tools](13-custom-tools.md) 定义独立的 custom 调用/结果 Block 和文本 Delta，扩展 Tool/ToolChoice 及 INV-1/3/4/5。
- Responses 非流式与流式双向映射，含任意文本、grammar、内容列表、关联及不支持目标的显式 loss。
- IR contract 保持 `0.2.0`（兼容读取 `0.1.0`）；新增 20 个共享向量，总数 205。
- 规划在后续版本（如 2.3.0）补齐 Chat Completions 与 Anthropic Messages 的对等能力支持。

## 2.1.x - Unreleased

### Added

- Responses input messages accept `developer` alongside `system`, `user`, and
  `assistant`. N-R-1 retains instruction precedence over user content and the
  shared system/developer encounter order after top-level `instructions`.
- Developer instructions map to the existing IR system field with one
  `degraded` loss per developer role; the role distinction cannot round-trip.
- Instruction-only requests gain an empty user message with an explicit
  `degraded` loss, preserving the existing IR request schema.
- Twelve shared vectors cover developer messages, content parts, ordering,
  instruction-only normalization, and conversion to Chat Completions and
  Anthropic Messages. No IR schema or contract version changes.

## 2.1.0 - 2026-09-27

Minor evolution adding structured-output format mapping across all protocol
faces. IR documents continue to use `specVersion: "0.2.0"`.

### Added

- `Params.ResponseFormat` supports `text`, `json_object`, and `json_schema`
  (`name`, optional `description`, verbatim `schema`, optional `strict`).
- Chat Completions `response_format` and Responses `text.format` map losslessly
  through IR; Anthropic encoding drops the preference with one documented
  `unmapped-field` loss and does not synthesize tools.
- Nine shared golden vectors cover the new face and cross-protocol mappings;
  all five language implementations validate 163 vectors.

### Clarified

- The existing spec/02 unknown-field rule applies recursively to recognized
  face objects and native stream event objects. A whole unsupported unit still
  produces only its whole-unit loss; opaque payloads are not inspected. No IR,
  schema, or loss reason is added.
- Raw JSON implementations preserve source encounter order when combining
  field-level and semantic losses.

## 2.0.0 - 2026-09-19

Major milestone extending the protocol-conversion specification with reasoning
content, request-side reasoning effort, and granular usage accounting.

### Added

- **Reasoning content**: `ThinkingBlock` with model reasoning text and optional
  opaque provider `signature` (spec/01 §3.4); streaming `ThinkingDelta` and
  `SignatureDelta` (spec/01 §5.2); M9 streaming reasoning profile (spec/20 §11,
  rule N-S-11).
- **Request reasoning effort**: `Params.ReasoningEffort` enum (`minimal`, `low`,
  `medium`, `high`), mapped natively to/from OpenAI CC and Responses, and mapped
  to/from Anthropic `thinking.budget_tokens` via documented approximation table.
- **Usage granularity**: optional `cache_read_input_tokens`,
  `cache_creation_input_tokens`, `input_tokens_details.cached_tokens`, and
  `output_tokens_details.reasoning_tokens` (spec/01 §4.2).
- Per-face mappings: N-CC-12 (Chat Completions `reasoning_content`), N-R-13
  (Responses `reasoning` item and summary parts), and N-AN-11 (Anthropic
  `thinking` blocks and budget mapping).

### Changed

- Specification version promoted to **2.0.0** following sealed-union extensions
  for `Block` and `Delta`.
- The IR contract `specVersion` supports both `"0.1.0"` and `"0.2.0"` on read;
  converters emit `"0.2.0"`.

## 1.0.1 - 2026-09-19

### Added

- Glossary (`spec/90-glossary.md`): shared cross-language terminology for
  faces, the IR, invariants, vectors, losses, and streaming profiles.
  Documentation only: no rule, schema, or vector change; the IR contract
  `specVersion` remains `0.1.0`.

### Changed

- Loss conventions: normative bucket taxonomy (derived, envelope, unmapped)
  migrated into `spec/02-loss-policy.md` §9; per-face mapping documents (10–12)
  and `vectors/README.md` cross-referenced accordingly with no behavioral change.

## 1.0.0 - 2026-09-04

All four supported languages — Go, Rust, Python, and C++ — implement the
specification against the identical 125 golden vectors. The specification reaches
1.0.0 stability.

### Changed

- Specification promoted to 1.0.0 following the multi-language implementation
  milestones (0.0.x Go, 0.1.0 Rust, 0.2.0 Python, 0.3.0 C++, 1.0.0 all languages).
- Future additions to sealed unions or enums will constitute major version bumps.

## 0.0.2 - 2026-09-02

The 0.0 series is **frozen** as the specification baseline for the Rust
implementation (see the versioning policy in [README.md](README.md)).

### Added

- Versioning policy (README): the two version axes (spec version vs. the
  IR contract `specVersion`), the cadence tied to the implementation
  roadmap (0.0.x Go baseline, 0.1.0 Rust, 0.2.0 Python, 0.3.0 C++, 1.0.0
  only once every supported language implements the spec against the same
  vectors), and the additive-only freeze rules.

### Changed

- Fixed stale forward references: mapping documents 10–12 are no longer
  marked "planned" in 00 §1 and no longer call 20 "planned" in their
  §6; 00 §3 cites document 20 as landed; the glossary (90) notes when it
  will land.
- Pinned the RE streaming encoder's zero-delta rule: a tool block with no
  supplied argument deltas synthesizes one full-arguments
  `response.function_call_arguments.delta`, matching the CC and AN
  encoders and the Go implementation (20 §10.1).
- Repointed the schema `$id` identifiers from the `oxa-protocol`
  placeholder to `elkpi` (01 §6): v0.0.1 already shipped the final module
  path. Identifier-only change; no validation behavior.

## 0.1.1 - 2026-08-28

### Changed

- Clarified N-AN-4: raw tool-input byte fidelity is normative for
  JSON-decoded (typed) paths; generic in-memory inputs convert with
  canonicalized bytes. No schema or vector-format change.

## 0.1.0 - 2026-08-28

Initial spec core (00–03, IR and loss schemas).

### Added

- `spec/00` — scope, non-goals, stream-equivalence principle, hub-and-spoke
  architecture.
- `spec/01` — the intermediate representation: request, message, block,
  response, and event types; invariants INV-1 through INV-9.
- `spec/02` — loss policy: the loss record, reason codes, the error/loss
  boundary, streaming rules.
- `spec/03` — model handling: verbatim pass-through, the modelmap
  injection point, protocol-parameter asymmetry principles.
- `spec/schema/ir.schema.json` and `spec/schema/loss.schema.json`
  (JSON Schema 2020-12).
