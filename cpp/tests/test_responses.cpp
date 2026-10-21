#include "oxa/openai/responses.hpp"
#include "oxa/vectest.hpp"
#include "test_util.hpp"

class ResponsesConverter : public oxa::vectest::Converter {
public:
    std::string_view face() const override {
        return "responses";
    }

    oxa::StatusOr<oxa::Conversion<oxa::ir::Request>> decode_request(
        const oxa::json::Value& wire) const override {
        return oxa::openai::responses::decode_request(wire);
    }

    oxa::StatusOr<oxa::Conversion<oxa::ir::Response>> decode_response(
        const oxa::json::Value& wire) const override {
        return oxa::openai::responses::decode_response(wire);
    }

    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_request(
        const oxa::ir::Request& req) const override {
        return oxa::openai::responses::encode_request(req);
    }

    oxa::StatusOr<oxa::Conversion<oxa::json::Value>> encode_response(
        const oxa::ir::Response& resp) const override {
        return oxa::openai::responses::encode_response(resp);
    }
};

int main() {
    ResponsesConverter conv;
    auto rep_res = oxa::vectest::run_nonstream(conv);
    CHECK_MSG(rep_res.ok(), rep_res.status().to_string());
    if (!rep_res->failures.empty()) {
        for (const auto& f : rep_res->failures) {
            std::fprintf(stderr, "FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
        }
    }
    CHECK(rep_res->failures.empty());
    CHECK(rep_res->executed == 131);

    auto wire = oxa::json::parse(R"({
        "model":"gpt-4o",
        "input":"json",
        "text":{"format":{"type":"json_schema","name":"result",
            "schema":{"type":"object","x-order":[2,1]},"strict":false}}
    })");
    CHECK(wire.ok());
    auto decoded = oxa::openai::responses::decode_request(*wire);
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
        auto encoded = oxa::openai::responses::encode_request(decoded->value);
        CHECK(encoded.ok());
        if (encoded.ok()) CHECK(oxa::json::structurally_equal(*wire, encoded->value));
    }

    // Edge cases: missing/invalid response items
    const char* bad_responses[] = {
        R"({"id":"r","model":"m","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file"}}]})",
        R"({"id":"r","model":"m","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file","path":null}}]})",
        R"({"id":"r","model":"m","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"create_file","path":"a"}}]})",
        R"({"id":"r","model":"m","status":"completed","output":[{"id":"apc","type":"apply_patch_call","status":"completed","operation":{"type":"delete_file","path":"a"}}]})",
        R"({"id":"r","model":"m","status":"completed","output":[{"type":"apply_patch_call","call_id":"c","status":"completed","operation":{"type":"delete_file","path":"a"}}]})",
        R"({"id":"r","model":"m","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":"completed"}]})",
        R"({"id":"r","model":"m","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","operation":{"type":"delete_file","path":"a"}}]})",
    };
    for (const auto* br : bad_responses) {
        auto r = oxa::openai::responses::decode_response(br);
        CHECK(!r.ok());
    }

    // Edge cases: request association errors
    const char* bad_requests[] = {
        R"({"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call","call_id":"same","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call","call_id":"same","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call_output","call_id":"same","status":"completed"},{"type":"apply_patch_call_output","call_id":"same","status":"completed"}]})",
        R"({"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call","call_id":"a","status":"completed","operation":{"type":"future"}},{"type":"apply_patch_call_output","call_id":"b","status":"future"}]})",
        R"({"model":"m","input":[{"role":"user","content":"hi"},{"type":"apply_patch_call_output","call_id":"c","status":"completed"}]})",
    };
    for (const auto* brq : bad_requests) {
        auto r = oxa::openai::responses::decode_request(brq);
        CHECK(!r.ok());
    }

    // Edge cases: streaming unknown op/status with loss
    {
        const char* scenarios[][2] = {
            {R"({"type":"future","extra":true})", "completed"},
            {R"({"type":"delete_file","path":"a"})", "future"},
            {R"({"type":"delete_file","path":"a","diff":42})", "completed"},
        };
        for (const auto& sc : scenarios) {
            oxa::openai::responses::StreamDecoder d;
            CHECK(d.feed(R"({"type":"response.created","response":{"id":"r","model":"m","status":"in_progress","output":[]}})").ok());
            std::string add_ev = std::string(R"({"type":"response.output_item.added","output_index":0,"item":{"id":"apc","type":"apply_patch_call","call_id":"c","status":"in_progress","operation":)") + sc[0] + "}}";
            CHECK(d.feed(add_ev).ok());
            std::string done_ev = std::string(R"({"type":"response.output_item.done","output_index":0,"item":{"id":"apc","type":"apply_patch_call","call_id":"c","status":")") + sc[1] + std::string(R"(","operation":)") + sc[0] + "}}";
            auto done_res = d.feed(done_ev);
            CHECK(done_res.ok());
            if (std::string(sc[1]) == "future" || std::string(sc[0]).find("future") != std::string::npos) {
                CHECK(done_res->empty());
            }
            std::string comp_ev = std::string(R"({"type":"response.completed","response":{"id":"r","model":"m","status":"completed","output":[{"id":"apc","type":"apply_patch_call","call_id":"c","status":")") + sc[1] + std::string(R"(","operation":)") + sc[0] + "}]}}";
            auto comp_res = d.feed(comp_ev);
            CHECK_MSG(comp_res.ok(), std::string(comp_res.message()));
            CHECK(d.flush().ok());
            CHECK(!d.losses().empty());
        }
    }

    // Edge case: rejecting content/delta event on active apply_patch item
    {
        oxa::openai::responses::StreamDecoder d;
        CHECK(d.feed(R"({"type":"response.created","response":{"id":"r","model":"m","status":"in_progress","output":[]}})").ok());
        CHECK(d.feed(R"({"type":"response.output_item.added","output_index":0,"item":{"id":"apc","type":"apply_patch_call","call_id":"c","status":"in_progress","operation":{"type":"delete_file","path":"a"}}})").ok());
        auto bad_delta = d.feed(R"({"type":"response.output_text.delta","output_index":0,"item_id":"apc","content_index":0,"delta":"unexpected"})");
        CHECK(!bad_delta.ok());
    }

    std::printf("test_responses: all %zu vectors passed\n", rep_res->executed);
    return 0;
}
