//! IR → face conversions (spec/17 §3.1/§4.1). The response encode parses the
//! aggregated model output back into typed answers; range and integer
//! validation compare JSON number tokens as exact decimals.

use serde_json::Value;
use std::collections::BTreeMap;

use oxa_ir::{Block, Loss, LossReason, Request as IrRequest, Response as IrResponse, StopReason};

use crate::config::Config;
use crate::decode::{
    INSTRUCTION_TEMPLATE, QUESTION_TYPE_BOX, QUESTION_TYPE_CHOICE, QUESTION_TYPE_NOUL,
    QUESTION_TYPE_SCORE, SYNTHESIZED_ID, fail, supported_question_type, usage_loss_list,
};
use crate::error::Error;
use crate::json_util::{compare_decimal, parse_decimal, score_token_within_levels};
use crate::question::JevQuestion;
use crate::{WireRequest, WireResponse, WireUsage};

type JsonMap = serde_json::Map<String, Value>;

/// Rebuilds a Jev wire request from the IR (spec/17 §3.1).
pub fn encode_request(
    request: &IrRequest,
    config: &Config,
) -> Result<(WireRequest, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    let format = request
        .params
        .as_ref()
        .and_then(|params| params.response_format.as_ref());
    let Some(oxa_ir::ResponseFormat::JsonSchema { schema, .. }) = format else {
        return Err(fail("EncodeRequest requires a json_schema response format"));
    };
    if schema.is_empty() {
        return Err(fail("EncodeRequest requires a json_schema response format"));
    }

    let (questions, schema_losses) = questions_from_schema(schema)?;
    losses.extend(schema_losses);

    let (state, state_losses) = state_from_ir(request, &questions)?;
    losses.extend(state_losses);
    losses.extend(request_losses(request));

    Ok((
        WireRequest {
            model: config.map_model(&request.model),
            state: Value::String(state),
            questions: questions_wire(&questions),
            assets: None,
        },
        losses,
    ))
}

fn questions_wire(questions: &BTreeMap<String, JevQuestion>) -> BTreeMap<String, Value> {
    let mut out = BTreeMap::new();
    for (id, question) in questions {
        let mut object = JsonMap::new();
        object.insert("type".to_owned(), Value::String(question.kind.clone()));
        if let Some(instructions) = &question.instructions {
            object.insert(
                "instructions".to_owned(),
                Value::String(instructions.clone()),
            );
        }
        if let Some(criteria) = &question.criteria {
            object.insert("criteria".to_owned(), criteria.clone());
        }
        out.insert(id.clone(), Value::Object(object));
    }
    out
}

fn request_losses(request: &IrRequest) -> Vec<Loss> {
    let mut losses = Vec::new();
    let jev10 = |path: &str, field: &str| Loss {
        path: path.to_owned(),
        field: field.to_owned(),
        reason: LossReason::UnmappedField,
        detail: "N-JEV-10: one loss per present field".to_owned(),
    };
    if let Some(params) = &request.params {
        if params.temperature.is_some() {
            losses.push(jev10("params", "temperature"));
        }
        if params.top_p.is_some() {
            losses.push(jev10("params", "top_p"));
        }
        if params.max_tokens.is_some() {
            losses.push(jev10("params", "max_tokens"));
        }
        if params.stop_sequences.is_some() {
            losses.push(jev10("params", "stop_sequences"));
        }
        if params.reasoning_effort.is_some() {
            losses.push(jev10("params", "reasoning_effort"));
        }
        if params.verbosity.is_some() {
            losses.push(jev10("params", "verbosity"));
        }
    }
    if let Some(metadata) = &request.metadata {
        for key in metadata.keys() {
            losses.push(Loss {
                path: "metadata".to_owned(),
                field: key.clone(),
                reason: LossReason::UnmappedField,
                detail: "N-JEV-10: no Jev equivalent".to_owned(),
            });
        }
    }
    if request
        .tools
        .as_ref()
        .is_some_and(|tools| !tools.is_empty())
    {
        losses.push(jev10("tools", "tools"));
    }
    if request.tool_choice.is_some() {
        losses.push(jev10("tool_choice", "tool_choice"));
    }
    if !request.native.is_empty() {
        losses.push(jev10("native", "native"));
    }
    losses
}

