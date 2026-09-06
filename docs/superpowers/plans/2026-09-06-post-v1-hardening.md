# Post-v1 Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 验证 oxa 的四种安装产物可以被源码树外的消费者使用，并把现有流式 tool 聚合测试扩展为四语言可重放的可靠性回归闭环。

**Architecture:** 使用 `ci/consumers/` 保存极小的外部消费者源码，由一个 shell runner 在临时目录构建和运行；消费者只依赖安装/打包产物，不读取仓库内实现路径。使用根目录 `testdata/stream-fragment-corpus.json` 保存语言中立的 tool 参数与分片方案，各语言测试只读取该 corpus 并断言 IR grammar、loss 顺序和 raw argument 拼接结果。

**Tech Stack:** GitHub Actions, POSIX shell, Go 1.23/1.24, Cargo/Rust stable, Python 3.10+, Hatchling/build, CMake 3.20+, C++20, existing oxa public APIs.

**Spec:** `docs/superpowers/specs/2026-09-06-post-v1-hardening-design.md`

## Global Constraints

- 不改变既有协议转换行为、既有 golden vector 预期、`v1.0.0`/`go/v1.0.0` 标签或 `IR specVersion`。
- 行为变化必须遵循 `spec → vectors → implementation`；本计划的 consumer、CI、文档和测试改动不得引入行为变化。
- 生产 face 包只能依赖 IR、modelmap 和该语言标准库/声明的 JSON 库；Rust 运行时依赖必须如实声明为 `serde`/`serde_json`。
- Go fuzz 已存在四个 target；不得重复创建同用途 target，只扩展分片方案和确定性回放。
- 安装验证必须在源码树外运行；不得用 Go `replace`、Python `PYTHONPATH=src`、Cargo workspace path 依赖或 C++ 源码 `add_subdirectory` 冒充 clean consumer。
- consumer 与 fuzz 中间文件必须写入临时目录并在退出时清理；CI 不上传任何 registry、不读取发布 secret。
- 所有脚本使用 `set -euo pipefail`；缺少构建工具时失败而不是静默跳过。
- 生产实现继续使用现有结构错误/loss 边界；可靠性测试不把语义 loss 当结构错误。
- 每个任务独立提交；若 consumer 暴露实现 bug，bugfix 与 CI/夹具提交分开。

---

## 文件与职责映射

- Modify `README.md`: 修正 Rust 版本轴与依赖说明，链接 clean-consumer 验证入口。
- Modify `rust/README.md`: 明确库版本、IR contract 版本和 Rust 运行时依赖。
- Modify `CONTRIBUTING.md`: 将冻结前语言骨架规则替换为 v1 后的行为/新语言贡献门禁。
- Modify `CHANGELOG.md`: 更正 v1.0.0 的依赖声明，保留历史版本条目事实。
- Create `ci/consumers/run.sh`: 在临时根目录准备并运行四个 consumer；不改变源码工作区。
- Create `ci/consumers/go/main.go`: 外部 Go module 使用公开请求和流式 API。
- Create `ci/consumers/python/smoke.py`: 安装后的 Python 包 smoke test。
- Create `ci/consumers/rust/Cargo.toml`, `ci/consumers/rust/src/main.rs`: workspace 外 Rust consumer。
- Create `ci/consumers/cpp/CMakeLists.txt`, `ci/consumers/cpp/main.cpp`: 安装后 `find_package` consumer。
- Modify `cpp/CMakeLists.txt`: 让 `oxa::oxa` 导出 `cxx_std_20` usage requirement。
- Create `testdata/stream-fragment-corpus.json`: 语言中立的合法 tool argument 与分片案例。
- Create `go/internal/vectest/fragment_corpus_test.go`: Go 四面 stream decoder 的确定性 corpus replay。
- Create `python/tests/test_fragment_corpus.py`: Python 三面 stream decoder 的 corpus replay。
- Create `rust/crates/oxa-vectest/tests/fragment_corpus.rs`: Rust 三面 stream decoder 的 corpus replay。
- Create `cpp/tests/test_stream_corpus.cpp`: C++ 三面 stream decoder 的 corpus replay。
- Create `ci/reliability/run.sh`: 在固定预算内运行各语言 corpus replay；不运行无界 fuzz。
- Modify `.github/workflows/ci.yml`: 增加 `consumers` 与 `reliability` jobs。
- Modify `docs/release-checklist.md`: 把持续 consumer/reliability checks 与人工 registry 上传边界写清楚。

## Interfaces

