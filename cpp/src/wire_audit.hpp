#pragma once

#include <algorithm>
#include <limits>
#include <map>
#include <set>
#include <string>
#include <string_view>
#include <tuple>
#include <vector>
#include "oxa/json.hpp"
#include "oxa/ir.hpp"

namespace oxa::detail {
using AuditFields = std::map<std::string_view, std::string_view>;
struct AuditVariant {
    std::string_view tag;
    std::map<std::string_view, std::string_view> cases;
    std::string_view default_shape;
};
struct AuditProfile {
    std::map<std::string_view, AuditFields> fields;
    std::map<std::string_view, AuditVariant> variants;
};

inline std::string audit_join(std::string_view path, std::string_view key) {
    return path.empty() ? std::string(key) : std::string(path) + "." + std::string(key);
}

inline std::vector<ir::Loss> audit_wire(const json::Value& root, std::string_view kind,
                                      const AuditProfile& profile, std::vector<ir::Loss> existing) {
    std::map<std::string, std::size_t> positions;
    std::vector<ir::Loss> extra;
    std::size_t fallback = 0;
    const auto position = [&](const json::Value& node) {
        return node.source() ? std::size_t(node.span_begin()) : fallback++;
    };
    const auto walk = [&](const auto& self, const json::Value& node, std::string_view name,
                          const std::string& path) -> void {
        positions[path] = position(node);
        if (name.empty()) return;
        if (name.ends_with("[]")) {
            if (!node.is_array()) return;
            for (std::size_t index = 0; index < node.as_array().size(); ++index) {
                self(self, node.as_array()[index], name.substr(0, name.size() - 2),
                     path + "[" + std::to_string(index) + "]");
            }
            return;
        }
        if (!node.is_object()) return;
        if (auto variant = profile.variants.find(name); variant != profile.variants.end()) {
            const auto* tag = node.find(variant->second.tag);
            std::string_view next = variant->second.default_shape;
            if (tag != nullptr) {
                if (!tag->is_string()) return;
                auto entry = variant->second.cases.find(tag->as_string());
                if (entry == variant->second.cases.end()) return;
                next = entry->second;
            }
            if (!next.empty()) self(self, node, next, path);
            return;
        }
        if (name == "tool") {
            const auto* type_val = node.find("type");
            if (type_val != nullptr && type_val->is_string() && !type_val->as_string().empty() &&
                type_val->as_string() != "function" && type_val->as_string() != "custom") {
                for (const auto& [key, value] : node.as_object()) {
                    positions[audit_join(path, key)] = position(value);
                }
                return;
            }
        }
        auto fields = profile.fields.find(name);
        if (fields == profile.fields.end()) return;
        std::vector<std::pair<const std::string*, const json::Value*>> children;
        for (const auto& [key, value] : node.as_object()) children.emplace_back(&key, &value);
        if (node.source() && std::all_of(children.begin(), children.end(), [&](const auto& child) {
            return child.second->source() == node.source();
        })) {
            std::stable_sort(children.begin(), children.end(), [](const auto& left, const auto& right) {
                return left.second->span_begin() < right.second->span_begin();
            });
        }
        for (const auto& [key, value] : children) {
            const auto child_path = audit_join(path, *key);
            positions[child_path] = position(*value);
            auto found = fields->second.find(*key);
            if (found == fields->second.end()) {
                extra.push_back(ir::make_loss(child_path, *key, ir::LOSS_UNMAPPED_FIELD, ""));
            } else {
                self(self, *value, found->second, child_path);
            }
        }
    };
    walk(walk, root, kind, "");
    const auto source_path = [&](std::string path) {
        if (positions.contains(path)) return path;
        if (positions.contains(audit_join("response", path))) return audit_join("response", path);
        if (positions.contains(audit_join("delta", path))) return audit_join("delta", path);
        if (path.starts_with("output[")) {
            auto close = path.find(']');
            if (close != std::string::npos) {
                auto tail = path.substr(close + 1);
                if (positions.contains("item" + tail)) return "item" + tail;
                if (tail.starts_with(".content[")) {
                    auto part_close = tail.find(']');
                    if (part_close != std::string::npos && positions.contains("part" + tail.substr(part_close + 1)))
                        return "part" + tail.substr(part_close + 1);
                }
            }
        }
        auto block = path.find(".content_block");
        if (block != std::string::npos && positions.contains(path.substr(block + 1))) return path.substr(block + 1);
        return path;
    };
    std::set<std::tuple<std::string, std::string, std::string>> seen;
    for (const auto& loss : existing) seen.emplace(loss.path, loss.field, loss.reason);
    for (auto& loss : extra) {
        if (!seen.contains({loss.path, loss.field, loss.reason})) existing.push_back(std::move(loss));
    }
    const auto order = [&](const ir::Loss& loss) {
        const auto path = source_path(loss.path);
        auto field = positions.find(audit_join(path, loss.field));
        if (loss.reason != ir::LOSS_UNSUPPORTED_SEMANTIC && field != positions.end()) return field->second;
        auto found = positions.find(path);
        return found == positions.end() ? std::numeric_limits<std::size_t>::max() : found->second;
    };
    std::stable_sort(existing.begin(), existing.end(), [&](const auto& left, const auto& right) {
        return order(left) < order(right);
    });
    return existing;
}
}
