# Cross-Language Vector Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the Rust, Python, and C++ implementations back in line with the 130 golden vectors so every `ci.yml` job is green again after the TypeScript remediation vectors landed.

**Architecture:** The vectors already define the contract (`vectors/responses/stream/skipped-part-loss.json` et al.); Go and TypeScript pass all 130. The only semantic gap is the Responses stream decoder's handling of **unknown event types** in Rust, Python, and C++: they unconditionally record a `path="type"` loss, where the contract requires absorbing identity-matching descendants of an active skipped unit (N-S-3) and validating identity otherwise. Python and C++ additionally hardcode golden-vector totals in tests that went stale (125 → 130 vectors). Each language mirrors the reference logic already shipped in `ts/src/openai/responses/stream.ts` (`#hasNativeUnitIdentity`, `#requireUnknownEventIdentity`, `#requireSkippedDescendant`, `stream.ts:104-121,504-543`).

**Tech Stack:** Rust 2021 (`cargo test/clippy/fmt`), Python 3.10+ (`unittest`, run with `PYTHONPATH=src`), C++20 (CMake + CTest).

**Spec:** `spec/20-streaming-semantics.md` (N-S-3 loss containment, N-S-6 lifecycle) and `docs/superpowers/specs/2026-09-16-typescript-release-blocker-remediation-design.md` §4 (skipped-unit containment, already implemented in TS). Reference implementation: `ts/src/openai/responses/stream.ts`.

## Global Constraints

- Vectors are the sole behavioral contract: this plan changes **no vector, no spec, no manifest** — implementations catch up to `vectors/` as-is.
- Unknown stream events follow the TS contract exactly: an event carrying any of `output_index` / `item_id` / `content_index` while a skipped unit is active is absorbed silently when its identity matches (item identity always; content index additionally when the skip is part-level), and is a structural stream error (`ValueError` / `Err` / `Status` per language) when it does not. With no skipped unit active, an identity-bearing unknown event must still validate against the open supported item/part and then record exactly one `path="type"` loss; an identity-less unknown event records exactly one `path="type"` loss unchanged.
- Known-descendant handling, usage parsing, nonstream conversion, and loss records other than the unknown-event branch are out of scope — they already pass all 130 vectors in all languages.
- Registry publication (PyPI/crates.io 1.0.1 patch releases) is **out of scope** for this plan; it is a separate decision after CI is green.
- Keep commits narrowly cherry-pickable: one language per commit boundary; test-count refreshes separate from behavior fixes.
- Execute in an isolated worktree on branch `vector-alignment` created from `main` (currently `2b36b61`).

---

## File Map

- `python/tests/test_anthropic_vectors.py:26` and `python/tests/test_stream_vectors.py:60` — stale vector-count tripwires (30→31, 8→12).
- `python/src/oxa/openai/responses/streamin.py` (~line 408, final fallback in `feed`) — unknown-event branch gains skipped-unit absorption and identity validation.
- `python/tests/test_stream_unit.py` — focused regression tests for absorption, identity mismatch, and identity-less loss.
- `rust/crates/oxa-responses/src/streamin.rs:461-472` (`_ =>` arm of the `feed` match) — same change in Rust.
- `rust/crates/oxa-responses/tests/stream.rs` — same regressions using the existing `StreamEvent` builder helpers.
- `cpp/tests/test_anthropic.cpp:42`, `cpp/tests/test_vectors.cpp:76`, `cpp/tests/test_stream.cpp:94,109,124` — stale count tripwires (30→31, 30→31, 2→3, 3→4, 3→5).
- `cpp/src/openai/responses.cpp:1140-1144` (tail of `StreamDecoder::feed`) — same change in C++.

### Task 1: Python alignment

**Files:**
- Modify: `python/src/oxa/openai/responses/streamin.py` (fallback branch at the end of `feed`)
- Modify: `python/tests/test_anthropic_vectors.py:26`, `python/tests/test_stream_vectors.py:60`
- Modify: `python/tests/test_stream_unit.py` (append tests to `StreamUnitTests`)

**Interfaces:**
- Consumes: `StreamDecoder.feed(ev: dict, raw_text: str = "") -> list[Event]`, `self._require_active_item(ev, event_type)`, decoder state `_skipped_item`, `_skipped_part`, `_item_open`, `_block_open`, `_output_index`, `_item_id`, `_content_index`, `loss(path, field, reason, detail)`, `LOSS_UNSUPPORTED_SEMANTIC`.
- Produces: behavior matching `vectors/responses/stream/skipped-part-loss.json`; no new public API.

