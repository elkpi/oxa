// Tests for v2.4.0 Anthropic request fidelity and native options across IR.

#include <cstdio>
#include <string>

#include "oxa/anthropic/messages.hpp"
#include "oxa/ir.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/openai/responses.hpp"
#include "test_util.hpp"

namespace {

void test_native_portable_round_trip() {
    oxa::ir::Request req;
    req.model = "claude-3-7-sonnet-20250219";

    oxa::ir::TextBlock user_blk;
    user_blk.text = "Hello";
    oxa::ir::NativeAnnotation cc_user;
    cc_user.source = "anthropic";
    cc_user.kind = "cache_control";
    cc_user.value = "{\"ttl\":\"1h\",\"type\":\"ephemeral\"}";
    cc_user.omission = oxa::ir::NativeOmission{"cache_control", std::string(oxa::ir::LOSS_UNMAPPED_FIELD), std::nullopt};
    user_blk.native.push_back(cc_user);

    oxa::ir::Message user_msg;
    user_msg.role = std::string(oxa::ir::ROLE_USER);
    user_msg.content.push_back(user_blk);
    req.messages.push_back(user_msg);

    oxa::ir::TextBlock sys_blk;
    sys_blk.text = "System prompt";
    oxa::ir::NativeAnnotation cc_sys;
    cc_sys.source = "anthropic";
    cc_sys.kind = "cache_control";
    cc_sys.value = "{\"type\":\"ephemeral\"}";
    cc_sys.omission = oxa::ir::NativeOmission{"cache_control", std::string(oxa::ir::LOSS_UNMAPPED_FIELD), std::nullopt};
    sys_blk.native.push_back(cc_sys);
    req.system.push_back(sys_blk);

    oxa::ir::Params params;
    params.reasoning_effort = "xhigh";
    req.params = params;

    oxa::ir::NativeAnnotation mode_ann;
    mode_ann.source = "anthropic";
    mode_ann.kind = "thinking_mode";
    mode_ann.value = "\"adaptive\"";
    mode_ann.omission = oxa::ir::NativeOmission{"type", std::string(oxa::ir::LOSS_UNMAPPED_FIELD), std::nullopt};
    req.native.push_back(mode_ann);

    oxa::ir::NativeAnnotation cm_ann;
    cm_ann.source = "anthropic";
    cm_ann.kind = "context_management";
    cm_ann.value = "{\"edits\":[{\"keep\":\"all\",\"type\":\"clear_thinking_20251015\"}]}";
    cm_ann.omission = oxa::ir::NativeOmission{"context_management", std::string(oxa::ir::LOSS_UNSUPPORTED_SEMANTIC), std::nullopt};
    req.native.push_back(cm_ann);

    auto dumped = oxa::ir::dump_request(req);
    auto serialized = oxa::json::serialize(dumped);
    auto parsed = oxa::json::parse(serialized);
    CHECK(parsed.ok());

    auto loaded = oxa::ir::load_request(*parsed);
    CHECK(loaded.ok());
    CHECK(loaded->native.size() == 2);
    CHECK(loaded->native[0].value == "\"adaptive\"");
    CHECK(loaded->params.has_value());
    CHECK(loaded->params->reasoning_effort == "xhigh");
    CHECK(loaded->system.size() == 1);
    CHECK(loaded->system[0].native.size() == 1);
    CHECK(loaded->messages.size() == 1);
    const auto* loaded_user = std::get_if<oxa::ir::TextBlock>(&loaded->messages[0].content[0]);
    CHECK(loaded_user != nullptr);
    CHECK(loaded_user->native.size() == 1);
}

void test_validate_native_request() {
    // 1. Duplicate kind
    oxa::ir::Request duplicate;
    duplicate.model = "model";
    duplicate.messages.push_back(oxa::ir::Message{std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"hi"}}});

    oxa::ir::NativeAnnotation n1;
    n1.source = "anthropic";
    n1.kind = "thinking_mode";
    n1.value = "\"adaptive\"";
    duplicate.native.push_back(n1);

    oxa::ir::NativeAnnotation n2;
    n2.source = "anthropic";
    n2.kind = "thinking_mode";
    n2.value = "\"enabled\"";
    duplicate.native.push_back(n2);

    auto st1 = oxa::ir::validate_native_request(duplicate);
    CHECK(!st1.ok());

    // 2. Invalid JSON value
    oxa::ir::Request bad_json;
    bad_json.model = "model";
    bad_json.messages.push_back(oxa::ir::Message{std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"hi"}}});
    oxa::ir::NativeAnnotation n_bad;
    n_bad.source = "anthropic";
    n_bad.kind = "thinking_mode";
    n_bad.value = "{bad json";
    bad_json.native.push_back(n_bad);

    auto st2 = oxa::ir::validate_native_request(bad_json);
    CHECK(!st2.ok());

    // 3. Host violation: thinking_mode on ContentBlock
    oxa::ir::Request host_violation;
    host_violation.model = "model";
    oxa::ir::TextBlock blk;
    blk.text = "hi";
    blk.native.push_back(n1);
    host_violation.messages.push_back(oxa::ir::Message{std::string(oxa::ir::ROLE_USER), {blk}});

    auto st3 = oxa::ir::validate_native_request(host_violation);
    CHECK(!st3.ok());
}