- `ci/consumers/run.sh` accepts environment variables `OXA_ROOT`, `OXA_VERSION`, `GO_VERSION`, and `PYTHON`; exits non-zero on any failed consumer and prints each consumer's command.
- Corpus case shape is a JSON object with `id`, `protocol`, `argument`, and `fragments`; `fragments` concatenates exactly to `argument`.
- Go replay helper loads the corpus from the repository root and invokes each face's existing `StreamDecoder.Feed`/`Flush` API; it validates `ir.ValidateEventStream` and compares each `ToolUseBlock.Input` to `argument`.
- Python replay uses each face's existing `StreamDecoder.feed`/`flush` API and `oxa.ir.validate_event_stream`.
- Rust replay uses each face's existing `StreamDecoder::feed`/`flush` API and `oxa_ir::validate_event_stream`.
- C++ replay uses each face's existing `StreamDecoder::feed`/`flush` API and `oxa::ir::validate_event_stream`.

---

### Task 1: Correct post-v1 documentation and dependency claims

**Files:**
- Modify: `README.md:120-124`
- Modify: `rust/README.md:1-55`
- Modify: `CONTRIBUTING.md:35-40`
- Modify: `CHANGELOG.md:14-24`
- Test: `git diff --check`, targeted text/metadata assertions

**Interfaces:**
- Consumes: current package manifests and `docs/superpowers/specs/2026-09-06-post-v1-hardening-design.md`.
- Produces: truthful version/dependency/install documentation with no protocol behavior change.

- [ ] **Step 1: Write the documentation assertions before editing**

Create a temporary shell check outside the repository that asserts the final source text contains `Rust library version 1.0.0`, `IR contract specVersion 0.1.0`, `serde`, `serde_json`, and `v1` contribution language, and does not contain the exact stale sentence `pure in-process conversion crates targeting spec 0.1.0 baseline`.

- [ ] **Step 2: Run the assertions to verify they fail**

Run:

```bash
bash /tmp/oxa-doc-assertions.sh
```

Expected: non-zero because the current root README still has the stale Rust sentence and the zero-dependency claim in `rust/README.md`.

- [ ] **Step 3: Apply the minimal documentation changes**

Use these exact semantic replacements:

```markdown
- **Rust**: workspace in [`rust/`](rust/README.md), library version `1.0.0`; its IR contract remains `specVersion: 0.1.0` and production crates use `serde` and `serde_json`.
- **Python**: PEP 621 package in [`python/`](python/README.md), pure Python standard library with zero runtime dependencies.
- **C++**: standard C++20 library in [`cpp/`](cpp/README.md), zero third-party runtime dependencies and exception-free error handling.
```

Replace Rust README's zero-runtime-dependency sentence with:

```markdown
- **Runtime dependencies:** `serde` and `serde_json`; development-only vector tooling may add test dependencies.
```

Replace the pre-freeze contribution paragraph with a v1-frozen rule: behavior changes require spec/vector/implementation ordering, and new-language contributions require a tracked compatibility and consumer test plan.

Change the v1.0.0 changelog bullet to say that Go/Python/C++ have no third-party runtime dependency and Rust uses `serde`/`serde_json`.

- [ ] **Step 4: Run the assertions and metadata checks**

Run:

```bash
bash /tmp/oxa-doc-assertions.sh
git diff --check
```

Expected: both commands exit 0; only documentation files differ.

- [ ] **Step 5: Commit**

```bash
git add README.md rust/README.md CONTRIBUTING.md CHANGELOG.md
git commit -m "docs: correct post-v1 dependency and version claims"
```

---

### Task 2: Add isolated consumer fixtures

**Files:**
- Create: `ci/consumers/go/main.go`
- Create: `ci/consumers/python/smoke.py`
- Create: `ci/consumers/rust/Cargo.toml`
- Create: `ci/consumers/rust/src/main.rs`
- Create: `ci/consumers/cpp/CMakeLists.txt`
- Create: `ci/consumers/cpp/main.cpp`
- Create: `ci/consumers/run.sh`
- Test: each fixture's own compile/run command from a temporary directory

**Interfaces:**
- Consumes: installed or packaged artifacts supplied by `run.sh`.
- Produces: non-zero exit on failed import/link/conversion; no source-tree fallback.

- [ ] **Step 1: Write failing fixture checks**

Add a temporary test that runs `ci/consumers/run.sh` with an empty artifact directory and asserts it fails before any consumer reports success.

- [ ] **Step 2: Implement Go fixture and local module proxy**

