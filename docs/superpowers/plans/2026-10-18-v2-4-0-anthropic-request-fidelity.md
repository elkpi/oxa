# v2.4.0 Anthropic 请求配置保真 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> 本项目本次建议使用 `superpowers:executing-plans`：主会话逐单元实现，不默认启动多 worker；跨语言一致性由共享向量验收。选择执行方式并审阅计划之前不开始实现。本文的提交消息只是获授权后的提交边界，不授权现在或执行时自动 commit/push/tag/publish。

**Goal:** 五语言完整保留本版识别的 Anthropic 请求配置，支持经 IR 转为 Chat、Responses 及原生回转，并在真实损失边界准确报损。

**Architecture:** 通用 effort 明确建模，专属配置通过对象所属的受限 native 附加信息保存。中立的遗漏描述与投影绑定让目标 spoke 不解析 Anthropic wire JSON，也能准确报损和尊重 IR 编辑。仍只有 face→IR、IR→face；不新增网络层和响应流语义。

**Tech Stack:** Go 1.23/1.24；TypeScript 5.9、Node 20 与 Web Runtime；Rust workspace/serde；Python 3.10～3.13/unittest；C++20/CMake 3.20；既有 JSON Schema、veccheck 与共享 golden vectors。

**Spec:** `docs/superpowers/specs/2026-10-18-v2-4-0-anthropic-request-fidelity-design.md`。执行者必须同时读取该设计、`CLAUDE.md`、`spec/README.md`、`spec/01`、`spec/02`、`spec/10`、`spec/11`、`spec/12`、`spec/20` 对应文件。

## Global Constraints

- 目标版本为 **v2.4.0**；不拆分功能缩减的 v2.3.2，不改变 Go `/v2` module path。
- 按 **规范 → golden vectors → 实现** 推进；规范、向量、各语言实现和版本协调分开保持可 cherry-pick 的边界。
- 只新增请求侧能力，包含 `stream: true` 请求；不新增响应 `context_management.applied_edits` 或响应流字段。
- effort：`minimal/low/medium/high/xhigh/max`；本版不新增 `none` 映射，不将 `adaptive` 当作 effort。
- 旧无来源配置编码预算表：`minimal→1024`、`low→2048`、`medium→8192`、`high→16384`；预算 Decode：`≤2048→low`、`≤8192→medium`、其余 `high`。
- 原生 effort：`low/medium/high/xhigh/max`；原生 thinking：`enabled/disabled/adaptive`；display：`summarized/omitted/updates`。
- context edits：`clear_tool_uses_20250919`、`clear_thinking_20251015`；缓存类型 `ephemeral`，保留缺省 TTL 与 `5m/1h`。
- loss reason 仍为 `unmapped-field/unmapped-value/unsupported-semantic/degraded`；丢弃不是 degraded；两阶段 losses 均须检查。
- 未知字段不得透传；不解析和重序列化 tool input/partial_json，不遍历 JSON Schema 或其他既有 opaque 内容。
- 不增加直接 face-to-face 转换、跨 spoke import、模型能力数据库、HTTP、header、认证或重试；`sse` 不变。
- 按仓库既有可选 IR 扩展惯例保持 IR `specVersion=0.2.0`，继续读取旧 `0.1.0/0.2.0`；不批量重写历史向量版本。文档说明旧严格消费者不一定理解新附加信息/新枚举，不宣称源码或 C++ ABI 完全兼容。
- 响应和事件的既有 IR JSON 形状不扩展。请求内容块与响应共用内存类型时，必须用请求专用 codec/schema 上下文隔离 native 字段。

## Review Focus

1. IR 被复制或经过 JSON 序列化后，精确预算和原生位置仍能恢复；修改 effort 后旧备份不能覆盖新值。归属 Task 3/4/10 的 `NativePortableRoundTrip`、`NativeEditedEffortWins`。
2. 未知 TTL/keep/trigger 不能因删掉限制而变成默认缓存/清理策略。归属 Task 4 及四语言移植的 `NativeUnknownEnumDropsUnit`。
3. 单文本内容简写、块合并、工具重排、嵌套 tool_result 不能移动或删除缓存断点。归属 Task 4/9 的 `NativeCachePlacement`。
4. 共享 Block 类型不能让 native 附加信息进入响应或 IR 事件，也不能为了请求功能改变流事件语法。归属 Task 3 及移植的 `NativeRequestOnly`。
5. 手工 IR 的重复 singleton、损坏绑定、未知 source/kind 不得注入任意 wire；合法 IR 编辑不能误报结构错误。归属 Task 3/10 的 `NativeIRValidation`。

---

## 实施前固定的接口与数据形状

这些名称是实施者根据设计 §9 作出的技术选择；不得在后续任务另起一套等价名称。

### IR JSON

新增可选 `native: NativeAnnotation[]`，空集合省略。宿主为 Request、SystemBlock、Tool，以及已有 request block 中能映射到合法 Anthropic 缓存位置的变体。Message 和 Params 不新增 native；thinking 配置放 Request 上。

```text
NativeAnnotation = {
  source: string,
  kind: string,
  value: string,
  omission?: { field: string, reason: LossReason, detail?: string },
  projection?: { origin: "budget" | "explicit", effort: ReasoningEffort }
}
```

