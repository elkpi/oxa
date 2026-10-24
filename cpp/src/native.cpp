#include <set>
#include <string>
#include <vector>

#include "oxa/ir.hpp"

namespace oxa::ir {

namespace {

enum class HostType {
    Request,
    System,
    Content,
    Tool,
};

Status validate_annotations(const std::vector<NativeAnnotation>& annotations,
                            HostType host,
                            std::string_view path) {
    std::set<std::string> seen;
    for (std::size_t i = 0; i < annotations.size(); ++i) {
        const auto& ann = annotations[i];
        if (ann.source.empty()) {
            return invalid_argument(std::string(path) + ".native[" + std::to_string(i) + "]: source cannot be empty");
        }
        if (ann.kind.empty()) {
            return invalid_argument(std::string(path) + ".native[" + std::to_string(i) + "]: kind cannot be empty");
        }
        auto parsed = json::parse(ann.value);
        if (!parsed.ok()) {
            return invalid_argument(std::string(path) + ".native[" + std::to_string(i) + "]: value is not valid JSON text");
        }
        if (ann.omission.has_value() && ann.omission->field.empty()) {
            return invalid_argument(std::string(path) + ".native[" + std::to_string(i) + "]: omission field cannot be empty");
        }
        std::string key = ann.source + ":" + ann.kind;
        if (!seen.insert(key).second) {
            return invalid_argument(std::string(path) + ": duplicate native annotation for " + key);
        }

        if (ann.source == NATIVE_SOURCE_ANTHROPIC) {
            if (ann.kind == NATIVE_KIND_THINKING_MODE) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on Request");
                }
                std::string s = parsed->is_string() ? parsed->as_string() : "";
                if (s != "adaptive" && s != "enabled" && s != "disabled") {
                    return invalid_argument("invalid thinking_mode \"" + s + "\"");
                }
            } else if (ann.kind == NATIVE_KIND_THINKING_BUDGET) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on Request");
                }
                if (!parsed->is_int() || parsed->as_int() <= 0) {
                    return invalid_argument("anthropic thinking_budget must be a positive integer");
                }
            } else if (ann.kind == NATIVE_KIND_THINKING_DISPLAY) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on Request");
                }
                std::string s = parsed->is_string() ? parsed->as_string() : "";
                if (s != "summarized" && s != "omitted" && s != "updates") {
                    return invalid_argument("invalid thinking_display \"" + s + "\"");
                }
            } else if (ann.kind == NATIVE_KIND_OUTPUT_EFFORT) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on Request");
                }
                std::string s = parsed->is_string() ? parsed->as_string() : "";
                if (s != "low" && s != "medium" && s != "high" && s != "xhigh" && s != "max") {
                    return invalid_argument("invalid output_effort \"" + s + "\"");
                }
            } else if (ann.kind == NATIVE_KIND_CONTEXT_MANAGEMENT) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on Request");
                }
                if (!parsed->is_object() || parsed->find("edits") == nullptr || !parsed->find("edits")->is_array()) {
                    return invalid_argument("context_management requires edits array");
                }
            } else if (ann.kind == NATIVE_KIND_CACHE_CONTROL) {
                if (!parsed->is_object()) {
                    return invalid_argument("invalid cache_control object");
                }
                const auto* t = parsed->find("type");
                if (t == nullptr || !t->is_string() || t->as_string() != "ephemeral") {
                    return invalid_argument("invalid cache_control object");
                }
                if (const auto* ttl = parsed->find("ttl")) {
                    std::string ttl_str = ttl->is_string() ? ttl->as_string() : "";
                    if (ttl_str != "5m" && ttl_str != "1h") {
                        return invalid_argument("invalid cache_control ttl \"" + ttl_str + "\"");
                    }
                }
            } else if (ann.kind == NATIVE_KIND_DEFER_LOADING) {
                if (host != HostType::Tool) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on Tool");
                }
                if (!parsed->is_bool() || !parsed->as_bool()) {
                    return invalid_argument("invalid defer_loading " + ann.value);
                }
            } else if (ann.kind == NATIVE_KIND_EAGER_INPUT_STREAMING) {
                if (host != HostType::Tool) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on Tool");
                }
                if (!parsed->is_bool()) {
                    return invalid_argument("invalid eager_input_streaming " + ann.value);
                }
            } else if (ann.kind == NATIVE_KIND_DOCUMENT_TITLE || ann.kind == NATIVE_KIND_DOCUMENT_CONTEXT) {
                if (host != HostType::Content) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on content blocks");
                }
                if (!parsed->is_string()) {
                    return invalid_argument("anthropic " + ann.kind + " value must be a JSON string");
                }
            } else if (ann.kind == NATIVE_KIND_DOCUMENT_CITATIONS) {
                if (host != HostType::Content) {
                    return invalid_argument(std::string(path) + ": anthropic " + ann.kind + " is only allowed on content blocks");
                }
                if (!parsed->is_object()) {
                    return invalid_argument("anthropic " + ann.kind + " value must be a JSON object");
                }
                if (const auto* enabled = parsed->find("enabled")) {
                    if (!enabled->is_bool()) {
                        return invalid_argument("anthropic " + ann.kind + " enabled must be a boolean");
                    }
                }
            }
        } else if (ann.source == NATIVE_SOURCE_OPENAI) {
            if (ann.kind == NATIVE_KIND_MAX_COMPLETION_TOKENS) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": openai " + ann.kind + " is only allowed on Request");
                }
                if (!parsed->is_bool() || !parsed->as_bool()) {
                    return invalid_argument("invalid max_completion_tokens " + ann.value);
                }
            } else if (ann.kind == NATIVE_KIND_STORE) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": openai " + ann.kind + " is only allowed on Request");
                }
                if (!parsed->is_bool() && !parsed->is_null()) {
                    return invalid_argument("invalid store " + ann.value);
                }
            } else if (ann.kind == NATIVE_KIND_STREAM) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": openai " + ann.kind + " is only allowed on Request");
                }
                if (!parsed->is_bool() && !parsed->is_null()) {
                    return invalid_argument("invalid stream " + ann.value);
                }
            } else if (ann.kind == NATIVE_KIND_STREAM_OPTIONS) {
                if (host != HostType::Request) {
                    return invalid_argument(std::string(path) + ": openai " + ann.kind + " is only allowed on Request");
                }
                if (!parsed->is_null()) {
                    if (!parsed->is_object()) {
                        return invalid_argument("invalid stream_options " + ann.value);
                    }
                    if (const auto* iu = parsed->find("include_usage"); iu && !iu->is_bool()) {
                        return invalid_argument("invalid stream_options " + ann.value + ": include_usage must be a boolean");
                    }
                    if (const auto* io = parsed->find("include_obfuscation"); io && !io->is_bool()) {
                        return invalid_argument("invalid stream_options " + ann.value + ": include_obfuscation must be a boolean");
                    }
                }
            } else if (ann.kind == NATIVE_KIND_CONTAINER) {
                if (host != HostType::Tool) {
                    return invalid_argument(std::string(path) + ": openai container is only allowed on Tool");
                }
                if (!parsed->is_object() && !parsed->is_string()) {
                    return invalid_argument(std::string(path) + ": openai container must be an object with path array or string");
                }
            } else if (ann.kind == NATIVE_KIND_DEFER_LOADING) {
                if (host != HostType::Tool) {
                    return invalid_argument(std::string(path) + ": openai defer_loading is only allowed on Tool");
                }
                if (!parsed->is_bool() || !parsed->as_bool()) {
                    return invalid_argument("invalid defer_loading " + ann.value);
                }
            }
        }
    }
    return ok_status();
}

