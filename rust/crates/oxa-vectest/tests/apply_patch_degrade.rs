use oxa_anthropic::StreamEncoder as AntStreamEncoder;
use oxa_chatcompletions::StreamEncoder as CCStreamEncoder;
use oxa_ir::{
    ApplyPatchCallStatus, ApplyPatchOperation, ApplyPatchResultStatus, Block, Delta, Event,
    StopReason, Usage,
};

#[test]
fn test_apply_patch_stream_degradation() {
    let events = vec![
        Event::MessageStart {
            id: "r".into(),
            model: "m".into(),
        },
        Event::ContentBlockStart {
            index: 0,
            block: Block::ApplyPatchToolResult {
                tool_use_id: "c".into(),
                status: ApplyPatchResultStatus::Completed,
                output: None,
            },
        },
        Event::ContentBlockStop { index: 0 },
        Event::MessageDelta {
            stop_reason: StopReason::EndTurn,
            stop_sequence: None,
            usage: Usage {
                input_tokens: 1,
                output_tokens: 2,
                cache_read_input_tokens: None,
                cache_creation_input_tokens: None,
                input_tokens_details: None,
                output_tokens_details: None,
            },
        },
        Event::MessageDone {},
    ];

    let mut cc = CCStreamEncoder::new(&oxa_chatcompletions::Config::default());
    for ev in &events {
        assert!(cc.apply(ev).is_ok());
    }

    let mut ant = AntStreamEncoder::new(&oxa_anthropic::Config::default());
    for ev in &events {
        assert!(ant.apply(ev).is_ok());
    }

    // Delta on atomic block must return error
    let delta_events = vec![
        Event::MessageStart {
            id: "r".into(),
            model: "m".into(),
        },
        Event::ContentBlockStart {
            index: 0,
            block: Block::ApplyPatchToolUse {
                id: "c".into(),
                status: ApplyPatchCallStatus::Completed,
                operation: ApplyPatchOperation::DeleteFile { path: "a".into() },
            },
        },
        Event::ContentBlockDelta {
            index: 0,
            delta: Delta::CustomToolInputDelta {
                text: "\"\"".into(),
            },
        },
    ];

    let mut cc2 = CCStreamEncoder::new(&oxa_chatcompletions::Config::default());
    let mut saw_err = false;
    for ev in &delta_events {
        if cc2.apply(ev).is_err() {
            saw_err = true;
            break;
        }
    }
    assert!(saw_err, "expected error on delta in CC stream");

    let mut ant2 = AntStreamEncoder::new(&oxa_anthropic::Config::default());
    saw_err = false;
    for ev in &delta_events {
        if ant2.apply(ev).is_err() {
            saw_err = true;
            break;
        }
    }
    assert!(saw_err, "expected error on delta in Anthropic stream");
}
