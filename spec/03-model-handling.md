# 03 — Model Handling

Status: normative. The current spec version is declared in [README.md](README.md).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in RFC 2119 and RFC 8174 when,
and only when, they appear in all capitals, as shown here.

## 1. Principle: no model knowledge

The `model` value ([01, §3.1](01-intermediate-representation.md)) passes
through verbatim by default: a face → IR converter copies it unchanged,
and an IR → face converter copies it unchanged.

oxa libraries MUST NOT contain built-in model knowledge: no model-name
mapping tables, no capability or version tables, no parsing of the model
string. The model string is opaque. A converter MUST NOT invent model
mappings — it never fabricates a model string that did not come from the
input or from the caller-supplied table of §2.

Anything model-dependent (which models support which parameters, context
windows, deprecation schedules) is the caller's concern, handled outside
oxa.

## 2. The modelmap injection point

The single, optional way to alter the model value: a caller-supplied map
from model names to model names. In the Go reference implementation this
is `modelmap.Table`; every other implementation exposes an equivalent
type.

Semantics:

- lookup is exact-match on the map's keys;
- on a miss, the identity fallback applies: the value is returned
  unchanged (no table installed is exactly an empty table);
- the map is applied to the `model` value on both directions — on the
  face → IR path and on the IR → face path;
- application is a pure string substitution; no validation of the result.

Consequence: with a non-symmetric table, converting a payload through the
IR and back does not necessarily restore the original model string (the
reverse direction misses and falls back to identity). Callers who need
symmetry supply both directions or no table.

## 3. Protocol-parameter asymmetries

The three faces disagree on parameters in ways unrelated to models:
Anthropic Messages requires `max_tokens` where Chat Completions makes it
optional; Chat Completions has `reasoning_effort` with no Anthropic
equivalent; and so on. These asymmetries are **per-face concerns**: the
concrete lists and the concrete defaults are defined by the mapping
documents 10–12. This document fixes only the principles:

1. Converters MUST succeed without model knowledge. The model identity
   never gates, short-circuits, or parameterizes a conversion.
2. When the target face REQUIRES a parameter that the source lacks, the
   mapping document MUST define the default the converter applies. The
   converter applies that default and records a loss with reason
   `degraded` (see [02](02-loss-policy.md) §3), with a `detail` naming the
   parameter and the default value applied.
3. When the source carries a parameter the target cannot express, it is
   dropped and recorded as a loss per document [02](02-loss-policy.md);
   the mapping documents pin the reason code for each case.
