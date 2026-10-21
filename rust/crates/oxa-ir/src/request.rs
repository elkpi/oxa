//! Request-side IR types (spec/01 §3). All JSON property names are pinned
//! with explicit `serde(rename)` so no rename-rule inference is involved.

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use std::collections::BTreeMap;

/// A conversation to be sent to a model, face-neutral (spec/01 §3.1).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Request {
    #[serde(rename = "model")]
    pub model: String,
    #[serde(rename = "system", default, skip_serializing_if = "Vec::is_empty")]
    pub system: Vec<SystemBlock>,
    #[serde(rename = "messages")]
    pub messages: Vec<Message>,
    #[serde(rename = "tools", default, skip_serializing_if = "Option::is_none")]
    pub tools: Option<Vec<Tool>>,
    #[serde(
        rename = "tool_choice",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub tool_choice: Option<ToolChoice>,
    #[serde(rename = "params", default, skip_serializing_if = "Option::is_none")]
    pub params: Option<Params>,
    #[serde(rename = "metadata", default, skip_serializing_if = "Option::is_none")]
    pub metadata: Option<BTreeMap<String, String>>,
    #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
    pub native: Vec<NativeAnnotation>,
}

/// System prompt content (spec/01 §3.2). Sealed; exactly one variant in v1.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum SystemBlock {
    #[serde(rename = "text")]
    Text {
        #[serde(rename = "text")]
        text: String,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
}

/// Message role (spec/01 §3.3).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Role {
    #[serde(rename = "user")]
    User,
    #[serde(rename = "assistant")]
    Assistant,
}

/// A message: role plus an ordered block list (spec/01 §3.3).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Message {
    #[serde(rename = "role")]
    pub role: Role,
    #[serde(rename = "content")]
    pub content: Vec<Block>,
}

