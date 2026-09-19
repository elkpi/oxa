#!/usr/bin/env python3
"""Validates that all five language implementations (Go, Rust, TS, Python, C++)
define complete and un-drifted IR and Loss constants matching the JSON schemas:
- spec/schema/ir.schema.json
- spec/schema/loss.schema.json
"""

import ast
import json
from pathlib import Path
import re
import sys
from typing import NamedTuple

ROOT = Path(__file__).resolve().parent.parent

# V2 additions to IR enums
V2_BLOCK_TYPES = {"thinking"}
V2_DELTA_TYPES = {"thinking_delta", "signature_delta"}


class SchemaEnums(NamedTuple):
    spec_versions: set[str]
    roles: set[str]
    block_types: set[str]
    tool_choice_modes: set[str]
    stop_reasons: set[str]
    event_types: set[str]
    delta_types: set[str]
    loss_reasons: set[str]

    def tokens_for_version(self, version: str) -> set[str]:
        if version not in self.spec_versions:
            raise ValueError(f"unknown specVersion {version!r}, allowed: {self.spec_versions}")
        if version == "0.1.0":
            return (
                {"0.1.0"}
                | self.roles
                | (self.block_types - V2_BLOCK_TYPES)
                | self.tool_choice_modes
                | self.stop_reasons
                | self.event_types
                | (self.delta_types - V2_DELTA_TYPES)
                | self.loss_reasons
            )
        # Latest version (0.2.0)
        return (
            {version}
            | self.roles
            | self.block_types
            | self.tool_choice_modes
            | self.stop_reasons
            | self.event_types
            | self.delta_types
            | self.loss_reasons
        )


def load_schema_enums() -> SchemaEnums:
    ir_schema_path = ROOT / "spec" / "schema" / "ir.schema.json"
    loss_schema_path = ROOT / "spec" / "schema" / "loss.schema.json"

    with open(ir_schema_path, "r", encoding="utf-8") as f:
        ir_schema = json.load(f)
    with open(loss_schema_path, "r", encoding="utf-8") as f:
        loss_schema = json.load(f)

    defs = ir_schema["$defs"]

    spec_version_prop = defs["request"]["properties"]["specVersion"]
    if "const" in spec_version_prop:
        spec_versions = {spec_version_prop["const"]}
    else:
        spec_versions = set(spec_version_prop.get("enum", []))

    roles = set(defs["message"]["properties"]["role"]["enum"])

    block_types = {
        defs[ref.split("/")[-1]]["properties"]["type"]["const"]
        for ref in [item["$ref"] for item in defs["block"]["oneOf"]]
    }

    tool_choice_modes = {
        variant["properties"]["mode"]["const"]
        for variant in defs["toolChoice"]["oneOf"]
    }

    stop_reasons = set(defs["stopReason"]["enum"])

    event_types = {
        defs[ref.split("/")[-1]]["properties"]["type"]["const"]
        for ref in [item["$ref"] for item in defs["event"]["oneOf"]]
    }

    delta_types = {
        defs[ref.split("/")[-1]]["properties"]["type"]["const"]
        for ref in [item["$ref"] for item in defs["delta"]["oneOf"]]
    }

    loss_reasons = set(loss_schema["properties"]["reason"]["enum"])

    return SchemaEnums(
        spec_versions=spec_versions,
        roles=roles,
        block_types=block_types,
        tool_choice_modes=tool_choice_modes,
        stop_reasons=stop_reasons,
        event_types=event_types,
        delta_types=delta_types,
        loss_reasons=loss_reasons,
    )


def extract_python_constants() -> set[str]:
    path = ROOT / "python" / "src" / "oxa" / "ir" / "constants.py"
    with open(path, "r", encoding="utf-8") as f:
        tree = ast.parse(f.read(), filename=str(path))

    constants: set[str] = set()
    for stmt in tree.body:
        if isinstance(stmt, ast.AnnAssign) and isinstance(stmt.value, ast.Constant):
            if isinstance(stmt.value.value, str):
                constants.add(stmt.value.value)
        elif isinstance(stmt, ast.Assign) and isinstance(stmt.value, ast.Constant):
            if isinstance(stmt.value.value, str):
                constants.add(stmt.value.value)

    return constants