`go/main.go` must import `github.com/elkpi/oxa/go/openai/chatcompletions`, construct a request with one user message, call `DecodeRequest`, assert no error and at least one IR message, then instantiate `StreamDecoder`, feed a role chunk, a text/tool chunk sequence from an existing stream vector, call `Flush`, and assert non-empty events.

`run.sh` must create a file-based Go module proxy containing the checked-out `go/` module under version `v1.0.0`, set `GOPROXY=file://...` and `GOSUMDB=off`, initialize a new module in `$RUNNER_TEMP`, run `go get github.com/elkpi/oxa/go@v1.0.0`, and run the fixture from outside `$OXA_ROOT`.

- [ ] **Step 3: Implement Python wheel/sdist fixture**

`smoke.py` must run outside the repository, import `oxa`, assert `oxa.__version__ == "1.0.0"`, construct `oxa.openai.chatcompletions` input using its documented `decode_request` function, and run a `StreamDecoder` with a valid text stream. The runner must build both `--wheel` and `--sdist`, create separate virtual environments, install each artifact with `--no-deps`, and invoke the script without `PYTHONPATH`.

- [ ] **Step 4: Implement Rust packaged consumer fixture**

The fixture's `Cargo.toml` must depend only on the public `oxa-chatcompletions` and `oxa-ir` crates at `1.0.0`; `src/main.rs` must construct the exported `oxa_chatcompletions::types::Request`, call `decode_request` with `Config::default()`, assert the IR has one message, and print a success marker. `run.sh` must package each public crate, extract the selected crate archive outside the workspace, and build the consumer against extracted package directories; it must not add `oxa-vectest` as a dependency.

- [ ] **Step 5: Implement C++ installed consumer fixture**

`cpp/main.cpp` must include `oxa/openai/chatcompletions.hpp`, parse a small request with `decode_request(std::string_view)`, assert `ok()`, and return 0. `cpp/CMakeLists.txt` must call `find_package(oxa CONFIG REQUIRED)`, build with `add_executable`, link `oxa::oxa`, and not set `CMAKE_CXX_STANDARD`. The runner must configure/build the library, install it to a temporary prefix, configure the consumer with `CMAKE_PREFIX_PATH`, build, and run it.

- [ ] **Step 6: Run all fixture checks from a clean temporary directory**

Run:

```bash
OXA_ROOT="$PWD" bash ci/consumers/run.sh
```

Expected: Go, Rust, Python wheel, Python sdist, and C++ installed consumer each print a success marker and exit 0; the current repository remains unchanged except for intended fixture files.

- [ ] **Step 7: Commit**

```bash
git add ci/consumers
 git commit -m "test: add isolated downstream consumer smoke fixtures"
```

---

### Task 3: Make C++20 usage requirements transitive

**Files:**
- Modify: `cpp/CMakeLists.txt:18-30`
- Test: `ci/consumers/cpp` installed consumer

**Interfaces:**
- Consumes: existing `oxa` target and package export.
- Produces: exported `oxa::oxa` target with a public C++20 compile requirement.

- [ ] **Step 1: Add consumer test without a manual standard flag**

Run the C++ consumer from Task 2 with no `CMAKE_CXX_STANDARD` setting and record the configure/build result. If it fails because the compiler defaults below C++20, keep the failure as the regression signal; if it passes, inspect the compile command to confirm the target requirement is already transitive.

- [ ] **Step 2: Add the minimal target feature**

Immediately after `add_library(oxa ...)`, add:

```cmake
target_compile_features(oxa PUBLIC cxx_std_20)
```

Do not remove the project-wide standard settings; the target feature is required for installed consumers.

- [ ] **Step 3: Re-run the installed consumer**

Run:

```bash
OXA_ROOT="$PWD" CONSUMER_ONLY=cpp bash ci/consumers/run.sh
```

Expected: the external consumer configures, builds, links, and runs without a manual standard flag.

- [ ] **Step 4: Commit**

```bash
git add cpp/CMakeLists.txt
 git commit -m "fix(cpp): export C++20 requirement to consumers"
```

---

### Task 4: Add shared deterministic stream-fragment corpus

**Files:**
- Create: `testdata/stream-fragment-corpus.json`
- Create: `go/internal/vectest/fragment_corpus_test.go`
- Create: `python/tests/test_fragment_corpus.py`
- Create: `rust/crates/oxa-vectest/tests/fragment_corpus.rs`
- Create: `cpp/tests/test_stream_corpus.cpp`
- Test: four language replay tests