- 本版已知 source 为 `anthropic`。已知 kind：`thinking_mode`、`thinking_budget`、`thinking_display`、`output_effort`、`cache_control`、`context_management`。
- source/kind 使用非空字符串而不是只接受本版值的封闭 JSON enum，使合法的未知扩展可以按 Q12 报 loss。value 是 opaque JSON 文本字符串，不是 provider 对象或完整原始请求。
- 对已知 kind，value 的 JSON 外形分别为字符串、整数、字符串、字符串、对象、对象。通用 IR 只做外形和绑定结构检查；策略字段和值由 Anthropic face 解释。
- 同一宿主的已知 `(source,kind)` 只能一份。thinking/output_effort 只允许在 Request；cache_control 只允许在规范固定的合法宿主；context_management 只允许在 Request。
- projection 仅用于 `thinking_budget` 的预算派生 effort，或 `output_effort` 的显式 effort。显式 effort 存在时，budget 不携带 budget 投影。正常 current/baseline 不同是编辑；同一宿主同时宣称两种 effort 来源或不合法绑定结构是 error。
- omission 是来源 decoder 生成的中立描述，不含原 wire Path；目标 encoder 按当前 IR 的实际位置生成 Path。已知种类的 reason/field 组合由 IR 校验，不能通过手工清空描述绕过必需报损。
- thinking_mode 一般携带 `field=type/reason=unmapped-field`。有效预算投影的 `degraded/budget_tokens` 覆盖该组模式近似，遗漏 helper 在此条件下抑制 mode 的重复记账；不靠删除 mode 配置实现去重。
- 失效的预算投影不继续报“旧预算近似”；新通用值仍输出，mode 不再享受旧预算的去重覆盖。原生重建成本按设计 §5 报损。
- output_effort 没有遗漏损失：通用 effort 已承载语义；该项只保存原生位置/显式存在性和原投影。已知 cache/display/context 的遗漏 reason/field 按设计 §6 固定。
- 不解析未知 source/kind 的私有 value；整项遗漏一条 `unsupported-semantic`。未知 source 定位到 `native[i].source`、Field=`source`；source 已知而 kind 未知则定位到 `native[i].kind`、Field=`kind`；两者均未知只报前者。不能以其中的自报 omission 决定是否记录未知扩展损失。
- Encode 遍历固定为 Request.native、System 顺序、Messages/Content 深度优先（先宿主后其 native 下标，再子内容）、Tools 顺序；不用 map 迭代顺序。Path 为当前宿主下的 `native[i].value`，Field 仍是原生语义字段标识。

请求专用 schema `$defs/requestBlock` 递归允许上述 request native 属性；现有用于 response/event 的 `$defs/block` 保持不变。Go 公共 `MarshalBlock/UnmarshalBlock` 不因此扩大响应/事件语法；Request codec 使用私有 request-block 路径。其他语言也需要等价的上下文分离。

### 中立 helper 接口

| 语言 | 新类型与 helper，保留既有 face API 签名 |
| --- | --- |
| Go | `ir.NativeAnnotation/NativeOmission/NativeProjection`；宿主 `Native []NativeAnnotation`；`ValidateNativeRequest(req *Request) error`；`OmittedNativeRequestLosses(req *Request, target string) ([]Loss,error)` |
| TS | 同名 readonly interfaces；宿主 `native?: readonly NativeAnnotation[]`；`validateNativeRequest(request: Request): void`；`omittedNativeRequestLosses(request: Request,target: string): readonly Loss[]` |
| Rust | 同名 structs，宿主 `native: Vec<NativeAnnotation>`（serde default/skip empty）；`validate_native_request(&Request)->Result<(),Error>`；`omitted_native_request_losses(&Request,&str)->Result<Vec<Loss>,Error>` |
| Python | 同名 frozen/slots dataclasses，宿主 `native: tuple[NativeAnnotation,...]=()`；`validate_native_request(req: Request)->None`；`omitted_native_request_losses(req: Request,target: str)->list[Loss]` |
| C++ | 同名 structs，宿主 `std::vector<NativeAnnotation> native`；`Status validate_native_request(const Request&)`；`StatusOr<std::vector<Loss>> omitted_native_request_losses(const Request&,std::string_view)` |

target 只由三个既有 spoke 使用固定值 `anthropic/chatcompletions/responses` 调用。helper 仅读中立元数据、投影和当前通用值，不解析 Anthropic 对象、不修改入参；Anthropic face 恢复前自行检查已知 value。非法元数据返回各语言既有错误机制，不另建错误框架。

## 文件责任图

