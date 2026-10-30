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

/// A citation reference attached to a TextBlock (since 2.4.11).
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct TextCitation {
    #[serde(rename = "source")]
    pub source: String,
    #[serde(rename = "value")]
    pub value: String,
}

/// A content block (spec/01 §3.4). Sealed; includes the Spec 2.0 thinking variant.
/// `ToolUseBlock::input` is opaque raw JSON text (INV-1): it is a plain
/// string and is never parsed or re-serialized by any conversion path.
#[derive(Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
pub enum Block {
    #[serde(rename = "text")]
    Text {
        #[serde(rename = "text")]
        text: String,
        #[serde(rename = "citations", default, skip_serializing_if = "Option::is_none")]
        citations: Option<Vec<TextCitation>>,
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
    #[serde(rename = "redacted_thinking")]
    RedactedThinking {
        #[serde(rename = "data")]
        data: String,
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
    #[serde(rename = "document")]
    Document {
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
        #[serde(rename = "file_id", default, skip_serializing_if = "Option::is_none")]
        file_id: Option<String>,
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
    #[serde(rename = "web_search_call")]
    WebSearchCall {
        id: String,
        status: String,
        action: Value,
        #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
        extra: BTreeMap<String, Value>,
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
            | Block::RedactedThinking { native, .. }
            | Block::Image { native, .. }
            | Block::Document { native, .. }
            | Block::ToolUse { native, .. }
            | Block::CustomToolUse { native, .. }
            | Block::CustomToolResult { native, .. }
            | Block::ApplyPatchToolUse { native, .. }
            | Block::ApplyPatchToolResult { native, .. }
            | Block::ToolResult { native, .. } => native,
            Block::WebSearchCall { .. } => &[],
        }
    }
}

impl std::fmt::Debug for Block {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Block::Text {
                text,
                citations,
                native,
            } => f
                .debug_struct("Text")
                .field("text", text)
                .field("citations", citations)
                .field("native", native)
                .finish(),
            Block::Thinking {
                thinking,
                signature,
                native,
            } => f
                .debug_struct("Thinking")
                .field("thinking", thinking)
                .field("signature", signature)
                .field("native", native)
                .finish(),
            Block::RedactedThinking { data, native } => f
                .debug_struct("RedactedThinking")
                .field("data", &format!("[REDACTED len={}]", data.len()))
                .field("native", native)
                .finish(),
            Block::Image {
                media_type,
                data,
                url,
                native,
            } => f
                .debug_struct("Image")
                .field("media_type", media_type)
                .field("data", data)
                .field("url", url)
                .field("native", native)
                .finish(),
            Block::Document {
                media_type,
                data,
                url,
                file_id,
                native,
            } => f
                .debug_struct("Document")
                .field("media_type", media_type)
                .field("data", data)
                .field("url", url)
                .field("file_id", file_id)
                .field("native", native)
                .finish(),
            Block::ToolUse {
                id,
                name,
                input,
                native,
            } => f
                .debug_struct("ToolUse")
                .field("id", id)
                .field("name", name)
                .field("input", input)
                .field("native", native)
                .finish(),
            Block::CustomToolUse {
                id,
                name,
                input,
                native,
            } => f
                .debug_struct("CustomToolUse")
                .field("id", id)
                .field("name", name)
                .field("input", input)
                .field("native", native)
                .finish(),
            Block::CustomToolResult {
                tool_use_id,
                output,
                native,
            } => f
                .debug_struct("CustomToolResult")
                .field("tool_use_id", tool_use_id)
                .field("output", output)
                .field("native", native)
                .finish(),
            Block::ApplyPatchToolUse {
                id,
                status,
                operation,
                native,
            } => f
                .debug_struct("ApplyPatchToolUse")
                .field("id", id)
                .field("status", status)
                .field("operation", operation)
                .field("native", native)
                .finish(),
            Block::ApplyPatchToolResult {
                tool_use_id,
                status,
                output,
                native,
            } => f
                .debug_struct("ApplyPatchToolResult")
                .field("tool_use_id", tool_use_id)
                .field("status", status)
                .field("output", output)
                .field("native", native)
                .finish(),
            Block::WebSearchCall {
                id,
                status,
                action,
                extra,
            } => f
                .debug_struct("WebSearchCall")
                .field("id", id)
                .field("status", status)
                .field("action", action)
                .field("extra", extra)
                .finish(),
            Block::ToolResult {
                tool_use_id,
                content,
                is_error,
                native,
            } => f
                .debug_struct("ToolResult")
                .field("tool_use_id", tool_use_id)
                .field("content", content)
                .field("is_error", is_error)
                .field("native", native)
                .finish(),
        }
    }
}

/// A function or custom tool definition (spec/01 §3.5). Absent `tool_type`
/// means the legacy function declaration and retains its historical JSON shape.
#[derive(Clone, Debug, PartialEq)]
pub struct Tool {
    pub tool_type: Option<ToolType>,
    pub name: String,
    pub description: Option<String>,
    pub input_schema: Option<Value>,
    pub strict: Option<bool>,
    pub format: Option<CustomToolFormat>,
    pub native: Vec<NativeAnnotation>,
    pub source: Option<String>,
    pub definition: Option<String>,
}

