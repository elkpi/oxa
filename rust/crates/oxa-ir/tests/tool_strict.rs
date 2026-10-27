use oxa_ir::Tool;
use serde_json::json;

#[test]
fn tool_strict_canonical_round_trip_preserves_tristate() {
    for value in [None, Some(false), Some(true)] {
        let mut input = json!({"name": "f", "input_schema": {}});
        if let Some(strict) = value {
            input["strict"] = json!(strict);
        }
        let tool: Tool = serde_json::from_value(input.clone()).unwrap();
        assert_eq!(tool.strict, value);
        assert_eq!(serde_json::to_value(tool).unwrap(), input);
    }
}

#[test]
fn non_function_tools_reject_strict() {
    for input in [
        json!({"name": "x", "input_schema": {}, "strict": null}),
        json!({"type": "custom", "name": "x", "strict": null}),
        json!({"type": "custom", "name": "x", "strict": false}),
        json!({"type": "apply_patch", "strict": true}),
        json!({"type": "native", "name": "x", "source": "s", "definition": "{\"type\":\"provider_tool\",\"name\":\"x\"}", "strict": false}),
    ] {
        assert!(serde_json::from_value::<Tool>(input).is_err());
    }
}
