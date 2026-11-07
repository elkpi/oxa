//! Face → IR conversions (spec/17 §3/§4). The raw JSON entrypoints audit
//! unknown properties in source encounter order; errors are reserved for
//! structural violations (missing model/state, empty questions, wrong
//! criteria shape, non-object answers).

use serde_json::Value;
use std::collections::BTreeMap;

use oxa_ir::{
    Block, Loss, LossReason, Message, Params, Request as IrRequest, Response as IrResponse, Role,
    StopReason, SystemBlock, Usage,
};

use crate::config::Config;
use crate::error::Error;
use crate::json_util::compact_json_text;
use crate::question::JevQuestion;

pub(crate) const INSTRUCTION_TEMPLATE: &str = "Answer the structured questions that follow as a single JSON object. Keys MUST be exactly the question IDs given in the user message; values MUST match each question's type: noul answers are numbers in [0,1], choice answers are one of the listed option names, score answers are one of the listed level numbers. Output raw JSON only: no prose, no markdown fences.";

pub(crate) const RESPONSE_FORMAT_NAME: &str = "jev_answers";

pub(crate) const SYNTHESIZED_ID: &str = "jev";

pub(crate) const QUESTION_TYPE_NOUL: &str = "noul";
pub(crate) const QUESTION_TYPE_CHOICE: &str = "choice";
pub(crate) const QUESTION_TYPE_SCORE: &str = "score";
pub(crate) const QUESTION_TYPE_BOX: &str = "box";

pub(crate) fn fail(message: impl Into<String>) -> Error {
    Error::new(format!("jev: {}", message.into()))
}

pub(crate) fn supported_question_type(kind: &str) -> bool {
    kind == QUESTION_TYPE_NOUL || kind == QUESTION_TYPE_CHOICE || kind == QUESTION_TYPE_SCORE
}

pub(crate) fn whole_unit_skip_detail(kind: &str) -> &'static str {
    if kind == QUESTION_TYPE_BOX {
        "N-JEV-5: experimental box questions are out of scope"
    } else {
        "N-JEV-6: skipped from schema and prompt"
    }
}

pub(crate) fn criteria_line(name: &str, description: &Value) -> String {
    match description {
        Value::Null => format!("- {name}"),
        Value::String(text) => format!("- {name}: {text}"),
        other => {
            let raw = serde_json::to_string(other).unwrap_or_default();
            format!("- {name}: {raw}")
        }
    }
}

/// Builds the synthesized `json_schema` response format (spec/17 §7.3) from
/// pre-validated supported questions.
pub(crate) fn synthesized_response_format(
    questions: &BTreeMap<String, JevQuestion>,
) -> Result<Params, Error> {
    use serde_json::map::Map;
    let mut properties = Map::new();
    let mut required: Vec<Value> = Vec::new();
    for (id, question) in questions {
        let mut property = Map::new();
        match question.kind.as_str() {
            QUESTION_TYPE_NOUL => {
                property.insert("type".to_owned(), Value::String("number".to_owned()));
                property.insert("minimum".to_owned(), serde_json::json!(0));
                property.insert("maximum".to_owned(), serde_json::json!(1));
            }
            QUESTION_TYPE_CHOICE => {
                let crit = question
                    .criteria
                    .as_ref()
                    .and_then(|c| c.as_object())
                    .ok_or_else(|| fail("question choice criteria is required"))?;
                if crit.is_empty() {
                    return Err(fail("question choice criteria must not be empty"));
                }
                let enum_values: Vec<Value> = crit
                    .keys()
                    .map(|name| Value::String(name.clone()))
                    .collect();
                property.insert("type".to_owned(), Value::String("string".to_owned()));
                property.insert("enum".to_owned(), Value::Array(enum_values));
            }
            QUESTION_TYPE_SCORE => {
                let levels = question
                    .criteria
                    .as_ref()
                    .and_then(|c| c.as_array())
                    .ok_or_else(|| fail("question score criteria is required"))?;
                if levels.is_empty() {
                    return Err(fail("question score criteria must not be empty"));
                }
                let enum_values: Vec<Value> = (1..=levels.len())
                    .map(|level| serde_json::json!(level))
                    .collect();
                property.insert("type".to_owned(), Value::String("number".to_owned()));
                property.insert("enum".to_owned(), Value::Array(enum_values));
            }
            other => return Err(fail(format!("unsupported question type {other}"))),
        }
        if let Some(instructions) = &question.instructions {
            if !instructions.is_empty() {
                property.insert(
                    "description".to_owned(),
                    Value::String(instructions.clone()),
                );
            }
        }
        properties.insert(id.clone(), Value::Object(property));
        required.push(Value::String(id.clone()));
    }
    if properties.is_empty() {
        return Err(fail("request questions produced no supported questions"));
    }
    let mut schema = Map::new();
    schema.insert("type".to_owned(), Value::String("object".to_owned()));
    schema.insert("properties".to_owned(), Value::Object(properties));
    schema.insert("required".to_owned(), Value::Array(required));
    schema.insert("additionalProperties".to_owned(), Value::Bool(false));
    Ok(Params {
        response_format: Some(oxa_ir::ResponseFormat::JsonSchema {
            name: RESPONSE_FORMAT_NAME.to_owned(),
            description: None,
            schema,
            schema_raw: None,
            strict: None,
        }),
        ..Params::default()
    })
}

