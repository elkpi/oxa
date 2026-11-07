//! Runs the shared SystemOne Jev golden vectors through the Rust face via the
//! oxa-vectest harness (vectors/README.md comparison rules).

use std::path::Path;

use oxa_jev::Config;

struct VectorConverter {
    config: Config,
}

impl oxa_vectest::Converter for VectorConverter {
    fn face(&self) -> &'static str {
        "jev"
    }

    fn decode_request_wire(
        &self,
        wire: &str,
    ) -> Result<(oxa_ir::Request, Vec<oxa_ir::Loss>), String> {
        oxa_jev::decode_request_json(wire, &self.config).map_err(|err| err.to_string())
    }

    fn decode_response_wire(
        &self,
        wire: &str,
    ) -> Result<(oxa_ir::Response, Vec<oxa_ir::Loss>), String> {
        oxa_jev::decode_response_json(wire, &self.config).map_err(|err| err.to_string())
    }

    fn encode_request_ir(
        &self,
        req: &oxa_ir::Request,
    ) -> Result<(String, Vec<oxa_ir::Loss>), String> {
        oxa_jev::encode_request_json(req, &self.config).map_err(|err| err.to_string())
    }

    fn encode_response_ir(
        &self,
        resp: &oxa_ir::Response,
    ) -> Result<(String, Vec<oxa_ir::Loss>), String> {
        oxa_jev::encode_response_json(resp, &self.config).map_err(|err| err.to_string())
    }
}

#[test]
fn vectors() {
    let manifest_dir = Path::new(env!("CARGO_MANIFEST_DIR"));
    match oxa_vectest::run_in(
        manifest_dir,
        &VectorConverter {
            config: Config::default(),
        },
    ) {
        Ok(oxa_vectest::Outcome::Skipped) => {
            panic!(
                "nonstream vectors unexpectedly skipped; integration tests must locate the monorepo vectors"
            );
        }
        Ok(oxa_vectest::Outcome::Ran(report)) => {
            assert!(report.executed > 0, "nonstream vectors must execute");
            assert!(
                report.failures.is_empty(),
                "vector failures:\n{:#?}",
                report.failures
            );
        }
        Err(err) => panic!("vectest harness error: {err}"),
    }
}
