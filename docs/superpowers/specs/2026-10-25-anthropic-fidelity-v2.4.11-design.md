# v2.4.11 — Anthropic 思考历史、请求控制与文本引文保真设计

日期：2026-10-25

状态：设计草案，待用户确认；本文件不授权实现、提交、推送或发布。

基线：`ed1824d`（v2.4.10 后的 main）。

## 1. 目标与范围

本版解决 Anthropic Messages 同面转换丢失协议状态的问题，并在 Go、TypeScript、Python、Rust、C++ 五种实现中提供一致行为。用户确认纳入的范围为：

1. `redacted_thinking`：请求 assistant 历史、响应、流式及 canonical IR JSON。
2. `thinking.type = "between_tools"`：保留独立原生模式。
3. `top_k`：保留旧版 Anthropic wire 的显式采样配置。
4. `tool_choice.disable_parallel_tool_use`：保留显式 `true` 与 `false`。
5. 文本块 `citations`：响应、assistant 历史及 `citations_delta` 流式完整支持。

核心验收标准：来源面能够表达的信息先进入 IR；同面编码恢复；真正编码到无法表达的目标面时才报损。不得在 Anthropic 解码阶段预先删除 redacted payload 或已支持的引文。

不扩展 HTTP、认证、重试、模型能力表、API-version 路由，也不执行服务器工具。不以“支持 Anthropic 所有字段”为无限扩张范围；`strict`、新的服务器工具结果块、compaction 等其他能力不包含在这五项交付内。

## 2. 协议依据与重要修正

### 2.1 已核实的事实

- 官方固定 SDK 的 `RedactedThinkingBlock` / Param 只有 `type: "redacted_thinking"` 和必填字符串 `data`，与普通 `thinking` 块是不同变体。
- `ThinkingConfigBetweenTools` 只有 `type: "between_tools"`，不能借机合成 `display` 或 `budget_tokens`。
- `ToolChoiceAuto`、`ToolChoiceAny`、`ToolChoiceTool` 支持可选布尔 `disable_parallel_tool_use`；`ToolChoiceNone` 没有此属性。
- `TextBlock.citations` 是引文数组或 `null`；请求 `TextBlockParam.citations` 是可选数组或 `null`。两者均支持五种引文位置变体。
- 文档位置类响应引文有 `file_id`，对应 request Param 未声明它。响应对象和请求历史对象不是完全相同的 Schema。
- 官方 API version `2026-10-21` 已移除 `temperature` / `top_p` / `top_k`，发送任一字段均会 400；`2023-06-01` 属于仍须处理的旧协议。新版还拒绝 user 文本中的非空 `citations`，但仍允许 assistant 历史引文。

因此，上一轮“citations 只在请求 Native 保留，响应报损”的设想不采用：那会在生成响应时先丢信息，使历史回放无从恢复。`top_k` 也不应描述为所有当前 Anthropic API version 都支持的字段。

### 2.2 来源

