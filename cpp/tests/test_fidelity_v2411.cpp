// v2.4.11 Anthropic fidelity tests: redacted_thinking history, between_tools
// thinking mode, top_k, disable_parallel_tool_use, and text citations
// (spec/12 N-AN-17/18/19, spec/15, spec/20).

#include <cassert>
#include <iostream>
#include <string>

#include "oxa/anthropic/messages.hpp"
#include "oxa/ir.hpp"
#include "oxa/json.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/openai/responses.hpp"

using namespace oxa;

namespace {

int failures = 0;

#define CHECK(cond)                                                    \
    do {                                                               \
        if (!(cond)) {                                                 \
            std::cerr << "FAIL " << __FILE__ << ":" << __LINE__ << ": " \
                      << #cond << "\n";                                \
            ++failures;                                                \
        }                                                              \
    } while (0)

#define CHECK_EQ(a, b)                                                  \
    do {                                                                \
        auto va_ = (a);                                                 \
        auto vb_ = (b);                                                 \
        if (!(va_ == vb_)) {                                            \
            std::cerr << "FAIL " << __FILE__ << ":" << __LINE__ << ": " \
                      << #a << " != " << #b << "\n";                    \
            ++failures;                                                 \
        }                                                               \
    } while (0)

bool has_loss(const std::vector<ir::Loss>& losses, std::string_view path,
              std::string_view field, std::string_view reason,
              std::string_view detail_substring) {
    for (const auto& l : losses) {
        if (l.path == path && l.field == field && l.reason == reason &&
            l.detail.find(detail_substring) != std::string::npos) {
            return true;
        }
    }
    return false;
}

}  // namespace

static void test_redacted_thinking_roundtrip() {
    const char* raw = R"({
        "model": "claude-opus-5-5",
        "max_tokens": 4096,
        "messages": [
            {"role": "user", "content": "Prior prompt."},
            {
                "role": "assistant",
                "content": [
                    {"type": "redacted_thinking", "data": "OPAQUE_ENCRYPTED_DATA"},
                    {"type": "text", "text": "Follow up."}
                ]
            },
            {"role": "user", "content": "Continue."}
        ]
    })";

    anthropic::messages::Options opts;
    auto dec = anthropic::messages::decode_request(raw, opts);
    CHECK(dec.ok());
    if (!dec.ok()) return;
    CHECK(dec->losses.empty());

    const auto& asst = dec->value.messages[1].content;
    CHECK_EQ(asst.size(), std::size_t{2});
    const auto* rt = std::get_if<ir::RedactedThinkingBlock>(&asst[0]);
    CHECK(rt != nullptr);
    if (rt) CHECK_EQ(rt->data, std::string("OPAQUE_ENCRYPTED_DATA"));

    auto enc = anthropic::messages::encode_request(dec->value, opts);
    CHECK(enc.ok());
    if (!enc.ok()) return;
    CHECK(enc->losses.empty());
    auto serialized = json::serialize(enc->value);
    CHECK(serialized.find("\"type\":\"redacted_thinking\"") != std::string::npos);
    CHECK(serialized.find("\"data\":\"OPAQUE_ENCRYPTED_DATA\"") != std::string::npos);
}

static void test_between_tools_roundtrip_and_conflict() {
    const char* raw = R"({
        "model": "claude-sonnet-5-5",
        "max_tokens": 4096,
        "thinking": {"type": "between_tools"},
        "messages": [{"role": "user", "content": "Hello"}]
    })";

    anthropic::messages::Options opts;
    auto dec = anthropic::messages::decode_request(raw, opts);
    CHECK(dec.ok());
    if (!dec.ok()) return;
    CHECK(dec->losses.empty());

    bool found_mode = false;
    for (const auto& ann : dec->value.native) {
        if (ann.kind == ir::NATIVE_KIND_THINKING_MODE && ann.value == "\"between_tools\"") {
            found_mode = true;
        }
    }
    CHECK(found_mode);

    auto enc = anthropic::messages::encode_request(dec->value, opts);
    CHECK(enc.ok());
    if (enc.ok()) {
        CHECK(enc->losses.empty());
        const auto* th = enc->value.find("thinking");
        CHECK(th != nullptr);
        if (th) {
            const auto* tv = th->find("type");
            CHECK(tv && tv->is_string() && tv->as_string() == "between_tools");
        }
    }

    const char* conflict = R"({
        "model": "claude-sonnet-5-5",
        "max_tokens": 4096,
        "thinking": {"type": "between_tools", "budget_tokens": 2048},
        "messages": [{"role": "user", "content": "Hello"}]
    })";
    auto bad = anthropic::messages::decode_request(conflict, opts);
    CHECK(!bad.ok());
}

