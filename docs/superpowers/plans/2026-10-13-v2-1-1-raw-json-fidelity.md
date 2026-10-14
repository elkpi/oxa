# v2.1.1 原始 JSON 保真与字段审计实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复五种语言的未知字段审计和 Anthropic opaque tool input 保真缺口，修复 Python 多工具 input 错误关联，完成 v2.1.1 implementation PR，并在用户 review/merge 后按 release checklist 正式发布。

**Architecture:** 不改变 OXA IR 与 hub-and-spoke 模型。为收到 provider JSON 的调用增加保留原文的入口；在 face converter 内对已识别对象审计未映射字段，按源顺序合并 loss，不把 unknown fields 透传给目标。Python 按 JSON 结构路径定位 raw tool input。五种语言各自使用本地 helper，保持 spoke 的依赖规则。

**Tech Stack:** Go 1.23/1.24, TypeScript Node 20 / Web Runtime, Rust stable + serde_json `raw_value`, Python 3.10–3.13 standard library, C++20, JSON Schema 2020-12, shared golden vectors / veccheck.

**Spec:** `docs/superpowers/specs/2026-10-13-v2-1-1-raw-json-fidelity-design.md`；normative baseline `spec/02-loss-policy.md`, `spec/10-mapping-openai-chat-completions.md`, `spec/11-mapping-openai-responses.md`, `spec/12-mapping-anthropic-messages.md`.

## Global Constraints

- 行为变化按仓库 contract 顺序交付：specification → golden vectors + regenerated manifest → implementation。
- 不改 `spec/schema/ir.schema.json`，IR `specVersion` 保持 `0.2.0`，不新增 IR field/variant、Runtime IR、transport、routing、admission policy 或 provider capability lookup。
- 五种发行包版本一致为 `2.1.1`；Spec 保持 `2.1.0`。Go module path 保持 `github.com/elkpi/oxa/go/v2`。
- 对象/typed API 保持兼容；只有原始输入入口承诺能审计 JSON 反序列化前的未知属性并保留 source bytes。禁止用全局 provenance side table。
- `unmapped-field` 按键存在记录，包括 `false`、`0`、`null`、空字符串、空数组、空对象；未知值不作类型验证、不递归 opaque payload、不透传。
- 整个 unsupported unit 已有 loss 时不重复记录其子字段；相同 loss 不得去重不同 source construct；source loss 顺序稳定。
- 对源 JSON 中的 Anthropic tool input，严格保留数字词法、键顺序、空白和转义；不能先转成通用 map/number 再序列化。
- Python 不得用搜索 tool ID 文本的方式定位 `input`；必须按 JSON 对象/数组路径定位。
- Go spokes 只依赖标准库、`ir` 和 `modelmap`；Rust spokes 不依赖其他 face crate；TypeScript 不新增 runtime dependency；Python 使用标准库；C++ 保持 C++20、零第三方 runtime dependency 和 `-fno-exceptions` 支持。
- 不同语言的测试/实现不互相写文件；共享 spec/vectors/manifest 只由协调工作单元修改。
- PR 合并前不打 release tags、不运行会发布 npm/PyPI/crates.io 的 workflow。正式发布以用户已 review 并 merge PR 为前置门禁。

## Review Focus

1. `store:false`、unknown `null`、空对象仍按字段存在报告；多个未知字段按源 encounter order 返回。由 vectors 和五种语言的单测覆盖 presence 与 loss ordering。
2. 已知对象深层 unknown 与 whole unknown block/item：保留整体 unsupported-semantic，不增加重复子字段 loss；opaque schema/tool input 内部不扫描。由 Task 1 vectors 和每语种单测覆盖。
3. opaque `tool_use.input` 含 `9007199254740993`、`1e+01`、反斜线 escape、空白和 key order：IR input 必须逐字等于 source substring。由 Anthropic request vector 和五语种测试覆盖。
4. 两个 tool_use 的 `input` 和 `id` 属性顺序交错：每个 input 仍归属正确 block。由 Python response regression、跨语言向量及结构路径测试覆盖。
5. malformed JSON/已知字段结构错误继续 error 且无 partial IR；raw stream unknown property 记录 loss 但不改变 lifecycle、不注入 IR event。由 raw entrypoint 与每 face stream tests 覆盖。

---

## Work-unit contracts

所有单元串行执行，除非最终执行方法另行选择 subagent-driven。每个 work unit 只修改下列文件边界，输出为对应提交和通过的定向测试结果；不得消费未通过验证的中间输出。每条单测命令最多运行 180 秒，单语言全套命令最多 600 秒；任一测试卡住超过上限、源文件范围变化或出现超出设计的 IR/API 语义时停止该单元并记录原因，不以部分结果冒充通过。串行执行无 worktree/后台 worker，临时 build/cache 放在各语言默认忽略目录；最终确认 git status 干净。若选择 subagent-driven，开始前另建 manifest，固定 U2-Go/U3-TS/U4-Rust/U5-Python/U6-Cpp 的文件输入、唯一 `.partial` patch 工件、校验命令和 45 分钟 worker 上限；每个 worker 只向仓库外写自己的 patch，原子发布后由协调单元验证并应用；完成、失败或取消时停止 worker、清理临时文件和 worktree。不得并行写 spec、vectors 或 manifest。

### Task 1: 澄清 unknown-field 规范并新增 shared regression vectors