- 规范：`spec/01-intermediate-representation.md`、`spec/02-loss-policy.md`、三个 mapping 文档、`spec/README.md`、`spec/90-glossary.md`、新 `spec/15-native-request-options.md`、`spec/schema/ir.schema.json`。
- 向量：`vectors/anthropic/nonstream/`、两个 OpenAI nonstream 目录、`vectors/cross/nonstream/`、`vectors/manifest.json`。
- Go：IR 类型/codec + 新 `go/ir/native.go`；三个 request spoke；Anthropic 新 `request_native.go`；对应单测及 cross runner 接入。
- TS：IR 类型/非流 codec + 新 `ts/src/ir/native.ts`；Anthropic 新 `native.ts` 和既有 audit/nonstream；两个 OpenAI nonstream；`ts/src/generated/ir-schema.ts` 由生成器刷新。
- Rust：`oxa-ir` 类型/codec + 新 `native.rs`；三个 spoke 请求转换；`oxa-vectest` 的覆盖计数与原生 pair 接入。
- Python：IR 类型/codec + 新 `native.py`；Anthropic 新 `native.py`、既有 `_wire_fields.py`；两个 OpenAI 请求转换与测试。
- C++：IR 类型/codec + 新 `cpp/src/native.cpp`；Anthropic 新 `messages_native.hpp`；既有 request spoke/audit；CMake 注册新翻译单元；测试保留无 exceptions 路径。
- 集成与发布：既有 consumer、vectest、README/CHANGELOG/版本文件；不修改发布 workflow，除非验证证据证明本功能确实需要。

## 工作单元协议与依赖

每个 Task 是独立可审查单元。执行开始建立仓库外 `<run-dir>/manifest.json`，固定 Task ID、输入文件、预期代码/测试工件、状态、开始时间、截止时间和校验命令；每项校验结果先写独占 `.partial` 再原子发布。只能消费 verified 工件，不能以 worker 回复代替测试结果。

默认顺序：`0→1→2→3→4→5→6→7→8→9→10→11→12`。没有默认并行；改变为并行时先固定非重叠语言分块和唯一输出，worker 只交付自己的 patch/报告，专门汇总单元消费已校验产物，不能共同写 manifest 或最终向量。

每个单元的输出是下述指定代码变更及 `<run-dir>/<id>.json` 校验记录。未列文件只读；需要扩大写范围先更新单元边界，不能顺手重构。每条测试/构建命令最多 10 分钟；下表是单元总上限。超时、无进展、输入 HEAD 意外变化、验证失败且无法在剩余时限定位时，停止并记录原因；不得无限等待或用部分输出冒充通过。

临时目录、worker、worktree 由 Task 0 登记。仅删除任务创建且已检查的资源，不清理用户已有内容，不把缓存/测试输出提交 Git；保留工作分支与最终必要审计结果，清理其他任务临时资源。提交动作一律需要单独授权，未授权时以审阅 diff/建议提交边界结束。

---

### Task 0：执行环境与基线（上限 30 分钟）

**Files:** 只读 `.gitignore`、`CLAUDE.md`、Makefile、各语言 manifest 和本计划；不改实现。

**Interfaces:** 消费当前仓库 HEAD；产出 `<run-dir>/manifest.json`、`00-preflight.json`，包含工具版本、初始脏文件和基线测试状态。

- [ ] **Step 1：隔离执行环境。** 执行时先使用 `superpowers:using-git-worktrees`；保留已有两份设计/计划文档，不能用 reset/clean 丢弃它们。未提交的文档不会自动进入新 worktree：确认目标不存在后复制两份文档，并核对内容哈希一致。记录初始 HEAD 和全部已有改动。
- [ ] **Step 2：运行基线。** 根目录 `make vectors && make check-modulepath`；`go/` 下 `go test -count=1 ./...`；其余完整命令见 Task 12。记录真实失败，缺少工具时记录阻塞，不声称完整环境已通过。
- [ ] **Step 3：固定资源与任务 manifest。** 后续 shell 调用显式传工作目录和 run-dir，不依赖跨调用环境变量。所有发布/网络模型调用关闭；禁止自动安装系统工具或替用户登录。
- [ ] **Step 4：验证。** `git status --short` 与初始清单比较；输出 valid JSON 的 preflight 记录。完成条件是输入稳定、工具/失败状态可复查，不要求掩盖既有失败。
- [ ] **Step 5：提交边界。** 本单元无产品 commit；只登记执行元数据。

### Task 1：先冻结规范和 schema（上限 60 分钟）

**Files:** Modify 上述规范文件及 `spec/schema/ir.schema.json`；Create `spec/15-native-request-options.md`。不改向量或语言实现。

**Interfaces:** 消费设计 Q1～Q12；产出本文固定的 native JSON 形状、kind/宿主白名单、遗漏/编辑/无效 IR 表；IR contract 仍为 `0.2.0`，Spec 目标 `2.4.0`。

- [ ] **Step 1：核对并固化官方请求形状。** 在 spec/15 固化来源链接与闭合字段表：cache `{type:"ephemeral",ttl?:"5m"|"1h"}`；tool clearing 的 `trigger/clear_at_least={type:"input_tokens",value:整数}`、`keep={type:"tool_uses",value:整数}`、`exclude_tools=string[]`、`clear_tool_inputs=boolean`；thinking clearing 的 `keep="all"|{type:"thinking_turns",value:整数}`。只按官方定义确认这些已有子集和合法宿主，不扩大支持到其他参数/模型能力。未证实的位置不制造“合法 provider 请求”向量，也不调用模型验证；若主要来源无法读取，保留该项证据缺口，不伪造字段依据。
- [ ] **Step 2：先写结构验收用例。** 用以下断言描述 schema 期望；旧 shape 可读，request native 可读，response/event native 不可读，未知 source/kind 的基本 shape 可读：

```text
validate(request_with_native_cache) == valid
validate(request_with_native_source_future) == valid
validate(response_with_native_text_block) == invalid
validate(event_with_native_text_block) == invalid
```

