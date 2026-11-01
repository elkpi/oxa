use std::collections::HashSet;

use crate::codec::Error;
use crate::loss::{Loss, LossReason};
use crate::request::{Block, NativeAnnotation, Request, Role, SystemBlock, ToolChoiceMode};

pub const NATIVE_KIND_THINKING_MODE: &str = "thinking_mode";
pub const NATIVE_KIND_THINKING_BUDGET: &str = "thinking_budget";
pub const NATIVE_KIND_THINKING_DISPLAY: &str = "thinking_display";
pub const NATIVE_KIND_OUTPUT_EFFORT: &str = "output_effort";
pub const NATIVE_KIND_CACHE_CONTROL: &str = "cache_control";
pub const NATIVE_KIND_CONTEXT_MANAGEMENT: &str = "context_management";
pub const NATIVE_KIND_DEFER_LOADING: &str = "defer_loading";
pub const NATIVE_KIND_DOCUMENT_TITLE: &str = "document_title";
pub const NATIVE_KIND_DOCUMENT_CONTEXT: &str = "document_context";
pub const NATIVE_KIND_DOCUMENT_CITATIONS: &str = "document_citations";
pub const NATIVE_KIND_CONTAINER: &str = "container";
pub const NATIVE_KIND_EAGER_INPUT_STREAMING: &str = "eager_input_streaming";
pub const NATIVE_KIND_STORE: &str = "store";
pub const NATIVE_KIND_STREAM: &str = "stream";
pub const NATIVE_KIND_STREAM_OPTIONS: &str = "stream_options";
pub const NATIVE_KIND_MAX_COMPLETION_TOKENS: &str = "max_completion_tokens";
pub const NATIVE_KIND_TOP_K: &str = "top_k";
pub const NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE: &str = "disable_parallel_tool_use";
pub const NATIVE_KIND_PROMPT_CACHE_KEY: &str = "prompt_cache_key";
pub const NATIVE_KIND_PROMPT_CACHE_OPTIONS: &str = "prompt_cache_options";
pub const NATIVE_KIND_PROMPT_CACHE_BREAKPOINT: &str = "prompt_cache_breakpoint";
pub const NATIVE_SOURCE_ANTHROPIC: &str = "anthropic";
pub const NATIVE_SOURCE_OPENAI: &str = "openai";

pub const CACHE_CONTROL_LOSS_DETAIL: &str =
    "anthropic-native cache breakpoint; the target face has no cache_control representation";

pub const PROMPT_CACHE_KEY_LOSS_DETAIL: &str =
    "Anthropic Messages has no prompt_cache_key parameter; prompt cache key is dropped";

pub const PROMPT_CACHE_OPTIONS_LOSS_DETAIL: &str =
    "Anthropic Messages has no prompt_cache_options parameter; prompt cache options are dropped";

pub const PROMPT_CACHE_BREAKPOINT_LOSS_DETAIL: &str = "Anthropic Messages has no prompt_cache_breakpoint parameter; prompt cache breakpoint is dropped";

pub const DEFER_LOADING_LOSS_DETAIL: &str = "deferred tool loading is a provider-side context-assembly instruction; the tool definition is unchanged and loads up front when the target has no tool search";

pub const EAGER_INPUT_STREAMING_LOSS_DETAIL: &str =
    "target does not preserve eager tool input streaming semantics";

pub const STREAM_OPTIONS_LOSS_DETAIL: &str = "target does not emit the requested final usage chunk";

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub struct LossFingerprint {
    pub path: String,
    pub field: String,
    pub reason: LossReason,
}

pub fn fingerprint_losses(losses: &[Loss], limit: usize) -> Vec<LossFingerprint> {
    let n = if limit > 0 && limit < losses.len() {
        limit
    } else {
        losses.len()
    };
    losses[..n]
        .iter()
        .map(|l| LossFingerprint {
            path: l.path.clone(),
            field: l.field.clone(),
            reason: l.reason,
        })
        .collect()
}

pub const DOCUMENT_TITLE_LOSS_DETAIL: &str =
    "anthropic-native document title; the target face has no document representation";

pub const DOCUMENT_CONTEXT_LOSS_DETAIL: &str =
    "anthropic-native document context; the target face has no document representation";

pub const DOCUMENT_CITATIONS_LOSS_DETAIL: &str = "anthropic-native document citations; citations alter response semantics and the target face has no document representation";

#[derive(Clone, Copy, PartialEq, Eq)]
enum HostType {
    Request,
    System,
    Content,
    Tool,
}

