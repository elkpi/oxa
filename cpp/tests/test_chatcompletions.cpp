#include "oxa/openai/chatcompletions.hpp"
#include "oxa/vectest.hpp"
#include "test_util.hpp"

class ChatCompletionsConverter : public oxa::vectest::Converter {
public:
    std::string_view face() const override {
        return "chatcompletions";
    }

    oxa::StatusOr<oxa::Conversion<oxa::ir::Request>> decode_request(
        const oxa::json::Value& wire) const override {
        return oxa::openai::chatcompletions::decode_request(wire);
    }

    oxa::StatusOr<oxa::Conversion<oxa::ir::Response>> decode_response(
        const oxa::json::Value& wire) const override {
        return oxa::openai::chatcompletions::decode_response(wire);
    }

    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_request(
        const oxa::ir::Request& req) const override {
        return oxa::openai::chatcompletions::encode_request(req);
    }

    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_response(
        const oxa::ir::Response& resp) const override {
        return oxa::openai::chatcompletions::encode_response(resp);
    }
};

int main() {
    ChatCompletionsConverter conv;
    auto rep_res = oxa::vectest::run_nonstream(conv);
    CHECK_MSG(rep_res.ok(), rep_res.status().to_string());
    if (!rep_res->failures.empty()) {
        for (const auto& f : rep_res->failures) {
            std::fprintf(stderr, "FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
        }
    }
    CHECK(rep_res->failures.empty());
    CHECK(rep_res->executed == 44);

    auto wire = oxa::json::parse(R"({
        "model":"gpt-4o",
        "messages":[{"role":"user","content":"json"}],
        "response_format":{"type":"json_schema","json_schema":{"name":"result",
            "schema":{"type":"object","x-order":[2,1]},"strict":false}}
    })");
    CHECK(wire.ok());
    auto decoded = oxa::openai::chatcompletions::decode_request(*wire);
    CHECK(decoded.ok());
    if (decoded.ok()) {
        CHECK(decoded->losses.empty());
        CHECK(decoded->value.params.has_value());
        if (decoded->value.params.has_value()) {
            CHECK(decoded->value.params->response_format.has_value());
            if (decoded->value.params->response_format.has_value()) {
                CHECK(decoded->value.params->response_format->type == "json_schema");
                CHECK(decoded->value.params->response_format->strict.has_value());
                CHECK(!*decoded->value.params->response_format->strict);
            }
        }
        auto encoded = oxa::openai::chatcompletions::encode_request(decoded->value);
        CHECK(encoded.ok());
        if (encoded.ok()) CHECK(oxa::json::structurally_equal(*wire, encoded->value));
    }
    std::printf("test_chatcompletions: all %zu vectors passed\n", rep_res->executed);
    return 0;
}