- [ ] **Step 3：更新 schema 与映射表。** 采用 requestBlock 专用递归定义，不使用 TS 生成器不支持的 schema 组合技巧。新增 effort 两值，保持 loss schema 四类 reason；把旧缓存/预算“Decode 必然报损”改成按真实阶段报损，并写清 singleton/投影关系。
- [ ] **Step 4：验证。** 解析全部 schema JSON，检查 `$ref` 可定位；根目录 `make vectors` 验证历史向量仍结构合法。此阶段其他语言新枚举/生成代码尚未更新，不能把全仓 constants/check 的暂时 RED 隐瞒成 PASS。
- [ ] **Step 5：建议提交。** `docs(spec): 定义受限原生请求配置与分阶段损失`；只包含规范/schema，不夹带向量或实现。

### Task 2：golden vectors 与 manifest（上限 90 分钟）

**Files:** Modify/Create 上述向量目录与 `vectors/manifest.json`；不修改语言实现来迎合 expected。

**Interfaces:** 消费 Task 1 的 JSON/行为契约；产出 request-to-ir、ir-to-request、protocol-to-protocol 的 expected IR/wire/losses；expected 字段遵守既有 vector.schema。

- [ ] **Step 1：固定四个非重叠子块。** `02-thinking`、`02-cache`、`02-context`、`02-cross`；每块最多 16 个文件，超过则在开始前按稳定文件名再分块。逐文件列到 manifest；不手写 manifest 哈希。
- [ ] **Step 2：更新旧行为向量。** 修改 `thinking-budget-request-to-ir.json` 的精确配置保留和 Decode loss；将已不再 Decode 丢失的 `loss-cache-control-request-to-ir.json` 重命名为 `cache-control-request-to-ir.json`，同步 name；保留未知整块仅一条 loss 的回归。
- [ ] **Step 3：新增最少向量族。** 文件前缀固定 `native-thinking-*`、`native-cache-*`、`native-context-*`；覆盖五档 effort、budget=6000、budget+显式 high、disabled、unknown mode/display/ttl/keep、各缓存宿主与合法嵌套、两类 edits/keep all/混合未知 edit、format 仍丢弃。跨侧新增 `anthropic-to-{anthropic,chatcompletions,responses}-native-request.json`，以及预算派生与显式 effort 共存的三侧例。
- [ ] **Step 4：锁定精确断言。** `adaptive+effort=xhigh` 输出 xhigh；OpenAI 侧 mode 恰有一条 `unmapped-field/type`；预算单独派生恰有一条 `degraded/budget_tokens`；预算+显式 effort 恰有 type、budget_tokens 两条 unmapped-field；display 另加一条；原生完整例 losses 为空。
- [ ] **Step 5：验证并发布。** `go/` 下 `go run ./cmd/veccheck -root .. -write-manifest`，再 `go run ./cmd/veccheck -root .. -check-manifest`。记录新/改/删文件数和 manifest 总数。运行 face/cross 测试确认当前实现因新契约 RED，而不是 JSON 错误；不得改 expected 让旧实现假绿。建议独立提交 `test(vectors): 覆盖原生保留和准确跨协议损失`。

### Task 3：Go IR、codec 与中立 helper（上限 90 分钟）

**Files:** Modify `go/ir/{request.go,block.go,custom.go,apply_patch.go,json.go,json_test.go}`；Create `go/ir/native.go`、`go/ir/native_test.go`。

**Interfaces:** 实现“中立 helper 接口”表的 Go 签名；新增 effort 常量 `ReasoningEffortXHigh/ReasoningEffortMax`；Request/合法宿主的 `Native` 字段空值不改变既有 JSON。

- [ ] **Step 1：先写失败测试。** 新 `TestNativePortableRoundTrip`、`TestNativeIRValidation`、`TestNativeRequestOnly`、`TestNativeOmissionDoesNotMutateRequest`、`TestNativeProjectionOmissions`。核心断言：

```text
UnmarshalRequest(MarshalRequest(req)).Native[0].Value == original_value
ValidateNativeRequest(duplicate_known_kind) -> error
OmittedNativeRequestLosses(unknown_source_kind, "responses") -> one unsupported-semantic
OmittedNativeRequestLosses(valid_budget_projection, "responses") -> one degraded/budget_tokens
OmittedNativeRequestLosses(edited_budget_projection, "responses") -> no stale budget degraded
MarshalResponse(response_with_native_block) -> error
```

- [ ] **Step 2：运行 RED。** `go test -count=1 ./ir -run 'TestNative'`，确认缺类型/行为，而非已有无关失败。
- [ ] **Step 3：最小实现。** native.go 仅处理中立元数据、投影、固定顺序遍历；不导入任何 face。codec 区分 request-only block 路径，公共 response/event 路径拒绝 native；重复 singleton、损坏 binding 用 error，当前 effort 与 baseline 不同不报错。tool input、签名与 schema 字节不被新 walker 访问或重写。
- [ ] **Step 4：运行 GREEN。** 上述测试，再 `go test -count=1 ./ir ./internal/deps`。旧无 native 的 canonical JSON 应字节/结构保持原有测试期望；Decode/Encode 不修改传入请求。
- [ ] **Step 5：建议提交。** `feat(go/ir): 增加请求原生配置与中立遗漏检查`，实现和直接单测同 commit。