pub fn validate_native_request(request: &Request) -> Result<(), Error> {
    validate_annotations(&request.native, HostType::Request, "request")?;

    let mut budget_proj = 0;
    let mut explicit_proj = 0;
    for n in &request.native {
        if let Some(ref proj) = n.projection {
            if proj.origin != "budget" && proj.origin != "explicit" {
                return Err(Error::Custom(format!(
                    "ir: native projection origin \"{}\" invalid",
                    proj.origin
                )));
            }
            if proj.origin == "budget" {
                if n.kind != NATIVE_KIND_THINKING_BUDGET {
                    return Err(Error::Custom(
                        "ir: budget projection is only allowed on thinking_budget".into(),
                    ));
                }
                budget_proj += 1;
                if budget_proj > 1 {
                    return Err(Error::Custom(
                        "ir: duplicate budget projection on request".into(),
                    ));
                }
            } else if proj.origin == "explicit" {
                if n.kind != NATIVE_KIND_OUTPUT_EFFORT {
                    return Err(Error::Custom(
                        "ir: explicit projection is only allowed on output_effort".into(),
                    ));
                }
                explicit_proj += 1;
                if explicit_proj > 1 {
                    return Err(Error::Custom(
                        "ir: duplicate explicit projection on request".into(),
                    ));
                }
            }
        }
    }

    if budget_proj > 0 && explicit_proj > 0 {
        return Err(Error::Custom(
            "ir: conflicting effort projections on request".into(),
        ));
    }

    let mut has_between_tools = false;
    let mut has_budget_or_display = false;
    let mut has_disable_parallel = false;
    for n in &request.native {
        if n.source == NATIVE_SOURCE_ANTHROPIC {
            if n.kind == NATIVE_KIND_THINKING_MODE && n.value == r#""between_tools""# {
                has_between_tools = true;
            } else if n.kind == NATIVE_KIND_THINKING_BUDGET
                || n.kind == NATIVE_KIND_THINKING_DISPLAY
            {
                has_budget_or_display = true;
            } else if n.kind == NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE {
                has_disable_parallel = true;
            }
        }
    }
    if has_between_tools && has_budget_or_display {
        return Err(Error::Custom(
            "anthropic: cannot specify budget_tokens or display with thinking.type between_tools"
                .into(),
        ));
    }
    if has_disable_parallel {
        if let Some(ref tc) = request.tool_choice {
            if tc.mode == ToolChoiceMode::None {
                return Err(Error::Custom(
                    "anthropic: cannot specify disable_parallel_tool_use when tool_choice is none or not specified"
                        .into(),
                ));
            }
        } else {
            return Err(Error::Custom(
                "anthropic: cannot specify disable_parallel_tool_use when tool_choice is none or not specified"
                    .into(),
            ));
        }
    }

    for (i, sb) in request.system.iter().enumerate() {
        let SystemBlock::Text { native, .. } = sb;
        validate_annotations(native, HostType::System, &format!("system[{i}]"))?;
    }

    for (m_idx, msg) in request.messages.iter().enumerate() {
        for (c_idx, blk) in msg.content.iter().enumerate() {
            validate_block_native(
                blk,
                HostType::Content,
                &format!("messages[{m_idx}].content[{c_idx}]"),
            )?;
        }
    }

    if let Some(ref tools) = request.tools {
        for (t_idx, tool) in tools.iter().enumerate() {
            validate_annotations(&tool.native, HostType::Tool, &format!("tools[{t_idx}]"))?;
            let mut has_cache_control = false;
            let mut has_defer_loading = false;
            for n in &tool.native {
                if n.source == NATIVE_SOURCE_ANTHROPIC {
                    if n.kind == NATIVE_KIND_CACHE_CONTROL {
                        has_cache_control = true;
                    } else if n.kind == NATIVE_KIND_DEFER_LOADING {
                        has_defer_loading = true;
                    }
                }
            }
            if has_cache_control && has_defer_loading {
                return Err(Error::Custom(format!(
                    "ir: tools[{t_idx}]: cannot specify both defer_loading and cache_control"
                )));
            }
        }
    }

    Ok(())
}

