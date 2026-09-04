// oxa JSON — a minimal, dependency-free JSON value model, parser, and
// serializer used by every oxa C++ module.
//
// Key properties:
// 1. Integer fidelity (spec/01 INV-7): `1` and `1.0` are distinct values.
// 2. Source spans & safe retention (spec/01 INV-1): parsed values safely hold
//    their source text in a shared buffer so raw_slice() is always valid.
// 3. No exceptions: returns StatusOr<Value> on errors; rejects duplicate keys.

#pragma once

#include <cstdint>
#include <map>
#include <memory>
#include <string>
#include <string_view>
#include <vector>

#include "oxa/status.hpp"

namespace oxa::json {

class Value;
using Array = std::vector<Value>;
using Object = std::map<std::string, Value, std::less<>>;

class Value {
public:
    enum class Kind : std::uint8_t {
        Null,
        Bool,
        Int,
        Double,
        String,
        Array,
        Object,
        Raw,
    };

    Value() noexcept = default;

    static Value null() noexcept { return Value(); }
    static Value boolean(bool b) noexcept {
        Value v;
        v.kind_ = Kind::Bool;
        v.b_ = b;
        return v;
    }
    static Value integer(std::int64_t i) noexcept {
        Value v;
        v.kind_ = Kind::Int;
        v.i_ = i;
        return v;
    }
    static Value real(double d) noexcept {
        Value v;
        v.kind_ = Kind::Double;
        v.d_ = d;
        return v;
    }
    static Value string(std::string s) {
        Value v;
        v.kind_ = Kind::String;
        v.s_ = std::move(s);
        return v;
    }
    static Value array(Array a = {}) {
        Value v;
        v.kind_ = Kind::Array;
        v.arr_ = std::move(a);
        return v;
    }
    static Value object(Object o = {}) {
        Value v;
        v.kind_ = Kind::Object;
        v.obj_ = std::move(o);
        return v;
    }
    static Value raw(std::string text) {
        Value v;
        v.kind_ = Kind::Raw;
        v.s_ = std::move(text);
        return v;
    }

    Kind kind() const noexcept { return kind_; }
    bool is_null() const noexcept { return kind_ == Kind::Null; }
    bool is_bool() const noexcept { return kind_ == Kind::Bool; }
    bool is_int() const noexcept { return kind_ == Kind::Int; }
    bool is_double() const noexcept { return kind_ == Kind::Double; }
    bool is_number() const noexcept { return is_int() || is_double(); }
    bool is_string() const noexcept { return kind_ == Kind::String; }
    bool is_array() const noexcept { return kind_ == Kind::Array; }
    bool is_object() const noexcept { return kind_ == Kind::Object; }
    bool is_raw() const noexcept { return kind_ == Kind::Raw; }

    bool as_bool() const noexcept { return b_; }
    std::int64_t as_int() const noexcept { return i_; }
    double as_double() const noexcept { return d_; }
    const std::string& as_string() const noexcept { return s_; }
    const std::string& as_raw_text() const noexcept { return s_; }
    const Array& as_array() const noexcept { return arr_; }
    Array& as_array() noexcept { return arr_; }
    const Object& as_object() const noexcept { return obj_; }
    Object& as_object() noexcept { return obj_; }

    const Value* find(std::string_view key) const noexcept {
        if (kind_ != Kind::Object) return nullptr;
        auto it = obj_.find(key);
        return it == obj_.end() ? nullptr : &it->second;
    }

    Value* find(std::string_view key) noexcept {
        if (kind_ != Kind::Object) return nullptr;
        auto it = obj_.find(key);
        return it == obj_.end() ? nullptr : &it->second;
    }

    const Value* get(std::string_view key) const noexcept {
        return find(key);
    }

    void set(std::string key, Value v) {
        if (kind_ != Kind::Object) {
            kind_ = Kind::Object;
        }
        obj_.insert_or_assign(std::move(key), std::move(v));
    }

    void push_back(Value v) {
        if (kind_ != Kind::Array) {
            kind_ = Kind::Array;
        }
        arr_.push_back(std::move(v));
    }

    std::uint32_t span_begin() const noexcept { return begin_; }
    std::uint32_t span_end() const noexcept { return end_; }
    void set_span(std::uint32_t b, std::uint32_t e,
                  std::shared_ptr<const std::string> src = nullptr) noexcept {
        begin_ = b;
        end_ = e;
        source_ = std::move(src);
    }

    std::string_view raw_slice() const noexcept {
        if (kind_ == Kind::Raw) {
            return s_;
        }
        if (source_ && end_ > begin_ && end_ <= source_->size()) {
            return std::string_view(source_->data() + begin_, end_ - begin_);
        }
        if (kind_ == Kind::String) {
            return s_;
        }
        return {};
    }

    std::string_view raw_slice(std::string_view fallback_source) const noexcept {
        if (source_ && end_ > begin_ && end_ <= source_->size()) {
            return std::string_view(source_->data() + begin_, end_ - begin_);
        }
        if (end_ > begin_ && end_ <= fallback_source.size()) {
            return fallback_source.substr(begin_, static_cast<std::size_t>(end_ - begin_));
        }
        return raw_slice();
    }

    std::shared_ptr<const std::string> source() const noexcept { return source_; }

private:
    Kind kind_ = Kind::Null;
    bool b_ = false;
    std::int64_t i_ = 0;
    double d_ = 0.0;
    std::string s_;
    Array arr_;
    Object obj_;
    std::uint32_t begin_ = 0;
    std::uint32_t end_ = 0;
    std::shared_ptr<const std::string> source_;
};

// Parses one complete JSON document. Trailing whitespace is allowed; trailing
// non-whitespace is rejected. Duplicate keys are rejected.
StatusOr<Value> parse(std::string_view text);

// Serializes a value. Raw values are emitted verbatim. Object keys are
// emitted in sorted order.
std::string serialize(const Value& v);

// Structural equality (vectors/README.md rules 1-3): object key order is
// irrelevant, arrays are ordered, strings are exact, and Int and Double are
// distinct types (1 != 1.0). Raw values are parsed and compared structurally.
bool structurally_equal(const Value& a, const Value& b);

}  // namespace oxa::json
