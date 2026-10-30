use oxa_ir::inspect_web_search_call;
use serde_json::json;

#[test]
fn official_web_search_action_validation() {
    for action in [
        json!({"type": "search", "sources": [{"type": "url", "url": "https://example.com"}]}),
        json!({"type": "open_page", "url": null}),
        json!({"type": "find_in_page", "url": "https://example.com", "pattern": "q"}),
    ] {
        assert!(
            inspect_web_search_call("ws", "completed", &action, "input[0]")
                .unwrap()
                .is_empty()
        );
    }
    for action in [
        json!(null),
        json!({}),
        json!({"type": "future"}),
        json!({"type": "find_in_page", "url": "https://example.com"}),
        json!({"type": "find_in_page", "url": null, "pattern": "q"}),
        json!({"type": "search", "query": null}),
        json!({"type": "search", "sources": [{"type": "url"}]}),
    ] {
        assert!(
            inspect_web_search_call("ws", "completed", &action, "input[0]").is_err(),
            "{action}"
        );
    }
}