fn validate_annotations(
    annotations: &[NativeAnnotation],
    host: HostType,
    path: &str,
) -> Result<(), Error> {
    let mut seen = HashSet::new();
    for (i, ann) in annotations.iter().enumerate() {
        if ann.source.is_empty() {
            return Err(Error::Custom(format!(
                "{path}.native[{i}]: source cannot be empty"
            )));
        }
        if ann.kind.is_empty() {
            return Err(Error::Custom(format!(
                "{path}.native[{i}]: kind cannot be empty"
            )));
        }
        let parsed_json: serde_json::Value = serde_json::from_str(&ann.value).map_err(|_| {
            Error::Custom(format!("{path}.native[{i}]: value is not valid JSON text"))
        })?;
        if let Some(ref om) = ann.omission
            && om.field.is_empty()
        {
            return Err(Error::Custom(format!(
                "{path}.native[{i}]: omission field cannot be empty"
            )));
        }
        let key = format!("{}:{}", ann.source, ann.kind);
        if !seen.insert(key) {
            return Err(Error::Custom(format!(
                "{path}: duplicate native annotation for {}:{}",
                ann.source, ann.kind
            )));
        }

        if ann.source == NATIVE_SOURCE_ANTHROPIC {
            match ann.kind.as_str() {
                NATIVE_KIND_THINKING_MODE => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let s = parsed_json.as_str().unwrap_or_default();
                    if s != "adaptive" && s != "enabled" && s != "disabled" && s != "between_tools"
                    {
                        return Err(Error::Custom(format!("invalid thinking_mode {s:?}")));
                    }
                }
                NATIVE_KIND_TOP_K => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let n = parsed_json.as_i64();
                    if n.is_none() || n.unwrap() < 0 || n.unwrap() > 9007199254740991 {
                        return Err(Error::Custom(format!("invalid top_k {:?}", parsed_json)));
                    }
                }
                NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    if parsed_json.as_bool().is_none() {
                        return Err(Error::Custom(format!(
                            "invalid disable_parallel_tool_use {:?}",
                            parsed_json
                        )));
                    }
                }
                NATIVE_KIND_THINKING_BUDGET => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let n = parsed_json.as_i64().unwrap_or(0);
                    if n <= 0 {
                        return Err(Error::Custom(
                            "anthropic thinking_budget must be a positive integer".into(),
                        ));
                    }
                }
                NATIVE_KIND_THINKING_DISPLAY => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let s = parsed_json.as_str().unwrap_or_default();
                    if s != "summarized" && s != "omitted" && s != "updates" {
                        return Err(Error::Custom(format!("invalid thinking_display {s:?}")));
                    }
                }
                NATIVE_KIND_OUTPUT_EFFORT => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let s = parsed_json.as_str().unwrap_or_default();
                    if s != "low" && s != "medium" && s != "high" && s != "xhigh" && s != "max" {
                        return Err(Error::Custom(format!("invalid output_effort {s:?}")));
                    }
                }
                NATIVE_KIND_CONTEXT_MANAGEMENT => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    let Some(obj) = parsed_json.as_object() else {
                        return Err(Error::Custom(
                            "context_management requires edits array".into(),
                        ));
                    };
                    if !obj.get("edits").is_some_and(|e| e.is_array()) {
                        return Err(Error::Custom(
                            "context_management requires edits array".into(),
                        ));
                    }
                }
                NATIVE_KIND_CACHE_CONTROL => {
                    let Some(obj) = parsed_json.as_object() else {
                        return Err(Error::Custom("invalid cache_control object".into()));
                    };
                    if obj.get("type").and_then(|t| t.as_str()) != Some("ephemeral") {
                        return Err(Error::Custom("invalid cache_control object".into()));
                    }
                    if let Some(ttl) = obj.get("ttl") {
                        let ttl_str = ttl.as_str().unwrap_or_default();
                        if ttl_str != "5m" && ttl_str != "1h" {
                            return Err(Error::Custom(format!(
                                "invalid cache_control ttl {ttl_str:?}"
                            )));
                        }
                    }
                }
                NATIVE_KIND_DOCUMENT_TITLE | NATIVE_KIND_DOCUMENT_CONTEXT => {
                    if host != HostType::Content {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on content blocks",
                            ann.kind
                        )));
                    }
                    if parsed_json.as_str().is_none() {
                        return Err(Error::Custom(format!(
                            "anthropic {} value must be a JSON string",
                            ann.kind
                        )));
                    }
                }
                NATIVE_KIND_DOCUMENT_CITATIONS => {
                    if host != HostType::Content {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on content blocks",
                            ann.kind
                        )));
                    }
                    let Some(obj) = parsed_json.as_object() else {
                        return Err(Error::Custom(format!(
                            "anthropic {} value must be a JSON object",
                            ann.kind
                        )));
                    };
                    if let Some(enabled) = obj.get("enabled")
                        && !enabled.is_boolean()
                    {
                        return Err(Error::Custom(format!(
                            "anthropic {} enabled must be a boolean",
                            ann.kind
                        )));
                    }
                }
                NATIVE_KIND_DEFER_LOADING => {
                    if host != HostType::Tool {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Tool",
                            ann.kind
                        )));
                    }
                    if parsed_json.as_bool() != Some(true) {
                        return Err(Error::Custom(format!(
                            "invalid defer_loading {parsed_json:?}"
                        )));
                    }
                }
                NATIVE_KIND_EAGER_INPUT_STREAMING => {
                    if host != HostType::Tool {
                        return Err(Error::Custom(format!(
                            "{path}: anthropic {} is only allowed on Tool",
                            ann.kind
                        )));
                    }
                    if !parsed_json.is_boolean() {
                        return Err(Error::Custom(format!(
                            "invalid eager_input_streaming {parsed_json:?}"
                        )));
                    }
                }
                _ => {}
            }
        } else if ann.source == NATIVE_SOURCE_OPENAI {
            match ann.kind.as_str() {
                NATIVE_KIND_MAX_COMPLETION_TOKENS => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: openai {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    if parsed_json.as_bool() != Some(true) {
                        return Err(Error::Custom(format!(
                            "invalid max_completion_tokens {parsed_json:?}"
                        )));
                    }
                }
                NATIVE_KIND_STORE => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: openai {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    if !parsed_json.is_boolean() && !parsed_json.is_null() {
                        return Err(Error::Custom(format!("invalid store {parsed_json:?}")));
                    }
                }
                NATIVE_KIND_STREAM => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: openai {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    if !parsed_json.is_boolean() && !parsed_json.is_null() {
                        return Err(Error::Custom(format!("invalid stream {parsed_json:?}")));
                    }
                }
                NATIVE_KIND_STREAM_OPTIONS => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: openai {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    if !parsed_json.is_null() {
                        if let Some(map) = parsed_json.as_object() {
                            for (k, v) in map {
                                if (k == "include_usage" || k == "include_obfuscation")
                                    && !v.is_boolean()
                                {
                                    return Err(Error::Custom(format!(
                                        "invalid stream_options {parsed_json:?}: {k} must be a boolean"
                                    )));
                                }
                            }
                        } else {
                            return Err(Error::Custom(format!(
                                "invalid stream_options {parsed_json:?}"
                            )));
                        }
                    }
                }
                NATIVE_KIND_CONTAINER => {
                    if host != HostType::Tool {
                        return Err(Error::Custom(format!(
                            "{path}: openai container is only allowed on Tool"
                        )));
                    }
                    if !parsed_json.is_object() && !parsed_json.is_string() {
                        return Err(Error::Custom(format!(
                            "{path}: openai container must be an object with path array or string"
                        )));
                    }
                }
                NATIVE_KIND_DEFER_LOADING => {
                    if host != HostType::Tool {
                        return Err(Error::Custom(format!(
                            "{path}: openai defer_loading is only allowed on Tool"
                        )));
                    }
                    if parsed_json.as_bool() != Some(true) {
                        return Err(Error::Custom(format!(
                            "invalid defer_loading {parsed_json:?}"
                        )));
                    }
                }
                NATIVE_KIND_PROMPT_CACHE_KEY => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: openai {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    if !parsed_json.is_string() {
                        return Err(Error::Custom(format!(
                            "invalid prompt_cache_key {parsed_json:?}"
                        )));
                    }
                }
                NATIVE_KIND_PROMPT_CACHE_OPTIONS => {
                    if host != HostType::Request {
                        return Err(Error::Custom(format!(
                            "{path}: openai {} is only allowed on Request",
                            ann.kind
                        )));
                    }
                    if !parsed_json.is_object() && !parsed_json.is_null() {
                        return Err(Error::Custom(format!(
                            "invalid prompt_cache_options {parsed_json:?}"
                        )));
                    }
                }
                NATIVE_KIND_PROMPT_CACHE_BREAKPOINT => {
                    if host != HostType::Content && host != HostType::System {
                        return Err(Error::Custom(format!(
                            "{path}: openai {} is only allowed on content or system blocks",
                            ann.kind
                        )));
                    }
                    if !parsed_json.is_object() {
                        return Err(Error::Custom(format!(
                            "invalid prompt_cache_breakpoint {parsed_json:?}"
                        )));
                    }
                }
                _ => {}
            }
        }
    }
    Ok(())
}

