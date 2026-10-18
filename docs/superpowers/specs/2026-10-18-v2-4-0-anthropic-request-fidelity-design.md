# v2.4.0 Anthropic 请求配置保真设计

日期：2026-10-18
状态：Grilling 设计定稿；Q1～Q12 已确认，产品与行为决策前沿已清空。实现尚未开始，实施计划需在执行前审阅。
版本：Q9 已确认完整方案发布为 v2.4.0；不另外安排功能缩减的 v2.3.2。
交付范围：Go、TypeScript、Python、Rust、C++；请求转换，不扩展响应或响应流。

本文是实施前设计，不取代 `vectors/`、`spec/schema/` 和 `spec/*.md` 的规范优先级。本文的存在不代表转换功能已经实现，也不授权提交、推送、发版或发布包。

## 0. 版本门禁与检查结论

`spec/README.md:26`、`:51`、`:57` 规定：小 feature/bugfix 可以升 patch，但框架内新参数能力应升 minor，冻结系列内不能改变既有 IR 形状或 loss 语义。本设计新增 IR 承载、扩展 effort，并移动既有 loss 产生阶段，不能未经说明直接视为普通 2.3.2 补丁。

Q9 已确认：完整方案调整为 2.4.0，保留 Q1～Q8 的全部能力与规则；不另外安排功能缩减的 2.3.2，不引入版本政策例外。

公开 API 的迁移也必须明示：`rust/crates/oxa-ir/src/request.rs:268` 的 effort 是封闭枚举，新增档位会影响穷尽匹配；同文件的公开 Request/Tool/块类型增加字段会影响直接构造它们的消费者。现行仓库政策不因新增枚举自动升 major，但不等于源码完全兼容。其他语言公开类型及 C++ 布局变化也需要在计划中核对，而不是仅验证 JSON 缺省兼容。

独立可实施性检查提出的三项边界已通过 Q10～Q12 收敛：thinking 使用明确的组合记账表；未知枚举丢弃最小完整配置单元，不擅自触发默认策略；损坏或歧义的 IR 结构返回 error，合法但不可理解的来源扩展产生 loss。精确规则见 §6.1～§6.3。

设计定稿不等于实现已验证。实施计划依本文编写，执行前需要审阅；未经授权不创建提交、远程 PR、tag 或包发布。

## 1. 目标与问题分解

v2.3.1 的 Go 合成复现已经确认：`thinking.type` 为 `adaptive`、`disabled` 或未知值时，thinking 配置可能消失且没有对应 loss；`output_config`、`context_management` 与各处 `cache_control` 则按现有策略报 `unmapped-field` 并丢弃。其他四语言已作对应静态核对，尚未执行其运行时复现。

这不是“关闭 loss 检查”的版本。v2.4.0 必须交付：

1. Anthropic → IR → Responses。
2. Anthropic → IR → Chat Completions。
3. Anthropic → IR → Anthropic，用于验证支持范围内的配置没有在进入 IR 时提前丢失。

能映射的共有语义尽量映射；目标无法表达的配置继续报准确 loss；结构错误继续返回 error。调用方决定哪些 loss 可以接受，库不预设任何忽略策略。

原生保真只针对本版明确支持的配置和现有支持的内容结构，不是“任意 Anthropic 请求零 loss”的承诺。

## 2. 已确认的决策

| 决策 | 结论 |
| --- | --- |
| Q1：转换目标 | 两条跨协议链路都支持；不以隐藏 loss 伪装无损；原生回转验证保留能力 |
| Q2：语言范围 | 五语言采用同一规范和 golden vectors，协调验收、发布 |
| Q3：请求/响应边界 | 只新增请求侧能力；`stream: true` 的请求参数也适用，不新增响应或响应流字段 |
| Q4：IR 路线 | 共有语义明确建模，已识别的专属配置受限保留；禁止任意未知字段透传 |
| Q5：结构化输出 | 新增 `output_config.effort` 支持，不新增 `output_config.format` 语义映射或原生保留；后者仍明确报损 |
| Q6：loss 阶段 | 在实际丢弃或近似的边界报损；完整保留时不提前报损 |
| Q7：loss 结构 | 复用现有四类 reason；稳定的 Field、规范化的 Path；不要求解析 Detail |
| Q8：IR 编辑 | 显式修改后的通用 IR 值优先，旧来源备份不能覆盖新值；只影响相关备份 |
| Q9：版本 | 完整方案发布为 v2.4.0；不拆分功能缩减的 v2.3.2 |
| Q10：thinking 记账 | 按 §6.1 分项报损；预算派生 effort 的一条 degraded 覆盖该组近似，不重复记模式损失 |
| Q11：未知枚举 | 按 §6.2 丢弃最小完整配置单元；不把未知 TTL/限制值删除后改用默认策略 |
| Q12：不合法 IR | 按 §6.3 区分结构错误、未知扩展和正常编辑；不容许任意 wire 注入 |