fn state_from_ir(
    request: &IrRequest,
    questions: &BTreeMap<String, JevQuestion>,
) -> Result<(String, Vec<Loss>), Error> {
    let sys_parts: Vec<&str> = request
        .system
        .iter()
        .filter_map(|block| match block {
            oxa_ir::SystemBlock::Text { text, .. } if !text.is_empty() => Some(text.as_str()),
            _ => None,
        })
        .collect();
    if sys_parts.is_empty() {
        return Err(fail("EncodeRequest requires system text"));
    }
    let expected: BTreeMap<&str, &str> = questions
        .iter()
        .map(|(id, question)| (id.as_str(), question.kind.as_str()))
        .collect();
    let mut losses = Vec::new();
    for (index, message) in request.messages.iter().enumerate() {
        if message.role == oxa_ir::Role::User && is_derived_question_rendering(message, &expected) {
            continue;
        }
        losses.push(Loss {
            path: format!("messages[{index}]"),
            field: "content".to_owned(),
            reason: LossReason::UnsupportedSemantic,
            detail: "N-JEV-10: not the derived question rendering; Jev has no conversation turns"
                .to_owned(),
        });
    }
    Ok((strip_template(&sys_parts.join("\n\n")), losses))
}

// is_derived_question_rendering reports whether the message's content is
// exactly a §7.2 rendering of the expected questions: every block is a text
// block, every line is a question line, a criteria line, or blank, and the
// question (id, type) set matches. Mixed-in non-text blocks have no Jev
// destination and must surface as a loss, never as a silent drop.
fn is_derived_question_rendering(
    message: &oxa_ir::Message,
    expected: &BTreeMap<&str, &str>,
) -> bool {
    let mut texts: Vec<&str> = Vec::new();
    for block in &message.content {
        match block {
            Block::Text { text, .. } => texts.push(text),
            _ => return false,
        }
    }
    if texts.is_empty() {
        return false;
    }
    let mut found: BTreeMap<&str, &str> = BTreeMap::new();
    for text in texts {
        for line in text.split('\n') {
            if line.is_empty() || line == "criteria:" || line.starts_with("- ") {
                continue;
            }
            if let Some(rest) = line.strip_prefix("question ") {
                match parse_question_line(rest) {
                    Some((id, kind)) => {
                        if found.contains_key(id) {
                            return false;
                        }
                        found.insert(id, kind);
                    }
                    None => return false,
                }
                continue;
            }
            return false;
        }
    }
    if found.len() != expected.len() {
        return false;
    }
    expected
        .iter()
        .all(|(id, kind)| found.get(*id).copied() == Some(*kind))
}

fn parse_question_line(rest: &str) -> Option<(&str, &str)> {
    let open = rest.find(" (")?;
    let after = rest[open..].find("): ")?;
    let kind = rest.get(open + 2..open + after)?;
    Some((&rest[..open], kind))
}

fn strip_template(text: &str) -> String {
    let suffix = format!("\n\n{INSTRUCTION_TEMPLATE}");
    text.strip_suffix(&suffix).unwrap_or(text).to_owned()
}

