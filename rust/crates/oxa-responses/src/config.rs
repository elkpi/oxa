//! Per-converter configuration. With no table installed the model mapping is
//! exactly the identity (no table installed is exactly an empty table,
//! spec/03 §2).

use oxa_modelmap::Table;

/// Configures a conversion call.
#[derive(Clone, Debug, Default)]
pub struct Config {
    /// Caller-supplied model-name table (spec/03 §2). Lookup is exact-match
    /// with identity fallback; the table applies on both conversion directions.
    pub models: Option<Table>,
}

impl Config {
    /// Installs a caller-supplied model-name table.
    pub fn with_model_map(table: Table) -> Self {
        Config {
            models: Some(table),
        }
    }

    pub(crate) fn map_model(&self, model: &str) -> String {
        match &self.models {
            Some(table) => table.map(model),
            None => model.to_string(),
        }
    }
}