**Files:**
- Modify: `spec/02-loss-policy.md`
- Modify: `spec/10-mapping-openai-chat-completions.md`
- Modify: `spec/11-mapping-openai-responses.md`
- Modify: `spec/12-mapping-anthropic-messages.md`
- Modify: `spec/20-streaming-semantics.md`
- Modify: `spec/CHANGELOG.md` (clarification only; no Spec version bump)
- Create: `vectors/chatcompletions/nonstream/unknown-request-field-to-ir.json`
- Create: `vectors/anthropic/nonstream/unknown-request-field-to-ir.json`
- Create: `vectors/anthropic/nonstream/unknown-nested-request-field-to-ir.json`
- Create: `vectors/anthropic/nonstream/raw-json-request-tool-input-to-ir.json`
- Create: `vectors/anthropic/nonstream/tool-input-field-order-response-to-ir.json`
- Create: `vectors/responses/nonstream/unknown-request-fields-to-ir.json`
- Create: `vectors/responses/nonstream/unknown-reasoning-field-to-ir.json`
- Create: `vectors/chatcompletions/stream/unknown-event-field-to-ir.json`
- Create: `vectors/responses/stream/unknown-event-field-to-ir.json`
- Create: `vectors/anthropic/stream/unknown-event-field-to-ir.json`
- Modify: `vectors/anthropic/nonstream/loss-unknown-block-to-ir.json` (pin whole-unit loss de-duplication)
- Modify: `vectors/manifest.json` (generated only)

**Input range:** 已批准的 design doc、`spec/02` §4/§9、face mapping §3、`vectors/README.md` 和现有同类 vector。

**Output artifact:** 规范澄清 + 10 个新增 shared vectors + 1 个 whole-unit loss de-dup regression update + generated manifest。

**Completion conditions:** vectors schema/naming 均合法；false/null presence 有 loss；raw input expected value 为 source substring；每个新增场景至少由一个确认有该缺口的实现以预期差异失败，已正确实现该子行为的实现允许通过；manifest 检查通过。

- [x] **Step 1: 先写规范澄清。** 在 `spec/02` 明确 nested recognized object 的 unknown-field 规则、值不参与类型检查、opaque/whole-unit 的边界、presence semantics 和 loss 顺序；在 spec/10–12 的 loss catalog 引用该通则，列明 Responses `previous_response_id` / `store` 是无 IR destination 的字段，不向目标透传。在 `spec/20` 明确已知 stream event 上未映射属性产生 standalone `unmapped-field` loss，不改变 lifecycle、loss path 根于 event payload，loss 不进入 IR event。在 `spec/CHANGELOG.md` 记为对 Spec 2.1.0 既有规则的文字澄清，不引入新语义或 Spec version bump。明确现有 JSON Schema 和 IR version 不变。
- [x] **Step 2: 写请求和嵌套字段 vectors。** Chat Completions、Anthropic 各自写 `unknown_extension:true`；Anthropic 再在已识别的 message/content block 中放未知字段。Responses `unknown-request-fields` 同时放 `previous_response_id` 与 `store:false`，期待两条 `unmapped-field` losses；单独的 reasoning vector 在已识别 `reasoning` map 内添加新属性并期待路径为 `reasoning.unknown_extension`；更新现有 `loss-unknown-block-to-ir` vector，在整体丢弃的 document block 上加 `unknown_extension:true`，expected losses 仍只有一条 whole-block loss。
- [x] **Step 3: 写原文及多工具 vectors。** Anthropic request vector 把 tool input 写为 `{ "z": 9007199254740993, "a": 1e+01, "escaped": "\\u0041" }`，期待 IR input 字符串精确保留 source substring；response vector 放两个 tool_use，故意将两个 input 放在各自 id/name 之前，参数值设为不同 object，期待各自正确映射。
- [x] **Step 4: 写三个 streaming vectors。** 每个 face 的一个已知 native event 带 `unknown_extension:null`，expected IR events 与无扩展版本相同，expected loss 为 event payload 根路径 `unknown_extension` / field `unknown_extension` 的一条 `unmapped-field`；不得把 loss 加到 event sequence。
- [x] **Step 5: 校验 vectors 并观察 Red。**

Run from repository root:
```bash
(cd go && go run ./cmd/veccheck -root .. -write-manifest)
(cd go && go run ./cmd/veccheck -root .. -check-manifest)
```
Expected: 输出 173 vectors、174 checks、OK。运行现有各语言 vector suites；已知存在缺口的实现必须在对应 expected loss/raw input 向量上失败，已正确实现的子行为可以通过。逐项记录实际失败向量，不把无关失败算作 Red，不先改生产代码。
- [x] **Step 6: 提交独立规范/vector commit。**

```bash
git add spec/02-loss-policy.md spec/10-mapping-openai-chat-completions.md spec/11-mapping-openai-responses.md spec/12-mapping-anthropic-messages.md spec/20-streaming-semantics.md spec/CHANGELOG.md vectors/
git commit -m "spec: pin unknown-field loss and raw tool input fidelity"
```

**Timeout/cleanup:** 单命令 180 秒；保留 SDD ignored workspace 的 vector manifest/red logs 作审计证据；删除此任务创建的 `cpp/build/v2.1.1-red` 和 `/tmp/oxa-v211-vector-stage.*` staging trees。此任务不启动并行 worker。

---

### Task 2: Go raw JSON boundary、field audit 与 Anthropic 原文保真

