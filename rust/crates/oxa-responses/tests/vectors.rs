//! Runs the shared Responses golden vectors through the Rust face.

use std::path::Path;

use oxa_responses::{
    Config, StreamDecoder, StreamEncoder, decode_request_json, encode_request, encode_response,
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
        oxa_responses::decode_request_json(wire, &self.config).map_err(|err| err.to_string())
    }

    fn decode_response_wire(
        &self,
        wire: &str,
    ) -> Result<(oxa_ir::Response, Vec<oxa_ir::Loss>), String> {
        oxa_responses::decode_response_json(wire, &self.config).map_err(|err| err.to_string())
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

#[test]
fn empty_additional_tools_is_a_no_op() {
    let existing = r#"{"type":"function","name":"existing","parameters":{"type":"object"}}"#;
    let with_empty = format!(
        r#"{{"model":"m","input":[{{"type":"additional_tools","role":"developer","tools":[]}},{{"type":"message","role":"user","content":"hello"}}],"tools":[{existing}]}}"#
    );
    let without_empty = format!(
        r#"{{"model":"m","input":[{{"type":"message","role":"user","content":"hello"}}],"tools":[{existing}]}}"#
    );

    let (request, losses) =
        decode_request_json(&with_empty, &Config::default()).expect("decode with empty inventory");
    let (expected, _) = decode_request_json(&without_empty, &Config::default())
        .expect("decode without empty inventory");

    assert_eq!(request, expected);
    assert_eq!(losses.len(), 1);
    assert_eq!(losses[0].path, "input[0].tools");
    assert_eq!(losses[0].field, "tools");
    assert_eq!(losses[0].reason, oxa_ir::LossReason::UnmappedField);
}

#[test]
fn empty_additional_tools_does_not_split_runs() {
    let empty = r#"{"type":"additional_tools","role":"developer","tools":[]}"#;
    let user = r#"{"role":"user","content":"hi"}"#;
    let call_a = r#"{"type":"function_call","call_id":"a","name":"a","arguments":"{}"}"#;
    let call_b = r#"{"type":"function_call","call_id":"b","name":"b","arguments":"{}"}"#;
    let result_a = r#"{"type":"function_call_output","call_id":"a","output":"a"}"#;
    let result_b = r#"{"type":"function_call_output","call_id":"b","output":"b"}"#;

    let with_empty = format!(
        r#"{{"model":"m","input":[{user},{call_a},{empty},{call_b},{result_a},{result_b}]}}"#
    );
    let without_empty =
        format!(r#"{{"model":"m","input":[{user},{call_a},{call_b},{result_a},{result_b}]}}"#);
    let (with, losses) =
        decode_request_json(&with_empty, &Config::default()).expect("decode with split");
    let (without, _) =
        decode_request_json(&without_empty, &Config::default()).expect("decode baseline");
    assert_eq!(with, without);
    assert_eq!(losses.len(), 1);

    let with_split_results =
        format!(r#"{{"model":"m","input":[{user},{call_a},{result_a},{empty},{result_b}]}}"#);
    let results_baseline =
        format!(r#"{{"model":"m","input":[{user},{call_a},{result_a},{result_b}]}}"#);
    let (with_results, losses) =
        decode_request_json(&with_split_results, &Config::default()).expect("decode split results");
    let (baseline_results, _) =
        decode_request_json(&results_baseline, &Config::default()).expect("decode result baseline");
    assert_eq!(with_results, baseline_results);
    assert_eq!(losses.len(), 1);
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
        "responses"
    }

    fn decode_native_event(&mut self, event: &str) -> Result<Vec<oxa_ir::Event>, String> {
        self.decoder.feed_json(event).map_err(|err| err.to_string())
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
