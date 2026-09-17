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


class SchemaEnums(NamedTuple):
    spec_version: str
    roles: set[str]
    block_types: set[str]
    tool_choice_modes: set[str]
    stop_reasons: set[str]
    event_types: set[str]
    delta_types: set[str]
    loss_reasons: set[str]

    @property
    def all_tokens(self) -> set[str]:
        return (
            {self.spec_version}
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

    spec_version = defs["request"]["properties"]["specVersion"]["const"]
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
        spec_version=spec_version,
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
    required_tokens = expected.all_tokens

    extractors = {
        "Python (python/src/oxa/ir/constants.py)": extract_python_constants,
        "TypeScript (ts/src/ir/constants.ts)": extract_ts_constants,
        "C++ (cpp/include/oxa/ir.hpp)": extract_cpp_constants,
        "Go (go/ir/*.go)": extract_go_constants,
        "Rust (rust/crates/oxa-ir/src/*.rs)": extract_rust_constants,
    }

    errors: list[str] = []

    print(f"Loaded {len(required_tokens)} normative IR & Loss tokens from schemas.")
    print("Checking multi-language constant convergence...")

    for lang, extractor in extractors.items():
        found = extractor()
        missing = required_tokens - found
        if missing:
            errors.append(f"  ❌ {lang}: missing {len(missing)} tokens: {sorted(missing)}")
        else:
            print(f"  ✅ {lang}: all {len(required_tokens)} tokens present.")

    if errors:
        print("\nConstant convergence check failed:")
        for err in errors:
            print(err)
        return 1

    print("\n🎉 All 5 languages have 100% complete and synchronized IR & Loss constants!")
    return 0


if __name__ == "__main__":
    sys.exit(main())
