#pragma once

#include <optional>
#include <string>
#include <string_view>
#include <vector>

#include "oxa/ir.hpp"
#include "oxa/json.hpp"

namespace oxa::anthropic::messages {

inline constexpr std::string_view CACHE_CONTROL_LOSS_DETAIL = ir::CACHE_CONTROL_LOSS_DETAIL;

inline std::optional<std::string> parse_cache_control(
    const json::Value& cc,
    std::string_view path,
    std::vector<ir::Loss>& losses) {
    if (!cc.is_object()) {
        return std::nullopt;
    }
    const auto* type_val = cc.find("type");
    std::string type_str = (type_val != nullptr && type_val->is_string()) ? type_val->as_string() : "";
    if (type_str != "ephemeral") {
        losses.push_back(ir::Loss{
            std::string(path) + ".type",
            "type",
            std::string(ir::LOSS_UNMAPPED_VALUE),
            "unknown cache_control.type \"" + type_str + "\"",
        });
        return std::nullopt;
    }

    const auto* ttl_val = cc.find("ttl");
    if (ttl_val != nullptr && !ttl_val->is_null()) {
        std::string ttl_str = ttl_val->is_string() ? ttl_val->as_string() : "";
        if (ttl_str != "5m" && ttl_str != "1h") {
            losses.push_back(ir::Loss{
                std::string(path) + ".ttl",
                "ttl",
                std::string(ir::LOSS_UNMAPPED_VALUE),
                "unknown cache_control.ttl \"" + ttl_str + "\"",
            });
            return std::nullopt;
        }
        return "{\"ttl\":\"" + ttl_str + "\",\"type\":\"ephemeral\"}";
    }

    return "{\"type\":\"ephemeral\"}";
}

inline std::optional<std::string> parse_context_management(
    const json::Value& cm,
    std::string_view path,
    std::vector<ir::Loss>& losses) {
    if (!cm.is_object()) {
        return std::nullopt;
    }
    const auto* edits_val = cm.find("edits");
    if (edits_val == nullptr || !edits_val->is_array()) {
        return std::nullopt;
    }

    std::vector<json::Value> valid_edits;
    for (std::size_t i = 0; i < edits_val->as_array().size(); ++i) {
        const auto& edit = edits_val->as_array()[i];
        if (!edit.is_object()) continue;
        std::string edit_path = std::string(path) + ".edits[" + std::to_string(i) + "]";
        const auto* type_val = edit.find("type");
        std::string type_str = (type_val != nullptr && type_val->is_string()) ? type_val->as_string() : "";

        if (type_str == "clear_thinking_20251015") {
            for (const auto& kv : edit.as_object()) {
                if (kv.first != "type" && kv.first != "keep") {
                    losses.push_back(ir::Loss{
                        edit_path + "." + kv.first,
                        kv.first,
                        std::string(ir::LOSS_UNMAPPED_FIELD),
                        "",
                    });
                }
            }
            bool valid = true;
            auto canon_edit = json::Value::object();
            canon_edit.set("type", json::Value::string("clear_thinking_20251015"));
            if (const auto* keep_val = edit.find("keep")) {
                if (keep_val->is_string()) {
                    if (keep_val->as_string() != "all") {
                        losses.push_back(ir::Loss{
                            edit_path + ".keep",
                            "keep",
                            std::string(ir::LOSS_UNMAPPED_VALUE),
                            "",
                        });
                        valid = false;
                    } else {
                        canon_edit.set("keep", json::Value::string("all"));
                    }
                } else if (keep_val->is_object()) {
                    for (const auto& kv : keep_val->as_object()) {
                        if (kv.first != "type" && kv.first != "value") {
                            losses.push_back(ir::Loss{
                                edit_path + ".keep." + kv.first,
                                kv.first,
                                std::string(ir::LOSS_UNMAPPED_FIELD),
                                "",
                            });
                        }
                    }
                    const auto* k_type = keep_val->find("type");
                    if (k_type == nullptr || !k_type->is_string() || k_type->as_string() != "thinking_turns") {
                        losses.push_back(ir::Loss{
                            edit_path + ".keep",
                            "keep",
                            std::string(ir::LOSS_UNMAPPED_VALUE),
                            "",
                        });
                        valid = false;
                    } else {
                        auto keep_obj = json::Value::object();
                        keep_obj.set("type", json::Value::string("thinking_turns"));
                        if (const auto* v = keep_val->find("value")) {
                            keep_obj.set("value", *v);
                        }
                        canon_edit.set("keep", std::move(keep_obj));
                    }
                } else {
                    losses.push_back(ir::Loss{
                        edit_path + ".keep",
                        "keep",
                        std::string(ir::LOSS_UNMAPPED_VALUE),
                        "",
                    });
                    valid = false;
                }
            }
            if (valid) {
                valid_edits.push_back(std::move(canon_edit));
            }
        } else if (type_str == "clear_tool_uses_20250919") {
            for (const auto& kv : edit.as_object()) {
                if (kv.first != "type" && kv.first != "trigger" && kv.first != "keep" &&
                    kv.first != "clear_at_least" && kv.first != "exclude_tools" &&
                    kv.first != "clear_tool_inputs") {
                    losses.push_back(ir::Loss{
                        edit_path + "." + kv.first,
                        kv.first,
                        std::string(ir::LOSS_UNMAPPED_FIELD),
                        "",
                    });
                }
            }
            bool valid = true;
            auto canon_edit = json::Value::object();
            canon_edit.set("type", json::Value::string("clear_tool_uses_20250919"));

            if (const auto* trig_val = edit.find("trigger")) {
                if (trig_val->is_object()) {
                    for (const auto& kv : trig_val->as_object()) {
                        if (kv.first != "type" && kv.first != "value") {
                            losses.push_back(ir::Loss{
                                edit_path + ".trigger." + kv.first,
                                kv.first,
                                std::string(ir::LOSS_UNMAPPED_FIELD),
                                "",
                            });
                        }
                    }
                    const auto* t_type = trig_val->find("type");
                    if (t_type == nullptr || !t_type->is_string() || t_type->as_string() != "input_tokens") {
                        losses.push_back(ir::Loss{
                            edit_path + ".trigger",
                            "trigger",
                            std::string(ir::LOSS_UNMAPPED_VALUE),
                            "",
                        });
                        valid = false;
                    } else {
                        auto trig_obj = json::Value::object();
                        trig_obj.set("type", json::Value::string("input_tokens"));
                        if (const auto* v = trig_val->find("value")) {
                            trig_obj.set("value", *v);
                        }
                        canon_edit.set("trigger", std::move(trig_obj));
                    }
                } else {
                    losses.push_back(ir::Loss{
                        edit_path + ".trigger",
                        "trigger",
                        std::string(ir::LOSS_UNMAPPED_VALUE),
                        "",
                    });
                    valid = false;
                }
            }

            if (const auto* keep_val = edit.find("keep")) {
                if (keep_val->is_object()) {
                    for (const auto& kv : keep_val->as_object()) {
                        if (kv.first != "type" && kv.first != "value") {
                            losses.push_back(ir::Loss{
                                edit_path + ".keep." + kv.first,
                                kv.first,
                                std::string(ir::LOSS_UNMAPPED_FIELD),
                                "",
                            });
                        }
                    }
                    const auto* k_type = keep_val->find("type");
                    if (k_type == nullptr || !k_type->is_string() || k_type->as_string() != "tool_uses") {
                        losses.push_back(ir::Loss{
                            edit_path + ".keep",
                            "keep",
                            std::string(ir::LOSS_UNMAPPED_VALUE),
                            "",
                        });
                        valid = false;
                    } else {
                        auto keep_obj = json::Value::object();
                        keep_obj.set("type", json::Value::string("tool_uses"));
                        if (const auto* v = keep_val->find("value")) {
                            keep_obj.set("value", *v);
                        }
                        canon_edit.set("keep", std::move(keep_obj));
                    }
                } else {
                    losses.push_back(ir::Loss{
                        edit_path + ".keep",
                        "keep",
                        std::string(ir::LOSS_UNMAPPED_VALUE),
                        "",
                    });
                    valid = false;
                }
            }

            if (const auto* cal_val = edit.find("clear_at_least")) {
                if (cal_val->is_object()) {
                    for (const auto& kv : cal_val->as_object()) {
                        if (kv.first != "type" && kv.first != "value") {
                            losses.push_back(ir::Loss{
                                edit_path + ".clear_at_least." + kv.first,
                                kv.first,
                                std::string(ir::LOSS_UNMAPPED_FIELD),
                                "",
                            });
                        }
                    }
                    const auto* c_type = cal_val->find("type");
                    if (c_type == nullptr || !c_type->is_string() || c_type->as_string() != "input_tokens") {
                        losses.push_back(ir::Loss{
                            edit_path + ".clear_at_least",
                            "clear_at_least",
                            std::string(ir::LOSS_UNMAPPED_VALUE),
                            "",
                        });
                        valid = false;
                    } else {
                        auto cal_obj = json::Value::object();
                        cal_obj.set("type", json::Value::string("input_tokens"));
                        if (const auto* v = cal_val->find("value")) {
                            cal_obj.set("value", *v);
                        }
                        canon_edit.set("clear_at_least", std::move(cal_obj));
                    }
                } else {
                    losses.push_back(ir::Loss{
                        edit_path + ".clear_at_least",
                        "clear_at_least",
                        std::string(ir::LOSS_UNMAPPED_VALUE),
                        "",
                    });
                    valid = false;
                }
            }

            if (const auto* et = edit.find("exclude_tools")) {
                canon_edit.set("exclude_tools", *et);
            }
            if (const auto* cti = edit.find("clear_tool_inputs")) {
                canon_edit.set("clear_tool_inputs", *cti);
            }

            if (valid) {
                valid_edits.push_back(std::move(canon_edit));
            }
        } else {
            losses.push_back(ir::Loss{
                edit_path,
                "type",
                std::string(ir::LOSS_UNSUPPORTED_SEMANTIC),
                "unsupported context edit type \"" + type_str + "\"",
            });
        }
    }

    if (valid_edits.empty()) {
        return std::nullopt;
    }

    auto out_obj = json::Value::object();
    auto edits_arr = json::Value::array();
    for (auto&& e : valid_edits) {
        edits_arr.push_back(std::move(e));
    }
    out_obj.set("edits", std::move(edits_arr));
    return json::serialize(out_obj);
}

inline std::optional<json::Value> get_block_cache_control(
    const std::vector<ir::NativeAnnotation>& native) {
    for (const auto& n : native) {
        if (n.source == ir::NATIVE_SOURCE_ANTHROPIC && n.kind == ir::NATIVE_KIND_CACHE_CONTROL) {
            auto parsed = json::parse(n.value);
            if (parsed.ok()) {
                return std::move(*parsed);
            }
        }
    }
    return std::nullopt;
}

inline bool get_tool_defer_loading(
    const std::vector<ir::NativeAnnotation>& native) {
    for (const auto& n : native) {
        if ((n.source == ir::NATIVE_SOURCE_ANTHROPIC || n.source == "openai") && n.kind == ir::NATIVE_KIND_DEFER_LOADING) {
            auto parsed = json::parse(n.value);
            if (parsed.ok() && parsed->is_bool() && parsed->as_bool()) {
                return true;
            }
        }
    }
    return false;
}

inline std::optional<bool> get_tool_eager_input_streaming(
    const std::vector<ir::NativeAnnotation>& native) {
    for (const auto& n : native) {
        if (n.kind == ir::NATIVE_KIND_EAGER_INPUT_STREAMING) {
            auto parsed = json::parse(n.value);
            if (parsed.ok() && parsed->is_bool()) {
                return parsed->as_bool();
            }
        }
    }
    return std::nullopt;
}

inline void decode_request_native(
    const json::Value& wire,
    ir::Request& req,
    std::vector<ir::Loss>& losses) {
    // 1. Thinking
    if (const auto* thinking = wire.find("thinking"); thinking != nullptr && thinking->is_object()) {
        const auto* type_val = thinking->find("type");
        std::string kind = (type_val != nullptr && type_val->is_string()) ? type_val->as_string() : "";
        if (kind == "adaptive" || kind == "disabled") {
            req.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_ANTHROPIC),
                std::string(ir::NATIVE_KIND_THINKING_MODE),
                "\"" + kind + "\"",
                ir::NativeOmission{"type", std::string(ir::LOSS_UNMAPPED_FIELD), std::nullopt},
                std::nullopt,
            });
        } else if (kind == "enabled") {
            const auto* oc = wire.find("output_config");
            bool has_explicit_effort = (oc != nullptr && oc->is_object() && oc->find("effort") != nullptr && oc->find("effort")->is_string());
            std::optional<ir::NativeOmission> omission = has_explicit_effort
                ? std::optional<ir::NativeOmission>(ir::NativeOmission{"type", std::string(ir::LOSS_UNMAPPED_FIELD), std::nullopt})
                : std::nullopt;
            req.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_ANTHROPIC),
                std::string(ir::NATIVE_KIND_THINKING_MODE),
                "\"enabled\"",
                omission,
                std::nullopt,
            });

            const auto* budget_val = thinking->find("budget_tokens");
            if (budget_val != nullptr && budget_val->is_int() && budget_val->as_int() > 0) {
                std::int64_t budget = budget_val->as_int();
                if (has_explicit_effort) {
                    req.native.push_back(ir::NativeAnnotation{
                        std::string(ir::NATIVE_SOURCE_ANTHROPIC),
                        std::string(ir::NATIVE_KIND_THINKING_BUDGET),
                        std::to_string(budget),
                        ir::NativeOmission{"budget_tokens", std::string(ir::LOSS_UNMAPPED_FIELD), std::nullopt},
                        std::nullopt,
                    });
                } else {
                    std::string derived = (budget <= 2048) ? "low" : ((budget <= 8192) ? "medium" : "high");
                    if (req.params.has_value()) {
                        req.params->reasoning_effort = derived;
                    }
                    req.native.push_back(ir::NativeAnnotation{
                        std::string(ir::NATIVE_SOURCE_ANTHROPIC),
                        std::string(ir::NATIVE_KIND_THINKING_BUDGET),
                        std::to_string(budget),
                        ir::NativeOmission{"budget_tokens", std::string(ir::LOSS_DEGRADED), "budget approximated"},
                        ir::NativeProjection{"budget", derived},
                    });
                }
            }
        } else {
            losses.push_back(ir::Loss{
                "thinking.type",
                "type",
                std::string(ir::LOSS_UNMAPPED_VALUE),
                "unknown thinking.type \"" + kind + "\"",
            });
        }

        const auto* display_val = thinking->find("display");
        if (display_val != nullptr && display_val->is_string()) {
            std::string display = display_val->as_string();
            if (display == "summarized" || display == "omitted" || display == "updates") {
                req.native.push_back(ir::NativeAnnotation{
                    std::string(ir::NATIVE_SOURCE_ANTHROPIC),
                    std::string(ir::NATIVE_KIND_THINKING_DISPLAY),
                    "\"" + display + "\"",
                    ir::NativeOmission{"display", std::string(ir::LOSS_UNMAPPED_FIELD), std::nullopt},
                    std::nullopt,
                });
            } else {
                losses.push_back(ir::Loss{
                    "thinking.display",
                    "display",
                    std::string(ir::LOSS_UNMAPPED_VALUE),
                    "unknown thinking.display \"" + display + "\"",
                });
            }
        }
    }

    // 2. OutputConfig
    if (const auto* oc = wire.find("output_config"); oc != nullptr && oc->is_object()) {
        const auto* effort_val = oc->find("effort");
        if (effort_val != nullptr && effort_val->is_string()) {
            std::string effort = effort_val->as_string();
            if (effort == "low" || effort == "medium" || effort == "high" ||
                effort == "xhigh" || effort == "max") {
                if (req.params.has_value()) {
                    req.params->reasoning_effort = effort;
                }
                req.native.push_back(ir::NativeAnnotation{
                    std::string(ir::NATIVE_SOURCE_ANTHROPIC),
                    std::string(ir::NATIVE_KIND_OUTPUT_EFFORT),
                    "\"" + effort + "\"",
                    std::nullopt,
                    ir::NativeProjection{"explicit", effort},
                });
            } else {
                losses.push_back(ir::Loss{
                    "output_config.effort",
                    "effort",
                    std::string(ir::LOSS_UNMAPPED_VALUE),
                    "unknown output_config.effort \"" + effort + "\"",
                });
            }
        }
    }

    // 3. Top-level cache_control
    if (const auto* cc = wire.find("cache_control")) {
        auto canon = parse_cache_control(*cc, "cache_control", losses);
        if (canon.has_value()) {
            req.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_ANTHROPIC),
                std::string(ir::NATIVE_KIND_CACHE_CONTROL),
                std::move(*canon),
                ir::NativeOmission{"cache_control", std::string(ir::LOSS_UNMAPPED_FIELD), std::string(CACHE_CONTROL_LOSS_DETAIL)},
                std::nullopt,
            });
        }
    }

    // 4. ContextManagement
    if (const auto* cm = wire.find("context_management")) {
        auto canon = parse_context_management(*cm, "context_management", losses);
        if (canon.has_value()) {
            req.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_ANTHROPIC),
                std::string(ir::NATIVE_KIND_CONTEXT_MANAGEMENT),
                std::move(*canon),
                ir::NativeOmission{"context_management", std::string(ir::LOSS_UNSUPPORTED_SEMANTIC), std::nullopt},
                std::nullopt,
            });
        }
    }
}

