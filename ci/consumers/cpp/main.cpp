#include <iostream>
#include <string_view>
#include <vector>

#include "oxa/ir.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/systemone/jev.hpp"

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

    oxa::ir::Request native_req;
    native_req.model = "m";
    native_req.messages.push_back(oxa::ir::Message{
        std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"hi"}}});
    native_req.native.push_back(oxa::ir::NativeAnnotation{
        std::string(oxa::ir::NATIVE_SOURCE_ANTHROPIC),
        std::string(oxa::ir::NATIVE_KIND_THINKING_MODE),
        "\"adaptive\"",
        std::nullopt,
        std::nullopt,
    });
    if (!oxa::ir::validate_native_request(native_req).ok()) {
        std::cerr << "validate native request failed\n";
        return 1;
    }

    const std::string jev_request_json =
        R"({"model":"jev-latest","state":"state text","questions":{"q":{"type":"noul"}}})";
    auto jev_req = oxa::systemone::jev::decode_request(std::string_view(jev_request_json));
    if (!jev_req.ok() || !jev_req->losses.empty()) {
        std::cerr << "jev decode_request failed\n";
        return 1;
    }
    auto jev_wire = oxa::systemone::jev::encode_request(jev_req->value);
    if (!jev_wire.ok()) {
        std::cerr << "jev encode_request failed\n";
        return 1;
    }
    const std::string jev_response_json =
        R"({"model":"m","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":2}})";
    auto jev_resp =
        oxa::systemone::jev::decode_response(std::string_view(jev_response_json));
    if (!jev_resp.ok()) {
        std::cerr << "jev decode_response failed\n";
        return 1;
    }
    auto jev_resp_wire = oxa::systemone::jev::encode_response(jev_resp->value);
    if (!jev_resp_wire.ok()) {
        std::cerr << "jev encode_response failed\n";
        return 1;
    }
    const auto& jev_answers = jev_resp_wire->value.find("answers");
    if (jev_answers == nullptr || jev_answers->find("q") == nullptr ||
        jev_answers->find("q")->find("noul") == nullptr) {
        std::cerr << "jev response answers missing\n";
        return 1;
    }

    std::cout << "cpp consumer: OK\n";
    return 0;
}