- [ ] **Step 1: Refresh the stale count tripwires (turns the semantic gap RED)**

```python
# python/tests/test_anthropic_vectors.py:26
self.assertEqual(len(vectors), 31, "expected 31 nonstream anthropic vectors")
# python/tests/test_stream_vectors.py:60
self.assertEqual(total_vectors, 12, "expected 12 total stream vectors across all faces")
```

- [ ] **Step 2: Run stream vectors and capture the RED**

Run: `cd python && PYTHONPATH=src python3 -m unittest tests.test_stream_vectors -v`
Expected: FAIL on `responses.stream.skipped-part-loss` with `unexpected loss reported: path='type' field='type' reason='unsupported-semantic'` (the decoder emits a second loss for `response.output_image.delta`).

- [ ] **Step 3: Commit the count refresh**

```bash
git add python/tests/test_anthropic_vectors.py python/tests/test_stream_vectors.py
git commit -m "test(python): refresh golden vector counts"
```

- [ ] **Step 4: Write the failing unit tests**

Append to `StreamUnitTests` in `python/tests/test_stream_unit.py`:

```python
    def _responses_skipped_part_open(self) -> ResponsesStreamDecoder:
        dec = ResponsesStreamDecoder()
        dec.feed(
            {
                "type": "response.created",
                "response": {"id": "r1", "object": "response", "status": "in_progress", "model": "m", "output": []},
            }
        )
        dec.feed(
            {
                "type": "response.output_item.added",
                "output_index": 0,
                "item": {"type": "message", "id": "i1", "role": "assistant", "status": "in_progress", "content": []},
            }
        )
        dec.feed(
            {
                "type": "response.content_part.added",
                "item_id": "i1",
                "output_index": 0,
                "content_index": 0,
                "part": {"type": "output_image"},
            }
        )
        return dec

    def test_responses_unknown_descendant_of_skipped_part_is_absorbed(self) -> None:
        dec = self._responses_skipped_part_open()
        events = dec.feed(
            {"type": "response.output_image.delta", "item_id": "i1", "output_index": 0, "content_index": 0, "delta": "x"}
        )
        self.assertEqual(events, [])
        self.assertEqual(len(dec.losses()), 1)
        self.assertEqual(dec.losses()[0].path, "output[0].content[0]")
        self.assertEqual(dec.losses()[0].reason, "unsupported-semantic")

    def test_responses_unknown_descendant_with_wrong_item_is_error(self) -> None:
        dec = self._responses_skipped_part_open()
        with self.assertRaises(ValueError) as cm:
            dec.feed(
                {"type": "response.output_image.delta", "item_id": "other", "output_index": 0, "content_index": 0, "delta": "x"}
            )
        self.assertIn("does not match", str(cm.exception))
        self.assertEqual(len(dec.losses()), 1)

    def test_responses_identity_less_unknown_event_records_one_loss(self) -> None:
        dec = ResponsesStreamDecoder()
        dec.feed(
            {
                "type": "response.created",
                "response": {"id": "r1", "object": "response", "status": "in_progress", "model": "m", "output": []},
            }
        )
        events = dec.feed({"type": "response.weird_event"})
        self.assertEqual(events, [])
        self.assertEqual(len(dec.losses()), 1)
        self.assertEqual(dec.losses()[0].path, "type")
```

- [ ] **Step 5: Run unit tests and capture the RED**

Run: `cd python && PYTHONPATH=src python3 -m unittest tests.test_stream_unit -v`
Expected: the two new skipped-part tests FAIL (`len(dec.losses())` is 2, not 1; wrong-item feed does not raise). The identity-less test PASSES already — it pins current behavior against regressions.

- [ ] **Step 6: Implement the guarded unknown-event branch**

Replace the unconditional fallback at the end of `feed` in `python/src/oxa/openai/responses/streamin.py` (currently the `self._losses.append(loss("type", "type", ...))` block at ~line 408) with:

```python
        # Unknown event types: absorb identity-matching descendants of an
        # active skipped unit (N-S-3); validate identity against the open
        # supported unit otherwise; always keep at most one loss per event.
        has_identity = (
            "output_index" in ev or "item_id" in ev or "content_index" in ev
        )
        if self._skipped_item or self._skipped_part:
            if has_identity:
                self._require_active_item(ev, kind)
                if self._skipped_part and not self._skipped_item:
                    content_index = int(ev.get("content_index", -1))
                    if content_index != self._content_index:
                        raise ValueError(
                            f"responses: {kind} content_index {content_index} does not match the skipped part"
                        )
                return []
        elif has_identity:
            self._require_active_item(ev, kind)
            if self._block_open:
                content_index = int(ev.get("content_index", -1))
                if content_index != self._content_index:
                    raise ValueError(
                        f"responses: {kind} content_index {content_index} does not match the open content part"
                    )
            elif "content_index" in ev:
                raise ValueError(f"responses: {kind} has no open content part")
        self._losses.append(
            loss(
                "type",
                "type",
                LOSS_UNSUPPORTED_SEMANTIC,
                f"Responses stream event type {kind!r} is not decoded in the Responses stream profile",
            )
        )
        return []
```

