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
    AnthropicConverter conv;
    auto rep_res = oxa::vectest::run_nonstream(conv);
    CHECK_MSG(rep_res.ok(), rep_res.status().to_string());
    if (!rep_res->failures.empty()) {
        for (const auto& f : rep_res->failures) {
            std::fprintf(stderr, "FAIL: %s: %s\n", f.vector_name.c_str(), f.message.c_str());
        }
    }
    CHECK(rep_res->failures.empty());
    CHECK(rep_res->executed == 30);
    std::printf("test_anthropic: all %zu vectors passed\n", rep_res->executed);
    return 0;
}