**Files:**
- Modify: `go/openai/chatcompletions/decode.go`, `go/openai/chatcompletions/streamin.go`, `go/openai/chatcompletions/vectors_test.go`, `go/openai/chatcompletions/wire_test.go`, `go/openai/chatcompletions/streaming_test.go`
- Create/modify: `go/openai/chatcompletions/rawjson.go`
- Modify: `go/openai/responses/decode.go`, `go/openai/responses/streamin.go`, `go/openai/responses/vectors_test.go`, `go/openai/responses/wire_test.go`, `go/openai/responses/streaming_test.go`
- Create/modify: `go/openai/responses/rawjson.go`
- Modify: `go/anthropic/messages/decode.go`, `go/anthropic/messages/streamin.go`, `go/anthropic/messages/vectors_test.go`, `go/anthropic/messages/wire_test.go`, `go/anthropic/messages/streaming_test.go`
- Create/modify: `go/anthropic/messages/rawjson.go`

**Consumes:** Task 1 spec and raw vectors.

**Produces:**
```go
func DecodeRequestJSON(raw []byte, opts ...Option) (*ir.Request, []ir.Loss, error)
func DecodeResponseJSON(raw []byte, opts ...Option) (*ir.Response, []ir.Loss, error)
func (d *StreamDecoder) FeedJSON(raw []byte) ([]ir.Event, error)
```
每個 face 提供对应入口；typed API 保留。实现 package-local scanner，spoke 之间无新 imports。

**Completion conditions:** 三个 spokes 的新 vectors 全过；typed APIs 原有 tests 仍过；Anthropic request JSON path 保留原始 `input`；Go lint/format/dependency guard 通过。

- [ ] **Step 1: 先跑 Task 1 向量 Red 并记录失败。**

Run from `go/`: `go test -count=1 ./openai/chatcompletions ./openai/responses ./anthropic/messages`
Expected: unknown-field vectors 报 expected loss missing，Anthropic request tool vector 报 IR input mismatch，stream vectors 报 loss missing；不能修改 expected vectors 让其通过。
- [ ] **Step 2: 写 raw API 的 presence regression。** 在 `go/openai/responses/wire_test.go` 添加测试，要求转换成功、IR 含一条 user message、loss 恰为 `store` 的 `unmapped-field`：

```go
func TestDecodeRequestJSONReportsPresentFalseUnknownField(t *testing.T) {
    raw := []byte(`{"model":"m","input":"hi","store":false}`)
    got, losses, err := DecodeRequestJSON(raw)
    if err != nil || got == nil || len(got.Messages) != 1 {
        t.Fatalf("DecodeRequestJSON: request=%+v err=%v", got, err)
    }
    if len(losses) != 1 || losses[0].Path != "store" || losses[0].Field != "store" || losses[0].Reason != ir.LossUnmappedField {
        t.Fatalf("losses = %#v, want one unmapped-field for present store:false", losses)
    }
}
```

Task 1 shared vector 已记录现有实现的 Red；本测试在写入生产逻辑前建立精确 API contract。
- [ ] **Step 3: 先加最小 raw wrapper，再验证测试 Red，然后实现 scanner。** 暂时让 `DecodeRequestJSON` parse typed `Request` 并调用现有 `DecodeRequest`；运行 Step 2 test，必须因 `losses` 缺失失败而不是 JSON/IR 构造错误。然后实现 package-local JSON scanner，使用 `json.Decoder`/`json.RawMessage` 保留未知属性 presence 和源 offset；为 request/response/stream event 定义已识别对象 field allowlist，只遍历 spec 指定的已知结构节点；按 source offset 稳定合并未知字段 losses 与 semantic losses；增加 input 顺序为 `store` 再 `previous_response_id` 的单测，逐项断言 loss 顺序与原文一致；结构错误时返回 nil IR。vector adapters 从原始 `json.RawMessage` 调用新 API，不能先 `json.Unmarshal` 成 Request。
- [ ] **Step 4: 修复 Anthropic request content 原文路径。** 在 `DecodeRequestJSON` 中沿 `messages[i].content[j]` 结构将 tool input 的原始 `json.RawMessage` 传至 IR string token；递归覆盖 tool_result content；绝不经 generic `[]any` → `json.Marshal` → `BlockWire` round-trip。增加上述大整数/指数/escape/whitespace 断言。
- [ ] **Step 5: 实现原始 stream event 入口。** 三个 StreamDecoder 的 `FeedJSON` 先 parse/审计 event，再交给现有生命周期转换；unknown fields 进入 `Losses()`，不会改变 lifecycle state 或产生 IR events。分别测试 unknown field 后追加有效 event 仍可正确 Feed。
- [ ] **Step 6: 运行 Go 全套和静态检查。**

Run from `go/`: `go test -count=1 ./... && go vet ./... && test -z "$(gofmt -l .)" && go run ./cmd/veccheck -root .. -check-manifest`
Expected: 所有 Go tests pass；gofmt 无待格式化文件；dependency guard pass；manifest 检查 pass。
- [ ] **Step 7: 提交 Go 单元。**

```bash
git add go/openai/chatcompletions go/openai/responses go/anthropic/messages
git commit -m "fix(go): audit raw JSON fields and preserve Anthropic input"
```

**Timeout/cleanup:** targeted tests 180 秒，全 Go tests 600 秒；Go build artifacts 留在默认 cache，不在仓库新增临时文件。

---

### Task 3: TypeScript raw JSON entrypoints、field audit 与 stream audit