void test_native_thinking_combinations() {
    // 1. Adaptive thinking
    auto wire_adaptive = oxa::json::parse(
        "{\"model\":\"claude-3-7-sonnet-20250219\",\"max_tokens\":4096,"
        "\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}],"
        "\"thinking\":{\"type\":\"adaptive\"}}");
    CHECK(wire_adaptive.ok());
    auto dec1 = oxa::anthropic::messages::decode_request(*wire_adaptive);
    CHECK(dec1.ok());
    CHECK(dec1->losses.empty());
    CHECK(!dec1->value.params.has_value() || !dec1->value.params->reasoning_effort.has_value());
    CHECK(dec1->value.native.size() == 1);
    CHECK(dec1->value.native[0].kind == oxa::ir::NATIVE_KIND_THINKING_MODE);

    // Encode back to Anthropic
    auto enc1 = oxa::anthropic::messages::encode_request(dec1->value);
    CHECK(enc1.ok());
    CHECK(enc1->losses.empty());
    const auto* th1 = enc1->value.find("thinking");
    CHECK(th1 != nullptr && th1->is_object());
    const auto* th1_type = th1->find("type");
    CHECK(th1_type != nullptr && th1_type->as_string() == "adaptive");

    // Encode to ChatCompletions
    auto cc1 = oxa::openai::chatcompletions::encode_request(dec1->value);
    CHECK(cc1.ok());
    bool found_type_loss = false;
    for (const auto& l : cc1->losses) {
        if (l.field == "type" && l.reason == oxa::ir::LOSS_UNMAPPED_FIELD) {
            found_type_loss = true;
        }
    }
    CHECK(found_type_loss);

    // 2. Enabled thinking with exact budget 6000
    auto wire_budget = oxa::json::parse(
        "{\"model\":\"claude-3-7-sonnet-20250219\",\"max_tokens\":8000,"
        "\"messages\":[{\"role\":\"user\",\"content\":\"Solve math\"}],"
        "\"thinking\":{\"type\":\"enabled\",\"budget_tokens\":6000}}");
    CHECK(wire_budget.ok());
    auto dec2 = oxa::anthropic::messages::decode_request(*wire_budget);
    CHECK(dec2.ok());
    CHECK(dec2->losses.empty());
    CHECK(dec2->value.params.has_value() && dec2->value.params->reasoning_effort == "medium");

    // Encode back to Anthropic: exact 6000 restored
    auto enc2 = oxa::anthropic::messages::encode_request(dec2->value);
    CHECK(enc2.ok());
    CHECK(enc2->losses.empty());
    const auto* th2 = enc2->value.find("thinking");
    CHECK(th2 != nullptr && th2->is_object());
    const auto* th2_b = th2->find("budget_tokens");
    CHECK(th2_b != nullptr && th2_b->as_int() == 6000);

    // Encode to Responses: effort is medium, budget degraded loss
    auto resp2 = oxa::openai::responses::encode_request(dec2->value);
    CHECK(resp2.ok());
    const auto* r2 = resp2->value.find("reasoning");
    CHECK(r2 != nullptr && r2->is_object());
    const auto* r2_eff = r2->find("effort");
    CHECK(r2_eff != nullptr && r2_eff->as_string() == "medium");
    bool found_budget_degraded = false;
    for (const auto& l : resp2->losses) {
        if (l.field == "budget_tokens" && l.reason == oxa::ir::LOSS_DEGRADED) {
            found_budget_degraded = true;
        }
    }
    CHECK(found_budget_degraded);
}

