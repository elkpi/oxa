//! Runs every non-streaming cross-protocol golden vector through the three
//! Rust faces without giving any production face a dependency on another face.

use std::path::Path;

use oxa_anthropic as anthropic;
use oxa_chatcompletions as chatcompletions;
use oxa_responses as responses;
use oxa_vectest::{Converter, Outcome, run_cross_in};

macro_rules! converter {
    ($name:ident, $face:literal, $provider:ident) => {
        struct $name {
            config: $provider::Config,
        }

        impl Converter for $name {
            fn face(&self) -> &'static str {
                $face
            }

            fn decode_request_wire(
                &self,
                wire: &str,
            ) -> Result<(oxa_ir::Request, Vec<oxa_ir::Loss>), String> {
                let wire: $provider::Request =
                    serde_json::from_str(wire).map_err(|err| err.to_string())?;
                $provider::decode_request(&wire, &self.config).map_err(|err| err.to_string())
            }

            fn decode_response_wire(
                &self,
                wire: &str,
            ) -> Result<(oxa_ir::Response, Vec<oxa_ir::Loss>), String> {
                let wire: $provider::Response =
                    serde_json::from_str(wire).map_err(|err| err.to_string())?;
                $provider::decode_response(&wire, &self.config).map_err(|err| err.to_string())
            }

            fn encode_request_ir(
                &self,
                request: &oxa_ir::Request,
            ) -> Result<(String, Vec<oxa_ir::Loss>), String> {
                let (wire, losses) = $provider::encode_request(request, &self.config)
                    .map_err(|err| err.to_string())?;
                let wire = serde_json::to_string(&wire).map_err(|err| err.to_string())?;
                Ok((wire, losses))
            }

            fn encode_response_ir(
                &self,
                response: &oxa_ir::Response,
            ) -> Result<(String, Vec<oxa_ir::Loss>), String> {
                let (wire, losses) = $provider::encode_response(response, &self.config)
                    .map_err(|err| err.to_string())?;
                let wire = serde_json::to_string(&wire).map_err(|err| err.to_string())?;
                Ok((wire, losses))
            }
        }
    };
}

converter!(Anthropic, "anthropic", anthropic);
converter!(ChatCompletions, "chatcompletions", chatcompletions);
converter!(Responses, "responses", responses);

fn assert_cross(source: &dyn Converter, target: &dyn Converter) {
    let manifest_dir = Path::new(env!("CARGO_MANIFEST_DIR"));
    match run_cross_in(manifest_dir, source, target) {
        Ok(Outcome::Skipped) => panic!(
            "cross vectors unexpectedly skipped; integration tests must locate the monorepo vectors"
        ),
        Ok(Outcome::Ran(report)) => {
            assert_eq!(
                report.executed,
                2,
                "expected request and response cross vectors for {} -> {}",
                source.face(),
                target.face()
            );
            assert!(
                report.failures.is_empty(),
                "cross vector failures for {} -> {}:\n{:#?}",
                source.face(),
                target.face(),
                report.failures
            );
        }
        Err(err) => panic!(
            "cross vector harness error for {} -> {}: {err}",
            source.face(),
            target.face()
        ),
    }
}

#[test]
fn nonstream_cross_vectors() {
    let anthropic = Anthropic {
        config: anthropic::Config::default(),
    };
    let chatcompletions = ChatCompletions {
        config: chatcompletions::Config::default(),
    };
    let responses = Responses {
        config: responses::Config::default(),
    };

    assert_cross(&anthropic, &chatcompletions);
    assert_cross(&anthropic, &responses);
    assert_cross(&chatcompletions, &anthropic);
    assert_cross(&chatcompletions, &responses);
    assert_cross(&responses, &anthropic);
    assert_cross(&responses, &chatcompletions);
}
