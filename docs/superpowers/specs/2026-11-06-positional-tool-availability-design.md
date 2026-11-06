# 位置相关的工具可用性设计 (spec 3.0.0 / IR 0.3.0)

## 意图

下游网关要能分清三件事：工具早先可用、后来被移除；请求本身畸形；目标协议表示不了这次变更。OXA 今天做不到。Anthropic 的 `tool_addition`、`tool_removal` 和 `compaction` 被拒绝或整块丢掉，Responses 的 `additional_tools` 被拆开追加进请求级 `Tools`，位置没了。官方要求 compaction 和 `tool_changes` 原样回传，Responses 也要求 `additional_tools` 留在原来的输入位置。

这次把「某一拍之后工具集变了」做成 IR 能表达的有序变更。Anthropic 和 Responses 能保住各自的原脸往返。Chat Completions 不能表示这种位置，必须显式记 loss，不能把时间线拍进请求级 `tools`。

## 目标

- 合法的 Anthropic `tool_addition` / `tool_removal` 不再被 `system content must be text` 拒绝。
- `compaction` 整块能从 Anthropic 解进来再编回去，包括 `content`、`encrypted_content`、`signature`、`cache_control` 和 `tool_changes`。
- Responses `additional_tools` 按原位置往返，不再提升进 `Request.Tools`。
- 三张脸的转换结果是确定的：能表示的添加写回原位置，不能表示的逐条记 loss。
- 畸形形状和违反局部位置的块是结构错误。历史对不上不是结构错误。

## 非目标

- 不改响应解码，不改三张脸的流式转换。
- 不建模 `clear_at` 和消息级 `output_config`。
- 不核对名字在那一拍是否存在，不核对签名，不拒绝跨位置重名。
- 不提供「计算某一拍可用工具名单」的运行时接口。名单由消费方从开头的 `Tools`、活分组和 compaction 上的变更自己算。
- 不把 Responses 自己的 compaction / `context_management` 接成这块 Anthropic compaction。
- 不新增「调用方要求保真就直接失败」的开关。
- 不重开 N-AN-21 的内容块 `tool_reference`（那个块用 `tool_name`，和这里的变更引用不是同一个对象）。
- 不看 `anthropic-beta` 头。OXA 只看见 JSON body。

## 反目标

- 把中途添加和移除写进 `Request.Tools`，让下游以为这些工具从一开始就在。
- 为了让 Anthropic 能发出去，把落在非法位置的分组挪到附近的合法边界。
- 只记一条更清楚的 loss，却仍然丢掉 compaction 或 `additional_tools` 的位置。
- 为了保真去恢复 N-R-5 已经打乱的 assistant 线序。

## 约束

- 枢纽是 IR。脸与脸之间不直接转换。
- `Request.Tools` 继续表示对话开头那一份工具。
- IR 消息角色仍只有 `user` 和 `assistant`。不新增 system 或 developer 角色，否则夹在函数调用中间的 `additional_tools` 会拆开 assistant 轮次，破坏 call / result 配对。
- assistant 轮次仍按 N-R-5 规范化成 thinking、文本、工具调用。分组的位置跟规范化之后的内容走。
- 工具定义复用现有 `Tool` 解码和编码。`input_schema` 等 JSON 仍按 INV-1 原样携带。
- `cache_control` 继续用 `NativeAnnotation`。
- 规范、共享向量、Go、TypeScript、Python、Rust、C++ 同一次交付。默认分支不能停在「只有规范、实现还对不上」的状态。
- 这改了 2.x 已经生效的 N-R-15 提升行为和空项 loss，不能放进 2.6.0。

## 方案

考虑过三种做法。

1. **只记 loss，不进 IR。** 合法请求不再被结构错误拒绝，但 compaction 不能回传，网关也分不清「后来被移除」和「这张脸表示不了」。不采用。
2. **原脸注解，或在每条消息上存一份工具名单。** 注解不够让每张脸读懂变更。名单会和必须原样回传的 `tool_changes` 打架，`mcp_toolset_reference` 也算不出一份干净名单。不采用。
3. **有序分组加独立的 compaction 块。** 活变更按对话位置排列。compaction 整块留下，它的 `tool_changes` 只挂在块上。这是选定的方案。

issue 里那个 `Message.ToolAvailability *ToolAvailabilityEvent` 不采用。一个指针装不下一条系统消息里的多项变更，也装不下夹在两个工具调用之间的 `additional_tools`。

## 已确认的行为

### 活分组

请求新增有序的 `tool_change_groups`。它不修改 `Request.Tools`。

每个分组有：

- `message_index`：`0` 到 `len(messages)`。
- `content_index`：`-1` 表示这条消息之前。`message_index == len(messages)` 且 `content_index == -1` 表示整段对话之后。大于等于 `0` 表示插在该消息规范化内容的这个下标之前，等于内容长度表示最后一块之后。
- 可选 `id`。来自 `additional_tools`。缺省就不写出。
- 按原顺序排列的 parts。part 是文本，或一条变更。
- `extra`：这个对象上文档没有写、OXA 也不单独建模的属性。

