# 15 — Native Request Options（规范 2.4）

Status: normative. 扩展 spec/01、spec/02、spec/10、spec/11、spec/12；IR contract 保持 `0.2.0`，兼容读取 `0.1.0`。本规范定义通用 IR 对协议特定请求配置的受限保留、跨协议中立遗漏报损及原生回转保真规则。

## 1. 结构与生命周期 — N-NAT-1

通用 IR 在请求侧宿主对象上提供可选的 `native` 属性，类型为 `NativeAnnotation[]`（空数组在 JSON 序列化中省略）。

`NativeAnnotation` 的 JSON Schema 结构定义于 `spec/schema/ir.schema.json`（$defs/nativeAnnotation）：

- `source`（string，必填）：来源协议标识符。本版已知来源为 `"anthropic"`。未知来源必须为非空字符串。
- `kind`（string，必填）：配置分类标识符。本版已知种类见 §2。未知种类必须为非空字符串。
- `value`（string，必填）：配置内容的精确 JSON 文本字符串。
- `omission`（object，可选）：该配置未被目标协议原生支持时的中立遗漏描述，由来源解码器生成：
  - `field`（string，必填）：遗漏的语义字段名称。
  - `reason`（string，必填）：取值范围限定于 `spec/02` 的四类 Reason：`unmapped-field`、`unmapped-value`、`unsupported-semantic`、`degraded`。
  - `detail`（string，可选）：人类可读的遗漏补充说明。
- `projection`（object，可选）：该配置在通用 IR 字段上的投影元数据，用于跟踪 IR 编辑：
  - `origin`（string，必填）：`"budget"`（由预算近似派生）或 `"explicit"`（由显式配置映射）。
  - `effort`（string，必填）：解码时投影到 `Params.ReasoningEffort` 的基准值。

限制规则：
1. `native` 仅属于请求侧，MUST NOT 出现在 `Response`、`Response.Content`、`Event` 或 `EventStream` 中。
2. 同一宿主对象上，相同 `(source, kind)` 的已知配置在数组中只能出现一次（singleton）；出现多次属于结构错误（structural error）。
3. 来源解码器在解析来源请求时，必须通过该协议的字段审计，仅将已知且结构合法的配置存入 `NativeAnnotation`。未知字段仍按 `spec/02` 作为 `unmapped-field` 报损并丢弃，不得通过 `native` 借道透传。

## 2. 宿主与已知种类 — N-NAT-2

本版允许携带 `native` 的请求宿主对象及其已知 `kind` 如下：

| 宿主对象 | 允许的已知 kind | 来源 wire 对应位置 |
|---|---|---|
| `Request` | `thinking_mode` | `thinking.type`（`adaptive` / `enabled` / `disabled` / `between_tools`） |
| `Request` | `thinking_budget` | `thinking.budget_tokens`（正整数） |
| `Request` | `thinking_display` | `thinking.display`（`summarized` / `omitted` / `updates`） |
| `Request` | `output_effort` | `output_config.effort`（`low` / `medium` / `high` / `xhigh` / `max`） |
| `Request` | `cache_control` | 顶层 `cache_control` |
| `Request` | `context_management` | 顶层 `context_management.edits` |
| `Request` | `top_k` | 顶层 `top_k`（非负安全整数，2.4.11） |
| `Request` | `disable_parallel_tool_use` | `tool_choice.disable_parallel_tool_use`（`true` / `false`，2.4.11） |
| `SystemBlock` | `cache_control` | `system[i].cache_control` |
| `TextBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control` |
| `ImageBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control` |
| `ToolUseBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control` |
| `ToolResultBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control` |
| `ToolResultBlock.Content` 中的块 | `cache_control` | `messages[i].content[j].content[k].cache_control` |
| `Tool` | `cache_control` | `tools[i].cache_control` |
| `Tool` | `defer_loading` | `tools[i].defer_loading`（仅限 `true`，Anthropic 及 Responses 2.4.9） |
| `Tool` | `eager_input_streaming` | `tools[i].eager_input_streaming`（`true` / `false`，Anthropic 2.4.10） |
| `Request` | `max_completion_tokens` | `max_completion_tokens`（OpenAI Chat Completions 2.4.10 拼写保真） |
| `Request` | `store` | `store`（`true` / `false`，OpenAI 2.4.10） |
| `Request` | `stream` | `stream`（`true` / `false`，OpenAI 2.4.10 传输标记） |
| `Request` | `stream_options` | `stream_options`（OpenAI 2.4.10 流式配置） |
| `DocumentBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control`（2.4.8） |
| `DocumentBlock`（仅限 Request） | `document_title` | `messages[i].content[j].title`（2.4.8） |
| `DocumentBlock`（仅限 Request） | `document_context` | `messages[i].content[j].context`（2.4.8） |
| `DocumentBlock`（仅限 Request） | `document_citations` | `messages[i].content[j].citations`（2.4.8） |
| `RedactedThinkingBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control`（2.4.11） |

