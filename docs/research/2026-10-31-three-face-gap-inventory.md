# 三脸协议缺口清单

**调查日期：** 2026-10-31

**状态：** 调研结论。不改 `spec/`，不改实现，不写转换方案的字节级映射。

这是 Chat Completions、Responses、Anthropic Messages 相对 oxa 规格 **2.4.13**（IR contract **0.2.0**）还没接通的官方字段。事实源是三家 API reference；仓库侧以 `spec/10`、`spec/11`、`spec/12`、`spec/15`、`spec/16`、`spec/20` 已写明的 loss 为准。语言之间谁没跟上规格，不在本清单里。

## 版本号

不要把这份清单整包做成 **2.4.14**，也不要做成 **3.0.0**。

规格自己的演进规则（`spec/README.md`）是：patch 修 bug 和小澄清；minor 在同一套 face → IR → face 框架里补能力；major 只给架构重写。新增 sealed union / enum 变体必须写明穷尽匹配的迁移要求，**不因此升 major**。同一系列里已经写明的 loss 语义不能改；新的可选语义用 minor 引进。

| 版本 | 装什么 | 不装什么 |
|---|---|---|
| **2.4.14**（patch） | P0：把代码里已经在报、规格没写的 loss 补进规格。不改变「接通之后的成功路径」。 | 任何会让现有向量从「有 loss」变成「无 loss」的接通。 |
| **2.5.0**（minor） | 下面的 P1。新块、新枚举值都是可选变体。IR contract **保持 0.2.0**，沿用 2.4.11 / 2.4.13 的做法，但 changelog 必须写穷尽匹配的迁移说明。 | 传输层、Realtime、执行工具、通用未知字段透传。 |
| **3.0.0** | 不适用。没有转换管线重写。修订 `spec/16` 的跨脸损失也仍然是 hub-and-spoke 里的映射变化。 | — |

changelog 已经有 2.4.13。P0 用它的下一个 patch。若 2.4.13 实际上还没封板，P0 留在 2.4.13，不要为了一条规格补句再跳号。P1 仍然不进 2.4 系列：接通引用、文件、缓存断点或原生工具，会改掉今天向量里已经钉死的 `unsupported-semantic` / `unmapped-field`。

P3 和「待定」本身不构成一次发布。只补「规格没点名、但通用未知字段审计已经会报 loss」的，不必发版。

## 结论

优先做 **2.5.0** 的是这些 P1。两张脸有、第三张没有，也算 P1；缺的那张脸记有序 loss。

1. 请求 `metadata` 三脸都有，IR 有字段但映射写明丢弃。
2. `developer` 指令身份。Responses 同脸都回不去；Chat Completions 的规格角色表里还没有它。
3. 推理回放凭证：`encrypted_content`、`redacted_thinking`、thinking `signature`。建议一个不透明槽，不解码，密文不进 loss。
4. 引用：Anthropic citations 对 OpenAI annotations。
5. 用户文件：Anthropic `document` 对 Chat `file`、Responses `input_file`。
6. 拒绝正文。终止原因 `refusal` 已经接通；缺的是 refusal 文本和 Anthropic `stop_details`。
7. `verbosity`（Chat 与 Responses；Anthropic 记 loss）。
8. 显式缓存：`prompt_cache_breakpoint` / `prompt_cache_key` 对 Anthropic `cache_control`。规格仍写 OpenAI 没有缓存断点表示，这句已经过时。
9. 并行工具开关：`parallel_tool_calls` 对 `disable_parallel_tool_use`（极性相反）。
10. `logprobs`（两张 OpenAI 脸；Anthropic 记 loss）。
11. Responses `web_search_call` 的流式。非流式块已经在 IR 里。
12. 五个原生工具角色：web search、code execution、computer、MCP、tool search。P1 只接通角色。带日期的 type 字符串各留在原脸，不互相映射，preview 不接正式版。这会建议以后改 `spec/16`，本文件不改。
13. 上下文压缩：Responses `compaction` 对 Anthropic beta `compaction` / `context_management`。