**Interfaces:**
- Consumes: native `StreamDecoder` APIs and corpus cases.
- Produces: deterministic tests asserting `concat(fragments) == ToolUseBlock.input`, contiguous block indexes, valid event grammar, and ordered losses.

- [ ] **Step 1: Add corpus schema and cases**

Create `testdata/stream-fragment-corpus.json` with exactly this top-level shape and at least four cases:

```json
{
  "version": 1,
  "cases": [
    {
      "id": "ascii-empty-middle",
      "protocol": "chatcompletions",
      "argument": "{\"a\":1}",
      "fragments": ["{\"a\"", "", ":1}"]
    },
    {
      "id": "unicode-escaped",
      "protocol": "responses",
      "argument": "{\"text\":\"caf\\u00e9\"}",
      "fragments": ["{\"text\":\"caf", "\\u00e9", "\"}"]
    },
    {
      "id": "exponent-and-empty",
      "protocol": "anthropic",
      "argument": "{\"value\":1e+01}",
      "fragments": ["{\"value\":", "", "1e+01}"]
    },
    {
      "id": "nested-escaped",
      "protocol": "chatcompletions",
      "argument": "{\"x\":{\"quote\":\"\\\\\\\"\"}}",
      "fragments": ["{\"x\":{", "\"quote\":", "\"\\\\\\\"\"}}"]
    }
  ]
}
```

Before tests consume it, validate every case has a supported protocol and `"".join(fragments) == argument`.

- [ ] **Step 2: Add the Go replay test skeleton and run it red**

Create the test with a `loadFragmentCorpus` helper declaration and one table-driven test that calls it. Run `go test ./internal/vectest -run FragmentCorpus -count=1`; expected: compilation failure because the loader and protocol-specific chunk builders are not implemented yet. Keep this failure local to the new test and do not change production code.

- [ ] **Step 3: Implement the Go replay test**

Load the corpus by walking from `go/` to the first parent containing both `.git` and `testdata/`; select cases by protocol. Use the existing stream vector chunk types and constructors; do not add a production API. Preserve empty fragments as actual deltas, call `Flush`, run `ir.ValidateEventStream`, compare the first `ToolUseBlock.Input` to `argument`, and assert the full ordered event stream includes one `InputJSONDelta` per corpus fragment.

- [ ] **Step 4: Add Python replay test**

Use `json.load`, locate the repository root from `Path(__file__).parents`, build each face's existing wire chunk dictionaries, call `feed` for every fragment and `flush`, validate through `oxa.ir.validate_event_stream`, and compare `ToolUseBlock.input` to `argument`. Run `python3 -m unittest tests/test_fragment_corpus.py` from `python/`.

- [ ] **Step 5: Add Rust replay test**

Use `serde_json` to load the corpus, deserialize native chunk structures, call each face's `StreamDecoder::feed` and `flush`, run `oxa_ir::validate_event_stream`, and compare the decoded `Block::ToolUse.input` to `argument`. Make the test skip with a clear message when no repository root is available, matching the existing vectest convention.

- [ ] **Step 6: Add C++ replay test**

Load the corpus through the existing JSON helper, construct native chunks with `json::parse`, call each face decoder, validate `oxa::ir::validate_event_stream`, and compare `ToolUseBlock.input`. Register the test in the existing CTest loop.

- [ ] **Step 7: Run all four replay suites**

Run:

```bash
(cd go && go test ./... -run FragmentCorpus -count=1)
(cd python && python3 -m unittest tests/test_fragment_corpus.py)
(cd rust && cargo test -p oxa-vectest fragment_corpus -- --nocapture)
cmake --build cpp/build --parallel && ctest --test-dir cpp/build -R test_stream_corpus --output-on-failure
```

Expected: every corpus case passes in all four implementations; no existing vector changes occur.

- [ ] **Step 8: Commit**

```bash
git add testdata/stream-fragment-corpus.json go/internal/vectest/fragment_corpus_test.go python/tests/test_fragment_corpus.py rust/crates/oxa-vectest/tests/fragment_corpus.rs cpp/tests/test_stream_corpus.cpp cpp/CMakeLists.txt
 git commit -m "test: add deterministic stream fragmentation replay"
```

---

### Task 5: Add bounded reliability replay to CI

**Files:**
- Create: `ci/reliability/run.sh`
- Modify: `.github/workflows/ci.yml:179`
- Test: `bash ci/reliability/run.sh`

**Interfaces:**
- Consumes: `testdata/stream-fragment-corpus.json` and four language replay suites.
- Produces: deterministic, bounded CI status; no unbounded fuzz process.

