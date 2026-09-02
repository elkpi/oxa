//! The nonstream vector runner: the face-implementation surface and the
//! per-vector execution of `to-ir` / `from-ir` conversions.

use std::path::Path;

use oxa_ir::{Loss, Request, Response};
use serde_json::Value;

use crate::compare::{compare_json, compare_losses};
use crate::load::{LossRecord, Vector, find_repo_root, load_vectors};

/// The face-implementation surface the harness drives. The four methods
/// correspond exactly to the four conversion directions of a spoke:
/// `decode_*_wire` take face wire JSON and return an IR value plus losses
/// (face → IR); `encode_*_ir` take an IR value and return rendered face wire
/// JSON plus losses (IR → face). All comparisons are structural JSON
/// comparisons; there is no struct-equality shortcut, which serde skip
/// attributes would make fragile.
pub trait Converter {
    /// The vectors/ directory name of the face ("chatcompletions",
    /// "responses", "anthropic").
    fn face(&self) -> &'static str;
    fn decode_request_wire(&self, wire: &str) -> Result<(Request, Vec<Loss>), String>;
    fn decode_response_wire(&self, wire: &str) -> Result<(Response, Vec<Loss>), String>;
    fn encode_request_ir(&self, req: &Request) -> Result<(String, Vec<Loss>), String>;
    fn encode_response_ir(&self, resp: &Response) -> Result<(String, Vec<Loss>), String>;
}

/// Per-vector failure record: the vector name and the failure message. These
/// mirror Go's per-subtest `t.Errorf`/`t.Fatalf` outcomes.
#[derive(Debug, Default)]
pub struct Report {
    pub executed: usize,
    pub failures: Vec<(String, String)>,
}

/// Run outcome: `Skipped` mirrors Go's `t.Skip` in dependency mode (no repo
/// root found).
#[derive(Debug)]
pub enum Outcome {
    Skipped,
    Ran(Report),
}

/// Executes every nonstream golden vector of the converter's face, locating
/// the repo root from the current working directory. Inside the monorepo it
/// must find at least one vector.
pub fn run(conv: &dyn Converter) -> Result<Outcome, String> {
    run_in(Path::new("."), conv)
}

/// [`run`] with an explicit starting directory for the repo-root walk.
pub fn run_in(dir: &Path, conv: &dyn Converter) -> Result<Outcome, String> {
    let Some(root) = find_repo_root(dir) else {
        return Ok(Outcome::Skipped);
    };
    let vectors = load_vectors(&root, conv.face(), "nonstream")?;
    if vectors.is_empty() {
        return Err(format!(
            "no nonstream vectors found for face {}; the harness must execute at least one",
            conv.face()
        ));
    }
    let mut report = Report::default();
    for vector in &vectors {
        match vector.conversion.as_str() {
            "to-ir" => run_to_ir(conv, vector, &mut report),
            "from-ir" => run_from_ir(conv, vector, &mut report),
            other => report
                .failures
                .push((vector.name.clone(), format!("unknown conversion {other:?}"))),
        }
    }
    report.executed = vectors.len();
    Ok(Outcome::Ran(report))
}

fn run_to_ir(conv: &dyn Converter, vector: &Vector, report: &mut Report) {
    let Some(expected) = vector.expected_ir.as_ref() else {
        report.failures.push((
            vector.name.clone(),
            "to-ir vector is missing expected_ir".to_string(),
        ));
        return;
    };
    let outcome: Result<(Value, Vec<Loss>), String> = (|| {
        let (doc, losses) = if vector.is_request() {
            let (req, losses) = conv.decode_request_wire(&json_text(&vector.input))?;
            (
                oxa_ir::to_json(&req).map_err(|err| err.to_string())?,
                losses,
            )
        } else {
            let (resp, losses) = conv.decode_response_wire(&json_text(&vector.input))?;
            (
                oxa_ir::to_json(&resp).map_err(|err| err.to_string())?,
                losses,
            )
        };
        let doc: Value = serde_json::from_str(&doc).map_err(|err| err.to_string())?;
        Ok((doc, losses))
    })();
    match outcome {
        Ok((doc, losses)) => {
            if let Err(err) = compare_json(expected, &doc) {
                report
                    .failures
                    .push((vector.name.clone(), format!("expected_ir mismatch: {err}")));
            }
            compare_reported_losses(vector, &losses, report);
        }
        Err(err) => report
            .failures
            .push((vector.name.clone(), format!("decode failed: {err}"))),
    }
}

fn run_from_ir(conv: &dyn Converter, vector: &Vector, report: &mut Report) {
    let Some(expected) = vector.expected_output.as_ref() else {
        report.failures.push((
            vector.name.clone(),
            "from-ir vector is missing expected_output".to_string(),
        ));
        return;
    };
    let outcome: Result<(Value, Vec<Loss>), String> = (|| {
        let (out, losses) = if vector.is_request() {
            let req: Request =
                oxa_ir::from_json(&json_text(&vector.input)).map_err(|err| err.to_string())?;
            conv.encode_request_ir(&req)?
        } else {
            let resp: Response =
                oxa_ir::from_json(&json_text(&vector.input)).map_err(|err| err.to_string())?;
            conv.encode_response_ir(&resp)?
        };
        let out: Value = serde_json::from_str(&out).map_err(|err| err.to_string())?;
        Ok((out, losses))
    })();
    match outcome {
        Ok((out, losses)) => {
            if let Err(err) = compare_json(expected, &out) {
                report.failures.push((
                    vector.name.clone(),
                    format!("expected_output mismatch: {err}"),
                ));
            }
            compare_reported_losses(vector, &losses, report);
        }
        Err(err) => report
            .failures
            .push((vector.name.clone(), format!("encode failed: {err}"))),
    }
}

pub(crate) fn compare_reported_losses(vector: &Vector, losses: &[Loss], report: &mut Report) {
    let reported: Vec<LossRecord> = losses.iter().map(LossRecord::from).collect();
    if let Err(err) = compare_losses(&vector.expected_losses, &reported) {
        report
            .failures
            .push((vector.name.clone(), format!("losses mismatch: {err}")));
    }
}

pub(crate) fn json_text(value: &Value) -> String {
    serde_json::to_string(value).expect("vector JSON re-serialization cannot fail")
}
