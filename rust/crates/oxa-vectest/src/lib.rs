//! oxa-vectest: the test-only harness that runs the oxa golden vectors
//! (vectors/README.md) against a face implementation. It mirrors the Go
//! reference harness (`go/internal/vectest`) so every language consumes the
//! same vectors under the same normative comparison rules.
//!
//! Runner entry points: [`run`] / [`run_cross`] / [`run_stream`] (and their
//! `_in` variants taking an explicit start directory). Each returns
//! [`Outcome::Skipped`] when the repo root cannot be located (dependency
//! mode) and otherwise a [`Report`] whose `failures` list carries one entry
//! per failing vector.

mod compare;
mod cross;
mod load;
mod run;
mod stream;

pub use compare::{compare_json, compare_losses};
pub use cross::{cross_vectors_for, run_cross, run_cross_in};
pub use load::{Endpoint, LossRecord, Vector, find_repo_root, load_vectors};
pub use run::{Converter, Outcome, Report, run, run_in};
pub use stream::{StreamConverter, run_stream, run_stream_in, stream_from_ir, stream_to_ir};
