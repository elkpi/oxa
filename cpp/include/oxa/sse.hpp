// Standalone byte-level Server-Sent Events (SSE) frame adapter.
//
// Implements the SSE framing boundary specified in spec/20-streaming-semantics.md
// §6 (N-S-8). It knows nothing about JSON, the oxa IR, or any provider face:
// it decodes and encodes SSE frames as opaque bytes.

#pragma once

#include <string>
#include <string_view>
#include <vector>

namespace oxa::sse {

// One decoded SSE frame: the event name (empty when absent) and the joined
// data payload (multiple data: lines are joined with '\n').
struct Frame {
    std::string event;
    std::string data;

    bool operator==(const Frame& other) const = default;
};

// Decodes every complete frame in `input`. A final frame without a trailing
// blank line is still emitted (matching the reference implementations). Blank
// lines without preceding fields, comment lines (leading ':'), and unknown
// fields (id:, retry:, …) are ignored; a repeated event field keeps the last.
std::vector<Frame> decode(std::string_view input);

// Encodes one frame following the standard SSE convention: an optional
// `event: <name>\n` line when the event is non-empty, one `data: <line>\n`
// line per LF-separated line of data (`data: \n` when empty), and a
// terminating blank line.
std::string encode(const Frame& frame);

}  // namespace oxa::sse