- [ ] **Step 7: Run the full Python suite (GREEN)**

Run: `cd python && PYTHONPATH=src python3 -m unittest discover -s tests`
Expected: all tests pass, including all 12 stream vectors and the three new unit tests.

- [ ] **Step 8: Commit**

```bash
git add python/src/oxa/openai/responses/streamin.py python/tests/test_stream_unit.py
git commit -m "fix(python): absorb responses unknown stream descendants"
```

### Task 2: Rust alignment

**Files:**
- Modify: `rust/crates/oxa-responses/src/streamin.rs:461-472` (the `_ =>` arm of the `feed` match)
- Modify: `rust/crates/oxa-responses/tests/stream.rs` (append tests)

**Interfaces:**
- Consumes: `StreamDecoder::feed(&mut self, ev: &StreamEvent) -> Result<Vec<Event>, Error>`, `self.require_active_item(ev, event_type: &str)`, `StreamEvent { kind: String, output_index: Option<i64>, item_id: Option<String>, content_index: Option<i64>, .. }`, decoder fields `skipped_item`, `skipped_part`, `block_open`, `content_index`, and `loss(path, field, reason, detail)`.
- Produces: behavior matching `skipped-part-loss.json`; no new public API.

- [ ] **Step 1: Write the failing tests**

Append to `rust/crates/oxa-responses/tests/stream.rs` (reuse `stream_created` / `stream_item_added` helpers; construct the image part and unknown event inline):

```rust
fn stream_image_part_added(output_index: i64, content_index: i64, item_id: &str) -> StreamEvent {
    StreamEvent {
        kind: "response.content_part.added".to_string(),
        item_id: Some(item_id.to_string()),
        output_index: Some(output_index),
        content_index: Some(content_index),
        part: Some(OutputPart {
            kind: "output_image".to_string(),
            text: String::new(),
            annotations: Vec::new(),
        }),
        ..Default::default()
    }
}

fn unknown_descendant(output_index: i64, content_index: i64, item_id: &str) -> StreamEvent {
    StreamEvent {
        kind: "response.output_image.delta".to_string(),
        item_id: Some(item_id.to_string()),
        output_index: Some(output_index),
        content_index: Some(content_index),
        ..Default::default()
    }
}

#[test]
fn unknown_descendant_of_skipped_part_is_absorbed() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("resp_img", "gpt-4o-mini")).unwrap();
    d.feed(&stream_item_added(0, "msg_img")).unwrap();
    assert!(d.feed(&stream_image_part_added(0, 0, "msg_img")).unwrap().is_empty());
    assert!(d.feed(&unknown_descendant(0, 0, "msg_img")).unwrap().is_empty());
    assert_eq!(d.losses().len(), 1);
    assert_eq!(d.losses()[0].path, "output[0].content[0]");
}

#[test]
fn unknown_descendant_with_wrong_item_is_error() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("resp_img", "gpt-4o-mini")).unwrap();
    d.feed(&stream_item_added(0, "msg_img")).unwrap();
    d.feed(&stream_image_part_added(0, 0, "msg_img")).unwrap();
    let err = d.feed(&unknown_descendant(0, 0, "other")).unwrap_err();
    assert!(err.to_string().contains("does not match"));
    assert_eq!(d.losses().len(), 1);
}

#[test]
fn identity_less_unknown_event_records_one_loss() {
    let config = Config::default();
    let mut d = StreamDecoder::new(&config);
    d.feed(&stream_created("resp_w", "gpt-4o-mini")).unwrap();
    let weird = StreamEvent {
        kind: "response.weird_event".to_string(),
        ..Default::default()
    };
    assert!(d.feed(&weird).unwrap().is_empty());
    assert_eq!(d.losses().len(), 1);
    assert_eq!(d.losses()[0].path, "type");
}
```

- [ ] **Step 2: Run and capture the RED**

