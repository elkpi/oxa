#pragma once

#include <optional>
#include <string>
#include <string_view>
#include <vector>

#include "oxa/ir.hpp"
#include "oxa/json.hpp"
#include "oxa/modelmap.hpp"
#include "oxa/status.hpp"

namespace oxa::openai::chatcompletions {

struct Options {
    modelmap::Table model_map;
};

// Non-streaming converters (spec/10 §4).
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
    std::int64_t text_index_ = -1;
    bool text_open_ = false;
    std::string finish_reason_;
    std::optional<ir::Usage> usage_;

    struct ToolAccum {
        std::int64_t index = -1;
        std::string id;
        std::string name;
        std::string arguments;
        std::vector<std::string> fragments;
    };
    std::map<std::int64_t, ToolAccum> tools_;
    std::int64_t next_block_index_ = 0;
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
    bool tool_seen_ = false;
    bool ordering_degrade_ = false;
    std::int64_t next_ir_index_ = 0;
    std::size_t next_native_tool_ = 0;

    struct ActiveBlock {
        enum class Kind { Text, Tool } kind;
        std::int64_t index = 0;
        std::string tool_id;
        std::string tool_name;
        std::string tool_input;
        std::vector<std::string> fragments;
        std::size_t native_index = 0;
        bool tool_started = false;
    };
    std::optional<ActiveBlock> active_;
    std::vector<json::Value> pending_tools_;
    bool finished_ = false;
    bool done_ = false;
};

}  // namespace oxa::openai::chatcompletions
