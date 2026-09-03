//! Runs the shared anthropic golden vectors through the Rust face via the
//! oxa-vectest harness (vectors/README.md comparison rules).

use std::path::Path;

use oxa_anthropic::{
    Config, Request, Response, StreamDecoder, StreamEncoder, StreamEvent, decode_request,
    decode_response, encode_request, encode_response,
};

struct VectorConverter {
    config: Config,
}

impl oxa_vectest::Converter for VectorConverter {
    fn face(&self) -> &'static str {
        "anthropic"
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

struct StreamVectorConverter {
    config: Config,
    decoder: StreamDecoder,
    encoder: StreamEncoder,
}

impl StreamVectorConverter {
    fn new(config: Config) -> Self {
        let decoder = StreamDecoder::new(&config);
        let encoder = StreamEncoder::new(&config);
        StreamVectorConverter {
            config,
            decoder,
            encoder,
        }
    }
}

impl oxa_vectest::StreamConverter for StreamVectorConverter {
    fn face(&self) -> &'static str {
        "anthropic"
    }

    fn decode_native_event(&mut self, event: &str) -> Result<Vec<oxa_ir::Event>, String> {
        let ev: StreamEvent = serde_json::from_str(event).map_err(|err| err.to_string())?;
        self.decoder.feed(&ev).map_err(|err| err.to_string())
    }

    fn flush_decoder(&mut self) -> Result<Vec<oxa_ir::Event>, String> {
        self.decoder.flush().map_err(|err| err.to_string())
    }

    fn decoder_losses(&self) -> Vec<oxa_ir::Loss> {
        self.decoder.losses().to_vec()
    }

    fn apply_ir_event(
        &mut self,
        event: &oxa_ir::Event,
    ) -> Result<(Vec<serde_json::Value>, Vec<oxa_ir::Loss>), String> {
        let (events, losses) = self.encoder.apply(event).map_err(|err| err.to_string())?;
        let values = events
            .into_iter()
            .map(|e| serde_json::to_value(e).map_err(|err| err.to_string()))
            .collect::<Result<Vec<_>, _>>()?;
        Ok((values, losses))
    }

    fn reset_stream_vector(&mut self) {
        self.decoder = StreamDecoder::new(&self.config);
        self.encoder = StreamEncoder::new(&self.config);
    }
}

#[test]
fn stream_vectors() {
    let manifest_dir = Path::new(env!("CARGO_MANIFEST_DIR"));
    let mut conv = StreamVectorConverter::new(Config::default());
    match oxa_vectest::run_stream_in(manifest_dir, &mut conv) {
        Ok(oxa_vectest::Outcome::Skipped) => {
            panic!(
                "stream vectors unexpectedly skipped; integration tests must locate the monorepo vectors"
            );
        }
        Ok(oxa_vectest::Outcome::Ran(report)) => {
            assert!(report.executed > 0, "stream vectors must execute");
            assert!(
                report.failures.is_empty(),
                "stream vector failures:\n{:#?}",
                report.failures
            );
        }
        Err(err) => panic!("vectest harness error: {err}"),
    }
}
