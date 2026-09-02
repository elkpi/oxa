//! The cross-protocol vector runner (spec/02 §6): each vector composes
//! source decode → IR → target encode; losses concatenate in that order and
//! compare as an unordered set.

use std::path::Path;

use crate::compare::compare_json;
use crate::load::{Vector, find_repo_root, load_vectors};
use crate::run::{Converter, Outcome, Report, compare_reported_losses};

/// Executes every nonstream cross-protocol vector whose source and target
/// endpoints match the two converters' `face()` protocol names, locating the
/// repo root from the current working directory. A matched pair with no
/// vectors fails.
pub fn run_cross(source: &dyn Converter, target: &dyn Converter) -> Result<Outcome, String> {
    run_cross_in(Path::new("."), source, target)
}

/// [`run_cross`] with an explicit starting directory for the repo-root walk.
pub fn run_cross_in(
    dir: &Path,
    source: &dyn Converter,
    target: &dyn Converter,
) -> Result<Outcome, String> {
    let Some(root) = find_repo_root(dir) else {
        return Ok(Outcome::Skipped);
    };
    let vectors = load_vectors(&root, "cross", "nonstream")?;
    let matched = cross_vectors_for(source, target, &vectors);
    if matched.is_empty() {
        return Err(format!(
            "no cross vectors found for pair {} -> {}",
            source.face(),
            target.face()
        ));
    }
    let executed = matched.len();
    let mut report = Report::default();
    for vector in &matched {
        run_cross_vector(source, target, vector, &mut report);
    }
    report.executed = executed;
    Ok(Outcome::Ran(report))
}

/// Selects the vectors whose endpoints match the pair.
pub fn cross_vectors_for<'a>(
    source: &dyn Converter,
    target: &dyn Converter,
    vectors: &'a [Vector],
) -> Vec<&'a Vector> {
    vectors
        .iter()
        .filter(|vector| {
            vector.source.protocol == source.face() && vector.target.protocol == target.face()
        })
        .collect()
}

fn run_cross_vector(
    source: &dyn Converter,
    target: &dyn Converter,
    vector: &Vector,
    report: &mut Report,
) {
    if vector.conversion != "protocol-to-protocol" {
        report.failures.push((
            vector.name.clone(),
            format!(
                "cross vector has conversion {:?}, want protocol-to-protocol",
                vector.conversion
            ),
        ));
        return;
    }
    let Some(expected) = vector.expected_output.as_ref() else {
        report.failures.push((
            vector.name.clone(),
            "cross vector is missing expected_output".to_string(),
        ));
        return;
    };
    let outcome: Result<(serde_json::Value, Vec<oxa_ir::Loss>), String> = (|| {
        let input = vector.input_text();
        let (out, losses) = if vector.is_request() {
            let (req, decode_losses) = source.decode_request_wire(&input)?;
            let (out, encode_losses) = target.encode_request_ir(&req)?;
            (out, [decode_losses, encode_losses].concat())
        } else {
            let (resp, decode_losses) = source.decode_response_wire(&input)?;
            let (out, encode_losses) = target.encode_response_ir(&resp)?;
            (out, [decode_losses, encode_losses].concat())
        };
        let out: serde_json::Value = serde_json::from_str(&out).map_err(|err| err.to_string())?;
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
        Err(err) => report.failures.push((
            vector.name.clone(),
            format!("cross conversion failed: {err}"),
        )),
    }
}
