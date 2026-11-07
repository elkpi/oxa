//! JSON helpers for the Jev face: compact re-serialization, source-order
//! object walks, and exact decimal comparison of number tokens (spec/17
//! §7.5/§7.6 canon; range validation must not round tokens through f64, and
//! exponent handling must not expand adversarially long powers of ten into
//! memory).

use serde::de::{MapAccess, Visitor};
use serde::{Deserialize, Deserializer};
use serde_json::Value;
use serde_json::value::RawValue;

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
// A token is represented as ±digits × 10^exp with `digits` stripped of
// leading and trailing zeros, so magnitude comparison works on the digit
// strings and their order of magnitude without ever materializing a power
// of ten: 1.0000000000000001 is greater than 1 and 1.5 is not a score level,
// even though both collapse to the same f64.

pub(crate) struct DecimalParts {
    pub(crate) negative: bool,
    pub(crate) digits: String,
    pub(crate) exp: i64,
}

/// Parses the exponent literal with saturation at ±2^62: the sign of the
/// overflow preserves whether the value is astronomically large or small,
/// which is all the range checks need.
fn parse_exponent_saturating(literal: &str) -> i64 {
    const LIMIT: i64 = 1 << 62;
    let negative = literal.starts_with('-');
    let body = literal.trim_start_matches('+').trim_start_matches('-');
    if body.is_empty() || !body.chars().all(|c| c.is_ascii_digit()) {
        return 0;
    }
    let mut value: i64 = 0;
    for character in body.chars() {
        let digit = (character as u8 - b'0') as i64;
        if value > LIMIT / 10 {
            value = LIMIT;
            break;
        }
        value *= 10;
        if value > LIMIT - digit {
            value = LIMIT;
            break;
        }
        value += digit;
    }
    if negative { -value } else { value }
}

pub(crate) fn parse_decimal(token: &str) -> Option<DecimalParts> {
    let negative = token.starts_with('-');
    let body = token.trim_start_matches('-');
    let (mantissa, exponent) = match body.find(['e', 'E']) {
        Some(index) => (
            &body[..index],
            Some(parse_exponent_saturating(&body[index + 1..])),
        ),
        None => (body, None),
    };
    let (int_part, frac_part) = match mantissa.find('.') {
        Some(dot) => (&mantissa[..dot], &mantissa[dot + 1..]),
        None => (mantissa, ""),
    };
    if int_part.is_empty() && frac_part.is_empty() {
        return None;
    }
    if !int_part.chars().all(|c| c.is_ascii_digit())
        || !frac_part.chars().all(|c| c.is_ascii_digit())
    {
        return None;
    }
    let mut exp = exponent.unwrap_or(0) - frac_part.len() as i64;
    let mut digits = format!("{int_part}{frac_part}");
    let stripped = digits.trim_start_matches('0');
    let stripped = if stripped.is_empty() { "0" } else { stripped };
    digits = stripped.to_owned();
    if digits != "0" {
        let trailing = digits.len() - digits.trim_end_matches('0').len();
        digits.truncate(digits.len() - trailing);
        exp += trailing as i64;
    } else {
        exp = 0;
    }
    Some(DecimalParts {
        negative,
        digits,
        exp,
    })
}

pub(crate) fn decimal_is_zero(parts: &DecimalParts) -> bool {
    parts.digits == "0"
}

/// Reports whether a JSON number token is exactly within [0,1] in decimal
/// terms (spec/17 §4.1 noul range).
pub(crate) fn decimal_in_unit_interval(a: &Value) -> bool {
    let Some(number) = a.as_number() else {
        return false;
    };
    let Some(parts) = parse_decimal(&number.to_string()) else {
        return false;
    };
    if decimal_is_zero(&parts) {
        return true;
    }
    if parts.negative {
        return false;
    }
    // A positive nonzero value is within [0,1] exactly when it is 1 or a
    // decimal fraction below 1: digits × 10^exp ≤ 1 with digits ≥ 1.
    if parts.exp > 0 {
        return false;
    }
    if parts.exp == 0 {
        return parts.digits == "1";
    }
    // exp < 0: value = digits × 10^exp ≤ 1 ⇔ digits ≤ 10^(-exp). The bound
    // only matters when its digit count can reach digits.len(); a longer
    // bound means digits is certainly below it, and a saturated exponent
    // never materializes as a power of ten.
    let limit_exp = (-parts.exp) as usize;
    let digits = parts.digits.trim_start_matches('0');
    if limit_exp + 1 > digits.len() {
        return true;
    }
    let bound = format!("1{}", "0".repeat(limit_exp));
    if digits.len() < bound.len() {
        return true;
    }
    digits.len() == bound.len() && digits <= bound.as_str()
}

