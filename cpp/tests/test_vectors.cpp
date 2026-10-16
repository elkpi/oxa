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
    // Baseline 0.1.0 vectors compare against converters that emit 0.2.0.
    {
        auto expected = oxa::json::parse(R"({"specVersion":"0.1.0"})");
        auto actual = oxa::json::parse(R"({"specVersion":"0.2.0"})");
        CHECK(expected.ok() && actual.ok());
        CHECK(oxa::vectest::compare_json(*expected, *actual).ok());
        auto unsupported = oxa::json::parse(R"({"specVersion":"0.3.0"})");
        CHECK(unsupported.ok());
        CHECK(!oxa::vectest::compare_json(*actual, *unsupported).ok());
    }

    ChatCompletionsConverter cc;
    AnthropicConverter ant;
    ResponsesConverter resp;

    // 1. Chat Completions nonstream
    {
        auto r = oxa::vectest::run_nonstream(cc);
        CHECK_MSG(r.ok(), r.status().to_string());
        if (!r->failures.empty()) {
            for (const auto& f : r->failures) {
                std::fprintf(stderr, "CC FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
            }
        }
        CHECK(r->failures.empty());
        CHECK(r->executed == 46);
    }

    // 2. Anthropic nonstream
    {
        auto r = oxa::vectest::run_nonstream(ant);
        CHECK_MSG(r.ok(), r.status().to_string());
        if (!r->failures.empty()) {
            for (const auto& f : r->failures) {
                std::fprintf(stderr, "ANT FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
            }
        }
        CHECK(r->failures.empty());
        CHECK(r->executed == 42);
    }

    // 3. Responses nonstream
    {
        auto r = oxa::vectest::run_nonstream(resp);
        CHECK_MSG(r.ok(), r.status().to_string());
        if (!r->failures.empty()) {
            for (const auto& f : r->failures) {
                std::fprintf(stderr, "RESP FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
            }
        }
        CHECK(r->failures.empty());
        CHECK(r->executed == 119);
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
        CHECK(r->executed == 20);
        std::printf("test_cross: all %zu vectors passed\n", r->executed);
    }

    // Unsupported target profiles drop custom declarations, choices, calls, and results once each.
    {
        oxa::ir::Request req;
        req.model = "m";
        req.tools = std::vector<oxa::ir::Tool>{};
        oxa::ir::Tool tool; tool.type = "custom"; tool.name = "code_exec";
        req.tools->push_back(tool);
        req.tool_choice = oxa::ir::ToolChoice{std::string(oxa::ir::TOOL_CHOICE_TOOL), "code_exec", "custom"};
        req.params = oxa::ir::Params{}; req.params->max_tokens = 32;
        req.messages.push_back({"assistant", {oxa::ir::CustomToolUseBlock{"c", "code_exec", "arbitrary"}}});
        req.messages.push_back({"user", {oxa::ir::CustomToolResultBlock{"c", std::string("result")}}});
        auto cc_req = oxa::openai::chatcompletions::encode_request(req);
        auto ant_req = oxa::anthropic::messages::encode_request(req);
        CHECK(cc_req.ok() && ant_req.ok());
        if (cc_req.ok()) CHECK(cc_req->losses.size() == 4);
        if (ant_req.ok()) CHECK(ant_req->losses.size() == 4);
        oxa::ir::Response response;
        response.id = "r"; response.model = "m"; response.stop_reason = std::string(oxa::ir::STOP_TOOL_USE);
        response.content.push_back(oxa::ir::CustomToolUseBlock{"c", "code_exec", "arbitrary"});
        auto cc_resp = oxa::openai::chatcompletions::encode_response(response);
        auto ant_resp = oxa::anthropic::messages::encode_response(response);
        CHECK(cc_resp.ok() && ant_resp.ok());
        if (cc_resp.ok()) {
            CHECK(cc_resp->losses.size() == 2);
            CHECK(cc_resp->value.find("choices")->as_array()[0].find("finish_reason")->as_string() == "stop");
        }
        if (ant_resp.ok()) {
            CHECK(ant_resp->losses.size() == 2);
            CHECK(ant_resp->value.find("stop_reason")->as_string() == "end_turn");
        }
    }

    std::puts("test_vectors: all 181 nonstream vectors passed");
    return 0;
}