### Task 4：Go Anthropic 请求解码与原生恢复（上限 90 分钟）

**Files:** Modify `go/anthropic/messages/{types.go,decode.go,encode.go,rawjson.go,custom_filter.go,wire_test.go}`；Create `go/anthropic/messages/request_native.go`、`go/anthropic/messages/request_native_test.go`；不得更改 stream 语义。

**Interfaces:** 既有 `DecodeRequestJSON`、typed `DecodeRequest`、`EncodeRequest` 的签名不变；在 request_native.go 新增私有 `decodeRequestNative(wire *Request, req *ir.Request) ([]ir.Loss,error)`、`encodeRequestNative(req *ir.Request, wire *Request) ([]ir.Loss,error)`。前者追加到新建 IR，后者只修改新建 wire，不修改输入 IR；error 时公共入口不返回部分输出。不对外暴露 provider 类型到 IR。

- [ ] **Step 1：先写失败测试。** `TestNativeThinkingCombinations`、`TestNativeEditedEffortWins`、`TestNativeUnknownEnumDropsUnit`、`TestNativeCachePlacement`、`TestNativeContextEdits`、`TestNativeFormatStillAudited`；同时测试 JSON 和 typed 可见字段的入口。

```text
Decode(adaptive + effort=xhigh).Params.ReasoningEffort == "xhigh"
Encode(Decode(enabled,budget=6000)).thinking.budget_tokens == 6000
Encode(edit_effort(Decode(adaptive,xhigh),"low")).output_config.effort == "low"
Encode(Decode(cache.ttl="future")).cache_control is absent
Encode(Decode(context.keep=unknown)).context_management.edits contains only valid edits
Decode(output_config={effort:"high",format:{...}}).losses includes format once
```

- [ ] **Step 2：运行 RED。** `go test -count=1 ./anthropic/messages -run 'TestNative'`，记录各类缺口。
- [ ] **Step 3：实现。** 原审计白名单识别本版成员，但未知成员仍审计；source encounter order 与语义损失正确合并。known value 存入所属 native 附加项，预算/显式 effort 来源和 omission 按 Q10 固定。native 恢复按 Q8，失效预算不能复活；新的 xhigh/max 使用 adaptive+effort，不按模型名分支。缓存存在时禁止导致断点丢失的字符串简写；嵌套缓存递归绑定，别把 tool input 当嵌套 block 扫描。
- [ ] **Step 4：运行 GREEN。** 上述测试及 `go test -count=1 ./anthropic/messages`。非法 source/kind、重复项不能经原生合并制造 model/tools 等未授权字段；响应流原回归保持不变。
- [ ] **Step 5：建议提交。** `feat(go/anthropic): 保留请求配置并恢复有效原生语义`。

### Task 5：Go 两个 OpenAI 请求侧与三条链路（上限 60 分钟）

**Files:** Modify `go/openai/chatcompletions/{decode.go,encode.go,wire_test.go}`、`go/openai/responses/{decode.go,encode.go,wire_test.go}`、`go/internal/vectest/cross_test.go`；Create `go/internal/vectest/native_request_test.go`。

**Interfaces:** 复用 `ir.OmittedNativeRequestLosses`；OpenAI 不解析任何 native value。既有请求签名不变，effort xhigh/max 成为明确支持值。

- [ ] **Step 1：先写失败测试。** `TestNativeRequestThreeRoutes`、`TestNativeDecodeEncodeLossStages`、`TestNativeEffortNewValues`。对同一 req 先编码 Chat/Responses，再编码 Anthropic，确认原生配置仍在。
- [ ] **Step 2：运行 RED。** `go test -count=1 ./internal/vectest -run Native`；确认原生 pair 的覆盖缺失会导致测试失败，不能 silently skip。
- [ ] **Step 3：实现。** 两个 encoder 在有效 IR 上追加 helper 的遗漏 losses；保留已有 unrelated losses。两个 decoder 的 effort 白名单新增 xhigh/max。Go cross 集成测试显式注册 `anthropic→anthropic`；三条链路只组合既有 spoke。
- [ ] **Step 4：运行 GREEN。** `go test -count=1 ./openai/... ./internal/vectest ./internal/deps`，再 `go test -count=1 ./...`；原始 JSON 的 Decode loss 顺序与 Encode 固定 IR 顺序分别有精确列表断言，不只依赖 unordered vector 比较。
- [ ] **Step 5：建议提交。** `feat(go/openai): 映射扩展 effort 并报告原生配置遗漏`。

### Task 6：TypeScript 完整垂直移植（上限 90 分钟）

**Files:** Modify `ts/src/ir/{types.ts,nonstream.ts,index.ts}`、`ts/src/anthropic/messages/{audit.ts,nonstream.ts,types.ts}`、`ts/src/openai/chatcompletions/nonstream.ts`、`ts/src/openai/responses/nonstream.ts`、`ts/test/nonstream-vector-integration.test.ts`；Create `ts/src/ir/native.ts`、`ts/src/anthropic/messages/native.ts`、`ts/test/native-request.test.ts`；Generate `ts/src/generated/ir-schema.ts`，不手改生成文件。