def extract_ts_constants() -> set[str]:
    path = ROOT / "ts" / "src" / "ir" / "constants.ts"
    with open(path, "r", encoding="utf-8") as f:
        content = f.read()

    # Matches export const NAME = "value" as const;
    matches = re.findall(r'export\s+const\s+\w+\s*=\s*"([^"]+)"\s*as\s*const;', content)
    return set(matches)


def extract_cpp_constants() -> set[str]:
    path = ROOT / "cpp" / "include" / "oxa" / "ir.hpp"
    with open(path, "r", encoding="utf-8") as f:
        content = f.read()

    # Matches inline constexpr std::string_view NAME = "value";
    matches = re.findall(r'inline\s+constexpr\s+std::string_view\s+\w+\s*=\s*"([^"]+)";', content)
    return set(matches)


def extract_go_constants() -> set[str]:
    go_ir_dir = ROOT / "go" / "ir"
    tokens: set[str] = set()

    for go_file in go_ir_dir.glob("*.go"):
        if go_file.name.endswith("_test.go"):
            continue
        with open(go_file, "r", encoding="utf-8") as f:
            content = f.read()

        # Matches const Name = "value" or const Name Type = "value"
        # or inside const (...) blocks: Name = "value" or Name Type = "value"
        matches = re.findall(r'(?:const\s+)?([A-Z]\w+)(?:\s+\w+)?\s*=\s*"([^"]+)"', content)
        for _, val in matches:
            tokens.add(val)

    return tokens


def extract_rust_constants() -> set[str]:
    rust_dir = ROOT / "rust" / "crates" / "oxa-ir" / "src"
    tokens: set[str] = set()

    # 1. SPEC_VERSION in codec.rs
    codec_path = rust_dir / "codec.rs"
    with open(codec_path, "r", encoding="utf-8") as f:
        m = re.search(r'pub\s+const\s+SPEC_VERSION:\s*&str\s*=\s*"([^"]+)";', f.read())
        if m:
            tokens.add(m.group(1))

    # 2. Serde renames across rust types
    for rs_file in rust_dir.glob("*.rs"):
        with open(rs_file, "r", encoding="utf-8") as f:
            content = f.read()
        renames = re.findall(r'#\[serde\(rename\s*=\s*"([^"]+)"\)\]', content)
        tokens.update(renames)

    return tokens


def main() -> int:
    expected = load_schema_enums()

    extractors = {
        "Python (python/src/oxa/ir/constants.py)": extract_python_constants,
        "TypeScript (ts/src/ir/constants.ts)": extract_ts_constants,
        "C++ (cpp/include/oxa/ir.hpp)": extract_cpp_constants,
        "Go (go/ir/*.go)": extract_go_constants,
        "Rust (rust/crates/oxa-ir/src/*.rs)": extract_rust_constants,
    }

    errors: list[str] = []

    print(f"Loaded schema IR & Loss enums (supported spec versions: {sorted(expected.spec_versions)}).")
    print("Checking multi-language constant convergence...")

    for lang, extractor in extractors.items():
        found = extractor()
        # Detect which specVersion this language declares
        matched_versions = expected.spec_versions & found
        if not matched_versions:
            errors.append(f"  ❌ {lang}: declares no supported specVersion from {expected.spec_versions}")
            continue
        lang_version = sorted(matched_versions)[-1]
        required_tokens = expected.tokens_for_version(lang_version)

        missing = required_tokens - found
        if missing:
            errors.append(f"  ❌ {lang} [v{lang_version}]: missing {len(missing)} tokens: {sorted(missing)}")
        else:
            print(f"  ✅ {lang} [v{lang_version}]: all {len(required_tokens)} tokens present.")

    if errors:
        print("\nConstant convergence check failed:")
        for err in errors:
            print(err)
        return 1

    print("\n🎉 All 5 languages have complete and synchronized IR & Loss constants for their active spec versions!")
    return 0


if __name__ == "__main__":
    sys.exit(main())
