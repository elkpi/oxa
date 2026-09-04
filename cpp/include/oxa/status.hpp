#pragma once

#include <cstdint>
#include <string>
#include <string_view>
#include <utility>
#include <variant>
#include <vector>

#include "oxa/loss.hpp"

namespace oxa {

enum class StatusCode : std::uint8_t {
    kOk = 0,
    kInvalidArgument = 1,
    kFailedPrecondition = 2,
    kResourceExhausted = 3,
    kInternal = 4,
};

inline std::string_view status_code_to_string(StatusCode code) noexcept {
    switch (code) {
        case StatusCode::kOk:
            return "OK";
        case StatusCode::kInvalidArgument:
            return "INVALID_ARGUMENT";
        case StatusCode::kFailedPrecondition:
            return "FAILED_PRECONDITION";
        case StatusCode::kResourceExhausted:
            return "RESOURCE_EXHAUSTED";
        case StatusCode::kInternal:
            return "INTERNAL";
    }
    return "UNKNOWN";
}

class Status {
public:
    Status() noexcept : code_(StatusCode::kOk) {}

    Status(StatusCode code, std::string message)
        : code_(code), message_(std::move(message)) {}

    bool ok() const noexcept { return code_ == StatusCode::kOk; }
    explicit operator bool() const noexcept { return ok(); }

    StatusCode code() const noexcept { return code_; }
    std::string_view message() const noexcept { return message_; }

    std::string to_string() const {
        if (ok()) {
            return "OK";
        }
        std::string res;
        std::string_view code_str = status_code_to_string(code_);
        res.reserve(code_str.size() + 2 + message_.size());
        res.append(code_str);
        if (!message_.empty()) {
            res.append(": ");
            res.append(message_);
        }
        return res;
    }

    bool operator==(const Status& other) const noexcept {
        return code_ == other.code_ && message_ == other.message_;
    }

    bool operator!=(const Status& other) const noexcept {
        return !(*this == other);
    }

private:
    StatusCode code_;
    std::string message_;
};

inline Status ok_status() noexcept {
    return Status();
}

inline Status invalid_argument(std::string message) {
    return Status(StatusCode::kInvalidArgument, std::move(message));
}

inline Status failed_precondition(std::string message) {
    return Status(StatusCode::kFailedPrecondition, std::move(message));
}

inline Status resource_exhausted(std::string message) {
    return Status(StatusCode::kResourceExhausted, std::move(message));
}

inline Status internal_error(std::string message) {
    return Status(StatusCode::kInternal, std::move(message));
}

template <typename T>
class StatusOr {
public:
    StatusOr(const T& value) : data_(value) {}
    StatusOr(T&& value) : data_(std::move(value)) {}

    StatusOr(Status status) : data_(std::move(status)) {
        if (std::get<Status>(data_).ok()) {
            data_ = Status(StatusCode::kInternal, "StatusOr initialized with OK status");
        }
    }

    StatusOr(StatusCode code, std::string message)
        : data_(Status(code, std::move(message))) {}

    bool ok() const noexcept { return std::holds_alternative<T>(data_); }
    explicit operator bool() const noexcept { return ok(); }

    const Status& status() const noexcept {
        if (ok()) {
            static const Status ok_status;
            return ok_status;
        }
        return std::get<Status>(data_);
    }

    StatusCode code() const noexcept {
        return status().code();
    }

    std::string_view message() const noexcept {
        return status().message();
    }

    const T& value() const & noexcept { return std::get<T>(data_); }
    T& value() & noexcept { return std::get<T>(data_); }
    const T&& value() const && noexcept { return std::get<T>(std::move(data_)); }
    T&& value() && noexcept { return std::get<T>(std::move(data_)); }

    const T* operator->() const noexcept { return &std::get<T>(data_); }
    T* operator->() noexcept { return &std::get<T>(data_); }

    const T& operator*() const & noexcept { return std::get<T>(data_); }
    T& operator*() & noexcept { return std::get<T>(data_); }
    const T&& operator*() const && noexcept { return std::get<T>(std::move(data_)); }
    T&& operator*() && noexcept { return std::get<T>(std::move(data_)); }

private:
    std::variant<Status, T> data_;
};

template <typename T>
struct Conversion {
    T value;
    std::vector<ir::Loss> losses;
};

#define OXA_STATUS_CONCAT_INNER(x, y) x##y
#define OXA_STATUS_CONCAT(x, y) OXA_STATUS_CONCAT_INNER(x, y)

#define OXA_RETURN_IF_ERROR(expr)                           \
    do {                                                    \
        auto _oxa_status = (expr);                          \
        if (!_oxa_status.ok()) {                            \
            return _oxa_status;                             \
        }                                                   \
    } while (0)

#define OXA_ASSIGN_OR_RETURN(lhs, expr)                     \
    auto OXA_STATUS_CONCAT(_oxa_res_, __LINE__) = (expr);   \
    if (!OXA_STATUS_CONCAT(_oxa_res_, __LINE__).ok()) {     \
        return OXA_STATUS_CONCAT(_oxa_res_, __LINE__).status(); \
    }                                                       \
    lhs = std::move(OXA_STATUS_CONCAT(_oxa_res_, __LINE__)).value()

}  // namespace oxa
