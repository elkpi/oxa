#include <iostream>
#include <string_view>
#include <vector>

#include "oxa/ir.hpp"
#include "oxa/openai/chatcompletions.hpp"

int main() {
    const auto request = oxa::openai::chatcompletions::decode_request(
        R"({"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]})");
    if (!request.ok() || request->value.messages.size() != 1) {
        std::cerr << "request conversion failed\n";
        return 1;
    }

    oxa::openai::chatcompletions::StreamDecoder decoder;
    std::vector<oxa::ir::Event> events;
    const auto feed = [&decoder, &events](std::string_view chunk) {
        const auto parsed = oxa::json::parse(chunk);
        if (!parsed.ok()) {
            return false;
        }
        const auto decoded = decoder.feed(parsed.value());
        if (!decoded.ok()) {
            return false;
        }
        events.insert(events.end(), decoded->begin(), decoded->end());
        return true;
    };

    if (!feed(
            R"({"id":"stream-1","object":"chat.completion.chunk","created":0,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]})") ||
        !feed(
            R"({"id":"stream-1","object":"chat.completion.chunk","created":0,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"q\""}}]},"finish_reason":null}]})") ||
        !feed(
            R"({"id":"stream-1","object":"chat.completion.chunk","created":0,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":1}"}}]},"finish_reason":null}]})") ||
        !feed(
            R"({"id":"stream-1","object":"chat.completion.chunk","created":0,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]})")) {
        std::cerr << "stream conversion failed\n";
        return 1;
    }

    const auto terminal = decoder.flush();
    if (!terminal.ok()) {
        std::cerr << "stream flush failed\n";
        return 1;
    }
    events.insert(events.end(), terminal->begin(), terminal->end());
    if (events.empty() || !oxa::ir::validate_event_stream(events).ok()) {
        std::cerr << "invalid stream events\n";
        return 1;
    }
    std::cout << "cpp consumer: OK\n";
    return 0;
}
