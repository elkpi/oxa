# v2.1.1 原始 JSON 保真与字段审计设计

## 目标

发布 v2.1.1，修复 Go、TypeScript、Rust、Python、C++ 与既有 Spec 2.1.0 的不一致：请求中的未映射字段可能未报告；Anthropic `tool_use.input` 在部分公开解码路径中会丢失原始 JSON 文本；Python 的 Anthropic 响应解码可能把一个 tool_use 的 input 关联给另一个 tool_use。

此工作不新增业务运行时模型。OXA 继续作为纯、进程内协议转换库；业务运行时拥有自己的状态、准入策略和 provider adapter。OXA IR 仅表达已有通用转换语义。Spec 版本维持 2.1.0，IR 文档版本维持 0.2.0：本工作落实已存在的 loss 和 INV-1 契约，不增加 IR schema 或 sealed union 变体。发行包统一升级至 2.1.1。

## 方案选择

选择新增原始 JSON 解码入口、在转换入口做结构化字段审计，并通过原始 JSON 节点位置保留 opaque 参数。保留现有 typed/object API 以兼容程序内构造的输入。

不选择只向 typed structs 增加 `previous_response_id` / `store` 字段：这无法解决任意扩展字段、深层字段及字段顺序差异。不把 unknown fields 放进 IR 或目标 wire，也不使用全局 provenance side table。不扩展 OXA IR 来表示 error event、server/custom tool 或 compaction。

## 公开输入接口

新增与既有返回形状兼容的 raw JSON 入口：

- Go：各 face 的 `DecodeRequestJSON`、`DecodeResponseJSON`；流 decoder 增加 `FeedJSON`。
- TypeScript：各 face 的 `decodeRequestJSON`、`decodeResponseJSON`；流 decoder 增加 `feedJSON`。实现复用 `parseJson` 的数值 token 和 source-text 能力，不使用 `JSON.parse` 替代。
- Rust：各 face 的 `decode_request_json`、`decode_response_json`；流 decoder 增加 `feed_json`。实现复用 `serde_json` raw-value 能力。
- Python：各 face 的 `decode_request_json`、`decode_response_json`；流 decoder 增加 `feed_json`。现有 dict 接口继续可用；raw-text 的参数定位由对象结构位置驱动。
- C++：增强现有 `std::string_view` request/response overload；各流 decoder 增加接收原始 JSON text 的 overload。

具体返回类型、配置参数和错误包装须与各语言现有 converter interface 一致，并由实现计划在改代码前逐项核实。raw JSON 入口是接收 provider JSON 的推荐入口。现有 typed/object 入口保留；来源信息已在调用前丢弃时，不承诺恢复任意未知字段或字节级参数文本。不得用 side table 把外部对象地址映射回输入文本。

所有 vector adapter 必须将原始 vector JSON 交给 raw JSON 入口，确保 golden test 覆盖公开输入边界，不再先转成 typed struct 后才测试 converter。

## 未映射字段审计语义

在 `spec/02-loss-policy.md` 中澄清既有规则，并由 vectors 固定行为：

- 每个已识别结构对象中的额外属性，按 JSON 键是否出现报告一条 `unmapped-field`；`path` 是源文档的 dot/bracket 路径，`field` 是键名。
- `null`、`false`、`0`、空字符串、空数组、空对象均表示属性已出现，不能静默跳过。未映射属性的值完全不作类型验证。
- 已知但 IR 无对应物的字段（如 Responses `previous_response_id`、`store`）同样记录 `unmapped-field`；目标 converter 不得透传。
- 审计递归只进入已知、保留、且规格化处理的结构位置。opaque payload 内部、schema 内容、tool input/arguments、以及随整个 unsupported item/block 一并丢弃的后代不再展开审计。
- 不重复生成 converter 已报告的相同 `(path, field, reason)` loss；多个独立 source construct 的重复 loss 不合并。
- malformed JSON 和已知字段类型/结构错误仍按已有规则返回 error，不能返回 partial IR。
- streaming 未知属性产生的 losses 只进入累计 `Losses()`（或等价 accessor），不得注入 IR event。
- 新增 loss 按 source encounter order 输出。raw JSON parser 为字段和已识别语义节点保留 source offset，转换结果与 inspector losses 合并时按 offset 稳定排序；同一 offset 保持 converter 原有相对顺序。无来源字节的 typed/object input 保持该入口原有的确定顺序。

每个 face 的结构字段 allowlist 由对应 mapping spec 和实现中的本地定义共同维护；TypeScript/Python/C++ 对象入口与 raw JSON 入口走同一审计逻辑。Go/Rust typed structs 无法暴露已被调用方丢弃的 unknown keys，故原始 JSON 是这两种语言满足字段审计契约的必需入口；typed API 不声称审计调用方在反序列化前丢弃的字段。不新增运行时 schema registry、外部字段数据库或自动 provider capability discovery。