P2 本轮是空的。P0 只有一条规格补写。

## 来源与没核完的部分

| 脸 | Reference |
|---|---|
| Chat Completions | https://platform.openai.com/docs/api-reference/chat/create |
| Responses | https://platform.openai.com/docs/api-reference/responses/create |
| Responses 流式 | https://platform.openai.com/docs/api-reference/responses-streaming |
| Messages | https://docs.anthropic.com/en/api/messages |
| Messages beta | https://platform.claude.com/docs/en/api/http/beta/messages |

没核完，清单里不当成「不存在」：

- Chat Completions 与 Responses 的 `/create` 页面一次抓取是 404，另一次读到了正文。下面 OpenAI 字段以此次读到的正文为准，不是无缓存浏览器的完整快照。
- 独立的 Responses WebSocket 事件参考没打开。只确认 create reference 把 WebSocket `response.create` 写成 Responses 的调用形式，并把 `incomplete_details.reason = steered` 写成 `response.steer` 的结果。见文末「传输差异待核」。
- Anthropic beta 条目多数只确认 header 名是 `anthropic-beta`，没有逐项确认 header 的值。下面只在 reference 有明确句子时写出值。
- 各家 computer / search / code execution 的独立指南页没有逐页打开。类型来自主 reference 里展开的 schema。

官方 schema 自相矛盾、但不单列成 oxa 缺口：Chat 请求和最终 `tool_calls` 已有 `custom`，本次读到的 Chat Completion Chunk 仍把 `delta.tool_calls[].type` 写成可选 `function`，没有 custom delta。

SDK-only 字段不收。Responses 顶层 `output_text` 被 reference 写成 SDK 便捷属性。

## P0 — 规格没承诺

| 项 | 脸 | 现状 | 建议 | 版本 |
|---|---|---|---|---|
| Thinking `signature` 编到 Responses | Anthropic → Responses，以及 IR → Responses 流式 | 规格 N-R-13 和 `spec/20` §11.2 只写了 `encrypted_content` 丢弃，以及 CC 编码丢 signature。仓库调研读到 Go Responses 请求历史、非流式响应和流式编码会对 `ThinkingBlock.Signature` 记 `unmapped-field`（`go/openai/responses/encode.go`、`go/openai/responses/streamout.go`）。这不是规格承诺。 | 2.4.14 把现有损失写进规格。不在 patch 里规定它如何进不透明槽。接通归下面的 P1。 | 2.4.14 |

没把「通用未知字段审计」算成静默丢弃。`spec/02` 已经要求 JSON 入口按存在性报 `unmapped-field`。某个新字段若只是还没点名，先看这条总规则，不单开 P0。

## P1 — 同一角色，IR 没接通

建议动作都是「接通」。有损可以，但必须是有序 loss。没有落点的脸不要造假字段。beta 打标，不能单凭 beta 扩 IR。

### 请求身份、缓存、采样