- [ ] **Step 1: Write the reliability runner contract test**

Assert that the runner contains `set -euo pipefail`, invokes all four language commands, and rejects `-fuzztime=0`, `while true`, `tail -f`, and missing corpus paths.

- [ ] **Step 2: Implement the bounded runner**

`ci/reliability/run.sh` must set `ROOT`, verify the corpus file, run the Go replay test, Python unittest, Rust package test, and C++ named CTest. It must accept `OXA_RELIABILITY_ROOT` but default to the repository root and remove any temporary build directories it creates with a trap. It must not invoke registry tools or a long-running fuzz process.

- [ ] **Step 3: Add the GitHub Actions job**

Add a `reliability` job after `consumers`, using the same checkout and language setup actions already present. Run the bounded runner with a job-level timeout of 10 minutes. Keep existing fuzz targets in normal Go tests; this job is deterministic replay, not random fuzz exploration.

- [ ] **Step 4: Run the job locally**

Run:

```bash
bash ci/reliability/run.sh
```

Expected: all four replay suites pass within the ten-minute budget; if a required tool is missing, the command fails with its installation/selection message.

- [ ] **Step 5: Commit**

```bash
git add ci/reliability/run.sh .github/workflows/ci.yml
 git commit -m "ci: run bounded reliability replay"
```

---

### Task 6: Document the post-v1 verification workflow

**Files:**
- Modify: `docs/release-checklist.md:15-26`
- Modify: `README.md:126-135`
- Test: markdown link/path and version checks

**Interfaces:**
- Consumes: final consumer/reliability job names and commands.
- Produces: documentation that matches executable CI behavior and states registry uploads are separately authorized.

- [ ] **Step 1: Add release checklist assertions**

Require the checklist to name `consumers` and `reliability`, state that consumer checks install artifacts outside the source tree, and state that registry uploads are not performed by PR/main CI.

- [ ] **Step 2: Update README links**

Add a “Downstream verification” subsection linking to `docs/release-checklist.md` and the four language consumer fixture directories; describe Rust dependencies accurately and keep the public quick starts minimal.

- [ ] **Step 3: Validate documentation against files**

Run:

```bash
python3 - <<'PY'
from pathlib import Path
for path in ["README.md", "rust/README.md", "CONTRIBUTING.md", "CHANGELOG.md", "docs/release-checklist.md"]:
    assert Path(path).is_file(), path
text = Path("docs/release-checklist.md").read_text()
for marker in ("consumers", "reliability", "registry"):
    assert marker in text, marker
PY
git diff --check
```

Expected: exit 0 and no broken links introduced.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/release-checklist.md
 git commit -m "docs: document post-v1 hardening workflow"
```

---

### Task 7: Full verification and handoff

**Files:**
- No source changes expected.
- Verify: all files and commits from Tasks 1–6.

**Interfaces:**
- Consumes: branch history and all task outputs.
- Produces: a verified branch summary; no release tag or registry upload.

- [ ] **Step 1: Run repository gates**

```bash
make test
make lint
make vectors
(cd rust && cargo fmt --all -- --check && cargo clippy --workspace --all-targets -- -D warnings && cargo test --workspace)
(cd python && python3 -m unittest discover -s tests)
cmake -B cpp/build -S cpp -DCMAKE_BUILD_TYPE=Debug
cmake --build cpp/build --parallel
ctest --test-dir cpp/build -C Debug --output-on-failure
```

Expected: every command exits 0; report exact failures instead of claiming completion if any command fails.

- [ ] **Step 2: Run clean consumers and reliability replay again**

```bash
OXA_ROOT="$PWD" bash ci/consumers/run.sh
bash ci/reliability/run.sh
```

Expected: all four consumer families and all four replay suites exit 0 from outside their source roots.

- [ ] **Step 3: Verify branch, diff, and release boundaries**

```bash
git diff --check
git status --short --branch
git log --oneline --decorate -8
git tag --points-at HEAD
git grep -nE 'go get .*@|python -m build|cargo publish|twine upload|conan upload|vcpkg' -- .github ci docs README.md || true
```

Expected: branch is `post-v1-hardening`, worktree is clean after commits, no new release tag points at the branch, and no registry upload command is present.

- [ ] **Step 4: Final report**

Report the branch name, commit list, exact verification commands/results, any external registry work intentionally not performed, and any remaining issue with a reproducible command. Do not claim a GitHub PR, remote push, package registry publication, or new release until separately requested and freshly verified.
