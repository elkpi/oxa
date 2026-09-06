#!/usr/bin/env bash
set -euo pipefail

ROOT=${OXA_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
ROOT=$(cd "$ROOT" && pwd)
VERSION=${OXA_VERSION:-1.0.0}
CONSUMER_ONLY=${CONSUMER_ONLY:-all}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/oxa-consumers.XXXXXX")
trap 'rm -rf "$TMP"' EXIT

if [[ ! -f "$ROOT/go/go.mod" || ! -f "$ROOT/rust/Cargo.toml" || ! -f "$ROOT/python/pyproject.toml" || ! -f "$ROOT/cpp/CMakeLists.txt" ]]; then
    echo "consumer smoke: repository root is missing a language package" >&2
    exit 1
fi

should_run() {
    [[ "$CONSUMER_ONLY" == all || "$CONSUMER_ONLY" == "$1" ]]
}

run_go_consumer() {
    command -v go >/dev/null
    local proxy="$TMP/go-proxy"
    local consumer="$TMP/go-consumer"
    mkdir -p "$proxy/github.com/elkpi/oxa/go/@v" "$consumer"
    python3 - "$ROOT" "$proxy" "$VERSION" <<'PY'
import json
import pathlib
import subprocess
import sys
import zipfile

root = pathlib.Path(sys.argv[1])
proxy = pathlib.Path(sys.argv[2])
version = sys.argv[3]
if not version.startswith("v"):
    version = "v" + version
module = "github.com/elkpi/oxa/go"
files = subprocess.check_output(
    ["git", "-C", str(root), "ls-files", "go"], text=True
).splitlines()
mod = (root / "go/go.mod").read_bytes()
(proxy / "github.com/elkpi/oxa/go/@v" / f"{version}.mod").write_bytes(mod)
(proxy / "github.com/elkpi/oxa/go/@v" / f"{version}.info").write_text(
    json.dumps({"Version": version, "Time": "2026-09-06T00:00:00Z"})
)
zip_path = proxy / "github.com/elkpi/oxa/go/@v" / f"{version}.zip"
with zipfile.ZipFile(zip_path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
    prefix = f"{module}@{version}/"
    for file_name in files:
        path = root / file_name
        archive.write(path, prefix + file_name.removeprefix("go/"))
PY
    cp "$ROOT/ci/consumers/go/main.go" "$consumer/main.go"
    (
        cd "$consumer"
        go mod init oxa-consumer
        GOPROXY="file://${proxy}|https://proxy.golang.org" GOSUMDB=off \
            go get "github.com/elkpi/oxa/go@v${VERSION#v}"
        GOPROXY="file://${proxy}|https://proxy.golang.org" GOSUMDB=off \
            go run .
    )
}

run_python_consumer() {
    local python=${PYTHON:-python3}
    command -v "$python" >/dev/null
    local build_env="$TMP/python-build-env"
    local dist="$TMP/python-dist"
    "$python" -m venv "$build_env"
    "$build_env/bin/python" -m pip install --disable-pip-version-check --quiet build
    mkdir -p "$dist"
    (
        cd "$ROOT/python"
        "$build_env/bin/python" -m build --wheel --sdist --outdir "$dist"
    )
    local wheel
    local sdist
    wheel=$(find "$dist" -maxdepth 1 -type f -name '*.whl' -print -quit)
    sdist=$(find "$dist" -maxdepth 1 -type f -name '*.tar.gz' -print -quit)
    [[ -n "$wheel" && -n "$sdist" ]]
    for kind in wheel sdist; do
        local artifact=$wheel
        [[ "$kind" == sdist ]] && artifact=$sdist
        local env="$TMP/python-$kind-env"
        "$python" -m venv "$env"
        "$env/bin/python" -m pip install --disable-pip-version-check --quiet --no-deps "$artifact"
        (
            cd "$TMP"
            "$env/bin/python" "$ROOT/ci/consumers/python/smoke.py"
        )
    done
}

run_rust_consumer() {
    command -v cargo >/dev/null
    local target="$TMP/cargo-target"
    local crates="$TMP/rust-crates"
    mkdir -p "$crates"
    for crate in oxa-ir oxa-modelmap oxa-chatcompletions oxa-anthropic oxa-responses oxa-sse; do
        local manifest="$ROOT/rust/crates/$crate/Cargo.toml"
        local package_list="$TMP/$crate.package-list"
        (
            cd "$ROOT/rust"
            CARGO_TARGET_DIR="$target" cargo package \
                --manifest-path "$manifest" --allow-dirty --no-verify --list
        ) > "$package_list"
        grep -qx 'Cargo.toml' "$package_list"
        grep -q '^src/' "$package_list"
        mkdir -p "$crates/$crate/src"
        cp -R "$ROOT/rust/crates/$crate/src/." "$crates/$crate/src/"
        sed \
            -e 's/edition.workspace = true/edition = "2024"/' \
            -e 's/version.workspace = true/version = "1.0.0"/' \
            -e 's/license.workspace = true/license = "Apache-2.0"/' \
            -e 's/repository.workspace = true/repository = "https:\/\/github.com\/elkpi\/oxa"/' \
            -e 's/oxa-ir = { workspace = true }/oxa-ir = { version = "1.0.0", path = "..\/oxa-ir" }/' \
            -e 's/oxa-modelmap = { workspace = true }/oxa-modelmap = { version = "1.0.0", path = "..\/oxa-modelmap" }/' \
            -e 's/serde = { workspace = true }/serde = { version = "1", features = ["derive"] }/' \
            -e 's/serde_json = { workspace = true }/serde_json = { version = "1", features = ["raw_value"] }/' \
            -e '/^\[dev-dependencies\]/,$d' \
            "$manifest" > "$crates/$crate/Cargo.toml"
    done
    mkdir -p "$TMP/rust-consumer/src"
    cp "$ROOT/ci/consumers/rust/src/main.rs" "$TMP/rust-consumer/src/main.rs"
    sed "s#path = \"../oxa-chatcompletions\"#path = \"../rust-crates/oxa-chatcompletions\"#; s#path = \"../oxa-ir\"#path = \"../rust-crates/oxa-ir\"#" \
        "$ROOT/ci/consumers/rust/Cargo.toml" > "$TMP/rust-consumer/Cargo.toml"
    (
        cd "$TMP/rust-consumer"
        CARGO_TARGET_DIR="$target" cargo run
    )
}

run_cpp_consumer() {
    command -v cmake >/dev/null
    local build="$TMP/cpp-build"
    local prefix="$TMP/cpp-prefix"
    local consumer_build="$TMP/cpp-consumer-build"
    cmake -S "$ROOT/cpp" -B "$build" -DCMAKE_BUILD_TYPE=Debug
    cmake --build "$build" --parallel
    cmake --install "$build" --prefix "$prefix"
    cmake -S "$ROOT/ci/consumers/cpp" -B "$consumer_build" \
        -DCMAKE_BUILD_TYPE=Debug -DCMAKE_PREFIX_PATH="$prefix"
    cmake --build "$consumer_build" --parallel
    env -u OXA_ROOT "$consumer_build/oxa_consumer"
}

if should_run go; then
    echo "consumer smoke: Go"
    run_go_consumer
fi
if should_run python; then
    echo "consumer smoke: Python wheel and sdist"
    run_python_consumer
fi
if should_run rust; then
    echo "consumer smoke: Rust packages"
    run_rust_consumer
fi
if should_run cpp; then
    echo "consumer smoke: C++ installed package"
    run_cpp_consumer
fi

echo "consumer smoke: all selected consumers passed"