| 项 | 官方字段 | IR 今天 | 建议 | 标记 |
|---|---|---|---|---|
| metadata | 三脸都有请求 `metadata`。Anthropic 形状是 `{user_id}`，不是任意字符串图（N-AN-8）。 | `Request.Metadata` 存在。三脸解码都是整图一条 `unmapped-field`，反向同样丢。 | 接通这张图。Anthropic 只保留 `user_id`，其余键记 loss。不要把 `safety_identifier` 塞进这张图，那一对在「待定」。 | — |
| developer | Chat 消息 `role: developer`（规格角色表只有 system / user / assistant / tool）。Responses `developer` 进 `Request.System`，每条 `degraded`，编回 Responses 也恢复不了（N-R-1）。 | 没有指令层级，只有文本化的 System。 | 增加指令层级身份。Anthropic 没有 developer 时记 loss。提取后补的空 user message 仍是结构 `degraded`，不单独立项。 | — |
| verbosity | Chat `verbosity`；Responses `text.verbosity`（`low` / `medium` / `high`）。规格只给 Responses 记了 `unmapped-field`（N-R-8）。 | 无。 | CC ↔ Responses 接通。Anthropic 记 loss。 | — |
| 显式缓存 | Chat / Responses：`prompt_cache_breakpoint`、`prompt_cache_key`、`prompt_cache_options`。Anthropic：`cache_control`（`ephemeral`，ttl `5m` / `1h`），规格用 NativeAnnotation 保留，编到 OpenAI 时逐断点 `unmapped-field`。`spec/15` 的 detail 仍写目标脸没有 cache 表示。 | 只有 Anthropic 原生注解，没有跨脸缓存断点。 | 断点对 `cache_control`。`prompt_cache_key` 只在两张 OpenAI 脸之间回转，Anthropic 记 loss。TTL 枚举对不上的值记 loss，不要把 `30m` 折成 `5m`。`prompt_cache_retention` 已弃用，不新开字段。 | OpenAI 字段本次未标 beta |
| 并行工具 | Chat / Responses `parallel_tool_calls`。Anthropic `tool_choice.disable_parallel_tool_use`。规格明确这两者今天不是互映射（N-AN-19，`spec/15` §3.8）。 | 两边各自丢，或只留 Anthropic 原生注解。 | 接通为一个布尔语义，极性取反。无法表达的一侧记 loss。 | — |
| logprobs | Chat 请求 `logprobs` / `top_logprobs` 已记 `unmapped-field`。Responses 另有请求 `top_logprobs`、`include` 里的 `message.output_text.logprobs`，以及输出文本上的 `logprobs`。 | 无 logprob 载荷。 | 两张 OpenAI 脸接通采样开关和输出 logprob 载荷。Anthropic 记 loss。不要为了 logprobs 改文本块的必填形状。 | — |
| moderation | Chat 与 Responses 请求 `moderation`（model、policy）。 | 规格未点名。 | 两张 OpenAI 脸回转。Anthropic 记 loss。 | — |

### 内容块

| 项 | 官方字段 | IR 今天 | 建议 | 标记 |
|---|---|---|---|---|
| 推理回放凭证 | Responses reasoning `encrypted_content`（可经 `include: reasoning.encrypted_content` 取回），丢弃并记 `unmapped-field`（N-R-13）。Anthropic `redacted_thinking.data` 同脸保留，编到 OpenAI 整块 `unsupported-semantic`，禁止把密文写成文本或写进 loss（N-AN-17）。thinking `signature` 在 Anthropic 和 CC 有规格；Responses 见 P0。 | `RedactedThinkingBlock`、`ThinkingBlock.Signature` 只在部分脸上有落点。 | 一个不透明槽，只做原样搬运。不解码。密文不进 loss detail。某一侧没有 wire 落点时记 loss，不造可见 thinking 文本。本清单不规定三种凭证的字节如何互转。 | — |
| 引用 | Anthropic 五类 citation 已进 `TextCitation`，编到 OpenAI 只留正文并记 `unsupported-semantic`（N-AN-18）。Responses 非空 annotations 记 `unmapped-field`；类型有 `file_citation`、`url_citation`、`container_file_citation`、`file_path`。Chat 返回 annotation 类型 `url_citation`。 | 有 citation 结构，没接到 OpenAI annotations。 | 接通。位置类型对不上的记 loss。Anthropic 回放时非空 `file_id` 的现有损失保留到映射写清为止。 | — |
| 用户文件 | Anthropic `document`：source `base64` / `url` / `file` 已保真；`text` / `content` 规格写明 future，整块 `unsupported-semantic`（N-AN-16）。Chat 内容块 `file`（`file_data` / `file_id` / `filename`）。Responses `input_file`（另有 `file_url`、`detail`）。规格仍认为两张 OpenAI 脸没有 document 契约，整块丢弃。 | 有 `DocumentBlock`，只从 Anthropic 三种 source 进来。 | 接通「用户提供的文件」这个角色。source 种类对不上时记 loss。`plain-text` / `content` 仍只有 Anthropic 有，不因此把整个角色降成 P3；这两种 source 编到 OpenAI 继续 loss。 | — |
| 拒绝正文 | 终止原因已经接通：CC `content_filter` → `refusal`；Responses `refusal` → `failed` + `error.code = refusal`；Anthropic `stop_reason: refusal` → `refusal`。没接通的是正文：Chat / Responses 的 `refusal` 内容，以及 Anthropic `stop_details`（`category`、`explanation`）。 | 只有停止原因，没有拒绝正文块。 | 接通正文。Anthropic 的 category 枚举在 OpenAI 无落点时记 loss。不要再加一个停止原因。 | — |

