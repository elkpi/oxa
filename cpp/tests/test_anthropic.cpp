#include "oxa/anthropic/messages.hpp"
#include "oxa/vectest.hpp"
#include "test_util.hpp"

class AnthropicConverter : public oxa::vectest::Converter {
public:
    std::string_view face() const override {
        return "anthropic";
    }

    oxa::StatusOr<oxa::Conversion<oxa::ir::Request>> decode_request(
        const oxa::json::Value& wire) const override {
        return oxa::anthropic::messages::decode_request(wire);
    }

    oxa::StatusOr<oxa::Conversion<oxa::ir::Response>> decode_response(
        const oxa::json::Value& wire) const override {
        return oxa::anthropic::messages::decode_response(wire);
    }

    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_request(
        const oxa::ir::Request& req) const override {
        return oxa::anthropic::messages::encode_request(req);
    }

    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_response(
        const oxa::ir::Response& resp) const override {
        return oxa::anthropic::messages::encode_response(resp);
    }
};

int main() {
    AnthropicConverter conv;
    auto rep_res = oxa::vectest::run_nonstream(conv);
    CHECK_MSG(rep_res.ok(), rep_res.status().to_string());
    if (!rep_res->failures.empty()) {
        for (const auto& f : rep_res->failures) {
            std::fprintf(stderr, "FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
        }
    }
    CHECK(rep_res->failures.empty());
    CHECK(rep_res->executed == 90);

    // Test mid-conversation system messages
    {
        auto wire_res = oxa::json::parse(R"({
            "model": "claude-opus-5-5",
            "max_tokens": 1024,
            "system": [{"type": "text", "text": "Base prompt."}],
            "messages": [
                {"role": "user", "content": "Hi"},
                {"role": "assistant", "content": "Hello"},
                {"role": "user", "content": "Summarize"},
                {"role": "system", "content": "Terse mode."},
                {
                    "role": "system",
                    "content": [
                        {
                            "type": "text",
                            "text": "Under 20 words.",
                            "cache_control": {"type": "ephemeral"}
                        }
                    ]
                }
            ]
        })");
        CHECK(wire_res.ok());
        auto decoded = oxa::anthropic::messages::decode_request(*wire_res);
        CHECK(decoded.ok());
        if (decoded.ok()) {
            CHECK(decoded->value.messages.size() == 3);
            CHECK(decoded->value.system.size() == 3);
            CHECK(decoded->value.system[0].text == "Base prompt.");
            CHECK(decoded->value.system[1].text == "Terse mode.");
            CHECK(decoded->value.system[2].text == "Under 20 words.");
            CHECK(decoded->value.system[2].native.size() == 1);
            CHECK(decoded->value.system[2].native[0].kind == "cache_control");
            CHECK(decoded->losses.size() == 2);
            CHECK(decoded->losses[0].path == "messages[3].role");
            CHECK(decoded->losses[0].field == "role");
            CHECK(decoded->losses[0].reason == oxa::ir::LOSS_DEGRADED);
            CHECK(decoded->losses[1].path == "messages[4].role");
            CHECK(decoded->losses[1].field == "role");
            CHECK(decoded->losses[1].reason == oxa::ir::LOSS_DEGRADED);
        }

        // Placement error: index 0
        auto err0 = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 1024,
            "messages": [{"role": "system", "content": "Init"}, {"role": "user", "content": "Hi"}]
        })");
        CHECK(err0.ok());
        auto dec0 = oxa::anthropic::messages::decode_request(*err0);
        CHECK(!dec0.ok());
        CHECK(dec0.status().to_string().find("messages[0]: system message cannot appear at messages[0]") != std::string::npos);

        // Placement error: followed by user
        auto err_user = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 1024,
            "messages": [{"role": "user", "content": "Hi"}, {"role": "system", "content": "Mid"}, {"role": "user", "content": "Another"}]
        })");
        CHECK(err_user.ok());
        auto dec_user = oxa::anthropic::messages::decode_request(*err_user);
        CHECK(!dec_user.ok());
        CHECK(dec_user.status().to_string().find("system message must be followed by an assistant turn or be the last message") != std::string::npos);

        // Placement error: preceded by assistant
        auto err_asst = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 1024,
            "messages": [{"role": "user", "content": "Hi"}, {"role": "assistant", "content": "Answer"}, {"role": "system", "content": "Mid"}]
        })");
        CHECK(err_asst.ok());
        auto dec_asst = oxa::anthropic::messages::decode_request(*err_asst);
        CHECK(!dec_asst.ok());
        CHECK(dec_asst.status().to_string().find("system message must follow a user turn") != std::string::npos);

        // Content error: non-text block
        auto err_content = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 1024,
            "messages": [{"role": "user", "content": "Hi"}, {"role": "system", "content": [{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "abc"}}]}]
        })");
        CHECK(err_content.ok());
        auto dec_content = oxa::anthropic::messages::decode_request(*err_content);
        CHECK(!dec_content.ok());
        CHECK(dec_content.status().to_string().find("system content must be text") != std::string::npos);
    }

    oxa::ir::Request request;
    request.model = "claude-3-5-sonnet-20241022";
    request.messages.push_back(oxa::ir::Message{
        std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"json"}}});
    request.params = oxa::ir::Params{};
    request.params->max_tokens = 1024;
    request.params->response_format = oxa::ir::ResponseFormat{};
    request.params->response_format->type = "json_object";
    auto encoded = oxa::anthropic::messages::encode_request(request);
    CHECK(encoded.ok());
    if (encoded.ok()) {
        CHECK(encoded->losses.size() == 1);
        CHECK(encoded->losses[0].path == "params.response_format");
        CHECK(encoded->losses[0].field == "response_format");
        CHECK(encoded->losses[0].reason == oxa::ir::LOSS_UNMAPPED_FIELD);
    }
    std::printf("test_anthropic: all %zu vectors passed\n", rep_res->executed);
    return 0;
}
