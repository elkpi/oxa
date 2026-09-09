#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PYPI_TOKEN_FILE="/home/ping/.ssh/pypi.token"
CRATES_TOKEN_FILE="/home/ping/.ssh/crates.io.token"

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
    echo "=== RUNNING IN DRY-RUN MODE ==="
fi

# 1. Check tokens
if [[ ! -f "$PYPI_TOKEN_FILE" ]]; then
    echo "Error: PyPI token file not found at $PYPI_TOKEN_FILE" >&2
    exit 1
fi

if [[ ! -f "$CRATES_TOKEN_FILE" ]]; then
    echo "Error: crates.io token file not found at $CRATES_TOKEN_FILE" >&2
    exit 1
fi

PYPI_TOKEN=$(cat "$PYPI_TOKEN_FILE" | tr -d '\r\n')
CRATES_TOKEN=$(cat "$CRATES_TOKEN_FILE" | tr -d '\r\n')

# 2. Publish Python package (elkpi-oxa) to PyPI
echo "--> [1/3] Checking Python distribution (elkpi-oxa)..."
PYPI_STATUS=$(curl -s -o /dev/null -w "%{http_code}" https://pypi.org/pypi/elkpi-oxa/1.0.0/json || true)
if [[ "$PYPI_STATUS" == "200" ]]; then
    echo "    elkpi-oxa v1.0.0 is already live on PyPI, skipping upload."
else
    DIST_DIR=$(mktemp -d /tmp/oxa-pypi-dist.XXXXXX)
    trap 'rm -rf "$DIST_DIR"' EXIT

    uv build --wheel --sdist --out-dir "$DIST_DIR" "$ROOT/python"

    if [[ "$DRY_RUN" == "true" ]]; then
        echo "--> [Dry-run] Checking PyPI upload for elkpi-oxa..."
        uv publish --dry-run "$DIST_DIR"/*
    else
        echo "--> Publishing elkpi-oxa to PyPI..."
        UV_PUBLISH_TOKEN="$PYPI_TOKEN" uv publish "$DIST_DIR"/*
        echo "    Published elkpi-oxa to PyPI successfully!"
    fi
fi

# 3. Publish Rust crates to crates.io in dependency waves
publish_crate() {
    local crate_name="$1"
    local version="1.0.0"
    local manifest_path="$ROOT/rust/crates/$crate_name/Cargo.toml"
    local code
    code=$(curl -s -o /dev/null -w "%{http_code}" -A "oxa-publish (github.com/elkpi/oxa)" "https://crates.io/api/v1/crates/$crate_name/$version" || true)
    if [[ "$code" == "200" ]]; then
        echo "    $crate_name v$version is already live on crates.io, skipping."
        return 0
    fi
    echo "    Publishing $crate_name..."
    if [[ "$DRY_RUN" == "true" ]]; then
        cargo publish --dry-run --allow-dirty --manifest-path "$manifest_path"
    else
        CARGO_REGISTRY_TOKEN="$CRATES_TOKEN" cargo publish --allow-dirty --manifest-path "$manifest_path"
    fi
}

wait_for_crate() {
    local crate_name="$1"
    local version="1.0.0"
    if [[ "$DRY_RUN" == "true" ]]; then
        return 0
    fi
    echo "    Waiting for $crate_name v$version to appear on crates.io index..."
    local attempts=0
    while (( attempts < 30 )); do
        local code
        code=$(curl -s -o /dev/null -w "%{http_code}" -A "oxa-publish (github.com/elkpi/oxa)" "https://crates.io/api/v1/crates/$crate_name/$version" || true)
        if [[ "$code" == "200" ]]; then
            echo "    $crate_name v$version is live on crates.io!"
            sleep 5
            return 0
        fi
        sleep 5
        (( attempts++ ))
    done
    echo "    Warning: timeout waiting for $crate_name in index, continuing..."
}

echo "--> [2/3] Publishing Rust Wave 1 (base crates: oxa-modelmap, oxa-sse, oxa-ir)..."
publish_crate "oxa-modelmap"
publish_crate "oxa-sse"
publish_crate "oxa-ir"

wait_for_crate "oxa-modelmap"
wait_for_crate "oxa-ir"

echo "--> [3/3] Publishing Rust Wave 2 (face crates: oxa-chatcompletions, oxa-anthropic, oxa-responses)..."
publish_crate "oxa-chatcompletions"
publish_crate "oxa-anthropic"
publish_crate "oxa-responses"

wait_for_crate "oxa-chatcompletions"
wait_for_crate "oxa-anthropic"
wait_for_crate "oxa-responses"

echo "--> Publishing Rust Wave 3 (umbrella facade: elkpi-oxa)..."
publish_crate "elkpi-oxa"
wait_for_crate "elkpi-oxa"

echo "=== All packages published successfully! ==="