// questions_from_schema reads the synthesized schema's properties (the one
// authorized read-only consumption of Params.ResponseFormat.Schema, spec/17
// §3) and reconstructs the questions map.
fn questions_from_schema(
    schema: &JsonMap,
) -> Result<(BTreeMap<String, JevQuestion>, Vec<Loss>), Error> {
    let Some(properties) = schema.get("properties").and_then(|v| v.as_object()) else {
        return Err(fail("response format schema has no properties"));
    };
    let mut questions: BTreeMap<String, JevQuestion> = BTreeMap::new();
    let mut losses: Vec<Loss> = Vec::new();
    for (id, property) in properties {
        let object = match property.as_object() {
            Some(object) => object,
            None => {
                return Err(fail(format!("schema property {id} must be an object")));
            }
        };
        let extra_key = object.keys().find(|key| {
            !matches!(
                key.as_str(),
                "type" | "minimum" | "maximum" | "enum" | "description"
            )
        });
        if extra_key.is_some() {
            losses.push(Loss {
                path: format!("questions.{id}"),
                field: "type".to_owned(),
                reason: LossReason::UnsupportedSemantic,
                detail: "N-JEV-6: schema constraint beyond the synthesized projection".to_owned(),
            });
            continue;
        }
        let prop_type = object.get("type").and_then(|v| v.as_str());
        let description = object
            .get("description")
            .and_then(|v| v.as_str())
            .map(str::to_owned);
        match prop_type {
            Some("number") => {
                if let Some(enum_values) = object.get("enum").and_then(|v| v.as_array()) {
                    match score_levels(enum_values) {
                        Some(levels) => {
                            questions.insert(
                                id.clone(),
                                JevQuestion {
                                    kind: QUESTION_TYPE_SCORE.to_owned(),
                                    instructions: description,
                                    criteria: Some(Value::Array(vec![
                                        Value::String(String::new());
                                        levels
                                    ])),
                                },
                            );
                            losses.push(Loss {
                                path: format!("questions.{id}"),
                                field: "criteria".to_owned(),
                                reason: LossReason::Degraded,
                                detail: "N-JEV-9: criteria descriptions live in the prompt text"
                                    .to_owned(),
                            });
                            continue;
                        }
                        None => {
                            losses.push(Loss {
                                path: format!("questions.{id}"),
                                field: "type".to_owned(),
                                reason: LossReason::UnsupportedSemantic,
                                detail: "N-JEV-6: skipped from schema and prompt".to_owned(),
                            });
                            continue;
                        }
                    }
                }
                let minimum = object.get("minimum").and_then(exact_decimal_token);
                let maximum = object.get("maximum").and_then(exact_decimal_token);
                if minimum.as_deref() == Some("0") && maximum.as_deref() == Some("1") {
                    questions.insert(
                        id.clone(),
                        JevQuestion {
                            kind: QUESTION_TYPE_NOUL.to_owned(),
                            instructions: description,
                            criteria: None,
                        },
                    );
                    continue;
                }
            }
            Some("string") => {
                if let Some(enum_values) = object.get("enum").and_then(|v| v.as_array()) {
                    let names: Option<Vec<&str>> = enum_values
                        .iter()
                        .map(|value| value.as_str())
                        .collect::<Option<Vec<&str>>>();
                    if let Some(names) = names.filter(|names| !names.is_empty()) {
                        let mut criteria = serde_json::Map::new();
                        for name in &names {
                            criteria.insert((*name).to_owned(), Value::String((*name).to_owned()));
                        }
                        questions.insert(
                            id.clone(),
                            JevQuestion {
                                kind: QUESTION_TYPE_CHOICE.to_owned(),
                                instructions: description,
                                criteria: Some(Value::Object(criteria)),
                            },
                        );
                        losses.push(Loss {
                            path: format!("questions.{id}"),
                            field: "criteria".to_owned(),
                            reason: LossReason::Degraded,
                            detail: "N-JEV-9: criteria descriptions live in the prompt text"
                                .to_owned(),
                        });
                        continue;
                    }
                }
            }
            _ => {}
        }
        losses.push(Loss {
            path: format!("questions.{id}"),
            field: "type".to_owned(),
            reason: LossReason::UnsupportedSemantic,
            detail: "N-JEV-6: skipped from schema and prompt".to_owned(),
        });
    }
    if questions.is_empty() {
        return Err(fail(
            "response format schema has no reconstructable questions",
        ));
    }
    Ok((questions, losses))
}

// exact_decimal_token renders a JSON number token exactly; None for
// non-number leaves.
fn exact_decimal_token(value: &Value) -> Option<String> {
    value.as_number().map(|number| number.to_string())
}

// score_levels validates an enum of ascending 1..N integers exactly and
// returns N.
fn score_levels(enum_values: &[Value]) -> Option<usize> {
    if enum_values.is_empty() {
        return None;
    }
    for (index, value) in enum_values.iter().enumerate() {
        let token = exact_decimal_token(value)?;
        let parts = parse_decimal(&token)?;
        if parts.negative || parts.frac_digits.chars().any(|c| c != '0') {
            return None;
        }
        let digits = parts.int_digits.trim_start_matches('0');
        let digits = if digits.is_empty() { "0" } else { digits };
        if digits != (index + 1).to_string() {
            return None;
        }
    }
    Some(enum_values.len())
}