## 3. 功能范围

### 3.1 thinking 配置

本版支持请求级 `thinking.type` 的 `enabled`、`disabled`、`adaptive`，以及适用于本版支持配置的 `budget_tokens` 和 `display`。`display` 包括 `summarized`、`omitted`、`updates`。

- `thinking.type` 是模式，不是 effort。不得写入 `ReasoningEffort = "adaptive"`。
- 显式 `output_config.effort` 映射到通用 IR effort；不能仅因出现 `adaptive` 就猜测 `high` 或其他默认值。
- 没有显式 effort 的既有 `enabled + budget_tokens` 仍可按现有预算表生成近似 effort，同时保存精确来源配置。
- 同时存在显式 effort 和 enabled budget 时，显式 effort 决定通用 IR effort；精确 budget 单独保留，不额外冒充第二个 effort。
- 本版不把其他新模式（例如 `between_tools`）自动纳入支持。未知模式值必须报 `unmapped-value`，不得静默消失。
- 未知模式导致整个 thinking 构造无法解释时，按一个明确的不可映射单元处理，不假装其中的 display 能独立恢复完整配置。
- `updates` 需要的 beta header 由 HTTP 调用方负责；本库不读取、生成或验证 HTTP header。

原生编码恢复仍有效的模式、预算、展示偏好及显式 effort 的位置/存在性。跨协议时，显式 effort 映射后并不意味着 thinking 模式或展示偏好也已等价映射；无法等价表达的剩余部分必须报损。

### 3.2 effort

通用 IR effort 在既有 `minimal/low/medium/high` 上新增 `xhigh/max`。Anthropic 原生 effort 的已知五档为 `low/medium/high/xhigh/max`；OpenAI 两侧的协议枚举包含 `xhigh/max`，本版对这两档进行对应映射，不擅自降档。

- 不根据模型名称判断取值支持，不增加模型能力数据库。
- 本版不因该 issue 顺带新增 OpenAI `none` 的 IR 映射；`disabled` 也不能未经明确语义映射就冒充 effort。
- 不将“不知道某个模型是否支持某档 effort”报告成已发生的转换损失。
- 未知 effort 依既有未知枚举规则明确报损。

### 3.3 cache_control

保留受支持的缓存配置及挂载位置：

- 请求顶层的自动缓存配置。
- `system[]` 中已支持的块。
- `messages[].content[]` 中现有 IR 能承载、且 Anthropic wire 结构允许该属性的块。
- `tools[]` 中本项目已支持的工具定义。
- `tool_result` 自身，以及其已支持的嵌套内容块上合法的缓存配置。

不为此新增 document、server tool 等尚不支持的块/工具。未知整块被丢弃时，沿用一个 whole-block loss，不为其内部缓存配置重复报损。

本版保留 `ephemeral` 类型、缺省 TTL 与文档确认的 `5m/1h` TTL。缺省 TTL 与显式 TTL 的存在性不得无故互换。实现时以官方字段定义核对适用位置和允许成员，不能把“本地 JSON 能解析”当作 provider 认可该位置的证据。

缓存标注绑定所属 IR 对象，不能只保存一个全局原始数组下标用于回填。编码器不得为使用字符串简写、合并块或重排工具而悄悄迁移/删除缓存断点；必要时使用数组/对象形式。对象被调用方显式删除时，不把其标注迁移到其他对象。

Chat 和 Responses 不输出 Anthropic 缓存配置；其他缓存机制不自动视为等价物。每处不能保留的断点分别产生 `field: cache_control` 的 loss。

### 3.4 context_management

保留请求级 `context_management.edits` 中本版识别的两类策略：

- `clear_tool_uses_20250919`。
- `clear_thinking_20251015`，包含 `keep: "all"`。

保留已识别选项、数组顺序、显式存在性，不在客户端执行策略，不重排 edits，不改写为裁剪历史，不与摘要压缩混淆。