### 同脸流式断点

| 项 | 官方字段 | IR 今天 | 建议 |
|---|---|---|---|
| `web_search_call` 流式 | Responses 事件 `response.web_search_call.in_progress` / `searching` / `completed`。非流式 N-R-16 已恢复 ID、status、action。流式对任何目标脸，包括 Responses 自己，都是整项 loss、没有内容事件（`spec/20` §9）。 | 有 `WebSearchCallBlock`。 | 流式路径发出与非流式相同的块。跨脸仍然可以再记 `unsupported-semantic`，但 Responses 自己的流式不该丢。 |

`function_call_output` 和 Anthropic `server_tool_use` 的流式今天也是整项 `unsupported-semantic`（`spec/20` §9、§10.2）。它们不单独立项，跟下面的工具角色一起做：角色接通了，流式就要能发出对应的块。

### 原生工具角色

`spec/16` 今天只做同源 opaque 声明：跨来源整份声明 `unsupported-semantic`，并且写明不新增 `server_tool_use`、结果块、响应和历史映射。下面每条都是在建议以后改这条约定。声明、调用、结果要一起看。只回转声明不算接通。

P1 不把日期版本互相映射，也不把 preview 接成正式版。每个 type 字符串在原脸保留。原脸回转丢了自己的 type 字符串，才是那一行的保真问题。

| 角色 | 原脸 type（各留各的字符串） | 今天 | 标记 |
|---|---|---|---|
| web search | Responses `web_search`、`web_search_2025_08_26`；preview：`web_search_preview`、`web_search_preview_2025_03_11`。Anthropic `web_search_20250305`、`web_search_20260209`、`web_search_20260318`。Chat 是请求字段 `web_search_options`，不是 tools[] 成员。调用记录：Responses `web_search_call`；Anthropic `server_tool_use` + `web_search_tool_result`。 | 声明可同源 opaque。调用只在 Responses 非流式进 IR，跨脸丢。 | preview 与正式版不互映射 |
| code execution | Responses `code_interpreter`。Anthropic `code_execution_20250522`、`code_execution_20250825`、`code_execution_20260120`、`code_execution_20260521`。 | 同上，跨脸声明丢弃。结果块没有 IR。 | 不把 bash、text editor 并进这一角色 |
| computer | Responses `computer`；preview：`computer_use_preview`。Anthropic beta：`computer_20241022`、`computer_20250124`、`computer_20251124`。常规 reference 另有 `computer_toolset_20260801`（工具集，可以没有 `name`）。 | 声明 opaque。无计算机动作块。 | preview 不接正式版；Anthropic 带日期的 computer 工具标 beta。`browser_toolset` 不并进，见待定 |
| MCP | Responses 工具 `mcp`，以及 item：`mcp_list_tools`、`mcp_call`、`mcp_approval_request`、`mcp_approval_response`。Anthropic beta：`mcp_servers`、`mcp_toolset`、`mcp_tool_use`、`mcp_tool_result`。 | Responses 声明可同源保留。没有审批 / list 的跨脸落点。 | Anthropic 侧 beta。inline `mcp_toolset` 的明确 header 值是 `anthropic-beta: mcp-client-2026-09-15` |
| tool search | Responses `tool_search`（`execution: server / client`）。Anthropic `tool_search_tool_bm25_20251119`、`tool_search_tool_regex_20251119` 及不带日期的别名。 | 声明 opaque。 | `execution: client` 在另一张脸没有落点时记 loss，不要折成 server |

