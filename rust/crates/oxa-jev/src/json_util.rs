//! JSON helpers for the Jev face: source-order object walks, compact
//! re-serialization, and exact decimal comparison of number tokens (spec/17
//! §7.5/§7.6 canon; range validation must not round tokens through f64).

use serde::de::{MapAccess, Visitor};
use serde::{Deserialize, Deserializer};
use serde_json::Value;
use serde_json::value::RawValue;

/// A source-order JSON object: every key with its raw text.
pub struct RawObject<'a> {
    pub entries: Vec<(String, &'a RawValue)>,
}

impl<'de> Deserialize<'de> for RawObject<'de> {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct ObjectVisitor;
        impl<'de> Visitor<'de> for ObjectVisitor {
            type Value = RawObject<'de>;
            fn expecting(&self, formatter: &mut std::fmt::Formatter) -> std::fmt::Result {
                formatter.write_str("a JSON object")
            }
            fn visit_map<M: MapAccess<'de>>(self, mut map: M) -> Result<Self::Value, M::Error> {
                let mut entries = Vec::new();
                while let Some(key) = map.next_key::<String>()? {
                    entries.push((key, map.next_value::<&'de RawValue>()?));
                }
                Ok(RawObject { entries })
            }
        }
        deserializer.deserialize_map(ObjectVisitor)
    }
}

impl<'a> RawObject<'a> {
    /// Parses a JSON object keeping every key with its raw value text in
    /// source encounter order.
    pub fn parse(raw: &'a str, what: &str) -> Result<RawObject<'a>, crate::Error> {
        let value: &'a RawValue = serde_json::from_str(raw)
            .map_err(|err| crate::Error::new(format!("jev: {what}: {err}")))?;
        Self::from_raw_value(value, what)
    }

    pub fn from_raw_value(value: &'a RawValue, what: &str) -> Result<RawObject<'a>, crate::Error> {
        let parsed: RawObject<'a> = serde_json::from_str(value.get())
            .map_err(|_| crate::Error::new(format!("jev: {what} must be an object")))?;
        Ok(parsed)
    }

    pub fn get(&self, key: &str) -> Option<&'a RawValue> {
        self.entries
            .iter()
            .find(|(name, _)| name == key)
            .map(|(_, raw)| *raw)
    }

    pub fn tag(&self, key: &str) -> Option<String> {
        self.get(key).and_then(|raw| {
            serde_json::from_str::<String>(raw.get()).ok().or_else(|| {
                serde_json::from_str::<Value>(raw.get())
                    .ok()?
                    .as_str()
                    .map(str::to_owned)
            })
        })
    }

    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }
}

/// Removes whitespace outside JSON string literals, preserving key order and
/// number tokens — the wire-side equivalent of Go's json.Compact on RawValue
/// carries.
pub fn compact_json_text(raw: &str) -> String {
    let mut out = String::with_capacity(raw.len());
    let mut in_string = false;
    let mut escaped = false;
    for character in raw.chars() {
        if in_string {
            out.push(character);
            if escaped {
                escaped = false;
            } else if character == '\\' {
                escaped = true;
            } else if character == '"' {
                in_string = false;
            }
            continue;
        }
        match character {
            '"' => {
                in_string = true;
                out.push(character);
            }
            ' ' | '\n' | '\r' | '\t' => {}
            _ => out.push(character),
        }
    }
    out
}

// --- exact decimal comparison ------------------------------------------------
// Range and integer validation must not round JSON number tokens through
// IEEE 754 doubles: 1.0000000000000001 is greater than 1 and 1.5 is not a
// score level, even though both collapse to the same f64.

const JSON_NUMBER_PATTERN: &str = "^-?(?:0|[1-9][0-9]*)(?:\\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$";

pub(crate) struct DecimalParts {
    pub(crate) negative: bool,
    pub(crate) int_digits: String,
    pub(crate) frac_digits: String,
}

