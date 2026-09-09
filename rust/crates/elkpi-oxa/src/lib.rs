//! oxa: pure, in-process protocol-conversion library for OpenAI and Anthropic.
//!
//! This crate is the top-level entrypoint re-exporting the protocol face,
//! IR, and utility crates.

pub use oxa_ir as ir;
pub use oxa_modelmap as modelmap;
pub use oxa_sse as sse;

/// OpenAI protocol faces.
pub mod openai {
    pub use oxa_chatcompletions as chatcompletions;
    pub use oxa_responses as responses;
}

/// Anthropic protocol faces.
pub mod anthropic {
    pub use oxa_anthropic as messages;
}

// Flat re-exports
pub use oxa_anthropic as messages;
pub use oxa_chatcompletions as chatcompletions;
pub use oxa_responses as responses;