static void test_top_k_roundtrip() {
    anthropic::messages::Options opts;
    for (std::int64_t k : {0, 40, 100}) {
        std::string raw = std::string(R"({
                "model": "claude-opus-4-6",
                "max_tokens": 1024,
                "top_k": )" + std::to_string(k) + R"(,
                "messages": [{"role": "user", "content": "Hello"}]
            })");
        auto dec = anthropic::messages::decode_request(raw, opts);
        CHECK(dec.ok());
        if (!dec.ok()) continue;
        CHECK(dec->losses.empty());
        bool found = false;
        for (const auto& ann : dec->value.native) {
            if (ann.kind == ir::NATIVE_KIND_TOP_K && ann.value == std::to_string(k)) {
                found = true;
            }
        }
        CHECK(found);

        auto enc = anthropic::messages::encode_request(dec->value, opts);
        CHECK(enc.ok());
        if (!enc.ok()) continue;
        CHECK(enc->losses.empty());
        const auto* tk = enc->value.find("top_k");
        CHECK(tk != nullptr && tk->is_int() && tk->as_int() == k);
    }
}

static void test_disable_parallel_tool_use_roundtrip_and_none() {
    anthropic::messages::Options opts;
    for (bool flag : {true, false}) {
        std::string raw = std::string(R"({
                "model": "claude-sonnet-4-5",
                "max_tokens": 1024,
                "messages": [{"role": "user", "content": "Hello"}],
                "tools": [{"name": "t1", "input_schema": {"type": "object"}}],
                "tool_choice": {"type": "auto", "disable_parallel_tool_use": )" +
                                      (flag ? std::string("true") : std::string("false")) + R"(}
            })");
        auto dec = anthropic::messages::decode_request(raw, opts);
        CHECK(dec.ok());
        if (!dec.ok()) continue;
        CHECK(dec->losses.empty());
        std::string want = flag ? "true" : "false";
        bool found = false;
        for (const auto& ann : dec->value.native) {
            if (ann.kind == ir::NATIVE_KIND_DISABLE_PARALLEL_TOOL_USE && ann.value == want) {
                found = true;
            }
        }
        CHECK(found);

        auto enc = anthropic::messages::encode_request(dec->value, opts);
        CHECK(enc.ok());
        if (!enc.ok()) continue;
        CHECK(enc->losses.empty());
        const auto* tc = enc->value.find("tool_choice");
        CHECK(tc != nullptr);
        if (tc) {
            const auto* dpu = tc->find("disable_parallel_tool_use");
            CHECK(dpu != nullptr && dpu->is_bool() && dpu->as_bool() == flag);
        }
    }

    const char* none_raw = R"({
        "model": "claude-sonnet-4-5",
        "max_tokens": 1024,
        "messages": [{"role": "user", "content": "Hello"}],
        "tools": [{"name": "t1", "input_schema": {"type": "object"}}],
        "tool_choice": {"type": "none", "disable_parallel_tool_use": true}
    })";
    auto bad = anthropic::messages::decode_request(none_raw, opts);
    CHECK(!bad.ok());
}

static void test_text_citations_roundtrip() {
    const char* raw = R"({
        "id": "msg_cite",
        "type": "message",
        "role": "assistant",
        "model": "claude-sonnet-4-6",
        "content": [
            {
                "type": "text",
                "text": "Summary.",
                "citations": [
                    {
                        "type": "char_location",
                        "cited_text": "quote",
                        "document_index": 0,
                        "document_title": "Doc",
                        "start_char_index": 0,
                        "end_char_index": 5,
                        "file_id": "file_1"
                    }
                ]
            }
        ],
        "stop_reason": "end_turn",
        "usage": {"input_tokens": 10, "output_tokens": 10}
    })";

    anthropic::messages::Options opts;
    auto dec = anthropic::messages::decode_response(raw, opts);
    CHECK(dec.ok());
    if (!dec.ok()) return;
    CHECK(dec->losses.empty());
    CHECK_EQ(dec->value.content.size(), std::size_t{1});
    const auto* tb = std::get_if<ir::TextBlock>(&dec->value.content[0]);
    CHECK(tb != nullptr);
    if (tb) {
        CHECK(tb->citations.has_value() && tb->citations->size() == 1);
        if (tb->citations.has_value() && !tb->citations->empty()) {
            CHECK_EQ(tb->citations->at(0).source, std::string("anthropic"));
        }
    }

    auto enc = anthropic::messages::encode_response(dec->value, opts);
    CHECK(enc.ok());
    if (!enc.ok()) return;
    CHECK(enc->losses.empty());
    auto serialized = json::serialize(enc->value);
    CHECK(serialized.find("\"char_location\"") != std::string::npos);
    CHECK(serialized.find("\"file_1\"") != std::string::npos);
}

