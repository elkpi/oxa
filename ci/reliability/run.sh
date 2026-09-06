#!/usr/bin/env bash
set -euo pipefail

ROOT=${OXA_RELIABILITY_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
ROOT=$(cd "$ROOT" && pwd)
CORPUS="$ROOT/testdata/stream-fragment-corpus.json"
TMP=$(mktemp -d "${TMPDIR:-/tmp}/oxa-reliability.XXXXXX")
trap 'rm -rf "$TMP"' EXIT

[[ -f "$CORPUS" ]]

printf '%s\n' 'reliability replay: Go'
(
    cd "$ROOT/go"
    go test ./internal/vectest -run '^TestFragmentCorpus$' -count=1
)

printf '%s\n' 'reliability replay: Python'
(
    cd "$ROOT/python"
    PYTHONPATH=src python3 -m unittest tests/test_fragment_corpus.py
)

printf '%s\n' 'reliability replay: Rust'
(
    cd "$ROOT/rust"
    cargo test -p oxa-vectest stream_fragment_corpus -- --nocapture
)

printf '%s\n' 'reliability replay: C++'
cmake -S "$ROOT/cpp" -B "$TMP/cpp-build" -DCMAKE_BUILD_TYPE=Debug
cmake --build "$TMP/cpp-build" --target test_stream_corpus --parallel
ctest --test-dir "$TMP/cpp-build" -R '^test_stream_corpus$' --output-on-failure

printf '%s\n' 'reliability replay: all languages passed'
