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
        CHECK(r->executed == 52);
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
        CHECK(r->executed == 90);
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
        CHECK(r->executed == 131);
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
        CHECK(r->executed == 34);
        std::printf("test_cross: all %zu vectors passed\n", r->executed);
    }

    // Custom tools are supported in Chat Completions and Anthropic without loss.
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
        if (cc_req.ok()) CHECK(cc_req->losses.empty());
        if (ant_req.ok()) CHECK(ant_req->losses.empty());

        oxa::ir::Response response;
        response.id = "r"; response.model = "m"; response.stop_reason = std::string(oxa::ir::STOP_TOOL_USE);
        response.content.push_back(oxa::ir::CustomToolUseBlock{"c", "code_exec", "arbitrary"});
        auto cc_resp = oxa::openai::chatcompletions::encode_response(response);
        auto ant_resp = oxa::anthropic::messages::encode_response(response);
        CHECK(cc_resp.ok() && ant_resp.ok());
        if (cc_resp.ok()) {
            CHECK(cc_resp->losses.empty());
            CHECK(cc_resp->value.find("choices")->as_array()[0].find("finish_reason")->as_string() == "tool_calls");
        }
        if (ant_resp.ok()) {
            CHECK(ant_resp->losses.empty());
            CHECK(ant_resp->value.find("stop_reason")->as_string() == "tool_use");
        }
    }

    // Apply patch tools remain unsupported in Chat Completions and Anthropic.
    {
        oxa::ir::Request req;
        req.model = "m";
        req.tools = std::vector<oxa::ir::Tool>{};
        oxa::ir::Tool tool; tool.type = "apply_patch"; tool.name = "apply_patch";
        req.tools->push_back(tool);
        req.tool_choice = oxa::ir::ToolChoice{std::string(oxa::ir::TOOL_CHOICE_TOOL), "apply_patch", "apply_patch"};
        req.params = oxa::ir::Params{}; req.params->max_tokens = 32;
        req.messages.push_back({"user", {oxa::ir::TextBlock{"hello"}}});
        auto cc_req = oxa::openai::chatcompletions::encode_request(req);
        auto ant_req = oxa::anthropic::messages::encode_request(req);
        CHECK(cc_req.ok() && ant_req.ok());
        if (cc_req.ok()) CHECK(cc_req->losses.size() == 2);
        if (ant_req.ok()) CHECK(ant_req->losses.size() == 2);

        oxa::ir::Response response;
        response.id = "r"; response.model = "m"; response.stop_reason = std::string(oxa::ir::STOP_TOOL_USE);
        response.content.push_back(oxa::ir::ApplyPatchToolUseBlock{"p", "in_progress", oxa::ir::CreateFileOperation{"a.txt", ""}});
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

    // Declared function tool not promoted to custom tool in Anthropic decode
    {
        auto wire = oxa::json::parse(R"({
            "model": "claude-3-5-sonnet-20241022",
            "max_tokens": 1024,
            "tools": [
                {
                    "name": "echo",
                    "input_schema": {
                        "type": "object",
                        "properties": {"input": {"type": "string"}, "extra": {"type": "string"}},
                        "required": ["input"]
                    }
                }
            ],
            "messages": [
                {"role": "user", "content": "hi"},
                {
                    "role": "assistant",
                    "content": [
                        {"type": "tool_use", "id": "call_1", "name": "echo", "input": {"input": "hello"}}
                    ]
                },
                {
                    "role": "user",
                    "content": [{"type": "tool_result", "tool_use_id": "call_1", "content": "ok"}]
                }
            ]
        })");
        CHECK(wire.ok());
        auto decoded = oxa::anthropic::messages::decode_request(*wire);
        CHECK(decoded.ok());
        CHECK(decoded->losses.empty());
        CHECK(decoded->value.tools.has_value() && (*decoded->value.tools)[0].type == "function");
        CHECK(std::holds_alternative<oxa::ir::ToolUseBlock>(decoded->value.messages[1].content[0]));
        CHECK(std::holds_alternative<oxa::ir::ToolResultBlock>(decoded->value.messages[2].content[0]));
    }

    // Custom tool with grammar format produces unsupported-semantic loss on Anthropic encode
    {
        oxa::ir::Request req;
        req.model = "claude-3-5-sonnet-20241022";
        req.messages.push_back({"user", {oxa::ir::TextBlock{"hi"}}});
        oxa::ir::Tool tool;
        tool.type = "custom";
        tool.name = "calc";
        auto fmt = oxa::json::parse(R"({"type":"grammar","syntax":"regex","definition":"[0-9]+"})");
        CHECK(fmt.ok());
        tool.format = *fmt;
        req.tools = std::vector<oxa::ir::Tool>{tool};
        req.params = oxa::ir::Params{};
        req.params->max_tokens = 1024;
        auto encoded = oxa::anthropic::messages::encode_request(req);
        CHECK(encoded.ok());
        bool found = false;
        for (const auto& l : encoded->losses) {
            if (l.path == "tools[0].format" && l.reason == oxa::ir::LOSS_UNSUPPORTED_SEMANTIC) {
                found = true;
                break;
            }
        }
        CHECK(found);
    }

    // CustomToolResultBlock content array encodes as array and round-trips with ImageBlock
    {
        oxa::ir::Request req;
        req.model = "m";
        req.params = oxa::ir::Params{};
        req.params->max_tokens = 1024;
        req.messages.push_back({"user", {oxa::ir::TextBlock{"hi"}}});
        req.messages.push_back({"assistant", {oxa::ir::CustomToolUseBlock{"c", "exec", "cmd"}}});
        std::vector<oxa::ir::BlockHolder> output{
            {oxa::ir::TextBlock{"line 1"}},
            {oxa::ir::ImageBlock{std::nullopt, std::nullopt, std::string("https://example.com/img.png")}}
        };
        req.messages.push_back({"user", {oxa::ir::CustomToolResultBlock{"c", output}}});
        auto cc_enc = oxa::openai::chatcompletions::encode_request(req);
        CHECK(cc_enc.ok() && cc_enc->losses.empty());
        const auto* tool_msg = &cc_enc->value.find("messages")->as_array()[2];
        CHECK(tool_msg->find("content")->is_array());
        CHECK(tool_msg->find("content")->as_array().size() == 2);
        auto cc_dec = oxa::openai::chatcompletions::decode_request(cc_enc->value);
        CHECK(cc_dec.ok() && cc_dec->losses.empty());
        const auto& dec_block = cc_dec->value.messages[2].content[0];
        const auto* ctr = std::get_if<oxa::ir::CustomToolResultBlock>(&dec_block);
        CHECK(ctr != nullptr);
        CHECK(std::holds_alternative<std::vector<oxa::ir::BlockHolder>>(ctr->output));
        CHECK(std::get<std::vector<oxa::ir::BlockHolder>>(ctr->output).size() == 2);

        auto ant_enc = oxa::anthropic::messages::encode_request(req);
        CHECK(ant_enc.ok() && ant_enc->losses.empty());
        const auto* ant_content = ant_enc->value.find("messages")->as_array()[2].find("content")->as_array()[0].find("content");
        CHECK(ant_content && ant_content->is_array());
        CHECK(ant_content->as_array().size() == 2);
    }

    // Chat Completions preserves missing format and reports unknown format
    {
        auto wire_missing = oxa::json::parse(R"({
            "model": "m",
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"type": "custom", "custom": {"name": "c"}}]
        })");
        CHECK(wire_missing.ok());
        auto dec_missing = oxa::openai::chatcompletions::decode_request(*wire_missing);
        CHECK(dec_missing.ok() && dec_missing->losses.empty());
        CHECK(!(*dec_missing->value.tools)[0].format.has_value());

        auto wire_unknown = oxa::json::parse(R"({
            "model": "m",
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"type": "custom", "custom": {"name": "c", "format": {"type": "unknown_fmt"}}}]
        })");
        CHECK(wire_unknown.ok());
        auto dec_unknown = oxa::openai::chatcompletions::decode_request(*wire_unknown);
        CHECK(dec_unknown.ok());
        CHECK(dec_unknown->losses.size() == 1);
        CHECK(dec_unknown->losses[0].reason == oxa::ir::LOSS_UNSUPPORTED_SEMANTIC);
        CHECK(!(*dec_unknown->value.tools)[0].format.has_value());
    }

    std::puts("test_vectors: all 241 nonstream vectors passed");
    return 0;
}
