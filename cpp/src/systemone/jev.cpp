// The SystemOne Jev face: Jev wire ↔ IR conversions (spec/17). Range and
// integer validation compares number tokens exactly through a normalized
// ±digits × 10^exp representation (exponents saturate instead of expanding
// adversarially long powers of ten), and verbatim carries preserve the
// source number spelling through the JSON span facility (INV-1 discipline).

#include "oxa/systemone/jev.hpp"

#include <algorithm>
#include <charconv>
#include <cstdint>
#include <limits>
#include <string>
#include <utility>
#include <variant>
#include <vector>

namespace oxa::systemone::jev {
namespace {

using ir::make_loss;

// --- exact decimal comparison -----------------------------------------------

struct DecimalParts {
    bool negative = false;
    std::string digits;  // significant digits, leading/trailing zeros stripped
    std::int64_t exp = 0;
};

bool is_digit(char c) noexcept { return c >= '0' && c <= '9'; }

// Parses the exponent literal with saturation at ±2^62: the sign of the
// overflow preserves whether the value is astronomically large or small,
// which is all the range checks need.
std::int64_t parse_exponent_saturating(std::string_view literal) {
    constexpr std::int64_t kLimit = std::int64_t{1} << 62;
    bool negative = !literal.empty() && literal.front() == '-';
    std::size_t begin = 0;
    if (!literal.empty() && (literal.front() == '+' || literal.front() == '-')) ++begin;
    std::int64_t value = 0;
    for (std::size_t i = begin; i < literal.size(); ++i) {
        if (!is_digit(literal[i])) return 0;
        std::int64_t digit = literal[i] - '0';
        if (value > kLimit / 10) {
            value = kLimit;
            break;
        }
        value *= 10;
        if (value > kLimit - digit) {
            value = kLimit;
            break;
        }
        value += digit;
    }
    return negative ? -value : value;
}

bool parse_decimal(std::string_view token, DecimalParts& out) {
    bool negative = !token.empty() && token.front() == '-';
    std::string_view body = negative ? token.substr(1) : token;
    std::int64_t exponent = 0;
    const std::size_t e_index = body.find_first_of("eE");
    if (e_index != std::string_view::npos) {
        exponent = parse_exponent_saturating(body.substr(e_index + 1));
        body = body.substr(0, e_index);
    }
    std::string_view int_digits = body;
    std::string_view frac_digits;
    const std::size_t dot = body.find('.');
    if (dot != std::string_view::npos) {
        int_digits = body.substr(0, dot);
        frac_digits = body.substr(dot + 1);
    }
    if (int_digits.empty() && frac_digits.empty()) return false;
    for (char c : int_digits) {
        if (!is_digit(c)) return false;
    }
    for (char c : frac_digits) {
        if (!is_digit(c)) return false;
    }
    std::int64_t exp = exponent - static_cast<std::int64_t>(frac_digits.size());
    std::string digits = std::string(int_digits) + std::string(frac_digits);
    const std::size_t leading = digits.find_first_not_of('0');
    if (leading == std::string::npos) {
        out = DecimalParts{negative, "0", 0};
        return true;
    }
    digits = digits.substr(leading);
    const std::size_t trailing = digits.find_last_not_of('0');
    out = DecimalParts{negative, digits.substr(0, trailing + 1),
                       exp + static_cast<std::int64_t>(digits.size() - 1 - trailing)};
    return true;
}

bool decimal_is_zero(const DecimalParts& parts) noexcept { return parts.digits == "0"; }

// The source token of a parsed JSON number: spans give the original spelling
// for values that came from a parse; constructed values serialize instead.
std::string number_token(const json::Value& v) {
    if (v.is_raw()) {
        return v.as_raw_text();
    }
    if (v.source() && v.span_end() > v.span_begin()) {
        return std::string(v.raw_slice());
    }
    if (v.is_int()) {
        return std::to_string(v.as_int());
    }
    char buf[64];
    auto res = std::to_chars(buf, buf + sizeof(buf), v.as_double());
    return std::string(buf, static_cast<std::size_t>(res.ptr - buf));
}

// A JSON number either parses to Int/Double or arrives as an opaque raw
// token from parse_lossless (out-of-range literals); both compare exactly.
bool is_number_like(const json::Value& v) {
    if (v.is_number()) return true;
    if (!v.is_raw()) return false;
    DecimalParts parts;
    return parse_decimal(v.as_raw_text(), parts);
}

// Reports whether a JSON number is exactly zero in decimal terms.
bool decimal_value_is_zero(const json::Value& v) {
    DecimalParts parts;
    if (!parse_decimal(number_token(v), parts)) return false;
    return decimal_is_zero(parts);
}

// Reports whether a JSON number equals exactly 1, accepting any decimal
// spelling such as `1.0` or `1e0` (spec/17 §4.1).
bool decimal_equals_one(const json::Value& v) {
    DecimalParts parts;
    if (!parse_decimal(number_token(v), parts)) return false;
    if (decimal_is_zero(parts) || parts.negative) return false;
    return parts.exp == 0 && parts.digits == "1";
}

// Reports whether a JSON number is exactly within [0,1] in decimal terms
// (spec/17 §4.1 noul range).
bool decimal_in_unit_interval(const json::Value& v) {
    DecimalParts parts;
    if (!parse_decimal(number_token(v), parts)) return false;
    if (decimal_is_zero(parts)) return true;
    if (parts.negative) return false;
    // A positive nonzero value is within [0,1] exactly when it is 1 or a
    // decimal fraction below 1: digits × 10^exp ≤ 1 with digits ≥ 1.
    if (parts.exp > 0) return false;
    if (parts.exp == 0) return parts.digits == "1";
    // exp < 0: value = digits × 10^exp ≤ 1 ⇔ digits ≤ 10^(-exp). The bound
    // only matters when its digit count can reach digits.size(); a longer
    // bound means digits is certainly below it, and a saturated exponent
    // never materializes as a power of ten.
    const std::int64_t limit_exp = -parts.exp;
    if (limit_exp + 1 > static_cast<std::int64_t>(parts.digits.size())) return true;
    const std::string bound = "1" + std::string(static_cast<std::size_t>(limit_exp), '0');
    if (parts.digits.size() < bound.size()) return true;
    return parts.digits.size() == bound.size() && parts.digits <= bound;
}

// Validates a score value against the discrete 1..N levels exactly: the
// token must be an integer whose exact value falls in range (spec/17 §4.1).
bool score_token_within_levels(const json::Value& v, int levels) {
    DecimalParts parts;
    if (!parse_decimal(number_token(v), parts)) return false;
    if (decimal_is_zero(parts) || parts.negative) return false;
    // A fractional value (negative exp after trailing-zero stripping) is not
    // an integer level; a positive exp of 2 or more is at least 100, beyond
    // the 1..10 level bound.
    if (parts.exp < 0 || parts.exp > 2) return false;
    const std::string value_digits =
        parts.digits + std::string(static_cast<std::size_t>(parts.exp), '0');
    const std::string level_digits = std::to_string(levels);
    if (value_digits.size() != level_digits.size()) {
        return value_digits.size() < level_digits.size();
    }
    return value_digits <= level_digits;
}

// Validates an enum of ascending 1..N integers exactly and returns N.
std::optional<int> score_levels(const json::Value& enum_value) {
    if (!enum_value.is_array()) return std::nullopt;
    const auto& items = enum_value.as_array();
    if (items.empty()) return std::nullopt;
    for (std::size_t i = 0; i < items.size(); ++i) {
        if (!is_number_like(items[i])) return std::nullopt;
        DecimalParts parts;
        if (!parse_decimal(number_token(items[i]), parts)) return std::nullopt;
        if (decimal_is_zero(parts) || parts.negative || parts.exp < 0) return std::nullopt;
        const std::string expected = std::to_string(i + 1);
        // The value must render as the plain integer digits; bounding the
        // digit count by expected.size() keeps the shift bounded too.
        if (parts.exp + static_cast<std::int64_t>(parts.digits.size()) >
            static_cast<std::int64_t>(expected.size())) {
            return std::nullopt;
        }
        std::string value = parts.digits;
        if (parts.exp > 0) {
            value += std::string(static_cast<std::size_t>(parts.exp), '0');
        }
        if (value != expected) return std::nullopt;
    }
    return static_cast<int>(items.size());
}

// --- verbatim carries ---------------------------------------------------------

// Removes whitespace outside JSON string literals, preserving key order and
// number tokens — the wire-side equivalent of Go's json.Compact on raw
// carries.
std::string compact_json_text(std::string_view raw) {
    std::string out;
    out.reserve(raw.size());
    bool in_string = false;
    bool escaped = false;
    for (char c : raw) {
        if (in_string) {
            out += c;
            if (escaped) {
                escaped = false;
            } else if (c == '\\') {
                escaped = true;
            } else if (c == '"') {
                in_string = false;
            }
            continue;
        }
        switch (c) {
            case '"':
                in_string = true;
                out += c;
                break;
            case ' ':
            case '\n':
            case '\r':
            case '\t':
                break;
            default:
                out += c;
                break;
        }
    }
    return out;
}

// Rebuilds a value structurally with every number replaced by its source
// token as an opaque raw node (INV-1); used on the encode side, where the
// wire answers stay typed objects.
json::Value deep_raw_numbers(const json::Value& v) {
    switch (v.kind()) {
        case json::Value::Kind::Int:
        case json::Value::Kind::Double:
            return json::Value::raw(number_token(v));
        case json::Value::Kind::Array: {
            json::Value out = json::Value::array();
            for (const auto& item : v.as_array()) out.push_back(deep_raw_numbers(item));
            return out;
        }
        case json::Value::Kind::Object: {
            json::Value out = json::Value::object();
            for (const auto& [key, child] : v.as_object()) {
                out.set(key, deep_raw_numbers(child));
            }
            return out;
        }
        default:
            return v;
    }
}

// Rebuilds a value as an opaque raw node so serialization preserves the
// source spelling: numbers keep their tokens and nested objects/arrays keep
// their inner key order (spec/17 §7.5/§7.6; INV-1). Nodes without a source
// span (typed entrypoints) fall back to a sorted-key reconstruction.
json::Value deep_raw(const json::Value& v) {
    switch (v.kind()) {
        case json::Value::Kind::Int:
        case json::Value::Kind::Double:
            return json::Value::raw(number_token(v));
        case json::Value::Kind::Array:
        case json::Value::Kind::Object:
            if (v.source() && v.span_end() > v.span_begin()) {
                return json::Value::raw(compact_json_text(v.raw_slice()));
            }
            if (v.kind() == json::Value::Kind::Array) {
                json::Value out = json::Value::array();
                for (const auto& item : v.as_array()) out.push_back(deep_raw(item));
                return out;
            }
            {
                json::Value out = json::Value::object();
                for (const auto& [key, child] : v.as_object()) out.set(key, deep_raw(child));
                return out;
            }
        default:
            return v;
    }
}

// --- source-order loss collection ---------------------------------------------

// Collects losses keyed by their node's source position so the final list
// follows source encounter order (spec/02 §4); nodes without a source span
// (typed entrypoints and encode-side losses) keep collection order.
class LossCollector {
public:
    void add(const json::Value& node, ir::Loss loss) {
        std::size_t position;
        if (node.source() && node.span_end() > node.span_begin()) {
            position = node.span_begin();
        } else {
            position = fallback_++;
        }
        entries_.push_back({position, std::move(loss)});
    }

