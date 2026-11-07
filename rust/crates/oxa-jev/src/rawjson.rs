//! Raw JSON entrypoints (spec/02 §4): unknown properties are audited in
//! source encounter order, interleaved with the semantic losses of each key.

use serde_json::Value;
use serde_json::value::RawValue;
use std::collections::BTreeMap;

use oxa_ir::{Loss, LossReason, Request as IrRequest, Response as IrResponse};

use crate::config::Config;
use crate::decode::{
    INSTRUCTION_TEMPLATE, QUESTION_TYPE_BOX, SYNTHESIZED_ID, WireUsage, fail,
    supported_question_type, whole_unit_skip_detail,
};
use crate::error::Error;
use crate::json_util::{compact_json_text, parse_source_object};
use crate::question::JevQuestion;

/// Converts a raw Jev request document to the IR with losses in source
/// encounter order (spec/17 §3, spec/02 §4).
pub fn decode_request_json(raw: &str, config: &Config) -> Result<(IrRequest, Vec<Loss>), Error> {
    let root = parse_source_object(raw, "request JSON")?;
    let mut losses: Vec<Loss> = Vec::new();
    let mut model: Option<String> = None;
    let mut model_seen = false;
    let mut state_text: Option<String> = None;
    let mut state_seen = false;
    let mut questions: Option<BTreeMap<String, JevQuestion>> = None;
    let mut questions_seen = false;

    fn unknown(losses: &mut Vec<Loss>, path: &str, key: &str, what: &str) {
        losses.push(Loss {
            path: path.to_owned(),
            field: key.to_owned(),
            reason: LossReason::UnmappedField,
            detail: format!("unknown property on the {what}"),
        });
    }

    for (key, raw_value) in &root.entries {
        match key.as_str() {
            "model" => {
                let value: String = serde_json::from_str(raw_value.get())
                    .map_err(|_| fail("request model must be a string"))?;
                model = Some(value);
                model_seen = true;
            }
            "state" => {
                state_seen = true;
                if raw_value.get() == "null" {
                    continue;
                }
                let text = raw_value.get().to_owned();
                if text.starts_with('"') {
                    state_text = Some(
                        serde_json::from_str::<String>(&text)
                            .map_err(|err| fail(err.to_string()))?,
                    );
                } else if text.starts_with('{') || text.starts_with('[') {
                    losses.push(Loss {
                        path: "state".to_owned(),
                        field: "state".to_owned(),
                        reason: LossReason::Degraded,
                        detail: "N-JEV-1: state serialized to canonical JSON text".to_owned(),
                    });
                    let value: Value =
                        serde_json::from_str(&text).map_err(|err| fail(err.to_string()))?;
                    state_text =
                        Some(serde_json::to_string(&value).map_err(|err| fail(err.to_string()))?);
                } else {
                    return Err(fail("state must be a string, object, or array"));
                }
            }
            "questions" => {
                let map = parse_source_object(raw_value.get(), "request questions")?;
                if map.is_empty() {
                    return Err(fail("request questions must not be empty"));
                }
                let mut collected: BTreeMap<String, JevQuestion> = BTreeMap::new();
                for (id, question_raw) in &map.entries {
                    let question =
                        parse_source_object(question_raw.get(), &format!("question {id}"))?;
                    let kind = question
                        .tag("type")
                        .ok_or_else(|| fail(format!("question {id} type must be a string")))?;
                    if kind == QUESTION_TYPE_BOX || !supported_question_type(&kind) {
                        losses.push(Loss {
                            path: format!("questions.{id}"),
                            field: "type".to_owned(),
                            reason: LossReason::UnsupportedSemantic,
                            detail: whole_unit_skip_detail(&kind).to_owned(),
                        });
                        continue;
                    }
                    let mut question_value = JevQuestion {
                        kind: kind.clone(),
                        instructions: None,
                        criteria: None,
                    };
                    for (field, leaf) in &question.entries {
                        match field.as_str() {
                            "type" => {}
                            "instructions" => {
                                question_value.instructions = Some(
                                    serde_json::from_str::<String>(leaf.get()).map_err(|_| {
                                        fail(format!("question {id} instructions must be a string"))
                                    })?,
                                );
                            }
                            "criteria" => {
                                let value: Value = serde_json::from_str(leaf.get())
                                    .map_err(|err| fail(err.to_string()))?;
                                question_value.criteria = Some(value);
                            }
                            other => unknown(
                                &mut losses,
                                &format!("questions.{id}"),
                                other,
                                "Jev question",
                            ),
                        }
                    }
                    collected.insert(id.clone(), question_value);
                }
                questions = Some(collected);
                questions_seen = true;
            }
            "assets" => losses.push(Loss {
                path: "assets".to_owned(),
                field: "assets".to_owned(),
                reason: LossReason::UnsupportedSemantic,
                detail: "N-JEV-4: experimental image assets are out of scope".to_owned(),
            }),
            other => unknown(&mut losses, other, other, "Jev request"),
        }
    }

    if !model_seen || model.as_deref().unwrap_or("").is_empty() {
        return Err(fail("request model is required"));
    }
    if !state_seen || state_text.is_none() {
        return Err(fail("request state is required"));
    }
    if !questions_seen || questions.as_ref().is_none_or(|m| m.is_empty()) {
        return Err(fail("request questions must not be empty"));
    }

    let state = state_text.unwrap_or_default();
    let collected = questions.unwrap_or_default();
    if collected.is_empty() {
        return Err(fail("request questions produced no supported questions"));
    }
    let user_text = crate::decode::render_user_text(&collected)?;
    let params = crate::decode::synthesized_response_format(&collected)?;

    Ok((
        IrRequest {
            model: config.map_model(&model.unwrap_or_default()),
            system: vec![oxa_ir::SystemBlock::Text {
                text: format!("{state}\n\n{INSTRUCTION_TEMPLATE}"),
                role: String::new(),
                native: Vec::new(),
            }],
            messages: vec![oxa_ir::Message {
                role: oxa_ir::Role::User,
                content: vec![oxa_ir::Block::Text {
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

/// Converts a raw Jev response document to the IR with losses in source
/// encounter order (spec/17 §4, spec/02 §4).
pub fn decode_response_json(raw: &str, config: &Config) -> Result<(IrResponse, Vec<Loss>), Error> {
    let root = parse_source_object(raw, "response JSON")?;
    let mut losses: Vec<Loss> = Vec::new();
    let mut model: Option<String> = None;
    let mut model_seen = false;
    let mut answers: Option<BTreeMap<String, &RawValue>> = None;
    let mut answers_seen = false;
    let mut usage: Option<WireUsage> = None;
    let mut usage_seen = false;

    fn unknown(losses: &mut Vec<Loss>, path: &str, key: &str, what: &str) {
        losses.push(Loss {
            path: path.to_owned(),
            field: key.to_owned(),
            reason: LossReason::UnmappedField,
            detail: format!("unknown property on the {what}"),
        });
    }

    for (key, raw_value) in &root.entries {
        match key.as_str() {
            "model" => {
                let value: String = serde_json::from_str(raw_value.get())
                    .map_err(|_| fail("response model must be a string"))?;
                model = Some(value);
                model_seen = true;
            }
            "answers" => {
                let map = parse_source_object(raw_value.get(), "answers")?;
                if map.is_empty() {
                    return Err(fail("response answers must not be empty"));
                }
                let mut collected: BTreeMap<String, &RawValue> = BTreeMap::new();
                for (id, answer_raw) in &map.entries {
                    let answer = parse_source_object(answer_raw.get(), &format!("answer {id}"))?;
                    for (field, leaf) in &answer.entries {
                        match field.as_str() {
                            "type" => {
                                if serde_json::from_str::<String>(leaf.get()).ok().as_deref()
                                    == Some(QUESTION_TYPE_BOX)
                                {
                                    losses.push(Loss {
                                        path: format!("answers.{id}"),
                                        field: "type".to_owned(),
                                        reason: LossReason::UnsupportedSemantic,
                                        detail:
                                            "N-JEV-5: experimental box answers are carried verbatim"
                                                .to_owned(),
                                    });
                                }
                            }
                            "noul" | "choice" | "score" | "legend" | "probabilities"
                            | "confidence" | "box" => {}
                            other => {
                                unknown(&mut losses, &format!("answers.{id}"), other, "Jev answer")
                            }
                        }
                    }
                    collected.insert(id.clone(), *answer_raw);
                }
                answers = Some(collected);
                answers_seen = true;
            }
            "usage" => {
                let map = parse_source_object(raw_value.get(), "usage")?;
                let mut input: Option<i64> = None;
                let mut output: Option<i64> = None;
                for (field, leaf) in &map.entries {
                    match field.as_str() {
                        "input_tokens" => {
                            input = Some(parse_usage_integer(leaf.get(), "input_tokens")?);
                        }
                        "output_tokens" => {
                            output = Some(parse_usage_integer(leaf.get(), "output_tokens")?);
                        }
                        other => unknown(&mut losses, "usage", other, "Jev usage"),
                    }
                }
                usage = Some(WireUsage {
                    input_tokens: input.unwrap_or(0),
                    output_tokens: output.unwrap_or(0),
                });
                usage_seen = true;
            }
            other => unknown(&mut losses, other, other, "Jev response"),
        }
    }

    if !model_seen || model.as_deref().unwrap_or("").is_empty() {
        return Err(fail("response model is required"));
    }
    if !answers_seen {
        return Err(fail("response answers must not be empty"));
    }
    if !usage_seen {
        return Err(fail("response usage must be an object"));
    }

    // §7.5 canon: answer keys and each answer's recognized fields sort
    // ascending, while field values carry verbatim (compact source text).
    let mut entries: Vec<String> = Vec::new();
    let collected = answers.unwrap_or_default();
    for (id, answer_raw) in &collected {
        let answer = parse_source_object(answer_raw.get(), &format!("answer {id}"))?;
        let mut fields: Vec<(String, String)> = Vec::new();
        for (field, leaf) in &answer.entries {
            match field.as_str() {
                "type" | "noul" | "choice" | "score" | "legend" | "probabilities"
                | "confidence" | "box" => {
                    fields.push((field.clone(), compact_json_text(leaf.get())));
                }
                _ => {}
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
    }
    let canon_text = format!("{{{}}}", entries.join(","));

    Ok((
        IrResponse {
            id: SYNTHESIZED_ID.to_owned(),
            model: config.map_model(&model.unwrap_or_default()),
            content: vec![oxa_ir::Block::Text {
                text: canon_text,
                citations: None,
                native: Vec::new(),
            }],
            stop_reason: oxa_ir::StopReason::EndTurn,
            stop_sequence: None,
            usage: oxa_ir::Usage {
                input_tokens: usage.as_ref().map(|u| u.input_tokens).unwrap_or(0),
                output_tokens: usage.as_ref().map(|u| u.output_tokens).unwrap_or(0),
                cache_read_input_tokens: None,
                cache_creation_input_tokens: None,
                input_tokens_details: None,
                output_tokens_details: None,
            },
        },
        losses,
    ))
}

fn parse_usage_integer(raw: &str, name: &str) -> Result<i64, crate::Error> {
    let value: Value = serde_json::from_str(raw).map_err(|err| fail(err.to_string()))?;
    let number = value
        .as_number()
        .ok_or_else(|| fail(format!("usage {name} must be an integer")))?;
    let token = number.to_string();
    if token.contains(['.', 'e', 'E']) {
        return Err(fail(format!("usage {name} must be an integer")));
    }
    token
        .parse::<i64>()
        .map_err(|err| fail(format!("usage {name}: {err}")))
}