**Files:**
- Modify: `ts/src/json/parse.ts`, `ts/src/json/value.ts`, `ts/src/json/index.ts`（仅当需要 source offsets API）
- Modify: `ts/src/openai/chatcompletions/nonstream.ts`, `index.ts`, `stream.ts`
- Modify: `ts/src/openai/responses/nonstream.ts`, `index.ts`, `stream.ts`
- Modify: `ts/src/anthropic/messages/nonstream.ts`, `index.ts`, `stream.ts`
- Modify: `ts/test/chatcompletions-stream.test.ts`, `ts/test/responses-nonstream.test.ts`, `ts/test/responses-stream.test.ts`, `ts/test/anthropic-nonstream.test.ts`, `ts/test/anthropic-stream.test.ts`, `ts/test/nonstream-vector-runner.ts`, `ts/test/nonstream-vectors.test.ts`

**Consumes:** Task 1 spec/vectors; existing `JsonObject`, `parseJson`, `sourceTextOf`.

**Produces:**
```ts
function decodeRequestJSON(raw: string, options?: NonstreamOptions): ConversionResult<Request>
function decodeResponseJSON(raw: string, options?: NonstreamOptions): ConversionResult<Response>
class StreamDecoder { feedJSON(raw: string): readonly Event[] }
```
按各 face 现有返回类型、stream decoder class 名称发布实际 exports；不新增 npm runtime dependency。

**Completion conditions:** 所有 TS request/response/stream vectors pass；opaque `parseJson` source text 未回退到 `JSON.parse`；Web Runtime、Node tests 与 declarations pass。

- [ ] **Step 1: 将 Task 1 vectors 通过现有 vector runner 运行并记录 Red。**

Run: `cd ts && npm run test:node`
Expected: unknown-field vectors fail on expected losses; `raw-json-request-tool-input` fails if source text is absent from the vector path。
- [ ] **Step 2: 先用现有 object API 写 failing presence test。** 在 `ts/test/responses-nonstream.test.ts` 调用 `decodeRequest(parseJson('{"model":"m","input":"hi","store":false}'))`，断言结果含一条 `store` `unmapped-field` loss；运行此测试确认它因 loss 缺失而失败。raw `decodeRequestJSON` 的 parity assertion 在 wrapper 落地后加入，并比较两条入口的 value/losses。
- [ ] **Step 3: 实现每个 face 的 object allowlist scanner 和 raw JSON wrapper。** `decodeRequestJSON` 使用 `parseJson`；不使用 `JSON.parse`。对象输入使用现有 JsonObject/source token；raw 字段 presence 依据 `undefined` 区分，不以 false/null/empty truthiness 判定。opaque JSON schema、tool input 和参数字段不递归扫描。
- [ ] **Step 4: 保证 source-order merge 与 stream FeedJSON。** 从 parse/source node metadata 取得 loss node offsets，稳定排序 audit losses 与 converter losses；添加两个逆 allowlist 顺序的 raw object fields，断言 losses 与 JSON source order 相同；新增 `feedJSON` 并在调用现有 Feed 前完成字段审计。测试未知属性不改变 stream state，后续合法 event 正常完成。
- [ ] **Step 5: 更新所有 TS 导出和执行测试。**

Run: `cd ts && npm run generate:check && npm run fmt && npm run check && npm run test:architecture && npm run test:node && npm run test:web && npm run test:package && npm run test:consumer`
Expected: 命令全部退出 0，public declarations 含 raw JSON functions，package/consumer 验证通过。
- [ ] **Step 6: 提交 TypeScript 单元。**

```bash
git add ts/src ts/test
git commit -m "fix(ts): audit raw JSON fields and preserve source tokens"
```

**Timeout/cleanup:** Node 定向测试 180 秒，完整 TypeScript gate 600 秒；build output 保持在忽略的 `dist*`，不提交生成目录。

---

### Task 4: Rust raw entrypoints、Serde field audit 与 parameter fidelity

**Files:**
- Modify: `rust/crates/oxa-chatcompletions/src/{lib.rs,decode.rs,streamin.rs}` 和对应 `tests/{nonstream.rs,stream.rs,vectors.rs}`
- Modify: `rust/crates/oxa-responses/src/{lib.rs,decode.rs,streamin.rs}` 和对应 `tests/{nonstream.rs,stream.rs,vectors.rs}`
- Modify: `rust/crates/oxa-anthropic/src/{lib.rs,decode.rs,normalize.rs,streamin.rs,types.rs}` 和对应 `tests/{nonstream.rs,stream.rs,vectors.rs}`

**Consumes:** Task 1 shared vectors；已有 `serde_json` `raw_value` feature；Rust vector runner 已将原始 vector input 作为 `&str` 传给 adapter。

**Produces:**
```rust
pub fn decode_request_json(raw: &str, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error>
pub fn decode_response_json(raw: &str, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error>
impl StreamDecoder { pub fn feed_json(&mut self, raw: &str) -> Result<Vec<Event>, Error> }
```
三个 spokes 均导出同等 API，既有 `decode_*(&Request)` 与 Feed 类型 API 继续保留。

**Completion conditions:** typed Serde unknown-field drops 不再影响 raw entry/vector 路径；Anthropic JSON request raw input lexical form exact；spoke 依赖图无新增跨 face edge。

- [ ] **Step 1: 运行 Rust vectors 确认 Red。**