任何已知 kind 出现在上述合法宿主之外（例如在 `Message` 或 `Params` 上，或在 `ToolChoice` 上），均属于结构错误。

## 3. Anthropic 配置闭合定义 — N-NAT-3

本版对 Anthropic 请求中识别的配置及其合法参数定义如下：

### 3.1 Thinking 与 Effort
- `thinking.type` 必须为 `"adaptive"`、`"enabled"`、`"disabled"` 或 `"between_tools"` 之一。未知值产生针对 leaf 的 `unmapped-value` loss，不生成 `thinking_mode` 附加项。
- `between_tools` 独立存在，不得与 `budget_tokens` 或 `display` 联用，否则作为结构错误拒绝。不映射至 `Params.ReasoningEffort`。
- `thinking.budget_tokens` 必须为正整数。
- `thinking.display` 必须为 `"summarized"`、`"omitted"` 或 `"updates"` 之一。未知值产生针对 leaf 的 `unmapped-value` loss，不生成 `thinking_display` 附加项。
- `output_config.effort` 必须为 `"low"`、`"medium"`、`"high"`、`"xhigh"`、`"max"` 之一。
- 映射关系：显式 `output_config.effort` 映射至 `Params.ReasoningEffort`；当且仅当无显式 effort 时，`enabled` 的 `budget_tokens` 依预算表近似映射至 `Params.ReasoningEffort`（`≤2048→low`，`≤8192→medium`，其余→`high`），并携带 `origin: "budget"` 投影。`adaptive` 与 `between_tools` 单独存在时，`Params.ReasoningEffort` 保持为空，不合成默认 effort。

### 3.2 Cache Control
- `type` 必须为 `"ephemeral"`。
- 可选 `ttl` 仅支持 `"5m"` 与 `"1h"`。
- 未知 `type` 或未知 `ttl` 取值：放弃该断点（不生成 `cache_control` 附加项），产生一条定位到该未知 leaf 的 `unmapped-value` loss；MUST NOT 删除未知 TTL 后采用默认五分钟 TTL。

- 跨面遗漏 `detail`：当携带 `cache_control` 附加项的 IR 编码至无 cache 断点表示的目标面（OpenAI Chat Completions、OpenAI Responses）时，该 `unmapped-field` 遗漏的 `detail` 必须为 `anthropic-native cache breakpoint; the target face has no cache_control representation`，跨实现逐字节一致。该 detail 表达的是能力缺口（capability gap，目标面不可表达），而非实现缺失；调用方可据此按意图放行该类遗漏。

### 3.3 Context Management
- `edits` 数组项的 `type` 目前仅识别：
  - `"clear_tool_uses_20250919"`：可选参数 `trigger`（`{type:"input_tokens",value:int}`）、`keep`（`{type:"tool_uses",value:int}`）、`clear_at_least`（`{type:"input_tokens",value:int}`）、`exclude_tools`（`string[]`）、`clear_tool_inputs`（`boolean`）。
  - `"clear_thinking_20251015"`：可选参数 `keep`（`"all"` 或 `{type:"thinking_turns",value:int}`）。
- 未知 edit `type`：跳过该项，产生针对该 edit 的 `unsupported-semantic` loss，不递归审计其子属性；其余合法 edits 正常保留。
- 已知 edit 中的 trigger/keep 参数枚举值无法解释：跳过该 edit 并产生定位到该参数的 `unmapped-value` loss；MUST NOT 丢弃参数后执行默认策略。若所有 edits 均被跳过，则不生成 `context_management` 附加项。

### 3.4 Defer Loading（规范 2.4.7）
- `defer_loading` 仅适用于普通或 custom 工具（function / custom 工具声明）。
- 取值规则：
  - `true`：保留为 `kind: "defer_loading"` 的 `NativeAnnotation`，其 `value` 为 `"true"`。
  - `false` 或缺失：作为 API 默认行为，不生成 `NativeAnnotation`，不产生任何损失。
  - 显式非布尔类型（如数字、字符串、对象等）：属于结构类型错误（type violation），返回结构解析错误（error）。