/// Parses the aggregated model output back into typed Jev answers
/// (spec/17 §4.1).
pub fn encode_response(
    response: &IrResponse,
    config: &Config,
) -> Result<(WireResponse, Vec<Loss>), Error> {
    let mut losses = Vec::new();
    let mut texts: Vec<&str> = Vec::new();
    for (index, block) in response.content.iter().enumerate() {
        match block {
            Block::Text { text, .. } => texts.push(text),
            other => losses.push(Loss {
                path: format!("content[{index}]"),
                field: "type".to_owned(),
                reason: LossReason::UnsupportedSemantic,
                detail: "N-JEV-15: non-text blocks are excluded from answers".to_owned(),
            }),
        }
    }
    if texts.is_empty() {
        return Err(fail("response has no text content"));
    }
    let aggregated = texts.concat();
    let answers: BTreeMap<String, Value> = serde_json::from_str(&aggregated).map_err(|err| {
        fail(format!(
            "aggregated response text is not a JSON object: {err}"
        ))
    })?;
    if answers.is_empty() {
        return Err(fail("aggregated response text carries no answers"));
    }

    let context = AnswerContext::new(config);
    for (id, question) in &context.map {
        if !supported_question_type(&question.kind) {
            continue;
        }
        if !answers.contains_key(id) {
            return Err(fail(format!("answer {id} is missing from the response")));
        }
    }

    let mut recovered = BTreeMap::new();
    for (id, raw) in &answers {
        let answer = recover_answer(id, raw, &context)?;
        if !context.empty && !context.map.contains_key(id) {
            losses.push(Loss {
                path: "answers".to_owned(),
                field: id.clone(),
                reason: LossReason::UnmappedField,
                detail: "answer beyond the requested questions".to_owned(),
            });
        }
        if let Some(loss) = answer.loss {
            losses.push(loss);
        }
        recovered.insert(id.clone(), answer.value);
    }

    if response.stop_reason != StopReason::EndTurn && response.stop_reason != StopReason::Other {
        losses.push(Loss {
            path: "stop_reason".to_owned(),
            field: "stop_reason".to_owned(),
            reason: LossReason::UnmappedField,
            detail: "N-JEV-16: Jev responses carry no stop reason".to_owned(),
        });
    }
    losses.extend(usage_loss_list(&response.usage));

    Ok((
        WireResponse {
            model: config.map_model(&response.model),
            answers: recovered,
            usage: WireUsage {
                input_tokens: response.usage.input_tokens,
                output_tokens: response.usage.output_tokens,
            },
        },
        losses,
    ))
}

struct AnswerContext {
    map: BTreeMap<String, JevQuestion>,
    empty: bool,
}

impl AnswerContext {
    fn new(config: &Config) -> Self {
        let map = config.questions.clone().unwrap_or_default();
        AnswerContext {
            empty: map.is_empty(),
            map,
        }
    }

    fn question(&self, id: &str) -> Option<&JevQuestion> {
        self.map.get(id)
    }
}

struct RecoveredAnswer {
    value: Value,
    loss: Option<Loss>,
}

fn recover_answer(
    id: &str,
    raw: &Value,
    context: &AnswerContext,
) -> Result<RecoveredAnswer, Error> {
    match raw {
        Value::Bool(_) | Value::String(_) | Value::Number(_) => {
            recover_scalar_answer(id, raw, context)
        }
        Value::Object(_) => recover_object_answer(id, raw, context),
        _ => Err(fail(format!(
            "answer {id} must be an object or scalar value"
        ))),
    }
}

fn contradiction_loss(id: &str) -> Loss {
    Loss {
        path: format!("answers.{id}"),
        field: "type".to_owned(),
        reason: LossReason::UnmappedValue,
        detail: "N-JEV-12: carried answer contradicts the original question".to_owned(),
    }
}

fn degraded_type(id: &str) -> Loss {
    Loss {
        path: format!("answers.{id}"),
        field: "type".to_owned(),
        reason: LossReason::Degraded,
        detail: "N-JEV-13: noul/score are ambiguous without question context".to_owned(),
    }
}

fn out_of_range(id: &str, field: &str) -> Loss {
    Loss {
        path: format!("answers.{id}"),
        field: field.to_owned(),
        reason: LossReason::Degraded,
        detail: "N-JEV-14: value kept, not rewritten".to_owned(),
    }
}

fn boolean_noul(value: bool) -> Value {
    serde_json::json!(if value { 1 } else { 0 })
}

fn typed_answer(kind: &str, value_field: &str, value: Value) -> Value {
    let mut object = serde_json::Map::new();
    object.insert("type".to_owned(), Value::String(kind.to_owned()));
    object.insert(value_field.to_owned(), value);
    Value::Object(object)
}