fn validate_block_native(blk: &Block, host: HostType, path: &str) -> Result<(), Error> {
    let mut breakpoint_native: Option<&[NativeAnnotation]> = None;
    match blk {
        Block::WebSearchCall {
            id,
            status,
            action,
            extra,
        } => {
            crate::validate_web_search_extra(extra)?;
            crate::inspect_web_search_call(id, status, action, path)?;
        }
        Block::Text { native, .. }
        | Block::Image { native, .. }
        | Block::Document { native, .. }
        | Block::Refusal { native, .. } => {
            validate_annotations(native, host, path)?;
        }
        Block::Thinking { native, .. }
        | Block::RedactedThinking { native, .. }
        | Block::ToolUse { native, .. }
        | Block::CustomToolUse { native, .. }
        | Block::ApplyPatchToolUse { native, .. }
        | Block::ApplyPatchToolResult { native, .. } => {
            validate_annotations(native, host, path)?;
            breakpoint_native = Some(native);
        }
        Block::ToolResult {
            content, native, ..
        } => {
            validate_annotations(native, host, path)?;
            breakpoint_native = Some(native);
            for (k, child) in content.iter().enumerate() {
                validate_block_native(child, host, &format!("{path}.content[{k}]"))?;
            }
        }
        Block::CustomToolResult { output, native, .. } => {
            validate_annotations(native, host, path)?;
            breakpoint_native = Some(native);
            if let crate::request::CustomToolOutput::Content(children) = output {
                for (k, child) in children.iter().enumerate() {
                    validate_block_native(child, host, &format!("{path}.output.content[{k}]"))?;
                }
            }
        }
    }
    if let Some(native) = breakpoint_native {
        for (i, ann) in native.iter().enumerate() {
            if ann.source == NATIVE_SOURCE_OPENAI && ann.kind == NATIVE_KIND_PROMPT_CACHE_BREAKPOINT
            {
                return Err(Error::Custom(format!(
                    "ir: {path}.native[{i}]: openai prompt_cache_breakpoint is only allowed on text, image, or document blocks"
                )));
            }
        }
    }
    Ok(())
}