**Interfaces:** 实现 TS 类型/helper 表；face `decodeRequestJSON/encodeRequest` 保持 `ConversionResult<Request>` 等原签名；IR `encodeRequest/decodeRequest` 保留附加项。

- [ ] **Step 1：先写失败测试。** native-request.test.ts 移植 Task 3～5 命名和断言，保留 bigint/readonly 习惯：`assert.equal(decoded.value.params?.reasoning_effort,"xhigh")`；非法重复调用 `assert.throws`；未知 kind 恰一条 unsupported-semantic。
- [ ] **Step 2：运行 RED。** `npm run build:test` 后 `node --test dist-test/test/native-request.test.js`；公共类型缺口可以先产生编译失败。
- [ ] **Step 3：实现。** schema 类型 `npm run generate`；request-only codec/context guard、中立 helper、Anthropic 字段审计与恢复、两个 OpenAI omission 接入。Node 与 Web 共用实现，不引入 Node-only 运行时依赖。
- [ ] **Step 4：运行 GREEN。** `npm run generate:check && npm run check && npm run test:architecture && npm run test:node && npm run test:web && npm run fmt`。所有新增向量必须被执行，原生 pair 注册并纳入覆盖检查；不能只改硬编码总数绕过遗漏。
- [ ] **Step 5：建议提交。** `feat(ts): 实现请求原生保真与跨协议损失`。

### Task 7：Rust 完整垂直移植（上限 90 分钟）

**Files:** Modify `rust/crates/oxa-ir/src/{request.rs,codec.rs,lib.rs}`；Create `rust/crates/oxa-ir/src/native.rs`、`rust/crates/oxa-ir/tests/native.rs`；Modify `rust/crates/oxa-anthropic/src/{types.rs,audit.rs,decode.rs,encode.rs,normalize.rs,lib.rs}`，Create `rust/crates/oxa-anthropic/src/native.rs`；Modify `rust/crates/oxa-chatcompletions/src/{decode.rs,encode.rs}`、`rust/crates/oxa-responses/src/{decode.rs,encode.rs}`、`rust/crates/oxa-vectest/tests/cross_vectors.rs`；Create `rust/crates/oxa-anthropic/tests/native_request.rs`。

**Interfaces:** 实现 Rust 类型/helper 表；`ReasoningEffort::XHigh/Max`；既有 `decode_request/encode_request` 返回 Result 的签名不变。每个追加 Vec 使用 serde default/skip empty。

- [ ] **Step 1：先写失败测试。** 三 route、unknown TTL/keep、编辑优先、native request-only 和重复 kind；`assert_eq!(request.params.as_ref().unwrap().reasoning_effort,Some(ReasoningEffort::XHigh))`；`validate_native_request(&duplicate).is_err()`。
- [ ] **Step 2：运行 RED。** `cargo test -p oxa-ir --test native` 与 `cargo test -p oxa-anthropic --test native_request`。枚举穷尽匹配编译失败要逐个明确处理，不能用通配分支悄悄映射成 high。
- [ ] **Step 3：实现。** 三 crate 仅经 oxa-ir helper；native JSON/RawValue 和 serde 流程不得改变原始 tool input。补全 repo 中公开结构构造器；cross tests 注册原生 pair，覆盖计数由真实 manifest/已枚举 ID 校验，不仅替换数字。
- [ ] **Step 4：运行 GREEN。** `cargo test --workspace && cargo fmt --all -- --check && cargo clippy --workspace --all-targets -- -D warnings`。单独核对旧缺省 IR JSON 与两版本读取测试。
- [ ] **Step 5：建议提交。** `feat(rust): 实现请求配置保留与投影损失`；记录消费者新增 struct 字段、枚举 match 迁移要求。

### Task 8：Python 完整垂直移植（上限 90 分钟）

**Files:** Modify `python/src/oxa/ir/{types.py,codec.py,__init__.py}`、`python/src/oxa/_wire_fields.py`、`python/src/oxa/anthropic/messages/{decode.py,encode.py,normalize.py}`、`python/src/oxa/openai/chatcompletions/{decode.py,encode.py}`、`python/src/oxa/openai/responses/{decode.py,encode.py}`、`python/tests/test_cross_vectors.py`；Create `python/src/oxa/ir/native.py`、`python/src/oxa/anthropic/messages/native.py`、`python/tests/test_native_request.py`。

**Interfaces:** 实现 Python 类型/helper 表；沿用 tuple/frozen/slots 和既有 `decode_request_json(raw_text,table=None)->tuple[Request,list[Loss]]`、IR `dump_request/load_request`，不另建 dict-only 隐藏通道。

- [ ] **Step 1：先写失败测试。** `NativeRequestTest` 覆盖所有 Review Focus；`self.assertEqual(req.params.reasoning_effort,"xhigh")`；使用 `dataclasses.replace` 编辑 effort 后 native 输出 low；重复 singleton `assertRaises`，未知 source 恰一条 loss。
- [ ] **Step 2：运行 RED。** `PYTHONPATH=src python3 -m unittest discover -s tests -p 'test_native_request.py'`。
- [ ] **Step 3：实现。** 保留精确 opaque 文本和 presence；来源配置审计只遍历已知对象，不触及工具参数/schema。保持 typed 与完整 JSON 入口的审计责任区别。替换 cross tests 的固定 22 计数时，同时校验 manifest 预期 ID 与实际执行 ID 一致。
- [ ] **Step 4：运行 GREEN。** `PYTHONPATH=src python3 -m unittest discover -s tests`，再按环境执行 `ruff check src tests`、`mypy`；若工具不具备只记录该项未运行，不自动安装系统依赖。CI 继续覆盖 Python 3.10～3.13。
- [ ] **Step 5：建议提交。** `feat(python): 增加受限原生配置和准确报损`。

