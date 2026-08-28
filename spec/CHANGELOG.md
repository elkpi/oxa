# Spec changelog

Version history of the oxa specification itself, independent of the
implementations. The spec follows [Semantic Versioning](https://semver.org/);
precedence between spec, vectors, and schemas is defined in
[README.md](README.md#source-of-truth-precedence).

## 0.1.0 - 2026-08-28

Initial spec core (00–03, IR and loss schemas).

### Added

- `spec/00` — scope, non-goals, stream-equivalence principle, hub-and-spoke
  architecture.
- `spec/01` — the intermediate representation: request, message, block,
  response, and event types; invariants INV-1 through INV-9.
- `spec/02` — loss policy: the loss record, reason codes, the error/loss
  boundary, streaming rules.
- `spec/03` — model handling: verbatim pass-through, the modelmap
  injection point, protocol-parameter asymmetry principles.
- `spec/schema/ir.schema.json` and `spec/schema/loss.schema.json`
  (JSON Schema 2020-12).
