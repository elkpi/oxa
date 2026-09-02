//! The stream runner against programmable fake converters, exercised in a
//! fake repository under a tempdir. Mirrors go/internal/vectest/stream_test.go.

use std::cell::Cell;
use std::fs;
use std::path::{Path, PathBuf};

use oxa_ir::{Block, Delta, Event, EventStream, Loss, LossReason, StopReason, Usage, to_json};
use oxa_vectest::{
    LossRecord, Outcome, StreamConverter, Vector, run_stream_in, stream_from_ir, stream_to_ir,
};
use serde_json::Value;

fn start(id: &str) -> Event {
    Event::MessageStart {
        id: id.to_string(),
        model: "model".to_string(),
    }
}

fn text_block_start(index: i64) -> Event {
    Event::ContentBlockStart {
        index,
        block: Block::Text {
            text: String::new(),
        },
    }
}

fn text_delta(index: i64, text: &str) -> Event {
    Event::ContentBlockDelta {
        index,
        delta: Delta::TextDelta {
            text: text.to_string(),
        },
    }
}

fn block_stop(index: i64) -> Event {
    Event::ContentBlockStop { index }
}

fn message_delta(stop: StopReason) -> Event {
    Event::MessageDelta {
        stop_reason: stop,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 0,
            output_tokens: 0,
        },
    }
}

fn done() -> Event {
    Event::MessageDone {}
}

fn event_stream_doc(events: &[Event]) -> Value {
    let doc = to_json(&EventStream {
        events: events.to_vec(),
    })
    .expect("marshal event stream");
    serde_json::from_str(&doc).expect("parse event stream document")
}

fn native_envelope(events: &[Value]) -> Value {
    serde_json::json!({ "events": events })
}

fn loss_record(path: &str, field: &str, reason: &str) -> LossRecord {
    LossRecord {
        path: path.to_string(),
        field: field.to_string(),
        reason: reason.to_string(),
        detail: String::new(),
    }
}

fn ir_loss(path: &str, field: &str, reason: LossReason) -> Loss {
    Loss {
        path: path.to_string(),
        field: field.to_string(),
        reason,
        detail: String::new(),
    }
}

fn fake_repo() -> (tempfile::TempDir, PathBuf) {
    let root = tempfile::tempdir().expect("tempdir");
    fs::create_dir_all(root.path().join(".git")).expect("fake git directory");
    let path = root.path().to_path_buf();
    (root, path)
}

fn write_stream_vector(root: &Path, face: &str, file_name: &str, vector: &Vector) {
    let dir = root.join("vectors").join(face).join("stream");
    fs::create_dir_all(&dir).expect("make vector directory");
    let raw = serde_json::to_string_pretty(vector).expect("serialize vector");
    fs::write(dir.join(file_name), raw).expect("write vector");
}

fn assert_clean(outcome: Result<Outcome, String>) -> oxa_vectest::Report {
    match outcome {
        Ok(Outcome::Ran(report)) => {
            assert!(
                report.failures.is_empty(),
                "failures: {:#?}",
                report.failures
            );
            report
        }
        Ok(Outcome::Skipped) => panic!("fake repository must be found"),
        Err(err) => panic!("harness error: {err}"),
    }
}

/// Canned converter mirroring the Go fakeStreamConverter.
struct FakeStreamConverter {
    face: &'static str,
    decode_scripts: Vec<Vec<Event>>,
    decode_err: Option<String>,
    decode_calls: usize,
    flush_events: Vec<Event>,
    flush_called: bool,
    decoder_losses: Vec<Loss>,
    losses_called_after_flush: Cell<bool>,
    apply_scripts: Vec<(Vec<Value>, Vec<Loss>)>,
    apply_err: Option<String>,
    applied: usize,
}

impl StreamConverter for FakeStreamConverter {
    fn face(&self) -> &'static str {
        self.face
    }

    fn decode_native_event(&mut self, _event: &Value) -> Result<Vec<Event>, String> {
        self.decode_calls += 1;
        if let Some(err) = &self.decode_err {
            return Err(err.clone());
        }
        Ok(self
            .decode_scripts
            .get(self.decode_calls - 1)
            .cloned()
            .unwrap_or_default())
    }

    fn flush_decoder(&mut self) -> Result<Vec<Event>, String> {
        self.flush_called = true;
        Ok(self.flush_events.clone())
    }

    fn decoder_losses(&self) -> Vec<Loss> {
        self.losses_called_after_flush.set(self.flush_called);
        self.decoder_losses.clone()
    }

    fn apply_ir_event(&mut self, _event: &Event) -> Result<(Vec<Value>, Vec<Loss>), String> {
        if let Some(err) = &self.apply_err {
            return Err(err.clone());
        }
        let script = self
            .apply_scripts
            .get(self.applied)
            .cloned()
            .unwrap_or_default();
        self.applied += 1;
        Ok(script)
    }
}