Run: `cd rust && cargo test -p oxa-responses --test stream`
Expected: `unknown_descendant_of_skipped_part_is_absorbed` FAILS (2 losses, not 1) and `unknown_descendant_with_wrong_item_is_error` FAILS (feed returns `Ok`). `identity_less_unknown_event_records_one_loss` PASSES already. The vector suite `cargo test -p oxa-responses --test vectors` is also RED at this commit for `responses.stream.skipped-part-loss`.

- [ ] **Step 3: Implement the guarded `_ =>` arm**

Replace the `_ => { ... }` arm at `rust/crates/oxa-responses/src/streamin.rs:461-472` with:

```rust
            _ => {
                // Unknown event types: absorb identity-matching descendants of
                // an active skipped unit (N-S-3); validate identity against the
                // open supported unit otherwise; keep at most one loss.
                let has_identity = ev.output_index.is_some()
                    || ev.item_id.is_some()
                    || ev.content_index.is_some();
                if self.skipped_item || self.skipped_part {
                    if has_identity {
                        self.require_active_item(ev, &ev.kind)?;
                        if self.skipped_part && !self.skipped_item {
                            let content_index = ev.content_index.unwrap_or(-1);
                            if content_index != self.content_index {
                                return Err(Error::new(format!(
                                    "responses: {} content_index {} does not match the skipped part",
                                    ev.kind, content_index
                                )));
                            }
                        }
                        return Ok(Vec::new());
                    }
                } else if has_identity {
                    self.require_active_item(ev, &ev.kind)?;
                    if self.block_open {
                        let content_index = ev.content_index.unwrap_or(-1);
                        if content_index != self.content_index {
                            return Err(Error::new(format!(
                                "responses: {} content_index {} does not match the open content part",
                                ev.kind, content_index
                            )));
                        }
                    } else if ev.content_index.is_some() {
                        return Err(Error::new(format!(
                            "responses: {} has no open content part",
                            ev.kind
                        )));
                    }
                }
                self.losses.push(loss(
                    "type",
                    "type",
                    LossReason::UnsupportedSemantic,
                    format!(
                        "Responses stream event type {:?} is not decoded in the Responses stream profile",
                        ev.kind
                    ),
                ));
                Ok(Vec::new())
            }
```

- [ ] **Step 4: Run focused and workspace suites (GREEN)**

Run: `cd rust && cargo test -p oxa-responses`
Run: `cd rust && cargo test --workspace && cargo clippy --workspace --all-targets -- -D warnings && cargo fmt --all -- --check`
Expected: all pass, including `stream_vectors` over all 12 stream vectors.

- [ ] **Step 5: Commit**

```bash
git add rust/crates/oxa-responses/src/streamin.rs rust/crates/oxa-responses/tests/stream.rs
git commit -m "fix(rust): absorb responses unknown stream descendants"
```

### Task 3: C++ alignment

**Files:**
- Modify: `cpp/src/openai/responses.cpp:1140-1144` (tail of `StreamDecoder::feed`, before the final `losses_.push_back(make_resp_loss("type", "type", ...))` and `return events;`)
- Modify: `cpp/tests/test_anthropic.cpp:42`, `cpp/tests/test_vectors.cpp:76`, `cpp/tests/test_stream.cpp:94,109,124`

**Interfaces:**
- Consumes: `StreamDecoder::feed(const json::Value& chunk) -> StatusOr<std::vector<ir::Event>>`, `require_active_item(chunk, type)`, members `skipped_item_`, `skipped_part_`, `block_open_`, `content_index_`, `make_resp_loss(path, field, reason, detail)`, `ir::LOSS_UNSUPPORTED_SEMANTIC`, json accessors `find(...)->is_int()/as_int()`.
- Produces: behavior matching `skipped-part-loss.json`; no header change.

- [ ] **Step 1: Refresh the stale count tripwires**

```cpp
// cpp/tests/test_anthropic.cpp:42
CHECK(rep_res->executed == 31);
// cpp/tests/test_vectors.cpp:76 (anthropic section; 34/41/12 stay)
CHECK(r->executed == 31);
// cpp/tests/test_stream.cpp — chatcompletions, anthropic, responses sections
CHECK(r->executed == 3);  // line 94 (was 2: m7-tool-calls-to-ir/from-ir + usage-int64)
CHECK(r->executed == 4);  // line 109 (was 3: three m7-tool-use vectors + usage-int64)
CHECK(r->executed == 5);  // line 124 (was 3: m7-function-call-to-ir/from-ir,
                          //           function-call-output-loss + usage-int64 + skipped-part-loss)
```

- [ ] **Step 2: Build, run, and capture the RED**

