# oxa — Python Reference Implementation

Pure, in-process protocol-conversion library for OpenAI Chat Completions,
OpenAI Responses, and Anthropic Messages in Python.

Like the Go and Rust implementations, oxa in Python is not a proxy, HTTP
client, router, retry layer, authentication service, or model capability
database. It provides deterministic, pure conversions between protocol wire
structures and oxa's shared Intermediate Representation (IR).

## Architecture

- **Hub-and-spoke**: Spoke packages (`oxa.openai.chatcompletions`,
  `oxa.openai.responses`, `oxa.anthropic.messages`) implement only `face → IR`
  and `IR → face`. No direct face-to-face conversions.
- **Zero runtime dependencies**: Pure Python standard library only
  (`dataclasses`, `typing`, `json`, `enum`, etc.).
- **Opaque tool inputs (INV-1)**: Tool call arguments and streaming delta
  fragments remain unparsed raw JSON text.
- **Shared Golden Vectors**: Conforms to the exact same shared `vectors/` golden
  suite as Go and Rust.

## Installation

```bash
pip install elkpi-oxa
```

Import in Python:

```python
import oxa
from oxa.openai.chatcompletions import decode_request
```

## Development

The project uses [`uv`](https://docs.astral.sh/uv/) for development and
environment management:

```bash
cd python

# Sync virtualenv and development dependencies
uv sync

# Run tests
uv run pytest

# Type check
uv run mypy

# Lint & format check
uv run ruff check .
uv run ruff format --check .
```

## Vectors Location Convention

Test harnesses locate the shared `vectors/` suite by walking up parent
directories from `python/` until finding a directory containing both `vectors/`
and `.git/`. Tests skip cleanly if the repository root is absent (e.g. when
building a standalone package archive).

## 原始 JSON 入口

各 face 导出 `decode_request_json(raw_text, table=None)` /
`decode_response_json(raw_text, table=None)`；stream decoder 支持 `feed_json(raw_text)`。
旧 dict 接口仍可用；若同时提供 `raw_text`，它必须对应该 dict。参数原文按完整对象/数组
路径提取，不再通过 tool ID 的字符串位置关联。空 loss 列表不是业务准入证明。
