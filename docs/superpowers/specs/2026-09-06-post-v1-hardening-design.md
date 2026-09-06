# oxa v1 发布后加固设计

**日期：** 2026-09-06  
**基线：** `81e07de86af26eba7fb5d8b97f5ef1e3ec2243ac` (`v1.0.0`)  
**分支：** `post-v1-hardening`

## 目标

在不改变既有协议转换行为、不移动 `v1.0.0` 标签、也不改变 IR contract `specVersion` 的前提下，把 oxa 从“源码树内测试通过”加固为“安装产物可被外部消费者验证”，并把已有 Go fuzz 能力扩展为可重放的可靠性回归闭环。

本设计只覆盖仓库内可以独立验证的工程改进。PyPI、crates.io、Conan、vcpkg 等实际 registry 上传不在本分支执行，因为它们需要外部账号、包名确认、secret 和明确发布授权。

## 非目标

- 不增加新的协议字段、IR block、IR event、loss reason 或 stream grammar。
- 不修改 `vectors/` 中既有行为真值；只有在可靠性测试发现已被规范明确要求但缺少固定回归的输入时，才添加不改变既有预期的回归样例。
- 不把 Rust 的 `serde`/`serde_json` 误报为零第三方运行时依赖。
- 不把现有四个 Go fuzz target 重写成另一套重复 harness。
- 不要求不同协议的原始网络 chunk 边界相同；流式比较继续以规范化 IR、loss 顺序和 raw tool input 为准。

## 现状约束

1. 当前 CI 已运行四种语言的源码树测试、向量检查、lint 和 C++ 多平台构建，但没有稳定的 wheel/sdist 安装、Rust package 消费、Go 外部 module 或 CMake 安装后 consumer 门禁。
2. C++ 已有 `install(TARGETS)`、导出 target 和 `oxaConfig.cmake`；Python 已有 Hatchling wheel 配置；Rust 生产依赖已有 workspace 版本约束。因此本分支验证和修补消费路径，不重复搭建已有包装能力。
3. Go 已有 SSE、Chat Completions、Responses、Anthropic 四个 fuzz target。现有 tool 参数测试的分片策略仍较固定，CI 只运行普通测试，不执行有预算的 fuzz 探索。
4. 版本有三个独立概念：实现包版本、书面 spec 版本、IR contract `specVersion`。文档必须分别表述。

## 设计方案

### 1. 文档契约层

修正根 README、Rust README、CHANGELOG 和 CONTRIBUTING 中与 v1 发布后事实不一致的文案：

- 明确 Rust 运行时依赖为 `serde` 和 `serde_json`，Go/Python/C++ 的依赖表述不替代实际 manifest。
- 将根 README 的 Rust “spec 0.1.0 baseline”改成同时说明库版本 `1.0.0` 与 IR contract `specVersion: 0.1.0`，避免把两个版本轴混为一谈。
- 将冻结前“暂不接受语言骨架”的贡献规则更新为 v1 后的行为变更与新语言贡献约束。
- 给每种语言提供与 clean-consumer smoke 对齐的最小入口描述。

文档改动与实现/CI 改动分开提交，避免行为审查者误以为协议契约发生变化。

### 2. Clean-consumer 夹具

为每种语言建立隔离的临时消费者测试，优先使用 shell 驱动和极小的示例源码，夹具只写入 runner 的临时目录，不进入源码包的运行路径：

- **Go：** 从独立 module 执行 `go get`/构建目标 module，在源码 checkout 场景用明确的 commit/module 坐标，不使用仓库内 `replace`；运行一个公开 request converter 和 stream converter。
- **Rust：** 对可发布 crate 执行 package dry run，并在 workspace 外使用生成的归档或明确版本来源构建小 consumer；`oxa-vectest` 只作为不可发布测试 harness，不成为生产 consumer 依赖。
- **Python：** 使用构建工具生成 wheel 和 sdist，在独立 virtualenv 分别安装，再运行 `import oxa` 和最小转换调用；consumer 不设置 `PYTHONPATH`。
- **C++：** 安装到临时 prefix，在另一个 CMake 项目中执行 `find_package(oxa CONFIG REQUIRED)`，链接 `oxa::oxa`，consumer 不手工设置 C++ 标准；必要时给 library target 添加公开的 `cxx_std_20` compile feature，使要求通过导出 target 传递。

