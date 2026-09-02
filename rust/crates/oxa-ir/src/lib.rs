//! oxa-ir: the face-neutral intermediate representation defined by
//! spec/01-intermediate-representation.md. Types mirror the Go reference
//! field-for-field; JSON property names are pinned explicitly; tool inputs
//! and argument fragments are opaque raw JSON text (INV-1).
//!
//! This crate is the Rust side of the IR contract version pinned by
//! `spec/schema/ir.schema.json`; behavior is locked by the shared
//! `vectors/` golden set, not by this crate alone.

mod checker;
mod codec;
mod event;
mod request;
mod response;

pub use checker::{Violation, validate_event_stream, validate_event_stream_for_encoder};
pub use codec::{Error, SPEC_VERSION, from_json, to_json};
pub use event::{Delta, Event, EventStream};
pub use request::{
    Block, Message, Params, Request, Role, SystemBlock, Tool, ToolChoice, ToolChoiceMode,
};
pub use response::{Response, StopReason, Usage};