pub(crate) fn parse_decimal(token: &str) -> Option<DecimalParts> {
    if !is_json_number_shape(token) {
        return None;
    }
    let negative = token.starts_with('-');
    let mut body = token.trim_start_matches('-').to_owned();
    let mut exponent: i64 = 0;
    if let Some(e_index) = body.find(['e', 'E']) {
        exponent = body[e_index + 1..].parse().ok()?;
        body.truncate(e_index);
    }
    let (mut int_digits, mut frac_digits) = match body.find('.') {
        Some(dot) => (body[..dot].to_owned(), body[dot + 1..].to_owned()),
        None => (body.clone(), String::new()),
    };
    if exponent > 0 {
        let shift = exponent as usize;
        if shift >= frac_digits.len() {
            int_digits.push_str(&frac_digits);
            int_digits.push_str(&"0".repeat(shift - frac_digits.len()));
            frac_digits.clear();
        } else {
            int_digits.push_str(&frac_digits[..shift]);
            frac_digits.drain(..shift);
        }
    } else if exponent < 0 {
        let shift = (-exponent) as usize;
        if shift >= int_digits.len() {
            frac_digits = "0".repeat(shift - int_digits.len()) + &int_digits + &frac_digits;
            int_digits = "0".to_owned();
        } else {
            let keep = int_digits.len() - shift;
            frac_digits = int_digits[keep..].to_owned() + &frac_digits;
            int_digits.truncate(keep);
        }
    }
    let trimmed = int_digits.trim_start_matches('0');
    int_digits = if trimmed.is_empty() { "0" } else { trimmed }.to_owned();
    Some(DecimalParts {
        negative,
        int_digits,
        frac_digits,
    })
}

fn is_json_number_shape(token: &str) -> bool {
    let body = token.trim_start_matches('-');
    if body.is_empty() {
        return false;
    }
    let mut chars = body.chars().peekable();
    match chars.next() {
        Some('0') => {
            if let Some(next) = chars.peek() {
                if next.is_ascii_digit() {
                    return false;
                }
            }
        }
        Some(first) if first.is_ascii_digit() => {}
        _ => return false,
    }
    let mut seen_digit = true;
    for character in chars {
        match character {
            '.' => {
                if !seen_digit {
                    return false;
                }
                seen_digit = false;
            }
            'e' | 'E' => return true, // exponent body: delegate to parse
            _ if character.is_ascii_digit() => seen_digit = true,
            _ => return false,
        }
    }
    seen_digit
}

fn is_zero_decimal(parts: &DecimalParts) -> bool {
    parts.int_digits.chars().all(|c| c == '0') && parts.frac_digits.chars().all(|c| c == '0')
}

fn compare_magnitude(a: &DecimalParts, b: &DecimalParts) -> i32 {
    if a.int_digits.len() != b.int_digits.len() {
        return if a.int_digits.len() < b.int_digits.len() {
            -1
        } else {
            1
        };
    }
    if a.int_digits != b.int_digits {
        return if a.int_digits < b.int_digits { -1 } else { 1 };
    }
    let width = a.frac_digits.len().max(b.frac_digits.len());
    let af = format!("{:0<width$}", a.frac_digits, width = width);
    let bf = format!("{:0<width$}", b.frac_digits, width = width);
    if af == bf {
        0
    } else if af < bf {
        -1
    } else {
        1
    }
}

/// Compares a JSON number token against a decimal literal exactly; `None`
/// when the leaf is not a JSON number.
pub fn compare_decimal(a: &Value, b: &str) -> Option<i32> {
    let number = a.as_number()?;
    let left = parse_decimal(&number.to_string())?;
    let right = parse_decimal(b)?;
    let left_zero = is_zero_decimal(&left);
    let right_zero = is_zero_decimal(&right);
    if left_zero && right_zero {
        return Some(0);
    }
    if left_zero {
        return Some(if right.negative { 1 } else { -1 });
    }
    if right_zero {
        return Some(if left.negative { -1 } else { 1 });
    }
    if left.negative != right.negative {
        return Some(if left.negative { -1 } else { 1 });
    }
    let magnitude = compare_magnitude(&left, &right);
    Some(if left.negative { -magnitude } else { magnitude })
}

/// Validates a score value against the discrete 1..N levels exactly: the
/// token must be an integer and its exact value must fall in range
/// (spec/17 §4.1).
pub fn score_token_within_levels(a: &Value, levels: usize) -> bool {
    let number = match a.as_number() {
        Some(number) => number,
        None => return false,
    };
    let parts = match parse_decimal(&number.to_string()) {
        Some(parts) => parts,
        None => return false,
    };
    if parts.negative || parts.frac_digits.chars().any(|c| c != '0') {
        return false;
    }
    let digits = parts.int_digits.trim_start_matches('0');
    let digits = if digits.is_empty() { "0" } else { digits };
    if digits.len() > levels.to_string().len() {
        return false;
    }
    match digits.parse::<usize>() {
        Ok(value) => value >= 1 && value <= levels,
        Err(_) => false,
    }
}

/// Validates an enum of ascending 1..N integers exactly and returns N.
pub fn score_levels(enum_values: &[Value]) -> Option<usize> {
    if enum_values.is_empty() {
        return None;
    }
    for (index, value) in enum_values.iter().enumerate() {
        let number = value.as_number()?;
        let parts = parse_decimal(&number.to_string())?;
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
