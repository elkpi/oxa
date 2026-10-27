#include <stdexcept>

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
    auto conflicting_document = oxa::anthropic::messages::decode_request(
        R"({"model":"m","max_tokens":1,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"YQ==","url":""}}]}]})");
    CHECK(!conflicting_document.ok());

    {
        // An IR document with an empty url alongside file_id renders the file source.
        auto wire_res = oxa::json::parse(
            R"({"specVersion":"0.2.0","model":"m","params":{"max_tokens":1},"messages":[{"role":"user","content":[{"type":"text","text":"keep"},{"type":"document","file_id":"f","url":""}]}]})");
        CHECK(wire_res.ok());
        auto doc_req_res = oxa::ir::load_request(*wire_res);
        CHECK(doc_req_res.ok());
        auto encoded = oxa::anthropic::messages::encode_request(*doc_req_res);
        CHECK(encoded.ok());
        CHECK(encoded->losses.empty());
        const auto* content = encoded->value.find("messages");
        CHECK(content != nullptr && content->as_array().size() == 1);
        const auto& blocks = content->as_array()[0].find("content")->as_array();
        const auto* source = blocks[1].find("source");
        CHECK(source != nullptr);
        CHECK(source->find("type")->as_string() == "file");
        CHECK(source->find("file_id")->as_string() == "f");
    }
    {
        // A document block in a response position is dropped with exactly one loss.
        oxa::ir::Response resp;
        resp.id = "r";
        resp.model = "m";
        resp.stop_reason = std::string(oxa::ir::STOP_END_TURN);
        oxa::ir::DocumentBlock doc;
        doc.file_id = "f";
        resp.content.push_back(doc);
        auto encoded = oxa::anthropic::messages::encode_response(resp);
        CHECK(encoded.ok());
        CHECK(encoded->losses.size() == 1);
        CHECK(encoded->losses[0].reason == oxa::ir::LOSS_UNSUPPORTED_SEMANTIC);
        CHECK(encoded->value.find("content")->as_array().empty());
    }
#if defined(__EXCEPTIONS) || defined(__cpp_exceptions)
    {
        // dump_block (response side) refuses document blocks.
        oxa::ir::DocumentBlock doc;
        doc.file_id = "f";
        bool threw = false;
        try {
            static_cast<void>(oxa::ir::dump_block(oxa::ir::Block(doc)));
        } catch (const std::invalid_argument&) {
            threw = true;
        }
        CHECK(threw);
    }