未知 edit 类型作为不能解释的整体单元明确报损；其他已识别 edit 不因此消失。已识别结构内未知成员仍执行字段审计，不能因为 IR 中使用 opaque 存储就跳过审计。具体允许成员和形状在规范阶段由官方字段定义逐项固化。

编码到 Chat/Responses 时不模拟服务端上下文编辑，整体策略不可表达时产生一个 `field: context_management` 的 `unsupported-semantic` loss，而不是给每个子属性重复记账。

本版不新增响应的 `context_management.applied_edits` 或响应流的相关字段。它们仍按现行审计策略处理。

## 4. 受限来源配置的 IR 契约

这是一项显式的 IR/schema 能力扩展，不得借用现有字符串 Metadata 参数袋，也不得将 provider wire struct 暴露成通用 IR 类型。

### 4.1 逻辑数据要求

受限保留信息必须具备下列能力；具体字段命名、公共类型名称和各语言惯用容器由实施者决定：

1. 标识来源协议和规范定义的已知配置种类。
2. 在所属请求、系统块、内容块、工具或嵌套块上保存配置，而不是持有整个原始请求。
3. 将通过来源 face 字段审计的配置保存成 opaque JSON 文本；不把未知字段借道透传到目标。
4. 对与通用 IR effort 重叠的配置，记录足以辨认原投影值、显式 effort/预算派生来源及后续编辑的信息；不保存用户消息正文快照。
5. 提供 face-neutral 的“配置未被目标保留时会损失什么”的描述，使其他 spoke 不必解析 Anthropic wire JSON 就能报告准确 loss，包括预算投影的近似损失。
6. 可通过标准 IR JSON 结构序列化、反序列化；不是 Go 私有 side table，也不依赖对象地址、全局状态或回调。
7. 缺省没有受限保留信息时，既有 IR JSON 不改变。

保留机制的白名单只覆盖本版配置。未知来源、未知种类和不合法的 IR 附加数据必须有确定的校验/报损规则，不得被直接合并进 wire 输出。已识别配置中的工具参数和 JSON Schema 仍遵守既有 opaque 边界，不新增深度解析。

### 4.2 来源与目标的职责

- Anthropic 解码器负责识别、结构检查、字段审计和创建受限保留信息。
- Anthropic 编码器负责解释自身的已知配置并恢复到正确 wire 位置。
- Chat/Responses 编码器只消费通用 IR 语义和中立的遗漏描述，不解析 Anthropic 配置，不增加直接 face-to-face 路径。
- 模型名称仍只经 `modelmap` 转换。

## 5. IR 编辑与原生恢复

下列技术默认落实 Q8 的新值优先原则；公共类型命名和最小投影存储方式在实施计划中固定：

- effort 未变且预算原来是派生来源时，恢复精确原预算，不用离散预算覆盖它，也不凭空增加 `output_config.effort`。
- effort 来自显式原生 effort 时，新的 IR effort 始终决定输出值；同时存在的独立原生预算不被误认为 effort 的旧投影。
- adaptive 配置原来没有显式 effort，而调用方新增 effort 时，保留 adaptive 配置并输出新 effort。
- 调用方修改或清除预算派生的 IR effort 后，旧预算不能复活。仍可用既有预算表表达的新值采用显式近似重建并报 `degraded`；清除后不能形成完整 thinking 配置时，删除失效的整个配置并明确报损。
- 新值为 `xhigh/max`、旧 enabled 预算表不能表示时，不捏造预算。使用明确的现代 adaptive + effort 表达；改变原模式/预算的成本必须报损。
- 缓存、上下文管理和展示偏好不因无关 effort 编辑被一并清空；但失去宿主的配置不能单独伪恢复。

未带来源配置的既有 IR，其 `minimal/low/medium/high` → Anthropic 预算编码规则保持现状，避免本次能力扩展暗中改写既有调用。新支持的 `xhigh/max` 无旧预算规则，采用 adaptive + `output_config.effort`。这不是按模型名称选策略，具体模型是否接受仍由调用方负责。

用户若直接编辑受限来源配置本身，必须同时遵守 IR 的结构和投影绑定规则；不能依赖与通用字段相矛盾的私有 payload 覆盖通用字段。配置被恢复、抑制或重建的规则必须可单独测试。

## 6. loss 契约

继续使用 `unmapped-field`、`unmapped-value`、`unsupported-semantic`、`degraded` 四类 reason，不增加字段专属 reason 枚举。

