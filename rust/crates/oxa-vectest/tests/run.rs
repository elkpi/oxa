//! The nonstream runner against a programmable fake converter, exercised in
//! a fake repository under a tempdir.

use std::cell::RefCell;
use std::fs;
use std::path::{Path, PathBuf};

use oxa_ir::{Loss, LossReason, Request, Response, StopReason, Usage};
use oxa_vectest::{Converter, LossRecord, Outcome, Vector, run_in};
use serde_json::Value;

/// Canned converter: returns fixed IR values and fixed wire output with
/// fixed losses, mirroring the Go fake converters. Call arguments are
/// recorded through `RefCell` because the trait takes `&self`.
struct FakeConverter {
    decode_err: Option<String>,
    encode_err: Option<String>,
    report_decode_losses: bool,
    report_encode_losses: bool,
    decoded_request_wires: RefCell<Vec<String>>,
    decoded_response_wires: RefCell<Vec<String>>,
    encoded_request_models: RefCell<Vec<String>>,
    encoded_response_models: RefCell<Vec<String>>,
}

impl FakeConverter {
    fn new() -> Self {
        FakeConverter {
            decode_err: None,
            encode_err: None,
            report_decode_losses: true,
            report_encode_losses: false,
            decoded_request_wires: RefCell::new(Vec::new()),
            decoded_response_wires: RefCell::new(Vec::new()),
            encoded_request_models: RefCell::new(Vec::new()),
            encoded_response_models: RefCell::new(Vec::new()),
        }
    }

    fn decode_losses(&self) -> Vec<Loss> {
        if self.report_decode_losses {
            vec![Loss {
                path: "in".to_string(),
                field: "f".to_string(),
                reason: LossReason::UnmappedField,
                detail: String::new(),
            }]
        } else {
            Vec::new()
        }
    }

    fn encode_losses(&self) -> Vec<Loss> {
        if self.report_encode_losses {
            vec![Loss {
                path: "params".to_string(),
                field: "g".to_string(),
                reason: LossReason::UnmappedField,
                detail: String::new(),
            }]
        } else {
            Vec::new()
        }
    }
}

