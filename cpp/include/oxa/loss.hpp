#pragma once

#include <string>
#include <string_view>
#include <utility>

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

inline Loss make_loss(std::string path, std::string field, std::string_view reason,
                      std::string detail) {
    return Loss{std::move(path), std::move(field), std::string(reason), std::move(detail)};
}

}  // namespace oxa::ir