pub fn omitted_native_request_losses(request: &Request, target: &str) -> Result<Vec<Loss>, Error> {
    validate_native_request(request)?;

    let mut losses = Vec::new();
    let current_effort = request.params.as_ref().and_then(|p| p.reasoning_effort);

    let has_active_budget_proj = request.native.iter().any(|n| {
        n.source == NATIVE_SOURCE_ANTHROPIC
            && n.kind == NATIVE_KIND_THINKING_BUDGET
            && n.projection
                .as_ref()
                .map(|p| p.origin == "budget" && Some(p.effort) == current_effort)
                .unwrap_or(false)
    });

    for (i, ann) in request.native.iter().enumerate() {
        append_omission_loss(
            &mut losses,
            ann,
            &format!("native[{i}]"),
            has_active_budget_proj,
            current_effort,
            target,
        );
    }

    for (s_idx, sb) in request.system.iter().enumerate() {
        let SystemBlock::Text { native, .. } = sb;
        for (i, ann) in native.iter().enumerate() {
            append_omission_loss(
                &mut losses,
                ann,
                &format!("system[{s_idx}].native[{i}]"),
                false,
                None,
                target,
            );
        }
    }

    for (m_idx, msg) in request.messages.iter().enumerate() {
        for (c_idx, blk) in msg.content.iter().enumerate() {
            if matches!(blk, Block::Document { .. }) && msg.role != Role::User {
                continue;
            }
            collect_block_omissions(
                &mut losses,
                blk,
                &format!("messages[{m_idx}].content[{c_idx}]"),
                target,
            );
        }
    }

    if let Some(ref tools) = request.tools {
        for (t_idx, tool) in tools.iter().enumerate() {
            for (i, ann) in tool.native.iter().enumerate() {
                append_omission_loss(
                    &mut losses,
                    ann,
                    &format!("tools[{t_idx}].native[{i}]"),
                    false,
                    None,
                    target,
                );
            }
        }
    }

    Ok(losses)
}

