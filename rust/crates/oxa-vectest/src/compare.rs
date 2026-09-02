//! The normative comparison rules of vectors/README.md:
//!
//! 1. structural JSON equality (key order irrelevant, arrays ordered,
//!    strings exact);
//! 2. integers stay integers — `1` does not match `1.0` (spec/01 INV-7);
//! 3. raw-JSON strings are JSON strings, so rule 1 covers them exactly;
//! 4. losses compare as unordered sets keyed on `(path, field, reason)`.

use std::collections::HashMap;

use serde_json::Value;

use crate::load::LossRecord;

/// Structural JSON equality with integer type fidelity (rules 1–3). Returns
/// `Err` with a path describing the first difference.
pub fn compare_json(expected: &Value, actual: &Value) -> Result<(), String> {
    equal_json(expected, actual, String::new())
        .map_err(|path| format!("structural mismatch at {path}"))
}

fn equal_json(expected: &Value, actual: &Value, path: String) -> Result<(), String> {
    match (expected, actual) {
        (Value::Object(expected), Value::Object(actual)) => {
            for (key, value) in expected {
                let Some(other) = actual.get(key) else {
                    return Err(format!("{path}.{key}: missing in actual"));
                };
                equal_json(value, other, format!("{path}.{key}"))?;
            }
            for key in actual.keys() {
                if !expected.contains_key(key) {
                    return Err(format!("{path}.{key}: unexpected in actual"));
                }
            }
            Ok(())
        }
        (Value::Array(expected), Value::Array(actual)) => {
            if expected.len() != actual.len() {
                return Err(format!(
                    "{path}: expected {} elements, got {}",
                    expected.len(),
                    actual.len()
                ));
            }
            for (index, (expected, actual)) in expected.iter().zip(actual.iter()).enumerate() {
                equal_json(expected, actual, format!("{path}[{index}]"))?;
            }
            Ok(())
        }
        (Value::Number(expected), Value::Number(actual)) => {
            // serde_json's Number equality already carries integer type
            // fidelity: PosInt/NegInt never equal Float, while floats compare
            // numerically (0.5 == 0.50).
            if expected == actual {
                Ok(())
            } else {
                Err(format!(
                    "{path}: number differs: expected {expected}, got {actual}"
                ))
            }
        }
        (Value::String(expected), Value::String(actual)) => {
            if expected == actual {
                Ok(())
            } else {
                Err(format!(
                    "{path}: string differs: expected {expected:?}, got {actual:?}"
                ))
            }
        }
        (Value::Bool(expected), Value::Bool(actual)) => {
            if expected == actual {
                Ok(())
            } else {
                Err(format!(
                    "{path}: bool differs: expected {expected}, got {actual}"
                ))
            }
        }
        (Value::Null, Value::Null) => Ok(()),
        _ => Err(format!(
            "{path}: expected {}, got {}",
            type_name(expected),
            type_name(actual)
        )),
    }
}

fn type_name(value: &Value) -> &'static str {
    match value {
        Value::Null => "null",
        Value::Bool(_) => "bool",
        Value::Number(_) => "number",
        Value::String(_) => "string",
        Value::Array(_) => "array",
        Value::Object(_) => "object",
    }
}

/// Unordered-set comparison keyed on `(path, field, reason)` (rule 4); every
/// expected loss must be reported and every reported loss must be expected,
/// counted as a multiset.
pub fn compare_losses(expected: &[LossRecord], reported: &[LossRecord]) -> Result<(), String> {
    fn counts(list: &[LossRecord]) -> HashMap<(&str, &str, &str), usize> {
        let mut map = HashMap::new();
        for loss in list {
            *map.entry((
                loss.path.as_str(),
                loss.field.as_str(),
                loss.reason.as_str(),
            ))
            .or_insert(0) += 1;
        }
        map
    }
    let (expected, reported) = (counts(expected), counts(reported));
    let mut problems: Vec<String> = Vec::new();
    for (key, count) in &expected {
        if reported.get(key).copied().unwrap_or(0) < *count {
            problems.push(format!(
                "expected loss not reported (or reported fewer times): path={:?} field={:?} reason={:?}",
                key.0, key.1, key.2
            ));
        }
    }
    for (key, count) in &reported {
        if expected.get(key).copied().unwrap_or(0) < *count {
            problems.push(format!(
                "unexpected loss reported: path={:?} field={:?} reason={:?}",
                key.0, key.1, key.2
            ));
        }
    }
    if problems.is_empty() {
        Ok(())
    } else {
        Err(problems.join("; "))
    }
}