/// Renders the §7.2 user text for a set of supported questions in ascending
/// lexicographic ID order.
pub(crate) fn render_user_text(questions: &BTreeMap<String, JevQuestion>) -> Result<String, Error> {
    let mut lines: Vec<String> = Vec::new();
    for (id, question) in questions {
        lines.push(format!(
            "question {id} ({}): {}",
            question.kind,
            question.instructions.as_deref().unwrap_or("")
        ));
        let criteria_lines = match (question.kind.as_str(), question.criteria.as_ref()) {
            (QUESTION_TYPE_NOUL, Some(criteria)) if !criteria.is_null() => {
                let crit = criteria
                    .as_object()
                    .ok_or_else(|| fail("question noul criteria must be an object"))?;
                let mut lines = vec!["criteria:".to_owned()];
                for name in ["false", "true"] {
                    if let Some(leaf) = crit.get(name) {
                        lines.push(criteria_line(name, leaf));
                    }
                }
                lines
            }
            (QUESTION_TYPE_CHOICE, Some(criteria)) => {
                let crit = criteria
                    .as_object()
                    .ok_or_else(|| fail("question choice criteria is required"))?;
                if crit.is_empty() {
                    return Err(fail("question choice criteria must not be empty"));
                }
                let mut lines = vec!["criteria:".to_owned()];
                for (name, leaf) in crit {
                    lines.push(criteria_line(name, leaf));
                }
                lines
            }
            (QUESTION_TYPE_SCORE, Some(criteria)) => {
                let levels = criteria
                    .as_array()
                    .ok_or_else(|| fail("question score criteria is required"))?;
                if levels.is_empty() {
                    return Err(fail("question score criteria must not be empty"));
                }
                let mut lines = vec!["criteria:".to_owned()];
                for (index, leaf) in levels.iter().enumerate() {
                    lines.push(criteria_line(&format!("level {}", index + 1), leaf));
                }
                lines
            }
            _ => Vec::new(),
        };
        lines.extend(criteria_lines);
    }
    Ok(lines.join("\n"))
}