所有 consumer 夹具只断言已存在的公开 API、一个非流式转换和一个流式 tool 转换，不复制完整 vectors harness。

### 3. CI 门禁

在现有 workflow 中加入独立的 `consumers` job，避免把安装验证隐藏在已有源码测试 job 中。该 job：

- 在 Linux runner 上覆盖 Go、Rust、Python、C++ 四种 consumer；
- 复用现有语言版本和 CMake 版本下限；
- 在 `$RUNNER_TEMP` 或等价临时目录创建消费者；
- 使用 `set -euo pipefail`，每个生态步骤失败即失败；
- 不上传任何 registry，不读取发布 secret；
- 在 PR、main push 和 tag push 上执行，tag 只是额外验证，不触发上传。

CI 的包装工具属于构建依赖，不改变“生产 crate/package 的运行时依赖”声明。

### 4. 可靠性回放

在 Go 现有 fuzz target 之上加入确定性回放层：

- 共享 corpus 记录原始 tool argument、分片边界/空片段以及协议面；
- replay 断言聚合后的 raw input 与片段拼接字符串完全一致，并验证合法事件生命周期；
- 分片生成覆盖整串、单点、多段、空片段、Unicode rune 边界和 JSON 转义，不把 UTF-8 字节切断混入已解析字符串的分片契约；
- 现有 fuzz target 保持不变，增加短时、明确预算的 CI replay；长时间随机探索留在手动/nightly 入口；
- 只有确认符合规范的最小失败样例才回写为 vector 或共享 corpus；不把随机输出直接当 golden 真值。

四种语言首先共享输入与断言语义；如果本次实现成本允许，再为 Rust/Python/C++ 添加同一 corpus 的最小 replay，不引入四套互不兼容的随机生成器。

### 5. 版本和发布声明

将版本校验做成文档/CI 可验证断言：

- 项目实现版本保持 `1.0.0`，除非发现兼容性 bug 需要另行发布补丁。
- spec 版本保持 `1.0.0`。
- IR contract `specVersion` 继续保持 schema 规定的版本，不因实现加固升版。
- 本分支不创建或覆盖任何 release tag。

## 错误处理与安全边界

- Consumer 失败必须暴露完整构建/安装日志；不能将源码路径 fallback 当作通过。
- 缺少可选工具时，CI job 应明确失败并显示安装步骤，而不是静默跳过包装验证。
- 任何外部输入继续经过现有结构错误和 loss 边界；可靠性测试不能把语义不可映射误判为结构错误。
- 临时 consumer、virtualenv、打包归档和 fuzz 中间文件均在 runner 结束时清理，不写入工作区。

## 验证策略

本地验证分层执行：

1. 文档/配置静态检查：版本、依赖声明、路径与 workflow YAML 语法。
2. 既有门禁：`make test`、`make lint`、`make vectors`；Rust fmt/clippy/test；Python 单测；C++ CTest。
3. 包装验证：Go 外部 module、Rust package/consumer、Python wheel/sdist、CMake install/consumer。
4. 可靠性验证：共享 corpus replay，现有 Go fuzz target 的有限预算运行，以及工具参数原文和事件 grammar 断言。
5. 完成前检查：`git diff --check`、工作区状态、分支提交边界和未执行的 registry 操作说明。

## 提交边界

建议使用以下独立提交，任何一个提交都应可单独审查：

1. `docs: correct post-v1 dependency and version claims`
2. `test: add isolated downstream consumer smoke fixtures`
3. `ci: verify installed packages with clean consumers`
4. `test: add deterministic stream fragmentation replay`
5. `ci: run bounded reliability replay`
6. `docs: document post-v1 hardening workflow`

如果某个 consumer 验证暴露真实实现缺陷，修复必须拆成单独的 bugfix 提交，并附直接回归测试；不把它隐藏在 CI 提交中。