容器和 namespace 不是第六个角色。Responses `namespace` 已由 N-R-15 同源重组；编到没有容器的脸是已登记的 `degraded`。见附录。

### 上下文压缩

| 项 | 官方字段 | IR 今天 | 建议 | 标记 |
|---|---|---|---|---|
| compaction | Responses：`context_management[]`（读到的 type 是 `compaction`）、item `compaction`、输入 `compaction_trigger`。Anthropic beta：请求 `compaction: {type: summarize}`、`context_management.edits[]` 含 `compact_20260112`，以及 `clear_tool_uses_20250919`、`clear_thinking_20251015`；停止原因另有 `compaction`。 | Anthropic `context_management` 是原生注解。未知 edit 整项 loss。没有接到 Responses 的 compaction。 | 接通「压缩上下文」这个角色。Anthropic 的 clear-tool / clear-thinking edit 在 Responses 没有同名落点时记 loss，不要当成 compaction 本身。 | Anthropic 手动 compaction 为 beta，header 名 `anthropic-beta`，本次没读到逐项值 |

## P2 — 规格写了 deferred，且现在两侧都有

没有。

最接近的两条都不满足「两侧现在都有同一种东西」：

- Anthropic document 的 `source.type = text / content` 规格写成 future。OpenAI 新出现的是 `file` / `input_file`，不是这种 source。角色归 P1，这两种 source 仍只在 Anthropic。
- 规格推迟识别的「assistant 以 server tool result 结尾时，后面可以跟 system」（N-AN-15）仍是 Anthropic 单侧例外，不是两家都有的字段。继续推迟，不升 P2。

## P3 — 只有一张脸有

不扩 IR。原脸保真回写；别的脸记有序 loss。已经在规格里记过 loss 的，不重复列在这里，去附录。

| 字段 | 脸 | 备注 |
|---|---|---|
| `frequency_penalty`、`presence_penalty`、`logit_bias` | Chat | 采样专有。 |
| `modalities`、`audio`、`input_audio` | Chat | 音频输入输出。流式最后一条音频更新可以只有 expiry。 |
| `prediction` | Chat | 预测内容。用量里的 accepted / rejected prediction tokens 跟这条一起，不单开 IR。 |
| `seed` | Chat | reference 标了 Beta，没有额外 beta header。 |
| `n` 以外的单 choice 行为 | — | `n` / 多 choice 规格已处理，见附录。 |
| `background`、`previous_response_id`、`conversation`、`truncation`、`max_tool_calls`、`include`、`prompt`（模板 id / variables / version） | Responses | 会话续接和请求开关。`include` 是取回可选载荷的开关，载荷本身归对应 P1，不把整个 include 枚举做成 IR。 |
| assistant `phase`（`commentary` / `final_answer`） | Responses | 中间解说对最终回答。 |
| `access_programs.cyber` | Responses | 访问策略，不是工具语义。 |
| `file_search` 及 `file_search_call` | Responses | 向量库检索。Anthropic 没有对应工具。 |
| `image_generation` 及四条流式事件 | Responses | 含 `partial_image`。特定模型的透明背景 reference 另标 preview。 |
| `web_fetch_*` 及 `web_fetch_tool_result` | Anthropic | 抓取页面，不是 web search。 |
| `memory_20250818` | Anthropic | — |
| `inference_geo` | Anthropic | 响应 usage 里也有。 |
| advisor 工具与 `advisor_tool_result` | Anthropic beta | `advisor_20260301`。 |
| `fallback`、`fallbacks`、`fallback_credit_token` | Anthropic beta | 对象形式的明确 header 是 `anthropic-beta: fallback-credit-2026-07-01`。`server-side-fallback-2026-07-01` 只在 fallback 说明里读到。 |
| 输入块 `tool_addition` / `tool_removal` | Anthropic beta | 明确要求 `anthropic-beta: inline-tools-2026-09-15`。 |
| `thinking.block_binding`、`input_transformations`、`thinking_delta.estimated_tokens` | Anthropic beta | 后两者的明确 header 分别是 `thinking-binding-controls-2026-08-01`、`thinking-token-count-2026-05-13`。 |
| `output_config.task_budget`、`speed` | Anthropic beta | `speed` 是否等于 `service_tier` 见待定，这里先不要接。 |

