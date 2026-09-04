#include "oxa/anthropic/messages.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/openai/responses.hpp"
#include "oxa/vectest.hpp"
#include "test_util.hpp"

class ChatCompletionsConverter : public oxa::vectest::Converter {
public:
    std::string_view face() const override { return "chatcompletions"; }
    oxa::StatusOr<oxa::Conversion<oxa::ir::Request>> decode_request(const oxa::json::Value& wire) const override {
        return oxa::openai::chatcompletions::decode_request(wire);
    }
    oxa::StatusOr<oxa::Conversion<oxa::ir::Response>> decode_response(const oxa::json::Value& wire) const override {
        return oxa::openai::chatcompletions::decode_response(wire);
    }
    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_request(const oxa::ir::Request& req) const override {
        return oxa::openai::chatcompletions::encode_request(req);
    }
    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_response(const oxa::ir::Response& resp) const override {
        return oxa::openai::chatcompletions::encode_response(resp);
    }
};

class AnthropicConverter : public oxa::vectest::Converter {
public:
    std::string_view face() const override { return "anthropic"; }
    oxa::StatusOr<oxa::Conversion<oxa::ir::Request>> decode_request(const oxa::json::Value& wire) const override {
        return oxa::anthropic::messages::decode_request(wire);
    }
    oxa::StatusOr<oxa::Conversion<oxa::ir::Response>> decode_response(const oxa::json::Value& wire) const override {
        return oxa::anthropic::messages::decode_response(wire);
    }
    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_request(const oxa::ir::Request& req) const override {
        return oxa::anthropic::messages::encode_request(req);
    }
    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_response(const oxa::ir::Response& resp) const override {
        return oxa::anthropic::messages::encode_response(resp);
    }
};

class ResponsesConverter : public oxa::vectest::Converter {
public:
    std::string_view face() const override { return "responses"; }
    oxa::StatusOr<oxa::Conversion<oxa::ir::Request>> decode_request(const oxa::json::Value& wire) const override {
        return oxa::openai::responses::decode_request(wire);
    }
    oxa::StatusOr<oxa::Conversion<oxa::ir::Response>> decode_response(const oxa::json::Value& wire) const override {
        return oxa::openai::responses::decode_response(wire);
    }
    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_request(const oxa::ir::Request& req) const override {
        return oxa::openai::responses::encode_request(req);
    }
    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_response(const oxa::ir::Response& resp) const override {
        return oxa::openai::responses::encode_response(resp);
    }
};

int main() {
    ChatCompletionsConverter cc;
    AnthropicConverter ant;
    ResponsesConverter resp;

    // 1. Chat Completions nonstream
    {
        auto r = oxa::vectest::run_nonstream(cc);
        CHECK_MSG(r.ok(), r.status().to_string());
        CHECK(r->failures.empty());
        CHECK(r->executed == 34);
    }

    // 2. Anthropic nonstream
    {
        auto r = oxa::vectest::run_nonstream(ant);
        CHECK_MSG(r.ok(), r.status().to_string());
        CHECK(r->failures.empty());
        CHECK(r->executed == 30);
    }

    // 3. Responses nonstream
    {
        auto r = oxa::vectest::run_nonstream(resp);
        CHECK_MSG(r.ok(), r.status().to_string());
        CHECK(r->failures.empty());
        CHECK(r->executed == 41);
    }

    // 4. Cross-protocol nonstream
    {
        std::vector<const oxa::vectest::Converter*> convs{&cc, &ant, &resp};
        auto r = oxa::vectest::run_cross(convs);
        CHECK_MSG(r.ok(), r.status().to_string());
        if (!r->failures.empty()) {
            for (const auto& f : r->failures) {
                std::fprintf(stderr, "CROSS FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
            }
        }
        CHECK(r->failures.empty());
        CHECK(r->executed == 12);
        std::printf("test_cross: all %zu vectors passed\n", r->executed);
    }

    std::puts("test_vectors: all 117 nonstream vectors passed");
    return 0;
}