void test_native_edited_effort_wins() {
    auto wire_budget = oxa::json::parse(
        "{\"model\":\"claude-3-7-sonnet-20250219\",\"max_tokens\":8000,"
        "\"messages\":[{\"role\":\"user\",\"content\":\"Solve math\"}],"
        "\"thinking\":{\"type\":\"enabled\",\"budget_tokens\":6000}}");
    CHECK(wire_budget.ok());
    auto dec = oxa::anthropic::messages::decode_request(*wire_budget);
    CHECK(dec.ok());
    CHECK(dec->value.params.has_value() && dec->value.params->reasoning_effort == "medium");

    // Edit effort to low
    dec->value.params->reasoning_effort = "low";

    auto enc = oxa::anthropic::messages::encode_request(dec->value);
    CHECK(enc.ok());
    const auto* th = enc->value.find("thinking");
    CHECK(th != nullptr && th->is_object());
    const auto* th_b = th->find("budget_tokens");
    CHECK(th_b != nullptr && th_b->as_int() == 2048);

    bool found_effort_degraded = false;
    for (const auto& l : enc->losses) {
        if (l.field == "reasoning_effort" && l.reason == oxa::ir::LOSS_DEGRADED) {
            found_effort_degraded = true;
        }
    }
    CHECK(found_effort_degraded);
}

void test_native_unknown_enum_drops_unit() {
    auto wire = oxa::json::parse(
        "{\"model\":\"claude-3-7-sonnet-20250219\",\"max_tokens\":1000,"
        "\"messages\":[{\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":\"Hello\","
        "\"cache_control\":{\"type\":\"unknown_future_cache\"}}]}]}");
    CHECK(wire.ok());
    auto dec = oxa::anthropic::messages::decode_request(*wire);
    CHECK(dec.ok());
    bool found_unmapped = false;
    for (const auto& l : dec->losses) {
        if (l.field == "type" && l.reason == oxa::ir::LOSS_UNMAPPED_VALUE) {
            found_unmapped = true;
        }
    }
    CHECK(found_unmapped);
    const auto* text_blk = std::get_if<oxa::ir::TextBlock>(&dec->value.messages[0].content[0]);
    CHECK(text_blk != nullptr);
    CHECK(text_blk->native.empty());
}

void test_native_request_only() {
    oxa::ir::TextBlock blk;
    blk.text = "resp";
    oxa::ir::NativeAnnotation ann;
    ann.source = "anthropic";
    ann.kind = "cache_control";
    ann.value = "{\"type\":\"ephemeral\"}";
    blk.native.push_back(ann);

#if defined(__EXCEPTIONS) || defined(__cpp_exceptions)
    CHECK_THROWS(oxa::ir::dump_block(blk), "native");
#endif

    // Loading a response block carrying native must fail
    auto json_val = oxa::json::parse("{\"type\":\"text\",\"text\":\"resp\",\"native\":[{\"source\":\"anthropic\",\"kind\":\"cache_control\",\"value\":\"{}\"}]}");
    CHECK(json_val.ok());
    auto loaded = oxa::ir::load_block(*json_val);
    CHECK(!loaded.ok());
}

void test_native_omission_unknown_source() {
    oxa::ir::Request req;
    req.model = "model";
    req.messages.push_back(oxa::ir::Message{std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"hi"}}});

    oxa::ir::NativeAnnotation ann;
    ann.source = "unsupported_vendor";
    ann.kind = "custom_feature";
    ann.value = "\"value\"";
    req.native.push_back(ann);

    auto losses = oxa::ir::omitted_native_request_losses(req, "chatcompletions");
    CHECK(losses.ok());
    CHECK(losses->size() == 1);
    CHECK((*losses)[0].field == "source");
    CHECK((*losses)[0].reason == oxa::ir::LOSS_UNSUPPORTED_SEMANTIC);
}