    void add(const json::Value& node, std::string path, std::string field,
             std::string_view reason, std::string detail) {
        add(node, make_loss(std::move(path), std::move(field), reason, std::move(detail)));
    }

    std::vector<ir::Loss> sorted() && {
        std::stable_sort(entries_.begin(), entries_.end(),
                         [](const Entry& a, const Entry& b) { return a.position < b.position; });
        std::vector<ir::Loss> out;
        out.reserve(entries_.size());
        for (auto& entry : entries_) out.push_back(std::move(entry.loss));
        return out;
    }

private:
    struct Entry {
        std::size_t position;
        ir::Loss loss;
    };
    std::vector<Entry> entries_;
    std::size_t fallback_ = 0;
};

bool is_supported_question_type(std::string_view kind) noexcept {
    return kind == QUESTION_TYPE_NOUL || kind == QUESTION_TYPE_CHOICE ||
           kind == QUESTION_TYPE_SCORE;
}

std::string whole_unit_skip_detail(std::string_view kind) {
    if (kind == QUESTION_TYPE_BOX) {
        return "N-JEV-5: experimental box questions are out of scope";
    }
    return "N-JEV-6: skipped from schema and prompt";
}

bool has_criteria(const std::optional<json::Value>& criteria) noexcept {
    return criteria.has_value() && !criteria->is_null();
}

// --- shared decode walk -------------------------------------------------------

struct DecodedRequest {
    std::string model;
    std::map<std::string, Question> questions;
    std::string state_text;
};

// Audits one supported question's fields; returns the question parts.
Status question_parts(std::string_view id, const json::Value& question, LossCollector& losses,
                      Question& out) {
    for (const auto& [field, leaf] : question.as_object()) {
        if (field == "type") {
            if (!leaf.is_string()) {
                return invalid_argument("jev: question " + std::string(id) +
                                        " type must be a string");
            }
            out.type = leaf.as_string();
        } else if (field == "instructions") {
            if (!leaf.is_string()) {
                return invalid_argument("jev: question " + std::string(id) +
                                        " instructions must be a string");
            }
            out.instructions = leaf.as_string();
        } else if (field == "criteria") {
            out.criteria = leaf.is_null() ? std::nullopt : std::optional<json::Value>(leaf);
        } else {
            losses.add(leaf, "questions." + std::string(id), std::string(field),
                       ir::LOSS_UNMAPPED_FIELD, "unknown property on the Jev question");
        }
    }
    if (out.type.empty()) {
        return invalid_argument("jev: question " + std::string(id) + " type must be a string");
    }
    return Status();
}

// Criteria shapes are structural (spec/17 §1/§2): noul carries an optional
// object, choice a non-empty object, score a non-empty array.
Status validate_criteria(std::string_view id, std::string_view kind,
                         const std::optional<json::Value>& criteria) {
    if (kind == QUESTION_TYPE_NOUL) {
        if (has_criteria(criteria) && !criteria->is_object()) {
            return invalid_argument("jev: question " + std::string(id) +
                                    " noul criteria must be an object");
        }
        return Status();
    }
    if (!has_criteria(criteria)) {
        return invalid_argument("jev: question " + std::string(id) + " " + std::string(kind) +
                                " criteria is required");
    }
    if (kind == QUESTION_TYPE_CHOICE) {
        if (!criteria->is_object()) {
            return invalid_argument("jev: question " + std::string(id) +
                                    " choice criteria must be an object");
        }
        if (criteria->as_object().empty()) {
            return invalid_argument("jev: question " + std::string(id) +
                                    " choice criteria must not be empty");
        }
    } else {
        if (!criteria->is_array()) {
            return invalid_argument("jev: question " + std::string(id) +
                                    " score criteria must be an array");
        }
        if (criteria->as_array().empty()) {
            return invalid_argument("jev: question " + std::string(id) +
                                    " score criteria must not be empty");
        }
    }
    return Status();
}

StatusOr<DecodedRequest> walk_request(const json::Value& wire, LossCollector& losses) {
    const json::Value* model_value = nullptr;
    const json::Value* state_value = nullptr;
    const json::Value* questions_value = nullptr;
    bool state_seen = false;
    bool questions_seen = false;

    for (const auto& [key, leaf] : wire.as_object()) {
        if (key == "model") {
            if (!leaf.is_string()) {
                return invalid_argument("jev: request model must be a string");
            }
            model_value = &leaf;
        } else if (key == "state") {
            state_seen = true;
            if (leaf.is_null() || leaf.is_string() || leaf.is_object() || leaf.is_array()) {
                if (leaf.is_object() || leaf.is_array()) {
                    losses.add(leaf, "state", "state", ir::LOSS_DEGRADED,
                               "N-JEV-1: state serialized to canonical JSON text");
                }
                state_value = &leaf;
            } else {
                return invalid_argument("jev: state must be a string, object, or array");
            }
        } else if (key == "questions") {
            if (!leaf.is_object()) {
                return invalid_argument("jev: request questions must be an object");
            }
            if (leaf.as_object().empty()) {
                return invalid_argument("jev: request questions must not be empty");
            }
            questions_value = &leaf;
            questions_seen = true;
        } else if (key == "assets") {
            losses.add(leaf, "assets", "assets", ir::LOSS_UNSUPPORTED_SEMANTIC,
                       "N-JEV-4: experimental image assets are out of scope");
        } else {
            losses.add(leaf, key, key, ir::LOSS_UNMAPPED_FIELD,
                       "unknown property on the Jev request");
        }
    }

    if (model_value == nullptr || model_value->as_string().empty()) {
        return invalid_argument("jev: request model is required");
    }
    if (!state_seen || state_value == nullptr || state_value->is_null()) {
        return invalid_argument("jev: request state is required");
    }
    if (!questions_seen) {
        return invalid_argument("jev: request questions must not be empty");
    }

    DecodedRequest out;
    out.model = model_value->as_string();
    // State canon (spec/17 §7.6): keys sorted at every depth, compact,
    // exact number tokens.
    out.state_text =
        state_value->is_string() ? state_value->as_string() : json::serialize(deep_raw(*state_value));

    for (const auto& [id, question_value] : questions_value->as_object()) {
        if (!question_value.is_object()) {
            return invalid_argument("jev: question " + id + " must be an object");
        }
        const json::Value* type_value = question_value.find("type");
        if (type_value == nullptr || !type_value->is_string()) {
            return invalid_argument("jev: question " + id + " type must be a string");
        }
        const std::string kind = type_value->as_string();
        if (kind == QUESTION_TYPE_BOX || !is_supported_question_type(kind)) {
            // Whole-unit skips suppress the per-field audit inside the unit,
            // per spec/02 §4.
            losses.add(*type_value, "questions." + id, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                       whole_unit_skip_detail(kind));
            continue;
        }
        Question question;
        OXA_RETURN_IF_ERROR(question_parts(id, question_value, losses, question));
        OXA_RETURN_IF_ERROR(validate_criteria(id, question.type, question.criteria));
        out.questions.emplace(id, std::move(question));
    }
    if (out.questions.empty()) {
        return invalid_argument("jev: request questions produced no supported questions");
    }
    return out;
}

// --- §7.1/§7.2 prompt rendering -------------------------------------------------

std::string criteria_line(std::string_view name, const json::Value& description) {
    if (description.is_null()) {
        return "- " + std::string(name);
    }
    if (description.is_string()) {
        const std::string& text = description.as_string();
        if (text.empty()) return "- " + std::string(name);
        return "- " + std::string(name) + ": " + text;
    }
    return "- " + std::string(name) + ": " + json::serialize(deep_raw(description));
}

std::string render_user_text(const std::map<std::string, Question>& questions) {
    // §7.2 user text: ascending lexicographic ID order (std::map order).
    std::string out;
    for (const auto& [id, question] : questions) {
        if (!out.empty()) out += '\n';
        out += "question " + id + " (" + question.type + "): ";
        out += question.instructions.value_or("");
        std::vector<std::string> criteria_lines;
        if (question.type == QUESTION_TYPE_NOUL && has_criteria(question.criteria)) {
            criteria_lines.emplace_back("criteria:");
            for (const char* name : {"false", "true"}) {
                const json::Value* leaf = question.criteria->find(name);
                if (leaf != nullptr) criteria_lines.push_back(criteria_line(name, *leaf));
            }
        } else if (question.type == QUESTION_TYPE_CHOICE) {
            criteria_lines.emplace_back("criteria:");
            for (const auto& [name, leaf] : question.criteria->as_object()) {
                criteria_lines.push_back(criteria_line(name, leaf));
            }
        } else if (question.type == QUESTION_TYPE_SCORE) {
            criteria_lines.emplace_back("criteria:");
            for (std::size_t index = 0; index < question.criteria->as_array().size(); ++index) {
                criteria_lines.push_back(criteria_line(
                    "level " + std::to_string(index + 1), question.criteria->as_array()[index]));
            }
        }
        for (const auto& line : criteria_lines) {
            out += '\n';
            out += line;
        }
    }
    return out;
}

// §7.3 synthesized schema: property keys in ascending lexicographic ID order.
StatusOr<ir::ResponseFormat> synthesized_response_format(
    const std::map<std::string, Question>& questions) {
    json::Value properties = json::Value::object();
    json::Value required = json::Value::array();
    for (const auto& [id, question] : questions) {
        json::Value property = json::Value::object();
        if (question.type == QUESTION_TYPE_NOUL) {
            property.set("type", json::Value::string("number"));
            property.set("minimum", json::Value::integer(0));
            property.set("maximum", json::Value::integer(1));
        } else if (question.type == QUESTION_TYPE_CHOICE) {
            if (!has_criteria(question.criteria) || question.criteria->as_object().empty()) {
                return invalid_argument("jev: question choice criteria must not be empty");
            }
            property.set("type", json::Value::string("string"));
            json::Value names = json::Value::array();
            for (const auto& [name, leaf] : question.criteria->as_object()) {
                names.push_back(json::Value::string(name));
            }
            property.set("enum", std::move(names));
        } else {
            if (!has_criteria(question.criteria) || question.criteria->as_array().empty()) {
                return invalid_argument("jev: question score criteria must not be empty");
            }
            property.set("type", json::Value::string("number"));
            json::Value levels = json::Value::array();
            const std::size_t count = question.criteria->as_array().size();
            for (std::size_t i = 0; i < count; ++i) {
                levels.push_back(json::Value::integer(static_cast<std::int64_t>(i + 1)));
            }
            property.set("enum", std::move(levels));
        }
        if (question.instructions.has_value() && !question.instructions->empty()) {
            property.set("description", json::Value::string(*question.instructions));
        }
        properties.set(id, std::move(property));
        required.push_back(json::Value::string(id));
    }
    json::Value schema = json::Value::object();
    schema.set("type", json::Value::string("object"));
    schema.set("properties", std::move(properties));
    schema.set("required", std::move(required));
    schema.set("additionalProperties", json::Value::boolean(false));
    ir::ResponseFormat format;
    format.type = "json_schema";
    format.name = std::string(RESPONSE_FORMAT_NAME);
    format.schema = std::move(schema);
    return format;
}

// --- §7.4 schema reconstruction -------------------------------------------------

// Reports whether the property carries a key outside the branch's
// synthesized projection (spec/17 §7.4 exact-shape reconstruction).
bool keys_beyond(const json::Value& property, std::initializer_list<std::string_view> allowed) {
    for (const auto& [key, leaf] : property.as_object()) {
        bool beyond = true;
        for (std::string_view a : allowed) {
            if (key == a) {
                beyond = false;
                break;
            }
        }
        if (beyond) return true;
    }
    return false;
}

ir::Loss beyond_projection_loss(std::string_view id) {
    return make_loss("questions." + std::string(id), "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                     "N-JEV-6: schema constraint beyond the synthesized projection");
}

// Reconstructs questions from the synthesized schema's properties (§7.4,
// the one authorized read-only consumption of Params.ResponseFormat.Schema).
StatusOr<std::pair<std::map<std::string, Question>, std::vector<ir::Loss>>> questions_from_schema(
    const json::Value& schema) {
    const json::Value* properties = schema.find("properties");
    if (properties == nullptr || !properties->is_object() || properties->as_object().empty()) {
        return invalid_argument("jev: response format schema has no properties");
    }
    std::map<std::string, Question> questions;
    std::vector<ir::Loss> losses;
    for (const auto& [id, property] : properties->as_object()) {
        if (!property.is_object()) {
            return invalid_argument("jev: schema property " + id + " must be an object");
        }
        const json::Value* type_value = property.find("type");
        const std::string prop_type =
            type_value != nullptr && type_value->is_string() ? type_value->as_string() : "";
        const json::Value* description = property.find("description");
        std::optional<std::string> instructions;
        if (description != nullptr && description->is_string()) {
            instructions = description->as_string();
        }
        const json::Value* enum_value = property.find("enum");
        const bool has_enum = enum_value != nullptr && enum_value->is_array();
        if (prop_type == "number") {
            if (has_enum) {
                if (keys_beyond(property, {"type", "enum", "description"})) {
                    losses.push_back(beyond_projection_loss(id));
                    continue;
                }
                auto levels = score_levels(*enum_value);
                if (!levels.has_value()) {
                    losses.push_back(
                        make_loss("questions." + id, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                                  "N-JEV-6: skipped from schema and prompt"));
                    continue;
                }
                Question question;
                question.type = std::string(QUESTION_TYPE_SCORE);
                question.instructions = instructions;
                question.criteria =
                    json::Value::array(std::vector<json::Value>(*levels, json::Value::string("")));
                questions.emplace(id, std::move(question));
                losses.push_back(
                    make_loss("questions." + id, "criteria", ir::LOSS_DEGRADED,
                              "N-JEV-9: criteria descriptions live in the prompt text"));
                continue;
            }
            const json::Value* minimum = property.find("minimum");
            const json::Value* maximum = property.find("maximum");
            if (minimum != nullptr && minimum->is_number() && maximum != nullptr &&
                maximum->is_number() && decimal_value_is_zero(*minimum) &&
                decimal_equals_one(*maximum) &&
                !keys_beyond(property, {"type", "minimum", "maximum", "description"})) {
                Question question;
                question.type = std::string(QUESTION_TYPE_NOUL);
                question.instructions = instructions;
                questions.emplace(id, std::move(question));
                continue;
            }
        } else if (prop_type == "string") {
            if (has_enum) {
                if (keys_beyond(property, {"type", "enum", "description"})) {
                    losses.push_back(beyond_projection_loss(id));
                    continue;
                }
                bool all_strings = !enum_value->as_array().empty();
                for (const auto& name : enum_value->as_array()) {
                    if (!name.is_string()) all_strings = false;
                }
                if (all_strings) {
                    json::Value criteria = json::Value::object();
                    for (const auto& name : enum_value->as_array()) {
                        criteria.set(name.as_string(), json::Value::string(name.as_string()));
                    }
                    Question question;
                    question.type = std::string(QUESTION_TYPE_CHOICE);
                    question.instructions = instructions;
                    question.criteria = std::move(criteria);
                    questions.emplace(id, std::move(question));
                    losses.push_back(
                        make_loss("questions." + id, "criteria", ir::LOSS_DEGRADED,
                                  "N-JEV-9: criteria descriptions live in the prompt text"));
                    continue;
                }
            }
        }
        losses.push_back(make_loss("questions." + id, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                                   "N-JEV-6: skipped from schema and prompt"));
    }
    if (questions.empty()) {
        return invalid_argument("jev: response format schema has no reconstructable questions");
    }
    return std::make_pair(std::move(questions), std::move(losses));
}

// --- §3.1 IR → Jev request -------------------------------------------------------

std::string strip_template(std::string_view text) {
    const std::string suffix = std::string("\n\n") + std::string(INSTRUCTION_TEMPLATE);
    if (text.size() >= suffix.size() && text.substr(text.size() - suffix.size()) == suffix) {
        return std::string(text.substr(0, text.size() - suffix.size()));
    }
    return std::string(text);
}

// Every block must be a text block, every line a question line, a criteria
// line, or blank, and the question (id, type) set must match; mixed-in
// non-text blocks have no Jev destination and must surface as a loss.
bool is_derived_question_rendering(const std::vector<ir::Block>& content,
                                   const std::map<std::string, std::string>& expected) {
    std::vector<std::string> texts;
    for (const auto& block : content) {
        const auto* text_block = std::get_if<ir::TextBlock>(&block);
        if (text_block == nullptr) return false;
        texts.push_back(text_block->text);
    }
    if (texts.empty()) return false;
    std::map<std::string, std::string> found;
    for (const auto& text : texts) {
        std::size_t begin = 0;
        while (begin <= text.size()) {
            const std::size_t end = text.find('\n', begin);
            const std::string_view line = std::string_view(text).substr(
                begin, (end == std::string::npos ? text.size() : end) - begin);
            if (!(line.empty() || line == "criteria:" || line.starts_with("- "))) {
                if (!line.starts_with("question ")) return false;
                const std::string_view rest = line.substr(std::string_view("question ").size());
                const std::size_t open = rest.find(" (");
                if (open == std::string_view::npos) return false;
                const std::size_t close = rest.find("): ", open);
                if (close == std::string_view::npos) return false;
                std::string identifier(rest.substr(0, open));
                if (found.count(identifier) != 0) return false;
                found.emplace(std::move(identifier),
                              std::string(rest.substr(open + 2, close - open - 2)));
            }
            if (end == std::string::npos) break;
            begin = end + 1;
        }
    }
    if (found.size() != expected.size()) return false;
    for (const auto& [id, kind] : expected) {
        auto it = found.find(id);
        if (it == found.end() || it->second != kind) return false;
    }
    return true;
}

StatusOr<std::pair<std::string, std::vector<ir::Loss>>> state_from_ir(
    const ir::Request& request, const std::map<std::string, Question>& questions) {
    std::string joined;
    for (const auto& block : request.system) {
        if (!block.text.empty()) {
            if (!joined.empty()) joined += "\n\n";
            joined += block.text;
        }
    }
    if (joined.empty()) {
        return invalid_argument("jev: EncodeRequest requires system text");
    }
    std::map<std::string, std::string> expected;
    for (const auto& [id, question] : questions) expected.emplace(id, question.type);
    std::vector<ir::Loss> losses;
    for (std::size_t index = 0; index < request.messages.size(); ++index) {
        const auto& message = request.messages[index];
        if (message.role == "user" && is_derived_question_rendering(message.content, expected)) {
            continue;
        }
        losses.push_back(make_loss(
            "messages[" + std::to_string(index) + "]", "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
            "N-JEV-10: not the derived question rendering; Jev has no conversation turns"));
    }
    return std::make_pair(strip_template(joined), std::move(losses));
}

std::vector<ir::Loss> request_losses(const ir::Request& request) {
    std::vector<ir::Loss> losses;
    const auto jev10 = [](std::string path, std::string field) {
        return make_loss(std::move(path), std::move(field), ir::LOSS_UNMAPPED_FIELD,
                         "N-JEV-10: one loss per present field");
    };
    if (request.params.has_value()) {
        const auto& params = *request.params;
        if (params.temperature.has_value()) losses.push_back(jev10("params", "temperature"));
        if (params.top_p.has_value()) losses.push_back(jev10("params", "top_p"));
        if (params.max_tokens.has_value()) losses.push_back(jev10("params", "max_tokens"));
        if (params.stop_sequences.has_value()) {
            losses.push_back(jev10("params", "stop_sequences"));
        }
        if (params.reasoning_effort.has_value()) {
            losses.push_back(jev10("params", "reasoning_effort"));
        }
        if (params.verbosity.has_value()) losses.push_back(jev10("params", "verbosity"));
    }
    const auto no_equivalent = [](std::string path, std::string field) {
        return make_loss(std::move(path), std::move(field), ir::LOSS_UNMAPPED_FIELD,
                         "N-JEV-10: no Jev equivalent");
    };
    if (request.tools.has_value() && !request.tools->empty()) {
        losses.push_back(no_equivalent("tools", "tools"));
    }
    if (request.tool_choice.has_value()) {
        losses.push_back(no_equivalent("tool_choice", "tool_choice"));
    }
    if (request.metadata.has_value()) {
        for (const auto& [key, value] : *request.metadata) {
            losses.push_back(no_equivalent("metadata", key));
        }
    }
    if (!request.native.empty()) {
        losses.push_back(no_equivalent("native", "native"));
    }
    for (std::size_t index = 0; index < request.tool_change_groups.size(); ++index) {
        losses.push_back(make_loss(
            "tool_change_groups[" + std::to_string(index) + "]", "tool_change_groups",
            ir::LOSS_UNSUPPORTED_SEMANTIC,
            "N-JEV-10: positional tool availability changes have no Jev equivalent"));
    }
    return losses;
}

// --- §4.1 IR → Jev response -------------------------------------------------------

ir::Loss out_of_range_loss(std::string_view id, std::string_view field) {
    return make_loss("answers." + std::string(id), std::string(field), ir::LOSS_DEGRADED,
                     "N-JEV-14: value kept, not rewritten");
}

ir::Loss degraded_type_loss(std::string_view id) {
    return make_loss("answers." + std::string(id), "type", ir::LOSS_DEGRADED,
                     "N-JEV-13: noul/score are ambiguous without question context");
}

ir::Loss contradiction_loss(std::string_view id) {
    return make_loss("answers." + std::string(id), "type", ir::LOSS_UNMAPPED_VALUE,
                     "N-JEV-12: carried answer contradicts the original question");
}

ir::Loss box_answer_loss(std::string_view id) {
    return make_loss("answers." + std::string(id), "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                     "N-JEV-5: experimental box answers are carried verbatim");
}

std::vector<std::string> choice_names(const Question& question) {
    if (!has_criteria(question.criteria) || !question.criteria->is_object()) return {};
    std::vector<std::string> names;
    for (const auto& [name, leaf] : question.criteria->as_object()) names.push_back(name);
    return names;
}

bool is_choice_out_of_range(const Question& question, const json::Value& value) {
    const auto names = choice_names(question);
    return !names.empty() &&
           std::find(names.begin(), names.end(), value.as_string()) == names.end();
}

bool score_out_of_range(const Question& question, const json::Value& value) {
    if (!has_criteria(question.criteria) || !question.criteria->is_array()) return false;
    const auto levels = static_cast<int>(question.criteria->as_array().size());
    return levels > 0 && !score_token_within_levels(value, levels);
}

// Reports the range/option losses of an answer against its question context
// using exact decimal comparison; value-shape failures stay structural.
Status validate_answer(std::string_view id, const json::Value& answer, std::string_view kind,
                       const Question* question, std::optional<ir::Loss>& loss) {
    if (kind == QUESTION_TYPE_NOUL) {
        const json::Value* leaf = answer.find("noul");
        if (leaf == nullptr) {
            return invalid_argument("jev: answer " + std::string(id) + " carries no noul value");
        }
        if (leaf->is_bool()) return Status();
        if (!is_number_like(*leaf)) {
            return invalid_argument("jev: answer " + std::string(id) + " carries no noul value");
        }
        if (question == nullptr || question->type != QUESTION_TYPE_NOUL) return Status();
        if (!decimal_in_unit_interval(*leaf)) loss = out_of_range_loss(id, "noul");
        return Status();
    }
    if (kind == QUESTION_TYPE_CHOICE) {
        const json::Value* leaf = answer.find("choice");
        if (leaf == nullptr || !leaf->is_string()) {
            return invalid_argument("jev: answer " + std::string(id) + " carries no choice value");
        }
        if (question == nullptr) return Status();
        if (is_choice_out_of_range(*question, *leaf)) loss = out_of_range_loss(id, "choice");
        return Status();
    }
    if (kind == QUESTION_TYPE_SCORE) {
        const json::Value* leaf = answer.find("score");
        if (leaf == nullptr || !is_number_like(*leaf)) {
            return invalid_argument("jev: answer " + std::string(id) + " carries no score value");
        }
        if (question == nullptr) return Status();
        if (score_out_of_range(*question, *leaf)) loss = out_of_range_loss(id, "score");
        return Status();
    }
    return Status();
}

json::Value typed_value(std::string_view kind, const char* field, json::Value leaf) {
    json::Value wire = json::Value::object();
    wire.set("type", json::Value::string(std::string(kind)));
    wire.set(field, std::move(leaf));
    return wire;
}

// Scalar values follow the output contract; with question context the
// question's type governs and a scalar that cannot express it carries
// verbatim with N-JEV-12.
StatusOr<std::pair<json::Value, std::optional<ir::Loss>>> recover_scalar(
    std::string_view id, const json::Value& value, const Question* question) {
    if (question != nullptr) {
        const std::string& qtype = question->type;
        if (qtype == QUESTION_TYPE_NOUL) {
            if (value.is_bool()) {
                return std::make_pair(
                    typed_value(QUESTION_TYPE_NOUL, "noul",
                                json::Value::integer(value.as_bool() ? 1 : 0)),
                    std::optional<ir::Loss>());
            }
            if (is_number_like(value)) {
                std::optional<ir::Loss> loss;
                if (!decimal_in_unit_interval(value)) loss = out_of_range_loss(id, "noul");
                return std::make_pair(
                    typed_value(QUESTION_TYPE_NOUL, "noul", deep_raw_numbers(value)), std::move(loss));
            }
            return std::make_pair(deep_raw_numbers(value),
                                  std::optional<ir::Loss>(contradiction_loss(id)));
        }
        if (qtype == QUESTION_TYPE_CHOICE) {
            if (value.is_string()) {
                std::optional<ir::Loss> loss;
                if (is_choice_out_of_range(*question, value)) {
                    loss = out_of_range_loss(id, "choice");
                }
                return std::make_pair(
                    typed_value(QUESTION_TYPE_CHOICE, "choice",
                                json::Value::string(value.as_string())),
                    std::move(loss));
            }
            return std::make_pair(deep_raw_numbers(value),
                                  std::optional<ir::Loss>(contradiction_loss(id)));
        }
        if (qtype == QUESTION_TYPE_SCORE) {
            if (is_number_like(value)) {
                std::optional<ir::Loss> loss;
                if (score_out_of_range(*question, value)) loss = out_of_range_loss(id, "score");
                return std::make_pair(
                    typed_value(QUESTION_TYPE_SCORE, "score", deep_raw_numbers(value)), std::move(loss));
            }
            return std::make_pair(deep_raw(value), std::optional<ir::Loss>(contradiction_loss(id)));
        }
    }
    if (value.is_bool()) {
        return std::make_pair(
            typed_value(QUESTION_TYPE_NOUL, "noul",
                        json::Value::integer(value.as_bool() ? 1 : 0)),
            std::optional<ir::Loss>());
    }
    if (value.is_string()) {
        return std::make_pair(
            typed_value(QUESTION_TYPE_CHOICE, "choice", json::Value::string(value.as_string())),
            std::optional<ir::Loss>());
    }
    if (is_number_like(value)) {
        return std::make_pair(typed_value(QUESTION_TYPE_SCORE, "score", deep_raw(value)),
                              std::optional<ir::Loss>(degraded_type_loss(id)));
    }
    return invalid_argument("jev: answer " + std::string(id) + " must be an object or scalar value");
}

// Object answers are carried verbatim; a carried type that contradicts the
// matching question also carries verbatim rather than rewrite (N-JEV-12).
StatusOr<std::pair<json::Value, std::optional<ir::Loss>>> recover_object(
    std::string_view id, const json::Value& answer, const Question* question) {
    const json::Value* carried_value = answer.find("type");
    const bool has_carried = carried_value != nullptr && carried_value->is_string();
    const std::string carried = has_carried ? carried_value->as_string() : "";
    if (has_carried && carried == QUESTION_TYPE_BOX) {
        return std::make_pair(deep_raw_numbers(answer),
                              std::optional<ir::Loss>(box_answer_loss(id)));
    }
    if (question != nullptr && has_carried && carried != question->type) {
        return std::make_pair(deep_raw_numbers(answer),
                              std::optional<ir::Loss>(contradiction_loss(id)));
    }
    std::string kind;
    if (has_carried) {
        kind = carried;
    } else if (question != nullptr) {
        kind = question->type;
    } else if (answer.find("noul") != nullptr) {
        kind = std::string(QUESTION_TYPE_NOUL);
    } else if (answer.find("choice") != nullptr) {
        kind = std::string(QUESTION_TYPE_CHOICE);
    } else if (answer.find("score") != nullptr) {
        kind = std::string(QUESTION_TYPE_SCORE);
    } else {
        return invalid_argument("jev: answer " + std::string(id) + " carries no recognized value");
    }
    const json::Value* noul = answer.find("noul");
    const bool rewrite = !has_carried || (kind == QUESTION_TYPE_NOUL && noul != nullptr &&
                                          noul->is_bool());
    json::Value wire;
    if (rewrite) {
        wire = json::Value::object();
        for (const auto& [field, leaf] : answer.as_object()) {
            wire.set(field, deep_raw_numbers(leaf));
        }
        wire.set("type", json::Value::string(kind));
        const json::Value* carried_noul = wire.find("noul");
        if (kind == QUESTION_TYPE_NOUL && carried_noul != nullptr && carried_noul->is_bool()) {
            wire.set("noul", json::Value::integer(carried_noul->as_bool() ? 1 : 0));
        }
    } else {
        wire = deep_raw_numbers(answer);
    }
    std::optional<ir::Loss> loss;
    OXA_RETURN_IF_ERROR(validate_answer(id, answer, kind, question, loss));
    return std::make_pair(std::move(wire), std::move(loss));
}

}  // namespace

// --- public entrypoints -----------------------------------------------------------

StatusOr<Conversion<ir::Request>> decode_request(const json::Value& wire, const Options& opts) {
    if (!wire.is_object()) {
        return invalid_argument("jev: request JSON must be an object");
    }
    LossCollector collector;
    OXA_ASSIGN_OR_RETURN(DecodedRequest decoded, walk_request(wire, collector));
    std::vector<ir::Loss> losses = std::move(collector).sorted();
    std::string user_text = render_user_text(decoded.questions);
    OXA_ASSIGN_OR_RETURN(ir::ResponseFormat format,
                         synthesized_response_format(decoded.questions));
    ir::Request request;
    request.model = opts.model_map.map(decoded.model);
    request.system.push_back(ir::TextBlock(decoded.state_text + "\n\n" +
                                           std::string(INSTRUCTION_TEMPLATE)));
    ir::Message message;
    message.role = "user";
    message.content.push_back(ir::TextBlock(std::move(user_text)));
    request.messages.push_back(std::move(message));
    ir::Params params;
    params.response_format = std::move(format);
    request.params = std::move(params);
    return Conversion<ir::Request>{std::move(request), std::move(losses)};
}

StatusOr<Conversion<ir::Request>> decode_request(std::string_view wire_json,
                                                 const Options& opts) {
    OXA_ASSIGN_OR_RETURN(json::Value wire, json::parse_lossless(wire_json));
    return decode_request(wire, opts);
}

StatusOr<Conversion<json::Value>> encode_request(const ir::Request& req, const Options& opts) {
    const ir::ResponseFormat* format = nullptr;
    if (req.params.has_value() && req.params->response_format.has_value()) {
        format = &*req.params->response_format;
    }
    if (format == nullptr || format->type != "json_schema" || !format->schema.has_value()) {
        return invalid_argument("jev: EncodeRequest requires a json_schema response format");
    }
    OXA_ASSIGN_OR_RETURN(auto questions_res, questions_from_schema(*format->schema));
    auto& [questions, losses] = questions_res;

    OXA_ASSIGN_OR_RETURN(auto state_res, state_from_ir(req, questions));
    auto& [state, state_losses] = state_res;
    for (auto& loss : state_losses) losses.push_back(std::move(loss));
    for (auto& loss : request_losses(req)) losses.push_back(std::move(loss));

    json::Value questions_wire = json::Value::object();
    for (const auto& [id, question] : questions) {
        json::Value wire_question = json::Value::object();
        wire_question.set("type", json::Value::string(question.type));
        if (question.instructions.has_value() && !question.instructions->empty()) {
            wire_question.set("instructions", json::Value::string(*question.instructions));
        }
        if (question.criteria.has_value()) {
            wire_question.set("criteria", deep_raw_numbers(*question.criteria));
        }
        questions_wire.set(id, std::move(wire_question));
    }
    json::Value wire = json::Value::object();
    wire.set("model", json::Value::string(opts.model_map.map(req.model)));
    wire.set("state", json::Value::string(std::move(state)));
    wire.set("questions", std::move(questions_wire));
    return Conversion<json::Value>{std::move(wire), std::move(losses)};
}

StatusOr<Conversion<std::string>> encode_request_json(const ir::Request& req,
                                                      const Options& opts) {
    OXA_ASSIGN_OR_RETURN(Conversion<json::Value> converted, encode_request(req, opts));
    return Conversion<std::string>{json::serialize(converted.value), std::move(converted.losses)};
}

StatusOr<Conversion<ir::Response>> decode_response(const json::Value& wire, const Options& opts) {
    (void)opts;
    if (!wire.is_object()) {
        return invalid_argument("jev: response JSON must be an object");
    }
    LossCollector collector;
    const json::Value* model_value = nullptr;
    const json::Value* answers_value = nullptr;
    const json::Value* usage_value = nullptr;
    bool model_seen = false;
    bool answers_seen = false;
    bool usage_seen = false;

    for (const auto& [key, leaf] : wire.as_object()) {
        if (key == "model") {
            if (!leaf.is_string()) {
                return invalid_argument("jev: response model must be a string");
            }
            model_value = &leaf;
            model_seen = true;
        } else if (key == "answers") {
            if (!leaf.is_object() || leaf.as_object().empty()) {
                return invalid_argument("jev: response answers must not be empty");
            }
            answers_value = &leaf;
            answers_seen = true;
        } else if (key == "usage") {
            if (!leaf.is_object()) {
                return invalid_argument("jev: response usage must be an object");
            }
            usage_value = &leaf;
            usage_seen = true;
        } else {
            collector.add(leaf, key, key, ir::LOSS_UNMAPPED_FIELD,
                          "unknown property on the Jev response");
        }
    }
    if (!model_seen || model_value == nullptr || model_value->as_string().empty()) {
        return invalid_argument("jev: response model is required");
    }
    if (!answers_seen) {
        return invalid_argument("jev: response answers must not be empty");
    }
    if (!usage_seen) {
        return invalid_argument("jev: response usage must be an object");
    }

    // Audit the answers map in encounter order: recognized fields are
    // silent, box types report N-JEV-5, unknown properties report one
    // unmapped-field loss each (spec/02 §4).
    for (const auto& [id, answer] : answers_value->as_object()) {
        if (!answer.is_object()) {
            return invalid_argument("jev: answer " + id + " must be an object");
        }
        for (const auto& [field, leaf] : answer.as_object()) {
            const bool recognized =
                field == "type" || field == "noul" || field == "choice" || field == "score" ||
                field == "legend" || field == "probabilities" || field == "confidence" ||
                field == "box";
            if (!recognized) {
                collector.add(leaf, "answers." + id, field, ir::LOSS_UNMAPPED_FIELD,
                              "unknown property on the Jev answer; dropped");
            } else if (field == "type" && leaf.is_string() &&
                       leaf.as_string() == QUESTION_TYPE_BOX) {
                collector.add(leaf, "answers." + id, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                              "N-JEV-5: experimental box answers are carried verbatim");
            }
        }
    }
    const json::Value* input_tokens = usage_value->find("input_tokens");
    const json::Value* output_tokens = usage_value->find("output_tokens");
    for (const auto& [field, leaf] : usage_value->as_object()) {
        if (field != "input_tokens" && field != "output_tokens") {
            collector.add(leaf, "usage", field, ir::LOSS_UNMAPPED_FIELD,
                          "unknown property on the Jev usage");
        }
    }
    // Missing usage tokens default to 0; present ones must be integers.
    std::int64_t usage_input = 0;
    std::int64_t usage_output = 0;
    if (input_tokens != nullptr) {
        if (!input_tokens->is_int()) {
            return invalid_argument("jev: usage input_tokens must be an integer");
        }
        usage_input = input_tokens->as_int();
    }
    if (output_tokens != nullptr) {
        if (!output_tokens->is_int()) {
            return invalid_argument("jev: usage output_tokens must be an integer");
        }
        usage_output = output_tokens->as_int();
    }

    // §7.5 canon: answer keys and each answer's recognized fields sort
    // ascending, while field values carry verbatim.
    json::Value filtered = json::Value::object();
    for (const auto& [id, answer] : answers_value->as_object()) {
        json::Value fields = json::Value::object();
        for (const auto& [field, leaf] : answer.as_object()) {
            const bool recognized =
                field == "type" || field == "noul" || field == "choice" || field == "score" ||
                field == "legend" || field == "probabilities" || field == "confidence" ||
                field == "box";
            if (recognized) fields.set(field, leaf);
        }
        filtered.set(id, std::move(fields));
    }
    const std::string canon_text = json::serialize(deep_raw(filtered));

    ir::Response response;
    response.id = std::string(SYNTHESIZED_ID);
    response.model = opts.model_map.map(model_value->as_string());
    response.content.push_back(ir::TextBlock(canon_text));
    response.stop_reason = std::string(ir::STOP_END_TURN);
    response.usage = ir::Usage(usage_input, usage_output);
    return Conversion<ir::Response>{std::move(response), std::move(collector).sorted()};
}

StatusOr<Conversion<ir::Response>> decode_response(std::string_view wire_json,
                                                   const Options& opts) {
    OXA_ASSIGN_OR_RETURN(json::Value wire, json::parse_lossless(wire_json));
    return decode_response(wire, opts);
}

StatusOr<Conversion<json::Value>> encode_response(const ir::Response& resp, const Options& opts) {
    LossCollector collector;
    std::string aggregated;
    for (std::size_t index = 0; index < resp.content.size(); ++index) {
        if (const auto* text = std::get_if<ir::TextBlock>(&resp.content[index])) {
            aggregated += text->text;
        } else {
            collector.add(json::Value{}, "content[" + std::to_string(index) + "]", "type",
                          ir::LOSS_UNSUPPORTED_SEMANTIC,
                          "N-JEV-15: non-text blocks are excluded from answers");
        }
    }
    if (aggregated.empty()) {
        return invalid_argument("jev: response has no text content");
    }
    OXA_ASSIGN_OR_RETURN(json::Value parsed, json::parse_lossless(aggregated));
    if (!parsed.is_object() || parsed.as_object().empty()) {
        return invalid_argument("jev: aggregated response text is not a non-empty JSON object");
    }

    QuestionMap empty_context;
    const QuestionMap& context = opts.questions.has_value() ? *opts.questions : empty_context;
    for (const auto& [id, question] : context) {
        if (!is_supported_question_type(question.type)) continue;
        if (parsed.find(id) == nullptr) {
            return invalid_argument("jev: answer " + id + " is missing from the response");
        }
    }

    json::Value recovered = json::Value::object();
    for (const auto& [id, answer] : parsed.as_object()) {
        if (!context.empty() && context.find(id) == context.end()) {
            collector.add(json::Value{}, "answers", id, ir::LOSS_UNMAPPED_FIELD,
                          "answer beyond the requested questions");
        }
        auto it = context.find(id);
        const Question* question = it != context.end() ? &it->second : nullptr;
        OXA_ASSIGN_OR_RETURN(auto recovered_answer,
                             answer.is_object() ? recover_object(id, answer, question)
                                                : recover_scalar(id, answer, question));
        auto& [wire_answer, loss] = recovered_answer;
        if (loss.has_value()) {
            collector.add(json::Value{}, std::move(*loss));
        }
        recovered.set(id, std::move(wire_answer));
    }

    if (resp.stop_reason != ir::STOP_END_TURN && resp.stop_reason != "other") {
        collector.add(json::Value{}, "stop_reason", "stop_reason", ir::LOSS_UNMAPPED_FIELD,
                      "N-JEV-16: Jev responses carry no stop reason");
    }
    const auto usage_detail = [&collector](const char* field) {
        collector.add(json::Value{}, "usage", field, ir::LOSS_UNMAPPED_FIELD,
                      "N-JEV-17: usage granularity has no Jev equivalent");
    };
    if (resp.usage.cache_read_input_tokens.has_value()) usage_detail("cache_read_input_tokens");
    if (resp.usage.cache_creation_input_tokens.has_value()) {
        usage_detail("cache_creation_input_tokens");
    }
    if (resp.usage.input_tokens_details.has_value()) usage_detail("input_tokens_details");
    if (resp.usage.output_tokens_details.has_value()) usage_detail("output_tokens_details");

    json::Value usage = json::Value::object();
    usage.set("input_tokens", json::Value::integer(resp.usage.input_tokens));
    usage.set("output_tokens", json::Value::integer(resp.usage.output_tokens));
    json::Value wire = json::Value::object();
    wire.set("model", json::Value::string(opts.model_map.map(resp.model)));
    wire.set("answers", std::move(recovered));
    wire.set("usage", std::move(usage));
    return Conversion<json::Value>{std::move(wire), std::move(collector).sorted()};
}

StatusOr<Conversion<std::string>> encode_response_json(const ir::Response& resp,
                                                       const Options& opts) {
    OXA_ASSIGN_OR_RETURN(Conversion<json::Value> converted, encode_response(resp, opts));
    return Conversion<std::string>{json::serialize(converted.value), std::move(converted.losses)};
}

}  // namespace oxa::systemone::jev