## Opaque 参数的原始文本保真

对有源 JSON 的 Anthropic 请求、响应和流事件路径，`tool_use.input` 的 JSON object 节点须按结构位置提取原文并原样作为 IR raw JSON string token；保留键顺序、空白、转义形式和数字拼写。不得先转成通用数值对象再 marshal。

- Go：使用 `json.RawMessage`/等价节点来源跨越 request content 解码，避免 `json.Marshal`→`json.Unmarshal` 的通用对象往返。
- Rust：typed `Message`/`ContentValue` 解析不能先经过会归一化数字与 key order 的 `serde_json::Value` 才恢复原文；raw entrypoint 必须保留正确节点来源。
- Python：按 `messages[i].content[j].input`、递归 tool-result content、response `content[i].input` 等结构路径定位节点；不得用搜索 ID 字符串的方式关联 input 与 tool_use。
- C++：所有适用 raw JSON overload 都从正确 input 节点的 `raw_slice()` 获取字节；请求路径不能使用会 reserialize 的 default fallback。
- TypeScript：`parseJson`/`sourceTextOf` 的原文路径继续作为基线，新增 API 必须锁定它不会退化。

没有源文本的 typed/object 构造仍使用各语言现有 canonical representation，不对已经不存在的空格或数字拼写作承诺；这类 canonicalization 不产生 loss。

## 业务运行时职责

OXA 不新增运行时 error event、provider-hosted/server tool 生命周期、custom tool union、compaction event 或准入/路由策略。`MessageDone` 仅代表 OXA event stream 的语法终止，不能视作业务成功；`stop_reason: other` 和 loss 不能替代原生失败状态。

业务系统若需这些能力，应在 OXA 外维护业务拥有的 Runtime Model 和 provider adapter：先在原始输入上应用业务准入策略，再调用能表达目标语义的 adapter；仅把可无歧义转换的子集投影到 OXA IR。关键字段无法映射时，业务策略明确拒绝或选用专用 adapter，不根据 `losses == []` 推断安全、准入或语义完整。

## 测试与验收

### Shared golden vectors

至少新增以下 vectors，并更新 `vectors/manifest.json`：

1. Anthropic 请求顶层未知字段；Responses 请求 `previous_response_id`、`store:false`；覆盖字段 presence 而非 truthiness。
2. Anthropic 已知 message/block 的未知字段；Responses `reasoning` 额外字段；嵌套未知键的 null/false 值。
3. Opaque schema/tool input 不递归扫描；未知 whole item/block 只有一条 whole-unit loss。
4. Anthropic tool input 原文保真：超安全整数、指数数字、空白、转义和键顺序。
5. 两个或更多 tool_use 在请求、响应、tool-result 嵌套和流式 start 场景下，字段顺序变换仍正确对应 input。
6. 原生 unknown event 的 loss 单独保留，并验证其不会改变 stream decoder lifecycle state。

### 每语言单测

测试 raw JSON 入口与旧 typed/object 入口各自的保证；未知属性的值类别、source encounter ordering、opaque 节点、已知字段 structural error、whole-unit loss 去重及 IR 无 partial result。Python 的多 tool input 测试必须包含 `id` 在 `input` 后、相邻对象拥有不同参数。

### 发布门禁

所有五种语言全量测试、格式与 lint、vectors/manifest、consumer/package 和可靠性门禁按 `docs/release-checklist.md` 执行。实现 worker 测试通过只代表本语言单元完成，协调阶段仍需五语言全量验证和独立 review。

## 版本、README 与发布

GitHub 已确认 v2.1.0 于 2026-09-27 发布。README 和 spec README 的不实 `prepared`/`pending` 状态已在本分支先行纠正，独立提交 `7ef79af`。

实现包版本统一为 2.1.1；Spec 和 IR contract 版本不变。只有在分支实现、文档、vectors、五语种发布配置均验证后，才将 release notes 和 README 的 latest release 改为 v2.1.1。按 release checklist 合并到 main、等待 main CI 通过，再创建 GitHub `v2.1.1` release tag 与 Go `go/v2.1.1`、npm `ts/v2.1.1`、PyPI `py/v2.1.1`、crates.io `rust/v2.1.1` 子路径 tags；发布后分别查询 registry，未观测到成功不得声称已发布。C++ CMake project version 随 source release 更新为 2.1.1；仓库当前没有 C++ registry publisher，不声称将其发布到 npm/PyPI/crates.io。

## 工作边界

实现限于 Go、TypeScript、Rust、Python、C++ 的原始输入解析、unknown-field loss 和 Anthropic raw tool input 保真；spec 与 vectors 只记录这些当前协议转换规则。业务 Runtime Model 的实现、provider transport、请求上游取历史、自动 tool execution、策略准入、模型能力发现均不在本仓库范围。