Run: `cd rust && cargo test -p oxa-responses --test vectors && cargo test -p oxa-anthropic --test vectors`
Expected: 新增 unknown losses/raw request input vectors fail with missing loss or IR mismatch。
- [ ] **Step 2: 写 Rust raw entrypoint regression tests。** 先写调用 `decode_request_json` 的 `store:false` 断言和两组 Anthropic tool input 原文断言；Task 1 vector 已先提供真实 Red。为让测试以行为断言方式运行，先增加仅把 raw JSON 解析成 typed `Request` 后调用现有 decoder 的最小 wrapper；测试应因 store loss 缺失/raw input canonicalized 而失败，之后再替换为 RawValue/source-order scanner。
- [ ] **Step 3: 实现 JSON raw visitor 和 local field auditing。** `serde_json::Value` 不得作为 source-preserving intermediate；用 `RawValue`/自定义 visitor 读取已识别字段和未知键，记录源顺序信息；增加两个 JSON 属性顺序与 Rust struct/map 顺序相反的断言，loss slice 必须保留源顺序；按 face 遍历已知 schema node，opaque/unknown whole unit 仅留既有处理。typed API 保持现有调用方式。
- [ ] **Step 4: 修复 Anthropic `Message` / `ContentValue` 原始路径与 stream `feed_json`。** raw input substring 按 block 的结构节点归属；feed raw event 时按同一 scanner 生成 losses，并确保未知字段后生命周期 state 可继续。
- [ ] **Step 5: 更新 vector adapters 并运行 Rust 全套检查。**

Run: `cd rust && cargo fmt --all -- --check && cargo clippy --workspace --all-targets -- -D warnings && cargo test --workspace`
Expected: 三命令全通过，所有五语种共享 vectors 已包含 Task 1 additions。
- [ ] **Step 6: 提交 Rust 单元。**

```bash
git add rust/crates
git commit -m "fix(rust): audit raw JSON fields and retain Anthropic input"
```

**Timeout/cleanup:** 定向 crate tests 180 秒，workspace clippy/test 各 600 秒；Cargo target 保持在仓库忽略的 `rust/target`。

---

### Task 5: Python raw JSON boundary、字段审计与 Anthropic tool association fix

**Files:**
- Modify: `python/src/oxa/openai/chatcompletions/{__init__.py,decode.py,streamin.py}`
- Modify: `python/src/oxa/openai/responses/{__init__.py,decode.py,streamin.py}`
- Modify: `python/src/oxa/anthropic/messages/{__init__.py,decode.py,normalize.py,streamin.py}`
- Modify: `python/src/oxa/vectest/{load.py,runner.py}` only where structural raw slices need path-aware extraction
- Modify: `python/tests/test_{chatcompletions_nonstream,responses_nonstream,anthropic_nonstream,stream_unit,anthropic_vectors,responses_vectors}.py`

**Consumes:** Task 1 vectors; existing vector loader `Vector.input_raw`, nonstream runner passes `input_raw` to decoders; current public APIs accept dict plus optional raw text.

**Produces:**
```python
def decode_request_json(raw_text: str, table: Table | None = None) -> tuple[Request, list[Loss]]
def decode_response_json(raw_text: str, table: Table | None = None) -> tuple[Response, list[Loss]]
class StreamDecoder:
    def feed_json(self, raw_text: str) -> list[Event]
```
各 face 的 exports 同步；原有 dict API 继续工作。

**Completion conditions:** 顶层/嵌套未知属性 presence 正确；Anthropic request JSON raw input lexical form exact；多 tool response input 按结构 owner 关联，独立于 id/input 属性先后顺序。

- [ ] **Step 1: 先在 `python/tests/test_anthropic_nonstream.py` 写双 tool response regression。** 使用 `raw_text` 中两个 tool_use，故意让每个 `input` 都出现在各自 `id` 之前，值分别是 `{"x":1}` 与 `{"x":2}`；断言两个 IR blocks 分别保留不同值。Run `cd python && PYTHONPATH=src python3 -m unittest tests.test_anthropic_nonstream`; Expected: 现有 id-string search 错误地把第二个 input 分配给第一个。
- [ ] **Step 2: 在 Responses 与 Anthropic 测试加 unknown presence。** 对 `store:false`、unknown `null`、`reasoning.unknown_extension` 断言一条相应 `unmapped-field`，确保 dict API 和新 raw API 使用相同扫描规则。
- [ ] **Step 3: 实现结构化 raw JSON 节点提取器。** 从 raw object/array 层级解析 `messages[i].content[j].input`、tool_result 嵌套和 `content[i].input`；不根据 ID 或 key name 的全局字符串搜索定位。为 loss merge 保留 source order offset；加一组输入顺序为 `store` 在前、`previous_response_id` 在后的断言，loss 列表也必须按此顺序；未知值不递归。
- [ ] **Step 4: 增加 raw entrypoints 和 stream `feed_json`。** 使用标准库 JSON 解析、原始 text 和 path-aware extractor；将未知字段审计传至三 face decoders；保留 typed dict 接口。
- [ ] **Step 5: 更新 vector runner 的 raw input 使用并运行全套 Python tests。**

Run: `cd python && PYTHONPATH=src python3 -m unittest discover -s tests`
Expected: 全套通过，所有 shared vectors 通过，双 tool association regression 通过。
- [ ] **Step 6: 提交 Python 单元。**