fn collect_block_omissions(losses: &mut Vec<Loss>, blk: &Block, path: &str, target: &str) {
    let (native, children) = match blk {
        Block::Text { native, .. }
        | Block::Thinking { native, .. }
        | Block::Image { native, .. }
        | Block::ToolUse { native, .. }
        | Block::CustomToolUse { native, .. }
        | Block::ApplyPatchToolUse { native, .. }
        | Block::ApplyPatchToolResult { native, .. }
        | Block::Refusal { native, .. } => (native, None),
        Block::ToolResult {
            native, content, ..
        } => (native, Some(content.as_slice())),
        Block::Document {
            url, media_type, ..
        } if target == "chatcompletions"
            && (url.is_some()
                || media_type
                    .as_deref()
                    .is_some_and(|m| m != "application/pdf")) =>
        {
            static EMPTY: Vec<NativeAnnotation> = Vec::new();
            (&EMPTY, None)
        }
        Block::Document { native, .. } => (native, None),
        Block::RedactedThinking { .. } | Block::WebSearchCall { .. } => {
            // Whole-block drop; annotations are part of it and are not reported a second time.
            static EMPTY: Vec<NativeAnnotation> = Vec::new();
            (&EMPTY, None)
        }
        Block::CustomToolResult { native, output, .. } => {
            let ch = match output {
                crate::request::CustomToolOutput::Content(c) => Some(c.as_slice()),
                _ => None,
            };
            (native, ch)
        }
    };

    for (i, ann) in native.iter().enumerate() {
        append_omission_loss(
            losses,
            ann,
            &format!("{path}.native[{i}]"),
            false,
            None,
            target,
        );
    }

    if let Some(ch) = children {
        for (k, child) in ch.iter().enumerate() {
            if matches!(child, Block::Document { .. }) {
                continue;
            }
            collect_block_omissions(losses, child, &format!("{path}.content[{k}]"), target);
        }
    }
}

