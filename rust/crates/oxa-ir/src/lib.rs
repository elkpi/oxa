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
mod loss;
mod native;
mod request;
mod response;

pub use checker::{Violation, validate_event_stream, validate_event_stream_for_encoder};
pub use codec::{Error, SPEC_VERSION, from_json, to_json};
pub use event::{Delta, Event, EventStream};
pub use loss::{Loss, LossReason};
pub use native::{
    CACHE_CONTROL_LOSS_DETAIL, DEFER_LOADING_LOSS_DETAIL, DOCUMENT_CITATIONS_LOSS_DETAIL,
    DOCUMENT_CONTEXT_LOSS_DETAIL, DOCUMENT_TITLE_LOSS_DETAIL, NATIVE_KIND_CACHE_CONTROL,
    NATIVE_KIND_CONTAINER, NATIVE_KIND_CONTEXT_MANAGEMENT, NATIVE_KIND_DEFER_LOADING,
    NATIVE_KIND_DOCUMENT_CITATIONS, NATIVE_KIND_DOCUMENT_CONTEXT, NATIVE_KIND_DOCUMENT_TITLE,
    NATIVE_KIND_OUTPUT_EFFORT, NATIVE_KIND_THINKING_BUDGET, NATIVE_KIND_THINKING_DISPLAY,
    NATIVE_KIND_THINKING_MODE, NATIVE_SOURCE_ANTHROPIC, NATIVE_SOURCE_OPENAI,
    omitted_native_request_losses, validate_native_request,
};
pub use request::{
    ApplyPatchCallStatus, ApplyPatchOperation, ApplyPatchResultStatus, Block, CustomToolFormat,
    CustomToolOutput, Message, NativeAnnotation, NativeOmission, NativeProjection, Params,
    ReasoningEffort, Request, ResponseFormat, Role, SystemBlock, Tool, ToolChoice, ToolChoiceMode,
    ToolType,
};
pub use response::{InputTokensDetails, OutputTokensDetails, Response, StopReason, Usage};