弃用但仍在 reference 里的字段（Chat `max_tokens`、`functions` / `function_call`、`user`、`prompt_cache_retention`；Responses `generate_summary`、`prompt_cache_retention`；Anthropic 顶层 `output_format`、以及标了 Deprecated 的 `temperature` / `top_k` / `top_p`）不新开 IR。已经有映射的维持现状。

## 待定 — 不写接通建议

角色是否相同，这次定不了。不要在 2.5.0 里硬接。

| 候选 | 为什么不定 |
|---|---|
| Responses `shell` / `local_shell` 对 Anthropic `bash_*` | 都是命令执行，参数和结果形状不一样。Q20 明确不凭「都是工具」接通。 |
| `reasoning.summary` 对 `thinking.display` | 都在管推理怎么展示。一个是请求偏好，一个是 `summarized` / `omitted` / beta `updates`。 |
| 三脸的 `service_tier` | 枚举几乎对不上。Chat / Responses 有 `flex` / `scale` / `priority` / `fast`；Anthropic 请求侧是 `auto` / `standard_only`。Anthropic beta `speed` 也不要并进来，除非单独证明它就是 tier。 |
| Responses program replay fingerprint 对 Anthropic `encrypted_code_execution_result` | 都像代码执行的回放凭证。证据不够把它们收成 Q16 那个推理槽，也不够单开一个 P1 槽。 |
| Anthropic `text_editor_*` 对 Responses `apply_patch` | 都在改文件。Apply Patch 是 create / delete / update 操作；text editor 是 view / create / str_replace。规格禁止把 Apply Patch 伪装成 function 或 custom。 |
| Responses `configuration_update` 对 Anthropic 按消息的 `output_config.effort` | 都是对话中途改生成设置。一个是输入 item，一个是 beta 消息字段。 |
| Anthropic skills 对 Responses shell / container 的 `skill_reference` | 都叫 skill。宿主分别是 Messages `container.skills` 和 shell 环境。 |
| `safety_identifier` / 旧 `user` 对 Anthropic `metadata.user_id` | 都像终端用户标识。OpenAI 写明 `user` 正在被 `safety_identifier` 和缓存键取代。不进 metadata 的 P1。 |
| `browser_toolset_20260801` 对 computer | 浏览器工具集不是计算机操作工具的一个日期版本。 |

## 附录 — 规格已经处理

这些有跨脸损失或有损保真，但是规格写明了，而且没有新的未接通角色。不要为它们再开一轮 IR。