Status validate_block_native(const Block& blk, HostType host, std::string_view path);

struct BlockValidator {
    HostType host;
    std::string_view path;

    Status operator()(const TextBlock& b) const {
        return validate_annotations(b.native, host, path);
    }
    Status operator()(const ThinkingBlock& b) const {
        return validate_annotations(b.native, host, path);
    }
    Status operator()(const ImageBlock& b) const {
        return validate_annotations(b.native, host, path);
    }
    Status operator()(const DocumentBlock& b) const {
        return validate_annotations(b.native, host, path);
    }
    Status operator()(const ToolUseBlock& b) const {
        return validate_annotations(b.native, host, path);
    }
    Status operator()(const CustomToolUseBlock& b) const {
        return validate_annotations(b.native, host, path);
    }
    Status operator()(const ApplyPatchToolUseBlock& b) const {
        return validate_annotations(b.native, host, path);
    }
    Status operator()(const ApplyPatchToolResultBlock& b) const {
        return validate_annotations(b.native, host, path);
    }
    Status operator()(const ToolResultBlock& b) const {
        auto st = validate_annotations(b.native, host, path);
        if (!st.ok()) return st;
        for (std::size_t k = 0; k < b.content.size(); ++k) {
            std::string child_path = std::string(path) + ".content[" + std::to_string(k) + "]";
            auto child_st = validate_block_native(b.content[k].block, host, child_path);
            if (!child_st.ok()) return child_st;
        }
        return ok_status();
    }
    Status operator()(const CustomToolResultBlock& b) const {
        auto st = validate_annotations(b.native, host, path);
        if (!st.ok()) return st;
        if (const auto* vec = std::get_if<std::vector<BlockHolder>>(&b.output)) {
            for (std::size_t k = 0; k < vec->size(); ++k) {
                std::string child_path = std::string(path) + ".output.content[" + std::to_string(k) + "]";
                auto child_st = validate_block_native((*vec)[k].block, host, child_path);
                if (!child_st.ok()) return child_st;
            }
        }
        return ok_status();
    }
};

