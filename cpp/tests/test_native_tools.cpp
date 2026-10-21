#include <string>
#include <vector>
#include <cstdio>
#include "oxa/anthropic/messages.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/openai/responses.hpp"
#include "test_util.hpp"

static void test_native_tool_declaration_fidelity() {
    std::string definition = R"({ "type" : "advisor_20260301", "name":"advisor", "model":"opus", "max_uses":2, "vendor":{"n":1e+03}, "cache_control":{"type":"ephemeral"} })";
    std::string raw = R"({"model":"m","max_tokens":32,"tools":[)" + definition + R"(],"messages":[{"role":"user","content":"hi"}]})";
    auto dec_res = oxa::anthropic::messages::decode_request(raw);
    CHECK(dec_res.ok());
    CHECK(dec_res->losses.empty());
    CHECK(dec_res->value.tools.has_value());
    CHECK(dec_res->value.tools->size() == 1);
    const auto& t = (*dec_res->value.tools)[0];
    CHECK(t.type == "native");
    CHECK(t.name == "advisor");
    CHECK(t.source.value_or("") == "anthropic");
    CHECK(t.definition.value_or("") == definition);

    oxa::json::Value dumped = oxa::ir::dump_request(dec_res->value);
    auto load_res = oxa::ir::load_request(dumped);
    CHECK(load_res.ok());
    CHECK((*load_res->tools)[0].definition.value_or("") == definition);

    auto enc_res = oxa::anthropic::messages::encode_request(load_res.value());
    CHECK(enc_res.ok());
    CHECK(enc_res->losses.empty());
    const auto* tw_arr = enc_res->value.find("tools");
    CHECK(tw_arr && tw_arr->is_array());
    std::string serialized = oxa::json::serialize(tw_arr->as_array()[0]);
    CHECK(serialized.find(R"("model":"opus")") != std::string::npos);
    CHECK(serialized.find(R"("max_uses":2)") != std::string::npos);
}

static void test_malformed_native_declarations_rejected() {
    std::vector<std::string> invalid = {
        R"({"name":"function"})",
        R"({"name":"function","input_schema":[]})",
        R"({"type":null,"name":"function","input_schema":{}})",
        R"({"type":7,"name":"function","input_schema":{}})",
        R"({"type":"advisor_20260301","name":""})",
        R"({"type":"advisor_20260301","name":7})",
    };
    for (const auto& item : invalid) {
        std::string raw = R"({"model":"m","max_tokens":32,"tools":[)" + item + R"(],"messages":[{"role":"user","content":"hi"}]})";
        auto dec_res = oxa::anthropic::messages::decode_request(raw);
        CHECK(!dec_res.ok());
    }
}

static void test_native_tool_choice_without_explicit_tool_type() {
    std::string raw = R"({"model":"m","max_tokens":32,"tools":[{"type":"advisor_20260301","name":"advisor","model":"opus"}],"tool_choice":{"type":"tool","name":"advisor"},"messages":[{"role":"user","content":"hi"}]})";
    auto dec_res = oxa::anthropic::messages::decode_request(raw);
    CHECK(dec_res.ok());
    auto req = std::move(dec_res->value);
    req.tool_choice = oxa::ir::ToolChoice("tool", std::string("advisor"));

    auto cc_res = oxa::openai::chatcompletions::encode_request(req);
    CHECK(cc_res.ok());
    CHECK(!cc_res->value.find("tools"));
    CHECK(!cc_res->value.find("tool_choice"));
    CHECK(cc_res->losses.size() == 2);
    CHECK(cc_res->losses[0].field == "type");
    CHECK(cc_res->losses[1].field == "tool_choice");
    CHECK(cc_res->losses[1].reason == oxa::ir::LOSS_UNSUPPORTED_SEMANTIC);

    auto re_res = oxa::openai::responses::encode_request(req);
    CHECK(re_res.ok());
    CHECK(!re_res->value.find("tools"));
    CHECK(!re_res->value.find("tool_choice"));
    CHECK(re_res->losses.size() == 2);
    CHECK(re_res->losses[0].field == "type");
    CHECK(re_res->losses[1].field == "tool_choice");
    CHECK(re_res->losses[1].reason == oxa::ir::LOSS_UNSUPPORTED_SEMANTIC);
}

static void test_explicit_custom_label_keeps_input_schema() {
    std::string raw = R"({"model":"m","max_tokens":32,"tools":[{"type":"custom","name":"lookup","description":"Look up a city.","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"messages":[{"role":"user","content":"hi"}]})";
    auto dec_res = oxa::anthropic::messages::decode_request(raw);
    CHECK(dec_res.ok());
    CHECK(dec_res->losses.empty());
    CHECK(dec_res->value.tools.has_value());
    const auto& tool = (*dec_res->value.tools)[0];
    CHECK(tool.type != "custom");
    CHECK(!tool.input_schema.is_null());

    auto enc_res = oxa::anthropic::messages::encode_request(dec_res->value);
    CHECK(enc_res.ok());
    CHECK(enc_res->losses.empty());
    const auto* schema = enc_res->value.find("tools")->as_array()[0].find("input_schema");
    CHECK(schema && schema->is_object() && schema->find("required"));
}

static void test_encode_rejects_contradicting_native_identity() {
    std::string raw = R"({"model":"m","max_tokens":32,"tools":[{"type":"advisor_20260301","name":"advisor","model":"opus"}],"messages":[{"role":"user","content":"hi"}]})";
    auto dec_res = oxa::anthropic::messages::decode_request(raw);
    CHECK(dec_res.ok());
    auto req = dec_res->value;

    (*req.tools)[0].definition = R"({"type":"advisor_20260301","name":"other","model":"opus"})";
    auto mismatched = oxa::anthropic::messages::encode_request(req);
    CHECK(!mismatched.ok());

    (*req.tools)[0].definition = "{}";
    auto empty = oxa::anthropic::messages::encode_request(req);
    CHECK(!empty.ok());
}

int main() {
    test_native_tool_declaration_fidelity();
    test_malformed_native_declarations_rejected();
    test_native_tool_choice_without_explicit_tool_type();
    test_explicit_custom_label_keeps_input_schema();
    test_encode_rejects_contradicting_native_identity();
    std::printf("test_native_tools passed\n");
    return 0;
}
