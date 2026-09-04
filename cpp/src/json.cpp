#include "oxa/json.hpp"

#include <charconv>
#include <cmath>
#include <cstdio>
#include <memory>

namespace oxa::json {
namespace {

class Parser {
public:
    explicit Parser(std::string_view text)
        : source_(std::make_shared<std::string>(text)), text_(*source_) {}

    StatusOr<Value> parse_document() {
        skip_ws();
        OXA_ASSIGN_OR_RETURN(Value v, parse_value(0));
        skip_ws();
        if (pos_ != text_.size()) {
            return invalid_argument("json: trailing characters after document");
        }
        return v;
    }

private:
    static constexpr std::size_t kMaxDepth = 512;

    std::shared_ptr<const std::string> source_;
    std::string_view text_;
    std::size_t pos_ = 0;

    char peek() const noexcept { return pos_ < text_.size() ? text_[pos_] : '\0'; }

    void skip_ws() noexcept {
        while (pos_ < text_.size()) {
            char c = text_[pos_];
            if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
                ++pos_;
            } else {
                break;
            }
        }
    }

    Status expect(char c) {
        if (pos_ >= text_.size() || text_[pos_] != c) {
            std::string msg = "json: expected '";
            msg += c;
            msg += "'";
            return invalid_argument(std::move(msg));
        }
        ++pos_;
        return ok_status();
    }

    StatusOr<Value> parse_value(std::size_t depth) {
        if (depth > kMaxDepth) {
            return resource_exhausted("json: nesting too deep");
        }
        char c = peek();
        switch (c) {
            case '{': return parse_object(depth);
            case '[': return parse_array(depth);
            case '"': {
                OXA_ASSIGN_OR_RETURN(std::string s, parse_string_body());
                return Value::string(std::move(s));
            }
            case 't': return parse_literal("true", Value::boolean(true));
            case 'f': return parse_literal("false", Value::boolean(false));
            case 'n': return parse_literal("null", Value::null());
            default: return parse_number();
        }
    }

    StatusOr<Value> parse_literal(const char* word, Value out) {
        std::string_view w(word);
        if (text_.substr(pos_, w.size()) != w) {
            return invalid_argument("json: invalid literal");
        }
        const std::size_t start = pos_;
        pos_ += w.size();
        return finish(std::move(out), start);
    }

    StatusOr<Value> parse_object(std::size_t depth) {
        const std::size_t start = pos_;
        OXA_RETURN_IF_ERROR(expect('{'));
        Object obj;
        skip_ws();
        if (peek() == '}') {
            ++pos_;
            return finish(Value::object(std::move(obj)), start);
        }
        while (true) {
            skip_ws();
            OXA_ASSIGN_OR_RETURN(std::string key, parse_string_body());
            skip_ws();
            OXA_RETURN_IF_ERROR(expect(':'));
            skip_ws();
            OXA_ASSIGN_OR_RETURN(Value v, parse_value(depth + 1));
            if (obj.find(key) != obj.end()) {
                return invalid_argument("json: duplicate object key: " + key);
            }
            obj.insert_or_assign(std::move(key), std::move(v));
            skip_ws();
            char c = peek();
            if (c == ',') {
                ++pos_;
                continue;
            }
            if (c == '}') {
                ++pos_;
                break;
            }
            return invalid_argument("json: expected ',' or '}' in object");
        }
        return finish(Value::object(std::move(obj)), start);
    }

    StatusOr<Value> parse_array(std::size_t depth) {
        const std::size_t start = pos_;
        OXA_RETURN_IF_ERROR(expect('['));
        Array arr;
        skip_ws();
        if (peek() == ']') {
            ++pos_;
            return finish(Value::array(std::move(arr)), start);
        }
        while (true) {
            skip_ws();
            OXA_ASSIGN_OR_RETURN(Value val, parse_value(depth + 1));
            arr.push_back(std::move(val));
            skip_ws();
            char c = peek();
            if (c == ',') {
                ++pos_;
                continue;
            }
            if (c == ']') {
                ++pos_;
                break;
            }
            return invalid_argument("json: expected ',' or ']' in array");
        }
        return finish(Value::array(std::move(arr)), start);
    }

    StatusOr<std::string> parse_string_body() {
        OXA_RETURN_IF_ERROR(expect('"'));
        std::string out;
        while (true) {
            if (pos_ >= text_.size()) {
                return invalid_argument("json: unterminated string");
            }
            char c = text_[pos_];
            if (c == '"') {
                ++pos_;
                return out;
            }
            if (c == '\\') {
                ++pos_;
                if (pos_ >= text_.size()) {
                    return invalid_argument("json: unterminated escape");
                }
                char esc = text_[pos_++];
                switch (esc) {
                    case '"': out += '"'; break;
                    case '\\': out += '\\'; break;
                    case '/': out += '/'; break;
                    case 'b': out += '\b'; break;
                    case 'f': out += '\f'; break;
                    case 'n': out += '\n'; break;
                    case 'r': out += '\r'; break;
                    case 't': out += '\t'; break;
                    case 'u': {
                        OXA_ASSIGN_OR_RETURN(std::string uesc, parse_unicode_escape());
                        out += uesc;
                        break;
                    }
                    default: return invalid_argument("json: invalid escape character");
                }
            } else if (static_cast<unsigned char>(c) < 0x20) {
                return invalid_argument("json: control character in string");
            } else {
                out += c;
                ++pos_;
            }
        }
    }

