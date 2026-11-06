use oxa_ir::{Request, validate_native_request};
use serde_json::{Value, json};

fn request(native: Value, compaction: bool) -> Request {
    let change =
        json!({"op":"add", "ref":{"type":"tool_reference","name":"read"}, "native":native});
    let content = if compaction {
        json!([{"type":"compaction","tool_changes":[change.clone()]}])
    } else {
        json!([{"type":"text","text":"go"}])
    };
    let groups = if compaction {
        json!([])
    } else {
        json!([{"message_index":1,"content_index":-1,"parts":[{"type":"tool_change","change":change}]}])
    };
    serde_json::from_value(json!({"model":"m","messages":[{"role":"user","content":content}],"tool_change_groups":groups})).unwrap()
}

#[test]
fn change_native_conflicts_and_indices() {
    let cache =
        json!({"source":"anthropic","kind":"cache_control","value":"{\"type\":\"ephemeral\"}"});
    let defer = json!({"source":"anthropic","kind":"defer_loading","value":"true"});
    let future = json!({"source":"future","kind":"future","value":"{}"});
    let invalid =
        json!({"source":"anthropic","kind":"cache_control","value":"{\"type\":\"secret-value\"}"});
    for compaction in [false, true] {
        let path = if compaction {
            "messages[0].content[0].tool_changes[0]"
        } else {
            "tool_change_groups[0].parts[0].change"
        };
        let err = validate_native_request(&request(json!([cache, defer]), compaction))
            .unwrap_err()
            .to_string();
        assert!(err.contains(path), "{err}");
        validate_native_request(&request(json!([cache]), compaction)).unwrap();
        let err = validate_native_request(&request(json!([future, invalid]), compaction))
            .unwrap_err()
            .to_string();
        assert!(err.contains(&format!("{path}.native[1]")), "{err}");
        assert!(!err.contains("secret-value"), "{err}");
    }
}