### Task 9：C++ 完整垂直移植（上限 90 分钟）

**Files:** Modify `cpp/include/oxa/ir.hpp`、`cpp/src/ir.cpp`、`cpp/src/anthropic/{messages.cpp,messages_audit.hpp}`、`cpp/src/openai/chatcompletions.cpp`、`cpp/src/openai/responses.cpp`、`cpp/CMakeLists.txt`；Create `cpp/src/native.cpp`、`cpp/src/anthropic/messages_native.hpp`、`cpp/tests/test_native_request.cpp`；Modify `cpp/tests/test_vectors.cpp`。

**Interfaces:** 实现 C++ 类型/helper 表；继续使用 `Status/StatusOr<Conversion<...>>`，不新增 throw 依赖。新增翻译单元和 test target 必须注册到 CMake，不允许测试文件存在但未被构建。

- [ ] **Step 1：先写失败测试。** 同等 assertions：budget=6000 原生恢复、xhigh 精确输出、编辑 low 优先、未知 TTL 不生成五分钟替代缓存、损坏/重复返回非 OK Status、unknown source 返回恰一条 loss。
- [ ] **Step 2：运行 RED。** CMake 注册测试后，`cmake -B cpp/build -S cpp -DCMAKE_BUILD_TYPE=Debug`；`cmake --build cpp/build --parallel 2`；`ctest --test-dir cpp/build -R native_request --output-on-failure`。
- [ ] **Step 3：实现。** 新 helper 使用 typed IR 和中立元数据，保持 input/schema 原始文本；block 复制、单文本简写与工具分组不可丢 native。不得依靠对象地址、静态表或 exceptions 恢复来源。
- [ ] **Step 4：运行 GREEN。** `ctest --test-dir cpp/build --output-on-failure`。再使用任务独占的外部 build 目录配置 `-DCMAKE_CXX_FLAGS=-fno-exceptions` 并执行完整构建/ctest；记录 C++ 对外布局变化，需要消费者重新构建。
- [ ] **Step 5：建议提交。** `feat(cpp): 实现请求原生保真与安全遗漏处理`。

### Task 10：三链路、IR JSON 可携带性与消费者回归（上限 60 分钟）

**Files:** Modify 各语言新增 native 测试、`go/internal/vectest/native_request_test.go`、既有各语言 cross-vector 测试；Modify `ci/consumers/go/main.go`、`ci/consumers/python/smoke.py`、`ci/consumers/rust/src/main.rs`、`ci/consumers/cpp/main.cpp`；不改生产转换逻辑来隐藏失败。

**Interfaces:** 消费 Task 2 的同一份 golden native IR 文档和所有语言的 codec/face API；产出 manifest 向量 ID 覆盖与 consumer 结果。不要新增仅某语言识别的 side metadata。

- [ ] **Step 1：先写携带性测试。** 每种语言读取相同 expected IR，经各自 codec 序列化/读回，再走三个 encoder；断言 supported native 例原生恢复、OpenAI 精确 effort 与 loss 表一致。再先 encode 两个 OpenAI、后 encode Anthropic，确认前两次没有修改原请求。
- [ ] **Step 2：写共享类型负例。** 带 native 的 response/event block 必须按上下文拒绝；普通响应和所有旧 stream vectors 仍完全通过。新增功能不得绕过 INV-1、INV-5/INV-6 或 sse 边界。
- [ ] **Step 3：写 consumer 断言。** 在既有包导出中使用新类型，展示 Rust 新字段/穷尽 match 的正确构造；所有 source/kind 未知项无 wire 注入。跨测试覆盖表与 manifest ID 集合相等，原生 pair 不可跳过。
- [ ] **Step 4：运行 GREEN。** 五语言对应 native/cross suite；根目录 `OXA_ROOT="$PWD" bash ci/consumers/run.sh` 与 `OXA_ROOT="$PWD" bash ci/reliability/run.sh`。如果环境限制导致某 consumer 无法运行，记录具体平台和缺口，不能称消费者已全通过。
- [ ] **Step 5：建议提交。** `test(integration): 验证三链路和跨语言 IR 携带性`，仅实现验证，不混入重构。

### Task 11：迁移说明与协调版本准备（上限 45 分钟）

**Files:** Modify `README.md`、`CHANGELOG.md`、`spec/CHANGELOG.md`、`spec/README.md`、`docs/release-checklist.md`、`ts/package.json`、`ts/package-lock.json`、`python/pyproject.toml`、`rust/Cargo.toml`、`rust/Cargo.lock`、`cpp/CMakeLists.txt`；Go module path 保持原样。

**Interfaces:** 消费完整功能测试结果；产出一致的 2.4.0 版本声明和使用方迁移说明。只做本地准备，不生成 tag/发布动作。