impl Serialize for Tool {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        use serde::ser::SerializeStruct;
        self.validate_strict().map_err(serde::ser::Error::custom)?;
        let len = usize::from(self.tool_type.is_some())
            + usize::from(!self.name.is_empty())
            + usize::from(self.description.is_some())
            + usize::from(self.input_schema.is_some())
            + usize::from(self.strict.is_some())
            + usize::from(self.format.is_some())
            + usize::from(!self.native.is_empty())
            + usize::from(self.source.is_some())
            + usize::from(self.definition.is_some());
        let mut s = serializer.serialize_struct("Tool", len)?;
        if let Some(value) = &self.tool_type {
            s.serialize_field("type", value)?;
        }
        if !self.name.is_empty() {
            s.serialize_field("name", &self.name)?;
        }
        if let Some(value) = &self.description {
            s.serialize_field("description", value)?;
        }
        if let Some(value) = &self.input_schema {
            s.serialize_field("input_schema", value)?;
        }
        if let Some(value) = self.strict {
            s.serialize_field("strict", &value)?;
        }
        if let Some(value) = &self.format {
            s.serialize_field("format", value)?;
        }
        if !self.native.is_empty() {
            s.serialize_field("native", &self.native)?;
        }
        if let Some(value) = &self.source {
            s.serialize_field("source", value)?;
        }
        if let Some(value) = &self.definition {
            s.serialize_field("definition", value)?;
        }
        s.end()
    }
}

impl Tool {
    /// Checks that strictness is only present on function tools.
    pub fn validate_strict(&self) -> Result<(), String> {
        if self.strict.is_some() && !matches!(self.tool_type, None | Some(ToolType::Function)) {
            return Err("ir: strict is only allowed on function tools".into());
        }
        Ok(())
    }

    /// Validates strictness and native-tool boundaries (spec/16 N-NT-1).
    pub fn validate(&self) -> Result<(), String> {
        self.validate_strict()?;
        if self.tool_type == Some(ToolType::Native) {
            if self.name.is_empty()
                || self.source.as_deref().is_none_or(str::is_empty)
                || self.definition.as_deref().is_none_or(str::is_empty)
                || self.description.is_some()
                || self.input_schema.is_some()
                || self.format.is_some()
                || !self.native.is_empty()
            {
                return Err(
                    "ir: native tool requires name/source/definition and cannot carry other tool configuration"
                        .into(),
                );
            }
            let value: Value = serde_json::from_str(self.definition.as_deref().unwrap_or_default())
                .map_err(|error| format!("ir: native tool definition: {error}"))?;
            let object = value
                .as_object()
                .ok_or("ir: native tool definition must be a JSON object")?;
            let kind = object
                .get("type")
                .and_then(Value::as_str)
                .unwrap_or_default();
            if kind.is_empty() || kind == "function" || kind == "custom" {
                return Err("ir: native tool definition requires a native type".into());
            }
            let expected_name = object
                .get("name")
                .and_then(Value::as_str)
                .or_else(|| object.get("server_label").and_then(Value::as_str))
                .unwrap_or(kind);
            if expected_name != self.name.as_str() {
                return Err("ir: native tool definition name must match tool name".into());
            }
            return Ok(());
        }
        if self.source.is_some() || self.definition.is_some() {
            return Err("ir: only native tools may carry source/definition".into());
        }
        Ok(())
    }
}

fn deserialize_tool_strict<'de, D>(deserializer: D) -> Result<Option<bool>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    bool::deserialize(deserializer).map(Some)
}

impl<'de> Deserialize<'de> for Tool {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        /// Shape mirror without the native invariants; validation runs below.
        #[derive(serde::Deserialize)]
        struct RawTool {
            #[serde(rename = "type", default)]
            tool_type: Option<ToolType>,
            #[serde(rename = "name", default)]
            name: String,
            #[serde(rename = "description", default)]
            description: Option<String>,
            #[serde(rename = "input_schema", default)]
            input_schema: Option<Value>,
            #[serde(default, deserialize_with = "deserialize_tool_strict")]
            strict: Option<bool>,
            #[serde(rename = "format", default)]
            format: Option<CustomToolFormat>,
            #[serde(rename = "native", default)]
            native: Vec<NativeAnnotation>,
            #[serde(rename = "source", default)]
            source: Option<String>,
            #[serde(rename = "definition", default)]
            definition: Option<String>,
        }
        let raw = RawTool::deserialize(deserializer)?;
        let tool = Tool {
            tool_type: raw.tool_type,
            name: raw.name,
            description: raw.description,
            input_schema: raw.input_schema,
            strict: raw.strict,
            format: raw.format,
            native: raw.native,
            source: raw.source,
            definition: raw.definition,
        };
        tool.validate().map_err(serde::de::Error::custom)?;
        Ok(tool)
    }
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
        #[serde(default, skip_serializing_if = "String::is_empty")]
        name: String,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        description: Option<String>,
        schema: Map<String, Value>,
        #[serde(skip)]
        schema_raw: Option<String>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        strict: Option<bool>,
    },
}
