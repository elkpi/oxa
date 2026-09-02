//! Runs the shared Responses golden vectors through the Rust face.

use oxa_responses::{
    Config, Request, Response, decode_request, decode_response, encode_request, encode_response,
};

struct VectorConverter {
    config: Config,
}

impl oxa_vectest::Converter for VectorConverter {
    fn face(&self) -> &'static str {
        "responses"
    }

    fn decode_request_wire(
        &self,
        wire: &str,
    ) -> Result<(oxa_ir::Request, Vec<oxa_ir::Loss>), String> {
        let wire: Request = serde_json::from_str(wire).map_err(|err| err.to_string())?;
        decode_request(&wire, &self.config).map_err(|err| err.to_string())
    }

    fn decode_response_wire(
        &self,
        wire: &str,
    ) -> Result<(oxa_ir::Response, Vec<oxa_ir::Loss>), String> {
        let wire: Response = serde_json::from_str(wire).map_err(|err| err.to_string())?;
        decode_response(&wire, &self.config).map_err(|err| err.to_string())
    }

    fn encode_request_ir(
        &self,
        req: &oxa_ir::Request,
    ) -> Result<(String, Vec<oxa_ir::Loss>), String> {
        let (wire, losses) = encode_request(req, &self.config).map_err(|err| err.to_string())?;
        let out = serde_json::to_string(&wire).map_err(|err| err.to_string())?;
        Ok((out, losses))
    }

    fn encode_response_ir(
        &self,
        resp: &oxa_ir::Response,
    ) -> Result<(String, Vec<oxa_ir::Loss>), String> {
        let (wire, losses) = encode_response(resp, &self.config).map_err(|err| err.to_string())?;
        let out = serde_json::to_string(&wire).map_err(|err| err.to_string())?;
        Ok((out, losses))
    }
}

#[test]
fn vectors() {
    match oxa_vectest::run(&VectorConverter {
        config: Config::default(),
    }) {
        Ok(oxa_vectest::Outcome::Skipped) => {
            eprintln!("repo root not found; vector tests skipped (dependency mode)");
        }
        Ok(oxa_vectest::Outcome::Ran(report)) => {
            assert!(
                report.failures.is_empty(),
                "vector failures:\n{:#?}",
                report.failures
            );
        }
        Err(err) => panic!("vectest harness error: {err}"),
    }
}