- `cache_control` 无对应字段而被丢弃：`unmapped-field`，Field 为 `cache_control`。
- 上下文编辑策略整体无法表达：`unsupported-semantic`，Field 为 `context_management`。
- 未知 thinking 类型/effort/已识别配置中的未知枚举：`unmapped-value`；使用具体 leaf Field 和相应 Path。
- 未知成员：按现有规则 `unmapped-field`，presence 即触发，不以 null/false/空值绕过。
- 精确预算最终只以近似 effort 保留：`degraded`，不是字段已完整保留时提前报损。
- 纯丢弃不得标成 `degraded`。

Decode loss 以源 wire 为根，Encode loss 以 IR 为根；组合结果为 Decode losses 后接 Encode losses，不重写成伪源路径，也不添加阶段前缀。Decode 内维持源 encounter order，Encode 使用规范固定的 IR 遍历顺序，不能依赖 map 的迭代顺序。

调用方必须检查两个阶段的 losses。发布说明需明确：部分现有配置的 loss 阶段、路径和数量会变化；这属于保真能力变化，不是原地关闭审计。

### 6.1 Q10：有效 thinking 的跨协议记账表

本表用于不修改 IR 的原始语义投影；正常编辑后先按 §5 判断有效配置，再对实际输出记账。

| 输入配置 | 通用 effort 输出 | 模式/预算 loss |
| --- | --- | --- |
| adaptive，无显式 effort | 不合成默认 effort | 一条 `unmapped-field`，Field=`type` |
| adaptive + 显式 effort | 对应 effort | 一条 `unmapped-field`，Field=`type` |
| disabled | 不自动变成 `none` | 一条 `unmapped-field`，Field=`type` |
| enabled + budget，无显式 effort | 按既有预算表近似 | 一条 `degraded`，Field=`budget_tokens`；包含这组模式和预算的近似，不再另记 type |
| enabled + budget + 显式 effort | 显式 effort 优先 | type、budget_tokens 分别一条 `unmapped-field`；预算不是该 effort 的派生来源 |

目标不能表达的 display 再独立产生一条 `unmapped-field`，Field=`display`。原生完整恢复时不产生表中损失。未知 type 则不是上述已识别模式：Decode 产生一条定位到 type 的 `unmapped-value`，不递归重复报告整个已丢弃 thinking 中的子字段；独立的合法 `output_config.effort` 仍可映射。

### 6.2 Q11：未知枚举的最小完整丢弃单元

- 未知 cache TTL/type：丢弃该整个缓存断点，产生一条定位到未知 leaf 的 `unmapped-value`；不保留一个去掉 TTL 后采用默认时长的替代断点。
- 已知 context edit 中的 keep/trigger 等策略枚举不能解释：跳过该 edit，保留其余合法 edits，产生一条定位到未知 leaf 的 `unmapped-value`。若所有原有 edits 都被跳过，不再生成一个替代的空策略；不额外重复记父对象损失。
- 未知 edit 类型：整体跳过该 edit，产生一条 `unsupported-semantic`，不再逐个报告其成员。
- 未知 display：只删除独立的展示偏好并产生一条 `unmapped-value`，不连带删除有效模式和预算；不能声称原展示偏好仍已恢复。
- 整个最小完整单元被跳过后，不重复报告其中的其他属性。已识别对象中的普通未知字段继续遵守原字段审计规则，不能借 opaque 存储透传。

### 6.3 Q12：直接构造或反序列化的 IR

- 必需成员缺失、类型错误、已识别 payload 的格式错误：结构 error，不输出部分请求。
- 同一宿主重复提供同一种 singleton 已知配置：结构 error，不选 first/last。
- 结构合法，但 source/kind 组合未知：整项跳过，一条 `unsupported-semantic`，其他内容继续转换；不解析未知项的私有 payload。
- 通用 IR effort 与有效的原投影值不同：正常编辑，按 Q8 处理，不是 error。
- 投影绑定本身缺失必要结构、自相矛盾或损坏：结构 error；不能将正常的 current/baseline 差异误判为绑定损坏。

通用 IR 验证负责附加项的结构、singleton、绑定元数据及已知 payload 的 JSON 外形；只有来源 face 解释其配置语义。其他 spoke 不能为了验证或报损而解析 Anthropic wire 对象。恢复前来源 face 仍需校验其已知配置，不能绕过白名单合并任意原生字段。

## 7. 不做什么