/// A content block (spec/01 §3.4). Sealed; includes the Spec 2.0 thinking variant.
/// `ToolUseBlock::input` is opaque raw JSON text (INV-1): it is a plain
/// string and is never parsed or re-serialized by any conversion path.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum Block {
    #[serde(rename = "text")]
    Text {
        #[serde(rename = "text")]
        text: String,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
    #[serde(rename = "thinking")]
    Thinking {
        #[serde(rename = "thinking")]
        thinking: String,
        #[serde(rename = "signature", default, skip_serializing_if = "Option::is_none")]
        signature: Option<String>,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
    #[serde(rename = "image")]
    Image {
        #[serde(
            rename = "media_type",
            default,
            skip_serializing_if = "Option::is_none"
        )]
        media_type: Option<String>,
        #[serde(rename = "data", default, skip_serializing_if = "Option::is_none")]
        data: Option<String>,
        #[serde(rename = "url", default, skip_serializing_if = "Option::is_none")]
        url: Option<String>,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
    #[serde(rename = "tool_use")]
    ToolUse {
        #[serde(rename = "id")]
        id: String,
        #[serde(rename = "name")]
        name: String,
        #[serde(rename = "input")]
        input: String,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
    #[serde(rename = "custom_tool_use")]
    CustomToolUse {
        #[serde(rename = "id")]
        id: String,
        #[serde(rename = "name")]
        name: String,
        #[serde(rename = "input")]
        input: String,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
    #[serde(rename = "custom_tool_result")]
    CustomToolResult {
        #[serde(rename = "tool_use_id")]
        tool_use_id: String,
        #[serde(rename = "output")]
        output: CustomToolOutput,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
    #[serde(rename = "apply_patch_tool_use")]
    ApplyPatchToolUse {
        #[serde(rename = "id")]
        id: String,
        #[serde(rename = "status")]
        status: ApplyPatchCallStatus,
        #[serde(rename = "operation")]
        operation: ApplyPatchOperation,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
    #[serde(rename = "apply_patch_tool_result")]
    ApplyPatchToolResult {
        #[serde(rename = "tool_use_id")]
        tool_use_id: String,
        #[serde(rename = "status")]
        status: ApplyPatchResultStatus,
        #[serde(rename = "output", default, skip_serializing_if = "Option::is_none")]
        output: Option<String>,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
    #[serde(rename = "tool_result")]
    ToolResult {
        #[serde(rename = "tool_use_id")]
        tool_use_id: String,
        #[serde(rename = "content")]
        content: Vec<Block>,
        #[serde(rename = "is_error", default, skip_serializing_if = "Option::is_none")]
        is_error: Option<bool>,
        #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
        native: Vec<NativeAnnotation>,
    },
}

impl Block {
    pub fn native(&self) -> &[NativeAnnotation] {
        match self {
            Block::Text { native, .. }
            | Block::Thinking { native, .. }
            | Block::Image { native, .. }
            | Block::ToolUse { native, .. }
            | Block::CustomToolUse { native, .. }
            | Block::CustomToolResult { native, .. }
            | Block::ApplyPatchToolUse { native, .. }
            | Block::ApplyPatchToolResult { native, .. }
            | Block::ToolResult { native, .. } => native,
        }
    }
}

/// A function or custom tool definition (spec/01 §3.5). Absent `tool_type`
/// means the legacy function declaration and retains its historical JSON shape.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Tool {
    #[serde(rename = "type", default, skip_serializing_if = "Option::is_none")]
    pub tool_type: Option<ToolType>,
    #[serde(rename = "name", default, skip_serializing_if = "String::is_empty")]
    pub name: String,
    #[serde(
        rename = "description",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub description: Option<String>,
    #[serde(
        rename = "input_schema",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub input_schema: Option<Value>,
    #[serde(rename = "format", default, skip_serializing_if = "Option::is_none")]
    pub format: Option<CustomToolFormat>,
    #[serde(rename = "native", default, skip_serializing_if = "Vec::is_empty")]
    pub native: Vec<NativeAnnotation>,
    #[serde(rename = "source", default, skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
    #[serde(
        rename = "definition",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub definition: Option<String>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum ToolType {
    #[serde(rename = "function")]
    Function,
    #[serde(rename = "custom")]
    Custom,
    #[serde(rename = "apply_patch")]
    ApplyPatch,
    #[serde(rename = "native")]
    Native,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum ApplyPatchCallStatus {
    #[serde(rename = "in_progress")]
    InProgress,
    #[serde(rename = "completed")]
    Completed,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum ApplyPatchResultStatus {
    #[serde(rename = "completed")]
    Completed,
    #[serde(rename = "failed")]
    Failed,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum ApplyPatchOperation {
    #[serde(rename = "create_file")]
    CreateFile {
        #[serde(rename = "path")]
        path: String,
        #[serde(rename = "diff")]
        diff: String,
    },
    #[serde(rename = "delete_file")]
    DeleteFile {
        #[serde(rename = "path")]
        path: String,
    },
    #[serde(rename = "update_file")]
    UpdateFile {
        #[serde(rename = "path")]
        path: String,
        #[serde(rename = "diff")]
        diff: String,
    },
}

/// Custom format descriptor. Unknown type/syntax values deserialize so face
/// converters can report an unsupported-semantic loss instead of rewriting it.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct CustomToolFormat {
    #[serde(rename = "type")]
    pub format_type: String,
    #[serde(rename = "syntax", default, skip_serializing_if = "Option::is_none")]
    pub syntax: Option<String>,
    #[serde(
        rename = "definition",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub definition: Option<String>,
}

/// Custom result output preserves string-versus-list representation.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(untagged)]
pub enum CustomToolOutput {
    Text(String),
    Content(Vec<Block>),
}

/// Tool choice (spec/01 §3.6). `name`/`tool_type` are present iff mode is `tool`.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct ToolChoice {
    #[serde(rename = "mode")]
    pub mode: ToolChoiceMode,
    #[serde(rename = "name", default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(rename = "tool_type", default, skip_serializing_if = "Option::is_none")]
    pub tool_type: Option<ToolType>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum ToolChoiceMode {
    #[serde(rename = "auto")]
    Auto,
    #[serde(rename = "any")]
    Any,
    #[serde(rename = "tool")]
    Tool,
    #[serde(rename = "none")]
    None,
}

/// Request-side reasoning effort (spec/01 §3.7). Closed set since Spec 2.0.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum ReasoningEffort {
    #[serde(rename = "minimal")]
    Minimal,
    #[serde(rename = "low")]
    Low,
    #[serde(rename = "medium")]
    Medium,
    #[serde(rename = "high")]
    High,
    #[serde(rename = "xhigh")]
    XHigh,
    #[serde(rename = "max")]
    Max,
}

impl ReasoningEffort {
    pub fn as_str(&self) -> &'static str {
        match self {
            ReasoningEffort::Minimal => "minimal",
            ReasoningEffort::Low => "low",
            ReasoningEffort::Medium => "medium",
            ReasoningEffort::High => "high",
            ReasoningEffort::XHigh => "xhigh",
            ReasoningEffort::Max => "max",
        }
    }
}

/// A dialect-specific request option preserved across IR (spec/15).
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct NativeAnnotation {
    #[serde(rename = "source")]
    pub source: String,
    #[serde(rename = "kind")]
    pub kind: String,
    #[serde(rename = "value")]
    pub value: String,
    #[serde(rename = "omission", default, skip_serializing_if = "Option::is_none")]
    pub omission: Option<NativeOmission>,
    #[serde(
        rename = "projection",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub projection: Option<NativeProjection>,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct NativeOmission {
    #[serde(rename = "field")]
    pub field: String,
    #[serde(rename = "reason")]
    pub reason: crate::loss::LossReason,
    #[serde(rename = "detail", default, skip_serializing_if = "Option::is_none")]
    pub detail: Option<String>,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct NativeProjection {
    #[serde(rename = "origin")]
    pub origin: String, // "budget" | "explicit"
    #[serde(rename = "effort")]
    pub effort: ReasoningEffort,
}

/// Sampling parameters (spec/01 §3.7). Absent fields are meaningful:
/// `Option` distinguishes absent from zero, matching the pointer semantics
/// of the Go reference implementation.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct Params {
    #[serde(
        rename = "temperature",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub temperature: Option<f64>,
    #[serde(rename = "top_p", default, skip_serializing_if = "Option::is_none")]
    pub top_p: Option<f64>,
    #[serde(
        rename = "max_tokens",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub max_tokens: Option<i64>,
    #[serde(
        rename = "stop_sequences",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub stop_sequences: Option<Vec<String>>,
    #[serde(
        rename = "reasoning_effort",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub reasoning_effort: Option<ReasoningEffort>,
    #[serde(
        rename = "response_format",
        default,
        skip_serializing_if = "Option::is_none"
    )]
    pub response_format: Option<ResponseFormat>,
}

/// Output formatting preference (spec/01 §3.7.1, since 2.1).
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum ResponseFormat {
    #[serde(rename = "text")]
    Text,
    #[serde(rename = "json_object")]
    JsonObject,
    #[serde(rename = "json_schema")]
    JsonSchema {
        name: String,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        description: Option<String>,
        schema: Map<String, Value>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        strict: Option<bool>,
    },
}