```bash
git add python/src/oxa/openai python/src/oxa/anthropic python/src/oxa/vectest python/tests
git commit -m "fix(python): audit raw JSON and bind tool input by path"
```

**Timeout/cleanup:** 定向 tests 180 秒，全套 600 秒；Python 禁止在仓库留下 `__pycache__` 新产物，运行时设置 `PYTHONDONTWRITEBYTECODE=1` 或清理仅本任务创建的缓存。

---

### Task 6: C++ source-slice audit 与参数保真

**Files:**
- Modify: `cpp/include/oxa/openai/chatcompletions.hpp`, `cpp/include/oxa/openai/responses.hpp`, `cpp/include/oxa/anthropic/messages.hpp`
- Modify: `cpp/src/openai/chatcompletions.cpp`, `cpp/src/openai/responses.cpp`, `cpp/src/anthropic/messages.cpp`
- Modify: `cpp/tests/test_chatcompletions.cpp`, `cpp/tests/test_responses.cpp`, `cpp/tests/test_anthropic.cpp`, `cpp/tests/test_stream.cpp`, `cpp/tests/test_vectors.cpp`

**Consumes:** Task 1 vectors；既有 `decode_* (string_view)` overload 和 `json::Value::raw_slice()`。

**Produces:** 三个 stream decoder 的 `feed(std::string_view)` overload；object/value decode 与 string decode 共用 unknown-field inspection，原始 string 路径保留 source slices。

**Completion conditions:** request, response, stream unknown field vectors pass；Anthropic request `input` 精确保留 source substring；build 在标准和 `-fno-exceptions` 配置均通过。

- [ ] **Step 1: 写 C++ failing tests。** 在 `test_responses.cpp` 调用既有 string_view overload 处理 `store:false`，断言 `losses[0].path == "store"`；在 `test_anthropic.cpp` 对大整数/指数/空白 input 断言 IR input 等于完整 raw substring。

Run: `cmake -S cpp -B cpp/build/v2.1.1-red -DCMAKE_BUILD_TYPE=Debug && cmake --build cpp/build/v2.1.1-red --parallel && ctest --test-dir cpp/build/v2.1.1-red --output-on-failure`
Expected: 新测试因 `store` loss 缺失及 request input canonicalization 而失败；现有其他 tests 状态单独记录。
- [ ] **Step 2: 实现每个 face 的 local key inspector。** 遍历 `json::Value` known-object fields，不扫描 tool input/schema opaque JSON 内容；string overload 从 parsed source nodes 保留 raw slice 和 offset；增加 `store` / `previous_response_id` 两种输入键序的断言，loss vector 顺序必须随 source 顺序；保持 `StatusOr<Conversion<T>>` 错误模式、无 exceptions。
- [ ] **Step 3: 将 source slice 路径用于 Anthropic request content。** 当前 response 的 `keep_raw_input` 不足以证明 request 保真；request/tool_result 嵌套与响应路径都按对应 JSON node 取 `raw_slice()`。
- [ ] **Step 4: 为三种 StreamDecoder 增加 `feed(string_view)`。** 转换前 parse+audit，unknown metadata 不改变状态；typed `feed(json::Value)` 保持可用。
- [ ] **Step 5: 运行完整 C++ 标准及 `-fno-exceptions` 验证。**

Run: `cmake -S cpp -B cpp/build/v2.1.1 -DCMAKE_BUILD_TYPE=Debug && cmake --build cpp/build/v2.1.1 --parallel && ctest --test-dir cpp/build/v2.1.1 --output-on-failure && cmake -S cpp -B cpp/build/v2.1.1-noexcept -DCMAKE_BUILD_TYPE=Debug -DCMAKE_CXX_FLAGS=-fno-exceptions && cmake --build cpp/build/v2.1.1-noexcept --parallel && ctest --test-dir cpp/build/v2.1.1-noexcept --output-on-failure`
Expected: 两种配置 build 与 CTest 全通过；build directories 均位于 `.gitignore` 覆盖的 `cpp/build/`。
- [ ] **Step 6: 提交 C++ 单元。**

```bash
git add cpp/include/oxa/openai cpp/include/oxa/anthropic cpp/src/openai cpp/src/anthropic cpp/tests
git commit -m "fix(cpp): audit unknown JSON fields and retain raw tool input"
```

**Timeout/cleanup:** 每次 build/CTest 不超过 600 秒；Task 6 通过后删除仅本任务创建的 `cpp/build/v2.1.1` 和 `cpp/build/v2.1.1-noexcept`，Task 8 创建的 final directories 由 Task 8 清理。

---

### Task 7: 协调发行版本、README/vector counts 与 changelog

**Files:**
- Modify: `go/doc.go`, `go/doc_test.go`
- Modify: `ts/package.json`, `ts/package-lock.json`, `ts/scripts/test-package.mjs`
- Modify: `rust/Cargo.toml`, `rust/Cargo.lock`, `rust/crates/oxa-ir/tests/codec.rs`
- Modify: `python/pyproject.toml`, `python/src/oxa/__init__.py`, `python/tests/test_smoke.py`
- Modify: `cpp/CMakeLists.txt`
- Modify: `CHANGELOG.md`, `README.md`, `spec/README.md`, `ts/README.md`, `rust/README.md`, `cpp/README.md`, `docs/release-checklist.md`
- Modify: `ts/test/vectest.test.ts`, `python/tests/test_vectest.py`, `rust/crates/oxa-vectest/tests/load.rs`, `cpp/tests/test_vectors.cpp`, `cpp/tests/test_chatcompletions.cpp`, `cpp/tests/test_anthropic.cpp`, `cpp/tests/test_responses.cpp`, `cpp/tests/test_stream.cpp`