    StatusOr<std::uint32_t> parse_hex4() {
        if (pos_ + 4 > text_.size()) {
            return invalid_argument("json: truncated \\u escape");
        }
        std::uint32_t cp = 0;
        for (int i = 0; i < 4; ++i) {
            char c = text_[pos_++];
            cp <<= 4;
            if (c >= '0' && c <= '9') {
                cp |= static_cast<std::uint32_t>(c - '0');
            } else if (c >= 'a' && c <= 'f') {
                cp |= static_cast<std::uint32_t>(c - 'a' + 10);
            } else if (c >= 'A' && c <= 'F') {
                cp |= static_cast<std::uint32_t>(c - 'A' + 10);
            } else {
                return invalid_argument("json: invalid \\u escape");
            }
        }
        return cp;
    }

    static std::string encode_utf8(std::uint32_t cp) {
        std::string out;
        if (cp < 0x80) {
            out += static_cast<char>(cp);
        } else if (cp < 0x800) {
            out += static_cast<char>(0xC0 | (cp >> 6));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        } else if (cp < 0x10000) {
            out += static_cast<char>(0xE0 | (cp >> 12));
            out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        } else {
            out += static_cast<char>(0xF0 | (cp >> 18));
            out += static_cast<char>(0x80 | ((cp >> 12) & 0x3F));
            out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
            out += static_cast<char>(0x80 | (cp & 0x3F));
        }
        return out;
    }

    StatusOr<std::string> parse_unicode_escape() {
        OXA_ASSIGN_OR_RETURN(std::uint32_t cp, parse_hex4());
        if (cp >= 0xD800 && cp <= 0xDBFF) {
            if (pos_ + 1 < text_.size() && text_[pos_] == '\\' && text_[pos_ + 1] == 'u') {
                pos_ += 2;
                OXA_ASSIGN_OR_RETURN(std::uint32_t lo, parse_hex4());
                if (lo >= 0xDC00 && lo <= 0xDFFF) {
                    cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                    return encode_utf8(cp);
                }
            }
            return invalid_argument("json: invalid surrogate pair");
        }
        if (cp >= 0xDC00 && cp <= 0xDFFF) {
            return invalid_argument("json: lone low surrogate");
        }
        return encode_utf8(cp);
    }

    StatusOr<Value> parse_number() {
        const std::size_t start = pos_;
        if (peek() == '-') ++pos_;
        if (pos_ >= text_.size()) {
            return invalid_argument("json: invalid number");
        }
        if (text_[pos_] == '0') {
            ++pos_;
            if (pos_ < text_.size() && text_[pos_] >= '0' && text_[pos_] <= '9') {
                return invalid_argument("json: leading zeros are not allowed in numbers");
            }
        } else if (text_[pos_] >= '1' && text_[pos_] <= '9') {
            ++pos_;
            while (pos_ < text_.size() && text_[pos_] >= '0' && text_[pos_] <= '9') {
                ++pos_;
            }
        } else {
            return invalid_argument("json: invalid number");
        }

        bool is_double = false;
        if (pos_ < text_.size() && text_[pos_] == '.') {
            is_double = true;
            ++pos_;
            if (pos_ >= text_.size() || text_[pos_] < '0' || text_[pos_] > '9') {
                return invalid_argument("json: invalid number fraction");
            }
            while (pos_ < text_.size() && text_[pos_] >= '0' && text_[pos_] <= '9') {
                ++pos_;
            }
        }
        if (pos_ < text_.size() && (text_[pos_] == 'e' || text_[pos_] == 'E')) {
            is_double = true;
            ++pos_;
            if (pos_ < text_.size() && (text_[pos_] == '+' || text_[pos_] == '-')) ++pos_;
            if (pos_ >= text_.size() || text_[pos_] < '0' || text_[pos_] > '9') {
                return invalid_argument("json: invalid number exponent");
            }
            while (pos_ < text_.size() && text_[pos_] >= '0' && text_[pos_] <= '9') {
                ++pos_;
            }
        }
        std::string_view tok = text_.substr(start, pos_ - start);
        if (!is_double) {
            std::int64_t out = 0;
            auto [ptr, ec] = std::from_chars(tok.data(), tok.data() + tok.size(), out);
            if (ec == std::errc()) {
                return finish(Value::integer(out), start);
            }
            if (ec == std::errc::result_out_of_range) {
                return resource_exhausted("json: integer out of range");
            }
            return invalid_argument("json: invalid integer");
        }
        double d = 0;
        auto [ptr2, ec2] = std::from_chars(tok.data(), tok.data() + tok.size(), d);
        if (ec2 != std::errc()) {
            return invalid_argument("json: invalid number");
        }
        return finish(Value::real(d), start);
    }

