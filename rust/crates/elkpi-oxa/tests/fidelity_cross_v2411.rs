use oxa_chatcompletions::encode_request as encode_cc;
use oxa_ir::{Block, LossReason, Request};
use oxa_responses::encode_request as encode_re;

#[test]
fn test_cross_face_redacted_losses() {
    let req = Request {
        model: "claude-opus-5-5".to_string(),
        system: Vec::new(),
        messages: vec![
            oxa_ir::Message {
                role: oxa_ir::Role::User,
                content: vec![Block::Text {
                    text: "Question.".to_string(),
                    citations: None,
                    native: Vec::new(),
                }],
            },
            oxa_ir::Message {
                role: oxa_ir::Role::Assistant,
                content: vec![
                    Block::RedactedThinking {
                        data: "OPAQUE_TEST".to_string(),
                        native: Vec::new(),
                    },
                    Block::Text {
                        text: "Answer.".to_string(),
                        citations: None,
                        native: Vec::new(),
                    },
                ],
            },
            oxa_ir::Message {
                role: oxa_ir::Role::User,
                content: vec![Block::Text {
                    text: "Followup.".to_string(),
                    citations: None,
                    native: Vec::new(),
                }],
            },
        ],
        tools: None,
        tool_choice: None,
        params: None,
        metadata: None,
        native: Vec::new(),
    };

    let cc_config = oxa_chatcompletions::Config::default();
    let (_, cc_losses) = encode_cc(&req, &cc_config).expect("encode cc");
    assert!(
        cc_losses
            .iter()
            .any(|l| l.reason == LossReason::UnsupportedSemantic
                && l.detail.contains("encrypted thinking"))
    );

    let re_config = oxa_responses::Config::default();
    let (_, re_losses) = encode_re(&req, &re_config).expect("encode re");
    assert!(
        re_losses
            .iter()
            .any(|l| l.reason == LossReason::UnsupportedSemantic
                && l.detail.contains("encrypted thinking"))
    );
}