- 互斥约束：同一个工具声明若同时携带 `defer_loading: true` 与 `cache_control`（包括显式 `null`），属于 Anthropic API 冲突配置，解码器返回结构错误（error: `anthropic: tools[i]: cannot specify both defer_loading and cache_control`）。若直接构造或编辑的 IR 请求在同一 Tool 宿主上同时包含 `defer_loading` 与 `cache_control` 原生配置，IR 请求校验例程（如 `ValidateNativeRequest`）与 Anthropic 编码器亦必须作为冲突结构错误拒绝。
- 跨面遗漏与损失：Anthropic 与 OpenAI Responses 均原生支持 `defer_loading`，二者间相互转换无损恢复，不产生任何遗漏损失；当携带 `defer_loading` 附加项的 IR 编码至无延迟装载支持的目标面（OpenAI Chat Completions）时，由于工具定义本身已完整下发至目标上下文，仅上下文按需加载优化退化，遗漏原因记录为 `degraded`：
  - `omission.field`: `"defer_loading"`
  - `omission.reason`: `"degraded"`
  - `omission.detail`: `"deferred tool loading is a provider-side context-assembly instruction; the tool definition is unchanged and loads up front when the target has no tool search"`
- 原生回转：编码回 Anthropic 或 OpenAI Responses（自 2.4.9 起）时，若 `Tool.native` 中存在 `kind: "defer_loading"` 且其 JSON 布尔解析为 `true`（允许合法空白，来源为 `anthropic` 或 `openai`），在 wire 工具对象上输出 `"defer_loading": true`。

### 3.5 Document Metadata（规范 2.4.8）

Anthropic `document` 内容块（spec/12 N-AN-16）的文档级选项 `title`、`context`、`citations` 按 `NativeAnnotation` 保留在 `DocumentBlock.native` 上：

| kind | value 形态 | 宿主 |
|---|---|---|
| `document_title` | JSON 字符串 | 内容块（实践中仅 `DocumentBlock`） |
| `document_context` | JSON 字符串 | 内容块（实践中仅 `DocumentBlock`） |
| `document_citations` | JSON 对象，可选布尔 `enabled` | 内容块（实践中仅 `DocumentBlock`） |

- 解码保真：三项按原值无损捕获（0 decode loss）；`citations` 的 `value` 为规范化紧凑 JSON（与 §3.2 `cache_control` 的规范化一致，键序与空白不作保真承诺）。`title`/`context` 非字符串、`citations` 非对象或 `enabled` 非布尔，属于结构类型错误（error），与 §3.4 的类型违例处理一致。
- 原生回转：编码回 Anthropic 时按 `kind` 完整恢复 wire 上的 `title`/`context`/`citations`。
- 跨面遗漏：目标面原生支持文件输入时（OpenAI Responses 原生支持 base64/url/file_id 为 input_file，OpenAI Chat Completions 原生支持 base64/file_id 为 file），DocumentBlock 正常保真转换；其附属的文档级注解（document_title、document_context、document_citations）按 §4.1 逐条记录遗漏损失。仅当目标面无法表达对应来源形式（如 Chat Completions 遇到 url 形式文档）时，DocumentBlock 整块以一条 unsupported-semantic 损失报损，块内注解随整块丢弃，不再逐条报损（N-AN-9 对称原则）。

### 3.6 Eager Input Streaming（规范 2.4.10）
- `eager_input_streaming` 适用于用户定义的普通工具（`function`）或自定义工具（`custom`）；不适用于 Anthropic 服务端原生工具（`type` 非空且非 function/custom）。
- 取值规则：
  - `true`：保留为 `kind: "eager_input_streaming"` 的 `NativeAnnotation`，其 `value` 为 `"true"`，携带遗漏元数据：
    - `omission.field`: `"eager_input_streaming"`
    - `omission.reason`: `"degraded"`
    - `omission.detail`: `"target does not preserve eager tool input streaming semantics"`
  - `false`：保留为 `kind: "eager_input_streaming"` 的 `NativeAnnotation`，其 `value` 为 `"false"`，`omission` 为 `nil`。
  - 缺失：作为 API 默认行为，不生成 `NativeAnnotation`，不产生任何损失。
  - 显式非布尔类型（如 `null`、数字、字符串、对象等）：属于结构类型错误（type violation），返回结构解析错误（error）。
