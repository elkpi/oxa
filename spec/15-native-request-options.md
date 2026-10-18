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
| `Request` | `thinking_mode` | `thinking.type`（`adaptive` / `enabled` / `disabled`） |
| `Request` | `thinking_budget` | `thinking.budget_tokens`（正整数） |
| `Request` | `thinking_display` | `thinking.display`（`summarized` / `omitted` / `updates`） |
| `Request` | `output_effort` | `output_config.effort`（`low` / `medium` / `high` / `xhigh` / `max`） |
| `Request` | `cache_control` | 顶层 `cache_control` |
| `Request` | `context_management` | 顶层 `context_management.edits` |
| `SystemBlock` | `cache_control` | `system[i].cache_control` |
| `TextBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control` |
| `ImageBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control` |
| `ToolUseBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control` |
| `ToolResultBlock`（仅限 Request） | `cache_control` | `messages[i].content[j].cache_control` |
| `ToolResultBlock.Content` 中的块 | `cache_control` | `messages[i].content[j].content[k].cache_control` |
| `Tool` | `cache_control` | `tools[i].cache_control` |

任何已知 kind 出现在上述合法宿主之外（例如在 `Message` 或 `Params` 上，或在 `ToolChoice` 上），均属于结构错误。

## 3. Anthropic 配置闭合定义 — N-NAT-3

本版对 Anthropic 请求中识别的配置及其合法参数定义如下：

### 3.1 Thinking 与 Effort
- `thinking.type` 必须为 `"adaptive"`、`"enabled"` 或 `"disabled"` 之一。未知值产生针对 leaf 的 `unmapped-value` loss，不生成 `thinking_mode` 附加项。
- `thinking.budget_tokens` 必须为正整数。
- `thinking.display` 必须为 `"summarized"`、`"omitted"` 或 `"updates"` 之一。未知值产生针对 leaf 的 `unmapped-value` loss，不生成 `thinking_display` 附加项。
- `output_config.effort` 必须为 `"low"`、`"medium"`、`"high"`、`"xhigh"`、`"max"` 之一。
- 映射关系：显式 `output_config.effort` 映射至 `Params.ReasoningEffort`；当且仅当无显式 effort 时，`enabled` 的 `budget_tokens` 依预算表近似映射至 `Params.ReasoningEffort`（`≤2048→low`，`≤8192→medium`，其余→`high`），并携带 `origin: "budget"` 投影。`adaptive` 单独存在时，`Params.ReasoningEffort` 保持为空，不合成默认 effort。

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

## 4. 跨协议遗漏报损 — N-NAT-4

当 IR 编码至不原生支持上述特性的协议（OpenAI Chat Completions、OpenAI Responses）时，编码器必须通过通用中立 helper 收集遗漏损失。Chat Completions 与 Responses 面均无 cache 断点表示，Anthropic 的 `cache_control` 断点（顶层、system、消息内容块、嵌套 tool_result 内容、tools）跨面编码时必然逐断点产生一条 `unmapped-field` 遗漏，detail 见 §3.2：

### 4.1 遍历与路径规则
- 遍历顺序固定为：`Request.native`（下标升序）→ `System` 顺序及其 native → `Messages` 深度优先（消息顺序 → 内容块顺序及其 native → 嵌套 tool_result 内容及其 native）→ `Tools` 顺序及其 native。
- Encode loss 的 `Path` 基于当前 IR 结构定位到具体的宿主属性（如 `native[0]`、`system[0].native[0]`、`messages[1].content[0].native[0]`）。
- `Field`、`Reason` 与 `detail` 采用附加项携带的 `omission` 元数据。

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
4. **投影绑定冲突**：同一请求同时包含多个 conflicting effort 投影，或投影字段损坏，返回结构错误（error）。通用 effort 与 baseline 正常相异属于编辑，NOT an error。
