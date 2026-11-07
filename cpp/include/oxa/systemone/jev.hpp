// The SystemOne Jev face of the oxa conversion library: the structured
// decision protocol (`state + questions → typed answers`) projected onto the
// IR conversation model (spec/17). Nonstream only — Jev has no streaming
// mode. Semantic unmappables are losses, never errors; errors are reserved
// for structural type violations of known fields (spec/02 §4).

#pragma once

#include <map>
#include <optional>
#include <string>
#include <string_view>

#include "oxa/ir.hpp"
#include "oxa/json.hpp"
#include "oxa/modelmap.hpp"
#include "oxa/status.hpp"

namespace oxa::systemone::jev {

inline constexpr std::string_view RESPONSE_FORMAT_NAME = "jev_answers";
inline constexpr std::string_view SYNTHESIZED_ID = "jev";
inline constexpr std::string_view QUESTION_TYPE_NOUL = "noul";
inline constexpr std::string_view QUESTION_TYPE_CHOICE = "choice";
inline constexpr std::string_view QUESTION_TYPE_SCORE = "score";
inline constexpr std::string_view QUESTION_TYPE_BOX = "box";
inline constexpr std::string_view INSTRUCTION_TEMPLATE =
    "Answer the structured questions that follow as a single JSON object. "
    "Keys MUST be exactly the question IDs given in the user message; values "
    "MUST match each question's type: noul answers are numbers in [0,1], "
    "choice answers are one of the listed option names, score answers are "
    "one of the listed level numbers. Output raw JSON only: no prose, no "
    "markdown fences.";

// A caller-keyed question restored from the synthesized schema or installed
// as encode context (spec/17 §3.1/§4.1).
struct Question {
    std::string type;
    std::optional<std::string> instructions;
    std::optional<json::Value> criteria;
};

using QuestionMap = std::map<std::string, Question>;

struct Options {
    modelmap::Table model_map;
    // Original request questions installed as encode_response context; the
    // question's type governs answer recovery (spec/17 §4.1).
    std::optional<QuestionMap> questions;
};

// Non-streaming converters (spec/17 §3/§4).
StatusOr<Conversion<ir::Request>> decode_request(const json::Value& wire,
                                                 const Options& opts = {});
StatusOr<Conversion<ir::Request>> decode_request(std::string_view wire_json,
                                                 const Options& opts = {});

StatusOr<Conversion<json::Value>> encode_request(const ir::Request& req,
                                                 const Options& opts = {});
StatusOr<Conversion<std::string>> encode_request_json(const ir::Request& req,
                                                      const Options& opts = {});

StatusOr<Conversion<ir::Response>> decode_response(const json::Value& wire,
                                                   const Options& opts = {});
StatusOr<Conversion<ir::Response>> decode_response(std::string_view wire_json,
                                                   const Options& opts = {});

StatusOr<Conversion<json::Value>> encode_response(const ir::Response& resp,
                                                  const Options& opts = {});
StatusOr<Conversion<std::string>> encode_response_json(
    const ir::Response& resp, const Options& opts = {});

}  // namespace oxa::systemone::jev
