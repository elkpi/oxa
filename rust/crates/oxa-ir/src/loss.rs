//! Loss records (spec/02). Losses are first-class conversion output: every
//! semantic gap is reported, never silently dropped and never turned into an
//! error. `path` is root-relative: object keys joined by `.`, array elements
//! addressed by zero-based index in brackets.

use serde::{Deserialize, Serialize};

/// The reason code of a loss record (spec/02 §3). Closed set.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum LossReason {
    /// The source field has no representation in the target dialect; the
    /// field is dropped.
    #[serde(rename = "unmapped-field")]
    UnmappedField,
    /// The field exists on both sides, but this specific value has no
    /// mapping.
    #[serde(rename = "unmapped-value")]
    UnmappedValue,
    /// A whole construct or combination the target dialect cannot express.
    #[serde(rename = "unsupported-semantic")]
    UnsupportedSemantic,
    /// Best-effort carry with known distortion; reserved for carry, never
    /// for drops.
    #[serde(rename = "degraded")]
    Degraded,
}

impl LossReason {
    pub fn as_str(&self) -> &'static str {
        match self {
            LossReason::UnmappedField => "unmapped-field",
            LossReason::UnmappedValue => "unmapped-value",
            LossReason::UnsupportedSemantic => "unsupported-semantic",
            LossReason::Degraded => "degraded",
        }
    }
}

impl std::fmt::Display for LossReason {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// A fidelity cost of a conversion (spec/02 §2).
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Loss {
    #[serde(rename = "path")]
    pub path: String,
    #[serde(rename = "field")]
    pub field: String,
    #[serde(rename = "reason")]
    pub reason: LossReason,
    #[serde(rename = "detail", default, skip_serializing_if = "String::is_empty")]
    pub detail: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn loss_round_trips_through_the_canonical_shape() {
        let loss = Loss {
            path: "messages[0]".to_string(),
            field: "logprobs".to_string(),
            reason: LossReason::UnmappedField,
            detail: String::new(),
        };
        let json = serde_json::to_value(&loss).expect("serialize");
        assert_eq!(
            json,
            serde_json::json!({
                    "path": "messages[0]",
                    "field": "logprobs",
                    "reason": "unmapped-field"
                }
            )
        );
        let back: Loss = serde_json::from_value(json).expect("deserialize");
        assert_eq!(back, loss);
    }

    #[test]
    fn degraded_round_trips_with_detail() {
        let loss = Loss {
            path: "params".to_string(),
            field: "temperature".to_string(),
            reason: LossReason::Degraded,
            detail: "clamped".to_string(),
        };
        let json = serde_json::to_value(&loss).expect("serialize");
        assert_eq!(json.get("detail"), Some(&serde_json::json!("clamped")));
        let back: Loss = serde_json::from_value(json).expect("deserialize");
        assert_eq!(back, loss);
    }
}