static void test_ir_citation_delta_roundtrip() {
    // Streaming citation_delta round-trips through the IR event codec and
    // passes the INV-5 checker as a text-block delta.
    ir::TextCitation cite;
    cite.source = "anthropic";
    cite.value = R"({"type":"char_location","cited_text":"q","document_index":0,"document_title":null,"start_char_index":0,"end_char_index":1})";
    std::vector<ir::Event> events;
    events.push_back(ir::MessageStart{"msg_1", "model"});
    events.push_back(ir::ContentBlockStart{0, ir::TextBlock{""}});
    events.push_back(ir::ContentBlockDelta{0, ir::CitationDelta{cite}});
    events.push_back(ir::ContentBlockStop{0});
    events.push_back(ir::MessageDelta{std::string(ir::STOP_END_TURN), std::nullopt, ir::Usage{}});
    events.push_back(ir::MessageDone{});

    CHECK(ir::validate_event_stream(events).ok());

    json::Value doc = ir::dump_event_stream(events);
    auto loaded = ir::load_event_stream(doc);
    CHECK(loaded.ok());
    if (!loaded.ok()) return;
    CHECK_EQ(loaded->size(), events.size());
    const auto* delta = std::get_if<ir::ContentBlockDelta>(&(*loaded)[2]);
    CHECK(delta != nullptr);
    if (delta) {
        const auto* cite_delta = std::get_if<ir::CitationDelta>(&delta->delta);
        CHECK(cite_delta != nullptr);
        if (cite_delta) {
            CHECK_EQ(cite_delta->citation.source, std::string("anthropic"));
            CHECK_EQ(cite_delta->citation.value, cite.value);
        }
    }
}

static void test_cross_face_redacted_losses() {
    ir::Request req;
    req.model = "claude-opus-5-5";
    req.messages.push_back(ir::Message{"user", {ir::TextBlock{"Question."}}});
    req.messages.push_back(ir::Message{
        "assistant",
        {ir::RedactedThinkingBlock{"OPAQUE_TEST"}, ir::TextBlock{"Answer."}}});
    req.messages.push_back(ir::Message{"user", {ir::TextBlock{"Followup."}}});

    openai::chatcompletions::Options cc_opts;
    auto cc = openai::chatcompletions::encode_request(req, cc_opts);
    CHECK(cc.ok());
    if (cc.ok()) {
        CHECK(has_loss(cc->losses, "messages[1].content[0]", "type",
                       ir::LOSS_UNSUPPORTED_SEMANTIC, "encrypted thinking"));
    }

    openai::responses::Options re_opts;
    auto re = openai::responses::encode_request(req, re_opts);
    CHECK(re.ok());
    if (re.ok()) {
        bool found = false;
        for (const auto& l : re->losses) {
            if (l.reason == ir::LOSS_UNSUPPORTED_SEMANTIC &&
                l.detail.find("encrypted thinking") != std::string::npos) {
                found = true;
            }
        }
        CHECK(found);
    }
}

int main() {
    test_redacted_thinking_roundtrip();
    test_between_tools_roundtrip_and_conflict();
    test_top_k_roundtrip();
    test_disable_parallel_tool_use_roundtrip_and_none();
    test_text_citations_roundtrip();
    test_ir_citation_delta_roundtrip();
    test_cross_face_redacted_losses();
    if (failures != 0) {
        std::cerr << failures << " assertion(s) failed\n";
        return 1;
    }
    std::cout << "All v2.4.11 fidelity tests passed.\n";
    return 0;
}
