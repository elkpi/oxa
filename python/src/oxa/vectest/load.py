"""Vector loading and repository-root discovery (vectors/README.md)."""

from __future__ import annotations

from dataclasses import dataclass, field
import json
import os
from pathlib import Path
from typing import Any

from oxa.ir import Loss


def find_repo_root(start_dir: Path | str | None = None) -> Path | None:
    """Walks up looking for a directory containing both vectors/ and .git/."""
    current = Path(start_dir) if start_dir is not None else Path(__file__).resolve().parent
    if current.is_file():
        current = current.parent
    current = current.resolve()

    while True:
        if (current / "vectors").is_dir() and (current / ".git").is_dir():
            return current
        if current.parent == current:
            return None
        current = current.parent


@dataclass(slots=True)
class Endpoint:
    protocol: str = ""


@dataclass(slots=True)
class Vector:
    name: str
    description: str
    mode: str
    conversion: str
    source: Endpoint
    target: Endpoint
    input: Any
    input_raw: str
    expected_ir: Any | None = None
    expected_output: Any | None = None
    expected_losses: list[Loss] = field(default_factory=list)
    tags: list[str] = field(default_factory=list)

    def is_request(self) -> bool:
        return "response" not in self.tags


def _extract_field_raw(text: str, field_name: str) -> str:
    marker = f'"{field_name}"'
    idx = text.find(marker)
    if idx == -1:
        return ""
    colon = text.find(":", idx + len(marker))
    if colon == -1:
        return ""
    start = colon + 1
    while start < len(text) and text[start].isspace():
        start += 1
    try:
        _, end = json.JSONDecoder().raw_decode(text, start)
        return text[start:end]
    except Exception:
        return ""


def load_vectors(root: Path | str, face: str, mode: str) -> list[Vector]:
    """Reads all vector files of one face and mode sorted by filename."""
    dir_path = Path(root) / "vectors" / face / mode
    if not dir_path.is_dir():
        return []

    filenames = sorted(
        entry.name for entry in dir_path.iterdir() if entry.is_file() and entry.suffix == ".json"
    )

    vectors: list[Vector] = []
    for filename in filenames:
        path = dir_path / filename
        with open(path, "r", encoding="utf-8") as f:
            text = f.read()
        data = json.loads(text)
        input_raw = _extract_field_raw(text, "input")
        if not input_raw and "input" in data:
            input_raw = json.dumps(data["input"], ensure_ascii=False)

        source_proto = data.get("source", {}).get("protocol", "")
        target_proto = data.get("target", {}).get("protocol", "")
        losses_raw = data.get("expected_losses", [])
        expected_losses = [Loss.from_dict(item) for item in losses_raw]

        vectors.append(
            Vector(
                name=data.get("name", filename[:-5]),
                description=data.get("description", ""),
                mode=data.get("mode", mode),
                conversion=data.get("conversion", ""),
                source=Endpoint(protocol=source_proto),
                target=Endpoint(protocol=target_proto),
                input=data.get("input"),
                input_raw=input_raw,
                expected_ir=data.get("expected_ir"),
                expected_output=data.get("expected_output"),
                expected_losses=expected_losses,
                tags=data.get("tags", []),
            )
        )
    return vectors


def load_cross_vectors(root: Path | str) -> list[Vector]:
    """Reads all cross-protocol vectors sorted by filename."""
    return load_vectors(root, "cross", "nonstream")