#endif

    AnthropicConverter conv;
    auto rep_res = oxa::vectest::run_nonstream(conv);
    CHECK_MSG(rep_res.ok(), rep_res.status().to_string());
    if (!rep_res->failures.empty()) {
        for (const auto& f : rep_res->failures) {
            std::fprintf(stderr, "FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
        }
    }
    CHECK(rep_res->failures.empty());
    CHECK(rep_res->executed == 111);

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

        // Loss order: role loss precedes content-derived losses
        {
            auto order_wire = oxa::json::parse(R"({
                "model": "claude-opus-5-5", "max_tokens": 1024,
                "messages": [
                    {"role": "user", "content": "Hi"},
                    {"role": "system", "content": [
                        {"type": "text", "text": "x", "cache_control": {"type": "ephemeral", "ttl": "10m"}}
                    ]}
                ]
            })");
            CHECK(order_wire.ok());
            auto dec_order = oxa::anthropic::messages::decode_request(*order_wire);
            CHECK(dec_order.ok());
            if (dec_order.ok()) {
                CHECK(dec_order->losses.size() == 2);
                CHECK(dec_order->losses[0].path == "messages[1].role");
                CHECK(dec_order->losses[0].reason == oxa::ir::LOSS_DEGRADED);
                CHECK(dec_order->losses[1].path == "messages[1].content[0].cache_control.ttl");
                CHECK(dec_order->losses[1].reason == oxa::ir::LOSS_UNMAPPED_VALUE);
            }
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

    // Test defer_loading tool native option
    {
        // 1. defer_loading: true round-trip
        auto raw1 = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": true}]
        })");
        CHECK(raw1.ok());
        auto dec1 = oxa::anthropic::messages::decode_request(*raw1);
        CHECK(dec1.ok());
        if (dec1.ok()) {
            CHECK(dec1->losses.empty());
            CHECK(dec1->value.tools.has_value());
            CHECK(dec1->value.tools->size() == 1);
            CHECK((*dec1->value.tools)[0].native.size() == 1);
            CHECK((*dec1->value.tools)[0].native[0].kind == oxa::ir::NATIVE_KIND_DEFER_LOADING);
            CHECK((*dec1->value.tools)[0].native[0].value == "true");
            CHECK((*dec1->value.tools)[0].native[0].omission.has_value());
            CHECK((*dec1->value.tools)[0].native[0].omission->reason == oxa::ir::LOSS_DEGRADED);

            auto enc1 = oxa::anthropic::messages::encode_request(dec1->value);
            CHECK(enc1.ok());
            if (enc1.ok()) {
                CHECK(enc1->losses.empty());
                const auto* tools_arr = enc1->value.find("tools");
                CHECK(tools_arr != nullptr && tools_arr->is_array());
                const auto* t0 = tools_arr->as_array()[0].find("defer_loading");
                CHECK(t0 != nullptr && t0->is_bool() && t0->as_bool());
            }
        }

        // 2. defer_loading: false
        auto raw_false = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": false}]
        })");
        CHECK(raw_false.ok());
        auto dec_false = oxa::anthropic::messages::decode_request(*raw_false);
        CHECK(dec_false.ok());
        if (dec_false.ok()) {
            CHECK(dec_false->losses.empty());
            CHECK((*dec_false->value.tools)[0].native.empty());
        }

        // 3. conflict: defer_loading: true AND cache_control
        auto raw_conflict = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": true, "cache_control": {"type": "ephemeral"}}]
        })");
        CHECK(raw_conflict.ok());
        auto dec_conflict = oxa::anthropic::messages::decode_request(*raw_conflict);
        CHECK(!dec_conflict.ok());
        CHECK(dec_conflict.status().to_string().find("cannot specify both defer_loading and cache_control") != std::string::npos);

        // 4. non-boolean defer_loading
        auto raw_non_bool = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": "true"}]
        })");
        CHECK(raw_non_bool.ok());
        auto dec_non_bool = oxa::anthropic::messages::decode_request(*raw_non_bool);
        CHECK(!dec_non_bool.ok());
        CHECK(dec_non_bool.status().to_string().find("defer_loading must be a boolean") != std::string::npos);

        // 5. whitespace boolean annotation round-trips
        oxa::ir::Request req_whitespace;
        req_whitespace.model = "claude-opus-5-5";
        req_whitespace.params = oxa::ir::Params{};
        req_whitespace.params->max_tokens = 16000;
        req_whitespace.messages.push_back(oxa::ir::Message{std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"hi"}}});
        oxa::ir::Tool t_ws;
        t_ws.name = "t1";
        t_ws.input_schema = *oxa::json::parse(R"({"type":"object"})");
        t_ws.native.push_back(oxa::ir::NativeAnnotation{
            std::string(oxa::ir::NATIVE_SOURCE_ANTHROPIC),
            std::string(oxa::ir::NATIVE_KIND_DEFER_LOADING),
            " true \n",
            std::nullopt,
            std::nullopt,
        });
        req_whitespace.tools = std::vector<oxa::ir::Tool>{t_ws};
        auto val_ws = oxa::ir::validate_native_request(req_whitespace);
        CHECK(val_ws.ok());
        auto enc_ws = oxa::anthropic::messages::encode_request(req_whitespace);
        CHECK(enc_ws.ok());
        if (enc_ws.ok()) {
            CHECK(enc_ws->losses.empty());
            const auto* tools_arr = enc_ws->value.find("tools");
            CHECK(tools_arr != nullptr && tools_arr->is_array());
            const auto* t0 = tools_arr->as_array()[0].find("defer_loading");
            CHECK(t0 != nullptr && t0->is_bool() && t0->as_bool());
        }

        // 6. direct IR conflict: Tool with both cache_control and defer_loading
        oxa::ir::Request req_conflict;
        req_conflict.model = "claude-opus-5-5";
        req_conflict.params = oxa::ir::Params{};
        req_conflict.params->max_tokens = 16000;
        req_conflict.messages.push_back(oxa::ir::Message{std::string(oxa::ir::ROLE_USER), {oxa::ir::TextBlock{"hi"}}});
        oxa::ir::Tool t_conf;
        t_conf.name = "t1";
        t_conf.input_schema = *oxa::json::parse(R"({"type":"object"})");
        t_conf.native.push_back(oxa::ir::NativeAnnotation{
            std::string(oxa::ir::NATIVE_SOURCE_ANTHROPIC),
            std::string(oxa::ir::NATIVE_KIND_DEFER_LOADING),
            "true",
            std::nullopt,
            std::nullopt,
        });
        t_conf.native.push_back(oxa::ir::NativeAnnotation{
            std::string(oxa::ir::NATIVE_SOURCE_ANTHROPIC),
            std::string(oxa::ir::NATIVE_KIND_CACHE_CONTROL),
            R"({"type":"ephemeral"})",
            std::nullopt,
            std::nullopt,
        });
        req_conflict.tools = std::vector<oxa::ir::Tool>{t_conf};
        auto val_conf = oxa::ir::validate_native_request(req_conflict);
        CHECK(!val_conf.ok());
        CHECK(val_conf.to_string().find("cannot specify both defer_loading and cache_control") != std::string::npos);
        auto enc_conf = oxa::anthropic::messages::encode_request(req_conflict);
        CHECK(!enc_conf.ok());
        CHECK(enc_conf.status().to_string().find("cannot specify both defer_loading and cache_control") != std::string::npos);

        // 7. native tool declaration with non-boolean defer_loading is preserved opaquely
        auto raw_native = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"type": "future_20990101", "name": "future", "defer_loading": {"future": true}}]
        })");
        CHECK(raw_native.ok());
        auto dec_native = oxa::anthropic::messages::decode_request(*raw_native);
        CHECK(dec_native.ok());
        if (dec_native.ok()) {
            CHECK(dec_native->losses.empty());
            CHECK(dec_native->value.tools.has_value());
            CHECK((*dec_native->value.tools)[0].type == "native");
        }

        // 8. cache_control: null + defer_loading: true returns conflict error
        auto raw_null_cache = oxa::json::parse(R"({
            "model": "claude-opus-5-5", "max_tokens": 16000,
            "messages": [{"role": "user", "content": "hi"}],
            "tools": [{"name": "t1", "input_schema": {"type": "object"}, "defer_loading": true, "cache_control": null}]
        })");
        CHECK(raw_null_cache.ok());
        auto dec_null_cache = oxa::anthropic::messages::decode_request(*raw_null_cache);
        CHECK(!dec_null_cache.ok());
        CHECK(dec_null_cache.status().to_string().find("cannot specify both defer_loading and cache_control") != std::string::npos);
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
