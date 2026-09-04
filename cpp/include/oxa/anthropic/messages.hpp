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
    std::int64_t open_index_ = 0;
    std::int64_t next_index_ = 0;
    std::string tool_input_;
    std::vector<std::string> tool_parts_;
    bool delta_seen_ = false;
    bool done_ = false;
};

}  // namespace oxa::anthropic::messages
