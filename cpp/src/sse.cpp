#include "oxa/sse.hpp"

namespace oxa::sse {
namespace {

// Strips one trailing LF and an optional preceding CR (CRLF and mixed input).
std::string_view strip_line_ending(std::string_view line) {
    if (!line.empty() && line.back() == '\n') line.remove_suffix(1);
    if (!line.empty() && line.back() == '\r') line.remove_suffix(1);
    return line;
}

}  // namespace

std::vector<Frame> decode(std::string_view input) {
    std::vector<Frame> frames;
    std::string event;
    bool have = false;
    std::vector<std::string> data_lines;

    auto emit = [&] {
        if (!have) return;
        std::string data;
        for (std::size_t i = 0; i < data_lines.size(); ++i) {
            if (i > 0) data += '\n';
            data += data_lines[i];
        }
        frames.push_back(Frame{event, std::move(data)});
        event.clear();
        have = false;
        data_lines.clear();
    };

    std::size_t pos = 0;
    while (pos < input.size()) {
        std::size_t nl = input.find('\n', pos);
        std::string_view line;
        bool had_newline;
        if (nl == std::string_view::npos) {
            line = input.substr(pos);
            had_newline = false;
            pos = input.size();
        } else {
            line = input.substr(pos, nl - pos);
            had_newline = true;
            pos = nl + 1;
        }
        line = strip_line_ending(line);

        if (line.empty()) {
            emit();
            continue;
        }
        if (line.front() == ':') continue;  // comment line

        std::string_view name = line;
        std::string_view value;
        std::size_t colon = line.find(':');
        if (colon != std::string_view::npos) {
            name = line.substr(0, colon);
            value = line.substr(colon + 1);
            if (!value.empty() && value.front() == ' ') value.remove_prefix(1);
        }

        if (name == "data") {
            have = true;
            data_lines.emplace_back(value);
        } else if (name == "event") {
            have = true;
            event = std::string(value);  // last one wins
        }
        // id:, retry:, unknown fields: ignored.

        if (!had_newline) break;
    }
    emit();  // final frame without a trailing blank line still emits
    return frames;
}

std::string encode(const Frame& frame) {
    std::string out;
    if (!frame.event.empty()) {
        out += "event: ";
        out += frame.event;
        out += '\n';
    }
    if (frame.data.empty()) {
        out += "data: \n";
    } else {
        std::string_view rest = frame.data;
        while (true) {
            std::size_t nl = rest.find('\n');
            if (nl == std::string_view::npos) {
                out += "data: ";
                out += rest;
                out += '\n';
                break;
            }
            out += "data: ";
            out.append(rest.substr(0, nl));
            out += '\n';
            rest.remove_prefix(nl + 1);
        }
    }
    out += '\n';
    return out;
}

}  // namespace oxa::sse