inline void encode_request_native(
    const ir::Request& req,
    json::Value& wire,
    std::vector<ir::Loss>& losses) {
    std::optional<std::string> native_mode;
    std::optional<std::int64_t> native_budget;
    std::optional<ir::NativeProjection> native_budget_proj;
    std::optional<std::string> native_display;
    std::optional<std::string> native_effort;
    std::optional<ir::NativeProjection> native_effort_proj;

    for (std::size_t i = 0; i < req.native.size(); ++i) {
        const auto& n = req.native[i];
        if (n.source == "openai") {
            if (n.kind == ir::NATIVE_KIND_MAX_COMPLETION_TOKENS || n.kind == ir::NATIVE_KIND_STREAM) {
                continue;
            }
            if (n.kind == ir::NATIVE_KIND_STORE) {
                auto parsed = json::parse(n.value);
                if (parsed.ok() && parsed->is_bool() && parsed->as_bool()) {
                    std::string field = n.omission.has_value() && !n.omission->field.empty() ? n.omission->field : "store";
                    std::string reason = n.omission.has_value() && !n.omission->reason.empty() ? n.omission->reason : std::string(ir::LOSS_UNSUPPORTED_SEMANTIC);
                    std::string detail = n.omission.has_value() && n.omission->detail.has_value() ? *n.omission->detail : "target does not support output retention";
                    losses.push_back(ir::Loss{"native[" + std::to_string(i) + "]", std::move(field), std::move(reason), std::move(detail)});
                }
                continue;
            }
            if (n.kind == ir::NATIVE_KIND_STREAM_OPTIONS) {
                std::string field = n.omission.has_value() && !n.omission->field.empty() ? n.omission->field : "stream_options";
                std::string reason = n.omission.has_value() && !n.omission->reason.empty() ? n.omission->reason : std::string(ir::LOSS_DEGRADED);
                std::string detail = n.omission.has_value() && n.omission->detail.has_value() ? *n.omission->detail : std::string(ir::STREAM_OPTIONS_LOSS_DETAIL);
                losses.push_back(ir::Loss{"native[" + std::to_string(i) + "]", std::move(field), std::move(reason), std::move(detail)});
                continue;
            }
            if (n.omission.has_value()) {
                losses.push_back(ir::Loss{
                    "native[" + std::to_string(i) + "]",
                    n.omission->field,
                    n.omission->reason,
                    n.omission->detail.value_or(""),
                });
                continue;
            }
        } else if (n.source != ir::NATIVE_SOURCE_ANTHROPIC) {
            losses.push_back(ir::Loss{
                "native[" + std::to_string(i) + "]",
                "source",
                std::string(ir::LOSS_UNSUPPORTED_SEMANTIC),
                "unsupported native option source \"" + n.source + "\"",
            });
            continue;
        }
        if (n.kind == ir::NATIVE_KIND_THINKING_MODE) {
            auto parsed = json::parse(n.value);
            if (parsed.ok() && parsed->is_string()) {
                native_mode = parsed->as_string();
            }
        } else if (n.kind == ir::NATIVE_KIND_THINKING_BUDGET) {
            auto parsed = json::parse(n.value);
            if (parsed.ok() && parsed->is_int()) {
                native_budget = parsed->as_int();
                native_budget_proj = n.projection;
            }
        } else if (n.kind == ir::NATIVE_KIND_THINKING_DISPLAY) {
            auto parsed = json::parse(n.value);
            if (parsed.ok() && parsed->is_string()) {
                native_display = parsed->as_string();
            }
        } else if (n.kind == ir::NATIVE_KIND_OUTPUT_EFFORT) {
            auto parsed = json::parse(n.value);
            if (parsed.ok() && parsed->is_string()) {
                native_effort = parsed->as_string();
                native_effort_proj = n.projection;
            }
        } else if (n.kind == ir::NATIVE_KIND_CACHE_CONTROL) {
            auto parsed = json::parse(n.value);
            if (parsed.ok()) {
                wire.set("cache_control", std::move(*parsed));
            }
        } else if (n.kind == ir::NATIVE_KIND_CONTEXT_MANAGEMENT) {
            auto parsed = json::parse(n.value);
            if (parsed.ok()) {
                wire.set("context_management", std::move(*parsed));
            }
        } else {
            losses.push_back(ir::Loss{
                "native[" + std::to_string(i) + "]",
                "kind",
                std::string(ir::LOSS_UNSUPPORTED_SEMANTIC),
                "unsupported native option kind \"" + n.kind + "\"",
            });
        }
    }

    std::optional<std::string> current_effort = req.params.has_value() ? req.params->reasoning_effort : std::nullopt;

    if (native_mode.has_value() || native_effort.has_value() || current_effort.has_value()) {
        if (native_mode == "disabled") {
            auto th = json::Value::object();
            th.set("type", json::Value::string("disabled"));
            wire.set("thinking", std::move(th));
            if ((native_effort.has_value() || native_effort_proj.has_value()) && current_effort.has_value()) {
                auto oc = json::Value::object();
                oc.set("effort", json::Value::string(*current_effort));
                wire.set("output_config", std::move(oc));
            }
        } else if (native_mode == "adaptive") {
            auto th = json::Value::object();
            th.set("type", json::Value::string("adaptive"));
            if (native_display.has_value()) {
                th.set("display", json::Value::string(*native_display));
            }
            wire.set("thinking", std::move(th));
            if (current_effort.has_value()) {
                auto oc = json::Value::object();
                oc.set("effort", json::Value::string(*current_effort));
                wire.set("output_config", std::move(oc));
            }
        } else if (native_mode == "enabled") {
            if (native_budget.has_value()) {
                if (native_budget_proj.has_value() && native_budget_proj->origin == "budget") {
                    if (current_effort.has_value() && native_budget_proj->effort == *current_effort) {
                        auto th = json::Value::object();
                        th.set("type", json::Value::string("enabled"));
                        th.set("budget_tokens", json::Value::integer(*native_budget));
                        if (native_display.has_value()) {
                            th.set("display", json::Value::string(*native_display));
                        }
                        wire.set("thinking", std::move(th));
                    } else if (current_effort.has_value()) {
                        std::int64_t b = 0;
                        if (*current_effort == "minimal") b = 1024;
                        else if (*current_effort == "low") b = 2048;
                        else if (*current_effort == "medium") b = 8192;
                        else if (*current_effort == "high") b = 16384;

                        if (b > 0) {
                            auto th = json::Value::object();
                            th.set("type", json::Value::string("enabled"));
                            th.set("budget_tokens", json::Value::integer(b));
                            if (native_display.has_value()) {
                                th.set("display", json::Value::string(*native_display));
                            }
                            wire.set("thinking", std::move(th));
                            losses.push_back(ir::Loss{
                                "params.reasoning_effort",
                                "reasoning_effort",
                                std::string(ir::LOSS_DEGRADED),
                                "budget approximated",
                            });
                        } else {  // xhigh / max
                            auto th = json::Value::object();
                            th.set("type", json::Value::string("adaptive"));
                            if (native_display.has_value()) {
                                th.set("display", json::Value::string(*native_display));
                            }
                            wire.set("thinking", std::move(th));
                            auto oc = json::Value::object();
                            oc.set("effort", json::Value::string(*current_effort));
                            wire.set("output_config", std::move(oc));
                            losses.push_back(ir::Loss{
                                "params.reasoning_effort",
                                "reasoning_effort",
                                std::string(ir::LOSS_DEGRADED),
                                "switched to adaptive thinking mode for extended effort",
                            });
                        }
                    } else {
                        losses.push_back(ir::Loss{
                            "thinking",
                            "thinking",
                            std::string(ir::LOSS_UNSUPPORTED_SEMANTIC),
                            "budget projection invalidated when effort is cleared",
                        });
                    }
                } else {
                    auto th = json::Value::object();
                    th.set("type", json::Value::string("enabled"));
                    th.set("budget_tokens", json::Value::integer(*native_budget));
                    if (native_display.has_value()) {
                        th.set("display", json::Value::string(*native_display));
                    }
                    wire.set("thinking", std::move(th));
                    if (current_effort.has_value()) {
                        auto oc = json::Value::object();
                        oc.set("effort", json::Value::string(*current_effort));
                        wire.set("output_config", std::move(oc));
                    }
                }
            }
        } else if (native_effort.has_value() || native_effort_proj.has_value()) {
            if (current_effort.has_value()) {
                auto oc = json::Value::object();
                oc.set("effort", json::Value::string(*current_effort));
                wire.set("output_config", std::move(oc));
            }
        } else if (current_effort.has_value()) {
            std::int64_t b = 0;
            if (*current_effort == "minimal") b = 1024;
            else if (*current_effort == "low") b = 2048;
            else if (*current_effort == "medium") b = 8192;
            else if (*current_effort == "high") b = 16384;

            if (b > 0) {
                auto th = json::Value::object();
                th.set("type", json::Value::string("enabled"));
                th.set("budget_tokens", json::Value::integer(b));
                wire.set("thinking", std::move(th));
                losses.push_back(ir::Loss{
                    "params.reasoning_effort",
                    "reasoning_effort",
                    std::string(ir::LOSS_DEGRADED),
                    "budget approximated",
                });
            } else {
                auto th = json::Value::object();
                th.set("type", json::Value::string("adaptive"));
                wire.set("thinking", std::move(th));
                auto oc = json::Value::object();
                oc.set("effort", json::Value::string(*current_effort));
                wire.set("output_config", std::move(oc));
            }
        }
    }
}

}  // namespace oxa::anthropic::messages
