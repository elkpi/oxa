#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PYPI_TOKEN_FILE="${PYPI_TOKEN_FILE:-/home/ping/.ssh/pypi.token}"
CRATES_TOKEN_FILE="${CRATES_TOKEN_FILE:-/home/ping/.ssh/crates.io.token}"

DRY_RUN=false
TARGET_ALL=true
TARGET_PYTHON=false
TARGET_RUST=false

for arg in "$@"; do
    case "$arg" in
        --dry-run)
            DRY_RUN=true
            ;;
        --python-only)
            TARGET_ALL=false
            TARGET_PYTHON=true
            ;;
        --rust-only|--crates-only)
            TARGET_ALL=false
            TARGET_RUST=true
            ;;
        *)
            echo "Unknown argument: $arg" >&2
            echo "Usage: $0 [--dry-run] [--python-only] [--rust-only]" >&2
            exit 1
            ;;
    esac
done

if [[ "$TARGET_ALL" == "true" ]]; then
    TARGET_PYTHON=true
    TARGET_RUST=true
fi

if [[ "$DRY_RUN" == "true" ]]; then
    echo "=== RUNNING IN DRY-RUN MODE ==="
fi

# 1. Resolve tokens
PYPI_TOKEN="${PYPI_TOKEN:-${UV_PUBLISH_TOKEN:-}}"
CRATES_TOKEN="${CARGO_REGISTRY_TOKEN:-}"

if [[ "$TARGET_PYTHON" == "true" ]]; then
    if [[ -z "$PYPI_TOKEN" && -f "$PYPI_TOKEN_FILE" ]]; then
        PYPI_TOKEN=$(cat "$PYPI_TOKEN_FILE" | tr -d '\r\n')
    fi
    # In GitHub Actions without a token, uv publish can use PyPI Trusted Publishing (OIDC).
    # Locally, a token or token file is required unless dry-run.
    if [[ -z "$PYPI_TOKEN" && -z "${GITHUB_ACTIONS:-}" && "$DRY_RUN" != "true" ]]; then
        echo "Error: PyPI token not found (set PYPI_TOKEN/UV_PUBLISH_TOKEN or create $PYPI_TOKEN_FILE)" >&2
        exit 1
    fi
fi

if [[ "$TARGET_RUST" == "true" ]]; then
    if [[ -z "$CRATES_TOKEN" && -f "$CRATES_TOKEN_FILE" ]]; then
        CRATES_TOKEN=$(cat "$CRATES_TOKEN_FILE" | tr -d '\r\n')
    fi
    if [[ -z "$CRATES_TOKEN" && "$DRY_RUN" != "true" ]]; then
        echo "Error: crates.io token not found (set CARGO_REGISTRY_TOKEN or create $CRATES_TOKEN_FILE)" >&2
        exit 1
    fi
fi