同一位置的多个分组靠列表顺序区分，不另做序号。

一条变更是：

- `op`：`add` 或 `remove`。
- 工具定义，或一个引用。二者互斥。`remove` 只能是引用。
- 块级 `cache_control` 放在这条变更的 `native` 上。定义内部自己的 `cache_control` 仍留在 `Tool` 上。两处都保留。
- 变更对象自己的 `extra`。

引用是封闭的三种：

- `tool_reference`：非空 `name`。不接受 `tool_name` 作为别名。
- `mcp_tool_reference`：非空 `name` 和 `server_name`。
- `mcp_toolset_reference`：非空 `server_name`。

定义走该脸现有的工具解码器，结果是普通 `Tool`。Responses 的 `namespace` / `toolbox` 仍按 N-R-15 展平，container 注解留在这些 `Tool` 上。编回时用现有的容器重组，但重组结果放进原来的那个 `additional_tools.tools`，不进顶层 `tools`。

### 分组落在哪里

Responses 解码时，空的和非空的 `additional_tools` 都不结束当前轮次，也不把工具追加进 `Request.Tools`。

- 项出现在一条尚未结束的 assistant 或 tool-result 轮次内部：轮次照常合并。分组的 `content_index` 取「这项之后、仍属于同一轮次的下一个线项」所产生的第一块规范化内容的下标。如果这项后面没有再产生块，`content_index` 等于该消息内容长度。因此 call、`additional_tools`、call 会落在两个工具调用之间。reasoning、`additional_tools`、function call 会落在规范化后的那个 function call 之前。夹在会被 N-R-5 重新排序的文本和调用之间时，不恢复旧线序。
- 项出现在两条消息之间、输入开头或输入末尾：`content_index` 为 `-1`，`message_index` 指向下一条消息；没有下一条消息时用 `len(messages)`。

一项 `additional_tools` 一个分组。`role` 缺省或不是 `developer` 是结构错误。`tools` 缺省或不是数组是结构错误。`tools: []` 是零 part 的空分组，保留 `id` 和 `extra`，不再记 `unmapped-field`。

同一份 `tools` 数组或同一个容器内部的重名仍是结构错误。这份数组和 `Request.Tools` 之间同名，或两个分组之间同名，不拒绝。实现不能再用一个贯穿整份请求的 `seenNames` 来拒绝 `additional_tools`。

### Anthropic 系统消息

纯文本的会话中途 system 消息不变，仍按 N-AN-15 折进 `Request.System`，并记现有的那条 `degraded`。

只要内容里有 `tool_addition` 或 `tool_removal`，整段内容按原顺序进入一个分组，文本不折进 `Request.System`。其他块类型仍是结构错误。已有的位置规则继续有效：不能出现在 `messages[0]`，必须跟在 user 轮次之后，后面必须是 assistant 或位于末尾。连续的这类 system 消息各成一个分组，共享同一个边界光标，列表顺序就是线序。

光标：后面还有 assistant 时，指向那条 assistant 的 IR 下标，`content_index` 为 `-1`。system 消息位于末尾时，`message_index` 为 IR 消息条数，`content_index` 为 `-1`。

同条消息上的 `clear_at` 和 `output_config` 各记一条 `unmapped-field`，不保存，不写回，也不拒绝其中的工具变更。

### compaction

新增内容块 `compaction`。合法位置只有 `messages[0].content[0]`，并且不能出现在 system 消息里。别处出现是结构错误。因此一份请求最多一块。第二块无论 `content` 是否为 `null`，都会被位置规则拒绝。

- `content` 缺省和 JSON `null` 是两种状态。`null` 表示压缩失败，必须保留。空字符串是结构错误。
- `encrypted_content` 和 `signature` 保留「键是否出现」。出现且为空字符串也要写回。
- `cache_control` 用 `NativeAnnotation`。
- `tool_changes` 用同一种变更对象，按原顺序留在块上。不复制进 `tool_change_groups`，不追加进 `Request.Tools`。
- 块上的未知属性进入 `extra`，编回 Anthropic 时写回。

直接构造的 IR 如果把 compaction 放在别的位置，编码时同样是结构错误，不发出去。

### 结构错误和不是结构错误的事

结构错误：缺字段、类型不对、空的必填名字、`tool_removal` 带了定义、`compaction.content` 是空字符串、compaction 位置不对、system 消息位置不对、`additional_tools.role` 不是 `developer`、同一份声明内部重名、分组光标越界。

不是结构错误：名字在那一拍不存在、跨位置重名、签名对不上、beta 头缺席。

错误文案由实现写成稳定英文，必须带上路径和违反的规则，并有测试锁住。不在本设计里规定每一个字符串。

### 编码

**Anthropic。** 光标是合法 system 边界时，一个分组写成一条 `role: "system"` 消息，parts 按原顺序成为内容块。合法边界是：`content_index == -1`，不是对话开头，前一条 IR 消息是 user，并且后面是 assistant 或位于末尾。连续分组共享这个边界时，按列表顺序写成连续的 system 消息。

