#pragma once

#include <optional>
#include <string>
#include <string_view>
#include <vector>

#include "oxa/ir.hpp"
#include "oxa/json.hpp"
#include "oxa/modelmap.hpp"
#include "oxa/status.hpp"

namespace oxa::openai::responses {

struct Options {
    modelmap::Table model_map;
};

// Non-streaming converters (spec/11 §4).
StatusOr<Conversion<ir::Request>> decode_request(const json::Value& wire,
                                                 const Options& opts = {});
StatusOr<Conversion<ir::Request>> decode_request(std::string_view wire_json,
                                                 const Options& opts = {});

StatusOr<Conversion<json::Value>> encode_request(const ir::Request& req,
                                                 const Options& opts = {});

StatusOr<Conversion<ir::Response>> decode_response(const json::Value& wire,
                                                  const Options& opts = {});
StatusOr<Conversion<ir::Response>> decode_response(std::string_view wire_json,
                                                  const Options& opts = {});

StatusOr<Conversion<json::Value>> encode_response(const ir::Response& resp,
                                                  const Options& opts = {});

// Streaming converters (spec/20).
class StreamDecoder {
public:
    explicit StreamDecoder(Options opts = {});

    StatusOr<std::vector<ir::Event>> feed(const json::Value& chunk);
    StatusOr<std::vector<ir::Event>> flush();
    const std::vector<ir::Loss>& losses() const noexcept { return losses_; }

private:
    Options opts_;
    std::vector<ir::Loss> losses_;
    bool started_ = false;
    bool terminated_ = false;
    bool flushed_ = false;
    std::string id_;
    std::string model_;
    std::int64_t next_output_index_ = 0;
    std::int64_t next_block_index_ = 0;
    bool item_open_ = false;
    bool skipped_item_ = false;
    std::string item_type_;
    std::string item_id_;
    std::string skipped_call_id_;
    std::int64_t output_index_ = 0;
    std::int64_t next_content_index_ = 0;

    struct StreamFunctionCall {
        std::string item_id;
        std::int64_t output_index = 0;
        std::string call_id;
        std::string name;
        std::vector<std::string> fragments;
        bool arguments_done = false;
    };
    std::optional<StreamFunctionCall> function_call_;
    bool tool_use_seen_ = false;

    bool block_open_ = false;
    bool skipped_part_ = false;
    std::int64_t block_index_ = 0;
    std::int64_t content_index_ = 0;
    bool text_done_ = false;

    Status require_started(std::string_view event_type);
    Status require_active_item(const json::Value& ev, std::string_view event_type);
    Status require_function_call(const json::Value& ev, std::string_view event_type);
    ir::Loss unsupported_item_loss(std::int64_t output_index, std::string_view item_type);
    StatusOr<std::vector<ir::Event>> replay_function_call(const StreamFunctionCall& call);
};

class StreamEncoder {
public:
    explicit StreamEncoder(Options opts = {});

    StatusOr<Conversion<std::vector<json::Value>>> apply(const ir::Event& event);

private:
    enum class OutputItemKind { Message, FunctionCall };

    struct StreamOutputItem {
        OutputItemKind kind;
        std::string id;
        std::int64_t output_index = 0;
        std::vector<json::Value> content;
        std::int64_t next_content_index = 0;
        std::string call_id;
        std::string name;
    };

    struct StreamEncodeBlock {
        std::int64_t index = 0;
        OutputItemKind kind;
        std::int64_t content_index = 0;
        std::string text;
        std::string tool_input;
        std::vector<std::string> fragments;
    };

    Options opts_;
    std::string id_;
    std::string model_;
    bool started_ = false;
    bool delta_ = false;
    bool done_ = false;

    std::int64_t next_block_index_ = 0;
    std::int64_t next_output_index_ = 0;
    std::int64_t next_message_item_ = 0;
    std::int64_t next_function_item_ = 0;
    std::optional<StreamOutputItem> active_item_;
    std::optional<StreamEncodeBlock> active_block_;
    std::vector<json::Value> completed_;

    std::pair<StreamOutputItem, json::Value> open_message_item();
    std::pair<StreamOutputItem, json::Value> open_function_call_item(std::string_view call_id, std::string_view name);
    json::Value close_message_item();
    StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> start_text_block(std::int64_t index, const ir::TextBlock& block);
    StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> start_function_call_block(std::int64_t index, const ir::ToolUseBlock& block);
    StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> stop_text_block();
    StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> stop_function_call_block();
    StatusOr<std::pair<json::Value, std::vector<ir::Loss>>> terminal(const ir::MessageDelta& delta);
};

}  // namespace oxa::openai::responses
