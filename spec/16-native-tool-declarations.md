# 16 — 原生工具声明

状态：ready。此扩展自实现版本 2.4.5 提供，IR contract 标记保持 `0.2.0`。

## 1. 范围

Anthropic 的 Advisor、web search 等服务端工具和 bash、text editor 等协议内置客户端工具，以及 OpenAI Responses 的 web_search、file_search、code_interpreter、computer、mcp、tool_search 等平台原生/内置工具使用带 `type` 的声明，不要求用户提供 function `input_schema`。版本化 discriminator 不表示执行位置。OXA MUST 将支持范围内的此类声明表示为来源限定的原生工具，而不是伪装成 function 或令整个合法请求失败。

本扩展仅覆盖请求的命名 `tools[]` 声明。它不新增 `server_tool_use`、`advisor_tool_result`、响应、流式或历史消息的映射，不处理 HTTP `anthropic-beta`，不执行工具、不校验模型支持。无 `name` 的 toolset 声明不在本版支持范围。

## 2. IR 变体 — N-NT-1

`Tool` 增加以下互斥变体：

```json
{"type":"native","name":"advisor","source":"anthropic","definition":"{\"type\":\"advisor_20260301\",\"name\":\"advisor\",\"model\":\"opus\"}"}
```

- `type` MUST 为 `native`。
- `name` 和 `source` MUST 为非空字符串。当前可恢复的来源为 `anthropic` 与 `openai`（自 2.4.9 起）；未知非空 source 可进入 IR，但编码时不得借用其他协议的同名工具。
- `definition` MUST 为字符串，其文本 MUST 是合法 JSON 对象；其中 `type` MUST 为非空字符串且不是 `function`、`custom` 或容器类型（如 `namespace`/`toolbox`）；若 payload 包含 `name`，外层 `name` MUST 等于该字段；若 payload 无 `name`（如 OpenAI 内置服务工具），外层 `name` 缺省等于 wire `type`。空 discriminator、空 name、错误 JSON 类型、名称不一致均为结构错误。
- 此变体 MUST NOT 带 `description`、`input_schema`、`format` 或 `native`。原生声明的这些字段若存在于 provider payload，均只保存在 `definition` 内。
- `source` 和 `definition` MUST NOT 出现在其他 Tool 变体上。
- 此新增 union 变体不改变已有 function/custom/apply_patch 语义。消费方的穷尽匹配 MUST 增加 native 分支。

`definition` 是 provider-neutral 的来源标记 opaque 载荷，而不是公开的 provider wire struct。原始 JSON 路径 MUST 保存完整声明对象的源文本，包括未知配置、字段顺序、内部空白与数值拼写。IR JSON 编解码 MUST 保留字符串内容；同来源 typed 编码恢复原始对象文本。最外层 JSON 序列化允许调整对象间的格式，但不得丢失声明配置或改写数值。数值恢复语义：整数值（无论书写为 `2`、`2.0` 还是 `1e+03`）MUST 以精确值恢复，双精度浮点无法精确表示的整数字面量 MUST NOT 静默取整；非整数十进制字面量 MAY 按实现的标准双精度数值语义恢复。Go、TypeScript、Rust、C++ 以原始文本恢复全部字面量拼写。无源文本的内存对象采用既有 canonical JSON 编码，不产生 loss。

这是限定在原生 Tool 声明边界的保真规则，不改变 spec/15 的 NativeAnnotation 审计和禁止通用未知字段透传的规则。原生声明内的 cache_control 整体保留，不再投影独立 native annotation；嵌套 model 不经过顶层 modelmap，也不进行别名或能力推断。

## 3. Anthropic 解码和回写 — N-NT-2

- `tools[i].type` 缺失、为空字符串、为 `function` 或 `custom` 时，沿用既有普通工具/schema-wrapper 解码，仍 MUST 要求 `input_schema` 为 JSON 对象。显式 function/custom discriminator 被识别，不另报未映射字段损失。
- 其他非空字符串 type 的命名声明映射为 `Tool{type:native,name,source:anthropic,definition}`。不得按工具名称推断类型，也不得要求 provider 定义的工具具有 input_schema。
- 显式非字符串 type（包括 null），非对象工具声明，以及非字符串/空 name，均为结构错误。
- 原生声明整体存入 definition，不对其内部字段另行 unmapped-field 审计；未知配置是声明的一部分。普通工具的未知配置仍按既有字段审计处理。
- 不维护版本化工具白名单，不校验每种工具专属参数或模型兼容性；合法 JSON 声明的 provider 专属约束仍由调用方/目标 API 判断。
- 编码回 Anthropic 时，仅恢复 source=anthropic 的 native 声明，完整保留 definition。恢复前必须验证 N-NT-1，不允许外层身份与 payload 冲突。

### 3.1 OpenAI Responses 解码和回写 — N-NT-5（自 2.4.9）

- `tools[i].type` 为非空字符串且不是 `function`、`custom`、`apply_patch`、`namespace` 或 `toolbox` 时，映射为 `Tool{type:native,name,source:openai,definition}`。若 wire 对象包含 `name` 则外层 name 取其值，否则外层 name 取 wire `type`。
- 不维护版本化工具白名单；合法 JSON 声明的 provider 专属约束仍由调用方/目标 API 判断。
- 编码回 Responses 时，仅恢复 `source=openai` 的 native 声明，完整保留 definition 原始文本并写回顶层 `tools[]`。恢复前验证 N-NT-1，不允许外层身份与 payload 冲突。

## 4. 目标能力缺口与损失 — N-NT-3

当前 OpenAI Chat Completions 不表示来源限定声明；Anthropic 与 Responses 分别仅表示来自自身来源（`source=anthropic` 或 `source=openai`）的原生声明，对于其他来源的原生声明 MUST 省略整个 native Tool，不得生成同名 function 或转换成目标协议的近似 builtin。

每个被省略声明恰好一条损失，按输入 Tool 顺序：

- Path：`tools[i]`，使用原 IR 下标；
- Field：`type`；
- Reason：`unsupported-semantic`；
- Detail：`native tool "NAME" from "SOURCE" cannot be represented by the target protocol`，NAME、SOURCE 使用 JSON 字符串转义并带双引号。

不再针对其内部 cache_control、model 或扩展字段生成额外损失。

## 5. 工具选择 — N-NT-4

`ToolChoice.tool_type` 扩展为允许 `native`，仅可用于 `mode:tool` 且 name 非空。Anthropic 解析 named choice 时，若其 name 对应已声明 native 工具，MUST 标记 tool_type=native。

同来源且目标实际保留相应声明时，native named choice 回写为 Anthropic `{type:tool,name}`。若对应声明被目标省略，或 native choice 找不到可恢复的同名原生声明，则 MUST 省略 choice，并在声明损失后追加一条损失：Path=`tool_choice`、Field=`tool_choice`、Reason=`unsupported-semantic`、Detail=`native tool choice "NAME" cannot be represented by the target protocol`。

直接构造 IR 时，若 named choice 未指定 tool_type 但名称对应被省略的 native 声明，也 MUST 执行此保护，不得输出悬空 function choice。若 mode=any 且全部声明被省略，沿用现有 `all tool declarations were dropped` 损失；混合列表仍有可表达声明时，保留 any。

## 6. 验收

共享向量覆盖 Advisor 最小声明、未知配置、版本化客户端工具、未来 discriminator、混合普通/原生声明、同源回转、跨面省略、native named choice、全部声明被省略时的 any。语言测试另覆盖原文/IR 保真、nested modelmap 不变、普通 function 的结构错误和直接构造的非法 native IR。
