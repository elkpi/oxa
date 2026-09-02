//! The stream vector runner: native stream JSON is decoded and encoded by
//! face-local test adapters, so this generic module never constructs
//! provider wire types.

use std::path::Path;

use oxa_ir::{Event, EventStream, Loss};
use serde_json::Value;

use crate::compare::compare_json;
use crate::load::{find_repo_root, load_vectors};
use crate::run::{Outcome, Report, compare_reported_losses, json_text};

/// The face-implementation surface that [`run_stream`] drives. Adapters are
/// stateful (they retain the face decoder/encoder), so methods take `&mut
/// self`.
pub trait StreamConverter {
    /// The vectors/ directory name of the face.
    fn face(&self) -> &'static str;
    /// Decodes one native event (one element of `input.events`).
    fn decode_native_event(&mut self, event: &Value) -> Result<Vec<Event>, String>;
    /// Supplies decoder terminal output.
    fn flush_decoder(&mut self) -> Result<Vec<Event>, String>;
    /// Returns the full loss list; the runner calls it only after all native
    /// events and [`StreamConverter::flush_decoder`] have run.
    fn decoder_losses(&self) -> Vec<Loss>;
    /// Applies one canonical IR event, returning ordered native event
    /// documents plus reported losses.
    fn apply_ir_event(&mut self, event: &Event) -> Result<(Vec<Value>, Vec<Loss>), String>;
    /// Optional per-vector state reset so a converter remains isolated even
    /// when a prior vector ends with a conversion error. Defaults to a no-op.
    fn reset_stream_vector(&mut self) {}
}

/// Executes every stream golden vector for the converter's face, locating
/// the repo root from the current working directory. Requires at least one
/// stream vector when running from the monorepo.
pub fn run_stream(conv: &mut dyn StreamConverter) -> Result<Outcome, String> {
    run_stream_in(Path::new("."), conv)
}

/// [`run_stream`] with an explicit starting directory for the repo-root walk.
pub fn run_stream_in(dir: &Path, conv: &mut dyn StreamConverter) -> Result<Outcome, String> {
    let Some(root) = find_repo_root(dir) else {
        return Ok(Outcome::Skipped);
    };
    let vectors = load_vectors(&root, conv.face(), "stream")?;
    if vectors.is_empty() {
        return Err(format!(
            "no stream vectors found for face {}; the harness must execute at least one",
            conv.face()
        ));
    }
    let mut report = Report::default();
    for vector in &vectors {
        conv.reset_stream_vector();
        match vector.conversion.as_str() {
            "to-ir" => {
                let Some(expected) = vector.expected_ir.as_ref() else {
                    report.failures.push((
                        vector.name.clone(),
                        "to-ir vector is missing expected_ir".to_string(),
                    ));
                    continue;
                };
                match stream_to_ir(conv, &vector.input) {
                    Ok((actual, losses)) => {
                        if let Err(err) = compare_json(expected, &actual) {
                            report.failures.push((
                                vector.name.clone(),
                                format!("expected_ir mismatch: {err}"),
                            ));
                        }
                        compare_reported_losses(vector, &losses, &mut report);
                    }
                    Err(err) => report
                        .failures
                        .push((vector.name.clone(), format!("decode stream failed: {err}"))),
                }
            }
            "from-ir" => {
                let Some(expected) = vector.expected_output.as_ref() else {
                    report.failures.push((
                        vector.name.clone(),
                        "from-ir vector is missing expected_output".to_string(),
                    ));
                    continue;
                };
                if let Err(err) = native_events(expected) {
                    report.failures.push((
                        vector.name.clone(),
                        format!("expected_output events envelope: {err}"),
                    ));
                    continue;
                }
                match stream_from_ir(conv, &vector.input) {
                    Ok((actual, losses)) => {
                        if let Err(err) = compare_json(expected, &actual) {
                            report.failures.push((
                                vector.name.clone(),
                                format!("expected_output mismatch: {err}"),
                            ));
                        }
                        compare_reported_losses(vector, &losses, &mut report);
                    }
                    Err(err) => report
                        .failures
                        .push((vector.name.clone(), format!("encode stream failed: {err}"))),
                }
            }
            other => report
                .failures
                .push((vector.name.clone(), format!("unknown conversion {other:?}"))),
        }
    }
    report.executed = vectors.len();
    Ok(Outcome::Ran(report))
}

/// Feeds every native event of `input` through the converter, appends the
/// flush output, and returns the canonical IR event stream document plus the
/// decoder losses.
pub fn stream_to_ir(
    conv: &mut dyn StreamConverter,
    input: &Value,
) -> Result<(Value, Vec<Loss>), String> {
    let native_events = native_events(input)?;
    let mut events = Vec::new();
    for (index, raw) in native_events.iter().enumerate() {
        let decoded = conv
            .decode_native_event(raw)
            .map_err(|err| format!("decode native event {index} ({}): {err}", bounded(raw)))?;
        events.extend(decoded);
    }
    let flushed = conv
        .flush_decoder()
        .map_err(|err| format!("flush stream decoder: {err}"))?;
    events.extend(flushed);
    let losses = conv.decoder_losses();
    let doc = oxa_ir::to_json(&EventStream { events }).map_err(|err| err.to_string())?;
    let doc: Value = serde_json::from_str(&doc).map_err(|err| err.to_string())?;
    Ok((doc, losses))
}

/// Applies every IR event of the `input` stream through the converter and
/// returns the native `{"events": [...]}` envelope plus the concatenated
/// encoder losses.
pub fn stream_from_ir(
    conv: &mut dyn StreamConverter,
    input: &Value,
) -> Result<(Value, Vec<Loss>), String> {
    let stream: EventStream = oxa_ir::from_json(&json_text(input))
        .map_err(|err| format!("unmarshal IR event stream: {err}"))?;
    let mut native_events = Vec::new();
    let mut losses = Vec::new();
    for (index, event) in stream.events.iter().enumerate() {
        let (encoded, reported) = conv
            .apply_ir_event(event)
            .map_err(|err| format!("apply IR event {index}: {err}"))?;
        native_events.extend(encoded);
        losses.extend(reported);
    }
    Ok((serde_json::json!({ "events": native_events }), losses))
}

const MAX_NATIVE_EVENT_ERROR_CHARS: usize = 512;

fn bounded(value: &Value) -> String {
    let text = json_text(value);
    if text.chars().count() <= MAX_NATIVE_EVENT_ERROR_CHARS {
        return text;
    }
    let mut bounded: String = text.chars().take(MAX_NATIVE_EVENT_ERROR_CHARS).collect();
    bounded.push('…');
    bounded
}

fn native_events(input: &Value) -> Result<Vec<Value>, String> {
    let Some(envelope) = input.as_object() else {
        return Err("input is not an object".to_string());
    };
    let Some(events) = envelope.get("events") else {
        return Err("missing events envelope".to_string());
    };
    let Some(events) = events.as_array() else {
        return Err("events is not an array".to_string());
    };
    Ok(events.clone())
}
