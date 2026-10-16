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
        CHECK(r->executed == 6);
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
        CHECK(r->executed == 7);
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
        CHECK(r->executed == 23);
        std::printf("test_stream (responses): all %zu vectors passed\n", r->executed);
    }

    // 4. Unit tests for stream decoder/encoder edge cases & grammar checks
    {
        // Anthropic StreamDecoder maps the M9 thinking/signature lifecycle.
        {
            oxa::anthropic::messages::StreamDecoder decoder;
            const char* chunks[] = {
                R"({"type":"message_start","message":{"id":"m","model":"claude","usage":{"input_tokens":1,"output_tokens":0}}})",
                R"({"type":"content_block_start","index":0,"content_block":{"type":"thinking"}})",
                R"({"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}})",
                R"({"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}})",
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
            CHECK(decoder.losses().empty());
            CHECK(all_events.size() == 9);
            CHECK(std::holds_alternative<oxa::ir::ContentBlockStart>(all_events[1]));
            CHECK(std::holds_alternative<oxa::ir::ThinkingBlock>(
                std::get<oxa::ir::ContentBlockStart>(all_events[1]).block));
            CHECK(std::holds_alternative<oxa::ir::ThinkingDelta>(
                std::get<oxa::ir::ContentBlockDelta>(all_events[2]).delta));
            CHECK(std::holds_alternative<oxa::ir::SignatureDelta>(
                std::get<oxa::ir::ContentBlockDelta>(all_events[3]).delta));
            CHECK(std::get<oxa::ir::ContentBlockStart>(all_events[5]).index == 1);
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

    // Responses custom input snapshots are opaque, optional, and never re-appended.
    {
        oxa::openai::responses::StreamDecoder decoder;
        CHECK(decoder.feed(R"({"type":"response.created","response":{"id":"r","model":"m"}})").ok());
        CHECK(decoder.feed(R"({"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_0","call_id":"c","name":"code_exec","input":""}})").ok());
        CHECK(!decoder.feed(R"({"type":"response.custom_tool_call_input.delta","item_id":"ctc_0","output_index":0})").ok());
        CHECK(!decoder.feed(R"({"type":"response.custom_tool_call_input.delta","item_id":"ctc_0","output_index":0,"delta":null})").ok());
        CHECK(decoder.feed(R"({"type":"response.custom_tool_call_input.delta","item_id":"ctc_0","output_index":0,"delta":"raw"})").ok());
        CHECK(decoder.feed(R"({"type":"response.custom_tool_call_input.done","item_id":"ctc_0","output_index":0,"input":"raw"})").ok());
        CHECK(decoder.feed(R"({"type":"response.custom_tool_call_input.done","item_id":"ctc_0","output_index":0,"input":"raw"})").ok());
        CHECK(decoder.feed(R"({"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_0","call_id":"c","name":"code_exec","input":"raw"}})").ok());
        CHECK(!decoder.feed(R"({"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_0","call_id":"c","name":"code_exec","input":"raw"}})").ok());
        auto terminal = decoder.feed(R"({"type":"response.completed","response":{"id":"r","model":"m","status":"completed","output":[]}})");
        CHECK(terminal.ok());
        CHECK(decoder.flush().ok());

        oxa::openai::responses::StreamDecoder mismatch;
        CHECK(mismatch.feed(R"({"type":"response.created","response":{"id":"r","model":"m"}})").ok());
        CHECK(mismatch.feed(R"({"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_0","call_id":"c","name":"x","input":""}})").ok());
        CHECK(mismatch.feed(R"({"type":"response.custom_tool_call_input.delta","item_id":"ctc_0","output_index":0,"delta":"a"})").ok());
        CHECK(mismatch.feed(R"({"type":"response.custom_tool_call_input.done","item_id":"ctc_0","output_index":0,"input":"b"})").ok());
        CHECK(!mismatch.feed(R"({"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_0","call_id":"c","name":"x","input":"b"}})").ok());

        oxa::openai::responses::StreamDecoder conflict;
        CHECK(conflict.feed(R"({"type":"response.created","response":{"id":"r","model":"m"}})").ok());
        CHECK(conflict.feed(R"({"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_0","call_id":"c","name":"x","input":""}})").ok());
        CHECK(conflict.feed(R"({"type":"response.custom_tool_call_input.done","item_id":"ctc_0","output_index":0,"input":"a"})").ok());
        CHECK(!conflict.feed(R"({"type":"response.custom_tool_call_input.done","item_id":"ctc_0","output_index":0,"input":"b"})").ok());

        oxa::openai::responses::StreamDecoder missing;
        CHECK(missing.feed(R"({"type":"response.created","response":{"id":"r","model":"m"}})").ok());
        CHECK(!missing.feed(R"({"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_0","call_id":"c","name":"x"}})").ok());
    }

    // Unsupported target stream encoders absorb custom blocks and degrade stop only when needed.
    {
        const oxa::ir::MessageStart start{"r", "m"};
        const oxa::ir::ContentBlockStart custom_start{0, oxa::ir::CustomToolUseBlock{"c", "code_exec", "raw"}};
        const oxa::ir::ContentBlockDelta custom_delta{0, oxa::ir::CustomToolInputDelta{"raw"}};
        const oxa::ir::ContentBlockStop custom_stop{0};
        const oxa::ir::MessageDelta stop{std::string(oxa::ir::STOP_TOOL_USE), std::nullopt, {1, 1}};
        oxa::openai::chatcompletions::StreamEncoder cc;
        CHECK(cc.apply(start).ok());
        auto cc_start = cc.apply(custom_start);
        CHECK(cc_start.ok() && cc_start->losses.size() == 1 && cc_start->value.empty());
        CHECK(cc.apply(custom_delta).ok()); CHECK(cc.apply(custom_stop).ok());
        auto cc_terminal = cc.apply(stop);
        CHECK(cc_terminal.ok() && cc_terminal->losses.size() == 1);
        if (cc_terminal.ok()) CHECK(cc_terminal->value.back().find("choices")->as_array()[0].find("finish_reason")->as_string() == "stop");
        CHECK(cc.apply(oxa::ir::MessageDone{}).ok());

        oxa::anthropic::messages::StreamEncoder ant;
        CHECK(ant.apply(start).ok());
        auto ant_start = ant.apply(custom_start);
        CHECK(ant_start.ok() && ant_start->losses.size() == 1 && ant_start->value.empty());
        CHECK(ant.apply(custom_delta).ok()); CHECK(ant.apply(custom_stop).ok());
        auto ant_terminal = ant.apply(stop);
        CHECK(ant_terminal.ok() && ant_terminal->losses.size() == 1);
        if (ant_terminal.ok()) CHECK(ant_terminal->value[0].find("delta")->find("stop_reason")->as_string() == "end_turn");
        CHECK(ant.apply(oxa::ir::MessageDone{}).ok());

        // Dropped custom blocks do not consume target-native call/block indexes.
        oxa::openai::chatcompletions::StreamEncoder compact_cc;
        CHECK(compact_cc.apply(start).ok());
        CHECK(compact_cc.apply(custom_start).ok()); CHECK(compact_cc.apply(custom_delta).ok()); CHECK(compact_cc.apply(custom_stop).ok());
        CHECK(compact_cc.apply(oxa::ir::ContentBlockStart{1, oxa::ir::ToolUseBlock{"f", "clock", "{}"}}).ok());
        CHECK(compact_cc.apply(oxa::ir::ContentBlockDelta{1, oxa::ir::InputJsonDelta{"{}"}}).ok());
        CHECK(compact_cc.apply(oxa::ir::ContentBlockStop{1}).ok());
        auto compact_cc_stop = compact_cc.apply(stop);
        CHECK(compact_cc_stop.ok());
        if (compact_cc_stop.ok()) {
            const auto* choices = compact_cc_stop->value[0].find("choices");
            const auto* calls = choices->as_array()[0].find("delta")->find("tool_calls");
            CHECK(calls && calls->as_array()[0].find("index")->as_int() == 0);
        }

        oxa::anthropic::messages::StreamEncoder compact_ant;
        CHECK(compact_ant.apply(start).ok());
        CHECK(compact_ant.apply(custom_start).ok()); CHECK(compact_ant.apply(custom_delta).ok()); CHECK(compact_ant.apply(custom_stop).ok());
        auto native_start = compact_ant.apply(oxa::ir::ContentBlockStart{1, oxa::ir::ToolUseBlock{"f", "clock", "{}"}});
        CHECK(native_start.ok());
        if (native_start.ok()) CHECK(native_start->value[0].find("index")->as_int() == 0);
        CHECK(compact_ant.apply(oxa::ir::ContentBlockDelta{1, oxa::ir::InputJsonDelta{"{}"}}).ok());
        CHECK(compact_ant.apply(oxa::ir::ContentBlockStop{1}).ok());
    }

    std::puts("test_stream: all 24 stream vectors passed");
    return 0;
}