/// Collects the supported questions, reporting whole-unit skips for
/// experimental box or unsupported types.
pub(crate) fn supported_questions(
    questions: &BTreeMap<String, Value>,
    losses: &mut Vec<Loss>,
) -> Result<BTreeMap<String, JevQuestion>, Error> {
    let mut out = BTreeMap::new();
    for (id, raw_question) in questions {
        let question = raw_question
            .as_object()
            .ok_or_else(|| fail(format!("question {id} must be an object")))?;
        let kind = question
            .get("type")
            .and_then(|v| v.as_str())
            .ok_or_else(|| fail(format!("question {id} type must be a string")))?;
        if kind == QUESTION_TYPE_BOX || !supported_question_type(kind) {
            losses.push(Loss {
                path: format!("questions.{id}"),
                field: "type".to_owned(),
                reason: LossReason::UnsupportedSemantic,
                detail: whole_unit_skip_detail(kind).to_owned(),
            });
            continue;
        }
        let mut instructions: Option<String> = None;
        let mut criteria: Option<Value> = None;
        for (field, leaf) in question {
            match field.as_str() {
                "type" => {}
                "instructions" => {
                    instructions = Some(
                        leaf.as_str()
                            .ok_or_else(|| {
                                fail(format!("question {id} instructions must be a string"))
                            })?
                            .to_owned(),
                    );
                }
                "criteria" => criteria = Some(leaf.clone()),
                other => losses.push(Loss {
                    path: format!("questions.{id}"),
                    field: other.to_owned(),
                    reason: LossReason::UnmappedField,
                    detail: "unknown property on the Jev question".to_owned(),
                }),
            }
        }
        out.insert(
            id.clone(),
            JevQuestion {
                kind: kind.to_owned(),
                instructions,
                criteria,
            },
        );
    }
    Ok(out)
}

/// The typed Jev wire request.
#[derive(Default, Debug, serde::Serialize)]
pub struct WireRequest {
    pub model: String,
    pub state: Value,
    pub questions: BTreeMap<String, Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub assets: Option<Value>,
}

fn state_is_absent(state: &Value) -> bool {
    matches!(state, Value::Null)
}

/// Converts a Jev wire request to the IR (spec/17 §3). The typed entrypoint
/// reports losses in deterministic (sorted) order; the raw JSON entrypoint
/// audits in source encounter order.
pub fn decode_request(
    wire: &WireRequest,
    config: &Config,
) -> Result<(IrRequest, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    if wire.model.is_empty() {
        return Err(fail("request model is required"));
    }
    if state_is_absent(&wire.state) {
        return Err(fail("request state is required"));
    }
    if wire.questions.is_empty() {
        return Err(fail("request questions must not be empty"));
    }
    if !wire.state.is_string() && !wire.state.is_object() && !wire.state.is_array() {
        return Err(state_scalar_error());
    }
    let state_text = match &wire.state {
        Value::String(text) => text.clone(),
        other => {
            losses.push(Loss {
                path: "state".to_owned(),
                field: "state".to_owned(),
                reason: LossReason::Degraded,
                detail: "N-JEV-1: state serialized to canonical JSON text".to_owned(),
            });
            serde_json::to_string(other).map_err(|err| fail(err.to_string()))?
        }
    };
    if wire.assets.is_some() {
        losses.push(Loss {
            path: "assets".to_owned(),
            field: "assets".to_owned(),
            reason: LossReason::UnsupportedSemantic,
            detail: "N-JEV-4: experimental image assets are out of scope".to_owned(),
        });
    }

    let questions = supported_questions(&wire.questions, &mut losses)?;
    let user_text = render_user_text(&questions)?;
    let params = synthesized_response_format(&questions)?;

    Ok((
        IrRequest {
            model: config.map_model(&wire.model),
            system: vec![SystemBlock::Text {
                text: format!("{state_text}\n\n{INSTRUCTION_TEMPLATE}"),
                role: String::new(),
                native: Vec::new(),
            }],
            messages: vec![Message {
                role: Role::User,
                content: vec![Block::Text {
                    text: user_text,
                    citations: None,
                    native: Vec::new(),
                }],
            }],
            params: Some(params),
            tools: None,
            tool_choice: None,
            metadata: None,
            native: Vec::new(),
            tool_change_groups: Vec::new(),
        },
        losses,
    ))
}

fn state_scalar_error() -> Error {
    fail("state must be a string, object, or array")
}

/// The typed Jev wire response.
#[derive(Default, Debug, serde::Serialize)]
pub struct WireResponse {
    pub model: String,
    pub answers: BTreeMap<String, Value>,
    pub usage: WireUsage,
}