void test_explicit_effort_only_encodes_to_output_config() {
    auto wire = oxa::json::parse(
        "{\"model\":\"claude-opus-5-5\",\"max_tokens\":16000,"
        "\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],"
        "\"output_config\":{\"effort\":\"low\"}}");
    CHECK(wire.ok());
    auto dec = oxa::anthropic::messages::decode_request(*wire);
    CHECK(dec.ok());
    CHECK(dec->losses.empty());
    CHECK(dec->value.params.has_value() && dec->value.params->reasoning_effort == "low");

    auto enc = oxa::anthropic::messages::encode_request(dec->value);
    CHECK(enc.ok());
    CHECK(enc->losses.empty());
    CHECK(enc->value.find("thinking") == nullptr);
    const auto* oc = enc->value.find("output_config");
    CHECK(oc != nullptr && oc->is_object());
    const auto* eff = oc->find("effort");
    CHECK(eff != nullptr && eff->as_string() == "low");
}

void test_context_management_recursive_audit() {
    auto wire = oxa::json::parse(
        "{\"model\":\"claude-opus-5-5\",\"max_tokens\":16000,"
        "\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],"
        "\"context_management\":{\"edits\":[{"
        "\"type\":\"clear_tool_uses_20250919\","
        "\"future_policy\":true,"
        "\"trigger\":{\"type\":\"input_tokens\",\"value\":30000,\"future_nested\":false}"
        "}]}}");
    CHECK(wire.ok());
    auto dec = oxa::anthropic::messages::decode_request(*wire);
    CHECK(dec.ok());
    bool found_fp = false;
    bool found_fn = false;
    for (const auto& l : dec->losses) {
        if (l.field == "future_policy" && l.reason == oxa::ir::LOSS_UNMAPPED_FIELD) found_fp = true;
        if (l.field == "future_nested" && l.reason == oxa::ir::LOSS_UNMAPPED_FIELD) found_fn = true;
    }
    CHECK(found_fp);
    CHECK(found_fn);

    auto enc = oxa::anthropic::messages::encode_request(dec->value);
    CHECK(enc.ok());
    const auto* cm = enc->value.find("context_management");
    CHECK(cm != nullptr && cm->is_object());
    const auto* edits = cm->find("edits");
    CHECK(edits != nullptr && edits->is_array() && edits->as_array().size() == 1);
    const auto& edit = edits->as_array()[0];
    CHECK(edit.find("future_policy") == nullptr);
    const auto* trig = edit.find("trigger");
    CHECK(trig != nullptr && trig->is_object());
    CHECK(trig->find("future_nested") == nullptr);
}

void test_response_native_isolation() {
    auto wire = oxa::json::parse(
        "{\"id\":\"r1\",\"model\":\"claude-opus-5-5\",\"stop_reason\":\"end_turn\","
        "\"content\":[{\"type\":\"text\",\"text\":\"hi\",\"cache_control\":{\"type\":\"ephemeral\"}}],"
        "\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}");
    CHECK(wire.ok());
    auto dec = oxa::anthropic::messages::decode_response(*wire);
    CHECK(dec.ok());
    bool found_cc = false;
    for (const auto& l : dec->losses) {
        if (l.field == "cache_control" && l.reason == oxa::ir::LOSS_UNMAPPED_FIELD) found_cc = true;
    }
    CHECK(found_cc);

    // Response blocks must have no native annotations
    const auto* tb = std::get_if<oxa::ir::TextBlock>(&dec->value.content[0]);
    CHECK(tb != nullptr);
    CHECK(tb->native.empty());

    // Must dump without throwing
    auto dumped = oxa::ir::dump_response(dec->value);
    auto ser = oxa::json::serialize(dumped);
    CHECK(!ser.empty());
}

void test_custom_tool_result_nested_cache_control() {
    auto wire = oxa::json::parse(
        "{\"model\":\"claude-opus-5-5\",\"max_tokens\":16000,"
        "\"tools\":[{\"name\":\"f\",\"input_schema\":{\"type\":\"object\",\"properties\":{\"input\":{\"type\":\"string\"}},\"required\":[\"input\"]}}],"
        "\"messages\":["
        "{\"role\":\"user\",\"content\":\"hi\"},"
        "{\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"c1\",\"name\":\"f\",\"input\":{\"input\":\"run\"}}]},"
        "{\"role\":\"user\",\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"c1\",\"content\":[{\"type\":\"text\",\"text\":\"result\",\"cache_control\":{\"type\":\"ephemeral\"}}]}]}"
        "]}");
    CHECK(wire.ok());
    auto dec = oxa::anthropic::messages::decode_request(*wire);
    CHECK(dec.ok());
    auto ir_json = oxa::json::serialize(oxa::ir::dump_request(dec->value));
    auto parsed_ir = oxa::json::parse(ir_json);
    CHECK(parsed_ir.ok());
    auto reloaded = oxa::ir::load_request(*parsed_ir);
    CHECK(reloaded.ok());
    auto enc = oxa::anthropic::messages::encode_request(*reloaded);
    CHECK(enc.ok());
    const auto* msgs = enc->value.find("messages");
    CHECK(msgs != nullptr && msgs->is_array());
    const auto& user_msg = msgs->as_array()[2];
    const auto* content = user_msg.find("content");
    CHECK(content != nullptr && content->is_array());
    const auto& tr = content->as_array()[0];
    const auto* inner_content = tr.find("content");
    CHECK(inner_content != nullptr && inner_content->is_array());
    const auto& text_blk = inner_content->as_array()[0];
    CHECK(text_blk.find("cache_control") != nullptr);
}