#[test]
fn run_stream_to_ir_feeds_flushes_and_compares() {
    let (_repo, root) = fake_repo();
    let flush_events = vec![
        text_delta(0, "hello"),
        block_stop(0),
        Event::MessageDelta {
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 2,
                output_tokens: 3,
            },
        },
        done(),
    ];
    let mut conv = FakeStreamConverter {
        face: "fake",
        decode_scripts: vec![vec![start("stream-1")], vec![text_block_start(0)]],
        decode_err: None,
        decode_calls: 0,
        flush_events: flush_events.clone(),
        flush_called: false,
        decoder_losses: vec![ir_loss("native", "ignored", LossReason::UnmappedField)],
        losses_called_after_flush: Cell::new(false),
        apply_scripts: Vec::new(),
        apply_err: None,
        applied: 0,
    };
    let mut expected_events = vec![start("stream-1"), text_block_start(0)];
    expected_events.extend(flush_events);
    let vector = Vector {
        name: "fake.stream.to-ir".to_string(),
        mode: "stream".to_string(),
        conversion: "to-ir".to_string(),
        input: native_envelope(&[
            serde_json::json!({ "event": "one" }),
            serde_json::json!({ "event": "two" }),
        ]),
        expected_ir: Some(event_stream_doc(&expected_events)),
        expected_losses: vec![loss_record("native", "ignored", "unmapped-field")],
        ..Vector::default()
    };
    write_stream_vector(&root, conv.face, "to-ir.json", &vector);

    let report = assert_clean(run_stream_in(&root, &mut conv));

    assert_eq!(report.executed, 1);
    assert_eq!(conv.decode_calls, 2);
    assert!(conv.flush_called);
    assert!(conv.losses_called_after_flush.get());
}

#[test]
fn run_stream_resets_optional_vector_state() {
    let (_repo, root) = fake_repo();
    let mut conv = ResettingConverter {
        reset_count: 0,
        dirty: false,
        second_started_clean: false,
    };
    for name in ["first", "second"] {
        let vector = Vector {
            name: format!("resetting-fake.stream.{name}"),
            mode: "stream".to_string(),
            conversion: "to-ir".to_string(),
            input: native_envelope(&[serde_json::json!({ "event": name })]),
            expected_ir: Some(event_stream_doc(&[
                start(name),
                message_delta(StopReason::EndTurn),
                done(),
            ])),
            expected_losses: Vec::new(),
            ..Vector::default()
        };
        write_stream_vector(&root, "resetting-fake", &format!("{name}.json"), &vector);
    }

    assert_clean(run_stream_in(&root, &mut conv));

    assert_eq!(conv.reset_count, 2);
    assert!(
        conv.second_started_clean,
        "second vector began with dirty state from the first vector"
    );
}

struct ResettingConverter {
    reset_count: usize,
    dirty: bool,
    second_started_clean: bool,
}

impl StreamConverter for ResettingConverter {
    fn face(&self) -> &'static str {
        "resetting-fake"
    }

    fn reset_stream_vector(&mut self) {
        self.reset_count += 1;
        self.dirty = false;
    }

    fn decode_native_event(&mut self, event: &Value) -> Result<Vec<Event>, String> {
        let name = event
            .get("event")
            .and_then(Value::as_str)
            .ok_or_else(|| "unexpected fake event".to_string())?;
        match name {
            "first" => self.dirty = true,
            "second" => self.second_started_clean = !self.dirty,
            other => return Err(format!("unexpected fake event {other:?}")),
        }
        Ok(vec![start(name)])
    }

    fn flush_decoder(&mut self) -> Result<Vec<Event>, String> {
        Ok(vec![message_delta(StopReason::EndTurn), done()])
    }

    fn decoder_losses(&self) -> Vec<Loss> {
        Vec::new()
    }

    fn apply_ir_event(&mut self, _event: &Event) -> Result<(Vec<Value>, Vec<Loss>), String> {
        Err("unexpected ApplyIREvent".to_string())
    }
}

