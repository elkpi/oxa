# v0.3.0 后路线评审

**日期：** 2026-09-04
**范围：** 仅依据仓库的一手资料评审 v1.0.0、生态分发、质量工程与 post-v1 协议演进四个候选方向。
**结论：** 四个方向不应并列启动。建议采用“发布契约收口 → v1.0.0 → 分发落地 → 正确性 fuzz → 有目的的性能基准 → 协议演进”的串行门禁；其中 v1.0.0 前必须先处理若干文档与分发可达性缺口。

## 结论摘要

1. **v1.0.0 是下一主线，但不是立刻打 tag。** 规范已把其门槛定义为四种支持语言对同一 spec/vector set 的实现完成（[spec/README.md:30-41](../../spec/README.md#L30-L41)），当前 CI 已覆盖 Go、Rust、Python、C++ 与两个 C++ exception 配置（[.github/workflows/ci.yml:9-175](../../.github/workflows/ci.yml#L9-L175)）。但根 README 仍写着 `early development, pre-v1`（[README.md:10-12](../../README.md#L10-L12)），并把跨 face 流式转换标为 `not yet`（[README.md:76-86](../../README.md#L76-L86)）；这必须在 1.0 发布准备 PR 中澄清和验证。
2. **分发准备是 v1 之前的最小发布门槛，不等同于立即发布到所有 registry。** 当前 Go module 位于 `go/`（[go/go.mod:1-7](../../go/go.mod#L1-L7)），但远端不存在 `go/v*` module tag；`go list -m -versions github.com/elkpi/oxa/go` 无可用语义版本。Rust 的 face crate `cargo package` 已实测失败，因为其 path dependency 没有版本约束（如 [rust/crates/oxa-chatcompletions/Cargo.toml:9-16](../../rust/crates/oxa-chatcompletions/Cargo.toml#L9-L16)）。CMake 也没有安装或导出规则（[cpp/CMakeLists.txt:18-38](../../cpp/CMakeLists.txt#L18-L38)）。
3. **正确性 fuzz 优先于跨语言性能 benchmark。** 现有 fuzz 只有 Go SSE decoder（[go/sse/sse_fuzz_test.go:8-64](../../go/sse/sse_fuzz_test.go#L8-L64)），而 M7 流式 tool 聚合要求严格保留空 fragment、顺序和原始输入，且生命周期分歧必须报结构错误（[spec/20-streaming-semantics.md:345-368](../../spec/20-streaming-semantics.md#L345-L368)）。这是高风险、高收益的共同测试投资；benchmark 在缺少统一 workload/指标/隔离环境前只会产生不可比数字。
4. **post-v1 特性必须在 1.0 后规划，且不能笼统命名为“v1.1”。** 规范规定：1.0 后扩展 sealed union 或 enum 属于 major bump（[spec/README.md:44-50](../../spec/README.md#L44-L50)）。Extended Thinking、citation、provider tool 等通常需要新的 IR block/event/loss 表达，因此应预期为 **v2.0.0** 级提案；只增加不改变 sealed union/enum 的可选字段才可能走 v1.x。

## 现状与缺口

### 已满足的发布基础

- 四种实现都已在根 README 的语言矩阵中列为可用，版本分别为 Go pre-v1、Rust 0.1.0、Python 0.2.0、C++ 0.3.0（[README.md:55-73](../../README.md#L55-L73)）。
- C++ 发布记录明确覆盖了 105 个单 face 非流式、12 个 cross-protocol、8 个 stream 向量（[CHANGELOG.md:10-36](../../CHANGELOG.md#L10-L36)）；根 CI 同时运行 vectors、manifest、Go race 测试、Rust lint/test、Python matrix 与 C++ 默认/`-fno-exceptions` 构建（[.github/workflows/ci.yml:53-175](../../.github/workflows/ci.yml#L53-L175)）。
- 所有已列出的规范文档都是 `ready`；唯一计划中的附录是 glossary 90（[spec/README.md:67-82](../../spec/README.md#L67-L82)）。
- 项目已明确源真相顺序为 vectors、schema、Markdown（[spec/README.md:84-99](../../spec/README.md#L84-L99)），适合继续作为四语言的共同正确性基线。

### 需要先收口的缺口

| 缺口 | 证据 | 风险 | 建议归属 |
|---|---|---|---|
| 根 README 仍为 pre-v1，且 Quick Start 只有 Go | [README.md:10-12](../../README.md#L10-L12)、[README.md:92-116](../../README.md#L92-L116) | 发布 1.0 后文档与事实冲突；其他三语言没有获得等价入口 | v1 发布准备 PR |
| “Any face → any face” 的流式单元仍标注 `not yet` | [README.md:81-86](../../README.md#L81-L86) | 与 v1 流式范围（[spec/00-scope-and-architecture.md:21-30](../../spec/00-scope-and-architecture.md#L21-L30)）的可用性叙事不清 | v1 发布准备 PR：增加跨 face composition 测试，或明确 orchestration 是调用方责任 |
| Release checklist 只列 Go 相关的 CI/precondition | [docs/release-checklist.md:7-25](../../docs/release-checklist.md#L7-L25) | 1.0 可能只复核 Go，遗漏 Rust/Python/C++ 的发布断言 | v1 发布准备 PR |
| Go 子模块没有可解析的模块 tag | `go/go.mod` 位于子目录（[go/go.mod:1-3](../../go/go.mod#L1-L3)）；远端无 `go/v*` tag，`go list -m -versions` 无版本 | `go get github.com/elkpi/oxa/go@v1.0.0` 不具备稳定发布路径 | v1 发布准备 PR |
| Rust crates 无法打包 | `cargo package -p oxa-chatcompletions --allow-dirty --no-verify` 报错：`oxa-ir` 未声明版本；依赖是 path-only（[rust/crates/oxa-chatcompletions/Cargo.toml:9-16](../../rust/crates/oxa-chatcompletions/Cargo.toml#L9-L16)） | 不能以 crates.io 包分发；发布流程无法做 dry run | 分发准备 PR |
| C++ 只能源码 add_subdirectory/build，不能标准安装发现 | [cpp/CMakeLists.txt:18-38](../../cpp/CMakeLists.txt#L18-L38) 没有 `install()`、`EXPORT`、`Config.cmake` | 下游 CMake 消费成本高，v1 作为库的可用性不完整 | 分发准备 PR |
| CMake 声明与文档最低版本不一致 | `cmake_minimum_required(VERSION 3.16)`（[cpp/CMakeLists.txt:1](../../cpp/CMakeLists.txt#L1)） vs README 要求 3.20+（[cpp/README.md:29-32](../../cpp/README.md#L29-L32)） | 用户无法确定支持基线 | v1 发布准备 PR（选择一个事实并统一） |
| Python 已有 build metadata 与 `py.typed`，但没有 build/publish 验证 | hatchling/project metadata（[python/pyproject.toml:1-37](../../python/pyproject.toml#L1-L37)）；CI 仅 `unittest`（[.github/workflows/ci.yml:133-147](../../.github/workflows/ci.yml#L133-L147)） | wheel/sdist 的文件和安装行为未被验证 | 分发准备 PR |
| Fuzz 与 benchmark 基线缺失 | 仅发现 Go SSE fuzz（[go/sse/sse_fuzz_test.go:8-64](../../go/sse/sse_fuzz_test.go#L8-L64)）；仓库没有 benchmark 文件 | 流式状态机高风险路径没有随机分片验证；性能讨论不可复现 | 质量工程阶段 |

## 推荐路线

### 阶段 A：v1 发布契约收口（下一项工作，推荐）

**目标：** 不改变协议行为，不增加 feature；消除“已经实现”与“可作为 1.0 对外承诺”的差距。

**必须完成：**

1. 更新 `docs/release-checklist.md`，使 1.0 的 REQUIRED precondition 覆盖四种语言：Go race/vector、Rust fmt/clippy/test、Python lint/type/test/build smoke、C++ default/`-fno-exceptions`/安装 smoke，以及 GitHub tag CI。
2. 解决 README 的流式 cross-face 语义：
   - **推荐：** 在 shared vectest 或每语言 harness 中增加 decoder→IR→encoder 的跨 face 流式 composition 测试，验证 [spec/00-scope-and-architecture.md:72-81](../../spec/00-scope-and-architecture.md#L72-L81) 的 hub-and-spoke 组合；并将 README 的 `not yet` 改为已支持的 composition。
   - 如果不提供 composition helper，则把 README 改为“direct face-to-face orchestration is intentionally caller-owned”；不可继续使用无解释的 `not yet`。
3. 将 `spec/90-glossary.md` 落地为短小、非规范性术语表，或明确其不属于 1.0 shipped scope。当前文字称它应在多语言实现首次需要共同术语时到来（[spec/README.md:79-82](../../spec/README.md#L79-L82)），该条件已满足。
4. 改写根 README：删除 pre-v1 状态，列出四种语言的入口；同步修复过时的 Go README，其中仍称 `ir` 和 converter tests “not yet present”（[go/README.md:5-34](../../go/README.md#L5-L34)）。
5. 为 `github.com/elkpi/oxa/go` 预留/验证子模块 tag 方案：至少在候选发布上创建并验证 `go/v1.0.0` 标签，确保 `go list -m` 与一个临时消费者的 `go get ...@v1.0.0` 工作。

**验收：** 从干净环境执行每种语言的 documented install/build/test；候选 commit 的 CI 与 tag CI 均为绿；四种实现针对同一 manifest 跑完向量；文档不再存在 pre-v1 或 roadmap placeholder 表述。

### 阶段 B：1.0.0 收敛发布

**目标：** 发布项目级正式稳定契约，而不是扩充协议面。

**建议版本策略：**

- 先明确 `v1.0.0` 是项目级 tag 还是所有分发物的收敛版本。鉴于当前 Python/Rust/C++ 的 manifest 版本分别是 `0.2.0`、`0.1.0`、`0.3.0`（[python/pyproject.toml:5-27](../../python/pyproject.toml#L5-L27)、[rust/Cargo.toml:5-10](../../rust/Cargo.toml#L5-L10)、[cpp/CMakeLists.txt:1-5](../../cpp/CMakeLists.txt#L1-L5)），推荐把它们统一提升至 `1.0.0`，以免仓库 Release、包管理器和文档报告不同稳定性。
- 先发布 `v1.0.0-rc.1` 并运行完整 CI/安装 smoke，再发布 `v1.0.0`。这不是新增协议语义，因而不会改变冻结的 IR `specVersion`；当前 IR contract 仍明确独立于 implementation version（[spec/README.md:17-28](../../spec/README.md#L17-L28)）。
- tag/release 仍须遵循现有人工授权流程（[docs/release-checklist.md:27-33](../../docs/release-checklist.md#L27-L33)）。

### 阶段 C：生态分发（紧随 v1，按最小可消费性分两批）

**批次 C1：发布前或 RC 前必须完成的可达性检查**

- Go：使用 `go/v1.0.0(-rc.1)` 子目录语义 tag，并在外部临时 module 中 `go get` 验证。
- Rust：所有将发布到 crates.io 的内部依赖同时写 `path` 和受控 `version` 约束；`cargo package --locked` 对每个公开 crate 成功。保留 `oxa-vectest` 为 test-only crate，避免不必要的公开发布。
- Python：在 CI 运行 `python -m build`（或等价 hatch build）后，在新的 virtualenv 安装 wheel 并 import；现有 `py.typed` 已存在，适合同时验证 typing distribution。
- C++：补充 `install(TARGETS)`、header install、`oxaTargets.cmake` 与 `oxaConfig.cmake`，用一个独立 CMake consumer 执行 `find_package(oxa CONFIG REQUIRED)` smoke test。不要把 `pkg-config` 设为首要目标，除非有已知消费者需求。

**批次 C2：可在 v1 后执行的 registry 自动化**

- PyPI/crates.io/Conan/vcpkg/Homebrew 等分别需要维护者身份、命名、secret 与撤销策略；这些是外部发布治理，不应反向阻塞核心协议稳定性。
- CI 不应因为推送 main 就上传；继续沿用当前“tag/release 人工明确授权”的原则（[docs/release-checklist.md:1-5](../../docs/release-checklist.md#L1-L5)），增设手动 dispatch 或受保护 release workflow。

### 阶段 D：正确性与韧性工程（fuzz 优先，benchmark 后置）

**D1：跨语言性质测试 / fuzz**

1. 定义语言中立的随机测试契约：对一个完整工具参数字符串按随机边界切分，包括空 fragment、UTF-8、转义、`1e+01` 等文本；Decoder 重放后必须使 `ToolUseBlock.input == concat(fragments)`。这直接对应 M7 的 MUST 规则（[spec/20-streaming-semantics.md:345-368](../../spec/20-streaming-semantics.md#L345-L368)）。
2. 先在 Go 实现种子 corpus 和性质，再在 Rust、Python、C++ 复用同一 corpus。语言工具可不同，但输入生成器 seed 与失败最小化后的样本必须回写 `vectors/`（遵循 source-of-truth precedence）。
3. 流状态机负向生成：乱序、重复 terminal、index 跳跃、typed delta 不匹配、unknown block/item。断言错误是 `Status`/`error` 而非 panic/异常或 silent loss。
4. CI 采用短时 deterministic corpus replay；长时间 fuzz 留给 nightly/manual，避免 PR CI 随机和耗时失控。

**D2：基准测试，仅在 D1 后开始**

- 先冻结 workload：每个 face 的最小文本、长多轮、多 image、tool-heavy 非流式、M7 stream；输入来自 vectors 的副本或附带 provenance 的基准 corpus。
- 报告每种语言自己的绝对指标（吞吐、p50/p95、分配/峰值内存），不要将 Python 与 C++ 的“单次 ns”直接作为产品决策；不同 runtime 的启动、GC、编译模式和机器隔离会使横向数字误导。
- 将 benchmark 仅作为回归门槛（同语言、同 runner、阈值），不纳入 protocol conformance 成功标准。

### 阶段 E：post-v1 协议演进

1. 先建立 feature proposal 模板：真实 wire sample、映射表、IR 是否扩展、loss 语义、stream grammar、全部语言的 vector 预算与 migration 结论。
2. 按项目的锁定顺序执行：**spec → schema → vectors/manifest → implementations**（source precedence 见 [spec/README.md:84-123](../../spec/README.md#L84-L123)）。
3. 对候选功能分类：
   - 不新增 sealed union/enum，仅增可选兼容字段：可以规划 v1.x；
   - thinking/reasoning/citation、新 block/event、stop reason：通常需要 sealed union/enum 扩展，按规则规划 v2.0.0，而非模糊的“v1.1”。
4. 不要以“provider parity”为目标突破非目标边界：不做 HTTP、router、auth 或 model capability database（[spec/00-scope-and-architecture.md:32-43](../../spec/00-scope-and-architecture.md#L32-L43)）。

## 优先级与停止条件

| 优先级 | 工作 | 开始条件 | 完成/停止条件 |
|---|---|---|---|
| P0 | 阶段 A：v1 发布契约收口 | 已完成 v0.3.0 | 文档、release checklist、Go module tag、cross-stream 的承诺范围全部一致；若无法定义跨-stream API，明确将 orchestration 归为调用方责任 |
| P1 | 阶段 B：v1.0.0 RC 与正式发布 | P0 全部验收 | RC/tag CI/clean-consumer smoke 全绿；不可因新 feature 延迟 |
| P2 | 阶段 C：分发准备和 registry 发布 | P0 的最小可达性已完成 | 每种公共分发物可被相应生态的干净消费者安装/发现；registry 外部治理失败时不回退已稳定的协议版本 |
| P3 | 阶段 D1：fuzz/property harness | v1 stable tag | 共享 corpus、deterministic replay、随机分片与状态机负例覆盖四种语言；发现行为分歧时回到 spec/vector 层 |
| P4 | 阶段 D2：benchmark | D1 规则稳定 | 固定 workload/runner/指标；仅持续追踪同语言回归 |
| P5 | 阶段 E：feature proposal | v1 稳定且维护能力可用 | 每个 feature 独立评审其 v1.x 或 v2.0.0 兼容性；不得直接实现 |

## 不建议的调整

- **不建议**在 v1 前把 benchmark 作为主工作：没有共同负载和测量协议时，结果难以解释，且不能提高协议正确性。
- **不建议**直接开始 provider 新特性实现：会把冻结版本的发布收口与协议演进交织，增加四语言同步成本。
- **不建议**把 PyPI/crates.io/Conan/vcpkg 的实际上传全部设为 1.0 唯一门槛：外部账户、命名和 secret 是独立治理问题；应先证明每个分发物可打包和可由干净消费者使用。
- **不建议**保留“v1.1 承接任何新 API”的笼统表述：sealed union/enum 的扩展已被版本规则明确划为 major。