- 互斥约束：`eager_input_streaming` 与 `cache_control` 及 `defer_loading` 正交，同一个工具声明上可合法共存。

### 3.7 Top K（规范 2.4.11）
- `top_k` 为非负安全整数（范围 `0..9007199254740991`）。显式 `0` 正常保留为 `NativeAnnotation{Kind: "top_k", Value: "0"}`。
- 缺失不生成注解；显式 `null` 作为未设置处理；负数、非整数、布尔、超出安全整数范围属于结构类型错误（error）。
- 跨面遗漏：转至 OpenAI 目标面时记录 `unmapped-field` 损失，`omission.field: "top_k"`。
- 原生回转：编码回 Anthropic 时在顶层还原该数值，零损保真。

### 3.8 Disable Parallel Tool Use（规范 2.4.11）
- `disable_parallel_tool_use` 为布尔类型（`true` 或 `false`）。
- 取值规则：
  - 显式 `true` 或 `false` 均保留为 `NativeAnnotation{Kind: "disable_parallel_tool_use", Value: "true"|"false"}`。
  - 缺失不生成注解。显式 `null` 或非布尔属于结构类型错误（error）。
- 宿主与约束：仅允许当请求存在合法 `tool_choice`（`auto`、`any`、`tool`）时设置；当 `tool_choice` 为 `none` 或缺失时携带该字段为结构错误（error）。
- 跨面遗漏：转至 OpenAI 目标面时记录 `unmapped-field` 损失，`omission.field: "disable_parallel_tool_use"`。
- 原生回转：编码回 Anthropic 时在 `tool_choice` wire 对象上还原布尔字段。
- 跨面遗漏与损失：当编码至不支持细粒度入参流式控制的目标面（OpenAI Chat Completions / Responses）时：
  - 若 `value` 解析为 `true`，产生一条 `degraded` 损失，detail 采用上述规范文本；
  - 若 `value` 解析为 `false`，由于目标面默认即为标准缓冲流式（与 `false` 语义等价），不产生任何报损（0 loss）。
- 原生回转：编码回 Anthropic 时，若 `Tool.native` 中存在 `kind: "eager_input_streaming"`，按解析出的布尔值精确还原输出 `"eager_input_streaming": true|false`；若无注解则完全省略该字段。

### 3.7 OpenAI 请求控制选项（规范 2.4.10）
- **`max_completion_tokens`（Token 上限与拼写保真）**：
  - 语义映射：直接映射至通用 IR 的 `Params.MaxTokens`（正整数，0 解码损失）。
  - 拼写保真：若解码来源使用了 `max_completion_tokens`，在 `Request.native` 记录 `kind: "max_completion_tokens", source: "openai", value: "true"`；Chat Completions 编码器在回转时若检测到该注解，则输出 wire 字段 `"max_completion_tokens"`（否则输出默认的 `"max_tokens"`）。
  - 冲突拒绝：若来源请求同时显式包含 `max_tokens` 与 `max_completion_tokens`，解码器返回结构错误（error）。
- **`store`（输出数据留存控制）**：
  - 取值规则：布尔值 `true` 或 `false` 保留为 `kind: "store", source: "openai"` 的 `NativeAnnotation`，`value` 分别为 `"true"` 或 `"false"`。非布尔非 null 值作为结构错误（error）拒绝。
  - 同面与 OpenAI 家族回转：编码回 Chat Completions 或 Responses 时无损恢复 `"store": true|false`，0 loss。
  - 跨面转换（如 Anthropic）：若 `store: true`，产生一条 `Reason: "unsupported-semantic"` 损失（`field: "store"`，`detail: "target does not support output retention"`）；若 `store: false`，目标面天然不具备该留存机制，不产生报损（0 loss）。
- **`stream`（传输控制标记）**：
  - 取值规则：布尔值 `true` 或 `false` 保留为 `kind: "stream", source: "openai"` 的 `NativeAnnotation`。非流式解码器安全接收并记录该注解，0 解码损失。
  - 跨面转换：视为网关传输层控制标记，跨面转换时不产生报损（0 loss）。
  - 同面回转：编码回 Chat Completions 时若存在该注解，输出 wire 字段 `"stream": true|false`。