**Consumes:** 五语种 green implementation; `rg -n '163|2\.1\.0|prepared|pending'` inventory。

**Produces:** 所有发行元数据指向 2.1.1；README 如实区分 latest published v2.1.0 与当前尚未发布的 v2.1.1 源码；文档 vectors count 与新 manifest 一致。

**Completion conditions:** 版本测试和 package guards 同步通过；所有当前 vector-count assertion/documentation 等于 manifest 的 173（保留 v2.1.0 历史 changelog 和 reasoning 数值 `16384`）；`README.md` 不将 v2.1.1 写作已发布；`CHANGELOG.md` 有 v2.1.1 unreleased section。

- [ ] **Step 1: 用 `rg -n '163|2\.1\.0|prepared|pending'` 重跑 inventory。** 逐项把当前 test/package vector totals 改为 manifest 验证的 173；保留 `CHANGELOG.md` 中 v2.1.0 的历史 163-vector 记载和推理努力 token 数值 `16384`。
- [ ] **Step 2: 更新所有 package version 到 2.1.1。** 同步 npm lockfile、Rust workspace/crates lock metadata、Python runtime `__version__`、CMake project version 和 Go doc version/test。保留 Go import path v2。
- [ ] **Step 3: 增加 `CHANGELOG.md` v2.1.1 Unreleased 条目。** 记录新增 raw JSON 入口、unknown-field losses、Anthropic raw input 保真和 Python association fix；Spec 保持 2.1.0，不写 IR schema/version bump。
- [ ] **Step 4: 校正所有 shared-vector counts 与 README 状态。** 新总数为 173 vectors：C++ face nonstream counts 为 Chat Completions 45、Anthropic 41、Responses 49、cross 17；stream counts 为 6/7/8。当前 latest published release 仍为 v2.1.0；source 包版本 2.1.1 必须标作尚未发布，不得复现 v2.1.0 `prepared/pending` 错误。更新 Go/TS/Rust/Python/C++ 测试断言、`spec/README.md` 的共享向量总数、TS/Rust/C++ 文档和 `docs/release-checklist.md` 的旧 count。
- [ ] **Step 5: 运行版本/包装定向检查。**

Run: `cd go && go test -count=1 . && go run ./cmd/veccheck -root .. -check-manifest && cd ../ts && npm run test:package && cd ../rust && cargo test -p oxa-ir --test codec && cd ../python && PYTHONPATH=src python3 -m unittest tests.test_smoke tests.test_vectest`
Expected: 各项 version assertion、package validation、vector count assertions pass。
- [ ] **Step 6: 单独提交 release preparation。**

```bash
git add go/doc.go go/doc_test.go ts/package.json ts/package-lock.json ts/scripts/test-package.mjs rust/Cargo.toml rust/Cargo.lock rust/crates/oxa-ir/tests/codec.rs python/pyproject.toml python/src/oxa/__init__.py python/tests/test_smoke.py cpp/CMakeLists.txt CHANGELOG.md README.md spec/README.md ts/README.md rust/README.md cpp/README.md docs/release-checklist.md ts/test/vectest.test.ts python/tests/test_vectest.py rust/crates/oxa-vectest/tests/load.rs cpp/tests/test_vectors.cpp cpp/tests/test_chatcompletions.cpp cpp/tests/test_anthropic.cpp cpp/tests/test_responses.cpp cpp/tests/test_stream.cpp
git commit -m "chore(release): prepare coordinated v2.1.1 packages"
```

**Timeout/cleanup:** 定向 test 180 秒；不创建 release tag，不执行 registry publish workflow。

---

### Task 8: 五语言发布门禁、最终 review 与创建 PR

**Files:** 不新增生产文件；只处理全部已批准 task 的分支提交与 PR。

**Consumes:** Task 1–7 所有验证过的 commits，当前分支 `fix/raw-json-fidelity-v2.1.1`。

**Produces:** 远端分支和面向 `main` 的 PR；PR body 明确发布门禁“仅用户 review + merge 后 release”。

**Completion conditions:** 工作树干净；Go/Rust/Python/TypeScript/C++ 全套门禁、veccheck 和 manifest 全通过；PR 创建成功；没有 release tag 和 registry publish。

- [ ] **Step 1: 执行仓库 release checklist 的全量门禁。**