Status validate_block_native(const Block& blk, HostType host, std::string_view path) {
    return std::visit(BlockValidator{host, path}, blk);
}

void append_omission_loss(std::vector<Loss>& losses,
                          const NativeAnnotation& ann,
                          std::string_view path,
                          bool has_active_budget_proj,
                          const std::optional<std::string>& current_effort,
                          std::string_view target) {
    if (ann.source == NATIVE_SOURCE_OPENAI) {
        if (ann.kind == NATIVE_KIND_MAX_COMPLETION_TOKENS || ann.kind == NATIVE_KIND_STREAM) {
            return;
        }
        if (ann.kind == NATIVE_KIND_STREAM_OPTIONS) {
            if (target == "chatcompletions") {
                return;
            }
            auto parsed = json::parse(ann.value);
            bool has_usage_true = false;
            if (parsed.ok() && parsed->is_object()) {
                const auto* iu = parsed->find("include_usage");
                if (iu && iu->is_bool() && iu->as_bool()) {
                    has_usage_true = true;
                }
            }
            if (has_usage_true) {
                std::string field = "stream_options";
                std::string reason = std::string(LOSS_DEGRADED);
                std::string detail = std::string(STREAM_OPTIONS_LOSS_DETAIL);
                if (ann.omission.has_value()) {
                    if (!ann.omission->field.empty()) field = ann.omission->field;
                    if (!ann.omission->reason.empty()) reason = ann.omission->reason;
                    if (ann.omission->detail.has_value() && !ann.omission->detail->empty()) {
                        detail = *ann.omission->detail;
                    }
                }
                losses.push_back(Loss{std::string(path), std::move(field), std::move(reason), std::move(detail)});
            }
            return;
        }
        if (ann.kind == NATIVE_KIND_STORE) {
            if (target == "chatcompletions" || target == "responses") {
                return;
            }
            auto parsed = json::parse(ann.value);
            if (parsed.ok() && parsed->is_bool() && parsed->as_bool()) {
                std::string field = "store";
                std::string reason = std::string(LOSS_UNSUPPORTED_SEMANTIC);
                std::string detail = "target does not support output retention";
                if (ann.omission.has_value()) {
                    if (!ann.omission->field.empty()) field = ann.omission->field;
                    if (!ann.omission->reason.empty()) reason = ann.omission->reason;
                    if (ann.omission->detail.has_value() && !ann.omission->detail->empty()) {
                        detail = *ann.omission->detail;
                    }
                }
                losses.push_back(Loss{std::string(path), std::move(field), std::move(reason), std::move(detail)});
            }
            return;
        }
        if (target == "responses") {
            return;
        }
        if (target == "anthropic" && ann.kind == NATIVE_KIND_DEFER_LOADING) {
            return;
        }
        if (ann.omission.has_value()) {
            losses.push_back(Loss{
                std::string(path),
                ann.omission->field,
                ann.omission->reason,
                ann.omission->detail.value_or(""),
            });
        }
        return;
    }
    if (ann.source != NATIVE_SOURCE_ANTHROPIC) {
        losses.push_back(Loss{
            std::string(path),
            "source",
            std::string(LOSS_UNSUPPORTED_SEMANTIC),
            "unsupported native option source \"" + ann.source + "\"",
        });
        return;
    }

    if (ann.kind == NATIVE_KIND_OUTPUT_EFFORT) {
        return;
    }

    if (ann.kind == NATIVE_KIND_THINKING_MODE) {
        if (has_active_budget_proj) {
            return;
        }
        if (ann.omission.has_value()) {
            losses.push_back(Loss{
                std::string(path),
                ann.omission->field,
                ann.omission->reason,
                ann.omission->detail.value_or(""),
            });
        } else {
            losses.push_back(Loss{
                std::string(path),
                "type",
                std::string(LOSS_UNMAPPED_FIELD),
                "",
            });
        }
        return;
    }

    if (ann.kind == NATIVE_KIND_THINKING_BUDGET) {
        if (ann.projection.has_value() && ann.projection->origin == "budget") {
            if (current_effort.has_value() && ann.projection->effort == *current_effort && ann.omission.has_value()) {
                losses.push_back(Loss{
                    std::string(path),
                    ann.omission->field,
                    ann.omission->reason,
                    ann.omission->detail.value_or(""),
                });
            }
            return;
        }
        if (ann.omission.has_value()) {
            losses.push_back(Loss{
                std::string(path),
                ann.omission->field,
                ann.omission->reason,
                ann.omission->detail.value_or(""),
            });
        }
        return;
    }

    if (ann.kind == NATIVE_KIND_CACHE_CONTROL) {
        std::string field = "cache_control";
        std::string reason = std::string(LOSS_UNMAPPED_FIELD);
        std::string detail = std::string(CACHE_CONTROL_LOSS_DETAIL);
        if (ann.omission.has_value()) {
            if (!ann.omission->field.empty()) field = ann.omission->field;
            if (!ann.omission->reason.empty()) reason = ann.omission->reason;
            if (ann.omission->detail.has_value() && !ann.omission->detail->empty()) {
                detail = *ann.omission->detail;
            }
        }
        losses.push_back(Loss{
            std::string(path),
            std::move(field),
            std::move(reason),
            std::move(detail),
        });
        return;
    }

    if (ann.kind == NATIVE_KIND_DEFER_LOADING) {
        if (target == "responses" || target == "anthropic") {
            return;
        }
        std::string field = "defer_loading";
        std::string reason = std::string(LOSS_DEGRADED);
        std::string detail = std::string(DEFER_LOADING_LOSS_DETAIL);
        if (ann.omission.has_value()) {
            if (!ann.omission->field.empty()) field = ann.omission->field;
            if (!ann.omission->reason.empty()) reason = ann.omission->reason;
            if (ann.omission->detail.has_value() && !ann.omission->detail->empty()) {
                detail = *ann.omission->detail;
            }
        }
        losses.push_back(Loss{
            std::string(path),
            std::move(field),
            std::move(reason),
            std::move(detail),
        });
        return;
    }

    if (ann.kind == NATIVE_KIND_EAGER_INPUT_STREAMING) {
        if (target == "anthropic") {
            return;
        }
        auto parsed = json::parse(ann.value);
        if (parsed.ok() && parsed->is_bool() && parsed->as_bool()) {
            std::string field = "eager_input_streaming";
            std::string reason = std::string(LOSS_DEGRADED);
            std::string detail = std::string(EAGER_INPUT_STREAMING_LOSS_DETAIL);
            if (ann.omission.has_value()) {
                if (!ann.omission->field.empty()) field = ann.omission->field;
                if (!ann.omission->reason.empty()) reason = ann.omission->reason;
                if (ann.omission->detail.has_value() && !ann.omission->detail->empty()) {
                    detail = *ann.omission->detail;
                }
            }
            losses.push_back(Loss{
                std::string(path),
                std::move(field),
                std::move(reason),
                std::move(detail),
            });
        }
        return;
    }

    if (ann.kind == NATIVE_KIND_THINKING_DISPLAY ||
        ann.kind == NATIVE_KIND_CONTEXT_MANAGEMENT) {
        if (ann.omission.has_value()) {
            losses.push_back(Loss{
                std::string(path),
                ann.omission->field,
                ann.omission->reason,
                ann.omission->detail.value_or(""),
            });
        }
        return;
    }

    losses.push_back(Loss{
        std::string(path),
        "kind",
        std::string(LOSS_UNSUPPORTED_SEMANTIC),
        "unsupported native option kind \"" + ann.kind + "\"",
    });
}