void test_cache_control_omission_loss_detail() {
    auto wire = oxa::json::parse(
        "{\"model\":\"claude-opus-5-5\",\"max_tokens\":64000,"
        "\"system\":[{\"type\":\"text\",\"text\":\"S\",\"cache_control\":{\"type\":\"ephemeral\"}}],"
        "\"tools\":[{\"name\":\"f\",\"description\":\"d\",\"input_schema\":{\"type\":\"object\"},\"cache_control\":{\"type\":\"ephemeral\"}}],"
        "\"messages\":[{\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":\"go\",\"cache_control\":{\"type\":\"ephemeral\"}}]}]}");
    CHECK(wire.ok());
    auto dec = oxa::anthropic::messages::decode_request(*wire);
    CHECK(dec.ok());
    CHECK(dec->losses.empty());

    auto cc = oxa::openai::chatcompletions::encode_request(dec->value);
    CHECK(cc.ok());
    CHECK(cc->losses.size() == 3);
    for (const auto& l : cc->losses) {
        CHECK(l.field == "cache_control");
        CHECK(l.reason == oxa::ir::LOSS_UNMAPPED_FIELD);
        CHECK(l.detail == oxa::ir::CACHE_CONTROL_LOSS_DETAIL);
    }

    auto resp = oxa::openai::responses::encode_request(dec->value);
    CHECK(resp.ok());
    CHECK(resp->losses.size() == 3);
    for (const auto& l : resp->losses) {
        CHECK(l.detail == oxa::ir::CACHE_CONTROL_LOSS_DETAIL);
    }

    // Directly constructed IR without omission
    oxa::ir::Request req_no_om;
    req_no_om.model = "claude-opus-5-5";
    req_no_om.native.push_back(oxa::ir::NativeAnnotation{
        std::string(oxa::ir::NATIVE_SOURCE_ANTHROPIC),
        std::string(oxa::ir::NATIVE_KIND_CACHE_CONTROL),
        "{\"type\":\"ephemeral\"}",
        std::nullopt,
        std::nullopt,
    });
    req_no_om.messages.push_back(oxa::ir::Message{std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"hi"}}});
    auto cc_no_om = oxa::openai::chatcompletions::encode_request(req_no_om);
    CHECK(cc_no_om.ok());
    CHECK(cc_no_om->losses.size() == 1);
    CHECK(cc_no_om->losses[0].field == "cache_control");
    CHECK(cc_no_om->losses[0].reason == oxa::ir::LOSS_UNMAPPED_FIELD);
    CHECK(cc_no_om->losses[0].detail == oxa::ir::CACHE_CONTROL_LOSS_DETAIL);

    // Directly constructed IR with empty detail
    oxa::ir::Request req_empty_detail;
    req_empty_detail.model = "claude-opus-5-5";
    req_empty_detail.native.push_back(oxa::ir::NativeAnnotation{
        std::string(oxa::ir::NATIVE_SOURCE_ANTHROPIC),
        std::string(oxa::ir::NATIVE_KIND_CACHE_CONTROL),
        "{\"type\":\"ephemeral\"}",
        oxa::ir::NativeOmission{"cache_control", std::string(oxa::ir::LOSS_UNMAPPED_FIELD), std::nullopt},
        std::nullopt,
    });
    req_empty_detail.messages.push_back(oxa::ir::Message{std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"hi"}}});
    auto cc_empty = oxa::openai::chatcompletions::encode_request(req_empty_detail);
    CHECK(cc_empty.ok());
    CHECK(cc_empty->losses.size() == 1);
    CHECK(cc_empty->losses[0].detail == oxa::ir::CACHE_CONTROL_LOSS_DETAIL);
}

