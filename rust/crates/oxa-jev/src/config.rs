//! Per-converter configuration (spec/03 §2) plus the response-encode context
//! that enables exact answer-type recovery (spec/17 §4.1).

use std::collections::BTreeMap;

use oxa_modelmap::Table;

use crate::question::JevQuestion;

/// Configures a conversion call.
#[derive(Clone, Debug, Default)]
pub struct Config {
    /// Caller-supplied model-name table (spec/03 §2). Lookup is exact-match
    /// with identity fallback; the table applies on both conversion
    /// directions.
    pub models: Option<Table>,
    /// The original request's questions (spec/17 §4.1): answer types recover
    /// exactly from the matching question instead of the value-shape
    /// inference, and criteria ranges are validated.
    pub questions: Option<BTreeMap<String, JevQuestion>>,
}

impl Config {
    /// Installs a caller-supplied model-name table.
    pub fn with_model_map(table: Table) -> Self {
        Config {
            models: Some(table),
            questions: None,
        }
    }

    /// Installs the original request's questions as response-encode context.
    pub fn with_questions(questions: BTreeMap<String, JevQuestion>) -> Self {
        Config {
            models: None,
            questions: Some(questions),
        }
    }

    pub(crate) fn map_model(&self, model: &str) -> String {
        match &self.models {
            Some(table) => table.map(model),
            None => model.to_string(),
        }
    }
}