void collect_block_omissions(std::vector<Loss>& losses,
                             const Block& blk,
                             std::string_view path,
                             std::string_view target);

struct BlockOmissionCollector {
    std::vector<Loss>& losses;
    std::string_view path;
    std::string_view target;

    void operator()(const TextBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
    }
    void operator()(const ThinkingBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
    }
    void operator()(const ImageBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
    }
    void operator()(const DocumentBlock& /*b*/) const {
        // N-AN-16: a document block crossing to a face without document
        // support is dropped as one whole-block loss; its annotations are
        // part of it and are not reported a second time.
    }
    void operator()(const ToolUseBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
    }
    void operator()(const CustomToolUseBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
    }
    void operator()(const ApplyPatchToolUseBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
    }
    void operator()(const ApplyPatchToolResultBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
    }
    void operator()(const ToolResultBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
        for (std::size_t k = 0; k < b.content.size(); ++k) {
            collect_block_omissions(losses, b.content[k].block, std::string(path) + ".content[" + std::to_string(k) + "]", target);
        }
    }
    void operator()(const CustomToolResultBlock& b) const {
        for (std::size_t i = 0; i < b.native.size(); ++i) {
            append_omission_loss(losses, b.native[i], std::string(path) + ".native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
        if (const auto* vec = std::get_if<std::vector<BlockHolder>>(&b.output)) {
            for (std::size_t k = 0; k < vec->size(); ++k) {
                collect_block_omissions(losses, (*vec)[k].block, std::string(path) + ".output.content[" + std::to_string(k) + "]", target);
            }
        }
    }
};

void collect_block_omissions(std::vector<Loss>& losses,
                             const Block& blk,
                             std::string_view path,
                             std::string_view target) {
    std::visit(BlockOmissionCollector{losses, path, target}, blk);
}

}  // namespace

json::Value dump_native_annotation(const NativeAnnotation& ann) {
    auto obj = json::Value::object();
    obj.set("source", json::Value::string(ann.source));
    obj.set("kind", json::Value::string(ann.kind));
    obj.set("value", json::Value::string(ann.value));
    if (ann.omission.has_value()) {
        auto om = json::Value::object();
        om.set("field", json::Value::string(ann.omission->field));
        om.set("reason", json::Value::string(ann.omission->reason));
        if (ann.omission->detail.has_value()) {
            om.set("detail", json::Value::string(*ann.omission->detail));
        }
        obj.set("omission", std::move(om));
    }
    if (ann.projection.has_value()) {
        auto proj = json::Value::object();
        proj.set("origin", json::Value::string(ann.projection->origin));
        proj.set("effort", json::Value::string(ann.projection->effort));
        obj.set("projection", std::move(proj));
    }
    return obj;
}

StatusOr<NativeAnnotation> load_native_annotation(const json::Value& v) {
    if (!v.is_object()) {
        return invalid_argument("native annotation must be an object");
    }
    const auto* source = v.find("source");
    const auto* kind = v.find("kind");
    const auto* value = v.find("value");
    if (!source || !source->is_string() || !kind || !kind->is_string() || !value || !value->is_string()) {
        return invalid_argument("native annotation requires string source, kind, and value");
    }

    NativeAnnotation ann;
    ann.source = source->as_string();
    ann.kind = kind->as_string();
    ann.value = value->as_string();

    if (const auto* om = v.find("omission")) {
        if (!om->is_object()) {
            return invalid_argument("native omission must be an object");
        }
        const auto* field = om->find("field");
        const auto* reason = om->find("reason");
        if (!field || !field->is_string() || !reason || !reason->is_string()) {
            return invalid_argument("native omission requires string field and reason");
        }
        NativeOmission nom;
        nom.field = field->as_string();
        nom.reason = reason->as_string();
        if (const auto* detail = om->find("detail")) {
            if (!detail->is_string()) {
                return invalid_argument("native omission detail must be a string");
            }
            nom.detail = detail->as_string();
        }
        ann.omission = nom;
    }

    if (const auto* proj = v.find("projection")) {
        if (!proj->is_object()) {
            return invalid_argument("native projection must be an object");
        }
        const auto* origin = proj->find("origin");
        const auto* effort = proj->find("effort");
        if (!origin || !origin->is_string() || !effort || !effort->is_string()) {
            return invalid_argument("native projection requires string origin and effort");
        }
        if (origin->as_string() != "budget" && origin->as_string() != "explicit") {
            return invalid_argument("native projection origin must be 'budget' or 'explicit'");
        }
        std::string eff = effort->as_string();
        if (eff != REASONING_EFFORT_MINIMAL && eff != REASONING_EFFORT_LOW &&
            eff != REASONING_EFFORT_MEDIUM && eff != REASONING_EFFORT_HIGH &&
            eff != REASONING_EFFORT_XHIGH && eff != REASONING_EFFORT_MAX) {
            return invalid_argument("native projection effort is invalid");
        }
        ann.projection = NativeProjection{origin->as_string(), eff};
    }

    return ann;
}

Status validate_native_request(const Request& req) {
    auto st = validate_annotations(req.native, HostType::Request, "request");
    if (!st.ok()) return st;

    int budget_proj = 0;
    int explicit_proj = 0;
    for (const auto& n : req.native) {
        if (n.projection.has_value()) {
            if (n.projection->origin != "budget" && n.projection->origin != "explicit") {
                return invalid_argument("ir: native projection origin \"" + n.projection->origin + "\" invalid");
            }
            if (n.projection->origin == "budget") {
                if (n.kind != NATIVE_KIND_THINKING_BUDGET) {
                    return invalid_argument("ir: budget projection is only allowed on thinking_budget");
                }
                budget_proj++;
                if (budget_proj > 1) {
                    return invalid_argument("ir: duplicate budget projection on request");
                }
            } else if (n.projection->origin == "explicit") {
                if (n.kind != NATIVE_KIND_OUTPUT_EFFORT) {
                    return invalid_argument("ir: explicit projection is only allowed on output_effort");
                }
                explicit_proj++;
                if (explicit_proj > 1) {
                    return invalid_argument("ir: duplicate explicit projection on request");
                }
            }
        }
    }

    if (budget_proj > 0 && explicit_proj > 0) {
        return invalid_argument("ir: conflicting effort projections on request");
    }

    for (std::size_t i = 0; i < req.system.size(); ++i) {
        auto sys_st = validate_annotations(req.system[i].native, HostType::System, "system[" + std::to_string(i) + "]");
        if (!sys_st.ok()) return sys_st;
    }

    for (std::size_t m_idx = 0; m_idx < req.messages.size(); ++m_idx) {
        const auto& msg = req.messages[m_idx];
        for (std::size_t c_idx = 0; c_idx < msg.content.size(); ++c_idx) {
            std::string content_path = "messages[" + std::to_string(m_idx) + "].content[" + std::to_string(c_idx) + "]";
            auto blk_st = validate_block_native(msg.content[c_idx], HostType::Content, content_path);
            if (!blk_st.ok()) return blk_st;
        }
    }

    if (req.tools.has_value()) {
        for (std::size_t t_idx = 0; t_idx < req.tools->size(); ++t_idx) {
            std::string tool_path = "tools[" + std::to_string(t_idx) + "]";
            auto tool_st = validate_annotations((*req.tools)[t_idx].native, HostType::Tool, tool_path);
            if (!tool_st.ok()) return tool_st;
            bool has_cache_control = false;
            bool has_defer_loading = false;
            for (const auto& n : (*req.tools)[t_idx].native) {
                if (n.source == NATIVE_SOURCE_ANTHROPIC) {
                    if (n.kind == NATIVE_KIND_CACHE_CONTROL) {
                        has_cache_control = true;
                    } else if (n.kind == NATIVE_KIND_DEFER_LOADING) {
                        has_defer_loading = true;
                    }
                }
            }
            if (has_cache_control && has_defer_loading) {
                return invalid_argument("ir: " + tool_path + ": cannot specify both defer_loading and cache_control");
            }
        }
    }

    return ok_status();
}

StatusOr<std::vector<Loss>> omitted_native_request_losses(const Request& req, std::string_view target) {
    auto st = validate_native_request(req);
    if (!st.ok()) return st;

    std::vector<Loss> losses;
    std::optional<std::string> current_effort = req.params.has_value() ? req.params->reasoning_effort : std::nullopt;

    bool has_active_budget_proj = false;
    for (const auto& n : req.native) {
        if (n.source == NATIVE_SOURCE_ANTHROPIC && n.kind == NATIVE_KIND_THINKING_BUDGET &&
            n.projection.has_value() && n.projection->origin == "budget" &&
            current_effort.has_value() && n.projection->effort == *current_effort) {
            has_active_budget_proj = true;
            break;
        }
    }

    for (std::size_t i = 0; i < req.native.size(); ++i) {
        append_omission_loss(losses, req.native[i], "native[" + std::to_string(i) + "]", has_active_budget_proj, current_effort, target);
    }

    for (std::size_t s_idx = 0; s_idx < req.system.size(); ++s_idx) {
        for (std::size_t i = 0; i < req.system[s_idx].native.size(); ++i) {
            append_omission_loss(losses, req.system[s_idx].native[i], "system[" + std::to_string(s_idx) + "].native[" + std::to_string(i) + "]", false, std::nullopt, target);
        }
    }

    for (std::size_t m_idx = 0; m_idx < req.messages.size(); ++m_idx) {
        const auto& msg = req.messages[m_idx];
        for (std::size_t c_idx = 0; c_idx < msg.content.size(); ++c_idx) {
            std::string content_path = "messages[" + std::to_string(m_idx) + "].content[" + std::to_string(c_idx) + "]";
            collect_block_omissions(losses, msg.content[c_idx], content_path, target);
        }
    }

    if (req.tools.has_value()) {
        for (std::size_t t_idx = 0; t_idx < req.tools->size(); ++t_idx) {
            for (std::size_t i = 0; i < (*req.tools)[t_idx].native.size(); ++i) {
                append_omission_loss(losses, (*req.tools)[t_idx].native[i], "tools[" + std::to_string(t_idx) + "].native[" + std::to_string(i) + "]", false, std::nullopt, target);
            }
        }
    }

    return losses;
}

}  // namespace oxa::ir