void test_fingerprint_losses() {
    std::vector<oxa::ir::Loss> losses = {
        oxa::ir::Loss{"tools[0]", "eager_input_streaming", std::string(oxa::ir::LOSS_DEGRADED), "sensitive1"},
        oxa::ir::Loss{"native[1]", "store", std::string(oxa::ir::LOSS_UNSUPPORTED_SEMANTIC), "sensitive2"},
        oxa::ir::Loss{"stream_options", "extra", std::string(oxa::ir::LOSS_UNMAPPED_FIELD), "sensitive3"},
    };

    auto all = oxa::ir::fingerprint_losses(losses, 0);
    CHECK(all.size() == 3);
    CHECK(all[0].path == "tools[0]");
    CHECK(all[0].field == "eager_input_streaming");
    CHECK(all[0].reason == oxa::ir::LOSS_DEGRADED);

    auto trunc = oxa::ir::fingerprint_losses(losses, 2);
    CHECK(trunc.size() == 2);
    CHECK(trunc[1].field == "store");
}

void test_eager_input_streaming_32_tools_and_validation() {
    // 32 tools carrying eager_input_streaming: true produce 0 losses
    oxa::json::Value tools = oxa::json::Value::array();
    for (int i = 0; i < 32; ++i) {
        oxa::json::Value t = oxa::json::Value::object();
        t.set("name", oxa::json::Value::string("tool_" + std::to_string(i)));
        t.set("input_schema", oxa::json::Value::object());
        t.set("eager_input_streaming", oxa::json::Value::boolean(true));
        tools.push_back(std::move(t));
    }
    oxa::json::Value wire = oxa::json::Value::object();
    wire.set("model", oxa::json::Value::string("claude-opus-5-5"));
    wire.set("max_tokens", oxa::json::Value::integer(1024));
    oxa::json::Value msgs = oxa::json::Value::array();
    oxa::json::Value msg = oxa::json::Value::object();
    msg.set("role", oxa::json::Value::string("user"));
    msg.set("content", oxa::json::Value::string("hi"));
    msgs.push_back(std::move(msg));
    wire.set("messages", std::move(msgs));
    wire.set("tools", std::move(tools));

    auto dec = oxa::anthropic::messages::decode_request(wire);
    CHECK(dec.ok());
    CHECK(dec->losses.empty());
    CHECK(dec->value.tools.has_value() && dec->value.tools->size() == 32);
    CHECK((*dec->value.tools)[0].native.size() == 1);
    CHECK((*dec->value.tools)[0].native[0].kind == oxa::ir::NATIVE_KIND_EAGER_INPUT_STREAMING);

    // Non-boolean fails
    oxa::json::Value bad_wire = oxa::json::Value::object();
    bad_wire.set("model", oxa::json::Value::string("claude-opus-5-5"));
    bad_wire.set("max_tokens", oxa::json::Value::integer(1024));
    bad_wire.set("messages", oxa::json::Value::array());
    oxa::json::Value bad_tools = oxa::json::Value::array();
    oxa::json::Value bad_t = oxa::json::Value::object();
    bad_t.set("name", oxa::json::Value::string("t1"));
    bad_t.set("input_schema", oxa::json::Value::object());
    bad_t.set("eager_input_streaming", oxa::json::Value::string("true"));
    bad_tools.push_back(std::move(bad_t));
    bad_wire.set("tools", std::move(bad_tools));

    auto bad_dec = oxa::anthropic::messages::decode_request(bad_wire);
    CHECK(!bad_dec.ok());
}

}  // namespace

int main() {
    test_native_portable_round_trip();
    test_validate_native_request();
    test_native_thinking_combinations();
    test_native_edited_effort_wins();
    test_native_unknown_enum_drops_unit();
    test_native_request_only();
    test_native_omission_unknown_source();
    test_explicit_effort_only_encodes_to_output_config();
    test_context_management_recursive_audit();
    test_response_native_isolation();
    test_custom_tool_result_nested_cache_control();
    test_cache_control_omission_loss_detail();
    test_fingerprint_losses();
    test_eager_input_streaming_32_tools_and_validation();

    std::printf("test_native_request: all tests passed\n");
    return 0;
}
