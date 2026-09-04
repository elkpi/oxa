#include "oxa/anthropic/messages.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/openai/responses.hpp"
#include "oxa/vectest.hpp"
#include "test_util.hpp"

class ChatCompletionsStreamConverter : public oxa::vectest::StreamConverter {
public:
    std::string_view face() const override { return "chatcompletions"; }
    void reset_stream_vector() override {
        decoder_ = oxa::openai::chatcompletions::StreamDecoder();
        encoder_ = oxa::openai::chatcompletions::StreamEncoder();
    }
    oxa::StatusOr<std::vector<oxa::ir::Event>> decode_native_event(const oxa::json::Value& event) override {
        return decoder_.feed(event);
    }
    oxa::StatusOr<std::vector<oxa::ir::Event>> flush_decoder() override {
        return decoder_.flush();
    }
    std::vector<oxa::ir::Loss> decoder_losses() const override {
        return decoder_.losses();
    }
    oxa::StatusOr<oxa::Conversion<std::vector<oxa::json::Value>>> apply_ir_event(const oxa::ir::Event& event) override {
        return encoder_.apply(event);
    }

private:
    oxa::openai::chatcompletions::StreamDecoder decoder_;
    oxa::openai::chatcompletions::StreamEncoder encoder_;
};

class AnthropicStreamConverter : public oxa::vectest::StreamConverter {
public:
    std::string_view face() const override { return "anthropic"; }
    void reset_stream_vector() override {
        decoder_ = oxa::anthropic::messages::StreamDecoder();
        encoder_ = oxa::anthropic::messages::StreamEncoder();
    }
    oxa::StatusOr<std::vector<oxa::ir::Event>> decode_native_event(const oxa::json::Value& event) override {
        return decoder_.feed(event);
    }
    oxa::StatusOr<std::vector<oxa::ir::Event>> flush_decoder() override {
        return decoder_.flush();
    }
    std::vector<oxa::ir::Loss> decoder_losses() const override {
        return decoder_.losses();
    }
    oxa::StatusOr<oxa::Conversion<std::vector<oxa::json::Value>>> apply_ir_event(const oxa::ir::Event& event) override {
        return encoder_.apply(event);
    }

private:
    oxa::anthropic::messages::StreamDecoder decoder_;
    oxa::anthropic::messages::StreamEncoder encoder_;
};

class ResponsesStreamConverter : public oxa::vectest::StreamConverter {
public:
    std::string_view face() const override { return "responses"; }
    void reset_stream_vector() override {
        decoder_ = oxa::openai::responses::StreamDecoder();
        encoder_ = oxa::openai::responses::StreamEncoder();
    }
    oxa::StatusOr<std::vector<oxa::ir::Event>> decode_native_event(const oxa::json::Value& event) override {
        return decoder_.feed(event);
    }
    oxa::StatusOr<std::vector<oxa::ir::Event>> flush_decoder() override {
        return decoder_.flush();
    }
    std::vector<oxa::ir::Loss> decoder_losses() const override {
        return decoder_.losses();
    }
    oxa::StatusOr<oxa::Conversion<std::vector<oxa::json::Value>>> apply_ir_event(const oxa::ir::Event& event) override {
        return encoder_.apply(event);
    }

private:
    oxa::openai::responses::StreamDecoder decoder_;
    oxa::openai::responses::StreamEncoder encoder_;
};

int main() {
    // 1. Chat Completions stream
    {
        ChatCompletionsStreamConverter cc;
        auto r = oxa::vectest::run_stream(cc);
        CHECK_MSG(r.ok(), r.status().to_string());
        if (!r->failures.empty()) {
            for (const auto& f : r->failures) {
                std::fprintf(stderr, "STREAM FAIL (CC): %s: %s\n", f.vector_name.c_str(), f.message.c_str());
            }
        }
        CHECK(r->failures.empty());
        CHECK(r->executed == 2);
        std::printf("test_stream (chatcompletions): all %zu vectors passed\n", r->executed);
    }

    // 2. Anthropic stream
    {
        AnthropicStreamConverter ant;
        auto r = oxa::vectest::run_stream(ant);
        CHECK_MSG(r.ok(), r.status().to_string());
        if (!r->failures.empty()) {
            for (const auto& f : r->failures) {
                std::fprintf(stderr, "STREAM FAIL (Ant): %s: %s\n", f.vector_name.c_str(), f.message.c_str());
            }
        }
        CHECK(r->failures.empty());
        CHECK(r->executed == 3);
        std::printf("test_stream (anthropic): all %zu vectors passed\n", r->executed);
    }

    // 3. Responses stream
    {
        ResponsesStreamConverter resp;
        auto r = oxa::vectest::run_stream(resp);
        CHECK_MSG(r.ok(), r.status().to_string());
        if (!r->failures.empty()) {
            for (const auto& f : r->failures) {
                std::fprintf(stderr, "STREAM FAIL (Resp): %s: %s\n", f.vector_name.c_str(), f.message.c_str());
            }
        }
        CHECK(r->failures.empty());
        CHECK(r->executed == 3);
        std::printf("test_stream (responses): all %zu vectors passed\n", r->executed);
    }

    std::puts("test_stream: all 8 stream vectors passed");
    return 0;
}