fn recover_scalar_answer(
    id: &str,
    raw: &Value,
    context: &AnswerContext,
) -> Result<RecoveredAnswer, Error> {
    let question = context.question(id);
    if let Some(question) = question {
        match question.kind.as_str() {
            QUESTION_TYPE_NOUL => match raw {
                Value::Bool(value) => {
                    return Ok(RecoveredAnswer {
                        value: typed_answer(QUESTION_TYPE_NOUL, "noul", boolean_noul(*value)),
                        loss: None,
                    });
                }
                Value::Number(_) => {
                    let loss = if decimal_in_unit_interval(raw) {
                        None
                    } else {
                        Some(out_of_range(id, "noul"))
                    };
                    return Ok(RecoveredAnswer {
                        value: typed_answer(QUESTION_TYPE_NOUL, "noul", raw.clone()),
                        loss,
                    });
                }
                _ => {}
            },
            QUESTION_TYPE_CHOICE => {
                if let Value::String(choice) = raw {
                    let loss = match choice_names_of_criteria(question.criteria.as_ref()) {
                        Some(names) if !names.is_empty() && !names.contains(choice) => {
                            Some(out_of_range(id, "choice"))
                        }
                        _ => None,
                    };
                    return Ok(RecoveredAnswer {
                        value: typed_answer(QUESTION_TYPE_CHOICE, "choice", raw.clone()),
                        loss,
                    });
                }
            }
            QUESTION_TYPE_SCORE => {
                if raw.is_number() {
                    let levels = criteria_count_of(question.criteria.as_ref());
                    let loss = if levels > 0 && !score_token_within_levels(raw, levels) {
                        Some(out_of_range(id, "score"))
                    } else {
                        None
                    };
                    return Ok(RecoveredAnswer {
                        value: typed_answer(QUESTION_TYPE_SCORE, "score", raw.clone()),
                        loss,
                    });
                }
            }
            _ => {}
        }
        return Ok(RecoveredAnswer {
            value: raw.clone(),
            loss: Some(contradiction_loss(id)),
        });
    }
    match raw {
        Value::Bool(value) => Ok(RecoveredAnswer {
            value: typed_answer(QUESTION_TYPE_NOUL, "noul", boolean_noul(*value)),
            loss: None,
        }),
        Value::String(choice) => Ok(RecoveredAnswer {
            value: typed_answer(
                QUESTION_TYPE_CHOICE,
                "choice",
                Value::String(choice.clone()),
            ),
            loss: None,
        }),
        Value::Number(number) => Ok(RecoveredAnswer {
            value: typed_answer(QUESTION_TYPE_SCORE, "score", Value::Number(number.clone())),
            loss: Some(degraded_type(id)),
        }),
        _ => Err(fail(format!(
            "answer {id} must be an object or scalar value"
        ))),
    }
}

fn recover_object_answer(
    id: &str,
    obj: &Value,
    context: &AnswerContext,
) -> Result<RecoveredAnswer, Error> {
    let carried = obj.get("type").and_then(|v| v.as_str());
    if carried == Some(QUESTION_TYPE_BOX) {
        return Ok(RecoveredAnswer {
            value: obj.clone(),
            loss: Some(Loss {
                path: format!("answers.{id}"),
                field: "type".to_owned(),
                reason: LossReason::UnsupportedSemantic,
                detail: "N-JEV-5: experimental box answers are carried verbatim".to_owned(),
            }),
        });
    }
    let question = context.question(id);
    if let (Some(question), Some(carried)) = (question, carried) {
        if carried != question.kind {
            // The carried answer contradicts the original question: carry
            // verbatim rather than rewrite (spec/17 §4.1, N-JEV-12).
            return Ok(RecoveredAnswer {
                value: obj.clone(),
                loss: Some(contradiction_loss(id)),
            });
        }
    }
    let mut kind = carried.map(str::to_owned);
    if kind.is_none() {
        if let Some(question) = question {
            kind = Some(question.kind.clone());
        }
    }
    if kind.is_none() {
        kind = if obj.get("noul").is_some() {
            Some(QUESTION_TYPE_NOUL.to_owned())
        } else if obj.get("choice").is_some() {
            Some(QUESTION_TYPE_CHOICE.to_owned())
        } else if obj.get("score").is_some() {
            Some(QUESTION_TYPE_SCORE.to_owned())
        } else {
            return Err(fail(format!("answer {id} carries no recognized value")));
        }
    }
    let kind = kind.unwrap_or_default();

    // A typed answer carries verbatim; the encoder rewrites the object only
    // when it must synthesize the type or normalize a boolean noul.
    let rewrite = carried.is_none()
        || (kind == QUESTION_TYPE_NOUL && obj.get("noul").map(Value::is_boolean) == Some(true));
    if !rewrite {
        let loss = validate_answer(id, obj, &kind, question)?;
        return Ok(RecoveredAnswer {
            value: obj.clone(),
            loss,
        });
    }
    let mut working = obj.clone();
    if let Some(object) = working.as_object_mut() {
        object.insert("type".to_owned(), Value::String(kind.clone()));
        if kind == QUESTION_TYPE_NOUL && object.get("noul").map(Value::is_boolean) == Some(true) {
            let normalized = object.get("noul").and_then(Value::as_bool).unwrap_or(false);
            object.insert("noul".to_owned(), boolean_noul(normalized));
        }
    }
    let loss = validate_answer(id, &working, &kind, question)?;
    Ok(RecoveredAnswer {
        value: working,
        loss,
    })
}