Run: `cmake -B cpp/build -S cpp -DCMAKE_BUILD_TYPE=Debug && cmake --build cpp/build --config Debug --parallel && ctest --test-dir cpp/build -C Debug --output-on-failure`
Expected: `test_stream` FAILS in the responses section on `responses.stream.skipped-part-loss` (extra `path="type"` loss) — the count fixes let execution reach it. If a local C++ toolchain is unavailable on this machine, rely on the CI run in Task 4 as the RED/GREEN evidence and note that in the task report.

- [ ] **Step 3: Commit the count refresh**

```bash
git add cpp/tests/test_anthropic.cpp cpp/tests/test_vectors.cpp cpp/tests/test_stream.cpp
git commit -m "test(cpp): refresh golden vector counts"
```

- [ ] **Step 4: Implement the guarded unknown-event tail**

In `cpp/src/openai/responses.cpp`, insert this immediately before the existing final `losses_.push_back(make_resp_loss("type", "type", ...)); return events;` at the end of `StreamDecoder::feed`:

```cpp
    // Unknown event types: absorb identity-matching descendants of an active
    // skipped unit (N-S-3); validate identity against the open supported unit
    // otherwise; keep at most one loss per event.
    const bool has_identity = chunk.find("output_index") != nullptr ||
                              chunk.find("item_id") != nullptr ||
                              chunk.find("content_index") != nullptr;
    auto content_index_of = [&chunk]() -> std::int64_t {
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) return ci->as_int();
        return -1;
    };
    if (skipped_item_ || skipped_part_) {
        if (has_identity) {
            if (auto st = require_active_item(chunk, type); !st.ok()) return st;
            if (skipped_part_ && !skipped_item_) {
                const std::int64_t content_index = content_index_of();
                if (content_index != content_index_)
                    return invalid_argument("responses: " + type +
                                            " does not match the skipped content part");
            }
            return events;  // absorbed
        }
    } else if (has_identity) {
        if (auto st = require_active_item(chunk, type); !st.ok()) return st;
        if (block_open_) {
            if (content_index_of() != content_index_)
                return invalid_argument("responses: " + type +
                                        " does not match the open content part");
        } else if (chunk.find("content_index") != nullptr) {
            return invalid_argument("responses: " + type + " has no open content part");
        }
    }
```

Note: this code path must not throw — build with the CI `-fno-exceptions` variant in mind; `invalid_argument(...)` returns a `Status`, matching the file's existing error style.

- [ ] **Step 5: Build and run (GREEN)**

Run: `cmake --build cpp/build --config Debug --parallel && ctest --test-dir cpp/build -C Debug --output-on-failure`
Expected: all 11 CTest suites pass with the updated counts (or CI confirms it if built remotely).

- [ ] **Step 6: Commit**

```bash
git add cpp/src/openai/responses.cpp
git commit -m "fix(cpp): absorb responses unknown stream descendants"
```

### Task 4: Full verification and merge

**Files:**
- No source change; verification only.

**Interfaces:**
- Consumes: Tasks 1–3 commits on branch `vector-alignment`.
- Produces: a green `ci.yml` run on the branch and a merge-ready state.

- [ ] **Step 1: Re-run every local gate**

Run: `cd python && PYTHONPATH=src python3 -m unittest discover -s tests`
Run: `cd rust && cargo test --workspace && cargo clippy --workspace --all-targets -- -D warnings && cargo fmt --all -- --check`
Run: `make vectors && make test && make lint && make fmt` (repository root — Go and vectors untouched but verified)
Run: `npm --prefix ts run release:check` (TypeScript untouched but verified)
Expected: all green.

- [ ] **Step 2: Push the branch and trigger CI**

```bash
git push origin vector-alignment
gh workflow run ci.yml --ref vector-alignment
gh run list --branch vector-alignment --limit 1
```

- [ ] **Step 3: Inspect the completed run**

Run: `gh run watch <run-id> --exit-status` then `gh run view <run-id> --json conclusion,jobs`
Expected: `conclusion` is `success` — every job including `rust`, `python (3.10–3.13)`, `cpp` (all OS variants), `typescript`, `typescript-windows`, `consumers`, and `reliability`. If any job fails, reproduce locally, fix, and repeat from Step 1 of the owning task.

- [ ] **Step 4: Merge decision**

Present the green run for the merge decision (fast-forward `main`, mirroring the `typescript-support` flow). PyPI/crates.io 1.0.1 patch releases for the changed loss behavior are a separate follow-up decision and are not performed by this plan.