#[test]
fn run_stream_from_ir_applies_in_order_and_accumulates_losses() {
    let (_repo, root) = fake_repo();
    let mut conv = FakeStreamConverter {
        face: "fake",
        decode_scripts: Vec::new(),
        decode_err: None,
        decode_calls: 0,
        flush_events: Vec::new(),
        flush_called: false,
        decoder_losses: Vec::new(),
        losses_called_after_flush: Cell::new(false),
        apply_scripts: vec![
            (vec![serde_json::json!({ "event": "start" })], Vec::new()),
            (
                vec![serde_json::json!({ "event": "delta" })],
                vec![ir_loss("events[1]", "normalization", LossReason::Degraded)],
            ),
            (vec![serde_json::json!({ "event": "done" })], Vec::new()),
        ],
        apply_err: None,
        applied: 0,
    };
    let vector = Vector {
        name: "fake.stream.from-ir".to_string(),
        mode: "stream".to_string(),
        conversion: "from-ir".to_string(),
        input: event_stream_doc(&[
            start("stream-2"),
            message_delta(StopReason::EndTurn),
            done(),
        ]),
        expected_output: Some(native_envelope(&[
            serde_json::json!({ "event": "start" }),
            serde_json::json!({ "event": "delta" }),
            serde_json::json!({ "event": "done" }),
        ])),
        expected_losses: vec![loss_record("events[1]", "normalization", "degraded")],
        ..Vector::default()
    };
    write_stream_vector(&root, conv.face, "from-ir.json", &vector);

    let report = assert_clean(run_stream_in(&root, &mut conv));

    assert_eq!(report.executed, 1);
    assert_eq!(conv.applied, 3);
}

#[test]
fn to_ir_error_includes_native_event_context() {
    let mut conv = FakeStreamConverter {
        face: "fake",
        decode_scripts: Vec::new(),
        decode_err: Some("native decode failed".to_string()),
        decode_calls: 0,
        flush_events: Vec::new(),
        flush_called: false,
        decoder_losses: Vec::new(),
        losses_called_after_flush: Cell::new(false),
        apply_scripts: Vec::new(),
        apply_err: None,
        applied: 0,
    };
    let input = native_envelope(&[serde_json::json!({ "type": "broken" })]);

    let err = stream_to_ir(&mut conv, &input).expect_err("native conversion error");
    assert!(
        err.contains(r#"decode native event 0 ({"type":"broken"}): native decode failed"#),
        "{err}"
    );
}

#[test]
fn to_ir_error_truncates_native_event_context() {
    let mut conv = FakeStreamConverter {
        face: "fake",
        decode_scripts: Vec::new(),
        decode_err: Some("native decode failed".to_string()),
        decode_calls: 0,
        flush_events: Vec::new(),
        flush_called: false,
        decoder_losses: Vec::new(),
        losses_called_after_flush: Cell::new(false),
        apply_scripts: Vec::new(),
        apply_err: None,
        applied: 0,
    };
    let event = serde_json::json!({ "type": "x".repeat(600) });
    let full = serde_json::to_string(&event).expect("serialize");
    let input = native_envelope(&[event]);

    let err = stream_to_ir(&mut conv, &input).expect_err("native conversion error");
    let prefix: String = full.chars().take(512).collect();
    assert!(
        err.contains(&prefix),
        "error must carry the bounded event: {err}"
    );
    assert!(err.contains('…'), "bounded marker present: {err}");
    assert!(
        !err.contains(&full),
        "error must not carry the unbounded event"
    );
}

#[test]
fn from_ir_error_carries_the_event_index() {
    let mut conv = FakeStreamConverter {
        face: "fake",
        decode_scripts: Vec::new(),
        decode_err: None,
        decode_calls: 0,
        flush_events: Vec::new(),
        flush_called: false,
        decoder_losses: Vec::new(),
        losses_called_after_flush: Cell::new(false),
        apply_scripts: Vec::new(),
        apply_err: Some("apply exploded".to_string()),
        applied: 0,
    };
    let input = event_stream_doc(&[start("s"), message_delta(StopReason::EndTurn), done()]);

    let err = stream_from_ir(&mut conv, &input).expect_err("apply error");
    assert!(err.contains("apply IR event 0: apply exploded"), "{err}");
}

#[test]
fn unknown_conversion_is_recorded_as_a_failure() {
    let (_repo, root) = fake_repo();
    let mut conv = FakeStreamConverter {
        face: "fake",
        decode_scripts: Vec::new(),
        decode_err: None,
        decode_calls: 0,
        flush_events: Vec::new(),
        flush_called: false,
        decoder_losses: Vec::new(),
        losses_called_after_flush: Cell::new(false),
        apply_scripts: Vec::new(),
        apply_err: None,
        applied: 0,
    };
    let vector = Vector {
        name: "fake.stream.sideways".to_string(),
        mode: "stream".to_string(),
        conversion: "sideways".to_string(),
        input: native_envelope(&[]),
        expected_losses: Vec::new(),
        ..Vector::default()
    };
    write_stream_vector(&root, conv.face, "sideways.json", &vector);

    let report = match run_stream_in(&root, &mut conv).expect("run") {
        Outcome::Ran(report) => report,
        other => panic!("expected Ran, got {other:?}"),
    };
    assert_eq!(report.failures.len(), 1);
    assert!(
        report.failures[0]
            .1
            .contains(r#"unknown conversion "sideways""#),
        "{}",
        report.failures[0].1
    );
}
