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

}  // namespace

int main() {
    test_native_portable_round_trip();
    test_validate_native_request();
    test_native_thinking_combinations();
    test_native_edited_effort_wins();
    test_native_unknown_enum_drops_unit();
    test_native_request_only();
    test_native_omission_unknown_source();

    std::printf("test_native_request: all tests passed\n");
    return 0;
}
