//! The cross-protocol runner against programmable fake converters,
//! exercised in a fake repository under a tempdir.

use std::cell::RefCell;
use std::fs;
use std::path::{Path, PathBuf};

use oxa_ir::{Loss, LossReason, Request, Response, StopReason, Usage};
use oxa_vectest::{Converter, Outcome, Vector, cross_vectors_for, run_cross_in};

/// Canned converter mirroring the Go fakeCrossConverter: fixed IR decode
/// results, fixed wire encode output, fixed losses.
struct FakeCrossConverter {
    face: &'static str,
    decoded_requests: RefCell<usize>,
    decoded_responses: RefCell<usize>,
    decode_losses: Vec<Loss>,
    encode_losses: Vec<Loss>,
}

impl FakeCrossConverter {
    fn new(face: &'static str, decode_losses: Vec<Loss>, encode_losses: Vec<Loss>) -> Self {
        FakeCrossConverter {
            face,
            decoded_requests: RefCell::new(0),
            decoded_responses: RefCell::new(0),
            decode_losses,
            encode_losses,
        }
    }
}

impl Converter for FakeCrossConverter {
    fn face(&self) -> &'static str {
        self.face
    }

    fn decode_request_wire(&self, _wire: &str) -> Result<(Request, Vec<Loss>), String> {
        *self.decoded_requests.borrow_mut() += 1;
        let req = Request {
            model: "m".to_string(),
            system: Vec::new(),
            messages: Vec::new(),
            tools: None,
            tool_choice: None,
            params: None,
            metadata: None,
        };
        Ok((req, self.decode_losses.clone()))
    }

    fn decode_response_wire(&self, _wire: &str) -> Result<(Response, Vec<Loss>), String> {
        *self.decoded_responses.borrow_mut() += 1;
        let resp = Response {
            id: "r".to_string(),
            model: "m".to_string(),
            content: Vec::new(),
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 0,
                output_tokens: 0,
            },
        };
        Ok((resp, self.decode_losses.clone()))
    }

    fn encode_request_ir(&self, _req: &Request) -> Result<(String, Vec<Loss>), String> {
        Ok((
            r#"{"kind":"output"}"#.to_string(),
            self.encode_losses.clone(),
        ))
    }

    fn encode_response_ir(&self, _resp: &Response) -> Result<(String, Vec<Loss>), String> {
        Ok((
            r#"{"kind":"output"}"#.to_string(),
            self.encode_losses.clone(),
        ))
    }
}

fn unmapped(path: &str, field: &str) -> Loss {
    Loss {
        path: path.to_string(),
        field: field.to_string(),
        reason: LossReason::UnmappedField,
        detail: String::new(),
    }
}

fn cross_vector(
    name: &str,
    source: &str,
    target: &str,
    request: bool,
    expected_losses: Vec<LossRecord>,
) -> Vector {
    Vector {
        name: format!("cross.nonstream.{name}"),
        mode: "nonstream".to_string(),
        conversion: "protocol-to-protocol".to_string(),
        source: oxa_vectest::Endpoint {
            protocol: source.to_string(),
        },
        target: oxa_vectest::Endpoint {
            protocol: target.to_string(),
        },
        input: serde_json::json!({ "kind": "input" }),
        expected_output: Some(serde_json::json!({ "kind": "output" })),
        expected_losses,
        tags: vec![if request { "request" } else { "response" }.to_string()],
        ..Vector::default()
    }
}

use oxa_vectest::LossRecord;

fn loss_record(loss: &Loss) -> LossRecord {
    LossRecord {
        path: loss.path.clone(),
        field: loss.field.clone(),
        reason: loss.reason.as_str().to_string(),
        detail: loss.detail.clone(),
    }
}

fn fake_repo() -> (tempfile::TempDir, PathBuf) {
    let root = tempfile::tempdir().expect("tempdir");
    fs::create_dir_all(root.path().join(".git")).expect("fake git directory");
    let path = root.path().to_path_buf();
    (root, path)
}

fn write_cross_vector(root: &Path, file_name: &str, vector: &Vector) {
    let dir = root.join("vectors").join("cross").join("nonstream");
    fs::create_dir_all(&dir).expect("make cross vector directory");
    let raw = serde_json::to_string_pretty(vector).expect("serialize vector");
    fs::write(dir.join(file_name), raw).expect("write vector");
}