fn append_omission_loss(
    losses: &mut Vec<Loss>,
    ann: &NativeAnnotation,
    path: &str,
    has_active_budget_proj: bool,
    current_effort: Option<crate::request::ReasoningEffort>,
    target: &str,
) {
    if ann.source == NATIVE_SOURCE_OPENAI {
        match ann.kind.as_str() {
            NATIVE_KIND_MAX_COMPLETION_TOKENS | NATIVE_KIND_STREAM => return,
            NATIVE_KIND_STREAM_OPTIONS => {
                if target == "chatcompletions" {
                    return;
                }
                #[derive(serde::Deserialize)]
                struct SoCheck {
                    include_usage: Option<bool>,
                }
                let has_usage_true = serde_json::from_str::<SoCheck>(&ann.value)
                    .map(|so| so.include_usage == Some(true))
                    .unwrap_or(false);
                if has_usage_true {
                    let (field, reason, detail) = if let Some(ref om) = ann.omission {
                        let f = if om.field.is_empty() {
                            "stream_options".to_string()
                        } else {
                            om.field.clone()
                        };
                        let r = om.reason;
                        let d = match om.detail.as_deref() {
                            Some(s) if !s.is_empty() => s.to_string(),
                            _ => STREAM_OPTIONS_LOSS_DETAIL.to_string(),
                        };
                        (f, r, d)
                    } else {
                        (
                            "stream_options".to_string(),
                            LossReason::Degraded,
                            STREAM_OPTIONS_LOSS_DETAIL.to_string(),
                        )
                    };
                    losses.push(Loss {
                        path: path.into(),
                        field,
                        reason,
                        detail,
                    });
                }
                return;
            }
            NATIVE_KIND_STORE => {
                if target == "chatcompletions" || target == "responses" {
                    return;
                }
                let is_true = serde_json::from_str::<bool>(&ann.value).unwrap_or(false);
                if is_true {
                    let (field, reason, detail) = if let Some(ref om) = ann.omission {
                        let f = if om.field.is_empty() {
                            "store".to_string()
                        } else {
                            om.field.clone()
                        };
                        let r = om.reason;
                        let d = match om.detail.as_deref() {
                            Some(s) if !s.is_empty() => s.to_string(),
                            _ => "target does not support output retention".to_string(),
                        };
                        (f, r, d)
                    } else {
                        (
                            "store".to_string(),
                            LossReason::UnsupportedSemantic,
                            "target does not support output retention".to_string(),
                        )
                    };
                    losses.push(Loss {
                        path: path.into(),
                        field,
                        reason,
                        detail,
                    });
                }
                return;
            }
            NATIVE_KIND_PROMPT_CACHE_KEY => {
                if target == "chatcompletions" || target == "responses" {
                    return;
                }
                let (field, reason, detail) = if let Some(ref om) = ann.omission {
                    let f = if om.field.is_empty() {
                        "prompt_cache_key".to_string()
                    } else {
                        om.field.clone()
                    };
                    let r = om.reason;
                    let d = match om.detail.as_deref() {
                        Some(s) if !s.is_empty() => s.to_string(),
                        _ => PROMPT_CACHE_KEY_LOSS_DETAIL.to_string(),
                    };
                    (f, r, d)
                } else {
                    (
                        "prompt_cache_key".to_string(),
                        LossReason::UnmappedField,
                        PROMPT_CACHE_KEY_LOSS_DETAIL.to_string(),
                    )
                };
                losses.push(Loss {
                    path: path.into(),
                    field,
                    reason,
                    detail,
                });
                return;
            }
            NATIVE_KIND_PROMPT_CACHE_OPTIONS => {
                if target == "chatcompletions" || target == "responses" {
                    return;
                }
                let (field, reason, detail) = if let Some(ref om) = ann.omission {
                    let f = if om.field.is_empty() {
                        "prompt_cache_options".to_string()
                    } else {
                        om.field.clone()
                    };
                    let r = om.reason;
                    let d = match om.detail.as_deref() {
                        Some(s) if !s.is_empty() => s.to_string(),
                        _ => PROMPT_CACHE_OPTIONS_LOSS_DETAIL.to_string(),
                    };
                    (f, r, d)
                } else {
                    (
                        "prompt_cache_options".to_string(),
                        LossReason::UnmappedField,
                        PROMPT_CACHE_OPTIONS_LOSS_DETAIL.to_string(),
                    )
                };
                losses.push(Loss {
                    path: path.into(),
                    field,
                    reason,
                    detail,
                });
                return;
            }
            NATIVE_KIND_PROMPT_CACHE_BREAKPOINT => {
                if target == "chatcompletions" || target == "responses" {
                    return;
                }
                let (field, reason, detail) = if let Some(ref om) = ann.omission {
                    let f = if om.field.is_empty() {
                        "prompt_cache_breakpoint".to_string()
                    } else {
                        om.field.clone()
                    };
                    let r = om.reason;
                    let d = match om.detail.as_deref() {
                        Some(s) if !s.is_empty() => s.to_string(),
                        _ => PROMPT_CACHE_BREAKPOINT_LOSS_DETAIL.to_string(),
                    };
                    (f, r, d)
                } else {
                    (
                        "prompt_cache_breakpoint".to_string(),
                        LossReason::UnmappedField,
                        PROMPT_CACHE_BREAKPOINT_LOSS_DETAIL.to_string(),
                    )
                };
                losses.push(Loss {
                    path: path.into(),
                    field,
                    reason,
                    detail,
                });
                return;
            }
            _ => {
                if target == "responses" {
                    return;
                }
                if target == "anthropic" && ann.kind == NATIVE_KIND_DEFER_LOADING {
                    return;
                }
                if let Some(ref om) = ann.omission {
                    losses.push(Loss {
                        path: path.into(),
                        field: om.field.clone(),
                        reason: om.reason,
                        detail: om.detail.clone().unwrap_or_default(),
                    });
                }
                return;
            }
        }
    }
    if ann.source != NATIVE_SOURCE_ANTHROPIC {
        losses.push(Loss {
            path: path.into(),
            field: "source".into(),
            reason: LossReason::UnsupportedSemantic,
            detail: format!("unsupported native option source \"{}\"", ann.source),
        });
        return;
    }

    match ann.kind.as_str() {
        NATIVE_KIND_OUTPUT_EFFORT => {}
        NATIVE_KIND_THINKING_MODE => {
            if has_active_budget_proj {
                return;
            }
            if let Some(ref om) = ann.omission {
                losses.push(Loss {
                    path: path.into(),
                    field: om.field.clone(),
                    reason: om.reason,
                    detail: om.detail.clone().unwrap_or_default(),
                });
            } else {
                losses.push(Loss {
                    path: path.into(),
                    field: "type".into(),
                    reason: LossReason::UnmappedField,
                    detail: String::new(),
                });
            }
        }
        NATIVE_KIND_THINKING_BUDGET => {
            if let Some(ref proj) = ann.projection
                && proj.origin == "budget"
            {
                if Some(proj.effort) == current_effort
                    && let Some(ref om) = ann.omission
                {
                    losses.push(Loss {
                        path: path.into(),
                        field: om.field.clone(),
                        reason: om.reason,
                        detail: om.detail.clone().unwrap_or_default(),
                    });
                }
                return;
            }
            if let Some(ref om) = ann.omission {
                losses.push(Loss {
                    path: path.into(),
                    field: om.field.clone(),
                    reason: om.reason,
                    detail: om.detail.clone().unwrap_or_default(),
                });
            }
        }
        NATIVE_KIND_CACHE_CONTROL => {
            let (field, reason, detail) = if let Some(ref om) = ann.omission {
                let f = if om.field.is_empty() {
                    "cache_control".to_string()
                } else {
                    om.field.clone()
                };
                let r = om.reason;
                let d = match om.detail.as_deref() {
                    Some(s) if !s.is_empty() => s.to_string(),
                    _ => CACHE_CONTROL_LOSS_DETAIL.to_string(),
                };
                (f, r, d)
            } else {
                (
                    "cache_control".to_string(),
                    LossReason::UnmappedField,
                    CACHE_CONTROL_LOSS_DETAIL.to_string(),
                )
            };
            losses.push(Loss {
                path: path.into(),
                field,
                reason,
                detail,
            });
        }
        NATIVE_KIND_DEFER_LOADING => {
            if target == "responses" || target == "anthropic" {
                return;
            }
            let (field, reason, detail) = if let Some(ref om) = ann.omission {
                let f = if om.field.is_empty() {
                    "defer_loading".to_string()
                } else {
                    om.field.clone()
                };
                let r = om.reason;
                let d = match om.detail.as_deref() {
                    Some(s) if !s.is_empty() => s.to_string(),
                    _ => DEFER_LOADING_LOSS_DETAIL.to_string(),
                };
                (f, r, d)
            } else {
                (
                    "defer_loading".to_string(),
                    LossReason::Degraded,
                    DEFER_LOADING_LOSS_DETAIL.to_string(),
                )
            };
            losses.push(Loss {
                path: path.into(),
                field,
                reason,
                detail,
            });
        }
        NATIVE_KIND_EAGER_INPUT_STREAMING => {
            if target == "anthropic" {
                return;
            }
            let is_true = serde_json::from_str::<bool>(&ann.value).unwrap_or(false);
            if is_true {
                let (field, reason, detail) = if let Some(ref om) = ann.omission {
                    let f = if om.field.is_empty() {
                        "eager_input_streaming".to_string()
                    } else {
                        om.field.clone()
                    };
                    let r = om.reason;
                    let d = match om.detail.as_deref() {
                        Some(s) if !s.is_empty() => s.to_string(),
                        _ => EAGER_INPUT_STREAMING_LOSS_DETAIL.to_string(),
                    };
                    (f, r, d)
                } else {
                    (
                        "eager_input_streaming".to_string(),
                        LossReason::Degraded,
                        EAGER_INPUT_STREAMING_LOSS_DETAIL.to_string(),
                    )
                };
                losses.push(Loss {
                    path: path.into(),
                    field,
                    reason,
                    detail,
                });
            }
        }
        NATIVE_KIND_TOP_K => {
            let (field, reason, detail) = if let Some(ref om) = ann.omission {
                let f = if om.field.is_empty() {
                    "top_k".to_string()
                } else {
                    om.field.clone()
                };
                (f, om.reason, om.detail.clone().unwrap_or_default())
            } else {
                (
                    "top_k".to_string(),
                    LossReason::UnmappedField,
                    String::new(),
                )
            };
            losses.push(Loss {
                path: path.into(),
                field,
                reason,
                detail,
            });
        }
        NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE => {
            let (field, reason, detail) = if let Some(ref om) = ann.omission {
                let f = if om.field.is_empty() {
                    "disable_parallel_tool_use".to_string()
                } else {
                    om.field.clone()
                };
                (f, om.reason, om.detail.clone().unwrap_or_default())
            } else {
                (
                    "disable_parallel_tool_use".to_string(),
                    LossReason::UnmappedField,
                    String::new(),
                )
            };
            losses.push(Loss {
                path: path.into(),
                field,
                reason,
                detail,
            });
        }
        NATIVE_KIND_THINKING_DISPLAY
        | NATIVE_KIND_CONTEXT_MANAGEMENT
        | NATIVE_KIND_DOCUMENT_TITLE
        | NATIVE_KIND_DOCUMENT_CONTEXT
        | NATIVE_KIND_DOCUMENT_CITATIONS => {
            if let Some(ref om) = ann.omission {
                losses.push(Loss {
                    path: path.into(),
                    field: om.field.clone(),
                    reason: om.reason,
                    detail: om.detail.clone().unwrap_or_default(),
                });
            }
        }
        _ => {
            losses.push(Loss {
                path: path.into(),
                field: "kind".into(),
                reason: LossReason::UnsupportedSemantic,
                detail: format!("unsupported native option kind \"{}\"", ann.kind),
            });
        }
    }
}