/// Validates a score value against the discrete 1..N levels exactly: the
/// token must be an integer whose exact value falls in range (spec/17 §4.1).
pub(crate) fn score_token_within_levels(a: &Value, levels: usize) -> bool {
    let Some(number) = a.as_number() else {
        return false;
    };
    let Some(parts) = parse_decimal(&number.to_string()) else {
        return false;
    };
    if decimal_is_zero(&parts) || parts.negative {
        return false;
    }
    // A fractional value (negative exp after trailing-zero stripping) is not
    // an integer level; a positive exp of 2 or more is at least 100, beyond
    // the 1..10 level bound.
    if parts.exp < 0 || parts.exp > 2 {
        return false;
    }
    let value_digits = format!("{}{}", parts.digits, "0".repeat(parts.exp as usize));
    let level_digits = levels.to_string();
    if value_digits.len() != level_digits.len() {
        return value_digits.len() < level_digits.len();
    }
    match value_digits.cmp(&level_digits) {
        std::cmp::Ordering::Less | std::cmp::Ordering::Equal => true,
        std::cmp::Ordering::Greater => false,
    }
}

/// Reports whether a JSON number token equals exactly 1 (used for the
/// synthesized-schema bound check, accepting any decimal spelling such as
/// `1.0` or `1e0`).
pub(crate) fn decimal_equals_one(a: &Value) -> bool {
    let Some(number) = a.as_number() else {
        return false;
    };
    let Some(parts) = parse_decimal(&number.to_string()) else {
        return false;
    };
    if decimal_is_zero(&parts) || parts.negative {
        return false;
    }
    parts.exp == 0 && parts.digits == "1"
}

// --- source-order object walks ------------------------------------------------

/// A source-order JSON object with raw value texts.
pub(crate) struct SourceObject<'a> {
    pub(crate) entries: Vec<(String, &'a RawValue)>,
}

impl<'a> SourceObject<'a> {
    pub(crate) fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    pub(crate) fn get(&self, key: &str) -> Option<&'a RawValue> {
        self.entries
            .iter()
            .find(|(name, _)| name == key)
            .map(|(_, raw)| *raw)
    }

    pub(crate) fn tag(&self, key: &str) -> Option<String> {
        self.entries
            .iter()
            .rev()
            .find(|(name, _)| name == key)
            .and_then(|(_, raw)| serde_json::from_str(raw.get()).ok())
    }
}

impl<'de> Deserialize<'de> for SourceObject<'de> {
    fn deserialize<D: Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        struct ObjectVisitor;
        impl<'de> Visitor<'de> for ObjectVisitor {
            type Value = SourceObject<'de>;
            fn expecting(&self, formatter: &mut std::fmt::Formatter) -> std::fmt::Result {
                formatter.write_str("a JSON object")
            }
            fn visit_map<M: MapAccess<'de>>(self, mut map: M) -> Result<Self::Value, M::Error> {
                let mut entries = Vec::new();
                while let Some(key) = map.next_key::<String>()? {
                    entries.push((key, map.next_value::<&'de RawValue>()?));
                }
                Ok(SourceObject { entries })
            }
        }
        deserializer.deserialize_map(ObjectVisitor)
    }
}

pub(crate) fn parse_source_object<'a>(
    raw: &'a str,
    what: &str,
) -> Result<SourceObject<'a>, crate::Error> {
    let value: &'a RawValue = serde_json::from_str(raw)
        .map_err(|err| crate::Error::new(format!("jev: {what}: {err}")))?;
    let parsed: SourceObject<'a> = serde_json::from_str(value.get())
        .map_err(|_| crate::Error::new(format!("jev: {what} must be an object")))?;
    Ok(parsed)
}