- [ ] **Step 1：先写发布断言并确认 RED。** 在 run-dir 的独占检查脚本读取 JS/Python/Rust/C++ 四个 manifest，断言版本均为 `2.4.0`；Rust 内部依赖与 lockfile、TS lockfile 根版本一致；Spec 2.4.0、IR contract 0.2.0 不混淆。当前 2.3.1 声明必须使检查失败。
- [ ] **Step 2：写用户迁移例。** 两阶段 loss 检查、cache/context 不同过滤策略、原生回转不是任意字段透传；显示 `type` 模式 loss 不因 effort 成功而消失；updates/context beta header 由调用方处理。说明 Rust 枚举 match/结构字面量、新 IR schema、C++ 重新构建、旧四档默认保持、新两档 modern 输出，以及 loss 阶段/路径/数量变化。
- [ ] **Step 3：更新协调版本。** TS 使用 `npm version 2.4.0 --no-git-tag-version --ignore-scripts` 更新 package/lock；Python project、Rust workspace/内部 crate 约束、CMake project 更新为 2.4.0。Rust 用可用的 cargo 离线流程刷新 lockfile，不手工捏造依赖校验值。CHANGELOG 和 Spec 标注目标版本，但不得把未发布状态写成已发布。
- [ ] **Step 4：验证 GREEN。** 重跑版本检查脚本；`python3 scripts/check-constants.py`、`make check-modulepath`，`ts/` 下 `npm run release:check`。检查 release checklist 的未通过项仍保持未勾选，不把 Go 通过等同于五语言完成。
- [ ] **Step 5：建议提交分开。** 文档 `docs: 说明 v2.4.0 配置保真与迁移要求`；版本协调另一个 `chore(release): 协调全包版本至 v2.4.0`。未经授权不执行提交、tag 或发布。

### Task 12：最终验收与清理（上限 60 分钟）

**Files:** 本单元只读，必要修复退回所属 Task 并重新验证；不在验收单元做无关改造。

**Interfaces:** 消费全部 verified 单元及稳定工作树；产出 `<run-dir>/12-acceptance.json`，逐项区分 passed/failed/not-run，不以自然语言“应该通过”代替输出。

- [ ] **Step 1：根目录门禁。** `make vectors && make check-modulepath && python3 scripts/check-constants.py`；`go/` 下 `go run ./cmd/veccheck -root .. -check-manifest`。
- [ ] **Step 2：五语言完整验证。** 工作目录按下表；所有 exit code、时长和向量覆盖记录到单独工件。

| 工作目录 | 命令 |
| --- | --- |
| `go/` | `go test -count=1 -race ./...`；`go build ./...`；`go vet ./...`；`gofmt -l .` 应无输出 |
| `ts/` | `npm run generate:check`；`npm run fmt`；`npm run check`；`npm run test:architecture`；`npm run test:node`；`npm run test:web`；`npm run test:package`；`npm run test:consumer` |
| `rust/` | `cargo fmt --all -- --check`；`cargo clippy --workspace --all-targets -- -D warnings`；`cargo test --workspace` |
| `python/` | `PYTHONPATH=src python3 -m unittest discover -s tests`；可用时 `ruff check src tests`、`mypy`，记录未运行项 |
| 根目录 | `cmake -B cpp/build -S cpp -DCMAKE_BUILD_TYPE=Debug`；`cmake --build cpp/build --parallel 2`；`ctest --test-dir cpp/build --output-on-failure`，以及 Task 9 的 no-exceptions 路径 |

- [ ] **Step 3：独立检查整个实现分支。** 执行方式确定后再使用对应 review skill；检查依赖边界、闭合的类型/omission 表、现有 0.1/0.2 回归与五项 Review Focus。远程 CI 只有已有授权的 PR/推送上下文才运行或读取；未运行的平台矩阵明确列为未运行。
- [ ] **Step 4：清理与报告。** 检查并停止任务后台资源；删除只属本任务的临时文件和额外 build/worktree，不动用户原有目录。保留必要且无凭据/隐私内容的验收摘要；`git status --short` 与计划写范围一致，`git diff --check` 无错误。
- [ ] **Step 5：完成标准。** 所有新增向量在五语言执行，原有向量全通过，三条请求链路与失效/编辑边界通过，已运行的 checks 真实绿色，未运行项明确披露。发布准备完成不等于已发布；tag/push/registry publishing 仍需要用户明确授权。

## 自查记录与执行交接

- 设计 §1～§3 对应 Task 1/2/4/5/6～9；§4 对应 Task 1/3/所有移植；§5 对应 Task 3/4/10；§6.1～6.3 对应 Task 2/3/4/5/10；§7 作为全部任务约束；§8 对应 Task 10～12。
- 五项 Review Focus 均有上文明确测试名称、输入或断言；没有把“添加适当测试”留给后续猜测。
- helper 名称、native 字段、六个 kind 与投影形状只在本计划接口节定义；跨语言用该定义，不自行分叉。
- 这是实施计划，不是实现结果。编写计划时没有执行上述产品验收命令，当前不声称任何新功能测试已经通过。
- 建议 **Native/当前会话逐单元执行**：IR 和遗漏表是共同接口，先做 Go 再逐语言移植比同时派发更容易控制一致性和开销。计划审阅后由用户确认是否开始实现；除非明确选择多 agent，否则不默认派发实现 worker。