impl Converter for FakeConverter {
    fn face(&self) -> &'static str {
        "fake"
    }

    fn decode_request_wire(&self, wire: &str) -> Result<(Request, Vec<Loss>), String> {
        self.decoded_request_wires
            .borrow_mut()
            .push(wire.to_string());
        if let Some(err) = &self.decode_err {
            return Err(err.clone());
        }
        let req = bare_request("m");
        Ok((req, self.decode_losses()))
    }

    fn decode_response_wire(&self, wire: &str) -> Result<(Response, Vec<Loss>), String> {
        self.decoded_response_wires
            .borrow_mut()
            .push(wire.to_string());
        if let Some(err) = &self.decode_err {
            return Err(err.clone());
        }
        Ok((bare_response("m"), self.decode_losses()))
    }

    fn encode_request_ir(&self, req: &Request) -> Result<(String, Vec<Loss>), String> {
        self.encoded_request_models
            .borrow_mut()
            .push(req.model.clone());
        if let Some(err) = &self.encode_err {
            return Err(err.clone());
        }
        Ok((r#"{"kind":"output"}"#.to_string(), self.encode_losses()))
    }

    fn encode_response_ir(&self, resp: &Response) -> Result<(String, Vec<Loss>), String> {
        self.encoded_response_models
            .borrow_mut()
            .push(resp.model.clone());
        if let Some(err) = &self.encode_err {
            return Err(err.clone());
        }
        Ok((r#"{"kind":"output"}"#.to_string(), self.encode_losses()))
    }
}

/// A minimal but fully constructed IR request (Request has no Default).
fn bare_request(model: &str) -> Request {
    Request {
        model: model.to_string(),
        system: Vec::new(),
        messages: Vec::new(),
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
    }
}

/// A minimal but fully constructed IR response.
fn bare_response(model: &str) -> Response {
    Response {
        id: "r".to_string(),
        model: model.to_string(),
        content: Vec::new(),
        stop_reason: StopReason::EndTurn,
        stop_sequence: None,
        usage: Usage {
            input_tokens: 0,
            output_tokens: 0,
        },
    }
}

fn request_doc(req: &Request) -> Value {
    let doc = oxa_ir::to_json(req).expect("marshal request");
    serde_json::from_str(&doc).expect("parse request document")
}

fn response_doc(resp: &Response) -> Value {
    let doc = oxa_ir::to_json(resp).expect("marshal response");
    serde_json::from_str(&doc).expect("parse response document")
}

fn fake_repo() -> (tempfile::TempDir, PathBuf) {
    let root = tempfile::tempdir().expect("tempdir");
    fs::create_dir_all(root.path().join(".git")).expect("fake git directory");
    let path = root.path().to_path_buf();
    (root, path)
}

fn write_vector(root: &Path, face: &str, mode: &str, name: &str, vector: &Vector) {
    let dir = root.join("vectors").join(face).join(mode);
    fs::create_dir_all(&dir).expect("make vector directory");
    let raw = serde_json::to_string_pretty(vector).expect("serialize vector");
    fs::write(dir.join(name), raw).expect("write vector");
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

fn to_ir_request_vector() -> Vector {
    let req = bare_request("m");
    Vector {
        name: "fake.nonstream.to-ir".to_string(),
        mode: "nonstream".to_string(),
        conversion: "to-ir".to_string(),
        input: serde_json::json!({ "model": "cc", "temperature": 0.5 }),
        expected_ir: Some(request_doc(&req)),
        expected_losses: vec![LossRecord {
            path: "in".to_string(),
            field: "f".to_string(),
            reason: "unmapped-field".to_string(),
            detail: String::new(),
        }],
        tags: vec!["request".to_string()],
        ..Vector::default()
    }
}

#[test]
fn runs_to_ir_vectors_through_the_request_direction() {
    let (_repo, root) = fake_repo();
    write_vector(
        &root,
        "fake",
        "nonstream",
        "to-ir.json",
        &to_ir_request_vector(),
    );
    let conv = FakeConverter::new();

    let report = assert_clean(run_in(&root, &conv));

    assert_eq!(report.executed, 1);
    let wires = conv.decoded_request_wires.borrow();
    assert_eq!(wires.len(), 1);
    assert_eq!(
        serde_json::from_str::<Value>(&wires[0]).expect("wire stays JSON"),
        serde_json::json!({ "model": "cc", "temperature": 0.5 })
    );
    assert!(conv.decoded_response_wires.borrow().is_empty());
}

#[test]
fn routes_response_vectors_to_the_response_direction() {
    let (_repo, root) = fake_repo();
    let resp = bare_response("m");
    let vector = Vector {
        name: "fake.nonstream.resp-to-ir".to_string(),
        mode: "nonstream".to_string(),
        conversion: "to-ir".to_string(),
        input: serde_json::json!({ "kind": "response-wire" }),
        expected_ir: Some(response_doc(&resp)),
        expected_losses: vec![LossRecord {
            path: "in".to_string(),
            field: "f".to_string(),
            reason: "unmapped-field".to_string(),
            detail: String::new(),
        }],
        tags: vec!["response".to_string()],
        ..Vector::default()
    };
    write_vector(&root, "fake", "nonstream", "resp-to-ir.json", &vector);
    let conv = FakeConverter::new();

    assert_clean(run_in(&root, &conv));

    assert!(conv.decoded_request_wires.borrow().is_empty());
    assert_eq!(conv.decoded_response_wires.borrow().len(), 1);
}

#[test]
fn runs_from_ir_vectors_through_the_encoder() {
    let (_repo, root) = fake_repo();
    let req = bare_request("m");
    let vector = Vector {
        name: "fake.nonstream.from-ir".to_string(),
        mode: "nonstream".to_string(),
        conversion: "from-ir".to_string(),
        input: request_doc(&req),
        expected_output: Some(serde_json::json!({ "kind": "output" })),
        expected_losses: Vec::new(),
        tags: vec!["request".to_string()],
        ..Vector::default()
    };
    write_vector(&root, "fake", "nonstream", "from-ir.json", &vector);
    let conv = FakeConverter::new();

    let report = assert_clean(run_in(&root, &conv));

    assert_eq!(report.executed, 1);
    assert_eq!(*conv.encoded_request_models.borrow(), ["m"]);
}

#[test]
fn decode_failure_is_recorded_as_a_failure() {
    let (_repo, root) = fake_repo();
    write_vector(
        &root,
        "fake",
        "nonstream",
        "to-ir.json",
        &to_ir_request_vector(),
    );
    let conv = FakeConverter {
        decode_err: Some("boom".to_string()),
        ..FakeConverter::new()
    };

    let report = match run_in(&root, &conv).expect("run") {
        Outcome::Ran(report) => report,
        _ => panic!("must run"),
    };
    assert_eq!(report.failures.len(), 1);
    assert_eq!(report.failures[0].0, "fake.nonstream.to-ir");
    assert!(report.failures[0].1.starts_with("decode failed: boom"));
}

#[test]
fn encode_failure_is_recorded_as_a_failure() {
    let (_repo, root) = fake_repo();
    let req = bare_request("m");
    let vector = Vector {
        name: "fake.nonstream.from-ir".to_string(),
        mode: "nonstream".to_string(),
        conversion: "from-ir".to_string(),
        input: request_doc(&req),
        expected_output: Some(serde_json::json!({ "kind": "output" })),
        expected_losses: Vec::new(),
        tags: vec!["request".to_string()],
        ..Vector::default()
    };
    write_vector(&root, "fake", "nonstream", "from-ir.json", &vector);
    let conv = FakeConverter {
        encode_err: Some("boom".to_string()),
        ..FakeConverter::new()
    };

    let report = match run_in(&root, &conv).expect("run") {
        Outcome::Ran(report) => report,
        _ => panic!("must run"),
    };
    assert_eq!(report.failures.len(), 1);
    assert!(report.failures[0].1.starts_with("encode failed: boom"));
}

#[test]
fn loss_mismatches_are_recorded_as_failures() {
    let (_repo, root) = fake_repo();

    // Expected loss the fake never reports.
    let conv = FakeConverter {
        report_decode_losses: false,
        ..FakeConverter::new()
    };
    write_vector(
        &root,
        "fake",
        "nonstream",
        "to-ir.json",
        &to_ir_request_vector(),
    );
    let report = match run_in(&root, &conv).expect("run") {
        Outcome::Ran(report) => report,
        _ => panic!("must run"),
    };
    assert_eq!(report.failures.len(), 1);
    let message = &report.failures[0].1;
    assert!(message.starts_with("losses mismatch:"), "{message}");
    assert!(message.contains("expected loss not reported"), "{message}");

    // Loss the fake reports but the vector does not expect.
    let (_repo, root) = fake_repo();
    let conv = FakeConverter {
        report_encode_losses: true,
        ..FakeConverter::new()
    };
    let req = bare_request("m");
    let vector = Vector {
        name: "fake.nonstream.from-ir".to_string(),
        mode: "nonstream".to_string(),
        conversion: "from-ir".to_string(),
        input: request_doc(&req),
        expected_output: Some(serde_json::json!({ "kind": "output" })),
        expected_losses: Vec::new(),
        tags: vec!["request".to_string()],
        ..Vector::default()
    };
    write_vector(&root, "fake", "nonstream", "from-ir.json", &vector);
    let report = match run_in(&root, &conv).expect("run") {
        Outcome::Ran(report) => report,
        _ => panic!("must run"),
    };
    assert_eq!(report.failures.len(), 1);
    assert!(
        report.failures[0].1.contains("unexpected loss reported"),
        "{}",
        report.failures[0].1
    );
}

#[test]
fn unknown_conversion_is_recorded_as_a_failure() {
    let (_repo, root) = fake_repo();
    let mut vector = to_ir_request_vector();
    vector.conversion = "sideways".to_string();
    write_vector(&root, "fake", "nonstream", "to-ir.json", &vector);
    let conv = FakeConverter::new();

    let report = match run_in(&root, &conv).expect("run") {
        Outcome::Ran(report) => report,
        _ => panic!("must run"),
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

#[test]
fn empty_vector_set_is_a_harness_error() {
    let (_repo, root) = fake_repo();
    // The face directory exists but carries no .json vector, so the repo
    // root is discoverable while the vector set stays empty.
    let dir = root.join("vectors").join("fake").join("nonstream");
    fs::create_dir_all(&dir).expect("make vector directory");
    fs::write(dir.join("notes.txt"), b"not a vector").expect("write placeholder");
    let conv = FakeConverter::new();
    let err = run_in(&root, &conv).expect_err("empty vector set must fail");
    assert!(
        err.contains("no nonstream vectors found for face fake"),
        "{err}"
    );
}

#[test]
fn missing_repo_root_skips() {
    let root = tempfile::tempdir().expect("tempdir");
    let conv = FakeConverter::new();
    match run_in(root.path(), &conv).expect("run") {
        Outcome::Skipped => {}
        other => panic!("expected Skipped, got {other:?}"),
    }
}