/// The Jev usage totals.
#[derive(Default, Debug, serde::Serialize)]
pub struct WireUsage {
    pub input_tokens: i64,
    pub output_tokens: i64,
}

pub(crate) fn usage_loss_list(usage: &Usage) -> Vec<Loss> {
    let mut losses = Vec::new();
    let mut add = |field: &str| {
        losses.push(Loss {
            path: "usage".to_owned(),
            field: field.to_owned(),
            reason: LossReason::UnmappedField,
            detail: "N-JEV-17: usage granularity has no Jev equivalent".to_owned(),
        });
    };
    if usage.cache_read_input_tokens.is_some() {
        add("cache_read_input_tokens");
    }
    if usage.cache_creation_input_tokens.is_some() {
        add("cache_creation_input_tokens");
    }
    if usage.input_tokens_details.is_some() {
        add("input_tokens_details");
    }
    if usage.output_tokens_details.is_some() {
        add("output_tokens_details");
    }
    losses
}

/// §7.5 canon: answer keys and each answer's recognized fields sort ascending,
/// while field values carry verbatim (nested key order and number tokens
/// preserved) — so the text composes layer-by-layer instead of deep-sorting
/// the whole payload.
pub(crate) fn answers_canon_text(
    answers: &BTreeMap<String, Value>,
    losses: &mut Vec<Loss>,
) -> Result<String, Error> {
    let mut entries: Vec<String> = Vec::new();
    for (id, answer) in answers {
        let object = answer
            .as_object()
            .ok_or_else(|| fail(format!("answer {id} must be an object")))?;
        let mut fields: Vec<(String, String)> = Vec::new();
        for (field, leaf) in object {
            let raw = serde_json::to_string(leaf).map_err(|err| fail(err.to_string()))?;
            match field.as_str() {
                "type" | "noul" | "choice" | "score" | "legend" | "probabilities"
                | "confidence" | "box" => {
                    fields.push((field.clone(), compact_json_text(&raw)));
                }
                other => losses.push(Loss {
                    path: format!("answers.{id}"),
                    field: other.to_owned(),
                    reason: LossReason::UnmappedField,
                    detail: "unknown property on the Jev answer; dropped".to_owned(),
                }),
            }
        }
        fields.sort_by(|a, b| a.0.cmp(&b.0));
        let body = fields
            .iter()
            .map(|(name, raw)| format!("{}:{raw}", serde_json::to_string(name).unwrap()))
            .collect::<Vec<_>>()
            .join(",");
        entries.push(format!(
            "{}:{{{}}}",
            serde_json::to_string(id).unwrap(),
            body
        ));
        if answer.get("type").and_then(|v| v.as_str()) == Some(QUESTION_TYPE_BOX) {
            losses.push(Loss {
                path: format!("answers.{id}"),
                field: "type".to_owned(),
                reason: LossReason::UnsupportedSemantic,
                detail: "N-JEV-5: experimental box answers are carried verbatim".to_owned(),
            });
        }
    }
    Ok(format!("{{{}}}", entries.join(",")))
}

/// Converts a Jev wire response to the IR (spec/17 §4).
pub fn decode_response(
    wire: &WireResponse,
    config: &Config,
) -> Result<(IrResponse, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    if wire.model.is_empty() {
        return Err(fail("response model is required"));
    }
    if wire.answers.is_empty() {
        return Err(fail("response answers must not be empty"));
    }
    let canon_text = answers_canon_text(&wire.answers, &mut losses)?;

    Ok((
        IrResponse {
            id: SYNTHESIZED_ID.to_owned(),
            model: config.map_model(&wire.model),
            content: vec![Block::Text {
                text: canon_text,
                citations: None,
                native: Vec::new(),
            }],
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: wire.usage.input_tokens,
                output_tokens: wire.usage.output_tokens,
                cache_read_input_tokens: None,
                cache_creation_input_tokens: None,
                input_tokens_details: None,
                output_tokens_details: None,
            },
        },
        losses,
    ))
}