光标在消息内部，或落在上述边界之外：不挪位置。每个 part 各记一条 `unsupported-semantic`，这组不写出。

分组被写出时，若带有 `id`，记一条 `unmapped-field`，不写到 Anthropic。被整组丢掉时，不为 `id` 另记 loss。

**Responses。** 分组按光标写回 `additional_tools`，`role` 固定为 `developer`。

- 零 part 的空分组写成 `tools: []`，并写回 `id`。
- 有 part 时，用现有 Responses 工具编码器渲染其中的添加定义，结果按相对顺序放进该项的 `tools`。这包括函数、custom、带 container 注解的工具，以及 `source: "openai"` 的原生工具。不把它们提升到顶层 `tools`。
- 移除、三种引用、文本 part，以及现有编码器不能渲染的定义，各记一条 `unsupported-semantic`，不进入该项。
- 有 part 但一个都渲染不出来时，不写出空的 `additional_tools`。只保留那些 part loss。
- 能写出项时，`id` 和 `extra` 写回。

**Chat Completions。** 不改 `tools`。每一条变更 part、每一段文本 part 各记一条 `unsupported-semantic`。每一个空分组另记一条。每一块 compaction 另记一条。compaction 里的 `tool_changes` 不另记，也不写成别的工具。没有新的失败开关。

**跨脸的通用规则。** 能转换的对象仍然转换。它上面的未知属性各记一条 `unmapped-field`，不写到对面。整条被丢掉的对象，内部属性不再另记 loss，这是现有 spec/02 规则。OpenAI 原生工具编到其他脸时沿用 spec/16，省略并记 loss。容器编到没有容器的脸时，沿用现有 `degraded`，展平后的定义如果目标脸能作为工具变更写出，就写在那个分组位置上，不写进请求级 `Tools`。

### 损失目录

这些 detail 是稳定英文，测试必须逐字匹配。

| 场景 | path | field | reason | detail |
|---|---|---|---|---|
| 分组里的文本编到 Responses 或 Chat，或因 Anthropic 位置非法而被丢 | `tool_change_groups[i].parts[j]` | `type` | `unsupported-semantic` | `positional system text has no equivalent on the target face` |
| 一条变更不能在目标脸表示 | `tool_change_groups[i].parts[j]` | `op` | `unsupported-semantic` | `positional tool availability change cannot be represented on the target face` |
| 空分组编到 Anthropic 或 Chat | `tool_change_groups[i]` | `tools` | `unsupported-semantic` | `empty positional tool group cannot be represented on the target face` |
| compaction 编到 Responses 或 Chat | `messages[i].content[j]` | `type` | `unsupported-semantic` | `Anthropic compaction block cannot be represented on the target face` |
| 已写出的分组带有 `id`，目标脸没有这项 | `tool_change_groups[i].id` | `id` | `unmapped-field` | `additional_tools item id has no equivalent on the target face` |
| system 消息上的 `clear_at` | `messages[i].clear_at` | `clear_at` | `unmapped-field` | `Anthropic system message clear_at has no IR equivalent` |
| system 消息上的 `output_config` | `messages[i].output_config` | `output_config` | `unmapped-field` | `Anthropic per-message output_config has no IR equivalent` |

纯文本 system 消息的现有 `degraded` detail 不改。未知属性的 `unmapped-field` 沿用该脸现有文案风格，每个属性一条。

### 版本

- 规范版本 **3.0.0**。
- IR 文档 `specVersion` **0.3.0**。请求、响应、事件流文档都改，尽管这次只给请求加字段、给内容块加一种变体。
- 五门语言的实现版本 **v3.0.0**。
- 不保留旧的 `additional_tools` 提升行为，不加兼容开关。
- 新增的密封联合变体要在 spec/01 写明消费方穷尽匹配的迁移要求。

## 留给实现的事

- 稳定的结构错误英文文案，只要带路径和规则并有测试锁住。
- 各语言里版本常量和向量条数断言的具体改法。
- 提交可以按「规范 → 向量 → 各语言实现」拆开，但要在同一分支上一并合并，避免默认分支 CI 先红。

## 验收

1. issue 里的三个 Anthropic 最小例子：添加、移除不再是结构错误；compaction 整块往返，`tool_changes` 不变。
2. `additional_tools` 在两个函数调用之间往返后仍在那个位置，顶层 `tools` 不增加这些工具。
3. 空的 `additional_tools` 往返后仍在，不再产生 `unmapped-field`。
4. 混合 system 消息的文本和变更按原顺序往返，文本不进 `Request.System`。纯文本 system 消息仍是现在的折叠加 `degraded`。
5. 非法 Anthropic 位置不挪。Chat 不改 `tools`。不能表示的变更逐条出现上表里的 loss。
6. 未知属性在原脸往返中还在，不漏到另一张脸。
7. `spec/11` 的 N-R-15 和 `spec/12` 的 N-AN-15 改成上面的规则，不留下互相矛盾的旧句子。
8. 五门语言的 CI 和 `veccheck -check-manifest` 通过。
