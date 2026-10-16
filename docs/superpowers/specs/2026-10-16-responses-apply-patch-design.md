# OpenAI Responses Apply Patch 方案 A 设计

日期：2026-10-16

状态：用户已确认方案并授权实现 v2.3.0、使用 worktree 与 GitHub CI；实现/发布结果另行验证。本文“本轮只交付设计”的表述记录的是先前设计阶段的边界，不限制已获授权的实现阶段。

基线：v2.2.0，IR contract `0.2.0`，205 个共享向量。

建议目标：**实现包 v2.3.0 / Spec 2.3.0，IR contract 维持 `0.2.0`**。

## 1. 已确认目标与范围

采用已确认的方案 A：专用工具声明、专用调用/结果 Block、强类型 Operation。支持 Responses 的 `apply_patch`、`apply_patch_call`、`apply_patch_call_output`，保留 create/delete/update 操作、路径、diff、关联标识和结果状态，不将它们表示为 function 或 generic custom tools。

本阶段设计覆盖：

- 工具声明与专用 `tool_choice` 的双向转换。
- 请求历史中的调用/结果、响应输出中的调用/结果。
- 基于通用 `response.output_item.added/done` 的原子项流式转换。
- Go、TypeScript、Rust、Python、C++ 的同一 IR JSON 契约与共享向量。
- Chat Completions / Anthropic Messages 当前 profile 无专用等价映射时的显式 loss。

明确不包含：执行补丁、读取目标文件、修改工作目录、验证 diff 能否应用、路径清理、权限检查、HTTP/重试/工具执行器，以及 Custom Tools 在其他协议面的补齐实现。

本轮只交付本设计，不修改 `spec/`、schema、vectors、产品代码或包版本，不创建实现骨架，也不提交或发布。

项目约束依据：[IR 与不变量](../../../spec/01-intermediate-representation.md)、[Loss policy](../../../spec/02-loss-policy.md)、[Responses 映射](../../../spec/11-mapping-openai-responses.md)、[流式语义](../../../spec/20-streaming-semantics.md) 及 [最新版本阶梯](../../../spec/README.md)。

## 2. 版本号建议与兼容边界

### 2.1 推荐 v2.3.0

这是现有 hub-and-spoke 框架中的独立能力补充，不是架构重构，因此推荐：

| 版本轴 | 当前 | 本方案建议 |
|---|---|---|
| 五语言实现包 | 2.2.0 | **2.3.0** |
| 规范 | 2.2.0 | **2.3.0** |
| IR document `specVersion` | 0.2.0 | **0.2.0，不变** |
| 兼容读取 | 0.1.0、0.2.0 | 保持不变 |
| Go module path | `github.com/elkpi/oxa/go/v2` | 保持不变 |

不建议 3.0.0：没有重构整体架构。不建议 2.2.1：新增完整工具类别、Operation 联合、两类 Block、状态语义和流式映射，比 BUG 修订或小 feature 更适合中间版本。

`spec/13` 对其他协议面补齐所写的“例如 2.3.0”是候选排期，不是已发布承诺。本方案不自动把该工作打包进来；若 Issue 2 先进入下一发行版，补齐工作可另排后续 minor，不能据此宣称它已完成。

### 2.2 兼容性声明

维持 `specVersion` 不表示旧实现能理解新 Block。v2.3.0 reader 必须继续读取旧的合法文档；含 Apply Patch 的文档应交给具备本能力的新实现，不能承诺旧 v2.2.0 reader 无损读取。

新增 sealed-union/enum 变体可能要求 Rust、TypeScript、C++ 下游补齐穷尽分支，Go 下游也需要处理新的 type-switch 分支。依项目最新版本规则采用 minor，但发行说明必须列明这些消费者迁移点，不宣称所有下游源码零改动。

现行 `spec/README.md` 的新版本阶梯已体现该政策，但其旧说明及 `spec/01 §2` 仍残留“sealed union 扩展必升 major”的文字。后续修改规范时应同步消除冲突，不再依据旧句子把本功能提升为 major；本轮不改正式规范。

## 3. 官方来源核对与补充

固定 OpenAI Python SDK commit：`e71d781ac5e7b3f365ac130bf338baaf13ae720d`。

重要结论：