- **`stream_options`（流式输出选项）**：
  - 取值规则：必须为 JSON 对象。已知属性 `include_usage` 和 `include_obfuscation` 必须为布尔值。保留为 `kind: "stream_options", source: "openai"` 的 `NativeAnnotation`，`value` 为规范化 JSON 文本。
  - 未知属性审计：若包含未知子属性，针对该未知路径（如 `stream_options.extra`）产生精确的 `unmapped-field` 报损。
  - 跨面转换：当编码至无法输出独立的终止前 usage chunk 的目标面时，产生一条 `degraded` 损失（`field: "stream_options"`，`detail: "target does not emit the requested final usage chunk"`）。
  - 同面回转：编码回 Chat Completions 时完整恢复 wire 上的 `"stream_options"` 对象。

## 4. 跨协议遗漏报损 — N-NAT-4

当 IR 编码至不原生支持上述特性的协议（OpenAI Chat Completions、OpenAI Responses）时，编码器必须通过通用中立 helper 收集遗漏损失。Chat Completions 与 Responses 面均无 cache 断点表示，Anthropic 的 `cache_control` 断点（顶层、system、消息内容块、嵌套 tool_result 内容、tools）跨面编码时必然逐断点产生一条 `unmapped-field` 遗漏，detail 见 §3.2；工具的 `defer_loading` 原生选项跨面编码至 Chat Completions 时必然逐工具产生一条 `degraded` 遗漏（Responses 自 2.4.9 原生支持 `defer_loading`，无损回转），detail 见 §3.4；工具的 `eager_input_streaming: true` 跨面编码至 Chat Completions 或 Responses 时产生一条 `degraded` 遗漏，detail 见 §3.6（`eager_input_streaming: false` 0 损失）；请求的 `store: true` 跨面编码至 Anthropic 时产生一条 `unsupported-semantic` 遗漏，detail 见 §3.7（`store: false` 0 损失）；请求的 `stream_options` 跨面编码至不支持最终 usage chunk 的目标面时产生一条 `degraded` 遗漏，detail 见 §3.7；请求的 `stream` 传输标记在跨面编码时始终为 0 损失：

### 4.1 遍历与路径规则
- 遍历顺序固定为：`Request.native`（下标升序）→ `System` 顺序及其 native → `Messages` 深度优先（消息顺序 → 内容块顺序及其 native → 嵌套 tool_result 内容及其 native）→ `Tools` 顺序及其 native。
- Encode loss 的 `Path` 基于当前 IR 结构定位到具体的宿主属性（如 `native[0]`、`system[0].native[0]`、`messages[1].content[0].native[0]`、`tools[0].native[0]`）。
- `Field`、`Reason` 与 `detail` 采用附加项携带的 `omission` 元数据；若 `cache_control` 的 `omission` 缺省或其 `detail` 为空，跨面编码至不支持 cache 断点的目标面时统一缺省填补 §3.2 规定的规范化 `detail` 常量并报 `unmapped-field` 遗漏；若 `defer_loading` 的 `omission` 缺省或其 `detail` 为空，统一缺省填补 §3.4 规定的规范化 `detail` 常量并报 `degraded` 遗漏。

### 4.2 Thinking 组合报损去重
对于 `Request` 上的 thinking 相关配置，遗漏记账规则如下（对应设计 §6.1）：

1. **`adaptive`（无显式 effort）**：一条 `unmapped-field`，`Field: "type"`。
2. **`adaptive` + 显式 effort**：通用 effort 正常映射至目标协议；thinking 模式产生一条 `unmapped-field`，`Field: "type"`。
3. **`disabled`**：一条 `unmapped-field`，`Field: "type"`。
4. **`enabled + budget`（无显式 effort）**：通用 effort 正常映射至目标协议；产生一条 `degraded`，`Field: "budget_tokens"`（`detail: "budget approximated"`）。该条 degraded 损失覆盖了模式和预算的组合近似，MUST NOT 重复产生针对 `type` 的损失。
5. **`enabled + budget` + 显式 effort**：显式 effort 优先映射至目标协议；`type` 产生一条 `unmapped-field`，`budget_tokens` 产生一条 `unmapped-field`（预算未参与 effort 投影）。
6. **`display`**：若存在且目标协议不支持展示偏好，独立产生一条 `unmapped-field`，`Field: "display"`。

### 4.3 未知扩展处理
- 对于直接构造或经 JSON 读入的未知 `source` 或未知 `kind`，整项跳过并在编码时产生一条 `unsupported-semantic` loss（未知 source 报 `Field: "source"`，source 已知而 kind 未知报 `Field: "kind"`）。MUST NOT 将未识别的 native 附加项解析或合并至目标 wire。

