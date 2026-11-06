#include "oxa/ir.hpp"
#include "oxa/anthropic/messages.hpp"
#include "oxa/openai/responses.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/json.hpp"
#include "test_util.hpp"

int main() {
    const auto source = oxa::json::parse(R"({"specVersion":"0.3.0","model":"m","messages":[{"role":"user","content":[{"type":"compaction","content":null,"tool_changes":[]},{"type":"text","text":"go"}]}],"tool_change_groups":[{"message_index":1,"content_index":-1,"parts":[{"type":"tool_change","change":{"op":"remove","ref":{"type":"mcp_toolset_reference","server_name":"svc"}}}]}]})");
    CHECK(source.ok());
    const auto request = oxa::ir::load_request(*source);
    CHECK_MSG(request.ok(), request.status().to_string());
    CHECK(oxa::json::structurally_equal(*source, oxa::ir::dump_request(*request)));
    const auto anthropic = oxa::json::parse(R"({"model":"m","max_tokens":32,"messages":[{"role":"user","content":[{"type":"compaction","content":null,"tool_changes":[]},{"type":"text","text":"go"}]},{"role":"system","content":[{"type":"tool_removal","tool":{"type":"mcp_toolset_reference","server_name":"svc"}}]},{"role":"assistant","content":"ok"}]})");
    CHECK(anthropic.ok());
    auto decoded = oxa::anthropic::messages::decode_request(*anthropic);
    CHECK_MSG(decoded.ok(), decoded.status().to_string());
    CHECK(decoded->losses.empty());
    auto encoded = oxa::anthropic::messages::encode_request(decoded->value);
    CHECK_MSG(encoded.ok(), encoded.status().to_string());
    CHECK(encoded->losses.empty());
    const auto* messages = encoded->value.find("messages");
    CHECK(messages && messages->as_array().size() == 3);
    CHECK(oxa::json::structurally_equal(messages->as_array()[0].find("content")->as_array()[0], anthropic->find("messages")->as_array()[0].find("content")->as_array()[0]));
    CHECK(oxa::json::structurally_equal(*messages->as_array()[1].find("content"), *anthropic->find("messages")->as_array()[1].find("content")));
    const auto responses = oxa::json::parse(R"({"model":"m","input":[{"role":"user","content":"go"},{"type":"function_call","name":"a","call_id":"a","arguments":"{}"},{"type":"additional_tools","role":"developer","id":"later","status":"completed","future":{"keep":true},"tools":[]},{"type":"function_call","name":"b","call_id":"b","arguments":"{}"}]})");
    CHECK(responses.ok());
    auto decoded_responses = oxa::openai::responses::decode_request(*responses);
    CHECK_MSG(decoded_responses.ok(), decoded_responses.status().to_string());
    CHECK(decoded_responses->losses.empty());
    CHECK(decoded_responses->value.tool_change_groups.size() == 1);
    CHECK(decoded_responses->value.tool_change_groups[0].message_index == 1);
    CHECK(decoded_responses->value.tool_change_groups[0].content_index == 1);
    auto encoded_responses = oxa::openai::responses::encode_request(decoded_responses->value);
    CHECK_MSG(encoded_responses.ok(), encoded_responses.status().to_string());
    CHECK(encoded_responses->losses.empty());
    CHECK(oxa::json::structurally_equal(encoded_responses->value.find("input")->as_array()[2], responses->find("input")->as_array()[2]));
    auto chat = oxa::openai::chatcompletions::encode_request(decoded->value);
    CHECK_MSG(chat.ok(), chat.status().to_string());
    CHECK(chat->losses.size() == 2);
    CHECK(chat->losses[0].reason == "unsupported-semantic");
    CHECK(chat->losses[1].reason == "unsupported-semantic");
    auto cross_responses = oxa::openai::responses::encode_request(decoded->value);
    CHECK_MSG(cross_responses.ok(), cross_responses.status().to_string());
    CHECK(cross_responses->losses.size() == 2);
    auto invalid = decoded->value;
    invalid.tool_change_groups[0].message_index = 9;
    CHECK(!oxa::anthropic::messages::encode_request(invalid).ok());
    CHECK(!oxa::openai::responses::encode_request(invalid).ok());
    CHECK(!oxa::openai::chatcompletions::encode_request(invalid).ok());
    return 0;
}