// validate_answer reports the range/option losses of an answer against its
// question context; value-shape failures stay structural.
fn validate_answer(
    id: &str,
    holder: &Value,
    kind: &str,
    question: Option<&JevQuestion>,
) -> Result<Option<Loss>, Error> {
    let Some(object) = holder.as_object() else {
        return Ok(None);
    };
    match kind {
        QUESTION_TYPE_NOUL => {
            let leaf = match object.get("noul") {
                Some(leaf) if leaf.is_boolean() => return Ok(None),
                Some(leaf) => leaf,
                None => return Err(fail(format!("answer {id} carries no noul value"))),
            };
            leaf.as_number()
                .ok_or_else(|| fail(format!("answer {id} carries no noul value")))?;
            let question = match question {
                Some(question) if question.kind == QUESTION_TYPE_NOUL => question,
                _ => return Ok(None),
            };
            if !decimal_in_unit_interval(leaf) {
                return Ok(Some(out_of_range(id, "noul")));
            }
            Ok(None)
        }
        QUESTION_TYPE_CHOICE => {
            let choice = match object.get("choice").and_then(|v| v.as_str()) {
                Some(choice) => choice,
                None => return Err(fail(format!("answer {id} carries no choice value"))),
            };
            let Some(question) = question else {
                return Ok(None);
            };
            if let Some(names) = choice_names_of_criteria(question.criteria.as_ref()) {
                if !names.is_empty() && !names.contains(&choice.to_owned()) {
                    return Ok(Some(out_of_range(id, "choice")));
                }
            }
            Ok(None)
        }
        QUESTION_TYPE_SCORE => {
            let leaf = match object.get("score") {
                Some(leaf) => leaf,
                None => return Err(fail(format!("answer {id} carries no score value"))),
            };
            leaf.as_number()
                .ok_or_else(|| fail(format!("answer {id} carries no score value")))?;
            let Some(question) = question else {
                return Ok(None);
            };
            let levels = criteria_count_of(question.criteria.as_ref());
            if levels > 0 && !score_token_within_levels(leaf, levels) {
                return Ok(Some(out_of_range(id, "score")));
            }
            Ok(None)
        }
        _ => Ok(None),
    }
}

fn choice_names_of_criteria(criteria: Option<&Value>) -> Option<Vec<String>> {
    criteria
        .and_then(|criteria| criteria.as_object())
        .map(|object| object.keys().cloned().collect())
}

fn criteria_count_of(criteria: Option<&Value>) -> usize {
    criteria
        .and_then(|criteria| criteria.as_array())
        .map(|array| array.len())
        .unwrap_or(0)
}

// decimal_in_unit_interval reports whether a JSON number token is exactly
// within [0,1] in decimal terms (spec/17 §4.1 noul range).
fn decimal_in_unit_interval(value: &Value) -> bool {
    let Some(low) = compare_decimal(value, "0") else {
        return false;
    };
    if low < 0 {
        return false;
    }
    let Some(high) = compare_decimal(value, "1") else {
        return false;
    };
    high <= 0
}
