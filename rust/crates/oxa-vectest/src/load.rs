//! Vector loading and repository-root discovery (vectors/README.md "How
//! implementations locate vectors").

use std::fs;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use serde_json::value::RawValue;

/// Walks up from `dir` looking for a directory containing both `vectors/`
/// and `.git/`. Returns `None` when none is found before the filesystem root
/// (the crate is being consumed as a dependency outside the monorepo).
pub fn find_repo_root(dir: &Path) -> Option<PathBuf> {
    let mut current = Some(dir);
    while let Some(dir) = current {
        if dir.join("vectors").exists() && dir.join(".git").exists() {
            return Some(dir.to_path_buf());
        }
        current = dir.parent();
    }
    None
}

/// Names one side of a vector (vector.schema.json `$defs/endpoint`).
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct Endpoint {
    #[serde(rename = "protocol", default)]
    pub protocol: String,
}

/// Mirrors `oxa_ir::Loss` with a plain-string reason so the harness stays
/// generic over the IR reason catalog. Losses always compare as unordered
/// sets keyed on `(path, field, reason)`; `detail` is informational.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct LossRecord {
    #[serde(rename = "path", default)]
    pub path: String,
    #[serde(rename = "field", default)]
    pub field: String,
    #[serde(rename = "reason", default)]
    pub reason: String,
    #[serde(rename = "detail", default)]
    pub detail: String,
}

impl From<&oxa_ir::Loss> for LossRecord {
    fn from(loss: &oxa_ir::Loss) -> Self {
        LossRecord {
            path: loss.path.clone(),
            field: loss.field.clone(),
            reason: loss.reason.as_str().to_string(),
            detail: loss.detail.clone(),
        }
    }
}

/// The subset of the vector file format (spec/schema/vector.schema.json) the
/// harness consumes.
#[derive(Clone, Debug, Default, Serialize, Deserialize)]
pub struct Vector {
    #[serde(rename = "name", default)]
    pub name: String,
    #[serde(rename = "description", default)]
    pub description: String,
    #[serde(rename = "mode", default)]
    pub mode: String,
    #[serde(rename = "conversion", default)]
    pub conversion: String,
    #[serde(rename = "source", default)]
    pub source: Endpoint,
    #[serde(rename = "target", default)]
    pub target: Endpoint,
    #[serde(rename = "input", default)]
    pub input: serde_json::Value,
    /// Raw source text of the top-level `input` value, retained by
    /// [`load_vectors`] for face decoders whose nested fields require lexical
    /// JSON preservation under INV-1.
    #[doc(hidden)]
    #[serde(skip)]
    pub input_raw: Option<Box<RawValue>>,
    #[serde(rename = "expected_ir", default)]
    pub expected_ir: Option<serde_json::Value>,
    #[serde(rename = "expected_output", default)]
    pub expected_output: Option<serde_json::Value>,
    #[serde(rename = "expected_losses", default)]
    pub expected_losses: Vec<LossRecord>,
    #[serde(rename = "tags", default)]
    pub tags: Vec<String>,
}

impl Vector {
    /// Whether the vector exercises the request direction, based on its
    /// tags ("request" or "response"; every vector carries exactly one of
    /// them).
    pub fn is_request(&self) -> bool {
        !self.tags.iter().any(|tag| tag == "response")
    }

    /// Returns the original top-level `input` JSON source when the vector was
    /// loaded from disk. Programmatically constructed vectors fall back to a
    /// structural serialization for test-fixture ergonomics.
    pub(crate) fn input_text(&self) -> std::borrow::Cow<'_, str> {
        match self.input_raw.as_deref() {
            Some(raw) => std::borrow::Cow::Borrowed(raw.get()),
            None => std::borrow::Cow::Owned(
                serde_json::to_string(&self.input)
                    .expect("vector JSON re-serialization cannot fail"),
            ),
        }
    }
}

#[derive(Deserialize)]
struct RawInput {
    #[serde(rename = "input", default)]
    input: Option<Box<RawValue>>,
}

/// Reads every vector file of one face and mode from
/// `<root>/vectors/<face>/<mode>/*.json`. The returned vectors are sorted by
/// file name so failures are deterministic. A missing directory yields an
/// empty list.
pub fn load_vectors(root: &Path, face: &str, mode: &str) -> Result<Vec<Vector>, String> {
    let dir = root.join("vectors").join(face).join(mode);
    let entries = match fs::read_dir(&dir) {
        Ok(entries) => entries,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(err) => return Err(format!("read {}: {err}", dir.display())),
    };
    let mut names: Vec<String> = Vec::new();
    for entry in entries {
        let entry = entry.map_err(|err| format!("read {}: {err}", dir.display()))?;
        let name = entry.file_name().to_string_lossy().into_owned();
        if entry.path().is_dir() || !name.ends_with(".json") {
            continue;
        }
        names.push(name);
    }
    names.sort();
    let mut vectors = Vec::new();
    for name in names {
        let raw =
            fs::read_to_string(dir.join(&name)).map_err(|err| format!("read {name}: {err}"))?;
        let mut vector: Vector =
            serde_json::from_str(&raw).map_err(|err| format!("parse {name}: {err}"))?;
        let raw_input: RawInput =
            serde_json::from_str(&raw).map_err(|err| format!("parse {name}: {err}"))?;
        vector.input_raw = raw_input.input;
        vectors.push(vector);
    }
    Ok(vectors)
}
