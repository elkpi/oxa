//! Structural conversion errors (spec/02 §4): only malformed input that
//! cannot be interpreted at all. Semantic unmappables are losses, never
//! errors.

use std::fmt;

/// A structural conversion error.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Error {
    message: String,
}

impl Error {
    pub(crate) fn new(message: impl Into<String>) -> Self {
        Error {
            message: message.into(),
        }
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for Error {}
