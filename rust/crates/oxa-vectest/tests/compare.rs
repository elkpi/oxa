//! Mirrors go/internal/vectest/compare_test.go: the normative comparison
//! rules of vectors/README.md.

use oxa_vectest::{LossRecord, compare_json, compare_losses};
use serde_json::Value;

fn value(s: &str) -> Value {
    serde_json::from_str(s).expect("test JSON must parse")
}

fn loss(path: &str, field: &str, reason: &str, detail: &str) -> LossRecord {
    LossRecord {
        path: path.to_string(),
        field: field.to_string(),
        reason: reason.to_string(),
        detail: detail.to_string(),
    }
}

#[test]
fn compare_json_cases() {
    let cases: &[(&str, &str, &str, bool)] = &[
        (
            "key order irrelevant",
            r#"{"a":1,"b":2}"#,
            r#"{"b":2,"a":1}"#,
            true,
        ),
        ("nested arrays ordered", "[1,[2,3]]", "[1,[3,2]]", false),
        (
            "strings exact",
            r#"{"t":"a\nb"}"#,
            r#"{"t":"a\nb "}"#,
            false,
        ),
        (
            "integers stay integers",
            r#"{"n":1}"#,
            r#"{"n":1.0}"#,
            false,
        ),
        (
            "floats equal numerically",
            r#"{"n":0.5}"#,
            r#"{"n":0.50}"#,
            true,
        ),
        ("float vs int unequal", r#"{"n":1.5}"#, r#"{"n":2}"#, false),
        ("missing key", r#"{"a":1}"#, r#"{"a":1,"b":2}"#, false),
        ("extra key", r#"{"a":1,"b":2}"#, r#"{"a":1}"#, false),
    ];
    for (name, expected, actual, equal) in cases {
        let result = compare_json(&value(expected), &value(actual));
        assert_eq!(result.is_ok(), *equal, "{name}: {result:?}");
    }
}

#[test]
fn mismatch_message_carries_the_first_difference_path() {
    let err = compare_json(
        &value(r#"{"a":{"b":[1,2]}}"#),
        &value(r#"{"a":{"b":[1,3]}}"#),
    )
    .expect_err("must differ");
    assert!(err.contains(".a.b[1]"), "path in message: {err}");
}

#[test]
fn compare_losses_as_unordered_set() {
    let expected = vec![
        loss("b", "y", "unmapped-value", "d1"),
        loss("a", "x", "unmapped-field", "d2"),
    ];
    let reordered = vec![
        loss("a", "x", "unmapped-field", "ignored"),
        loss("b", "y", "unmapped-value", ""),
    ];
    compare_losses(&expected, &reordered).expect("order and detail must not matter");
    let missing = vec![loss("a", "x", "unmapped-field", "")];
    assert!(
        compare_losses(&expected, &missing).is_err(),
        "missing-loss error"
    );
    let mut extra = reordered.clone();
    extra.push(loss("c", "z", "degraded", ""));
    assert!(
        compare_losses(&expected, &extra).is_err(),
        "unexpected-loss error"
    );
    compare_losses(&[], &[]).expect("empty loss lists must compare equal");
}

#[test]
fn duplicate_losses_compare_as_multiset() {
    let expected = vec![loss("p", "f", "unmapped-field", "")];
    let doubled = vec![
        loss("p", "f", "unmapped-field", ""),
        loss("p", "f", "unmapped-field", ""),
    ];
    assert!(
        compare_losses(&expected, &doubled).is_err(),
        "reporting the same loss twice must not satisfy a single expectation"
    );
}
