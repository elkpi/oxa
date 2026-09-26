#pragma once

#include <optional>
#include <set>
#include <string>
#include <string_view>
#include <vector>

#include "oxa/ir.hpp"
#include "oxa/json.hpp"
#include "oxa/modelmap.hpp"
#include "oxa/status.hpp"

namespace oxa::anthropic::messages {

// Wire constants (spec/12).
inline constexpr std::string_view ROLE_USER = "user";
inline constexpr std::string_view ROLE_ASSISTANT = "assistant";

inline constexpr std::string_view BLOCK_TYPE_TEXT = "text";
inline constexpr std::string_view BLOCK_TYPE_THINKING = "thinking";
inline constexpr std::string_view BLOCK_TYPE_IMAGE = "image";
inline constexpr std::string_view BLOCK_TYPE_TOOL_USE = "tool_use";
inline constexpr std::string_view BLOCK_TYPE_TOOL_RESULT = "tool_result";

inline constexpr std::string_view SOURCE_TYPE_BASE64 = "base64";
inline constexpr std::string_view SOURCE_TYPE_URL = "url";

inline constexpr std::string_view TOOL_CHOICE_TYPE_AUTO = "auto";
inline constexpr std::string_view TOOL_CHOICE_TYPE_ANY = "any";
inline constexpr std::string_view TOOL_CHOICE_TYPE_NONE = "none";
inline constexpr std::string_view TOOL_CHOICE_TYPE_TOOL = "tool";

inline constexpr std::string_view STOP_REASON_END_TURN = "end_turn";
inline constexpr std::string_view STOP_REASON_MAX_TOKENS = "max_tokens";
inline constexpr std::string_view STOP_REASON_STOP_SEQUENCE = "stop_sequence";
inline constexpr std::string_view STOP_REASON_TOOL_USE = "tool_use";
inline constexpr std::string_view STOP_REASON_REFUSAL = "refusal";

inline constexpr std::string_view EVENT_TYPE_MESSAGE_START = "message_start";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_START = "content_block_start";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_DELTA = "content_block_delta";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_STOP = "content_block_stop";
inline constexpr std::string_view EVENT_TYPE_MESSAGE_DELTA = "message_delta";
inline constexpr std::string_view EVENT_TYPE_MESSAGE_STOP = "message_stop";

inline constexpr std::string_view DELTA_TYPE_TEXT_DELTA = "text_delta";
inline constexpr std::string_view DELTA_TYPE_INPUT_JSON_DELTA = "input_json_delta";
inline constexpr std::string_view DELTA_TYPE_THINKING_DELTA = "thinking_delta";
inline constexpr std::string_view DELTA_TYPE_SIGNATURE_DELTA = "signature_delta";

inline constexpr std::string_view TYPE_MESSAGE = "message";

struct Options {
    modelmap::Table model_map;
};

// Non-streaming converters (spec/12 §4).
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
    std::string id_;
    std::string model_;
    bool block_open_ = false;
    bool skipped_open_ = false;
    std::set<std::int64_t> skipped_;
    bool open_tool_ = false;
    bool open_thinking_ = false;
    bool thinking_signature_seen_ = false;
    std::int64_t open_index_ = 0;
    std::int64_t open_ir_index_ = 0;
    std::int64_t next_index_ = 0;
    std::int64_t next_ir_index_ = 0;
    std::string tool_id_;
    std::string tool_name_;
    std::string tool_input_;
    std::vector<std::string> tool_parts_;
    bool delta_seen_ = false;
    std::string stop_reason_;
    std::optional<std::string> stop_seq_;
    ir::Usage usage_{0, 0};
    bool stopped_ = false;
    bool flushed_ = false;
};

class StreamEncoder {
public:
    explicit StreamEncoder(Options opts = {});

    StatusOr<Conversion<std::vector<json::Value>>> apply(const ir::Event& event);

private:
    Options opts_;
    std::string id_;
    std::string model_;
    bool started_ = false;
    bool block_open_ = false;
    bool open_tool_ = false;
    bool open_thinking_ = false;
    std::string thinking_input_;
    std::optional<std::string> thinking_signature_;
    std::vector<std::string> thinking_parts_;
    bool signature_seen_ = false;
    std::int64_t open_index_ = 0;
    std::int64_t next_index_ = 0;
    std::string tool_input_;
    std::vector<std::string> tool_parts_;
    bool delta_seen_ = false;
    bool done_ = false;
};

}  // namespace oxa::anthropic::messages