#[test]
fn composes_only_vectors_matching_the_pair() {
    let (_repo, root) = fake_repo();
    // Matches the fake converters' decode+encode losses so the harness's
    // concatenation and unordered-set comparison are both exercised.
    let expected_losses = vec![
        loss_record(&unmapped("in", "f")),
        loss_record(&unmapped("params", "g")),
    ];
    write_cross_vector(
        &root,
        "alpha-to-beta-request.json",
        &cross_vector(
            "alpha-to-beta-request",
            "alpha",
            "beta",
            true,
            expected_losses.clone(),
        ),
    );
    write_cross_vector(
        &root,
        "alpha-to-beta-response.json",
        &cross_vector(
            "alpha-to-beta-response",
            "alpha",
            "beta",
            false,
            expected_losses.clone(),
        ),
    );
    // Mismatched pair must be skipped by run_cross_in(alpha, beta).
    write_cross_vector(
        &root,
        "beta-to-alpha-request.json",
        &cross_vector(
            "beta-to-alpha-request",
            "beta",
            "alpha",
            true,
            expected_losses,
        ),
    );

    let decode_loss = vec![unmapped("in", "f")];
    let encode_loss = vec![unmapped("params", "g")];
    let alpha = FakeCrossConverter::new("alpha", decode_loss, Vec::new());
    let beta = FakeCrossConverter::new("beta", Vec::new(), encode_loss);

    let report = match run_cross_in(&root, &alpha, &beta).expect("run") {
        Outcome::Ran(report) => report,
        other => panic!("expected Ran, got {other:?}"),
    };

    assert_eq!(report.executed, 2);
    assert!(
        report.failures.is_empty(),
        "failures: {:#?}",
        report.failures
    );
    assert_eq!(*alpha.decoded_requests.borrow(), 1);
    assert_eq!(*alpha.decoded_responses.borrow(), 1);
}

#[test]
fn reports_a_loss_concatenation_mismatch() {
    let (_repo, root) = fake_repo();
    // Only the decode loss is expected, but the encode side also reports
    // one; the unordered-set comparison must flag the extra.
    write_cross_vector(
        &root,
        "alpha-to-beta-request.json",
        &cross_vector(
            "alpha-to-beta-request",
            "alpha",
            "beta",
            true,
            vec![loss_record(&unmapped("in", "f"))],
        ),
    );
    let alpha = FakeCrossConverter::new("alpha", vec![unmapped("in", "f")], Vec::new());
    let beta = FakeCrossConverter::new("beta", Vec::new(), vec![unmapped("params", "g")]);

    let report = match run_cross_in(&root, &alpha, &beta).expect("run") {
        Outcome::Ran(report) => report,
        other => panic!("expected Ran, got {other:?}"),
    };
    assert_eq!(report.failures.len(), 1);
    assert!(
        report.failures[0].1.contains("unexpected loss reported"),
        "{}",
        report.failures[0].1
    );
}

#[test]
fn empty_match_is_a_harness_error() {
    let (_repo, root) = fake_repo();
    // A vector for an unrelated pair keeps the repo root discoverable while
    // leaving no match for alpha -> beta.
    write_cross_vector(
        &root,
        "gamma-to-delta-request.json",
        &cross_vector("gamma-to-delta-request", "gamma", "delta", true, Vec::new()),
    );
    let alpha = FakeCrossConverter::new("alpha", Vec::new(), Vec::new());
    let beta = FakeCrossConverter::new("beta", Vec::new(), Vec::new());
    let err = run_cross_in(&root, &alpha, &beta).expect_err("no matching vectors");
    assert!(
        err.contains("no cross vectors found for pair alpha -> beta"),
        "{err}"
    );
}

#[test]
fn cross_vectors_for_filters_by_pair() {
    let vectors = vec![
        cross_vector("a-to-b-request", "alpha", "beta", true, Vec::new()),
        cross_vector("b-to-a-request", "beta", "alpha", true, Vec::new()),
        cross_vector("a-to-c-request", "alpha", "gamma", true, Vec::new()),
    ];
    let alpha = FakeCrossConverter::new("alpha", Vec::new(), Vec::new());
    let beta = FakeCrossConverter::new("beta", Vec::new(), Vec::new());

    let matched = cross_vectors_for(&alpha, &beta, &vectors);
    assert_eq!(matched.len(), 1);
    assert_eq!(matched[0].name, "cross.nonstream.a-to-b-request");

    let delta = FakeCrossConverter::new("delta", Vec::new(), Vec::new());
    assert!(cross_vectors_for(&alpha, &delta, &vectors).is_empty());
}

#[test]
fn missing_repo_root_skips() {
    let root = tempfile::tempdir().expect("tempdir");
    let alpha = FakeCrossConverter::new("alpha", Vec::new(), Vec::new());
    let beta = FakeCrossConverter::new("beta", Vec::new(), Vec::new());
    match run_cross_in(root.path(), &alpha, &beta).expect("run") {
        Outcome::Skipped => {}
        other => panic!("expected Skipped, got {other:?}"),
    }
}
