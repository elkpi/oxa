// oxa intermediate representation (spec/01).
//
// The IR is the hub of the conversion architecture: every converter either
// produces IR (face -> IR) or consumes it (IR -> face); no converter between
// two faces exists. The JSON shape of every type here is defined normatively
// by spec/schema/ir.schema.json (INV-9); the canonical codec lives in ir.cpp.

#pragma once

#include <cstdint>
#include <map>
#include <optional>
#include <stdexcept>
#include <string>
#include <string_view>
#include <variant>
#include <vector>

#include "oxa/json.hpp"

namespace oxa::ir {

// ---- constants (spec/01, spec/02) ----------------------------------------

inline constexpr std::string_view SPEC_VERSION = "0.1.0";

inline constexpr std::string_view ROLE_USER = "user";
inline constexpr std::string_view ROLE_ASSISTANT = "assistant";

inline constexpr std::string_view BLOCK_TYPE_TEXT = "text";
inline constexpr std::string_view BLOCK_TYPE_IMAGE = "image";
inline constexpr std::string_view BLOCK_TYPE_TOOL_USE = "tool_use";
inline constexpr std::string_view BLOCK_TYPE_TOOL_RESULT = "tool_result";

inline constexpr std::string_view TOOL_CHOICE_AUTO = "auto";
inline constexpr std::string_view TOOL_CHOICE_ANY = "any";
inline constexpr std::string_view TOOL_CHOICE_TOOL = "tool";
inline constexpr std::string_view TOOL_CHOICE_NONE = "none";

inline constexpr std::string_view STOP_END_TURN = "end_turn";
inline constexpr std::string_view STOP_MAX_TOKENS = "max_tokens";
inline constexpr std::string_view STOP_STOP_SEQUENCE = "stop_sequence";
inline constexpr std::string_view STOP_TOOL_USE = "tool_use";
inline constexpr std::string_view STOP_REFUSAL = "refusal";
inline constexpr std::string_view STOP_OTHER = "other";

inline constexpr std::string_view EVENT_TYPE_MESSAGE_START = "message_start";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_START = "content_block_start";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_DELTA = "content_block_delta";
inline constexpr std::string_view EVENT_TYPE_CONTENT_BLOCK_STOP = "content_block_stop";
inline constexpr std::string_view EVENT_TYPE_MESSAGE_DELTA = "message_delta";
inline constexpr std::string_view EVENT_TYPE_MESSAGE_DONE = "message_done";

inline constexpr std::string_view DELTA_TYPE_TEXT_DELTA = "text_delta";
inline constexpr std::string_view DELTA_TYPE_INPUT_JSON_DELTA = "input_json_delta";

inline constexpr std::string_view LOSS_UNMAPPED_FIELD = "unmapped-field";
inline constexpr std::string_view LOSS_UNMAPPED_VALUE = "unmapped-value";
inline constexpr std::string_view LOSS_UNSUPPORTED_SEMANTIC = "unsupported-semantic";
inline constexpr std::string_view LOSS_DEGRADED = "degraded";

// ---- blocks (spec/01 §3.4) ------------------------------------------------

struct TextBlock {
    std::string text;
};

struct ImageBlock {
    std::optional<std::string> media_type;
    std::optional<std::string> data;
    std::optional<std::string> url;
};

// ToolUseBlock input is raw JSON text carried opaquely: converters MUST NOT
// parse or re-serialize it on any conversion path (INV-1).
struct ToolUseBlock {
    std::string id;
    std::string name;
    std::string input;
};

struct ToolResultBlock {
    std::string tool_use_id;
    std::vector<struct BlockHolder> content;  // filled below via Block alias
    bool is_error = false;
};

using Block = std::variant<TextBlock, ImageBlock, ToolUseBlock, ToolResultBlock>;

struct BlockHolder {
    Block block;
};

using SystemBlock = TextBlock;

// ---- request components (spec/01 §3) ---------------------------------------

struct Message {
    std::string role;
    std::vector<Block> content;
};

struct Tool {
    std::string name;
    std::string description;  // empty means absent
    bool has_description = false;
    json::Value input_schema;
};

struct ToolChoice {
    std::string mode;
    std::optional<std::string> name;
};

// None means absent; absent and zero/empty are distinct states (spec/01 §3.7).
struct Params {
    std::optional<double> temperature;
    std::optional<double> top_p;
    std::optional<std::int64_t> max_tokens;
    std::optional<std::vector<std::string>> stop_sequences;
};

struct Request {
    std::string model;
    std::vector<Message> messages;
    std::vector<SystemBlock> system;
    std::optional<std::vector<Tool>> tools;
    std::optional<ToolChoice> tool_choice;
    std::optional<Params> params;
    std::optional<std::map<std::string, std::string>> metadata;
};

// ---- response (spec/01 §4) -------------------------------------------------

struct Usage {
    std::int64_t input_tokens = 0;
    std::int64_t output_tokens = 0;
};

struct Response {
    std::string id;
    std::string model;
    std::vector<Block> content;
    std::string stop_reason;
    std::optional<std::string> stop_sequence;  // only when stop_reason is stop_sequence
    Usage usage;
};

// ---- streaming events (spec/01 §5) ------------------------------------------

struct TextDelta {
    std::string text;
};

// InputJsonDelta partial_json is raw JSON text (INV-1): never parsed or
// re-serialized; concatenation of all fragments of a block is the block input.
struct InputJsonDelta {
    std::string partial_json;
};

using Delta = std::variant<TextDelta, InputJsonDelta>;

struct MessageStart {
    std::string id;
    std::string model;
};

struct ContentBlockStart {
    std::int64_t index = 0;
    Block block;
};

struct ContentBlockDelta {
    std::int64_t index = 0;
    Delta delta;
};

struct ContentBlockStop {
    std::int64_t index = 0;
};

struct MessageDelta {
    std::string stop_reason;
    std::optional<std::string> stop_sequence;
    Usage usage;
};

struct MessageDone {};

using Event = std::variant<MessageStart, ContentBlockStart, ContentBlockDelta, ContentBlockStop,
                           MessageDelta, MessageDone>;

// ---- canonical codec (spec/01, INV-9) ----------------------------------------

json::Value dump_block(const Block& b);
StatusOr<Block> load_block(const json::Value& v);

json::Value dump_request(const Request& r);
StatusOr<Request> load_request(const json::Value& v);

json::Value dump_response(const Response& r);
StatusOr<Response> load_response(const json::Value& v);

json::Value dump_event(const Event& e);
StatusOr<Event> load_event(const json::Value& v);

json::Value dump_event_stream(const std::vector<Event>& events);
StatusOr<std::vector<Event>> load_event_stream(const json::Value& v);

json::Value dump_loss(const Loss& l);

// ---- event-stream invariant checking (spec/01 §7) -----------------------------

// Validates a decoder-produced stream (strict): a tool block with a non-empty
// input must carry explicit input fragments.
Status validate_event_stream(const std::vector<Event>& events);

// Validates an encoder-input stream (lenient): accepts the documented encoder
// shorthand where a tool block carries its full input and no fragments.
Status validate_event_stream_for_encoder(const std::vector<Event>& events);

}  // namespace oxa::ir