- [固定 SDK：Messages 类型](https://github.com/anthropics/anthropic-sdk-typescript/blob/cb5fba419808e3844fa6407e59577ab7ba346b79/src/resources/messages/messages.ts)：引文类型 L2761–3012，redacted L5087–5113，TextBlock L5285 起，thinking 配置 L5494–5557，tool_choice L5690–5742。
- [Thinking 与回放规则](https://platform.claude.com/docs/en/build-with-claude/thinking)。
- [Thinking 工具工作流](https://platform.claude.com/docs/en/build-with-claude/thinking-tool-workflows)。
- [API version 2026-10-21 迁移指南](https://platform.claude.com/docs/en/api/migration-2026-10-21)。
- 仓库约束：[IR](../../../spec/01-intermediate-representation.md)、[Loss](../../../spec/02-loss-policy.md)、[Anthropic 映射](../../../spec/12-mapping-anthropic-messages.md)、[Native](../../../spec/15-native-request-options.md)、[Streaming](../../../spec/20-streaming-semantics.md)。

OXA 不知道调用方最终使用哪个 HTTP version 或模型，不会据模型名称删除采样字段、重写模式或强制切换 API version。新版服务器是否接受旧字段由调用方与上游处理。

## 3. 方案取舍

推荐“最小显式扩展”：redacted 新增独立 Block；两个请求控制继续使用 request Native；引文增加 TextBlock 专用字段和增量类型。

不采用以下两条路线：

- **全部塞入 Native**：需要把目前 request-only Native 扩展到响应和所有事件，影响范围远大于本次需求，也会把输出引文与请求选项混为一谈。
- **全部升级为通用参数或通用引用位置**：OpenAI 的并行控制、文本 annotations 与 Anthropic 的选项/索引并不完全等价；本版没有必要建立未经证明的公共语义映射。

本方案保持 hub-and-spoke：各 face 只依赖 IR；OpenAI 编码器通过 IR 统一 helper 处理引文能力缺口，不导入 Anthropic face。

## 4. IR 扩展

以下 Go 形状用于说明跨语言契约，不是本轮实现代码：

```go
type RedactedThinkingBlock struct {
    Data   string
    Native []NativeAnnotation
}

type TextCitation struct {
    Source string
    Value  string
}

type TextBlock struct {
    Text      string
    Citations []TextCitation
    Native    []NativeAnnotation
}

type CitationDelta struct {
    Citation TextCitation
}
```

### 4.1 RedactedThinkingBlock

- Block discriminator 为 `redacted_thinking`；canonical JSON 为 `{"type":"redacted_thinking","data":"OPAQUE_TEST_DATA"}`。
- `data` 必填且必须为字符串；缺失、`null`、数字/对象均为结构错误。暂不创造非空或 base64 格式限制；空字符串仍是显式字符串。
- 保留字符串值的 UTF-8 内容、顺序和重复次数，不解密、不做 base64 验证、不 trim、不作 Unicode normalization。不承诺外围 JSON 的空格、键序和转义拼写相同。
- 请求中仅用于 assistant 的直接历史内容；响应及流式 block start 中可用。user 内容或 tool_result 的嵌套内容不允许该变体。此新变体的角色规则不顺带改变已有普通 thinking 的校验行为。
- `Native` 沿用现有 request-only 规则；不因新变体开放响应 Native，也不新增 redacted 的 cache_control 能力。

### 4.2 TextCitation

canonical JSON 采用来源标记加 JSON 文本，沿用仓库 `NativeAnnotation.Value` / 原生工具 Definition 的承载模式：

```json
{
  "type": "text",
  "text": "有来源的回答。",
  "citations": [{
    "source": "anthropic",
    "value": "{\"type\":\"page_location\",\"cited_text\":\"测试引文\",\"document_index\":0,\"document_title\":null,\"start_page_number\":1,\"end_page_number\":2,\"file_id\":null}"
  }]
}
```

- 引文属于输出文本的语义数据，不属于 request Native；`TextBlock.Citations` 同时允许出现在 Response、assistant 历史和 EventStream 的 TextBlock 中。
- 不把 Anthropic wire struct 暴露为 IR 类型；`Value` 是已审计的位置对象 JSON 文本，不是可任意透传未知字段的容器。
- 本版 `Source` 只接受 `anthropic`。`Value` 必须是已支持引文变体的合法对象；直接构造 IR 或读取 canonical IR 时同样校验，不能借 IR 绕过来源审计。
- 数组顺序和重复项保留；不合并相邻文本块，不把 `cited_text` 拼入正文，不根据字符串长度重新计算来源索引。
- `citations` 缺失、`null`、`[]` 统一为“没有引文”，canonical IR 省略空数组；这属于无语义差异的空值规范化，不承诺原始 JSON 拼写回转。
- 对象内部键序/空白不作 byte fidelity 承诺；所有字符串值，包括 `encrypted_index`，必须原样保留。`encrypted_index` 也是不透明状态，不做解密或诊断输出。
- 本版不允许 user 文本或 tool_result 内文本携带非空引文，直接构造或编码此类请求返回定位明确的结构错误；assistant 历史才是本次请求侧引文宿主。

### 4.3 CitationDelta 与流语法

新增 IR delta discriminator `citation_delta`，形状为 `{"type":"citation_delta","citation":{"source":"anthropic","value":"..."}}`。它只允许出现在打开的 TextBlock 中，可与 TextDelta 按实际到达顺序交错。

现有六类 Event 不变。block/delta 合法组合新增：

- TextBlock：TextDelta 或 CitationDelta。
- RedactedThinkingBlock：零个 delta，只允许 start / stop。
- ThinkingBlock：仍沿用现有 ThinkingDelta / SignatureDelta，不改变规则。

聚合器按顺序累加 TextDelta 的文本和 CitationDelta 的引文；若 start 已携带引文，则先放入列表，再追加后续引文。不能把将来才收到的引文提前塞回 start，也不能既放入 start 又额外制造同一 delta。

## 5. Anthropic 映射

### 5.1 Redacted 请求、响应和流式

- `BlockWire` 增加 `Data`；JSON 入口额外记录属性存在性，以区分缺失和显式空字符串。marshal 使用 discriminator 专属字段，确保空 `data` 也被输出。
- request / response decode 与 encode 均 1:1 映射。普通 thinking 的 `thinking` 与 `signature` 保持现有行为。
- 原生 `content_block_start` 携带完整 `redacted_thinking.data`，映射一个 IR start；stop 映射 IR stop，不产生 thinking/text/signature delta。
- 对该 block 收到任何 delta 都是结构错误；重复 stop、错误 index、缺失 stop、非法终止按现有生命周期规则报错。
- 删除“redacted 是未知块”的旧测试预期，另选明显虚构的 discriminator 验证未知块吸收规则仍在。

### 5.2 between_tools

- `NativeKindThinkingMode` 的 Anthropic 枚举增加 `between_tools`，仍仅允许 Request 宿主。
- 解码不推导 ReasoningEffort / budget / display；显式的 `output_config.effort` 仍按原有规则独立映射。
- 编码时保留该模式，即使调用方编辑了公共 ReasoningEffort，也只更新独立的 output_config.effort，不擅自改成 adaptive/enabled。
- `between_tools` 与显式 `thinking.budget_tokens` 或 `thinking.display` 组合属于此判别联合体的结构冲突。raw、typed 和 canonical IR 校验均拒绝；raw 入口按属性存在性判断，不能因值为 `0` 或 `null` 就掩盖冲突。
- 不做 Sonnet 型号白名单或 effort/model 组合校验；不顺带收紧现有 enabled budget 最小值或预算与 max_tokens 的关系。

### 5.3 top_k

- Anthropic Request 增加可选整数表示，IR 使用 Request 上的 `NativeKindTopK`，`Value` 为 JSON 整数。
- 设计选择：支持非负、可由五语言精确表示的安全整数 `0..9007199254740991`，显式 `0` 不视为缺失；不替用户合成默认值。不新增公共 Params.TopK。
- 缺失不生成注解；显式 `null` 作为未设置处理；负数、非整数、布尔、字符串、超出安全整数范围为结构错误。数值限制是本版跨语言转换契约，不宣称是所有历史模型的完整服务器校验规则。
- Anthropic 回转恢复显式数值，零 decode / encode loss。此支持用于旧版 wire 保真，不代表 API version 2026-10-21 接受它。

### 5.4 disable_parallel_tool_use

- wire 使用 presence-aware 布尔表示，区分缺失、`false`、`true`；IR 使用 Request 上的 `NativeKindDisableParallelToolUse`。Go 为避免在 patch 版直接破坏现有 bool 字面量，保留 `DisableParallelToolUse bool`，新增 `HasDisableParallelToolUse bool`（不进入 JSON）；true 值或 Has 标志均表示字段存在。JSON decode 自动设置 Has，typed 调用方用 Has 表达显式 false，其余语言沿用各自 optional 表示。
- 注解 `Value` 为 JSON 布尔；显式 false 也保留。显式 null 或其他类型为结构错误。
- 仅与已支持的 tool_choice auto / any / tool 联用；none 携带该字段为结构错误。
- 直接构造 IR 时若只有此注解而没有 ToolChoice，或随后把 ToolChoice 改成 none，编码报结构错误，不制造默认 tool_choice，不悄悄丢 flag。
- 不自动反转成 OpenAI parallel_tool_calls；本版保真优先于扩大公共映射。

### 5.5 五类 citations 与结构审计

wire 层按 block discriminator 区分：document 的 `citations` 是配置对象，text 的 `citations` 是数组或 null，不共用一种校验规则。

支持全部五种 `type`：

| type | 主要字段 |
|---|---|
| `char_location` | cited_text、document_index、document_title、start_char_index、end_char_index；响应可带 file_id |
| `page_location` | cited_text、document_index、document_title、start_page_number、end_page_number；响应可带 file_id |
| `content_block_location` | cited_text、document_index、document_title、start_block_index、end_block_index；响应可带 file_id |
| `web_search_result_location` | cited_text、encrypted_index、title、url |
| `search_result_location` | cited_text、search_result_index、source、title、start_block_index、end_block_index |

- 字符串/可空字符串字段按固定 SDK 区分；位置/索引是非负整数，页码至少为 1，区间 end 大于 start。OXA 不查找原文验证范围或 cited_text，不把它们转换成回答正文的字符偏移。
- wire 兼容选择：响应 `file_id` 允许缺失；出现时必须是字符串或 null。其余必填字段按对应 SDK 类型校验。此放宽用于兼容早期未返回 file_id 的响应。
- unknown citation type：跳过该项，产生定位到该项 type 的 unsupported-semantic loss，不再审计其内部字段；合法兄弟项继续保留。
- 已知 type 的未知属性：按现有 raw audit 产生 unmapped-field 并删去该属性；已知字段类型错误或缺少必填字段返回 error。未知字段不能通过 Value 逃逸到下游。
- **wire 与 IR 的错误边界不同**：request / response JSON、typed wire 和原生 streaming 入口执行上述审计，可过滤未知字段/变体并返回 losses，且遵循原始字段/数组遇到顺序。canonical IR codec、直接构造 IR 的校验与目标 encoder 的前置校验则执行闭合契约：未知 Source/type、已知 type 的未知属性、缺少必填字段或错误类型一律返回结构错误，不过滤、不产生部分结果。现有 IR marshal/unmarshal API 不增加 loss 返回通道。两侧对已知且合法的字段使用同一套类型规则，不能把 wire 审计的过滤行为误套到 IR API。
- Response → IR → Response 保留响应 file_id；Request → IR → Request 按 Param 形状保留请求引用。
- Response IR 被复用为 assistant 请求历史时，位置类引文中的 file_id 是 response-only 元数据：request encode 去除此字段，非 null 值产生一条 file_id 的 unmapped-field loss；null 无实际信息，省略不报损。不得把整个引文删掉，也不得把 file_id 未经声明地透传到请求。
- `citations_delta` 每次携带一个完整 citation，映射到一个 CitationDelta；初始 text start 中的 citations 与后续 delta 分别处理，保证零重复、零重排。document citations 配置不受本扩展影响。

## 6. OpenAI 目标面的显式损失

OpenAI 两个 face 都不应把 Anthropic 加密回放状态转换成可见文本、reasoning_content、summary 或工具参数。现有 OpenAI annotations 即便能表示部分 URL，也不足以无损携带 Anthropic 文档/搜索索引和 encrypted_index；本版不尝试近似映射。

| IR 数据 | 目标行为 | loss 单位 |
|---|---|---|
| RedactedThinkingBlock | 整块省略，保留其他块 | 每块恰好一条 unsupported-semantic，field 为 type |
| between_tools | 不输出 Anthropic mode | 复用 Native omission，每注解一条 unmapped-field |
| top_k | 不输出 | 每注解一条 unmapped-field |
| disable_parallel_tool_use | 不输出，不改 OpenAI parallel_tool_calls | 每注解一条 unmapped-field，包括显式 false |
| TextBlock.Citations | 保留正文，省略整个非空引文列表 | 每个 TextBlock 恰好一条 unsupported-semantic，field 为 citations |

固定 redacted detail：`anthropic encrypted thinking replay state; the target face cannot preserve this block`。
固定 citations detail：`anthropic text citations; the target face cannot preserve source locations and replay metadata`。

非流式路径：请求 `messages[i].content[j]` / `.citations`；响应 `content[j]` / `.citations`。Native 遗漏保持现有 native 数组路径约定。

流式路径：新增逻辑记录传入 IR 事件的零基 encounter ordinal `e`，不把 block index 冒充 event 数组下标。redacted 的 loss 定位 `events[e].block`。引文在第一次实际出现时记录一次：start 上为 `events[e].block.citations`，delta 上为 `events[e].delta.citation`；后续同块引文增量吸收，不重复报损。保持当前文本通道正常输出，不把引文丢失变成整个 TextBlock 丢失。

被省略的 redacted start/stop 仍须经过语法和 index 校验；编码器维护独立 IR index 与 native output index，不能因省略块造成 native index 空洞。不得接受 redacted 上的非法 delta，也不得把损失注入事件流。

目标编码不得修改源 IR 或 Citation.Value；同一个 IR 先编码 OpenAI 后，再编码 Anthropic，仍能恢复来源状态。

## 7. 安全与诊断边界

- 仅使用 `OPAQUE_TEST_DATA` / `TEST_ENCRYPTED_INDEX` 等人工占位符，不引入真实签名、加密数据或用户文档。
- 错误和 loss detail 只包含字段路径、期望类型与 discriminator，不包含 data、signature、encrypted_index 或整个输入对象。
- 新 redacted 类型提供语言适用的安全调试表示：Go 的常见 fmt 格式、Rust Debug、Python repr 不回显 Data；TypeScript/C++ 不新增泄露载荷的日志或调试 helper。测试错误优先输出路径、长度与相等性而非对象转储。
- 正常 wire/canonical JSON 序列化必须包含真实 data，否则无法回放；序列化是显式数据出口，不等同诊断日志。库无法阻止调用方自行打印 JSON 或直接读取公开字段；文档明确调用方应过滤这些载荷。
- 不增加日志系统、加密服务、自动脱敏中间件或付费 API 集成测试。

## 8. 兼容性与版本

目标 release 为 `v2.4.11`，按仓库现行 evolution ladder 作为小能力/缺陷补充；IR contract 继续为 `0.2.0`，不改已有 discriminator 或字段含义。

这不意味着旧消费者能读取新变体：Rust enum 穷尽匹配、TypeScript discriminated union、Go type switch、Python/C++ variant 分派须新增 redacted 与 citation delta 分支；Go 无键 TextBlock 字面量和 Rust/C++ 显式初始化可能需要补字段。发布说明明确迁移影响，不宣称零源码兼容成本。

基线 389 个向量必须全部重新运行。除本次明确升级为“已支持”的行为外，其余预期不变；例如已有 `loss-disable-parallel-tool-use-to-ir` 应改为保留 Native 的成功向量并重新命名，而不是同时要求旧的报损预期继续成立。其他引用 redacted/citations/top_k 为未知语义或字段的旧用例亦按新契约有依据地更新。无引文文本、thinking/signature、adaptive/enabled/disabled/budget、document citations、其他 Native 和未知字段审计不得回归。新增字段仅在输入具备对应信息时出现。

request-only Native 的原始限制保持不变；响应和事件不得携带 Native。新的 citations 字段不是通过放宽 Native 限制实现的。

## 9. 工程落点

实现顺序必须是规范/Schema → golden vectors → 实现；规范与实现、各独立模块分别保持可 cherry-pick 的 commit 边界。此设计阶段不创建 commit。

- 规范：spec/01、02、10、11、12、15、20，以及 spec/README、CHANGELOG 和 IR/vector Schema；新增引文/块/增量定义及响应块联合体、角色和 native 宿主规则。
- Go：go/ir 的 block/event/json/native；Anthropic types/rawjson/request_native/decode/encode/streamin/streamout；两个 OpenAI face 的非流式与流式编码；go/internal/vectest 的事件归一化与比较。
- TypeScript：ts/src/ir、anthropic/messages、两个 OpenAI 编码器、vectest；通过已有 generate 脚本刷新 generated Schema types。
- Python：python/src/oxa/ir、anthropic/messages、OpenAI 编码器及 vectest；保持 dataclass/联合体与 checker 一致。
- Rust：rust/crates/oxa-ir、oxa-anthropic、oxa-chatcompletions、oxa-responses、oxa-vectest；更新 enum 的 exhaustive matches。
- C++：cpp/include/oxa/ir.hpp、anthropic/messages.hpp；cpp/src/ir.cpp、json.cpp、native.cpp、各 face、审计头和 vectest。
- 公共常量：更新 scripts/check-constants.py 所校验的跨语言集合；所有原有测试和 schema 生成检查继续生效。不引入 Anthropic SDK 运行时依赖。

## 10. 验收矩阵

所有有效行为由共享向量固定；结构错误和调试安全可由语言级单测固定。每项都覆盖 typed 与 JSON 入口，避免只有 raw 路径支持。

| 组 | 必须验证的用例 |
|---|---|
| Redacted 非流式 | 请求/响应零损回转；空 data；多语言与转义字符；thinking、redacted、redacted、tool_use 顺序及重复保真；原 thinking/signature 不变 |
| Redacted 错误 | 缺失/null/非字符串 data；非法角色/嵌套位置；canonical IR 验证；不含载荷的错误 |
| Redacted 流式 | 原子 start/stop；混合前后文本和工具块；index 0；错误 delta/index/缺失 stop；OpenAI 目标每块一损且继续后续输出 |
| Native modes | adaptive/enabled/disabled 旧例不变；between_tools 独立零损；显式 effort 独立保留；budget/display 冲突；错误宿主/重复注解；IR 编辑不改模式 |
| top_k | 缺失/null/0/正数/安全上界；负数/小数/布尔/越界；同面恢复；跨面报损；不按 model 做策略 |
| 并行控制 | 三种合法 tool_choice × true/false；缺失；null/错误类型；none 冲突；IR 移除 choice 后报错；跨面无暗中反转 |
| 引文非流式 | 五类 × request/response；nullable title、file_id；多条/重复顺序；空值；已知未知字段审计；未知 type；缺失字段/错误索引类型；response→history file_id 有损处明确 |
| 引文流式 | start 带引文；text/citation delta 交错；聚合后等价非流式；同面零损；OpenAI 每块一次 citation 损失；错误 block/delta 组合 |
| IR/不变性 | 新块/字段/delta 的 marshal/unmarshal；未知引文 Source/type/属性在 IR 入口报 error，而相同 wire 未知字段走 loss；Response 不带 Native；编码 OpenAI 不修改 IR，随后编码 Anthropic 仍保真 |
| 审计/安全 | 原始遇到顺序；被整块省略后无重复 loss；不把 data/cited_text 加到正文；诊断不暴露 opaque 值；无新运行时依赖 |

新增向量放在现有 vectors/anthropic/nonstream、vectors/anthropic/stream 和 vectors/cross 布局下，name 与路径相符；只在规范/向量更新阶段重新生成 vectors/manifest.json，不在设计阶段修改它。实际数量以实现后 veccheck 为准，不提前承诺未经验证的向量总数。

验证命令沿用现有 CI：

- Go：`go -C go test -count=1 -race ./...`、`go -C go vet ./...`、`make vectors`、`make check-constants`、`make check-modulepath`、`make fmt`。
- TypeScript（ts/）：`npm ci`、`npm run generate`、`npm run generate:check`、`npm run check`、architecture/node/web/package/consumer 测试。
- Python（python/）：`PYTHONPATH=src python3 -m unittest discover -s tests`，与现有 CI 的 Python 3.10–3.13 矩阵一致，不引入另一套 runner。
- Rust（rust/）：`cargo fmt --all -- --check`、`cargo clippy --workspace --all-targets -- -D warnings`、`cargo test --workspace`。
- C++：现有 CI CMake Debug 配置、编译与 `ctest --test-dir cpp/build -C Debug --output-on-failure`。

## 11. 可验证工作单元与停止规则

本次仅完成设计阶段；后续实现计划须经本设计确认后再编写。

- D1 设计：输入仅为上述固定协议来源和仓库规范/代码；输出为本文件；完成条件为五项能力的请求/响应/流式/跨面行为及兼容性均有明确决策；校验本地链接、范围覆盖、git diff。时限 20 分钟，来源不可达时标明证据边界，不把未核实内容当成协议事实。
- R1 builder check：唯一 worker 只读设计与相关规范，只写仓库外唯一 JSON 工件；先写 .partial，Schema/设计 SHA-256/安全校验通过后原子发布；时限 5 分钟，超时或无进展取消，不消费未验证输出。
- S1 汇总：只有本会话读取已验证检查工件，解决实现者不能自行判断的歧义并更新本文件；时限 10 分钟；`git diff --check` 且工作区只有本设计文件的改动。
- 清理：删除仓库外下载与检查临时工件；不遗留后台任务；保留用户要求的 worktree 与设计文件，不改 main、不写产品代码、不发布版本。

## 12. 待确认的设计选择

这是设计者的推荐，不是用户已逐项批准的细节：

1. citations 完整进入 IR，并新增 CitationDelta，而不是仅在请求 Native 中保留。
2. 两个 OpenAI 目标本版均不近似映射 Anthropic citations；保留正文并明确报一次能力缺口。
3. top_k 为旧 wire 保真，采用 Native 而非公共 Params 字段，不替调用方迁移 API version。
4. disable_parallel_tool_use 本版只做 Anthropic 保真，不建立与 OpenAI parallel_tool_calls 的自动转换。
5. response-only 的非空 file_id 转成请求 Param 时明确报字段损失，而非假定上游会接受 SDK 未声明的请求字段。

## 13. 设计复核记录

本设计经过一次独立 builder check。复核提出的唯一实现阻塞项是“canonical IR 的未知引文应报 error，还是过滤后报 loss”。本会话已按现有 IR API 的返回契约解决：wire 审计允许过滤并报损；canonical/直接 IR 保持严格校验并报结构错误，不新增 IR loss 通道，见 §5.5 及验收矩阵。

本轮实际验证：Go 基线测试通过；`make vectors` 检查 389 个向量通过；本地文档链接和五项范围检查通过。完整五语言新功能测试属于未来实现阶段，本轮未执行，也未声称功能已修复。
