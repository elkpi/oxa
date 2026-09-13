# Task 4 report — vector bindings and non-streaming faces

## Summary

Implemented the three TypeScript non-streaming protocol spokes and bound them
to the repository's lossless vector harness. Each face exposes
`decodeRequest`, `encodeRequest`, `decodeResponse`, and `encodeResponse`
with optional model mapping and `ConversionResult` losses.

- Added lossless IR request/response document codecs used by vector fixtures.
- Added Chat Completions request/response conversion.
- Added Responses request/response conversion.
- Added Anthropic Messages request/response conversion.
- Added one shared non-stream runner that filters by face and mode, parses the
  real fixtures through the lossless JSON tree, and compares output plus losses
  with `vectest`.
- Preserved opaque Chat Completions/Responses argument strings and Anthropic
  tool-input numeric spelling through the lossless JSON boundary.

No vectors or specifications changed.

## TDD evidence

- Initial Chat Completions RED: the real-vector test failed with TS2305 for the
  missing four face functions and four IR request/response codec functions.
- Responses RED: the real-vector test failed with TS2305 for the missing four
  Responses non-stream functions.
- Anthropic RED: the real-vector test failed with TS2307 because the
  non-stream Messages face did not exist.
- Each face was implemented only after its RED result, then run against its
  complete matching vector group.

## Commits

Implementation range: `7a76d39..5293195`.

- `9e15c64 feat(ts): add chat nonstream conversions`
- `4e50de6 feat(ts): add responses nonstream conversions`
- `5e73056 feat(ts): add anthropic nonstream conversions`
- `60ce6a8 test(ts): bind all nonstream vector groups`
- `5293195 style(ts): format nonstream conversions`

## Vector groups

- `vectors/chatcompletions/nonstream`: 34/34 passed.
- `vectors/responses/nonstream`: 41/41 passed.
- `vectors/anthropic/nonstream`: 30/30 passed.
- Total Task 4 face vectors: 105/105 passed.
- `make vectors`: all 125 repository vectors and 125 checks passed; schemas,
  names, and manifest are valid.

## TypeScript gates

All commands exited zero after formatting:

- `npm test`: 41 tests passed, 0 failed.
- `npm run build`
- `npm run check`
- `npm run fmt`
- `npm run generate:check`

## Residuals

- Anthropic streaming/M7 support remains intentionally unimplemented. Task 4
  exports only non-stream request/response conversion for that face.
- No npm publish or preview release was performed.
