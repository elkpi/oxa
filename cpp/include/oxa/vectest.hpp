#pragma once

#include <filesystem>
#include <functional>
#include <optional>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

#include "oxa/ir.hpp"
#include "oxa/json.hpp"
#include "oxa/status.hpp"

namespace oxa::vectest {

// Finds repository root by looking for both "vectors" and ".git".
std::optional<std::filesystem::path> find_repo_root(
    const std::filesystem::path& start = std::filesystem::current_path());

struct Vector {
    std::string name;
    std::string description;
    std::string mode;
    std::string conversion;
    std::string source_protocol;
    std::string target_protocol;
    json::Value input;
    std::optional<json::Value> expected_ir;
    std::optional<json::Value> expected_output;
    std::vector<ir::Loss> expected_losses;
    std::vector<std::string> tags;

    bool is_request() const noexcept {
        for (const auto& tag : tags) {
            if (tag == "response") return false;
        }
        return true;
    }
};

StatusOr<std::vector<Vector>> load_vectors(
    const std::filesystem::path& repo_root,
    std::string_view face_dir,
    std::string_view mode);

StatusOr<std::vector<Vector>> load_cross_vectors(
    const std::filesystem::path& repo_root);

Status compare_json(const json::Value& expected, const json::Value& actual);

Status compare_losses(const std::vector<ir::Loss>& expected,
                      const std::vector<ir::Loss>& actual);

class Converter {
public:
    virtual ~Converter() = default;
    virtual std::string_view face() const = 0;
    virtual StatusOr<Conversion<ir::Request>> decode_request(const json::Value& wire) const = 0;
    virtual StatusOr<Conversion<ir::Response>> decode_response(const json::Value& wire) const = 0;
    virtual StatusOr<Conversion<json::Value>> encode_request(const ir::Request& req) const = 0;
    virtual StatusOr<Conversion<json::Value>> encode_response(const ir::Response& resp) const = 0;
};

class StreamConverter {
public:
    virtual ~StreamConverter() = default;
    virtual std::string_view face() const = 0;
    virtual StatusOr<std::vector<ir::Event>> decode_native_event(const json::Value& event) = 0;
    virtual StatusOr<std::vector<ir::Event>> flush_decoder() = 0;
    virtual std::vector<ir::Loss> decoder_losses() const = 0;
    virtual StatusOr<Conversion<std::vector<json::Value>>> apply_ir_event(const ir::Event& event) = 0;
    virtual void reset_stream_vector() {}
};

struct Failure {
    std::string vector_name;
    std::string message;
};

struct Report {
    std::size_t executed = 0;
    std::vector<Failure> failures;

    bool ok() const noexcept { return failures.empty() && executed > 0; }
};

StatusOr<Report> run_nonstream(const Converter& conv,
                              const std::filesystem::path& start = std::filesystem::current_path());

StatusOr<Report> run_cross(const std::vector<const Converter*>& converters,
                          const std::filesystem::path& start = std::filesystem::current_path());

StatusOr<Report> run_stream(StreamConverter& conv,
                            const std::filesystem::path& start = std::filesystem::current_path());

}  // namespace oxa::vectest
