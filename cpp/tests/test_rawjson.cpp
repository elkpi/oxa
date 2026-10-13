#include "oxa/anthropic/messages.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/openai/responses.hpp"
#include "test_util.hpp"

int main() {
    auto source_string = oxa::json::parse(R"("literal")");
    CHECK(source_string.ok());
    CHECK(source_string->source() != nullptr);
    CHECK(source_string->raw_slice() == R"("literal")");
    auto request = oxa::openai::responses::decode_request(std::string_view(
        R"({"model":"m","input":"hi","store":false,"previous_response_id":"r1","unknown_extension":null})"));
    CHECK(request.ok());
    CHECK(request->losses.size() == 3);
    CHECK(request->losses[0].path == "store");
    CHECK(request->losses[1].path == "previous_response_id");
    CHECK(request->losses[2].path == "unknown_extension");

    const std::string input = R"({ "z": 9007199254740993, "a": 1e+01 })";
    const std::string wire = R"({"model":"m","max_tokens":32,"messages":[{"role":"user","content":"invoke"},{"role":"assistant","content":[{"type":"tool_use","input":)" + input + R"(,"id":"t1","name":"f"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]})";
    auto tool_request = oxa::anthropic::messages::decode_request(std::string_view(wire));
    CHECK(tool_request.ok());
    CHECK(tool_request->losses.empty());
    const auto& tool = std::get<oxa::ir::ToolUseBlock>(tool_request->value.messages[1].content[0]);
    CHECK(tool.input == input);

    oxa::openai::responses::StreamDecoder responses;
    CHECK(responses.feed(std::string_view(R"({"type":"response.created","response":{"id":"r1","model":"m","status":"in_progress","output":[]},"unknown_extension":null})")).ok());
    CHECK(responses.feed(std::string_view(R"({"type":"response.completed","response":{"id":"r1","model":"m","status":"completed","output":[]}})")).ok());
    CHECK(responses.flush().ok());
    CHECK(responses.losses().size() == 1);

    oxa::anthropic::messages::StreamDecoder anthropic;
    CHECK(anthropic.feed(std::string_view(R"({"type":"message_start","message":{"id":"m1","model":"m","content":[]},"unknown_extension":null})")).ok());
    CHECK(anthropic.feed(std::string_view(R"({"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}})")).ok());
    CHECK(anthropic.feed(std::string_view(R"({"type":"message_stop"})")).ok());
    CHECK(anthropic.flush().ok());
    CHECK(anthropic.losses().size() == 1);

    oxa::openai::chatcompletions::StreamDecoder chat;
    CHECK(chat.feed(std::string_view(R"({"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}],"unknown_extension":null})")).ok());
    CHECK(chat.feed(std::string_view(R"({"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]})")).ok());
    CHECK(chat.flush().ok());
    CHECK(chat.losses().size() == 1);
    return 0;
}