1. 专用声明和强制选择都只有 `type:apply_patch`，不需要工具名称。
2. 调用状态是 `in_progress | completed`；结果状态是 `completed | failed`。这是两个不同状态域。
3. 客户端 `input` 的调用/结果项允许省略或提供 null 的 item `id`；服务端输出模型声明 item `id`。两者都使用 `call_id` 关联，不能拿 item `id` 替代。
4. 客户端结果 `output` 为可选、可为 null 的字符串；缺失/null 与空字符串必须区别处理。
5. `ResponseOutputItem` 同时包含调用与结果，因此结果不仅能出现在客户端 `input`，也能出现在 `response.output` 和通用 output-item 流生命周期里。
6. 该固定 commit 的 `ResponseStreamEvent` 没有 Apply Patch 专用 diff-delta 事件。不能从其他工具的事件名类推、凭空新增 wire 事件。

来源：

- [工具声明](https://github.com/openai/openai-python/blob/e71d781ac5e7b3f365ac130bf338baaf13ae720d/src/openai/types/responses/apply_patch_tool_param.py)
- [专用工具选择](https://github.com/openai/openai-python/blob/e71d781ac5e7b3f365ac130bf338baaf13ae720d/src/openai/types/responses/tool_choice_apply_patch_param.py)
- [客户端输入项：ApplyPatchCall / ApplyPatchCallOutput](https://github.com/openai/openai-python/blob/e71d781ac5e7b3f365ac130bf338baaf13ae720d/src/openai/types/responses/response_input_item_param.py#L477-L605)
- [服务端调用](https://github.com/openai/openai-python/blob/e71d781ac5e7b3f365ac130bf338baaf13ae720d/src/openai/types/responses/response_apply_patch_tool_call.py)
- [服务端结果](https://github.com/openai/openai-python/blob/e71d781ac5e7b3f365ac130bf338baaf13ae720d/src/openai/types/responses/response_apply_patch_tool_call_output.py)
- [响应输出联合](https://github.com/openai/openai-python/blob/e71d781ac5e7b3f365ac130bf338baaf13ae720d/src/openai/types/responses/response_output_item.py#L333-L365)
- [流事件联合](https://github.com/openai/openai-python/blob/e71d781ac5e7b3f365ac130bf338baaf13ae720d/src/openai/types/responses/response_stream_event.py)
- [output_item.done](https://github.com/openai/openai-python/blob/e71d781ac5e7b3f365ac130bf338baaf13ae720d/src/openai/types/responses/response_output_item_done_event.py)

## 4. 方案 A 的最终 IR 形状

### 4.1 命名细化

保留方案 A 的核心，不复用 CustomToolUseBlock，也不使用 `map[string]any` 承载 Operation。

上轮示意直接用了 wire 的 `apply_patch_call` / `call_id`。本稿将 IR 命名统一到现有 `CustomToolUseBlock` 的约定：IR 使用 `apply_patch_tool_use` / `apply_patch_tool_result` 和 `id` / `tool_use_id`；Responses wire 仍使用 `apply_patch_call` / `apply_patch_call_output` 和 `call_id`。这是公开 JSON 契约的设计细化，须随本稿审阅，不是对现行实现的修改。

IR 与 wire 分别定义类型；即使部分字段对应，也不将 Responses 的 OutputItem/Operation wire struct 暴露到 `ir`。

### 4.2 Tool 声明

IR 与 Responses 都使用：

```json
{"type":"apply_patch"}
```

Canonical IR 仅允许 `type`，禁止 `name`、`description`、`input_schema`、`format`。实现时沿用现有 Tool 容器，在 serializer/schema 中按类别校验，不重构整个工具 API。Go 内存表示中 Name 等字段可以是零值，但不能序列化为多余属性。

原有 function/custom 声明的名称与字段规则不变。SDK 的 `allowed_callers` 不纳入本次 IR，按存在性记录 `unmapped-field`，包括 null/空列表，不能静默忽略。

### 4.3 ToolChoice

IR：

```json
{"mode":"tool","tool_type":"apply_patch"}
```

Responses：

```json
{"type":"apply_patch"}
```

规则：

- mode=tool 且 tool_type=apply_patch：name 必须缺失。
- mode=tool 且 tool_type 为 function/custom（或缺省 function）：name 必须非空。
- auto/any/none：继续禁止 name 和 tool_type。
- 不新增 mode，不从 `name:apply_patch` 推断类别；没有完整 tools 声明时，也应能独立映射这份显式 choice。

这要求细化既有“mode=tool 必有 name”的规则，而不是在构造时塞入一个虚假的 `name:apply_patch`。

### 4.4 ApplyPatchOperation

JSON 为三个扁平判别变体，不增加另一层 operation 参数袋：

```json
{"type":"create_file","path":"src/new.py","diff":"+print(\"hello\")\n"}
```

```json
{"type":"delete_file","path":"src/old.py"}
```

```json
{"type":"update_file","path":"src/main.py","diff":"@@\n-print(\"old\")\n+print(\"new\")\n"}
```

条件约束：

| 操作 | path | diff |
|---|---|---|
| create_file | 必填 string | 必填 string，空串有效 |
| delete_file | 必填 string | 必须缺失 |
| update_file | 必填 string | 必填 string，空串有效 |

路径按 SDK 字符串类型接收，不擅自增加文件系统规则：不 resolve、clean、改斜杠、改盘符、做 Unicode normalization，也不拦截 `..`、绝对路径或空串。安全执行由调用方负责；oxa 不执行，因此不能把路径检查误充为转换职责。

diff 是完整的不透明文本。保留 CRLF/LF、尾部换行、空格、Unicode 和控制字符，不解析 hunk，不补 header，不校验是否为可应用补丁。示例字符串只演示传输形状，不表示 oxa 已验证补丁语法。

JSON 外层正常转义。验收比较解码后字符串的 UTF-8 内容，不要求把 `\uXXXX` 与字面 Unicode 的外层 token 拼写也保持一致，更不要求整份 wire JSON/SSE 逐字节回放。

### 4.5 调用 Block

类型：`ApplyPatchToolUseBlock`。

```json
{
  "type":"apply_patch_tool_use",
  "id":"call_123",
  "status":"completed",
  "operation":{
    "type":"update_file",
    "path":"src/main.py",
    "diff":"@@\n-old\n+new\n"
  }
}
```

- id：非空字符串，对应 wire call_id，不是 item id。
- status：必填 `in_progress | completed`，使用独立的 ApplyPatchCallStatus 类型。
- operation：必填的强类型 Operation。
- 不携带 name、function input、custom input 或 is_error。

显式保留调用状态，避免把所有调用都悄悄编码成 completed；调用完成状态不表示文件操作执行成功。

### 4.6 结果 Block

类型：`ApplyPatchToolResultBlock`。

```json
{
  "type":"apply_patch_tool_result",
  "tool_use_id":"call_123",
  "status":"failed",
  "output":"patch could not be applied"
}
```

- tool_use_id：非空字符串，对应 wire call_id。
- status：必填 `completed | failed`，使用独立的 ApplyPatchResultStatus 类型，不能复用调用状态 enum。
- output：可选 string；canonical IR 不允许 null。
- 原生 output 缺失或 null 均规范化为 IR output 缺失，按 SDK Optional[str] 的空值语义处理，不报损；output="" 必须保留为空字符串，不能合并为缺失。
- IR→wire 时 output 缺失就省略，不合成空文本，也不额外输出 null。
- 不增加 IsError，不把 failed 改成顶层 Response failed/refusal。

### 4.7 五语言表示

公共 JSON 以本节为准，各语言使用本地强类型：Go 的封闭 Operation 接口及三个变体、Rust enum、TypeScript 判别联合、Python 三个 dataclass 的 union、C++ std::variant。每个变体都有必需 Path，只有 create/update 类型携带必需 Diff。

Go 的结果 Output 用 `*string` 区分缺失与空串；其余语言采用各自的 optional 表示。Path/Diff 是普通文本字段，不需要把它们装成承载 function 参数的 Raw JSON 值。

本稿不生成这些定义的代码；具体签名和文件拆分由设计批准后的实现计划列出。

## 5. Responses 非流式映射与关联

### 5.1 四个方向

| 位置/方向 | 对应关系 |
|---|---|
| request tools / tool_choice | 按 §4.2–4.3 双向映射 |
| request input 的 apply_patch_call | 进入 assistant 调用 run，生成专用 UseBlock |
| request input 的 apply_patch_call_output | 进入紧随其后的 user 结果 run，生成专用 ResultBlock |
| response output 的 apply_patch_call | 进入 Response.Content，生成专用 UseBlock |
| response output 的 apply_patch_call_output | 进入 Response.Content，生成专用 ResultBlock，不误当 assistant 发起的新调用 |
| 反向编码 | 各专用 Block 恢复对应 wire item，保留状态、Operation 与 output 的缺失性 |

解码工具声明和 choice 时必须先分派 type，再验证各变体的字段；不能沿用所有工具都先读取 name/input_schema 的逻辑。

### 5.2 混合历史与 INV-3/4

同一 assistant turn 可以包含 function、custom、apply_patch 三类调用；下一 user turn 统一包含匹配结果。既有思维/文本规范化不变，但三类调用之间、三类结果之间的共享相对顺序不能按类别排序。

关联以 `(调用类别, id)` 对应 `(结果类别, tool_use_id)`，同时要求同一调用 turn 内的 id 唯一。以下均非法：孤立结果、重复结果、缺少必需结果、错序结果、把 custom/function 结果当作专用 Apply Patch 结果。

请求历史规则沿用现有完整会话约束，不伪造一条调用来“修复”来源于未知外部上下文的结果。单独转换响应时不要求同时提供 tools 声明或请求历史；Response.Content 的结果可关联到外部已有上下文，不适用请求侧的“必须有前一 assistant turn”要求。

### 5.3 item id 与渲染默认值

- request input 的可选 item id 是 wire envelope，不进 IR；编码客户端 input 时省略。
- response output 编码生成唯一 item id：调用使用 `apc_0`、`apc_1`，结果使用 `apco_0`、`apco_1`；计数分别在一份响应内从 0 开始，不能所有项都复用一个常量 id。
- 原生 `call_id` 永远来自 IR 关联字段，不得合成、重编号或从 item id 推导。
- 不读取源文件、不据文件是否存在决定 create/update/delete。

### 5.4 顶层响应状态

沿用 N-R-11 的归类：native completed 且存在保留的 function/custom/apply_patch 调用时为 IR tool_use；仅有文本或结果时为 end_turn。既有 incomplete/error 映射不变。

同一输出同时带调用和结果时，保留二者及原有调用归类，不引入执行器决策。tool_use 不是重新执行补丁的授权；调用方必须利用 call_id 与已有结果识别已经回答的调用。failed 结果不自动使顶层 Response failed，不能把三个层次的状态混为一谈。

## 6. 流式设计：原子项，不新增 Delta

### 6.1 范围与 IR 语法

覆盖调用项和结果项的通用 output-item 生命周期。固定 SDK 没有专用 Apply Patch diff-delta，因此本阶段不新增 IR Delta，也不借用 InputJSONDelta / CustomToolInputDelta。

原生 item 在 output_item.done 闭合后，回放：

```text
content_block_start（完整专用调用或结果 Block）
content_block_stop
```

该块接受零个 Delta；任何 Delta 都是 IR block/delta 错配。整个消息仍满足 INV-5，保留块仍按 INV-6 从 0 连续编号。结果 Block 出现在 Response.Content 和 ContentBlockStart 中属于本次明确新增的合法位置，不能误用既有 function ToolResult 不支持响应流的规则将它跳过。

### 6.2 解码生命周期

1. added：验证原生索引和关联身份，暂存调用/结果项；暂不发 IR 块。
2. done：验证同一原生项，完整解析最终快照，并发出一个原子 IR 块。
3. terminal：校验可用的重复完整快照，不再创建块；最终状态/usage 进入 MessageDelta/MessageDone。

operation/status/output 在 added 中是初始快照，以 done 的最终值为准。尤其不能要求 added.diff 与 done.diff 完全相同：没有专用 diff-delta 并不证明初始快照就是最终补丁。done 中缺少必填字段仍然是错误，不能靠 added 悄悄补齐。

稳定身份是 item 类型、output_index、原生 item id 和 call_id；身份改变、重复 added/done、done 无 added、未闭合就 terminal、提前 Flush 均按现有生命周期规则报错。source item id 与 call_id 分别验证，不互相替代。

终止 response.output 如果携带了已经闭合的项，应按身份核对最终 status、Operation 或结果 output，不再输出一份相同 Block。item id 的必填性遵循该入口的 schema：不能把客户端 input 的 id 可选规则泛化为服务端 output 的规则；也不能将 call_id 与 item id 相互替代。

### 6.3 编码生命周期

每个 Apply Patch Block 生成一个 added 和一个 done：

- 调用 added 的 status 采用 in_progress；done 的 status 保留 IR 调用状态，不能强制改为 completed。
- 结果 added/done 的 status 都保留 IR 结果状态，不能为结果造出不存在的 in_progress 值。
- Operation、path、diff 原样进入项，不生成 function_call_arguments 或 custom_tool_call_input 事件。
- 终止 response.output 按原生输出顺序包含这些最终项，保留既有 message/reasoning/function/custom 项的组织方式。
- 原生索引、合成 item id、sequence_number 与 SSE framing 继续遵循既有 Responses profile，不扩展 sse 包职责。

## 7. Loss、错误与过滤后的一致性

### 7.1 错误与 loss 分类

| 条件 | 处理 |
|---|---|
| 非法 JSON、已知必填字段缺失或类型错误 | error，不返回部分结果 |
| create/update 的 diff 缺失/null/非字符串 | error；空字符串合法 |
| canonical IR 的 delete Operation 携带 diff | IR 结构错误 |
| wire delete Operation 出现额外 diff | 按不认识的属性记 unmapped-field，不能解析它的值 |
| wire 中未知 operation.type | 整个调用 unsupported-semantic，不包装成 custom/function；canonical IR 中的未知变体则是结构错误 |
| 原生调用/结果 status 是未来未知字符串 | unmapped-value，并丢弃无法形成合法 IR 的对应单位；不猜成 failed/completed |
| status 字段缺失/null/非字符串 | error |
| allowed_callers、caller、created_by 等未建模属性 | 按属性存在记 unmapped-field，包含 null/false/空值 |
| 身份/关联/流式语法错误 | error，不冒充 loss |

未知属性按原始来源顺序合并到 loss 列表，opaque diff/path/output 不递归检查。整个不支持单位已经被丢弃时，不重复审计其内部属性。

对于新类型在请求历史中的语义过滤，先使用本次转换内的关联表确定哪些调用/结果可以保留，再组装 IR。若某个 Apply Patch 调用或对应结果因未知 Operation/status 无法表达，应一并省略关联的另一半，并为被连带省略的单位记录 unsupported-semantic，保留源顺序；不能输出孤立结果或制造缺少结果的 IR 调用。孤立/重复/错类等本来就不合法的历史仍报结构错误，不能利用这个机制掩盖来源错误。

### 7.2 不支持的目标协议面

Chat Completions 和 Anthropic Messages 当前 profile 不提供专用 Apply Patch 等价项，默认严格降级：

- 每个专用声明、指定 choice、调用 Block、结果 Block 各记录一个 unsupported-semantic。
- 不把 Operation JSON 塞入 function arguments；不转成普通代码文本；不自动创建同名 custom tool；不注入提示词。
- 混合 turn 只丢弃不支持的单位，保留原 function/custom 处理规则及可表达内容。
- 若整个 turn 被丢空，按既有规则生成合法空文本消息；仍有 function 结果时，不能额外造一个空 user turn。
- any/required 的所有声明被过滤后，应省略 choice 并报损；专用 choice 被丢弃时不能残留强制调用不存在工具的配置。
- 若丢弃了调用、没有剩余受支持调用，且 stop_reason 原为 tool_use，才归一化为 end_turn 并记录 degraded；不能改变其他合法 stop reason。
- 流式每个被跳过的 IR 块一次 loss，校验该块合法语法后吸收其 stop，不占用目标原生块/工具索引；压紧保留项，保持最终 usage 与终止事件。

### 7.3 与 legacy custom apply_patch 并存

`{type:custom,name:apply_patch}` 继续走 Custom Tools 路径，任意 input 原样保留。`{type:apply_patch}` 才进入专用路径；两种声明、choice、调用与结果可并存，但不能共享类别或靠名称自动升级。

## 8. 组件边界与拟涉及位置

以下是后续实现的边界，不是本轮已完成改动：

| 层 | 职责 / 拟涉及位置 |
|---|---|
| 规范 | spec/01、11、20 及拟新增 spec/14-apply-patch.md；扩展类型、合法位置与关联规则 |
| 结构 | spec/schema/ir.schema.json：Tool/Choice 条件分支、Operation 联合、新 Block，无新 Delta |
| 行为锁定 | vectors/responses/nonstream、stream 与跨协议降级用例；刷新 manifest |
| Go | go/ir、go/openai/responses、两个目标编码器、veccheck 与 vectest |
| TypeScript | ts/src/ir、generated、Responses、目标编码器、流归一化/向量 runner |
| Rust | oxa-ir、oxa-responses、两个目标 crate、oxa-vectest |
| Python | python/src/oxa/ir、Responses、两个目标编码器、wire audit、vectest |
| C++ | ir.hpp/ir.cpp、Responses codec/audit、目标编码器、vectest |

不新增 direct face-to-face converter，不向 modelmap 或 sse 注入新职责。尽量将 Apply Patch 专有解析/映射放在各语言自己的独立小文件中，公共非流式和流式路径共享该语言内的 Operation 校验，避免两份不一致规则。

## 9. 向量与验收矩阵

实现时按“规范 → schema/黄金向量 → 实现”顺序锁定。新向量命名跟随实际路径生成，不预报尚未创建的向量数量；现有 205 个用例不能通过改期望或跳过来制造绿灯。

| 用例组 | 必须覆盖的行为 |
|---|---|
| 声明/选择 | 无 name 的专用声明和 choice；旧 function/custom 形状不变；IR 非法混合字段被拒绝 |
| Operation | create/delete/update 双向；create/update 空 diff 合法、缺失非法；delete 不发 diff |
| 文本保真 | 路径斜杠/盘符/..、Unicode、CRLF、尾部空白/换行、控制字符、不合法补丁文本原样保留 |
| 调用状态 | in_progress/completed 均保留，不当作结果成功/失败 |
| 结果状态/输出 | completed/failed；output 缺失、null、空字符串、非空字符串；错误类型拒绝 |
| 方向/位置 | input 历史和 response.output 的调用/结果均支持；服务器结果不误当新调用 |
| 关联 | 三种工具混合 turn；错误 id/类别、重复/孤立/缺少结果、错误相对顺序 |
| Custom 隔离 | 专用 apply_patch 与 custom name=apply_patch 同时存在，往返仍可区分 |
| 流式原子项 | added→done→terminal，只产生一次 start/stop，零 Delta，无参数增量事件 |
| 流式快照 | added 与 done 的 diff 不同但身份稳定；done 缺字段拒绝；terminal 不重复拼接/输出 |
| 流式结构错误 | 错 item id/call_id/output_index、重复闭合、提前终止、错配 Delta、错误 IR index |
| 审计/语义缺口 | 嵌套未知属性、null/false、未知 op/status；单位级抑制重复 loss；请求关联过滤后合法 |
| 不支持目标 | 声明/choice/调用/结果都有 loss；不生成 function；混合结果不增加空 turn；stop 与索引正确 |
| 兼容与分发 | 旧 IR 文档仍可读；新 canonical 输出仍为 0.2.0；包版本/生成类型/manifest 一致 |

所有五语言必须执行同一套新旧向量，不仅检查常量或退出码。向量加载器必须定位当前 worktree，不能误用父仓库的旧 vectors，也不能过滤掉新增类别。

正式验收由 GitHub CI 覆盖 Go 1.23/1.24 的 race、Node/Windows/Web/包消费者、Rust test/fmt/clippy、Python 3.10–3.13、C++ 三平台与 -fno-exceptions，以及 vectors/manifest/consumers/reliability。设计稿本身不声称已运行这些实现检查。

## 10. 本轮完成条件

D-01 输入边界为现行规范、相关 IR/Responses 类型及固定 SDK 文件；唯一交付物为本设计文件。最大时长 30 分钟；发现必须由用户决定的范围冲突或无法核实的关键协议行为时停止并明确缺口。

机械检查：Markdown 中所有 JSON 示例必须可解析；相对文档链接必须存在；目标版本、IR contract、三类 Operation、两个状态域、请求/响应/流式、错误/loss、Custom 隔离和验收矩阵必须齐全；Git 改动仅允许这份文档。

不创建临时文件、后台任务、子代理或 worktree，无待回收资源；不改已发布规范和版本。实现计划与产品改造不属于本轮交付，须在本设计审阅后再开始。