# 2. Publish Python package (elkpi-oxa) to PyPI
if [[ "$TARGET_PYTHON" == "true" ]]; then
    PYTHON_VERSION=$(grep '^version = ' "$ROOT/python/pyproject.toml" | head -n1 | sed -E 's/version = "(.*)"/\1/')
    echo "--> Checking Python distribution (elkpi-oxa v$PYTHON_VERSION)..."
    PYPI_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "https://pypi.org/pypi/elkpi-oxa/${PYTHON_VERSION}/json" || true)
    if [[ "$PYPI_STATUS" == "200" ]]; then
        echo "    elkpi-oxa v$PYTHON_VERSION is already live on PyPI, skipping upload."
    else
        DIST_DIR=$(mktemp -d /tmp/oxa-pypi-dist.XXXXXX)
        trap 'rm -rf "$DIST_DIR"' EXIT

        uv build --wheel --sdist --out-dir "$DIST_DIR" "$ROOT/python"

        if [[ "$DRY_RUN" == "true" ]]; then
            echo "--> [Dry-run] Checking PyPI upload for elkpi-oxa..."
            if [[ -n "$PYPI_TOKEN" ]]; then
                UV_PUBLISH_TOKEN="$PYPI_TOKEN" uv publish --dry-run "$DIST_DIR"/*
            else
                uv publish --dry-run "$DIST_DIR"/*
            fi
        else
            echo "--> Publishing elkpi-oxa v$PYTHON_VERSION to PyPI..."
            if [[ -n "$PYPI_TOKEN" ]]; then
                UV_PUBLISH_TOKEN="$PYPI_TOKEN" uv publish "$DIST_DIR"/*
            else
                uv publish "$DIST_DIR"/*
            fi
            echo "    Published elkpi-oxa v$PYTHON_VERSION to PyPI successfully!"
        fi
        rm -rf "$DIST_DIR"
        trap - EXIT
    fi
fi

# 3. Publish Rust crates to crates.io in dependency waves
if [[ "$TARGET_RUST" == "true" ]]; then
    RUST_VERSION=$(grep -A 5 '\[workspace\.package\]' "$ROOT/rust/Cargo.toml" | grep '^version = ' | head -n1 | sed -E 's/version = "(.*)"/\1/')
    echo "--> Publishing Rust crates (version $RUST_VERSION)..."

    publish_crate() {
        local crate_name="$1"
        local manifest_path="$ROOT/rust/crates/$crate_name/Cargo.toml"
        local code
        code=$(curl -s -o /dev/null -w "%{http_code}" -A "oxa-publish (github.com/elkpi/oxa)" "https://crates.io/api/v1/crates/$crate_name/$RUST_VERSION" || true)
        if [[ "$code" == "200" ]]; then
            echo "    $crate_name v$RUST_VERSION is already live on crates.io, skipping."
            return 0
        fi
        echo "    Publishing $crate_name v$RUST_VERSION..."
        if [[ "$DRY_RUN" == "true" ]]; then
            cargo publish --dry-run --allow-dirty --manifest-path "$manifest_path"
        else
            CARGO_REGISTRY_TOKEN="$CRATES_TOKEN" cargo publish --allow-dirty --manifest-path "$manifest_path"
        fi
    }

    wait_for_crate() {
        local crate_name="$1"
        if [[ "$DRY_RUN" == "true" ]]; then
            return 0
        fi
        echo "    Waiting for $crate_name v$RUST_VERSION to appear on crates.io index..."
        local attempts=0
        while (( attempts < 30 )); do
            local code
            code=$(curl -s -o /dev/null -w "%{http_code}" -A "oxa-publish (github.com/elkpi/oxa)" "https://crates.io/api/v1/crates/$crate_name/$RUST_VERSION" || true)
            if [[ "$code" == "200" ]]; then
                echo "    $crate_name v$RUST_VERSION is live on crates.io!"
                sleep 5
                return 0
            fi
            sleep 5
            (( attempts++ ))
        done
        echo "    Warning: timeout waiting for $crate_name in index, continuing..."
    }

    echo "--> [1/3] Publishing Rust Wave 1 (base crates: oxa-modelmap, oxa-sse, oxa-ir)..."
    publish_crate "oxa-modelmap"
    publish_crate "oxa-sse"
    publish_crate "oxa-ir"

    wait_for_crate "oxa-modelmap"
    wait_for_crate "oxa-ir"

    echo "--> [2/3] Publishing Rust Wave 2 (face crates: oxa-chatcompletions, oxa-anthropic, oxa-responses)..."
    publish_crate "oxa-chatcompletions"
    publish_crate "oxa-anthropic"
    publish_crate "oxa-responses"

    wait_for_crate "oxa-chatcompletions"
    wait_for_crate "oxa-anthropic"
    wait_for_crate "oxa-responses"

    echo "--> [3/3] Publishing Rust Wave 3 (umbrella facade: elkpi-oxa)..."
    publish_crate "elkpi-oxa"
    wait_for_crate "elkpi-oxa"
fi

echo "=== Selected packages processed successfully! ==="