- 不新增代理、HTTP client、认证、beta header 管理、路由、重试或模型能力数据库。
- 不新增直接 Anthropic→OpenAI 转换器，不使 spoke 导入其他 spoke。
- 不提供默认 loss 白名单，不把缓存损失视为可自动忽略。
- 不支持任意未知字段原样返回，不把整个原始请求保存在 IR。
- 不扩展 `output_config.format`、其他新增 thinking 模式、响应 context management 或 compaction。
- 不承诺字节级完整请求回放，不承诺缓存命中或模型接受参数。
- 不改变 sse 的 opaque 字节分帧职责，不修改响应事件语法以承载请求配置。

## 8. 验收条件

### 8.1 规范与向量

按规范 → golden vectors → 实现的顺序推进。更新 IR schema、loss 语义和相关三侧映射文档；更新必要的旧预算/缓存向量，并解释变化，不能只增加绕开旧行为的新测试。向量变更后重新生成 `vectors/manifest.json`。

最少覆盖：

1. adaptive 单独、adaptive + display、adaptive + 五档显式 effort。
2. disabled、enabled + 非离散原预算、enabled 与显式 effort 共存、未知 mode/effort/display。
3. 顶层、system、普通内容、工具、tool_result 及其合法嵌套内容的缓存标注，TTL 和 presence 差异。
4. 已知 context edits、keep all、混合已知/未知 edit、嵌套未知字段及顺序。
5. 原生保真、到 Responses、到 Chat 三条链路的最终 wire 与分阶段 losses。
6. effort 修改/清除、独立预算与显式 effort、无关附加配置不受影响。
7. IR JSON 往返后仍可恢复；五语言使用同样的结构和遗漏规则。
8. 未知整块只记一次 loss；不支持的 `output_config.format` 仍明确报损。
9. 旧四档无来源配置编码保持兼容；新两档有确定输出；`stream: true` 请求参数无特殊缺口。
10. 错误结构、重复/不合法附加配置、无关 source/kind 不得造成任意 wire 字段注入。

### 8.2 五语言与发布

五语言必须消费相同 golden vectors，并执行自身完整测试、格式、静态检查和必要构建。集成测试验证跨协议组合，而非只验证 face→IR 单侧结果。向量验证、manifest、依赖边界和原有回归必须通过。

实现与直接验证它的测试可在同一 commit；规范、向量、各语言实现、无关重构、版本协调分别保持可 cherry-pick 的边界。测试尚未执行通过前不得宣称 2.4.0 完成。

发布说明必须包含支持范围、真实不可映射损失、两阶段 loss 检查方式、IR 结构变化、旧默认保持与新档位行为，以及 beta header 属于调用方责任。版本更新、tag、包发布和远程操作均在明确授权后单独执行。

## 9. 留给实施者的事项

不需要再询问用户即可决定：公共字段/类型命名、内部文件拆分、共享 helper 布局、来源投影的最小存储形式、各语言惯用的数据结构、测试函数命名、稳定 Encode 遍历顺序和具体 work unit 大小。

这些选择必须满足本文可观察行为，不得将 API 命名自由解释成扩大支持范围或改变模式/effort 的语义。每个实施 work unit 开始前仍需定义输入、唯一工件、完成条件、机械验证、超时/取消及清理规则。

## 10. 依据与验证状态

仓库依据：`go/anthropic/messages/decode.go:134`、`go/ir/request.go:66`、`spec/01-intermediate-representation.md:177`、`spec/02-loss-policy.md`、`spec/12-mapping-anthropic-messages.md:196`、`spec/schema/ir.schema.json:394`。

外部依据：

- https://platform.claude.com/docs/en/build-with-claude/thinking
- https://platform.claude.com/docs/en/build-with-claude/effort
- https://platform.claude.com/docs/en/build-with-claude/prompt-caching
- https://platform.claude.com/docs/en/build-with-claude/context-editing
- https://github.com/openai/openai-python/blob/main/src/openai/types/shared/reasoning_effort.py
- https://github.com/openai/openai-python/blob/main/src/openai/types/shared/reasoning.py
- https://github.com/openai/openai-python/blob/main/src/openai/types/chat/completion_create_params.py

已核验：13 个 Go 合成复现、55 个源码/规范引文、五语言相关结构的静态核对、OpenAI 官方 SDK 的 effort 枚举。未做付费 API 调用，未运行五语言完整实现验收。后续读取官方 Anthropic cache 参数类型文件的一次请求超时，不能把该次请求当作新增字段位置证据；实施前的字段表必须完成官方形状核对。
