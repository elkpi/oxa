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

    // 4. Unit tests for stream decoder/encoder edge cases & grammar checks
    {
        // Anthropic StreamDecoder unknown block skipping & loss
        {
            oxa::anthropic::messages::StreamDecoder decoder;
            const char* chunks[] = {
                R"({"type":"message_start","message":{"id":"m","model":"claude","usage":{"input_tokens":1,"output_tokens":0}}})",
                R"({"type":"content_block_start","index":0,"content_block":{"type":"thinking"}})",
                R"({"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}})",
                R"({"type":"content_block_stop","index":0})",
                R"({"type":"content_block_start","index":1,"content_block":{"type":"text","text":"hello"}})",
                R"({"type":"content_block_stop","index":1})",
                R"({"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}})",
                R"({"type":"message_stop"})"
            };
            std::vector<oxa::ir::Event> all_events;
            for (const char* text : chunks) {
                auto parsed = oxa::json::parse(text);
                CHECK(parsed.ok());
                auto evs = decoder.feed(*parsed);
                CHECK(evs.ok());
                all_events.insert(all_events.end(), evs->begin(), evs->end());
            }
            // Should have skipped thinking, emitted text at IR index 0, and recorded 1 loss
            CHECK(decoder.losses().size() == 1);
            CHECK(decoder.losses()[0].reason == oxa::ir::LOSS_UNSUPPORTED_SEMANTIC);
            // 4 events: MessageStart, ContentBlockStart(index 0, text), ContentBlockStop(index 0), MessageDelta, MessageDone
            CHECK(all_events.size() == 5);
            CHECK(std::holds_alternative<oxa::ir::MessageStart>(all_events[0]));
            CHECK(std::holds_alternative<oxa::ir::ContentBlockStart>(all_events[1]));
            CHECK(std::get<oxa::ir::ContentBlockStart>(all_events[1]).index == 0);
        }

        // Anthropic StreamDecoder empty {} tool input fallback without fragments
        {
            oxa::anthropic::messages::StreamDecoder decoder;
            const char* chunks[] = {
                R"({"type":"message_start","message":{"id":"m","model":"claude","usage":{"input_tokens":1,"output_tokens":0}}})",
                R"({"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"get_time","input":{}}})",
                R"({"type":"content_block_stop","index":0})",
                R"({"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":1,"output_tokens":1}})",
                R"({"type":"message_stop"})"
            };
            std::vector<oxa::ir::Event> all_events;
            for (const char* text : chunks) {
                auto parsed = oxa::json::parse(text);
                CHECK(parsed.ok());
                auto evs = decoder.feed(*parsed);
                CHECK(evs.ok());
                all_events.insert(all_events.end(), evs->begin(), evs->end());
            }
            bool found_tool = false;
            for (const auto& ev : all_events) {
                if (const auto* cbs = std::get_if<oxa::ir::ContentBlockStart>(&ev)) {
                    if (const auto* tu = std::get_if<oxa::ir::ToolUseBlock>(&cbs->block)) {
                        CHECK(tu->input == "{}");
                        found_tool = true;
                    }
                }
            }
            CHECK(found_tool);
        }

        // StreamEncoder grammar error rejection: delta before start
        {
            oxa::ir::ContentBlockDelta delta{0, oxa::ir::TextDelta{"x"}};
            oxa::openai::chatcompletions::StreamEncoder cc_enc;
            CHECK(!cc_enc.apply(delta).ok());

            oxa::anthropic::messages::StreamEncoder ant_enc;
            CHECK(!ant_enc.apply(delta).ok());

            oxa::openai::responses::StreamEncoder resp_enc;
            CHECK(!resp_enc.apply(delta).ok());
        }
    }

    std::puts("test_stream: all 8 stream vectors passed");
    return 0;
}
