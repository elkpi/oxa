//! The single, optional model-renaming injection point defined by spec/03.
//! oxa libraries carry no built-in model knowledge; callers may supply a
//! [`Table`], and the table is applied to the model value on both conversion
//! directions. The model string is otherwise opaque and passes through
//! verbatim.

use std::collections::BTreeMap;

/// Maps model names to model names. Lookup is exact-match on the keys; on a
/// miss (or with an empty table) the identity fallback applies and the value
/// is returned unchanged. No table installed is exactly an empty table.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Table {
    entries: BTreeMap<String, String>,
}

impl Table {
    pub fn new() -> Self {
        Self::default()
    }

    /// Installs one exact-match mapping.
    pub fn insert(&mut self, from: impl Into<String>, to: impl Into<String>) {
        self.entries.insert(from.into(), to.into());
    }

    /// Returns the table entry for `model`, or `model` unchanged when there
    /// is none.
    pub fn map(&self, model: &str) -> String {
        self.entries
            .get(model)
            .cloned()
            .unwrap_or_else(|| model.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_table_is_the_identity() {
        let table = Table::new();
        assert_eq!(table.map("gpt-4o-mini"), "gpt-4o-mini");
    }

    #[test]
    fn exact_matches_are_rewritten() {
        let mut table = Table::new();
        table.insert("gpt-4o-mini", "claude-haiku-4-5");
        assert_eq!(table.map("gpt-4o-mini"), "claude-haiku-4-5");
    }

    #[test]
    fn misses_fall_back_to_identity() {
        let mut table = Table::new();
        table.insert("gpt-4o-mini", "claude-haiku-4-5");
        assert_eq!(table.map("gpt-4o"), "gpt-4o");
    }
}
