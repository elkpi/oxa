#pragma once

#include <string>
#include <string_view>
#include <utility>
#include <vector>

namespace oxa::ir {

// Loss records a single dropped or degraded semantic element during conversion (spec/02).
struct Loss {
    std::string path;
    std::string field;
    std::string reason;
    std::string detail;

    bool operator==(const Loss& other) const {
        return path == other.path && field == other.field && reason == other.reason &&
               detail == other.detail;
    }
};

struct LossFingerprint {
    std::string path;
    std::string field;
    std::string reason;

    bool operator==(const LossFingerprint& other) const {
        return path == other.path && field == other.field && reason == other.reason;
    }
};

inline std::vector<LossFingerprint> fingerprint_losses(const std::vector<Loss>& losses, size_t limit = 0) {
    size_t n = (limit > 0 && limit < losses.size()) ? limit : losses.size();
    std::vector<LossFingerprint> out;
    out.reserve(n);
    for (size_t i = 0; i < n; ++i) {
        out.push_back(LossFingerprint{losses[i].path, losses[i].field, losses[i].reason});
    }
    return out;
}

inline Loss make_loss(std::string path, std::string field, std::string_view reason,
                      std::string detail) {
    return Loss{std::move(path), std::move(field), std::string(reason), std::move(detail)};
}

}  // namespace oxa::ir
