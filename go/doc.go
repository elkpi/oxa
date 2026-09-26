// Package oxa provides pure, in-process protocol conversion libraries for
// OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages.
//
// It is not a proxy, HTTP client, router, retry layer, authentication service,
// or model capability database.
//
// # Architecture
//
// oxa uses a hub-and-spoke conversion architecture centered on a neutral
// Intermediate Representation (IR):
//
//   - Face -> IR: Wire protocols decode into neutral IR types ([ir.Request], [ir.Response], [ir.Event]).
//   - IR -> Face: Neutral IR types encode back into provider-specific wire payloads.
//
// Direct face-to-face conversions do not exist. Any semantic gaps between
// protocols produce ordered fidelity loss records ([ir.Loss]), while
// structural or lifecycle violations return explicit errors.
//
// # Packages
//
// The oxa Go module is organized into the following packages:
//
//   - [github.com/elkpi/oxa/go/v2/ir]: Owns the face-neutral intermediate representation, streaming event grammar, and loss types.
//   - [github.com/elkpi/oxa/go/v2/openai/chatcompletions]: Implements the OpenAI Chat Completions face (decoders, encoders, and streaming).
//   - [github.com/elkpi/oxa/go/v2/openai/responses]: Implements the OpenAI Responses API face.
//   - [github.com/elkpi/oxa/go/v2/anthropic/messages]: Implements the Anthropic Messages API face.
//   - [github.com/elkpi/oxa/go/v2/modelmap]: Implements optional, caller-supplied model name translation.
//   - [github.com/elkpi/oxa/go/v2/sse]: Standalone byte-level Server-Sent Events (SSE) framing adapter.
package oxa

// Version is the current release version of the oxa Go reference implementation.
const Version = "2.0.0"