Run from repository root:
```bash
set -euo pipefail
go -C go test -count=1 ./...
go -C go vet ./...
go -C go run ./cmd/veccheck -root .. -check-manifest
(cd ts && npm run generate:check && npm run fmt && npm run check && npm run test:architecture && npm run test:node && npm run test:web && npm run test:package && npm run test:consumer)
(cd rust && cargo fmt --all -- --check && cargo clippy --workspace --all-targets -- -D warnings && cargo test --workspace)
(cd python && PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=src python3 -m unittest discover -s tests)
cmake -S cpp -B cpp/build/v2.1.1-final -DCMAKE_BUILD_TYPE=Debug
cmake --build cpp/build/v2.1.1-final --parallel
ctest --test-dir cpp/build/v2.1.1-final --output-on-failure
cmake -S cpp -B cpp/build/v2.1.1-noexcept-final -DCMAKE_BUILD_TYPE=Debug -DCMAKE_CXX_FLAGS=-fno-exceptions
cmake --build cpp/build/v2.1.1-noexcept-final --parallel
ctest --test-dir cpp/build/v2.1.1-noexcept-final --output-on-failure
OXA_ROOT="$PWD" bash ci/consumers/run.sh
bash ci/reliability/run.sh
```
Expected: 所有命令退出 0；向量 manifest 与源码匹配；五语种实际 vectors 数一致。
- [ ] **Step 2: 等待 PR CI matrix 完成。** 要求 Go 1.23/1.24、TypeScript Node/Web/Windows、Rust、Python 3.10–3.13、C++ Ubuntu/macOS/Windows 与 Ubuntu `-fno-exceptions` jobs 全部 green；PR 的 clean-consumer 和 reliability jobs 也必须成功。读取 CI run 的最终状态，不能仅凭本地测试声称 release gate 通过。
- [ ] **Step 3: 独立审查最终 diff。** 检查只含计划中的文件；验证未知字段没有透传、无日志泄露 payload、无新增第三方依赖、无 IR/schema 变化、原始 JSON 测试涵盖大整数和多工具路径、release versions 相同。
- [ ] **Step 4: 推送分支并创建 PR。** 先执行 `git diff --check`、`git status --short`、`git log --oneline main..HEAD`；随后运行：

```bash
git push -u origin fix/raw-json-fidelity-v2.1.1
gh pr create --base main --head fix/raw-json-fidelity-v2.1.1 --title "fix: preserve raw protocol JSON fidelity" --body "Implements the approved v2.1.1 raw JSON fidelity and unknown-field audit design across Go, TypeScript, Rust, Python, and C++. Includes shared spec/vectors and package version updates. All local gates and PR CI must pass. Per request, do not release until the author reviews and merges this PR."
```

PR body 列出已实际运行的测试结果；不自动合并、不提前打 tag。
- [ ] **Step 5: 等待用户 review/merge。** PR 创建后停止自动推进；不 rebase、改写远端 branch、merge、创建 tags 或发布 registry，除非用户提出后续要求。用户明确 merge 后才进入 Task 9。

**Timeout/cleanup:** 单个 CI job 最多 30 分钟后报告真实状态，不重复推送发布动作；完成时清理 `cpp/build/v2.1.1*`，确认工作树 clean。

---

### Task 9: 用户 merge 后的 v2.1.1 正式发布（门控任务）

**Files:** `CHANGELOG.md`, `README.md`, release metadata only if the checked release checklist requires a post-merge release commit; Git tags/workflows are external outputs。

**Input gate:** 用户已 review 并 merge implementation PR；确认 merged commit 在 `main`，main CI 完成且全绿；五语言 release workflows / credentials preconditions 均可用。

**Output artifact:** GitHub v2.1.1 release 与各已配置 registry 的可查询 2.1.1 packages；C++ CMake source release version 为 2.1.1。

**Completion conditions:** `docs/release-checklist.md` 全项满足；tag 指向 main release commit；GitHub Release 和 npm/PyPI/crates.io registry 各自验证成功；未成功的目标逐项如实报告。

- [ ] **Step 1: 在 main 重新检查 merge commit、CI、package versions、manifest 和 consumers。** 任一 gate 不通过，停止发布并修复。
- [ ] **Step 2: 完成 dated `CHANGELOG.md` 与发行版本检查。** 确认 v2.1.1 release notes 与合并代码相符，运行 `cd ts && npm run release:check`、五语种 package/version gates；README 只描述当前已发布版本，不能在 registry 尚未发布时声称 v2.1.1 已发布。
- [ ] **Step 3: 只在 main CI 全绿且 release gates 通过后创建 tags。** 创建 root `v2.1.1` 和语言 tags `go/v2.1.1`、`ts/v2.1.1`、`py/v2.1.1`、`rust/v2.1.1` 并推送；逐一确认 tag 对应合并后的 main release commit。语言 tags 触发既有 npm/PyPI/crates.io workflows。
- [ ] **Step 4: 等待并核实 GitHub/registry 的真实结果。** 查询 GitHub Release、npm `@elkpi/oxa@2.1.1`、PyPI `elkpi-oxa==2.1.1` 和所有 crates.io crate 2.1.1；每个 workflow 最多等待 30 分钟，失败即记录并停止后续发布步骤。C++ 无 registry publisher，只核实 GitHub source/CMake version。依赖 crates 版本在 crates.io 可用后按 checklist 执行 `cargo package --locked`。
- [ ] **Step 5: 所有目标 registry 确认成功后再同步 README latest-release 状态为 v2.1.1。** 新建 docs-only PR 更新当前已发布版本表和 README；等待该 PR 按仓库流程合并并验证，绝不直接改写 main。

**Timeout/cleanup:** 每个 GitHub workflow 最多 30 分钟；失败即停并报告，不自动重试导致重复外部发布；不保留 release 之外的临时 artifacts。

---

## 执行选择

建议采用 **Native（当前主会话按 Task 1→9 串行执行）**：字段 allowlist、loss 去重/排序与原文 provenance 是跨语言共享语义，先统一 contract，再逐语种实现比并行修改更容易避免 API 和测试行为漂移。Subagent-driven 可减少五语言实现时间，但需要额外 manifest、隔离 worktrees、patch artifacts 与逐个验收，且共享规范/vector 阶段必须保持串行。