| 项 | 规格怎么处理的 |
|---|---|
| Responses `encrypted_content` 以外的 reasoning 附加属性（`summary` 偏好等） | N-R-8 / N-R-13：effort 以外记 `unmapped-field`。`summary` 对 `thinking.display` 的角色问题在待定，不推翻这条损失。 |
| `store`、`stream`、`stream_options` | `spec/15`：OpenAI 家族回转。`store: true` 到 Anthropic 是 `unsupported-semantic`，`false` 零损失。`stream` 跨脸零损失。 |
| `defer_loading`、`eager_input_streaming` | AN ↔ Responses 的 `defer_loading: true` 无损；到 Chat 是 `degraded`。`eager_input_streaming: true` 到两张 OpenAI 脸是 `degraded`，`false` 零损失。 |
| 结构化输出名称、description、strict | 2.4.13 已接通 JSON Schema。Anthropic 没有 name 时整段格式省略并报损失，不合成名字。 |
| Apply Patch | 只在 Responses 保真。CC / Anthropic 整段 `unsupported-semantic`。无官方 diff 增量事件，不补。和 text editor 的关系在待定。 |
| Custom grammar、`async` / `caller` / `allowed_callers` | grammar 到 Anthropic 记 `unsupported-semantic`。那些 SDK 属性逐字段 loss。`namespace` 已由 N-R-15 补上，不算缺口。 |
| 工具容器 / namespace | 同源重组。到 CC / Anthropic 逐容器 `degraded`，调用名仍可用。 |
| 普通 function 的多模态 tool result、`is_error` | CC / Responses 只拼文本。Custom 结果的 text / image 已经另有规则，不要混。 |
| Custom 结果里的 `input_file`、仅 `file_id` 的图片、image `detail` | 按项 `unsupported-semantic` 或 `unmapped-field`。空数组仍保留关联身份。 |
| 图片 URL 规范化 | 只收绝对 https 和 `image/*` 的 base64 data URL。 |
| 无签名 thinking 编回 Anthropic | 仍输出 thinking，并记 `degraded`（可能被拒绝）。 |
| Stop sequence 身份 | Responses 没有 stop 参数。CC / Responses 把 `stop_sequence` 收成普通 stop，记 `unmapped-value`。 |
| 多 choice、`choices[1:]` | 非流式只转 `choices[0]`。流式额外 choice 和 chunk logprob 被规格标成 envelope，不要求再补一条 loss。 |
| Responses 非 `max_output_tokens` 的 incomplete reason、失败状态 | 收成 `other` 加 loss。完整 provider error 模型不是 IR 的形状。`steered` 见下方待核，不改这条。 |
| 工具结果与文本的重排、缺 `max_tokens` 时填 4096 | 已登记 `degraded`。 |
| 未知字段 | JSON 入口按 `spec/02` 报损丢弃。不允许借 NativeAnnotation 做通用透传。 |
| 用量已接通的细分 | `cached_tokens`、`reasoning_tokens`、Anthropic cache creation / cache read。reference 里多出来的 audio / image / text token、prediction token、`server_tool_use` 计数、`thinking_tokens`，没逐项核对是否已经落到这些字段。核对前不新开 P1。 |

## 传输差异待核

Responses WebSocket 的连接、握手、`response.create`、lane 和重连不进 oxa。已有调研认为每个 response 的服务端事件复用 Responses 流式模型（`docs/research/2026-10-25-openai-responses-websocket.md`）。

这次 reference 额外点了两处名字，但没打开独立 WebSocket 事件页，所以不标 P1：

- WebSocket 客户端命令 `response.steer`
- `incomplete_details.reason: steered`

在读到事件表之前，只能说「可能比 SSE 多一个控制结果」，不能说 oxa 缺了一块内容模型。Realtime（`/v1/realtime`）仍是另一套协议，不进这三张脸。

## 明确不做

- HTTP 转发、代理、重试、鉴权、凭据。
- 模型能力表、上下文窗口、按模型决定参数能不能用。`modelmap` 保持调用方替换表，不做内置别名。
- 脸到脸的直接转换，或为编排再加第四张脸。
- 字节级流式往返、复刻 chunk 边界。比较的是 IR 语义。
- 让 `go/sse` 理解 JSON、`[DONE]` 或 WebSocket 帧。
- WebSocket / Realtime 客户端。
- 执行工具、Apply Patch、解释 diff、编译 grammar、按 `strict` 改写 JSON Schema。
- 把未知字段塞进 native 注解冒充保真。

## 建议的落地顺序

1. **2.4.14**：只补 Responses signature 的规格句子，并加一条锁定「今天就是 loss」的向量。
2. **2.5.0 第一刀**：metadata、developer、verbosity、并行工具开关、显式缓存。都是请求字段，块联合体动得少。
3. **2.5.0 第二刀**：引用、用户文件、拒绝正文、推理回放凭证、`web_search_call` 流式。会加可选块或可选槽。
4. **2.5.0 第三刀，单独评审**：五个原生工具角色。每刀只做一个角色，先声明和同脸调用回转，再考虑跨脸。不要在一刀里把日期版本对齐。

待定表里的项没有单独评审之前不进这三刀。