## 5. 原生回转保真与 IR 编辑 — N-NAT-5

编码回 Anthropic 时，编码器负责根据 `NativeAnnotation` 恢复原生请求字段：

### 5.1 显式修改优先原则
如果调用方在 Decode 与 Encode 之间显式修改了通用 IR 字段，以修改后的通用字段为准：
- **`Params.ReasoningEffort` 被修改**：
  - 若原配置为 `origin: "budget"`（预算派生），且当前 `ReasoningEffort` 与基准值不同，则原始精确预算失效，不得重新写回；新 effort 依既有预算表（`minimal→1024, low→2048, medium→8192, high→16384`）重建并报 `degraded`；若新值为 `xhigh` 或 `max`，则输出 `thinking: {type: "adaptive"}` 及 `output_config: {effort: new_effort}`，并报 `degraded` 记录模式与预算改变。
  - 若原配置为 `origin: "explicit"`（显式原生 effort），则输出新的 `output_config.effort`。
- **`Params.ReasoningEffort` 被清空**：
  - 依赖于通用 effort 的 thinking 配置失效；若无法构成合法 thinking，则不输出 thinking 并报相应 loss。
- 无关配置（如 `cache_control`、`context_management`、`thinking_display`）不受 effort 修改的影响，继续恢复。

### 5.2 宿主删除与简写防护
- 宿主内容块被调用方从 IR 中删除时，附着于该块上的 `cache_control` 一并消失，MUST NOT 将断点漂移至相邻块。
- 编码器在输出消息内容时，若目标块带有 `cache_control`，MUST NOT 采用字符串简写（如单文本块直接输出字符串），必须输出完整的 content block 对象以保留 `cache_control`。

## 6. 直接构造的 IR 校验规则 — N-NAT-6

实现必须提供公开或内部的通用 IR 请求校验例程（如 `ValidateNativeRequest`）：
1. **结构合法性**：`NativeAnnotation` 的必需字段非空，`value` 为合法的 JSON 文本；若 `omission` 存在，其 `reason` 必须属于合法的四类 reason。不满足时返回结构错误（error）。
2. **Singleton 约束**：同一宿主下同一 `(source, kind)` 出现多次，返回结构错误（error）。
3. **宿主合法性**：已知 kind 出现在未授权宿主上，返回结构错误（error）。
   - `eager_input_streaming`：仅允许在 `Tool` 宿主上，`source` 必须为 `"anthropic"`，`value` 解析为 JSON 布尔值。
   - `max_completion_tokens`：仅允许在 `Request` 宿主上，`source` 必须为 `"openai"`。
   - `store`：仅允许在 `Request` 宿主上，`source` 必须为 `"openai"`，`value` 解析为 JSON 布尔值。
   - `stream`：仅允许在 `Request` 宿主上，`source` 必须为 `"openai"`，`value` 解析为 JSON 布尔值。
   - `stream_options`：仅允许在 `Request` 宿主上，`source` 必须为 `"openai"`，`value` 解析为 JSON 对象。
4. **投影绑定冲突**：同一请求同时包含多个 conflicting effort 投影，或投影字段损坏，返回结构错误（error）。通用 effort 与 baseline 正常相异属于编辑，NOT an error。
5. **Tool 原生选项互斥**：同一 Tool 宿主上不得同时存在 `source: "anthropic"` 的 `defer_loading` 与 `cache_control`；存在冲突时返回结构错误（error）。

## 7. 损失安全指纹工具 — N-NAT-7（规范 2.4.10）

为满足网关运维与安全审计的可观测性需求，通用 IR 模块必须提供轻量、PII 安全的损失指纹辅助类型与函数：

```
type LossFingerprint struct {
    Path   string
    Field  string
    Reason string
}
func FingerprintLosses(losses []Loss, limit int) []LossFingerprint
```

- **语义保证**：仅保留 `Path`、`Field`、`Reason`，排除往往包含未脱敏取值与上下文文本的 `Detail`。若请求中包含携带敏感信息的未知字段名，调用方在持久化前可进一步对 Path/Field 执行字段脱敏。
- **截断规则**：当 `limit <= 0` 时返回全量指纹；当 `limit > 0` 且 `len(losses) > limit` 时截断至前 `limit` 条。
- **多语言对齐**：Go、TypeScript、Python、Rust、C++ 全部 5 种语言在 IR 命名空间导出该函数。