    Value finish(Value v, std::size_t start) {
        v.set_span(static_cast<std::uint32_t>(start), static_cast<std::uint32_t>(pos_), source_);
        return v;
    }
};

void append_escaped(std::string& out, std::string_view s) {
    out += '"';
    for (char c : s) {
        switch (c) {
            case '"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\b': out += "\\b"; break;
            case '\f': out += "\\f"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default:
                if (static_cast<unsigned char>(c) < 0x20) {
                    char buf[8];
                    std::snprintf(buf, sizeof(buf), "\\u%04x", static_cast<unsigned char>(c));
                    out += buf;
                } else {
                    out += c;
                }
        }
    }
    out += '"';
}

void append_double(std::string& out, double d) {
    if (!std::isfinite(d)) {
        out += "null";
        return;
    }
    char buf[64];
    auto res = std::to_chars(buf, buf + sizeof(buf), d);
    std::string_view s(buf, static_cast<std::size_t>(res.ptr - buf));
    out.append(s);
    if (s.find('.') == std::string_view::npos && s.find('e') == std::string_view::npos &&
        s.find('E') == std::string_view::npos) {
        out.append(".0");
    }
}

void serialize_to(std::string& out, const Value& v) {
    switch (v.kind()) {
        case Value::Kind::Null: out += "null"; break;
        case Value::Kind::Bool: out += v.as_bool() ? "true" : "false"; break;
        case Value::Kind::Int: out += std::to_string(v.as_int()); break;
        case Value::Kind::Double: append_double(out, v.as_double()); break;
        case Value::Kind::String: append_escaped(out, v.as_string()); break;
        case Value::Kind::Raw: out += v.as_raw_text(); break;
        case Value::Kind::Array: {
            out += '[';
            bool first = true;
            for (const Value& item : v.as_array()) {
                if (!first) out += ',';
                first = false;
                serialize_to(out, item);
            }
            out += ']';
            break;
        }
        case Value::Kind::Object: {
            out += '{';
            bool first = true;
            for (const auto& [k, item] : v.as_object()) {
                if (!first) out += ',';
                first = false;
                append_escaped(out, k);
                out += ':';
                serialize_to(out, item);
            }
            out += '}';
            break;
        }
    }
}

bool equal_values(const Value& a, const Value& b);

bool equal_numbers(const Value& a, const Value& b) {
    if (a.is_int() && b.is_int()) return a.as_int() == b.as_int();
    if (a.is_double() && b.is_double()) return a.as_double() == b.as_double();
    return false;  // Int vs Double are distinct (INV-7).
}

bool equal_values(const Value& a, const Value& b) {
    if (a.is_raw() || b.is_raw()) {
        auto pa_res = a.is_raw() ? parse(a.as_raw_text()) : StatusOr<Value>(a);
        auto pb_res = b.is_raw() ? parse(b.as_raw_text()) : StatusOr<Value>(b);
        if (!pa_res.ok() || !pb_res.ok()) return false;
        return equal_values(*pa_res, *pb_res);
    }
    if (a.is_object() && b.is_object()) {
        const Object& oa = a.as_object();
        const Object& ob = b.as_object();
        if (oa.size() != ob.size()) return false;
        for (const auto& [k, va] : oa) {
            auto it = ob.find(k);
            if (it == ob.end() || !equal_values(va, it->second)) return false;
        }
        return true;
    }
    if (a.is_array() && b.is_array()) {
        const Array& aa = a.as_array();
        const Array& ab = b.as_array();
        if (aa.size() != ab.size()) return false;
        for (std::size_t i = 0; i < aa.size(); ++i) {
            if (!equal_values(aa[i], ab[i])) return false;
        }
        return true;
    }
    if (a.is_number() && b.is_number()) return equal_numbers(a, b);
    if (a.kind() != b.kind()) return false;
    switch (a.kind()) {
        case Value::Kind::Null: return true;
        case Value::Kind::Bool: return a.as_bool() == b.as_bool();
        case Value::Kind::String: return a.as_string() == b.as_string();
        default: return false;
    }
}

}  // namespace

StatusOr<Value> parse(std::string_view text) {
    return Parser(text).parse_document();
}

std::string serialize(const Value& v) {
    std::string out;
    serialize_to(out, v);
    return out;
}

bool structurally_equal(const Value& a, const Value& b) {
    return equal_values(a, b);
}

}  // namespace oxa::json
