#include "oxa/openai/responses.hpp"
#include "responses_audit.hpp"

#include <cstdio>
#include <unordered_set>

namespace oxa::openai::responses {
namespace {

ir::Loss make_resp_loss(std::string path, std::string field, std::string_view reason,
                        std::string detail) {
    return ir::make_loss(std::move(path), std::move(field), reason, std::move(detail));
}

StatusOr<std::optional<ir::ResponseFormat>> decode_text_format(
    const json::Value& value, std::vector<ir::Loss>& losses) {
    if (!value.is_object()) return invalid_argument("responses: text.format must be an object");
    const auto* type = value.find("type");
    if (type == nullptr || !type->is_string()) {
        return invalid_argument("responses: text.format requires string type");
    }
    const auto& kind = type->as_string();
    if (kind == "text") {
        ir::ResponseFormat format;
        format.type = "text";
        return std::optional<ir::ResponseFormat>(std::move(format));
    }
    if (kind == "json_object") {
        ir::ResponseFormat format;
        format.type = "json_object";
        return std::optional<ir::ResponseFormat>(std::move(format));
    }
    if (kind != "json_schema") {
        losses.push_back(make_resp_loss("text.format.type", "type", ir::LOSS_UNMAPPED_VALUE,
                                        "unknown text.format type \"" + kind + "\""));
        return std::optional<ir::ResponseFormat>{};
    }
    const auto* name = value.find("name");
    if (name == nullptr || !name->is_string() || name->as_string().empty()) {
        return invalid_argument("responses: text.format json_schema requires non-empty name");
    }
    const auto* schema = value.find("schema");
    if (schema == nullptr || !schema->is_object()) {
        return invalid_argument("responses: text.format json_schema requires schema object");
    }
    ir::ResponseFormat format;
    format.type = "json_schema";
    format.name = name->as_string();
    format.schema = *schema;
    if (const auto* description = value.find("description"); description != nullptr) {
        if (!description->is_string()) return invalid_argument("responses: text.format description must be a string");
        format.description = description->as_string();
    }
    if (const auto* strict = value.find("strict"); strict != nullptr) {
        if (!strict->is_bool()) return invalid_argument("responses: text.format strict must be a boolean");
        format.strict = strict->as_bool();
    }
    return std::optional<ir::ResponseFormat>(std::move(format));
}

bool is_valid_https_url(std::string_view raw) {
    constexpr std::string_view kPrefix = "https://";
    if (!raw.starts_with(kPrefix)) return false;
    std::string_view rest = raw.substr(kPrefix.size());
    for (char c : rest) {
        if (static_cast<unsigned char>(c) <= 0x20) return false;
    }
    std::size_t end_host = rest.find_first_of("/?#");
    std::string_view host = (end_host == std::string_view::npos) ? rest : rest.substr(0, end_host);
    return !host.empty();
}

std::pair<std::vector<ir::Block>, std::vector<ir::Loss>> decode_content(
    const json::Value* content, const std::string& path) {
    std::vector<ir::Block> blocks;
    std::vector<ir::Loss> losses;
    if (!content || content->is_null()) {
        blocks.push_back(ir::TextBlock{""});
        return {blocks, losses};
    }
    if (content->is_string()) {
        blocks.push_back(ir::TextBlock{content->as_string()});
        return {blocks, losses};
    }
    if (content->is_array()) {
        const auto& arr = content->as_array();
        for (std::size_t i = 0; i < arr.size(); ++i) {
            const auto& part = arr[i];
            const auto* t = part.find("type");
            std::string kind = t && t->is_string() ? t->as_string() : "";
            std::string part_path = path + "[" + std::to_string(i) + "]";
            if (kind == "input_text") {
                const auto* txt = part.find("text");
                std::string text = txt && txt->is_string() ? txt->as_string() : "";
                blocks.push_back(ir::TextBlock{std::move(text)});
            } else if (kind == "input_image") {
                const auto* iu = part.find("image_url");
                std::string raw_url = iu && iu->is_string() ? iu->as_string() : "";
                std::string iu_path = part_path + ".image_url";
                if (raw_url.starts_with("https:")) {
                    if (is_valid_https_url(raw_url)) {
                        ir::ImageBlock img;
                        img.url = raw_url;
                        blocks.push_back(std::move(img));
                    } else {
                        losses.push_back(make_resp_loss(iu_path, "image_url",
                                                        ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                        "malformed https image URL has no IR equivalent"));
                    }
                } else if (raw_url.starts_with("data:")) {
                    std::string_view data_part = std::string_view(raw_url).substr(5);
                    std::size_t comma = data_part.find(',');
                    if (comma == std::string_view::npos) {
                        losses.push_back(make_resp_loss(iu_path, "image_url",
                                                        ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                        "malformed data image URL has no IR equivalent"));
                    } else {
                        std::string_view meta = data_part.substr(0, comma);
                        std::string_view b64 = data_part.substr(comma + 1);
                        constexpr std::string_view kBase64Suffix = ";base64";
                        if (!meta.ends_with(kBase64Suffix)) {
                            losses.push_back(make_resp_loss(iu_path, "image_url",
                                                          ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                          "malformed data image URL has no IR equivalent"));
                        } else {
                            std::string media_type(meta.substr(0, meta.size() - kBase64Suffix.size()));
                            std::string lower_mt = media_type;
                            for (char& c : lower_mt) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
                            if (!lower_mt.starts_with("image/") || lower_mt == "image/") {
                                losses.push_back(make_resp_loss(iu_path, "image_url",
                                                              ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                              "non-image data URL has no IR equivalent"));
                            } else {
                                ir::ImageBlock img;
                                img.media_type = std::move(media_type);
                                img.data = std::string(b64);
                                blocks.push_back(std::move(img));
                            }
                        }
                    }
                } else {
                    losses.push_back(make_resp_loss(iu_path, "image_url",
                                                    ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                    "only https and base64 data image URLs are supported"));
                }
            } else {
                losses.push_back(make_resp_loss(
                    part_path, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "Responses input content part type \"" + kind + "\" has no IR equivalent"));
            }
        }
        return {blocks, losses};
    }
    return {blocks, losses};
}

StatusOr<ir::Block> decode_tool_call_item(const json::Value& item, bool custom) {
    std::string call_id, name;
    if (const auto* v = item.find("call_id"); v && v->is_string()) call_id = v->as_string();
    if (const auto* v = item.find("name"); v && v->is_string()) name = v->as_string();
    if (const auto* v = item.find("namespace"); v && v->is_string() && !v->as_string().empty()) {
        const std::string ns = v->as_string();
        if (!name.starts_with(ns + ".")) {
            name = ns + "." + name;
        }
    }
    if (call_id.empty() || name.empty()) return invalid_argument("responses: tool call requires non-empty call_id and name");
    if (custom) {
        const auto* input = item.find("input");
        if (!input || !input->is_string()) return invalid_argument("responses: custom_tool_call requires string input");
        return ir::Block(ir::CustomToolUseBlock{std::move(call_id), std::move(name), input->as_string()});
    }
    std::string arguments;
    if (const auto* v = item.find("arguments"); v && v->is_string()) arguments = v->as_string();
    return ir::Block(ir::ToolUseBlock{std::move(call_id), std::move(name), std::move(arguments)});
}

StatusOr<std::variant<std::string, std::vector<ir::BlockHolder>>> decode_custom_output(
    const json::Value& value, const std::string& path, std::vector<ir::Loss>& losses) {
    if (value.is_string()) return std::variant<std::string, std::vector<ir::BlockHolder>>(value.as_string());
    if (!value.is_array()) return invalid_argument("responses: custom tool output must be a string or array");
    std::vector<ir::BlockHolder> parts;
    for (std::size_t i = 0; i < value.as_array().size(); ++i) {
        const auto& part = value.as_array()[i];
        const auto* t = part.find("type");
        const std::string kind = t && t->is_string() ? t->as_string() : "";
        const std::string part_path = path + "[" + std::to_string(i) + "]";
        if (kind == "input_text") {
            const auto* text = part.find("text");
            parts.push_back(ir::BlockHolder{ir::TextBlock{text && text->is_string() ? text->as_string() : ""}});
        } else if (kind == "input_image") {
            json::Value one = json::Value::array(); one.push_back(part);
            auto decoded = decode_content(&one, path);
            losses.insert(losses.end(), decoded.second.begin(), decoded.second.end());
            for (auto& block : decoded.first) if (std::holds_alternative<ir::ImageBlock>(block))
                parts.push_back(ir::BlockHolder{std::move(block)});
        } else {
            losses.push_back(make_resp_loss(part_path, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "custom tool output content is not supported"));
        }
    }
    return std::variant<std::string, std::vector<ir::BlockHolder>>(std::move(parts));
}

std::pair<std::optional<ir::ToolChoice>, std::vector<ir::Loss>> decode_tool_choice(
    const json::Value* tc) {
    if (!tc || tc->is_null()) return {std::nullopt, {}};
    if (tc->is_string()) {
        std::string s = tc->as_string();
        if (s == "auto") {
            return {ir::ToolChoice{std::string(ir::TOOL_CHOICE_AUTO), std::nullopt}, {}};
        }
        if (s == "none") {
            return {ir::ToolChoice{std::string(ir::TOOL_CHOICE_NONE), std::nullopt}, {}};
        }
        if (s == "required") {
            return {ir::ToolChoice{std::string(ir::TOOL_CHOICE_ANY), std::nullopt}, {}};
        }
        return {std::nullopt,
                {make_resp_loss("tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
                                "Responses tool_choice \"" + s + "\" has no IR equivalent")}};
    }
    if (tc->is_object()) {
        const auto* type_val = tc->find("type");
        std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";
        if (kind == "apply_patch") {
            ir::ToolChoice choice{std::string(ir::TOOL_CHOICE_TOOL), std::nullopt, "apply_patch"};
            return {std::move(choice), {}};
        }
        std::string name;
        if (const auto* nv = tc->find("name"); nv && nv->is_string()) name = nv->as_string();
        if (const auto* nsv = tc->find("namespace"); nsv && nsv->is_string() && !nsv->as_string().empty()) {
            const std::string ns = nsv->as_string();
            if (!name.starts_with(ns + ".")) {
                name = ns + "." + name;
            }
        }
        if ((kind == "function" || kind == "custom") && !name.empty()) {
            ir::ToolChoice choice{std::string(ir::TOOL_CHOICE_TOOL), name};
            if (kind == "custom") choice.tool_type = "custom";
            return {std::move(choice), {}};
        }
        return {std::nullopt,
                {make_resp_loss("tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
                                "only named function Responses tool_choice values are supported")}};
    }
    return {std::nullopt,
            {make_resp_loss("tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
                            "Responses tool_choice has no IR equivalent")}};
}

std::pair<std::string, std::vector<ir::Loss>> decode_status(
    const json::Value& wire, bool has_tool_use) {
    std::vector<ir::Loss> losses;
    if (const auto* err = wire.find("error"); err && err->is_object()) {
        std::string code;
        std::string msg;
        if (const auto* cv = err->find("code"); cv && cv->is_string()) code = cv->as_string();
        if (const auto* mv = err->find("message"); mv && mv->is_string()) msg = mv->as_string();
        losses.push_back(make_resp_loss(
            "error", "error", ir::LOSS_UNSUPPORTED_SEMANTIC,
            "failed Responses response carries error \"" + code + "\": " + msg));
        return {std::string(ir::STOP_OTHER), losses};
    }

    const auto* sv = wire.find("status");
    std::string st = sv && sv->is_string() ? sv->as_string() : "";
    if (st == "completed") {
        return {has_tool_use ? std::string(ir::STOP_TOOL_USE) : std::string(ir::STOP_END_TURN), losses};
    }
    if (st == "incomplete") {
        std::string reason;
        if (const auto* id_val = wire.find("incomplete_details"); id_val && id_val->is_object()) {
            if (const auto* rv = id_val->find("reason"); rv && rv->is_string()) reason = rv->as_string();
        }
        if (reason == "max_output_tokens") {
            return {std::string(ir::STOP_MAX_TOKENS), losses};
        }
        losses.push_back(make_resp_loss(
            "incomplete_details.reason", "reason", ir::LOSS_UNMAPPED_VALUE,
            "Responses incomplete_details reason \"" + reason + "\" has no IR equivalent"));
        return {std::string(ir::STOP_OTHER), losses};
    }
    if (st == "failed") {
        losses.push_back(make_resp_loss(
            "error", "error", ir::LOSS_UNSUPPORTED_SEMANTIC,
            "failed Responses response carries no error object"));
        return {std::string(ir::STOP_OTHER), losses};
    }
    return {std::string(ir::STOP_OTHER), losses};
}

std::vector<ir::Block> decode_reasoning_summary(
    const json::Value& item, std::size_t input_index, std::vector<ir::Loss>& losses) {
    std::vector<ir::Block> blocks;
    const auto* summary = item.find("summary");
    if (!summary || !summary->is_array()) return blocks;
    for (std::size_t i = 0; i < summary->as_array().size(); ++i) {
        const auto& part = summary->as_array()[i];
        const auto* type = part.find("type");
        const std::string kind = type && type->is_string() ? type->as_string() : "";
        if (kind == "output_text" || kind == "summary_text") {
            if (const auto* annotations = part.find("annotations");
                annotations && annotations->is_array() && !annotations->as_array().empty()) {
                losses.push_back(make_resp_loss(
                    "input[" + std::to_string(input_index) + "].summary[" + std::to_string(i) + "].annotations",
                    "annotations", ir::LOSS_UNMAPPED_FIELD,
                    "Responses reasoning summary annotations have no IR equivalent"));
            }
            const auto* text = part.find("text");
            blocks.push_back(ir::ThinkingBlock{
                text && text->is_string() ? text->as_string() : "", std::nullopt});
        } else {
            losses.push_back(make_resp_loss(
                "input[" + std::to_string(input_index) + "].summary[" + std::to_string(i) + "]",
                "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "Responses reasoning summary type \"" + kind + "\" has no IR equivalent"));
        }
    }
    return blocks;
}

StatusOr<std::optional<ir::ApplyPatchOperation>> decode_apply_patch_operation(
    const json::Value* op, const std::string& path, std::vector<ir::Loss>& losses) {
    if (!op || !op->is_object()) {
        return invalid_argument("responses: " + path + ".operation is required");
    }
    const auto* type_val = op->find("type");
    if (!type_val || !type_val->is_string() || type_val->as_string().empty()) {
        return invalid_argument("responses: " + path + ".operation.type is required");
    }
    std::string op_type = type_val->as_string();
    if (op_type == "create_file") {
        const auto* path_val = op->find("path");
        const auto* diff_val = op->find("diff");
        if (!path_val || !path_val->is_string()) {
            return invalid_argument("responses: " + path + ".operation.path must be a string");
        }
        if (!diff_val || !diff_val->is_string()) {
            return invalid_argument("responses: " + path + ".operation.diff is required for create_file");
        }
        return std::optional<ir::ApplyPatchOperation>{ir::CreateFileOperation{path_val->as_string(), diff_val->as_string()}};
    }
    if (op_type == "delete_file") {
        const auto* path_val = op->find("path");
        if (!path_val || !path_val->is_string()) {
            return invalid_argument("responses: " + path + ".operation.path must be a string");
        }
        return std::optional<ir::ApplyPatchOperation>{ir::DeleteFileOperation{path_val->as_string()}};
    }
    if (op_type == "update_file") {
        const auto* path_val = op->find("path");
        const auto* diff_val = op->find("diff");
        if (!path_val || !path_val->is_string()) {
            return invalid_argument("responses: " + path + ".operation.path must be a string");
        }
        if (!diff_val || !diff_val->is_string()) {
            return invalid_argument("responses: " + path + ".operation.diff is required for update_file");
        }
        return std::optional<ir::ApplyPatchOperation>{ir::UpdateFileOperation{path_val->as_string(), diff_val->as_string()}};
    }
    losses.push_back(make_resp_loss(path, "operation", ir::LOSS_UNSUPPORTED_SEMANTIC,
        "unknown apply_patch operation type: " + op_type));
    return std::optional<ir::ApplyPatchOperation>{};
}

StatusOr<std::optional<std::string>> decode_apply_patch_call_status(
    const json::Value* status_val, const std::string& path, std::vector<ir::Loss>& losses) {
    if (!status_val || !status_val->is_string() || status_val->as_string().empty()) {
        return invalid_argument("responses: " + path + ".status is required");
    }
    std::string_view status = status_val->as_string();
    if (status == "in_progress") return std::optional<std::string>("in_progress");
    if (status == "completed") return std::optional<std::string>("completed");
    losses.push_back(make_resp_loss(path + ".status", "status", ir::LOSS_UNMAPPED_VALUE,
        "unknown apply_patch_call status: " + std::string(status)));
    return std::optional<std::string>{};
}

StatusOr<std::optional<std::string>> decode_apply_patch_result_status(
    const json::Value* status_val, const std::string& path, std::vector<ir::Loss>& losses) {
    if (!status_val || !status_val->is_string() || status_val->as_string().empty()) {
        return invalid_argument("responses: " + path + ".status is required");
    }
    std::string_view status = status_val->as_string();
    if (status == "completed") return std::optional<std::string>("completed");
    if (status == "failed") return std::optional<std::string>("failed");
    losses.push_back(make_resp_loss(path + ".status", "status", ir::LOSS_UNMAPPED_VALUE,
        "unknown apply_patch_call_output status: " + std::string(status)));
    return std::optional<std::string>{};
}

json::Value encode_apply_patch_operation(const ir::ApplyPatchOperation& op) {
    json::Value out = json::Value::object();
    if (const auto* cf = std::get_if<ir::CreateFileOperation>(&op)) {
        out.set("type", json::Value::string("create_file"));
        out.set("path", json::Value::string(cf->path));
        out.set("diff", json::Value::string(cf->diff));
    } else if (const auto* df = std::get_if<ir::DeleteFileOperation>(&op)) {
        out.set("type", json::Value::string("delete_file"));
        out.set("path", json::Value::string(df->path));
    } else if (const auto* uf = std::get_if<ir::UpdateFileOperation>(&op)) {
        out.set("type", json::Value::string("update_file"));
        out.set("path", json::Value::string(uf->path));
        out.set("diff", json::Value::string(uf->diff));
    }
    return out;
}

struct PrecheckApplyPatchResult {
    std::map<std::size_t, ir::Block> decoded_calls;
    std::map<std::size_t, ir::Block> decoded_results;
    std::set<std::size_t> dropped;
    std::vector<ir::Loss> losses;
};

StatusOr<PrecheckApplyPatchResult> precheck_apply_patch_pairs(const std::vector<json::Value>& items) {
    PrecheckApplyPatchResult res;
    std::size_t i = 0;
    while (i < items.size()) {
        const auto& item = items[i];
        const auto* type_val = item.find("type");
        std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";
        if (kind == "apply_patch_call_output") {
            return invalid_argument("responses: input[" + std::to_string(i) + "]: apply_patch tool result without preceding tool calls (INV-3)");
        }
        const auto* role_val = item.find("role");
        std::string role = role_val && role_val->is_string() ? role_val->as_string() : "";

        bool is_assistant = ((kind.empty() || kind == "message") && role == "assistant") ||
                            kind == "function_call" || kind == "custom_tool_call" ||
                            kind == "apply_patch_call" || kind == "reasoning";

        if (!is_assistant) {
            ++i;
            continue;
        }

        struct CallInfo {
            std::string call_id;
            std::string cat;
            std::size_t index = 0;
        };
        std::vector<CallInfo> calls;
        std::set<std::string> seen_call_ids;
        std::size_t a_start = i;
        while (a_start < items.size()) {
            const auto& it = items[a_start];
            const auto* it_type = it.find("type");
            std::string it_kind = it_type && it_type->is_string() ? it_type->as_string() : "";
            const auto* it_role = it.find("role");
            std::string it_role_str = it_role && it_role->is_string() ? it_role->as_string() : "";

            std::string cur_kind;
            if (it_kind == "function_call") {
                cur_kind = "function";
            } else if (it_kind == "custom_tool_call") {
                cur_kind = "custom";
            } else if (it_kind == "apply_patch_call") {
                cur_kind = "apply_patch";
            } else if (it_kind == "reasoning" || ((it_kind.empty() || it_kind == "message") && it_role_str == "assistant")) {
                // continues assistant turn
            } else {
                break;
            }
            if (!cur_kind.empty()) {
                const auto* cid = it.find("call_id");
                std::string id = cid && cid->is_string() ? cid->as_string() : "";
                if (cur_kind == "apply_patch" && id.empty()) {
                    return invalid_argument("responses: input[" + std::to_string(a_start) + "]: apply_patch tool call requires nonempty call_id (INV-3)");
                }
                if (seen_call_ids.count(id)) {
                    return invalid_argument("responses: input[" + std::to_string(a_start) + "]: duplicate call_id \"" + id + "\" in turn (INV-3)");
                }
                seen_call_ids.insert(id);
                calls.push_back({id, cur_kind, a_start});
            }
            ++a_start;
        }

        std::vector<CallInfo> results;
        std::size_t o_start = a_start;
        while (o_start < items.size()) {
            const auto& it = items[o_start];
            const auto* it_type = it.find("type");
            std::string it_kind = it_type && it_type->is_string() ? it_type->as_string() : "";
            std::string matched_kind;
            if (it_kind == "function_call_output") {
                matched_kind = "function";
            } else if (it_kind == "custom_tool_call_output") {
                matched_kind = "custom";
            } else if (it_kind == "apply_patch_call_output") {
                matched_kind = "apply_patch";
            } else {
                break;
            }
            const auto* cid = it.find("call_id");
            std::string id = cid && cid->is_string() ? cid->as_string() : "";
            if (matched_kind == "apply_patch" && id.empty()) {
                return invalid_argument("responses: input[" + std::to_string(o_start) + "]: apply_patch tool result requires nonempty call_id (INV-3)");
            }
            results.push_back({id, matched_kind, o_start});
            ++o_start;
        }

        const bool has_apply_patch = std::any_of(calls.begin(), calls.end(), [](const CallInfo& c) { return c.cat == "apply_patch"; }) ||
                                     std::any_of(results.begin(), results.end(), [](const CallInfo& r) { return r.cat == "apply_patch"; });

        if (has_apply_patch) {
            if (calls.size() != results.size()) {
                return invalid_argument("responses: tool calls count (" + std::to_string(calls.size()) + ") and results count (" + std::to_string(results.size()) + ") mismatch (INV-3)");
            }
            for (std::size_t k = 0; k < calls.size(); ++k) {
                if (calls[k].call_id != results[k].call_id || calls[k].cat != results[k].cat) {
                    return invalid_argument("responses: tool result identity/type/order mismatch (INV-3)");
                }
            }
            for (std::size_t k = 0; k < calls.size(); ++k) {
                if (calls[k].cat != "apply_patch") continue;
                std::size_t c_idx = calls[k].index;
                std::size_t o_idx = results[k].index;
                const auto& c_item = items[c_idx];
                const auto& o_item = items[o_idx];

                std::vector<ir::Loss> call_losses;
                OXA_ASSIGN_OR_RETURN(auto op, decode_apply_patch_operation(c_item.find("operation"), "input[" + std::to_string(c_idx) + "]", call_losses));
                OXA_ASSIGN_OR_RETURN(auto call_status, decode_apply_patch_call_status(c_item.find("status"), "input[" + std::to_string(c_idx) + "]", call_losses));

                std::vector<ir::Loss> res_losses;
                OXA_ASSIGN_OR_RETURN(auto res_status, decode_apply_patch_result_status(o_item.find("status"), "input[" + std::to_string(o_idx) + "]", res_losses));

                std::optional<std::string> output_str;
                if (const auto* ov = o_item.find("output"); ov != nullptr && !ov->is_null()) {
                    if (!ov->is_string()) return invalid_argument("responses: input[" + std::to_string(o_idx) + "].output must be a string");
                    output_str = ov->as_string();
                }

                const bool has_call_loss = !call_losses.empty();
                const bool has_res_loss = !res_losses.empty();

                if (has_call_loss || has_res_loss) {
                    res.dropped.insert(c_idx);
                    res.dropped.insert(o_idx);
                    if (has_call_loss) {
                        res.losses.insert(res.losses.end(), call_losses.begin(), call_losses.end());
                    } else {
                        res.losses.push_back(make_resp_loss("input[" + std::to_string(c_idx) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC, "paired apply_patch result has unsupported semantic"));
                    }
                    if (has_res_loss) {
                        res.losses.insert(res.losses.end(), res_losses.begin(), res_losses.end());
                    } else {
                        res.losses.push_back(make_resp_loss("input[" + std::to_string(o_idx) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC, "paired apply_patch call has unsupported semantic"));
                    }
                } else if (op.has_value() && call_status.has_value() && res_status.has_value()) {
                    res.decoded_calls.emplace(c_idx, ir::ApplyPatchToolUseBlock{calls[k].call_id, *call_status, *op});
                    res.decoded_results.emplace(o_idx, ir::ApplyPatchToolResultBlock{results[k].call_id, *res_status, output_str});
                }
            }
        }
        i = o_start;
    }
    return res;
}

Status decode_responses_tool_item(
    const json::Value& t,
    const std::string& prefix,
    const std::string& container_type,
    const std::string& path,
    std::unordered_set<std::string>& seen_names,
    std::vector<ir::Tool>& tools,
    std::vector<ir::Loss>& losses) {
    if (!t.is_object()) return invalid_argument("responses: " + path + " must be an object");
    const auto* type_val = t.find("type");
    std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "function";

    if (kind == "namespace" || kind == "toolbox") {
        const auto* nv = t.find("name");
        std::string name = nv && nv->is_string() ? nv->as_string() : "";
        if (name.empty() || name.starts_with('.') || name.ends_with('.') || name.find("..") != std::string::npos) {
            return invalid_argument("responses: " + path + ": invalid " + kind + " name \"" + name + "\"");
        }
        const auto* tools_arr = t.find("tools");
        if (!tools_arr || !tools_arr->is_array() || tools_arr->as_array().empty()) {
            return invalid_argument("responses: " + path + ": " + kind + " requires non-empty tools array");
        }
        std::string current_prefix = prefix.empty() ? name : prefix + "." + name;
        for (std::size_t idx = 0; idx < tools_arr->as_array().size(); ++idx) {
            OXA_RETURN_IF_ERROR(decode_responses_tool_item(
                tools_arr->as_array()[idx],
                current_prefix,
                kind,
                path + ".tools[" + std::to_string(idx) + "]",
                seen_names,
                tools,
                losses));
        }
        return ok_status();
    }

    if (kind == "apply_patch") {
        ir::Tool tool;
        tool.type = "apply_patch";
        tools.push_back(std::move(tool));
        return ok_status();
    }

    if (kind == "custom") {
        const auto* nv = t.find("name");
        std::string raw_name = nv && nv->is_string() ? nv->as_string() : "";
        if (raw_name.empty()) return invalid_argument("responses: " + path + ".name is required for custom tool");
        std::string final_name = (!prefix.empty() && !raw_name.starts_with(prefix + ".")) ? prefix + "." + raw_name : raw_name;
        if (seen_names.count(final_name)) {
            return invalid_argument("responses: duplicate tool name \"" + final_name + "\"");
        }
        seen_names.insert(final_name);

        ir::Tool tool;
        tool.type = "custom";
        tool.name = final_name;
        if (const auto* dv = t.find("description"); dv && dv->is_string() && !dv->as_string().empty()) {
            tool.description = dv->as_string();
            tool.has_description = true;
        }
        if (const auto* format = t.find("format")) {
            if (!format->is_object()) return invalid_argument("responses: custom tool format must be an object");
            tool.format = *format;
            const auto* ft = format->find("type");
            const std::string ftype = ft && ft->is_string() ? ft->as_string() : "";
            if (ftype != "text" && ftype != "grammar") losses.push_back(make_resp_loss(
                path + ".format.type", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "unknown custom tool format type has no IR equivalent"));
            else if (ftype == "grammar") {
                const auto* syntax = format->find("syntax");
                if (!syntax || !syntax->is_string() || (syntax->as_string() != "lark" && syntax->as_string() != "regex"))
                    losses.push_back(make_resp_loss(path + ".format.syntax",
                        "syntax", ir::LOSS_UNSUPPORTED_SEMANTIC, "unknown custom tool grammar syntax has no IR equivalent"));
            }
        }
        if (!container_type.empty()) {
            tool.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_OPENAI),
                std::string(ir::NATIVE_KIND_CONTAINER),
                "\"" + container_type + "\"",
                ir::NativeOmission{"type", std::string(ir::LOSS_DEGRADED), "tool container grouping was flattened into dotted tool names"},
                std::nullopt,
            });
        }
        if (const auto* dl = t.find("defer_loading"); dl && dl->is_bool() && dl->as_bool()) {
            tool.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_OPENAI),
                std::string(ir::NATIVE_KIND_DEFER_LOADING),
                "true",
                ir::NativeOmission{"defer_loading", std::string(ir::LOSS_DEGRADED), std::string(ir::DEFER_LOADING_LOSS_DETAIL)},
                std::nullopt,
            });
        }
        tools.push_back(std::move(tool));
        return ok_status();
    }

    if (kind == "function" || kind.empty()) {
        const auto* nv = t.find("name");
        std::string raw_name = nv && nv->is_string() ? nv->as_string() : "";
        if (raw_name.empty()) return invalid_argument("responses: " + path + ".name is required for function tool");
        std::string final_name = (!prefix.empty() && !raw_name.starts_with(prefix + ".")) ? prefix + "." + raw_name : raw_name;
        if (seen_names.count(final_name)) {
            return invalid_argument("responses: duplicate tool name \"" + final_name + "\"");
        }
        seen_names.insert(final_name);

        ir::Tool tool;
        tool.type = "function";
        tool.name = final_name;
        if (const auto* dv = t.find("description"); dv && dv->is_string() && !dv->as_string().empty()) {
            tool.description = dv->as_string();
            tool.has_description = true;
        }
        if (const auto* pv = t.find("parameters")) tool.input_schema = *pv;
        else tool.input_schema = json::Value::null();

        if (t.find("strict") != nullptr) {
            losses.push_back(make_resp_loss(
                path + ".strict", "strict", ir::LOSS_UNMAPPED_FIELD,
                "Responses function tool strict mode has no IR equivalent in v1."));
        }

        if (!container_type.empty()) {
            tool.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_OPENAI),
                std::string(ir::NATIVE_KIND_CONTAINER),
                "\"" + container_type + "\"",
                ir::NativeOmission{"type", std::string(ir::LOSS_DEGRADED), "tool container grouping was flattened into dotted tool names"},
                std::nullopt,
            });
        }
        if (const auto* dl = t.find("defer_loading"); dl && dl->is_bool() && dl->as_bool()) {
            tool.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_OPENAI),
                std::string(ir::NATIVE_KIND_DEFER_LOADING),
                "true",
                ir::NativeOmission{"defer_loading", std::string(ir::LOSS_DEGRADED), std::string(ir::DEFER_LOADING_LOSS_DETAIL)},
                std::nullopt,
            });
        }
        tools.push_back(std::move(tool));
        return ok_status();
    }

    // Native tool vs unsupported tool
    bool has_name = t.find("name") && t.find("name")->is_string() && !t.find("name")->as_string().empty();
    bool is_known_native = (kind == "web_search" || kind == "file_search" || kind == "code_interpreter" ||
                            kind == "computer" || kind == "computer_use_preview" || kind == "mcp" ||
                            kind == "tool_search" || kind == "image_generation" || kind == "local_shell" ||
                            kind == "shell" || kind == "web_search_preview" || kind == "programmatic_tool_calling");

    if (has_name || is_known_native) {
        std::string tool_name = has_name ? t.find("name")->as_string() : kind;
        if (seen_names.count(tool_name)) {
            return invalid_argument("responses: duplicate tool name \"" + tool_name + "\"");
        }
        seen_names.insert(tool_name);

        ir::Tool tool;
        tool.type = "native";
        tool.name = tool_name;
        tool.source = "openai";
        tool.definition = json::serialize(t);
        tools.push_back(std::move(tool));
        return ok_status();
    }

    losses.push_back(make_resp_loss(path, "type",
        ir::LOSS_UNSUPPORTED_SEMANTIC, "Responses tool type \"" + kind + "\" has no IR equivalent"));
    return ok_status();
}

}  // namespace

static StatusOr<Conversion<ir::Request>> decode_request_unchecked(const json::Value& wire,
                                                 const Options& opts) {
    if (!wire.is_object()) return invalid_argument("responses: request must be an object");
    std::vector<ir::Loss> losses;

    if (wire.find("metadata") != nullptr) {
        losses.push_back(make_resp_loss("metadata", "metadata", ir::LOSS_UNMAPPED_FIELD,
                                        "Responses request metadata has no IR equivalent in v1."));
    }
    if (const auto* txt = wire.find("text"); txt && txt->is_object()) {
        if (txt->find("verbosity") != nullptr) {
            losses.push_back(make_resp_loss("text.verbosity", "verbosity", ir::LOSS_UNMAPPED_FIELD,
                                            "Responses output verbosity has no IR equivalent in v1."));
        }
    }
    if (wire.find("parallel_tool_calls") != nullptr) {
        losses.push_back(make_resp_loss("parallel_tool_calls", "parallel_tool_calls", ir::LOSS_UNMAPPED_FIELD,
                                        "Responses parallel tool calls have no IR equivalent in v1."));
    }

    ir::Request req;
    const auto* mv = wire.find("model");
    if (!mv || !mv->is_string()) return invalid_argument("responses: request missing model");
    req.model = opts.model_map.map(mv->as_string());

    if (const auto* inst = wire.find("instructions"); inst && inst->is_string()) {
        req.system.push_back(ir::TextBlock{inst->as_string()});
    }

    std::unordered_set<std::string> seen_names;
    std::vector<ir::Tool> tools;
    if (const auto* tools_val = wire.find("tools"); tools_val && tools_val->is_array()) {
        for (std::size_t i = 0; i < tools_val->as_array().size(); ++i) {
            OXA_RETURN_IF_ERROR(decode_responses_tool_item(
                tools_val->as_array()[i],
                "",
                "",
                "tools[" + std::to_string(i) + "]",
                seen_names,
                tools,
                losses));
        }
    }

    auto [tool_choice, tc_losses] = decode_tool_choice(wire.find("tool_choice"));
    req.tool_choice = std::move(tool_choice);
    losses.insert(losses.end(), tc_losses.begin(), tc_losses.end());

    const auto* inp = wire.find("input");
    if (!inp) return invalid_argument("responses: request missing input");

    if (inp->is_string()) {
        req.messages.push_back(ir::Message{std::string(ir::ROLE_USER), {ir::TextBlock{inp->as_string()}}});
    } else if (inp->is_array()) {
        const auto& items = inp->as_array();
        OXA_ASSIGN_OR_RETURN(auto precheck, precheck_apply_patch_pairs(items));
        losses.insert(losses.end(), precheck.losses.begin(), precheck.losses.end());
        std::size_t index = 0;
        while (index < items.size()) {
            const auto& item = items[index];
            const auto* type_val = item.find("type");
            std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";

            if (kind.empty() || kind == "message") {
                const auto* role_val = item.find("role");
                std::string role = role_val && role_val->is_string() ? role_val->as_string() : "";
                if (role == ROLE_SYSTEM || role == ROLE_DEVELOPER) {
                    if (role == ROLE_DEVELOPER) {
                        losses.push_back(make_resp_loss(
                            "input[" + std::to_string(index) + "].role", "role", ir::LOSS_DEGRADED,
                            "Responses developer instructions are carried as IR system content; the developer/system role distinction is not preserved."));
                    }
                    std::string path = "input[" + std::to_string(index) + "].content";
                    auto [blocks, bl_losses] = decode_content(item.find("content"), path);
                    losses.insert(losses.end(), bl_losses.begin(), bl_losses.end());
                    for (std::size_t child = 0; child < blocks.size(); ++child) {
                        if (auto* tb = std::get_if<ir::TextBlock>(&blocks[child])) {
                            req.system.push_back(std::move(*tb));
                        } else {
                            losses.push_back(make_resp_loss(
                                path + "[" + std::to_string(child) + "]", "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                                "Non-text content cannot be carried in the IR system field."));
                        }
                    }
                    ++index;
                } else if (role == "user") {
                    std::string path = "input[" + std::to_string(index) + "].content";
                    auto [blocks, bl_losses] = decode_content(item.find("content"), path);
                    losses.insert(losses.end(), bl_losses.begin(), bl_losses.end());
                    if (blocks.empty()) blocks.push_back(ir::TextBlock{""});
                    req.messages.push_back(ir::Message{std::string(ir::ROLE_USER), std::move(blocks)});
                    ++index;
                } else if (role == "assistant") {
                    // Assistant run: collect assistant messages and function_call items
                    std::vector<ir::Block> text_blocks;
                    std::vector<ir::Block> call_blocks;
                    while (index < items.size()) {
                        const auto& cur = items[index];
                        const auto* ct = cur.find("type");
                        std::string ckind = ct && ct->is_string() ? ct->as_string() : "";
                        if (ckind == "reasoning") {
                            auto blocks = decode_reasoning_summary(cur, index, losses);
                            text_blocks.insert(text_blocks.end(), std::make_move_iterator(blocks.begin()),
                                               std::make_move_iterator(blocks.end()));
                            ++index;
                            continue;
                        }
                        if (ckind == "apply_patch_call") {
                            if (precheck.dropped.count(index)) {
                                ++index;
                                continue;
                            }
                            if (precheck.decoded_calls.count(index)) {
                                call_blocks.push_back(precheck.decoded_calls.at(index));
                            }
                            ++index;
                            continue;
                        }
                        if (ckind == "function_call" || ckind == "custom_tool_call") {
                            OXA_ASSIGN_OR_RETURN(ir::Block call, decode_tool_call_item(cur, ckind == "custom_tool_call"));
                            call_blocks.push_back(std::move(call));
                            ++index;
                            continue;
                        }
                        if ((ckind.empty() || ckind == "message")) {
                            const auto* cr = cur.find("role");
                            std::string crole = cr && cr->is_string() ? cr->as_string() : "";
                            if (crole == "assistant") {
                                std::string path = "input[" + std::to_string(index) + "].content";
                                auto [blocks, bl_losses] = decode_content(cur.find("content"), path);
                                losses.insert(losses.end(), bl_losses.begin(), bl_losses.end());
                                text_blocks.insert(text_blocks.end(), std::make_move_iterator(blocks.begin()),
                                                   std::make_move_iterator(blocks.end()));
                                ++index;
                                continue;
                            }
                        }
                        break;
                    }
                    text_blocks.insert(text_blocks.end(), std::make_move_iterator(call_blocks.begin()),
                                       std::make_move_iterator(call_blocks.end()));
                    if (!text_blocks.empty()) {
                        req.messages.push_back(ir::Message{std::string(ir::ROLE_ASSISTANT), std::move(text_blocks)});
                    }
                } else {
                    return invalid_argument("responses: input[" + std::to_string(index) + "]: unknown role \"" + role + "\"");
                }
            } else if (kind == "function_call" || kind == "custom_tool_call" || kind == "reasoning" || kind == "apply_patch_call") {
                // Standalone reasoning/function_call items start an assistant run
                std::vector<ir::Block> text_blocks;
                std::vector<ir::Block> call_blocks;
                while (index < items.size()) {
                    const auto& cur = items[index];
                    const auto* ct = cur.find("type");
                    std::string ckind = ct && ct->is_string() ? ct->as_string() : "";
                    if (ckind == "reasoning") {
                        auto blocks = decode_reasoning_summary(cur, index, losses);
                        text_blocks.insert(text_blocks.end(), std::make_move_iterator(blocks.begin()),
                                           std::make_move_iterator(blocks.end()));
                        ++index;
                        continue;
                    }
                    if (ckind == "apply_patch_call") {
                        if (precheck.dropped.count(index)) {
                            ++index;
                            continue;
                        }
                        if (precheck.decoded_calls.count(index)) {
                            call_blocks.push_back(precheck.decoded_calls.at(index));
                        }
                        ++index;
                        continue;
                    }
                    if (ckind == "function_call" || ckind == "custom_tool_call") {
                        OXA_ASSIGN_OR_RETURN(ir::Block call, decode_tool_call_item(cur, ckind == "custom_tool_call"));
                        call_blocks.push_back(std::move(call));
                        ++index;
                        continue;
                    }
                    if (ckind.empty() || ckind == "message") {
                        const auto* cr = cur.find("role");
                        std::string crole = cr && cr->is_string() ? cr->as_string() : "";
                        if (crole == "assistant") {
                            std::string path = "input[" + std::to_string(index) + "].content";
                            auto [blocks, bl_losses] = decode_content(cur.find("content"), path);
                            losses.insert(losses.end(), bl_losses.begin(), bl_losses.end());
                            text_blocks.insert(text_blocks.end(), std::make_move_iterator(blocks.begin()),
                                               std::make_move_iterator(blocks.end()));
                            ++index;
                            continue;
                        }
                    }
                    break;
                }
                text_blocks.insert(text_blocks.end(), std::make_move_iterator(call_blocks.begin()),
                                   std::make_move_iterator(call_blocks.end()));
                if (!text_blocks.empty()) {
                    req.messages.push_back(ir::Message{std::string(ir::ROLE_ASSISTANT), std::move(text_blocks)});
                }
            } else if (kind == "function_call_output" || kind == "custom_tool_call_output" || kind == "apply_patch_call_output") {
                // Mixed result runs retain the source order and the call category.
                std::vector<ir::Block> tr_blocks;
                while (index < items.size()) {
                    const auto& cur = items[index];
                    const auto* ct = cur.find("type");
                    std::string ckind = ct && ct->is_string() ? ct->as_string() : "";
                    if (ckind != "function_call_output" && ckind != "custom_tool_call_output" && ckind != "apply_patch_call_output") break;
                    if (ckind == "apply_patch_call_output") {
                        if (precheck.dropped.count(index)) {
                            ++index;
                            continue;
                        }
                        if (precheck.decoded_results.count(index)) {
                            tr_blocks.push_back(precheck.decoded_results.at(index));
                        }
                        ++index;
                        continue;
                    }
                    const auto* cid = cur.find("call_id");
                    if (!cid || !cid->is_string() || cid->as_string().empty())
                        return invalid_argument("responses: call output requires non-empty call_id");
                    const auto* ov = cur.find("output");
                    if (ckind == "custom_tool_call_output") {
                        if (!ov) return invalid_argument("responses: custom_tool_call_output requires output");
                        OXA_ASSIGN_OR_RETURN(auto output, decode_custom_output(*ov,
                            "input[" + std::to_string(index) + "].output", losses));
                        tr_blocks.push_back(ir::CustomToolResultBlock{cid->as_string(), std::move(output)});
                    } else {
                        ir::ToolResultBlock tr; tr.tool_use_id = cid->as_string();
                        std::string text = ov && ov->is_string() ? ov->as_string() : "";
                        tr.content.push_back(ir::BlockHolder{ir::TextBlock{std::move(text)}});
                        tr_blocks.push_back(std::move(tr));
                    }
                    ++index;
                }
                if (!tr_blocks.empty()) {
                    req.messages.push_back(ir::Message{std::string(ir::ROLE_USER), std::move(tr_blocks)});
                }
            } else if (kind == "additional_tools") {
                const auto* item_tools = item.find("tools");
                if (!item_tools || !item_tools->is_array() || item_tools->as_array().empty()) {
                    return invalid_argument("responses: input[" + std::to_string(index) + "]: additional_tools requires non-empty tools array");
                }
                for (std::size_t t_idx = 0; t_idx < item_tools->as_array().size(); ++t_idx) {
                    OXA_RETURN_IF_ERROR(decode_responses_tool_item(
                        item_tools->as_array()[t_idx],
                        "",
                        "",
                        "input[" + std::to_string(index) + "].tools[" + std::to_string(t_idx) + "]",
                        seen_names,
                        tools,
                        losses));
                }
                ++index;
            } else {
                losses.push_back(make_resp_loss(
                    "input[" + std::to_string(index) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "Responses input item type \"" + kind + "\" has no IR equivalent"));
                ++index;
            }
        }
    }

    if (req.messages.empty()) {
        if (req.system.empty()) {
            return invalid_argument("responses: request carries no conversation input");
        }
        req.messages.push_back(ir::Message{std::string(ir::ROLE_USER), {ir::TextBlock{""}}});
        losses.push_back(make_resp_loss(
            "input", "input", ir::LOSS_DEGRADED,
            "An empty user message was added because the IR requires at least one conversation message."));
    }

    if (!tools.empty()) req.tools = std::move(tools);

    ir::Params params;
    bool has_params = false;
    if (const auto* temp = wire.find("temperature"); temp && temp->is_number()) {
        params.temperature = temp->is_int() ? static_cast<double>(temp->as_int()) : temp->as_double();
        has_params = true;
    }
    if (const auto* top_p = wire.find("top_p"); top_p && top_p->is_number()) {
        params.top_p = top_p->is_int() ? static_cast<double>(top_p->as_int()) : top_p->as_double();
        has_params = true;
    }
    if (const auto* mo = wire.find("max_output_tokens"); mo && mo->is_int()) {
        params.max_tokens = mo->as_int();
        has_params = true;
    }
    if (const auto* reasoning = wire.find("reasoning"); reasoning && !reasoning->is_null()) {
        std::string effort;
        if (reasoning->is_string()) {
            effort = reasoning->as_string();
        } else if (reasoning->is_object()) {
            if (reasoning->find("summary") != nullptr) {
                losses.push_back(make_resp_loss(
                    "reasoning.summary", "summary", ir::LOSS_UNMAPPED_FIELD,
                    "Responses reasoning summary preference has no IR equivalent"));
            }
            for (const auto& [key, value] : reasoning->as_object()) {
                if (key == "effort" || key == "summary") continue;
                (void)value;
                losses.push_back(make_resp_loss(
                    "reasoning." + key, key, ir::LOSS_UNMAPPED_FIELD,
                    "Responses reasoning property has no IR equivalent"));
            }
            if (const auto* effort_value = reasoning->find("effort"); effort_value && !effort_value->is_null()) {
                if (!effort_value->is_string()) {
                    return invalid_argument("responses: reasoning.effort must be a string");
                }
                effort = effort_value->as_string();
            }
        } else {
            return invalid_argument("responses: reasoning must be an object or string");
        }
        if (!effort.empty()) {
            if (effort == ir::REASONING_EFFORT_MINIMAL || effort == ir::REASONING_EFFORT_LOW ||
                effort == ir::REASONING_EFFORT_MEDIUM || effort == ir::REASONING_EFFORT_HIGH ||
                effort == ir::REASONING_EFFORT_XHIGH || effort == ir::REASONING_EFFORT_MAX) {
                params.reasoning_effort = effort;
                has_params = true;
            } else {
                losses.push_back(make_resp_loss(
                    "reasoning.effort", "effort", ir::LOSS_UNMAPPED_VALUE,
                    "unknown reasoning effort value \"" + effort + "\""));
            }
        }
    }
    if (const auto* text = wire.find("text"); text != nullptr && text->is_object()) {
        if (const auto* format = text->find("format"); format != nullptr) {
            OXA_ASSIGN_OR_RETURN(auto decoded_format, decode_text_format(*format, losses));
            if (decoded_format.has_value()) {
                params.response_format = std::move(*decoded_format);
                has_params = true;
            }
        }
    }
    if (has_params) req.params = std::move(params);

    return Conversion<ir::Request>{std::move(req), std::move(losses)};
}

StatusOr<Conversion<ir::Request>> decode_request(const json::Value& wire, const Options& opts) {
    auto result = decode_request_unchecked(wire, opts);
    if (!result.ok()) return result;
    result->losses = detail::audit_wire(wire, "request", wire_profile(), std::move(result->losses));
    return result;
}

StatusOr<Conversion<ir::Request>> decode_request(std::string_view wire_json,
                                                 const Options& opts) {
    OXA_ASSIGN_OR_RETURN(json::Value v, json::parse(wire_json));
    return decode_request(v, opts);
}

static json::Value encode_responses_grouped_tools(
    const std::vector<ir::Tool>& tools,
    std::vector<ir::Loss>& losses) {
    json::Value out = json::Value::array();
    std::size_t i = 0;
    while (i < tools.size()) {
        const auto& tool = tools[i];
        if (tool.type == "native") {
            if (tool.source.has_value() && *tool.source == "openai") {
                auto val = json::parse(tool.definition.value_or("{}"));
                if (val.ok()) {
                    out.push_back(val.value());
                }
            } else {
                losses.push_back(make_resp_loss(
                    "tools[" + std::to_string(i) + "]", "type",
                    ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "native tool \"" + tool.name + "\" from \"" + tool.source.value_or("") + "\" cannot be represented by the target protocol"));
            }
            ++i;
            continue;
        }

        if (tool.type == ir::TOOL_TYPE_APPLY_PATCH) {
            json::Value tw = json::Value::object();
            tw.set("type", json::Value::string("apply_patch"));
            out.push_back(std::move(tw));
            ++i;
            continue;
        }

        std::string container_kind;
        for (const auto& ann : tool.native) {
            if (ann.source == "openai" && ann.kind == "container") {
                auto parsed = json::parse(ann.value);
                if (parsed.ok() && parsed->is_string()) {
                    container_kind = parsed->as_string();
                } else {
                    std::string s = ann.value;
                    if (s.size() >= 2 && s.front() == '"' && s.back() == '"') {
                        container_kind = s.substr(1, s.size() - 2);
                    } else {
                        container_kind = s;
                    }
                }
                break;
            }
        }

        auto dot_idx = tool.name.find('.');
        if (dot_idx != std::string::npos) {
            std::string prefix = tool.name.substr(0, dot_idx);
            if (container_kind.empty()) container_kind = "namespace";
            json::Value child_tools = json::Value::array();
            std::size_t j = i;
            while (j < tools.size() && tools[j].name.starts_with(prefix + ".")) {
                const auto& sub = tools[j];
                std::string stripped_name = sub.name.substr(prefix.size() + 1);
                bool has_defer = false;
                for (const auto& ann : sub.native) {
                    if (ann.kind == "defer_loading" && ann.value == "true") {
                        has_defer = true;
                        break;
                    }
                }
                json::Value cw = json::Value::object();
                if (sub.type == "custom") {
                    cw.set("type", json::Value::string("custom"));
                    cw.set("name", json::Value::string(stripped_name));
                    if (sub.has_description) cw.set("description", json::Value::string(sub.description));
                    if (sub.format.has_value()) cw.set("format", *sub.format);
                    if (has_defer) cw.set("defer_loading", json::Value::boolean(true));
                } else {
                    cw.set("type", json::Value::string("function"));
                    cw.set("name", json::Value::string(stripped_name));
                    if (sub.has_description && !sub.description.empty()) {
                        cw.set("description", json::Value::string(sub.description));
                    }
                    cw.set("parameters", sub.input_schema.is_null() ? json::Value::object() : sub.input_schema);
                    if (has_defer) cw.set("defer_loading", json::Value::boolean(true));
                }
                child_tools.push_back(std::move(cw));
                ++j;
            }
            json::Value cobj = json::Value::object();
            cobj.set("type", json::Value::string(container_kind));
            cobj.set("name", json::Value::string(prefix));
            cobj.set("tools", std::move(child_tools));
            out.push_back(std::move(cobj));
            i = j;
            continue;
        }

        bool has_defer = false;
        for (const auto& ann : tool.native) {
            if (ann.kind == "defer_loading" && ann.value == "true") {
                has_defer = true;
                break;
            }
        }
        json::Value tw = json::Value::object();
        if (tool.type == "custom") {
            tw.set("type", json::Value::string("custom"));
            tw.set("name", json::Value::string(tool.name));
            if (tool.has_description) tw.set("description", json::Value::string(tool.description));
            if (tool.format.has_value()) tw.set("format", *tool.format);
            if (has_defer) tw.set("defer_loading", json::Value::boolean(true));
        } else {
            tw.set("type", json::Value::string("function"));
            tw.set("name", json::Value::string(tool.name));
            if (tool.has_description && !tool.description.empty()) {
                tw.set("description", json::Value::string(tool.description));
            }
            tw.set("parameters", tool.input_schema.is_null() ? json::Value::object() : tool.input_schema);
            if (has_defer) tw.set("defer_loading", json::Value::boolean(true));
        }
        out.push_back(std::move(tw));
        ++i;
    }
    return out;
}

StatusOr<Conversion<json::Value>> encode_request(const ir::Request& req,
                                                 const Options& opts) {
    std::vector<ir::Loss> losses;
    if (req.metadata.has_value() && !req.metadata->empty()) {
        losses.push_back(make_resp_loss(
            "metadata", "metadata", ir::LOSS_UNMAPPED_FIELD,
            "Responses requests have a string-valued metadata field with no IR equivalent; the IR metadata map is dropped."));
    }

    json::Value out = json::Value::object();
    out.set("model", json::Value::string(opts.model_map.map(req.model)));

    if (!req.system.empty()) {
        std::string inst;
        for (const auto& s : req.system) inst += s.text;
        out.set("instructions", json::Value::string(std::move(inst)));
    }

    bool all_tools_dropped = false;
    if (req.tools.has_value() && !req.tools->empty()) {
        json::Value tools_arr = encode_responses_grouped_tools(*req.tools, losses);
        all_tools_dropped = tools_arr.as_array().empty();
        if (!tools_arr.as_array().empty()) {
            out.set("tools", std::move(tools_arr));
        }
    }

    bool native_named_choice_dropped = false;
    if (req.tool_choice.has_value() && req.tool_choice->mode == "tool" && req.tool_choice->name.has_value()) {
        const auto& cname = *req.tool_choice->name;
        bool is_native = (req.tool_choice->tool_type == "native");
        if (!is_native && req.tools.has_value()) {
            for (const auto& t : *req.tools) {
                if (t.type == "native" && t.name == cname) {
                    is_native = true;
                    break;
                }
            }
        }
        bool is_openai_native = false;
        if (req.tools.has_value()) {
            for (const auto& t : *req.tools) {
                if (t.type == "native" && t.name == cname && t.source.has_value() && *t.source == "openai") {
                    is_openai_native = true;
                    break;
                }
            }
        }
        if (is_native && !is_openai_native) {
            native_named_choice_dropped = true;
        }
    }

    if (req.tool_choice.has_value() && req.tool_choice->mode == "any" && all_tools_dropped) {
        losses.push_back(make_resp_loss(
            "tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
            "all tool declarations were dropped"));
    } else if (native_named_choice_dropped) {
        losses.push_back(make_resp_loss(
            "tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
            "native tool choice \"" + *req.tool_choice->name + "\" cannot be represented by the target protocol"));
    } else if (req.tool_choice.has_value()) {
        const auto& tc = *req.tool_choice;
        if (tc.mode == ir::TOOL_CHOICE_AUTO) {
            out.set("tool_choice", json::Value::string("auto"));
        } else if (tc.mode == ir::TOOL_CHOICE_NONE) {
            out.set("tool_choice", json::Value::string("none"));
        } else if (tc.mode == ir::TOOL_CHOICE_ANY) {
            out.set("tool_choice", json::Value::string("required"));
        } else if (tc.mode == ir::TOOL_CHOICE_TOOL) {
            if (tc.tool_type.has_value() && *tc.tool_type == ir::TOOL_TYPE_APPLY_PATCH) {
                json::Value tco = json::Value::object();
                tco.set("type", json::Value::string("apply_patch"));
                out.set("tool_choice", std::move(tco));
            } else if (tc.name.has_value() && !tc.name->empty()) {
                json::Value tco = json::Value::object();
                const bool custom = tc.tool_type.has_value() && *tc.tool_type == "custom";
                tco.set("type", json::Value::string(custom ? "custom" : "function"));
                auto dot_idx = tc.name->find('.');
                if (dot_idx != std::string::npos) {
                    tco.set("namespace", json::Value::string(tc.name->substr(0, dot_idx)));
                    tco.set("name", json::Value::string(tc.name->substr(dot_idx + 1)));
                } else {
                    tco.set("name", json::Value::string(*tc.name));
                }
                out.set("tool_choice", std::move(tco));
            } else {
                losses.push_back(make_resp_loss("tool_choice", "tool_choice",
                                                ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                "IR named tool choice has no function name"));
            }
        }
    }

    std::vector<json::Value> items;
    for (std::size_t i = 0; i < req.messages.size(); ++i) {
        const auto& m = req.messages[i];
        if (m.role == ir::ROLE_USER) {
            std::vector<const ir::Block*> results;
            std::vector<const ir::Block*> normal;
            std::optional<std::size_t> first_normal;
            std::optional<std::size_t> last_result;

            for (std::size_t bi = 0; bi < m.content.size(); ++bi) {
                const auto& b = m.content[bi];
                if (std::holds_alternative<ir::ToolResultBlock>(b) ||
                    std::holds_alternative<ir::CustomToolResultBlock>(b) ||
                    std::holds_alternative<ir::ApplyPatchToolResultBlock>(b)) {
                    results.push_back(&b);
                    last_result = bi;
                } else if (std::holds_alternative<ir::DocumentBlock>(b)) {
                    losses.push_back(make_resp_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "IR ir.DocumentBlock cannot be rendered in a Responses user message item"));
                    normal.push_back(&b);
                } else {
                    if (!first_normal.has_value()) first_normal = bi;
                    normal.push_back(&b);
                }
            }

            if (first_normal.has_value() && last_result.has_value() && *first_normal < *last_result) {
                losses.push_back(make_resp_loss(
                    "messages[" + std::to_string(i) + "]", "ordering", ir::LOSS_DEGRADED,
                    "N-R-10: function_call_output items are hoisted ahead of the trailing user content; source order is not preserved"));
            }

            for (std::size_t ri = 0; ri < results.size(); ++ri) {
                const auto& result = *results[ri];
                json::Value item = json::Value::object();
                if (const auto* ap = std::get_if<ir::ApplyPatchToolResultBlock>(&result)) {
                    item.set("type", json::Value::string("apply_patch_call_output"));
                    item.set("call_id", json::Value::string(ap->tool_use_id));
                    item.set("status", json::Value::string(ap->status));
                    if (ap->output.has_value()) {
                        item.set("output", json::Value::string(*ap->output));
                    }
                } else if (const auto* custom = std::get_if<ir::CustomToolResultBlock>(&result)) {
                    item.set("type", json::Value::string("custom_tool_call_output"));
                    item.set("call_id", json::Value::string(custom->tool_use_id));
                    if (const auto* text = std::get_if<std::string>(&custom->output)) item.set("output", json::Value::string(*text));
                    else {
                        json::Value parts = json::Value::array();
                        for (const auto& inner : std::get<std::vector<ir::BlockHolder>>(custom->output)) {
                            json::Value part = json::Value::object();
                            if (const auto* text = std::get_if<ir::TextBlock>(&inner.block)) {
                                part.set("type", json::Value::string("input_text"));
                                part.set("text", json::Value::string(text->text));
                            } else if (const auto* image = std::get_if<ir::ImageBlock>(&inner.block)) {
                                part.set("type", json::Value::string("input_image"));
                                if (image->data.has_value() && !image->data->empty())
                                    part.set("image_url", json::Value::string("data:" + image->media_type.value_or("image/jpeg") + ";base64," + *image->data));
                                else if (image->url.has_value()) part.set("image_url", json::Value::string(*image->url));
                            }
                            parts.push_back(std::move(part));
                        }
                        item.set("output", std::move(parts));
                    }
                } else {
                    const auto* tr = std::get_if<ir::ToolResultBlock>(&result);
                    item.set("type", json::Value::string("function_call_output"));
                    item.set("call_id", json::Value::string(tr->tool_use_id));
                    std::string text;
                    for (const auto& inner : tr->content)
                        if (const auto* part = std::get_if<ir::TextBlock>(&inner.block)) text += part->text;
                    item.set("output", json::Value::string(std::move(text)));
                    if (tr->is_error) losses.push_back(make_resp_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(ri) + "].is_error",
                        "is_error", ir::LOSS_UNMAPPED_FIELD,
                        "Responses function_call_output items have no is_error field"));
                }
                items.push_back(std::move(item));
            }

            if (!normal.empty() || results.empty()) {
                json::Value mi = json::Value::object();
                mi.set("role", json::Value::string("user"));
                if (normal.size() == 1 && std::holds_alternative<ir::TextBlock>(*normal[0])) {
                    const auto& tb = std::get<ir::TextBlock>(*normal[0]);
                    mi.set("content", json::Value::string(tb.text));
                } else {
                    json::Value parts = json::Value::array();
                    for (const auto* b : normal) {
                        if (const auto* tb = std::get_if<ir::TextBlock>(b)) {
                            json::Value part = json::Value::object();
                            part.set("type", json::Value::string("input_text"));
                            part.set("text", json::Value::string(tb->text));
                            parts.push_back(std::move(part));
                        } else if (const auto* img = std::get_if<ir::ImageBlock>(b)) {
                            json::Value part = json::Value::object();
                            part.set("type", json::Value::string("input_image"));
                            if (img->data.has_value() && !img->data->empty()) {
                                std::string mt = img->media_type.value_or("image/jpeg");
                                part.set("image_url", json::Value::string("data:" + mt + ";base64," + *img->data));
                            } else if (img->url.has_value()) {
                                part.set("image_url", json::Value::string(*img->url));
                            }
                            parts.push_back(std::move(part));
                        }
                    }
                    mi.set("content", std::move(parts));
                }
                items.push_back(std::move(mi));
            }
        } else {
            // Assistant turn: reasoning summary, text message, then function_call items
            std::string a_txt;
            bool has_text = false;
            json::Value reasoning_summary = json::Value::array();
            std::vector<json::Value> fc_items;

            for (std::size_t bi = 0; bi < m.content.size(); ++bi) {
                const auto& b = m.content[bi];
                if (const auto* thinking = std::get_if<ir::ThinkingBlock>(&b)) {
                    json::Value part = json::Value::object();
                    part.set("type", json::Value::string("output_text"));
                    part.set("text", json::Value::string(thinking->thinking));
                    part.set("annotations", json::Value::array());
                    reasoning_summary.push_back(std::move(part));
                    if (thinking->signature.has_value() && !thinking->signature->empty()) {
                        losses.push_back(make_resp_loss(
                            "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "].signature",
                            "signature", ir::LOSS_UNMAPPED_FIELD,
                            "Responses request reasoning items carry no provider signature"));
                    }
                } else if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
                    a_txt += tb->text;
                    has_text = true;
                } else if (const auto* ap = std::get_if<ir::ApplyPatchToolUseBlock>(&b)) {
                    json::Value item = json::Value::object();
                    item.set("type", json::Value::string("apply_patch_call"));
                    item.set("call_id", json::Value::string(ap->id));
                    item.set("status", json::Value::string(ap->status));
                    item.set("operation", encode_apply_patch_operation(ap->operation));
                    fc_items.push_back(std::move(item));
                } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
                    json::Value fc = json::Value::object();
                    fc.set("type", json::Value::string("function_call"));
                    fc.set("call_id", json::Value::string(tu->id));
                    auto dot_idx = tu->name.find('.');
                    if (dot_idx != std::string::npos) {
                        fc.set("namespace", json::Value::string(tu->name.substr(0, dot_idx)));
                        fc.set("name", json::Value::string(tu->name.substr(dot_idx + 1)));
                    } else {
                        fc.set("name", json::Value::string(tu->name));
                    }
                    fc.set("arguments", json::Value::string(tu->input));
                    fc_items.push_back(std::move(fc));
                } else if (const auto* custom = std::get_if<ir::CustomToolUseBlock>(&b)) {
                    json::Value call = json::Value::object();
                    call.set("type", json::Value::string("custom_tool_call"));
                    call.set("call_id", json::Value::string(custom->id));
                    auto dot_idx = custom->name.find('.');
                    if (dot_idx != std::string::npos) {
                        call.set("namespace", json::Value::string(custom->name.substr(0, dot_idx)));
                        call.set("name", json::Value::string(custom->name.substr(dot_idx + 1)));
                    } else {
                        call.set("name", json::Value::string(custom->name));
                    }
                    call.set("input", json::Value::string(custom->input));
                    fc_items.push_back(std::move(call));
                }
            }
            const bool has_reasoning = !reasoning_summary.as_array().empty();
            if (has_reasoning) {
                json::Value reasoning = json::Value::object();
                reasoning.set("type", json::Value::string("reasoning"));
                reasoning.set("summary", std::move(reasoning_summary));
                items.push_back(std::move(reasoning));
            }
            if (has_text || (!has_reasoning && fc_items.empty())) {
                json::Value mi = json::Value::object();
                mi.set("role", json::Value::string("assistant"));
                mi.set("content", json::Value::string(std::move(a_txt)));
                items.push_back(std::move(mi));
            }
            for (auto& fc : fc_items) {
                items.push_back(std::move(fc));
            }
        }
    }

    if (req.system.empty() && items.size() == 1 &&
        items[0].find("role") && items[0].find("role")->is_string() &&
        items[0].find("role")->as_string() == "user" &&
        items[0].find("content") && items[0].find("content")->is_string()) {
        out.set("input", json::Value::string(items[0].find("content")->as_string()));
    } else {
        json::Value items_arr = json::Value::array();
        for (auto& item : items) items_arr.push_back(std::move(item));
        out.set("input", std::move(items_arr));
    }

    if (req.params.has_value()) {
        const auto& p = *req.params;
        if (p.temperature.has_value()) out.set("temperature", json::Value::real(*p.temperature));
        if (p.top_p.has_value()) out.set("top_p", json::Value::real(*p.top_p));
        if (p.max_tokens.has_value()) out.set("max_output_tokens", json::Value::integer(*p.max_tokens));
        if (p.reasoning_effort.has_value()) {
            json::Value reasoning = json::Value::object();
            reasoning.set("effort", json::Value::string(*p.reasoning_effort));
            out.set("reasoning", std::move(reasoning));
        }
        if (p.response_format.has_value()) {
            const auto& rf = *p.response_format;
            json::Value format = json::Value::object();
            format.set("type", json::Value::string(rf.type));
            if (rf.type == "json_schema") {
                format.set("name", json::Value::string(rf.name));
                if (rf.description.has_value()) {
                    format.set("description", json::Value::string(*rf.description));
                }
                if (rf.schema.has_value()) format.set("schema", *rf.schema);
                if (rf.strict.has_value()) format.set("strict", json::Value::boolean(*rf.strict));
            }
            json::Value text = json::Value::object();
            text.set("format", std::move(format));
            out.set("text", std::move(text));
        }
        if (p.stop_sequences.has_value() && !p.stop_sequences->empty()) {
            losses.push_back(make_resp_loss(
                "params.stop_sequences", "stop_sequences", ir::LOSS_UNMAPPED_FIELD,
                "Responses requests have no stop-sequences parameter; the IR stop sequences are dropped."));
        }
    }

    auto omitted = ir::omitted_native_request_losses(req, "responses");
    if (!omitted.ok()) return omitted.status();
    losses.insert(losses.end(), omitted->begin(), omitted->end());

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

static StatusOr<Conversion<ir::Response>> decode_response_unchecked(const json::Value& wire,
                                                  const Options& opts) {
    if (!wire.is_object()) return invalid_argument("responses: response must be an object");
    std::vector<ir::Loss> losses;
    ir::Response resp;

    if (const auto* id_val = wire.find("id"); id_val && id_val->is_string()) resp.id = id_val->as_string();
    if (const auto* m_val = wire.find("model"); m_val && m_val->is_string()) {
        resp.model = opts.model_map.map(m_val->as_string());
    }

    bool has_tool_use = false;
    if (const auto* out_val = wire.find("output"); out_val && out_val->is_array()) {
        for (std::size_t i = 0; i < out_val->as_array().size(); ++i) {
            const auto& item = out_val->as_array()[i];
            const auto* t = item.find("type");
            std::string kind = t && t->is_string() ? t->as_string() : "";
            std::string item_path = "output[" + std::to_string(i) + "]";
            if (kind == "message") {
                if (const auto* c_arr = item.find("content"); c_arr && c_arr->is_array()) {
                    for (std::size_t ci = 0; ci < c_arr->as_array().size(); ++ci) {
                        const auto& part = c_arr->as_array()[ci];
                        const auto* pt = part.find("type");
                        std::string pkind = pt && pt->is_string() ? pt->as_string() : "";
                        if (pkind != "output_text") {
                            losses.push_back(make_resp_loss(
                                item_path + ".content[" + std::to_string(ci) + "]", "type",
                                ir::LOSS_UNSUPPORTED_SEMANTIC,
                                "Responses output content type \"" + pkind + "\" has no IR equivalent"));
                            continue;
                        }
                        if (const auto* ann = part.find("annotations"); ann && ann->is_array() && !ann->as_array().empty()) {
                            losses.push_back(make_resp_loss(
                                item_path + ".content[" + std::to_string(ci) + "].annotations",
                                "annotations", ir::LOSS_UNMAPPED_FIELD,
                                "Responses output annotations have no IR equivalent in v1."));
                        }
                        if (const auto* txt = part.find("text"); txt && txt->is_string()) {
                            resp.content.push_back(ir::TextBlock{txt->as_string()});
                        }
                    }
                }
            } else if (kind == ITEM_TYPE_REASONING) {
                const auto* summary = item.find("summary");
                if (!summary || !summary->is_array() || summary->as_array().empty()) {
                    losses.push_back(make_resp_loss(
                        item_path, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "Responses reasoning output item with empty summary carries no convertible content"));
                } else {
                    for (std::size_t si = 0; si < summary->as_array().size(); ++si) {
                        const auto& part = summary->as_array()[si];
                        const auto* pt = part.find("type");
                        const std::string pkind = pt && pt->is_string() ? pt->as_string() : "";
                        if (pkind == "output_text" || pkind == "summary_text") {
                            if (const auto* annotations = part.find("annotations");
                                annotations && annotations->is_array() && !annotations->as_array().empty()) {
                                losses.push_back(make_resp_loss(
                                    item_path + ".summary[" + std::to_string(si) + "].annotations",
                                    "annotations", ir::LOSS_UNMAPPED_FIELD,
                                    "Responses reasoning summary annotations have no IR equivalent"));
                            }
                            const auto* text = part.find("text");
                            resp.content.push_back(ir::ThinkingBlock{
                                text && text->is_string() ? text->as_string() : "", std::nullopt});
                        } else {
                            losses.push_back(make_resp_loss(
                                item_path + ".summary[" + std::to_string(si) + "]", "type",
                                ir::LOSS_UNSUPPORTED_SEMANTIC,
                                "Responses reasoning summary type \"" + pkind + "\" has no IR equivalent"));
                        }
                    }
                }
                if (const auto* encrypted = item.find("encrypted_content");
                    encrypted && encrypted->is_string() && !encrypted->as_string().empty()) {
                    losses.push_back(make_resp_loss(
                        item_path + ".encrypted_content", "encrypted_content",
                        ir::LOSS_UNMAPPED_FIELD,
                        "Responses reasoning encrypted_content has no IR equivalent in v2."));
                }
            } else if (kind == "function_call") {
                OXA_ASSIGN_OR_RETURN(ir::Block call, decode_tool_call_item(item, false));
                resp.content.push_back(std::move(call));
                has_tool_use = true;
            } else if (kind == "custom_tool_call") {
                OXA_ASSIGN_OR_RETURN(ir::Block call, decode_tool_call_item(item, true));
                resp.content.push_back(std::move(call));
                has_tool_use = true;
            } else if (kind == "custom_tool_call_output") {
                const auto* cid = item.find("call_id"); const auto* output = item.find("output");
                if (!cid || !cid->is_string() || cid->as_string().empty() || !output)
                    return invalid_argument("responses: custom_tool_call_output requires non-empty call_id and output");
                OXA_ASSIGN_OR_RETURN(auto decoded, decode_custom_output(*output, item_path + ".output", losses));
                resp.content.push_back(ir::CustomToolResultBlock{cid->as_string(), std::move(decoded)});
            } else if (kind == "apply_patch_call") {
                const auto* id_val = item.find("id");
                if (!id_val || !id_val->is_string() || id_val->as_string().empty())
                    return invalid_argument("responses: " + item_path + ".id is required");
                const auto* cid = item.find("call_id");
                if (!cid || !cid->is_string() || cid->as_string().empty())
                    return invalid_argument("responses: " + item_path + ".call_id is required");
                std::vector<ir::Loss> item_losses;
                OXA_ASSIGN_OR_RETURN(auto call_status, decode_apply_patch_call_status(item.find("status"), item_path, item_losses));
                OXA_ASSIGN_OR_RETURN(auto op, decode_apply_patch_operation(item.find("operation"), item_path + ".operation", item_losses));
                if (!item_losses.empty()) {
                    losses.insert(losses.end(), item_losses.begin(), item_losses.end());
                } else if (call_status.has_value() && op.has_value()) {
                    resp.content.push_back(ir::ApplyPatchToolUseBlock{cid->as_string(), *call_status, *op});
                    has_tool_use = true;
                }
            } else if (kind == "apply_patch_call_output") {
                const auto* id_val = item.find("id");
                if (!id_val || !id_val->is_string() || id_val->as_string().empty())
                    return invalid_argument("responses: " + item_path + ".id is required");
                const auto* cid = item.find("call_id");
                if (!cid || !cid->is_string() || cid->as_string().empty())
                    return invalid_argument("responses: " + item_path + ".call_id is required");
                std::vector<ir::Loss> item_losses;
                OXA_ASSIGN_OR_RETURN(auto res_status, decode_apply_patch_result_status(item.find("status"), item_path, item_losses));
                std::optional<std::string> output;
                if (const auto* ov = item.find("output"); ov != nullptr && !ov->is_null()) {
                    if (!ov->is_string()) return invalid_argument("responses: " + item_path + ".output must be a string");
                    output = ov->as_string();
                }
                if (!item_losses.empty()) {
                    losses.insert(losses.end(), item_losses.begin(), item_losses.end());
                } else if (res_status.has_value()) {
                    resp.content.push_back(ir::ApplyPatchToolResultBlock{cid->as_string(), *res_status, std::move(output)});
                }
            } else {
                losses.push_back(make_resp_loss(
                    item_path, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "Responses output item type \"" + kind + "\" has no IR equivalent"));
            }
        }
    }

    auto [stop_reason, status_losses] = decode_status(wire, has_tool_use);
    resp.stop_reason = std::move(stop_reason);
    losses.insert(losses.end(), status_losses.begin(), status_losses.end());

    if (const auto* u = wire.find("usage"); u && u->is_object()) {
        if (const auto* in = u->find("input_tokens"); in && in->is_int()) resp.usage.input_tokens = in->as_int();
        if (const auto* out = u->find("output_tokens"); out && out->is_int()) resp.usage.output_tokens = out->as_int();
        if (const auto* details = u->find("input_token_details"); details && details->is_object()) {
            if (const auto* cached = details->find("cached_tokens"); cached && cached->is_int()) {
                resp.usage.input_tokens_details = ir::InputTokensDetails{cached->as_int()};
            }
        }
        if (const auto* details = u->find("output_token_details"); details && details->is_object()) {
            if (const auto* reasoning = details->find("reasoning_tokens"); reasoning && reasoning->is_int()) {
                resp.usage.output_tokens_details = ir::OutputTokensDetails{reasoning->as_int()};
            }
        }
    }

    return Conversion<ir::Response>{std::move(resp), std::move(losses)};
}

StatusOr<Conversion<ir::Response>> decode_response(const json::Value& wire, const Options& opts) {
    auto result = decode_response_unchecked(wire, opts);
    if (!result.ok()) return result;
    result->losses = detail::audit_wire(wire, "response", wire_profile(), std::move(result->losses));
    return result;
}

StatusOr<Conversion<ir::Response>> decode_response(std::string_view wire_json,
                                                  const Options& opts) {
    OXA_ASSIGN_OR_RETURN(json::Value v, json::parse(wire_json));
    return decode_response(v, opts);
}

StatusOr<Conversion<json::Value>> encode_response(const ir::Response& resp,
                                                  const Options& opts) {
    std::vector<ir::Loss> losses;
    json::Value out = json::Value::object();
    out.set("id", json::Value::string(resp.id));
    out.set("object", json::Value::string("response"));
    out.set("model", json::Value::string(opts.model_map.map(resp.model)));

    std::string text;
    bool has_text = false;
    std::size_t next_apc = 0;
    std::size_t next_apco = 0;
    json::Value reasoning_parts = json::Value::array();
    std::vector<json::Value> calls;

    for (std::size_t i = 0; i < resp.content.size(); ++i) {
        const auto& b = resp.content[i];
        if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
            text += tb->text;
            has_text = true;
        } else if (const auto* thinking = std::get_if<ir::ThinkingBlock>(&b)) {
            json::Value part = json::Value::object();
            part.set("type", json::Value::string("output_text"));
            part.set("text", json::Value::string(thinking->thinking));
            part.set("annotations", json::Value::array());
            reasoning_parts.push_back(std::move(part));
            if (thinking->signature.has_value() && !thinking->signature->empty()) {
                losses.push_back(make_resp_loss(
                    "content[" + std::to_string(i) + "].signature", "signature",
                    ir::LOSS_UNMAPPED_FIELD,
                    "Responses reasoning output items carry no provider signature"));
            }
        } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
            json::Value fc = json::Value::object();
            fc.set("type", json::Value::string("function_call"));
            fc.set("id", json::Value::string("fc_abc123"));
            fc.set("status", json::Value::string("completed"));
            fc.set("call_id", json::Value::string(tu->id));
            auto dot_idx = tu->name.find('.');
            if (dot_idx != std::string::npos) {
                fc.set("namespace", json::Value::string(tu->name.substr(0, dot_idx)));
                fc.set("name", json::Value::string(tu->name.substr(dot_idx + 1)));
            } else {
                fc.set("name", json::Value::string(tu->name));
            }
            fc.set("arguments", json::Value::string(tu->input));
            calls.push_back(std::move(fc));
        } else if (const auto* custom = std::get_if<ir::CustomToolUseBlock>(&b)) {
            json::Value call = json::Value::object();
            call.set("type", json::Value::string("custom_tool_call"));
            call.set("id", json::Value::string("ctc_abc123"));
            call.set("call_id", json::Value::string(custom->id));
            auto dot_idx = custom->name.find('.');
            if (dot_idx != std::string::npos) {
                call.set("namespace", json::Value::string(custom->name.substr(0, dot_idx)));
                call.set("name", json::Value::string(custom->name.substr(dot_idx + 1)));
            } else {
                call.set("name", json::Value::string(custom->name));
            }
            call.set("input", json::Value::string(custom->input));
            calls.push_back(std::move(call));
        } else if (const auto* custom_result = std::get_if<ir::CustomToolResultBlock>(&b)) {
            json::Value result = json::Value::object();
            result.set("type", json::Value::string("custom_tool_call_output"));
            result.set("call_id", json::Value::string(custom_result->tool_use_id));
            if (const auto* output = std::get_if<std::string>(&custom_result->output))
                result.set("output", json::Value::string(*output));
            else {
                json::Value parts = json::Value::array();
                for (const auto& inner : std::get<std::vector<ir::BlockHolder>>(custom_result->output)) {
                    json::Value part = json::Value::object();
                    if (const auto* text = std::get_if<ir::TextBlock>(&inner.block)) {
                        part.set("type", json::Value::string("input_text"));
                        part.set("text", json::Value::string(text->text));
                    } else if (const auto* image = std::get_if<ir::ImageBlock>(&inner.block)) {
                        part.set("type", json::Value::string("input_image"));
                        if (image->data.has_value() && !image->data->empty())
                            part.set("image_url", json::Value::string("data:" + image->media_type.value_or("image/jpeg") + ";base64," + *image->data));
                        else if (image->url.has_value()) part.set("image_url", json::Value::string(*image->url));
                    }
                    parts.push_back(std::move(part));
                }
                result.set("output", std::move(parts));
            }
            calls.push_back(std::move(result));
        } else if (const auto* ap = std::get_if<ir::ApplyPatchToolUseBlock>(&b)) {
            json::Value call = json::Value::object();
            call.set("type", json::Value::string("apply_patch_call"));
            call.set("id", json::Value::string("apc_" + std::to_string(next_apc++)));
            call.set("call_id", json::Value::string(ap->id));
            call.set("status", json::Value::string(ap->status));
            call.set("operation", encode_apply_patch_operation(ap->operation));
            calls.push_back(std::move(call));
        } else if (const auto* apr = std::get_if<ir::ApplyPatchToolResultBlock>(&b)) {
            json::Value result = json::Value::object();
            result.set("type", json::Value::string("apply_patch_call_output"));
            result.set("id", json::Value::string("apco_" + std::to_string(next_apco++)));
            result.set("call_id", json::Value::string(apr->tool_use_id));
            result.set("status", json::Value::string(apr->status));
            if (apr->output.has_value()) {
                result.set("output", json::Value::string(*apr->output));
            }
            calls.push_back(std::move(result));
        } else {
            losses.push_back(make_resp_loss(
                "content[" + std::to_string(i) + "]", "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "this IR block cannot be rendered in a Responses output item"));
        }
    }

    json::Value output_arr = json::Value::array();
    if (!reasoning_parts.as_array().empty()) {
        json::Value reasoning = json::Value::object();
        reasoning.set("type", json::Value::string(std::string(ITEM_TYPE_REASONING)));
        reasoning.set("id", json::Value::string("rs_abc123"));
        reasoning.set("summary", std::move(reasoning_parts));
        output_arr.push_back(std::move(reasoning));
    }
    if (has_text || resp.content.empty()) {
        json::Value msg = json::Value::object();
        msg.set("type", json::Value::string("message"));
        msg.set("id", json::Value::string("msg_abc123"));
        msg.set("status", json::Value::string("completed"));
        msg.set("role", json::Value::string("assistant"));
        json::Value c_arr = json::Value::array();
        json::Value op = json::Value::object();
        op.set("type", json::Value::string("output_text"));
        op.set("text", json::Value::string(std::move(text)));
        op.set("annotations", json::Value::array());
        c_arr.push_back(std::move(op));
        msg.set("content", std::move(c_arr));
        output_arr.push_back(std::move(msg));
    }
    for (auto& fc : calls) output_arr.push_back(std::move(fc));
    out.set("output", std::move(output_arr));

    if (resp.stop_reason == ir::STOP_END_TURN || resp.stop_reason == ir::STOP_TOOL_USE) {
        out.set("status", json::Value::string("completed"));
    } else if (resp.stop_reason == ir::STOP_MAX_TOKENS) {
        out.set("status", json::Value::string("incomplete"));
        json::Value inc = json::Value::object();
        inc.set("reason", json::Value::string("max_output_tokens"));
        out.set("incomplete_details", std::move(inc));
    } else if (resp.stop_reason == ir::STOP_STOP_SEQUENCE) {
        losses.push_back(make_resp_loss(
            "", "stop_sequence", ir::LOSS_UNMAPPED_VALUE,
            "Responses status carries no stop-sequence identity; the matched IR stop sequence is lost"));
        out.set("status", json::Value::string("completed"));
    } else if (resp.stop_reason == ir::STOP_REFUSAL) {
        out.set("status", json::Value::string("failed"));
        json::Value err = json::Value::object();
        err.set("code", json::Value::string("refusal"));
        err.set("message", json::Value::string(""));
        out.set("error", std::move(err));
    } else {
        return invalid_argument("responses: stop reason \"" + std::string(resp.stop_reason) +
                                "\" has no Responses equivalent");
    }

    json::Value usage = json::Value::object();
    usage.set("input_tokens", json::Value::integer(resp.usage.input_tokens));
    usage.set("output_tokens", json::Value::integer(resp.usage.output_tokens));
    usage.set("total_tokens", json::Value::integer(resp.usage.input_tokens + resp.usage.output_tokens));
    if (resp.usage.input_tokens_details.has_value()) {
        json::Value details = json::Value::object();
        details.set("cached_tokens", json::Value::integer(resp.usage.input_tokens_details->cached_tokens));
        usage.set("input_token_details", std::move(details));
    }
    if (resp.usage.output_tokens_details.has_value()) {
        json::Value details = json::Value::object();
        details.set("reasoning_tokens", json::Value::integer(resp.usage.output_tokens_details->reasoning_tokens));
        usage.set("output_token_details", std::move(details));
    }
    out.set("usage", std::move(usage));

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

// ---- StreamDecoder --------------------------------------------------------

StreamDecoder::StreamDecoder(Options opts) : opts_(std::move(opts)) {}

Status StreamDecoder::require_started(std::string_view event_type) {
    if (!started_) {
        return invalid_argument("responses: " + std::string(event_type) + " before response.created");
    }
    return ok_status();
}

Status StreamDecoder::require_active_item(const json::Value& ev, std::string_view event_type) {
    OXA_RETURN_IF_ERROR(require_started(event_type));
    std::int64_t out_idx = -1;
    if (const auto* oi = ev.find("output_index"); oi && oi->is_int()) out_idx = oi->as_int();
    std::string item_id;
    if (const auto* idv = ev.find("item_id"); idv && idv->is_string()) item_id = idv->as_string();
    if (!item_open_ || out_idx != output_index_ || item_id != item_id_) {
        return invalid_argument("responses: " + std::string(event_type) + " does not match the open output item");
    }
    return ok_status();
}

Status StreamDecoder::require_function_call(const json::Value& ev, std::string_view event_type) {
    OXA_RETURN_IF_ERROR(require_active_item(ev, event_type));
    if (!function_call_.has_value()) {
        return invalid_argument("responses: " + std::string(event_type) + " without an active function_call item");
    }
    return ok_status();
}

Status StreamDecoder::require_custom_tool_call(const json::Value& ev, std::string_view event_type) {
    OXA_RETURN_IF_ERROR(require_active_item(ev, event_type));
    if (!custom_tool_call_.has_value())
        return invalid_argument("responses: " + std::string(event_type) + " without an active custom_tool_call item");
    return ok_status();
}

ir::Loss StreamDecoder::unsupported_item_loss(std::int64_t output_index, std::string_view item_type) {
    std::string detail;
    if (item_type == "function_call_output") {
        detail = "N-S-10: Responses function_call_output has no supported IR block mapping; "
                 "response.output_item.done completes and is absorbed for this item-only lifecycle vector";
    } else {
        detail = "Responses streaming output item type \"" + std::string(item_type) + "\" is not decoded";
    }
    return make_resp_loss("output[" + std::to_string(output_index) + "]", "type",
                          ir::LOSS_UNSUPPORTED_SEMANTIC, std::move(detail));
}

StatusOr<std::vector<ir::Event>> StreamDecoder::replay_function_call(const StreamFunctionCall& call) {
    std::string joined;
    for (const auto& f : call.fragments) {
        joined += f;
    }
    std::int64_t index = next_block_index_++;
    std::vector<ir::Event> events;
    events.reserve(call.fragments.size() + 2);
    events.push_back(ir::ContentBlockStart{index, ir::ToolUseBlock{call.call_id, call.name, joined}});
    for (const auto& f : call.fragments) {
        events.push_back(ir::ContentBlockDelta{index, ir::InputJsonDelta{f}});
    }
    events.push_back(ir::ContentBlockStop{index});
    return events;
}

StatusOr<std::vector<ir::Event>> StreamDecoder::replay_custom_tool_call(const StreamCustomToolCall& call) {
    std::string joined;
    for (const auto& fragment : call.fragments) joined += fragment;
    const std::string final_input = call.snapshot.value_or(joined);
    if (call.snapshot.has_value() && joined != final_input && !call.fragments.empty())
        return invalid_argument("responses: custom tool input snapshot does not match delta fragments");
    std::int64_t index = next_block_index_++;
    std::vector<ir::Event> events;
    events.reserve(call.fragments.size() + 2);
    events.push_back(ir::ContentBlockStart{index, ir::CustomToolUseBlock{call.call_id, call.name, final_input}});
    if (call.fragments.empty()) events.push_back(ir::ContentBlockDelta{index, ir::CustomToolInputDelta{final_input}});
    else for (const auto& fragment : call.fragments)
        events.push_back(ir::ContentBlockDelta{index, ir::CustomToolInputDelta{fragment}});
    events.push_back(ir::ContentBlockStop{index});
    return events;
}

StatusOr<std::vector<ir::Event>> StreamDecoder::feed(const json::Value& event) {
    const auto before = losses_.size();
    auto result = feed_unchecked(event);
    if (!result.ok()) return result;
    std::vector<ir::Loss> produced(losses_.begin() + before, losses_.end());
    produced = detail::audit_wire(event, "event", wire_profile(), std::move(produced));
    losses_.resize(before);
    losses_.insert(losses_.end(), produced.begin(), produced.end());
    return result;
}

StatusOr<std::vector<ir::Event>> StreamDecoder::feed(std::string_view wire_json) {
    OXA_ASSIGN_OR_RETURN(json::Value event, json::parse(wire_json));
    return feed(event);
}

StatusOr<std::vector<ir::Event>> StreamDecoder::feed_unchecked(const json::Value& chunk) {
    if (flushed_) return invalid_argument("responses: event fed after stream flush");
    if (terminated_) return invalid_argument("responses: event fed after terminal response");
    if (!chunk.is_object()) return invalid_argument("responses: event must be an object");

    const auto* t = chunk.find("type");
    std::string type = t && t->is_string() ? t->as_string() : "";

    if ((apply_patch_call_.has_value() || apply_patch_result_.has_value()) &&
        (type.rfind("response.content_part.", 0) == 0 ||
         type.rfind("response.output_text.", 0) == 0 ||
         type.rfind("response.reasoning_summary_", 0) == 0 ||
         type.rfind("response.function_call_arguments.", 0) == 0 ||
         type.rfind("response.custom_tool_call_input.", 0) == 0)) {
        return invalid_argument("responses: content or delta event on apply patch item");
    }

    std::vector<ir::Event> events;

    if (type == "response.created") {
        if (started_) return invalid_argument("responses: duplicate response.created");
        const auto* resp = chunk.find("response");
        if (!resp || !resp->is_object()) return invalid_argument("responses: response.created without response");
        started_ = true;
        std::string raw_model;
        if (const auto* idv = resp->find("id"); idv && idv->is_string()) id_ = idv->as_string();
        if (const auto* mv = resp->find("model"); mv && mv->is_string()) raw_model = mv->as_string();
        model_ = opts_.model_map.map(raw_model);
        events.push_back(ir::MessageStart{id_, model_});
        return events;
    }

    if (type == "response.output_item.added") {
        OXA_RETURN_IF_ERROR(require_started("response.output_item.added"));
        if (item_open_) return invalid_argument("responses: response.output_item.added with an item still open");
        std::int64_t out_idx = -1;
        if (const auto* oi = chunk.find("output_index"); oi && oi->is_int()) out_idx = oi->as_int();
        if (out_idx != next_output_index_) {
            return invalid_argument("responses: output_item.added output_index " + std::to_string(out_idx) +
                                    ", want " + std::to_string(next_output_index_));
        }
        const auto* item = chunk.find("item");
        if (!item || !item->is_object()) return invalid_argument("responses: response.output_item.added without item");

        next_output_index_++;
        item_open_ = true;
        std::string ikind;
        if (const auto* it = item->find("type"); it && it->is_string()) ikind = it->as_string();
        item_type_ = ikind;
        output_index_ = out_idx;
        if (const auto* idv = item->find("id"); idv && idv->is_string()) item_id_ = idv->as_string();
        skipped_call_id_.clear();
        next_content_index_ = 0;
        function_call_ = std::nullopt;
        custom_tool_call_ = std::nullopt;
        apply_patch_call_ = std::nullopt;
        apply_patch_result_ = std::nullopt;

        std::string role;
        if (const auto* rv = item->find("role"); rv && rv->is_string()) role = rv->as_string();

        if (ikind == "message" && role == "assistant") {
            return events;
        }
        if (ikind == ITEM_TYPE_REASONING) {
            return events;
        }
        if (ikind == "function_call") {
            std::string call_id, name;
            if (const auto* cid = item->find("call_id"); cid && cid->is_string()) call_id = cid->as_string();
            if (const auto* nv = item->find("name"); nv && nv->is_string()) name = nv->as_string();
            if (item_id_.empty() || call_id.empty() || name.empty()) {
                return invalid_argument("responses: function_call item requires id, call_id, and name");
            }
            StreamFunctionCall fc;
            fc.item_id = item_id_;
            fc.output_index = out_idx;
            fc.call_id = call_id;
            fc.name = name;
            if (const auto* av = item->find("arguments"); av && av->is_string()) {
                fc.fragments.push_back(av->as_string());
            }
            function_call_ = std::move(fc);
            return events;
        }
        if (ikind == "custom_tool_call") {
            std::string call_id, name;
            if (const auto* v = item->find("call_id"); v && v->is_string()) call_id = v->as_string();
            if (const auto* v = item->find("name"); v && v->is_string()) name = v->as_string();
            const auto* input = item->find("input");
            if (item_id_.empty() || call_id.empty() || name.empty() || !input || !input->is_string() || !input->as_string().empty())
                return invalid_argument("responses: custom_tool_call added item requires id, call_id, name, and input empty string");
            custom_tool_call_ = StreamCustomToolCall{item_id_, out_idx, call_id, name, "", {}, std::nullopt};
            return events;
        }
        if (ikind == "apply_patch_call") {
            std::string call_id, status;
            if (const auto* cid = item->find("call_id"); cid && cid->is_string()) call_id = cid->as_string();
            if (const auto* st = item->find("status"); st && st->is_string()) status = st->as_string();
            const auto* op = item->find("operation");
            if (item_id_.empty() || call_id.empty() || status.empty() || !op || !op->is_object()) {
                return invalid_argument("responses: apply_patch_call requires id, call_id, status, and operation");
            }
            apply_patch_call_ = StreamApplyPatchCall{item_id_, out_idx, call_id, status, *op};
            return events;
        }
        if (ikind == "apply_patch_call_output") {
            std::string call_id, status;
            if (const auto* cid = item->find("call_id"); cid && cid->is_string()) call_id = cid->as_string();
            if (const auto* st = item->find("status"); st && st->is_string()) status = st->as_string();
            if (item_id_.empty() || call_id.empty() || status.empty()) {
                return invalid_argument("responses: apply_patch_call_output requires id, call_id, and status");
            }
            std::optional<std::string> output;
            if (const auto* ov = item->find("output"); ov != nullptr && !ov->is_null()) {
                if (!ov->is_string()) return invalid_argument("responses: apply_patch_call_output output must be a string");
                output = ov->as_string();
            }
            apply_patch_result_ = StreamApplyPatchResult{item_id_, out_idx, call_id, status, output};
            return events;
        }

        skipped_item_ = true;
        if (ikind == "function_call_output") {
            if (const auto* cid = item->find("call_id"); cid && cid->is_string()) skipped_call_id_ = cid->as_string();
        }
        losses_.push_back(unsupported_item_loss(out_idx, ikind));
        return events;
    }

    if (type == "response.content_part.added") {
        OXA_RETURN_IF_ERROR(require_active_item(chunk, "response.content_part.added"));
        if (function_call_.has_value()) {
            return invalid_argument("responses: response.content_part.added on function_call item");
        }
        if (block_open_ || skipped_part_) {
            return invalid_argument("responses: response.content_part.added with a part still open");
        }
        std::int64_t content_index = -1;
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) content_index = ci->as_int();
        if (content_index != next_content_index_) {
            return invalid_argument("responses: content_part.added content_index " + std::to_string(content_index) +
                                    ", want " + std::to_string(next_content_index_));
        }
        next_content_index_++;
        content_index_ = content_index;

        const auto* part = chunk.find("part");
        if (!part || !part->is_object()) return invalid_argument("responses: response.content_part.added without part");
        if (skipped_item_) {
            skipped_part_ = true;
            return events;
        }
        std::string ptype;
        if (const auto* pt = part->find("type"); pt && pt->is_string()) ptype = pt->as_string();
        if (ptype != "output_text") {
            skipped_part_ = true;
            losses_.push_back(make_resp_loss(
                "output[" + std::to_string(output_index_) + "].content[" + std::to_string(content_index) + "]",
                "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "Responses streaming content type \"" + ptype + "\" is not decoded in the Responses stream profile"));
            return events;
        }

        block_open_ = true;
        block_index_ = next_block_index_++;
        text_done_ = false;
        std::string txt;
        if (const auto* tv = part->find("text"); tv && tv->is_string()) txt = tv->as_string();
        events.push_back(ir::ContentBlockStart{block_index_, ir::TextBlock{std::move(txt)}});
        return events;
    }

    if (type == EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DELTA) {
        OXA_RETURN_IF_ERROR(require_custom_tool_call(chunk, type));
        if (custom_tool_call_->snapshot.has_value()) return invalid_argument("responses: custom tool input delta after input.done");
        const auto* delta = chunk.find("delta");
        if (!delta || !delta->is_string()) return invalid_argument("responses: custom tool input delta requires string delta");
        custom_tool_call_->fragments.push_back(delta->as_string());
        return events;
    }

    if (type == EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DONE) {
        OXA_RETURN_IF_ERROR(require_custom_tool_call(chunk, type));
        const auto* input = chunk.find("input");
        if (!input || !input->is_string()) return invalid_argument("responses: custom tool input.done requires string input");
        if (custom_tool_call_->snapshot.has_value() && *custom_tool_call_->snapshot != input->as_string())
            return invalid_argument("responses: conflicting custom tool input snapshots");
        custom_tool_call_->snapshot = input->as_string();
        return events;
    }

    if (type == "response.function_call_arguments.delta") {
        if (skipped_item_) {
            OXA_RETURN_IF_ERROR(require_active_item(chunk, "response.function_call_arguments.delta"));
            return events;
        }
        OXA_RETURN_IF_ERROR(require_function_call(chunk, "response.function_call_arguments.delta"));
        if (function_call_->arguments_done) {
            return invalid_argument("responses: response.function_call_arguments.delta after arguments.done");
        }
        std::string d;
        if (const auto* dv = chunk.find("delta"); dv && dv->is_string()) d = dv->as_string();
        function_call_->fragments.push_back(std::move(d));
        return events;
    }

    if (type == "response.function_call_arguments.done") {
        if (skipped_item_) {
            OXA_RETURN_IF_ERROR(require_active_item(chunk, "response.function_call_arguments.done"));
            return events;
        }
        OXA_RETURN_IF_ERROR(require_function_call(chunk, "response.function_call_arguments.done"));
        if (function_call_->arguments_done) {
            return invalid_argument("responses: duplicate response.function_call_arguments.done");
        }
        std::string call_id, name, args;
        if (const auto* cid = chunk.find("call_id"); cid && cid->is_string()) call_id = cid->as_string();
        if (const auto* nv = chunk.find("name"); nv && nv->is_string()) name = nv->as_string();
        if (const auto* av = chunk.find("arguments"); av && av->is_string()) args = av->as_string();
        std::string joined;
        for (const auto& f : function_call_->fragments) joined += f;
        if (call_id != function_call_->call_id || name != function_call_->name || args != joined) {
            return invalid_argument("responses: response.function_call_arguments.done does not match the active function call");
        }
        function_call_->arguments_done = true;
        return events;
    }

    if (type == "response.output_text.delta") {
        OXA_RETURN_IF_ERROR(require_active_item(chunk, "response.output_text.delta"));
        if (function_call_.has_value()) {
            return invalid_argument("responses: response.output_text.delta on function_call item");
        }
        std::int64_t content_index = -1;
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) content_index = ci->as_int();
        if (skipped_item_ || skipped_part_) {
            if (content_index != content_index_) {
                return invalid_argument("responses: output_text.delta content_index does not match the skipped part");
            }
            return events;
        }
        if (!block_open_ || content_index != content_index_) {
            return invalid_argument("responses: output_text.delta does not match the open content part");
        }
        if (text_done_) {
            return invalid_argument("responses: output_text.delta after output_text.done");
        }
        std::string d;
        if (const auto* dv = chunk.find("delta"); dv && dv->is_string()) d = dv->as_string();
        events.push_back(ir::ContentBlockDelta{block_index_, ir::TextDelta{std::move(d)}});
        return events;
    }

    if (type == "response.output_text.done") {
        OXA_RETURN_IF_ERROR(require_active_item(chunk, "response.output_text.done"));
        if (function_call_.has_value()) {
            return invalid_argument("responses: response.output_text.done on function_call item");
        }
        std::int64_t content_index = -1;
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) content_index = ci->as_int();
        if (skipped_item_ || skipped_part_) {
            if (content_index != content_index_) {
                return invalid_argument("responses: output_text.done content_index does not match the skipped part");
            }
            return events;
        }
        if (!block_open_ || content_index != content_index_) {
            return invalid_argument("responses: output_text.done does not match the open content part");
        }
        if (text_done_) {
            return invalid_argument("responses: duplicate output_text.done");
        }
        text_done_ = true;
        return events;
    }

    if (type == EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_ADDED) {
        OXA_RETURN_IF_ERROR(require_active_item(chunk, type));
        if (block_open_ || skipped_part_) {
            return invalid_argument("responses: reasoning_summary_part.added with a part still open");
        }
        std::int64_t content_index = -1;
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) content_index = ci->as_int();
        if (content_index != next_content_index_) {
            return invalid_argument("responses: reasoning_summary_part.added content_index " +
                                    std::to_string(content_index) + ", want " +
                                    std::to_string(next_content_index_));
        }
        next_content_index_++;
        content_index_ = content_index;
        const auto* part = chunk.find("part");
        if (!part || !part->is_object()) {
            return invalid_argument("responses: reasoning_summary_part.added without part");
        }
        if (skipped_item_) {
            skipped_part_ = true;
            return events;
        }
        std::string part_type;
        if (const auto* pt = part->find("type"); pt && pt->is_string()) part_type = pt->as_string();
        if (part_type != "output_text") {
            skipped_part_ = true;
            losses_.push_back(make_resp_loss(
                "output[" + std::to_string(output_index_) + "].summary[" +
                    std::to_string(content_index) + "]",
                "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "Responses reasoning summary type \"" + part_type + "\" is not decoded in the stream profile"));
            return events;
        }
        block_open_ = true;
        block_index_ = next_block_index_++;
        text_done_ = false;
        events.push_back(ir::ContentBlockStart{
            block_index_, ir::ThinkingBlock{"", std::nullopt}});
        return events;
    }

    if (type == EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DELTA) {
        OXA_RETURN_IF_ERROR(require_active_item(chunk, type));
        std::int64_t content_index = -1;
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) content_index = ci->as_int();
        if (skipped_item_ || skipped_part_) {
            if (content_index != content_index_) {
                return invalid_argument("responses: reasoning_summary_text.delta content_index does not match skipped part");
            }
            return events;
        }
        if (!block_open_ || content_index != content_index_) {
            return invalid_argument("responses: reasoning_summary_text.delta does not match open content part");
        }
        if (text_done_) return invalid_argument("responses: reasoning_summary_text.delta after text.done");
        std::string delta;
        if (const auto* d = chunk.find("delta"); d && d->is_string()) delta = d->as_string();
        events.push_back(ir::ContentBlockDelta{block_index_, ir::ThinkingDelta{std::move(delta)}});
        return events;
    }

    if (type == EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DONE) {
        OXA_RETURN_IF_ERROR(require_active_item(chunk, type));
        std::int64_t content_index = -1;
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) content_index = ci->as_int();
        if (skipped_item_ || skipped_part_) {
            if (content_index != content_index_) {
                return invalid_argument("responses: reasoning_summary_text.done content_index does not match skipped part");
            }
            return events;
        }
        if (!block_open_ || content_index != content_index_) {
            return invalid_argument("responses: reasoning_summary_text.done does not match open content part");
        }
        if (text_done_) return invalid_argument("responses: duplicate reasoning_summary_text.done");
        text_done_ = true;
        return events;
    }

    if (type == EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_DONE) {
        OXA_RETURN_IF_ERROR(require_active_item(chunk, type));
        if (!chunk.find("part")) {
            return invalid_argument("responses: reasoning_summary_part.done without part");
        }
        std::int64_t content_index = -1;
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) content_index = ci->as_int();
        if (skipped_item_ || skipped_part_) {
            if (content_index != content_index_) {
                return invalid_argument("responses: reasoning_summary_part.done content_index does not match skipped part");
            }
            skipped_part_ = false;
            return events;
        }
        if (!block_open_ || content_index != content_index_) {
            return invalid_argument("responses: reasoning_summary_part.done does not match open content part");
        }
        block_open_ = false;
        events.push_back(ir::ContentBlockStop{block_index_});
        return events;
    }

    if (type == "response.content_part.done") {
        OXA_RETURN_IF_ERROR(require_active_item(chunk, "response.content_part.done"));
        if (function_call_.has_value()) {
            return invalid_argument("responses: response.content_part.done on function_call item");
        }
        if (!chunk.find("part")) {
            return invalid_argument("responses: response.content_part.done without part");
        }
        std::int64_t content_index = -1;
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) content_index = ci->as_int();
        if (skipped_item_ || skipped_part_) {
            if (content_index != content_index_) {
                return invalid_argument("responses: content_part.done content_index does not match the skipped part");
            }
            skipped_part_ = false;
            return events;
        }
        if (!block_open_ || content_index != content_index_) {
            return invalid_argument("responses: content_part.done does not match the open content part");
        }
        if (!text_done_) {
            return invalid_argument("responses: content_part.done before output_text.done");
        }
        block_open_ = false;
        events.push_back(ir::ContentBlockStop{block_index_});
        return events;
    }

    if (type == "response.output_item.done") {
        OXA_RETURN_IF_ERROR(require_started("response.output_item.done"));
        std::int64_t out_idx = -1;
        if (const auto* oi = chunk.find("output_index"); oi && oi->is_int()) out_idx = oi->as_int();
        if (!item_open_ || out_idx != output_index_) {
            return invalid_argument("responses: response.output_item.done does not match the open item");
        }
        const auto* item = chunk.find("item");
        if (!item || !item->is_object()) {
            return invalid_argument("responses: response.output_item.done does not match the open item");
        }
        std::string i_id, i_type;
        if (const auto* idv = item->find("id"); idv && idv->is_string()) i_id = idv->as_string();
        if (const auto* tv = item->find("type"); tv && tv->is_string()) i_type = tv->as_string();
        if (i_id != item_id_ || i_type != item_type_) {
            return invalid_argument("responses: response.output_item.done does not match the open item");
        }
        if (skipped_item_ && item_type_ == "function_call_output") {
            std::string cid;
            if (const auto* cv = item->find("call_id"); cv && cv->is_string()) cid = cv->as_string();
            if (cid != skipped_call_id_) {
                return invalid_argument("responses: response.output_item.done does not match the active function_call_output");
            }
        }
        if (block_open_ || skipped_part_) {
            return invalid_argument("responses: response.output_item.done with a content part still open");
        }

        if (function_call_.has_value()) {
            std::string joined;
            for (const auto& f : function_call_->fragments) joined += f;
            std::string cid, name, args;
            if (const auto* cv = item->find("call_id"); cv && cv->is_string()) cid = cv->as_string();
            if (const auto* nv = item->find("name"); nv && nv->is_string()) name = nv->as_string();
            if (const auto* av = item->find("arguments"); av && av->is_string()) args = av->as_string();
            if (cid != function_call_->call_id || name != function_call_->name || args != joined) {
                return invalid_argument("responses: response.output_item.done does not match the active function call");
            }
            OXA_ASSIGN_OR_RETURN(events, replay_function_call(*function_call_));
            tool_use_seen_ = true;
            function_call_ = std::nullopt;
        }
        if (custom_tool_call_.has_value()) {
            const auto* cid = item->find("call_id"); const auto* name = item->find("name"); const auto* input = item->find("input");
            if (!cid || !cid->is_string() || !name || !name->is_string() || !input || !input->is_string() ||
                cid->as_string() != custom_tool_call_->call_id || name->as_string() != custom_tool_call_->name)
                return invalid_argument("responses: custom_tool_call.done identity does not match added item");
            if (custom_tool_call_->snapshot.has_value() && *custom_tool_call_->snapshot != input->as_string())
                return invalid_argument("responses: custom tool input snapshots conflict");
            std::string joined;
            for (const auto& fragment : custom_tool_call_->fragments) joined += fragment;
            if (!custom_tool_call_->fragments.empty() && joined != input->as_string())
                return invalid_argument("responses: custom tool input does not match delta fragments");
            custom_tool_call_->snapshot = input->as_string();
            OXA_ASSIGN_OR_RETURN(events, replay_custom_tool_call(*custom_tool_call_));
            tool_use_seen_ = true;
            custom_tool_call_ = std::nullopt;
        }
        if (apply_patch_call_.has_value()) {
            std::string cid;
            if (const auto* cv = item->find("call_id"); cv && cv->is_string()) cid = cv->as_string();
            if (cid != apply_patch_call_->call_id) {
                return invalid_argument("responses: response.output_item.done does not match the active apply patch call");
            }
            std::vector<ir::Loss> item_losses;
            OXA_ASSIGN_OR_RETURN(auto call_status, decode_apply_patch_call_status(item->find("status"), "output[" + std::to_string(output_index_) + "]", item_losses));
            OXA_ASSIGN_OR_RETURN(auto op, decode_apply_patch_operation(item->find("operation"), "output[" + std::to_string(output_index_) + "].operation", item_losses));
            if (!item_losses.empty()) {
                losses_.insert(losses_.end(), item_losses.begin(), item_losses.end());
            } else if (call_status.has_value() && op.has_value()) {
                std::int64_t b_idx = next_block_index_++;
                events.push_back(ir::ContentBlockStart{b_idx, ir::ApplyPatchToolUseBlock{cid, *call_status, *op}});
                events.push_back(ir::ContentBlockStop{b_idx});
                tool_use_seen_ = true;
            }
            const auto* st_v = item->find("status");
            const auto* op_v = item->find("operation");
            completed_apply_patch_calls_.push_back(CompletedApplyPatchCall{
                item_id_, cid, st_v && st_v->is_string() ? st_v->as_string() : "",
                op_v ? *op_v : json::Value::null()
            });
            apply_patch_call_ = std::nullopt;
        }
        if (apply_patch_result_.has_value()) {
            std::string cid;
            if (const auto* cv = item->find("call_id"); cv && cv->is_string()) cid = cv->as_string();
            if (cid != apply_patch_result_->call_id) {
                return invalid_argument("responses: response.output_item.done does not match the active apply patch result");
            }
            std::vector<ir::Loss> item_losses;
            OXA_ASSIGN_OR_RETURN(auto res_status, decode_apply_patch_result_status(item->find("status"), "output[" + std::to_string(output_index_) + "]", item_losses));
            std::optional<std::string> output;
            if (const auto* ov = item->find("output"); ov != nullptr && !ov->is_null()) {
                if (!ov->is_string()) return invalid_argument("responses: apply_patch_call_output output must be a string");
                output = ov->as_string();
            }
            if (!item_losses.empty()) {
                losses_.insert(losses_.end(), item_losses.begin(), item_losses.end());
            } else if (res_status.has_value()) {
                std::int64_t b_idx = next_block_index_++;
                events.push_back(ir::ContentBlockStart{b_idx, ir::ApplyPatchToolResultBlock{cid, *res_status, output}});
                events.push_back(ir::ContentBlockStop{b_idx});
            }
            const auto* st_v = item->find("status");
            completed_apply_patch_results_.push_back(CompletedApplyPatchResult{
                item_id_, cid, st_v && st_v->is_string() ? st_v->as_string() : "", output
            });
            apply_patch_result_ = std::nullopt;
        }

        item_open_ = false;
        item_type_.clear();
        item_id_.clear();
        skipped_call_id_.clear();
        skipped_item_ = false;
        return events;
    }

    if (type == "response.completed" || type == "response.incomplete" || type == "response.failed") {
        OXA_RETURN_IF_ERROR(require_started(type));
        if (item_open_ || block_open_ || skipped_part_) {
            return invalid_argument("responses: " + type + " before output lifecycle completed");
        }
        const auto* resp = chunk.find("response");
        if (!resp || !resp->is_object()) return invalid_argument("responses: " + type + " without response");

        if (const auto* out_arr = resp->find("output"); out_arr && out_arr->is_array()) {
            for (const auto& it : out_arr->as_array()) {
                const auto* it_type = it.find("type");
                std::string it_kind = it_type && it_type->is_string() ? it_type->as_string() : "";
                if (it_kind == "apply_patch_call") {
                    std::string it_id = it.find("id") && it.find("id")->is_string() ? it.find("id")->as_string() : "";
                    auto prior = std::find_if(completed_apply_patch_calls_.begin(), completed_apply_patch_calls_.end(),
                        [&](const CompletedApplyPatchCall& p) { return p.id == it_id; });
                    if (prior == completed_apply_patch_calls_.end())
                        return invalid_argument("responses: terminal apply patch snapshot mismatch");
                    std::string cid = it.find("call_id") && it.find("call_id")->is_string() ? it.find("call_id")->as_string() : "";
                    std::string st = it.find("status") && it.find("status")->is_string() ? it.find("status")->as_string() : "";
                    if (cid != prior->call_id || st != prior->status)
                        return invalid_argument("responses: terminal apply patch snapshot mismatch");
                    const auto* op = it.find("operation");
                    if (!op || !op->is_object())
                        return invalid_argument("responses: terminal apply patch operation mismatch");
                    const auto* opt1 = op->find("type");
                    const auto* opt2 = prior->operation.find("type");
                    if (!opt1 || !opt2 || opt1->as_string() != opt2->as_string())
                        return invalid_argument("responses: terminal apply patch operation mismatch");
                    if (opt1->as_string() != "create_file" && opt1->as_string() != "update_file" && opt1->as_string() != "delete_file") {
                        if (!json::structurally_equal(*op, prior->operation))
                            return invalid_argument("responses: terminal apply patch operation mismatch");
                    } else {
                        const auto* opp1 = op->find("path");
                        const auto* opp2 = prior->operation.find("path");
                        if ((opp1 == nullptr) != (opp2 == nullptr))
                            return invalid_argument("responses: terminal apply patch operation mismatch");
                        if (opp1 && opp1->as_string() != opp2->as_string())
                            return invalid_argument("responses: terminal apply patch operation mismatch");
                        if (opt1->as_string() != "delete_file") {
                            const auto* opd1 = op->find("diff");
                            const auto* opd2 = prior->operation.find("diff");
                            if ((opd1 == nullptr) != (opd2 == nullptr))
                                return invalid_argument("responses: terminal apply patch diff mismatch");
                            if (opd1 && opd1->as_string() != opd2->as_string())
                                return invalid_argument("responses: terminal apply patch diff mismatch");
                        }
                    }
                } else if (it_kind == "apply_patch_call_output") {
                    std::string it_id = it.find("id") && it.find("id")->is_string() ? it.find("id")->as_string() : "";
                    auto prior = std::find_if(completed_apply_patch_results_.begin(), completed_apply_patch_results_.end(),
                        [&](const CompletedApplyPatchResult& p) { return p.id == it_id; });
                    if (prior == completed_apply_patch_results_.end())
                        return invalid_argument("responses: terminal apply patch snapshot mismatch");
                    std::string cid = it.find("call_id") && it.find("call_id")->is_string() ? it.find("call_id")->as_string() : "";
                    std::string st = it.find("status") && it.find("status")->is_string() ? it.find("status")->as_string() : "";
                    if (cid != prior->call_id || st != prior->status)
                        return invalid_argument("responses: terminal apply patch snapshot mismatch");
                    std::optional<std::string> output;
                    if (const auto* ov = it.find("output"); ov && ov->is_string()) output = ov->as_string();
                    if (output != prior->output)
                        return invalid_argument("responses: terminal apply patch output mismatch");
                }
            }
        }

        auto [stop_reason, s_losses] = decode_status(*resp, tool_use_seen_);
        losses_.insert(losses_.end(), s_losses.begin(), s_losses.end());
        terminated_ = true;

        ir::Usage usage;
        if (const auto* uv = resp->find("usage"); uv && uv->is_object()) {
            if (const auto* in = uv->find("input_tokens"); in && in->is_int()) usage.input_tokens = in->as_int();
            if (const auto* out = uv->find("output_tokens"); out && out->is_int()) usage.output_tokens = out->as_int();
            if (const auto* details = uv->find("input_token_details"); details && details->is_object()) {
                if (const auto* cached = details->find("cached_tokens"); cached && cached->is_int()) {
                    usage.input_tokens_details = ir::InputTokensDetails{cached->as_int()};
                }
            }
            if (const auto* details = uv->find("output_token_details"); details && details->is_object()) {
                if (const auto* reasoning = details->find("reasoning_tokens"); reasoning && reasoning->is_int()) {
                    usage.output_tokens_details = ir::OutputTokensDetails{reasoning->as_int()};
                }
            }
        }

        events.push_back(ir::MessageDelta{stop_reason, std::nullopt, usage});
        events.push_back(ir::MessageDone{});
        return events;
    }

    // Unknown event types: absorb identity-matching descendants of an active
    // skipped unit (N-S-3); validate identity against the open supported unit
    // otherwise; keep at most one loss per event.
    const bool has_identity = chunk.find("output_index") != nullptr ||
                              chunk.find("item_id") != nullptr ||
                              chunk.find("content_index") != nullptr;
    auto content_index_of = [&chunk]() -> std::int64_t {
        if (const auto* ci = chunk.find("content_index"); ci && ci->is_int()) return ci->as_int();
        return -1;
    };
    if (skipped_item_ || skipped_part_) {
        if (has_identity) {
            if (auto st = require_active_item(chunk, type); !st.ok()) return st;
            if (skipped_part_ && !skipped_item_) {
                const std::int64_t content_index = content_index_of();
                if (content_index != content_index_)
                    return invalid_argument("responses: " + type +
                                            " does not match the skipped content part");
            }
            return events;  // absorbed
        }
    } else if (has_identity) {
        if (auto st = require_active_item(chunk, type); !st.ok()) return st;
        if (block_open_) {
            if (content_index_of() != content_index_)
                return invalid_argument("responses: " + type +
                                        " does not match the open content part");
        } else if (chunk.find("content_index") != nullptr) {
            return invalid_argument("responses: " + type + " has no open content part");
        }
    }

    losses_.push_back(make_resp_loss(
        "type", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
        "Responses stream event type \"" + type + "\" is not decoded in the Responses stream profile"));
    return events;
}

StatusOr<std::vector<ir::Event>> StreamDecoder::flush() {
    if (flushed_) return invalid_argument("responses: stream flushed twice");
    if (!terminated_) return invalid_argument("responses: stream ended without a terminal response event");
    flushed_ = true;
    return std::vector<ir::Event>{};
}

// ---- StreamEncoder --------------------------------------------------------

static std::string stream_generated_item_id(std::string_view prefix, std::int64_t ordinal) {
    char buf[64];
    std::snprintf(buf, sizeof(buf), "%.*s_abc%03lld", static_cast<int>(prefix.size()), prefix.data(),
                  static_cast<long long>(123 + 333 * ordinal));
    return std::string(buf);
}

StreamEncoder::StreamEncoder(Options opts) : opts_(std::move(opts)) {}

std::pair<StreamEncoder::StreamOutputItem, json::Value> StreamEncoder::open_message_item() {
    std::string item_id = stream_generated_item_id("msg", next_message_item_++);
    std::int64_t output_index = next_output_index_++;
    StreamOutputItem item;
    item.kind = OutputItemKind::Message;
    item.id = item_id;
    item.output_index = output_index;

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.added"));
    event.set("output_index", json::Value::integer(output_index));
    json::Value i = json::Value::object();
    i.set("id", json::Value::string(item_id));
    i.set("type", json::Value::string("message"));
    i.set("status", json::Value::string("in_progress"));
    i.set("role", json::Value::string("assistant"));
    event.set("item", std::move(i));

    return {std::move(item), std::move(event)};
}

std::pair<StreamEncoder::StreamOutputItem, json::Value> StreamEncoder::open_reasoning_item() {
    std::string item_id = stream_generated_item_id("rs", next_reasoning_item_++);
    std::int64_t output_index = next_output_index_++;
    StreamOutputItem item;
    item.kind = OutputItemKind::Reasoning;
    item.id = item_id;
    item.output_index = output_index;

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.added"));
    event.set("output_index", json::Value::integer(output_index));
    json::Value reasoning = json::Value::object();
    reasoning.set("id", json::Value::string(item_id));
    reasoning.set("type", json::Value::string(std::string(ITEM_TYPE_REASONING)));
    reasoning.set("status", json::Value::string("in_progress"));
    event.set("item", std::move(reasoning));
    return {std::move(item), std::move(event)};
}

std::pair<StreamEncoder::StreamOutputItem, json::Value> StreamEncoder::open_function_call_item(
    std::string_view call_id, std::string_view name) {
    std::string item_id = stream_generated_item_id("fc", next_function_item_++);
    std::int64_t output_index = next_output_index_++;
    StreamOutputItem item;
    item.kind = OutputItemKind::FunctionCall;
    item.id = item_id;
    item.output_index = output_index;
    item.call_id = std::string(call_id);
    item.name = std::string(name);

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.added"));
    event.set("output_index", json::Value::integer(output_index));
    json::Value i = json::Value::object();
    i.set("id", json::Value::string(item_id));
    i.set("type", json::Value::string("function_call"));
    i.set("status", json::Value::string("in_progress"));
    i.set("call_id", json::Value::string(std::string(call_id)));
    i.set("name", json::Value::string(std::string(name)));
    event.set("item", std::move(i));

    return {std::move(item), std::move(event)};
}

std::pair<StreamEncoder::StreamOutputItem, json::Value> StreamEncoder::open_custom_tool_call_item(
    std::string_view call_id, std::string_view name) {
    std::string item_id = "ctc_" + std::to_string(next_custom_item_++);
    std::int64_t output_index = next_output_index_++;
    StreamOutputItem item; item.kind = OutputItemKind::CustomToolCall; item.id = item_id;
    item.output_index = output_index; item.call_id = std::string(call_id); item.name = std::string(name);
    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.added"));
    event.set("output_index", json::Value::integer(output_index));
    json::Value wire_item = json::Value::object();
    wire_item.set("id", json::Value::string(item_id));
    wire_item.set("type", json::Value::string("custom_tool_call"));
    wire_item.set("call_id", json::Value::string(std::string(call_id)));
    wire_item.set("name", json::Value::string(std::string(name)));
    wire_item.set("input", json::Value::string(""));
    event.set("item", std::move(wire_item));
    return {std::move(item), std::move(event)};
}

json::Value StreamEncoder::close_reasoning_item() {
    auto item = std::move(*active_item_);
    active_item_ = std::nullopt;
    json::Value completed = json::Value::object();
    completed.set("id", json::Value::string(item.id));
    completed.set("type", json::Value::string(std::string(ITEM_TYPE_REASONING)));
    completed.set("status", json::Value::string("completed"));
    json::Value summary = json::Value::array();
    for (auto& part : item.content) summary.push_back(std::move(part));
    completed.set("summary", std::move(summary));

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.done"));
    event.set("output_index", json::Value::integer(item.output_index));
    event.set("item", completed);
    completed_.push_back(std::move(completed));
    return event;
}

json::Value StreamEncoder::close_message_item() {
    auto item = std::move(*active_item_);
    active_item_ = std::nullopt;

    json::Value completed = json::Value::object();
    completed.set("id", json::Value::string(item.id));
    completed.set("type", json::Value::string("message"));
    completed.set("status", json::Value::string("completed"));
    completed.set("role", json::Value::string("assistant"));
    json::Value content_arr = json::Value::array();
    for (auto& c : item.content) {
        content_arr.push_back(std::move(c));
    }
    completed.set("content", std::move(content_arr));

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.done"));
    event.set("output_index", json::Value::integer(item.output_index));
    event.set("item", completed);

    completed_.push_back(std::move(completed));
    return event;
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::start_text_block(
    std::int64_t index, const ir::TextBlock& block) {
    std::vector<json::Value> out;
    if (active_item_.has_value()) {
        if (active_item_->kind == OutputItemKind::Reasoning) {
            out.push_back(close_reasoning_item());
        } else if (active_item_->kind != OutputItemKind::Message) {
            return invalid_argument("responses: TextBlock cannot open before the active function_call item completes");
        }
    }
    if (!active_item_.has_value()) {
        auto [item, added] = open_message_item();
        active_item_ = std::move(item);
        out.push_back(std::move(added));
    }

    std::int64_t content_index = active_item_->next_content_index++;
    json::Value part = json::Value::object();
    part.set("type", json::Value::string("output_text"));
    part.set("text", json::Value::string(block.text));
    part.set("annotations", json::Value::array());
    active_item_->content.push_back(part);

    StreamEncodeBlock eb;
    eb.index = index;
    eb.kind = OutputItemKind::Message;
    eb.content_index = content_index;
    eb.text = block.text;
    active_block_ = std::move(eb);

    json::Value added_part = json::Value::object();
    added_part.set("type", json::Value::string("response.content_part.added"));
    added_part.set("item_id", json::Value::string(active_item_->id));
    added_part.set("output_index", json::Value::integer(active_item_->output_index));
    added_part.set("content_index", json::Value::integer(content_index));
    added_part.set("part", std::move(part));
    out.push_back(std::move(added_part));

    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::start_thinking_block(
    std::int64_t index, const ir::ThinkingBlock& block) {
    std::vector<json::Value> out;
    if (active_item_.has_value()) {
        if (active_item_->kind == OutputItemKind::Message) {
            out.push_back(close_message_item());
        } else if (active_item_->kind != OutputItemKind::Reasoning) {
            return invalid_argument("responses: ThinkingBlock cannot open before the active function_call item completes");
        }
    }
    if (!active_item_.has_value()) {
        auto [item, added] = open_reasoning_item();
        active_item_ = std::move(item);
        out.push_back(std::move(added));
    }
    std::int64_t content_index = active_item_->next_content_index++;
    json::Value part = json::Value::object();
    part.set("type", json::Value::string(std::string(PART_TYPE_OUTPUT_TEXT)));
    part.set("text", json::Value::string(block.thinking));
    part.set("annotations", json::Value::array());
    active_item_->content.push_back(part);

    StreamEncodeBlock encoded;
    encoded.index = index;
    encoded.kind = OutputItemKind::Reasoning;
    encoded.content_index = content_index;
    encoded.text = block.thinking;
    encoded.signature = block.signature.value_or("");
    active_block_ = std::move(encoded);

    json::Value part_added = json::Value::object();
    part_added.set("type", json::Value::string(std::string(EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_ADDED)));
    part_added.set("item_id", json::Value::string(active_item_->id));
    part_added.set("output_index", json::Value::integer(active_item_->output_index));
    part_added.set("content_index", json::Value::integer(content_index));
    part_added.set("part", std::move(part));
    out.push_back(std::move(part_added));
    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::start_function_call_block(
    std::int64_t index, const ir::ToolUseBlock& block) {
    if (block.id.empty() || block.name.empty()) {
        return invalid_argument("responses: ToolUseBlock requires nonempty ID and name");
    }
    std::vector<json::Value> out;
    if (active_item_.has_value()) {
        if (active_item_->kind == OutputItemKind::Message) {
            out.push_back(close_message_item());
        } else if (active_item_->kind == OutputItemKind::Reasoning) {
            out.push_back(close_reasoning_item());
        } else {
            return invalid_argument("responses: ToolUseBlock cannot open before the active function_call item completes");
        }
    }
    auto [item, added] = open_function_call_item(block.id, block.name);
    active_item_ = std::move(item);

    StreamEncodeBlock eb;
    eb.index = index;
    eb.kind = OutputItemKind::FunctionCall;
    eb.tool_input = block.input;
    active_block_ = std::move(eb);

    out.push_back(std::move(added));
    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::start_custom_tool_call_block(
    std::int64_t index, const ir::CustomToolUseBlock& block) {
    if (block.id.empty() || block.name.empty()) return invalid_argument("responses: CustomToolUseBlock requires nonempty ID and name");
    std::vector<json::Value> out;
    if (active_item_.has_value()) {
        if (active_item_->kind == OutputItemKind::Message) out.push_back(close_message_item());
        else if (active_item_->kind == OutputItemKind::Reasoning) out.push_back(close_reasoning_item());
        else return invalid_argument("responses: custom tool call cannot open before the active call completes");
    }
    auto [item, added] = open_custom_tool_call_item(block.id, block.name);
    active_item_ = std::move(item);
    StreamEncodeBlock eb; eb.index = index; eb.kind = OutputItemKind::CustomToolCall; eb.tool_input = block.input;
    active_block_ = std::move(eb); out.push_back(std::move(added));
    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::stop_text_block() {
    if (!active_item_.has_value() || active_item_->kind != OutputItemKind::Message) {
        return invalid_argument("responses: text block without an active message item");
    }
    auto block = std::move(*active_block_);
    active_block_ = std::nullopt;

    json::Value part = json::Value::object();
    part.set("type", json::Value::string("output_text"));
    part.set("text", json::Value::string(block.text));
    part.set("annotations", json::Value::array());
    active_item_->content[block.content_index] = part;

    std::string item_id = active_item_->id;
    std::int64_t output_index = active_item_->output_index;

    std::vector<json::Value> out;
    json::Value text_done = json::Value::object();
    text_done.set("type", json::Value::string("response.output_text.done"));
    text_done.set("item_id", json::Value::string(item_id));
    text_done.set("output_index", json::Value::integer(output_index));
    text_done.set("content_index", json::Value::integer(block.content_index));
    text_done.set("text", json::Value::string(block.text));
    out.push_back(std::move(text_done));

    json::Value part_done = json::Value::object();
    part_done.set("type", json::Value::string("response.content_part.done"));
    part_done.set("item_id", json::Value::string(item_id));
    part_done.set("output_index", json::Value::integer(output_index));
    part_done.set("content_index", json::Value::integer(block.content_index));
    part_done.set("part", std::move(part));
    out.push_back(std::move(part_done));

    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::stop_thinking_block() {
    if (!active_item_.has_value() || active_item_->kind != OutputItemKind::Reasoning) {
        return invalid_argument("responses: thinking block without an active reasoning item");
    }
    auto block = std::move(*active_block_);
    active_block_ = std::nullopt;
    const std::size_t content_index = static_cast<std::size_t>(block.content_index);
    active_item_->content[content_index].set("text", json::Value::string(block.text));

    json::Value part_done = json::Value::object();
    part_done.set("type", json::Value::string(std::string(EVENT_TYPE_RESPONSE_REASONING_SUMMARY_PART_DONE)));
    part_done.set("item_id", json::Value::string(active_item_->id));
    part_done.set("output_index", json::Value::integer(active_item_->output_index));
    part_done.set("content_index", json::Value::integer(block.content_index));
    part_done.set("part", active_item_->content[content_index]);

    std::vector<ir::Loss> losses;
    if (!block.signature.empty() && !block.signature_seen) {
        losses.push_back(make_resp_loss(
            "events[" + std::to_string(block.index) + "].block.signature", "signature",
            ir::LOSS_UNMAPPED_FIELD,
            "Responses carries no signature in reasoning summary"));
    }
    return std::make_pair(std::vector<json::Value>{std::move(part_done)}, std::move(losses));
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::stop_function_call_block() {
    if (!active_item_.has_value() || active_item_->kind != OutputItemKind::FunctionCall) {
        return invalid_argument("responses: tool block without an active function_call item");
    }
    auto block = std::move(*active_block_);
    active_block_ = std::nullopt;

    std::vector<json::Value> out;
    if (block.fragments.empty()) {
        block.fragments.push_back(block.tool_input);
        json::Value delta = json::Value::object();
        delta.set("type", json::Value::string("response.function_call_arguments.delta"));
        delta.set("item_id", json::Value::string(active_item_->id));
        delta.set("output_index", json::Value::integer(active_item_->output_index));
        delta.set("delta", json::Value::string(block.tool_input));
        out.push_back(std::move(delta));
    }
    std::string arguments;
    for (const auto& f : block.fragments) {
        arguments += f;
    }
    if (arguments != block.tool_input) {
        return invalid_argument("responses: ToolUseBlock input does not equal concatenated InputJSONDelta fragments");
    }

    json::Value completed = json::Value::object();
    completed.set("id", json::Value::string(active_item_->id));
    completed.set("type", json::Value::string("function_call"));
    completed.set("status", json::Value::string("completed"));
    completed.set("call_id", json::Value::string(active_item_->call_id));
    completed.set("name", json::Value::string(active_item_->name));
    completed.set("arguments", json::Value::string(arguments));

    json::Value args_done = json::Value::object();
    args_done.set("type", json::Value::string("response.function_call_arguments.done"));
    args_done.set("item_id", json::Value::string(active_item_->id));
    args_done.set("output_index", json::Value::integer(active_item_->output_index));
    args_done.set("call_id", json::Value::string(active_item_->call_id));
    args_done.set("name", json::Value::string(active_item_->name));
    args_done.set("arguments", json::Value::string(arguments));
    out.push_back(std::move(args_done));

    json::Value item_done = json::Value::object();
    item_done.set("type", json::Value::string("response.output_item.done"));
    item_done.set("output_index", json::Value::integer(active_item_->output_index));
    item_done.set("item", completed);
    out.push_back(std::move(item_done));

    completed_.push_back(std::move(completed));
    active_item_ = std::nullopt;
    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::stop_custom_tool_call_block() {
    if (!active_item_.has_value() || active_item_->kind != OutputItemKind::CustomToolCall)
        return invalid_argument("responses: custom tool block without an active custom_tool_call item");
    auto block = std::move(*active_block_); active_block_ = std::nullopt;
    std::vector<json::Value> out;
    if (block.fragments.empty()) {
        block.fragments.push_back(block.tool_input);
        json::Value delta = json::Value::object();
        delta.set("type", json::Value::string(std::string(EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DELTA)));
        delta.set("item_id", json::Value::string(active_item_->id));
        delta.set("output_index", json::Value::integer(active_item_->output_index));
        delta.set("delta", json::Value::string(block.tool_input));
        out.push_back(std::move(delta));
    }
    std::string input;
    for (const auto& fragment : block.fragments) input += fragment;
    if (input != block.tool_input) return invalid_argument("responses: CustomToolUseBlock input does not equal custom input delta fragments");
    json::Value snapshot = json::Value::object();
    snapshot.set("type", json::Value::string(std::string(EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DONE)));
    snapshot.set("item_id", json::Value::string(active_item_->id));
    snapshot.set("output_index", json::Value::integer(active_item_->output_index));
    snapshot.set("input", json::Value::string(input)); out.push_back(std::move(snapshot));
    json::Value completed = json::Value::object();
    completed.set("id", json::Value::string(active_item_->id));
    completed.set("type", json::Value::string("custom_tool_call"));
    completed.set("call_id", json::Value::string(active_item_->call_id));
    completed.set("name", json::Value::string(active_item_->name));
    completed.set("input", json::Value::string(input));
    json::Value done = json::Value::object();
    done.set("type", json::Value::string("response.output_item.done"));
    done.set("output_index", json::Value::integer(active_item_->output_index));
    done.set("item", completed); out.push_back(std::move(done));
    completed_.push_back(std::move(completed)); active_item_ = std::nullopt;
    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::start_apply_patch_call_block(
    std::int64_t index, const ir::ApplyPatchToolUseBlock& block) {
    if (block.id.empty()) {
        return invalid_argument("responses: ApplyPatchToolUseBlock requires nonempty ID");
    }
    std::vector<json::Value> out;
    if (active_item_.has_value()) {
        if (active_item_->kind == OutputItemKind::Message) {
            out.push_back(close_message_item());
        } else if (active_item_->kind == OutputItemKind::Reasoning) {
            out.push_back(close_reasoning_item());
        } else {
            return invalid_argument("responses: ApplyPatchToolUseBlock cannot open before active item completes");
        }
    }
    std::string item_id = "apc_" + std::to_string(next_apply_patch_call_item_++);
    std::int64_t output_index = next_output_index_++;
    StreamOutputItem item;
    item.kind = OutputItemKind::ApplyPatchCall;
    item.id = item_id;
    item.output_index = output_index;
    item.call_id = block.id;
    active_item_ = std::move(item);

    StreamEncodeBlock eb;
    eb.index = index;
    eb.kind = OutputItemKind::ApplyPatchCall;
    active_block_ = std::move(eb);
    active_apply_patch_block_ = block;

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.added"));
    event.set("output_index", json::Value::integer(output_index));
    json::Value wire = json::Value::object();
    wire.set("id", json::Value::string(item_id));
    wire.set("type", json::Value::string("apply_patch_call"));
    wire.set("call_id", json::Value::string(block.id));
    wire.set("status", json::Value::string("in_progress"));
    wire.set("operation", encode_apply_patch_operation(block.operation));
    event.set("item", std::move(wire));
    out.push_back(std::move(event));
    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::stop_apply_patch_call_block() {
    if (!active_item_.has_value() || active_item_->kind != OutputItemKind::ApplyPatchCall) {
        return invalid_argument("responses: apply_patch_call stop without active item");
    }
    const auto* ap = std::get_if<ir::ApplyPatchToolUseBlock>(&*active_apply_patch_block_);
    if (!ap) return invalid_argument("responses: missing active apply_patch_call block");

    active_block_ = std::nullopt;
    json::Value completed = json::Value::object();
    completed.set("id", json::Value::string(active_item_->id));
    completed.set("type", json::Value::string("apply_patch_call"));
    completed.set("call_id", json::Value::string(ap->id));
    completed.set("status", json::Value::string(ap->status));
    completed.set("operation", encode_apply_patch_operation(ap->operation));

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.done"));
    event.set("output_index", json::Value::integer(active_item_->output_index));
    event.set("item", completed);

    completed_.push_back(std::move(completed));
    active_item_ = std::nullopt;
    active_apply_patch_block_ = std::nullopt;
    return std::make_pair(std::vector<json::Value>{std::move(event)}, std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::start_apply_patch_result_block(
    std::int64_t index, const ir::ApplyPatchToolResultBlock& block) {
    if (block.tool_use_id.empty()) {
        return invalid_argument("responses: ApplyPatchToolResultBlock requires nonempty tool_use_id");
    }
    std::vector<json::Value> out;
    if (active_item_.has_value()) {
        if (active_item_->kind == OutputItemKind::Message) {
            out.push_back(close_message_item());
        } else if (active_item_->kind == OutputItemKind::Reasoning) {
            out.push_back(close_reasoning_item());
        } else {
            return invalid_argument("responses: ApplyPatchToolResultBlock cannot open before active item completes");
        }
    }
    std::string item_id = "apco_" + std::to_string(next_apply_patch_result_item_++);
    std::int64_t output_index = next_output_index_++;
    StreamOutputItem item;
    item.kind = OutputItemKind::ApplyPatchResult;
    item.id = item_id;
    item.output_index = output_index;
    item.call_id = block.tool_use_id;
    active_item_ = std::move(item);

    StreamEncodeBlock eb;
    eb.index = index;
    eb.kind = OutputItemKind::ApplyPatchResult;
    active_block_ = std::move(eb);
    active_apply_patch_block_ = block;

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.added"));
    event.set("output_index", json::Value::integer(output_index));
    json::Value wire = json::Value::object();
    wire.set("id", json::Value::string(item_id));
    wire.set("type", json::Value::string("apply_patch_call_output"));
    wire.set("call_id", json::Value::string(block.tool_use_id));
    wire.set("status", json::Value::string(block.status));
    if (block.output.has_value()) {
        wire.set("output", json::Value::string(*block.output));
    }
    event.set("item", std::move(wire));
    out.push_back(std::move(event));
    return std::make_pair(std::move(out), std::vector<ir::Loss>{});
}

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::stop_apply_patch_result_block() {
    if (!active_item_.has_value() || active_item_->kind != OutputItemKind::ApplyPatchResult) {
        return invalid_argument("responses: apply_patch_call_output stop without active item");
    }
    const auto* apr = std::get_if<ir::ApplyPatchToolResultBlock>(&*active_apply_patch_block_);
    if (!apr) return invalid_argument("responses: missing active apply_patch_call_output block");

    active_block_ = std::nullopt;
    json::Value completed = json::Value::object();
    completed.set("id", json::Value::string(active_item_->id));
    completed.set("type", json::Value::string("apply_patch_call_output"));
    completed.set("call_id", json::Value::string(apr->tool_use_id));
    completed.set("status", json::Value::string(apr->status));
    if (apr->output.has_value()) {
        completed.set("output", json::Value::string(*apr->output));
    }

    json::Value event = json::Value::object();
    event.set("type", json::Value::string("response.output_item.done"));
    event.set("output_index", json::Value::integer(active_item_->output_index));
    event.set("item", completed);

    completed_.push_back(std::move(completed));
    active_item_ = std::nullopt;
    active_apply_patch_block_ = std::nullopt;
    return std::make_pair(std::vector<json::Value>{std::move(event)}, std::vector<ir::Loss>{});
}

StatusOr<std::pair<json::Value, std::vector<ir::Loss>>> StreamEncoder::terminal(const ir::MessageDelta& delta) {
    json::Value resp = json::Value::object();
    resp.set("id", json::Value::string(id_));
    resp.set("object", json::Value::string("response"));
    resp.set("model", json::Value::string(model_));

    json::Value output_arr = json::Value::array();
    for (const auto& item : completed_) {
        output_arr.push_back(item);
    }
    resp.set("output", std::move(output_arr));

    json::Value usage = json::Value::object();
    usage.set("input_tokens", json::Value::integer(delta.usage.input_tokens));
    usage.set("output_tokens", json::Value::integer(delta.usage.output_tokens));
    usage.set("total_tokens", json::Value::integer(delta.usage.input_tokens + delta.usage.output_tokens));
    if (delta.usage.input_tokens_details.has_value()) {
        json::Value details = json::Value::object();
        details.set("cached_tokens", json::Value::integer(delta.usage.input_tokens_details->cached_tokens));
        usage.set("input_token_details", std::move(details));
    }
    if (delta.usage.output_tokens_details.has_value()) {
        json::Value details = json::Value::object();
        details.set("reasoning_tokens", json::Value::integer(delta.usage.output_tokens_details->reasoning_tokens));
        usage.set("output_token_details", std::move(details));
    }
    resp.set("usage", std::move(usage));

    std::vector<ir::Loss> losses;
    json::Value event = json::Value::object();

    if (delta.stop_reason == ir::STOP_END_TURN || delta.stop_reason == ir::STOP_TOOL_USE) {
        resp.set("status", json::Value::string("completed"));
        event.set("type", json::Value::string("response.completed"));
        event.set("response", std::move(resp));
    } else if (delta.stop_reason == ir::STOP_MAX_TOKENS) {
        resp.set("status", json::Value::string("incomplete"));
        json::Value inc = json::Value::object();
        inc.set("reason", json::Value::string("max_output_tokens"));
        resp.set("incomplete_details", std::move(inc));
        event.set("type", json::Value::string("response.incomplete"));
        event.set("response", std::move(resp));
    } else if (delta.stop_reason == ir::STOP_REFUSAL) {
        resp.set("status", json::Value::string("failed"));
        json::Value err = json::Value::object();
        err.set("code", json::Value::string("refusal"));
        err.set("message", json::Value::string(""));
        resp.set("error", std::move(err));
        event.set("type", json::Value::string("response.failed"));
        event.set("response", std::move(resp));
    } else if (delta.stop_reason == ir::STOP_STOP_SEQUENCE) {
        resp.set("status", json::Value::string("completed"));
        event.set("type", json::Value::string("response.completed"));
        event.set("response", std::move(resp));
        losses.push_back(make_resp_loss(
            "status", "stop_sequence", ir::LOSS_UNMAPPED_VALUE,
            "Responses status carries no stop-sequence identity; the matched IR stop sequence is lost"));
    } else {
        return invalid_argument("responses: stop reason \"" + std::string(delta.stop_reason) + "\" has no Responses equivalent");
    }

    return std::make_pair(std::move(event), std::move(losses));
}

StatusOr<Conversion<std::vector<json::Value>>> StreamEncoder::apply(const ir::Event& event) {
    if (done_ || (delta_ && !std::holds_alternative<ir::MessageDone>(event))) {
        return invalid_argument("responses: event applied after stream termination");
    }

    if (const auto* ms = std::get_if<ir::MessageStart>(&event)) {
        if (started_) return invalid_argument("responses: duplicate MessageStart");
        started_ = true;
        id_ = ms->id;
        model_ = opts_.model_map.map(ms->model);

        json::Value chunk = json::Value::object();
        chunk.set("type", json::Value::string("response.created"));
        json::Value resp = json::Value::object();
        resp.set("id", json::Value::string(id_));
        resp.set("object", json::Value::string("response"));
        resp.set("status", json::Value::string("in_progress"));
        resp.set("model", json::Value::string(model_));
        resp.set("output", json::Value::array());
        chunk.set("response", std::move(resp));
        return Conversion<std::vector<json::Value>>{{std::move(chunk)}, {}};
    }

    if (const auto* cbs = std::get_if<ir::ContentBlockStart>(&event)) {
        if (!started_ || active_block_.has_value() || delta_) {
            return invalid_argument("responses: ContentBlockStart out of grammar order");
        }
        if (cbs->index != next_block_index_) {
            return invalid_argument("responses: ContentBlockStart index " + std::to_string(cbs->index) +
                                    ", want " + std::to_string(next_block_index_));
        }
        next_block_index_++;
        if (const auto* tb = std::get_if<ir::TextBlock>(&cbs->block)) {
            OXA_ASSIGN_OR_RETURN(auto res, start_text_block(cbs->index, *tb));
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (const auto* thinking = std::get_if<ir::ThinkingBlock>(&cbs->block)) {
            OXA_ASSIGN_OR_RETURN(auto res, start_thinking_block(cbs->index, *thinking));
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (const auto* tu = std::get_if<ir::ToolUseBlock>(&cbs->block)) {
            OXA_ASSIGN_OR_RETURN(auto res, start_function_call_block(cbs->index, *tu));
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (const auto* custom = std::get_if<ir::CustomToolUseBlock>(&cbs->block)) {
            OXA_ASSIGN_OR_RETURN(auto res, start_custom_tool_call_block(cbs->index, *custom));
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (const auto* ap = std::get_if<ir::ApplyPatchToolUseBlock>(&cbs->block)) {
            OXA_ASSIGN_OR_RETURN(auto res, start_apply_patch_call_block(cbs->index, *ap));
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (const auto* apr = std::get_if<ir::ApplyPatchToolResultBlock>(&cbs->block)) {
            OXA_ASSIGN_OR_RETURN(auto res, start_apply_patch_result_block(cbs->index, *apr));
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        return invalid_argument("responses: ContentBlockStart carries unsupported block");
    }

    if (const auto* cbd = std::get_if<ir::ContentBlockDelta>(&event)) {
        if (!active_block_.has_value() || cbd->index != active_block_->index) {
            return invalid_argument("responses: ContentBlockDelta out of grammar order");
        }
        if (active_block_->kind == OutputItemKind::ApplyPatchCall || active_block_->kind == OutputItemKind::ApplyPatchResult) {
            return invalid_argument("responses: apply patch item does not accept deltas");
        }
        if (active_block_->kind == OutputItemKind::Message) {
            const auto* td = std::get_if<ir::TextDelta>(&cbd->delta);
            if (!td) return invalid_argument("responses: TextBlock received non-text delta");
            active_block_->text += td->text;

            json::Value chunk = json::Value::object();
            chunk.set("type", json::Value::string("response.output_text.delta"));
            chunk.set("item_id", json::Value::string(active_item_->id));
            chunk.set("output_index", json::Value::integer(active_item_->output_index));
            chunk.set("content_index", json::Value::integer(active_block_->content_index));
            chunk.set("delta", json::Value::string(td->text));
            return Conversion<std::vector<json::Value>>{{std::move(chunk)}, {}};
        }
        if (active_block_->kind == OutputItemKind::Reasoning) {
            if (const auto* thinking = std::get_if<ir::ThinkingDelta>(&cbd->delta)) {
                active_block_->text += thinking->text;
                json::Value chunk = json::Value::object();
                chunk.set("type", json::Value::string(std::string(EVENT_TYPE_RESPONSE_REASONING_SUMMARY_TEXT_DELTA)));
                chunk.set("item_id", json::Value::string(active_item_->id));
                chunk.set("output_index", json::Value::integer(active_item_->output_index));
                chunk.set("content_index", json::Value::integer(active_block_->content_index));
                chunk.set("delta", json::Value::string(thinking->text));
                return Conversion<std::vector<json::Value>>{{std::move(chunk)}, {}};
            }
            if (std::holds_alternative<ir::SignatureDelta>(cbd->delta)) {
                active_block_->signature_seen = true;
                return Conversion<std::vector<json::Value>>{{}, {make_resp_loss(
                    "events[" + std::to_string(cbd->index) + "].delta.signature", "signature",
                    ir::LOSS_UNMAPPED_FIELD,
                    "Responses carries no signature delta in streaming reasoning")}};
            }
            return invalid_argument("responses: ThinkingBlock received non-thinking delta");
        }
        if (active_block_->kind == OutputItemKind::CustomToolCall) {
            const auto* custom = std::get_if<ir::CustomToolInputDelta>(&cbd->delta);
            if (!custom) return invalid_argument("responses: CustomToolUseBlock received non-custom delta");
            active_block_->fragments.push_back(custom->text);
            json::Value chunk = json::Value::object();
            chunk.set("type", json::Value::string(std::string(EVENT_TYPE_RESPONSE_CUSTOM_TOOL_INPUT_DELTA)));
            chunk.set("item_id", json::Value::string(active_item_->id));
            chunk.set("output_index", json::Value::integer(active_item_->output_index));
            chunk.set("delta", json::Value::string(custom->text));
            return Conversion<std::vector<json::Value>>{{std::move(chunk)}, {}};
        }
        if (active_block_->kind == OutputItemKind::FunctionCall) {
            const auto* ij = std::get_if<ir::InputJsonDelta>(&cbd->delta);
            if (!ij) return invalid_argument("responses: ToolUseBlock received non-input-json delta");
            active_block_->fragments.push_back(ij->partial_json);

            json::Value chunk = json::Value::object();
            chunk.set("type", json::Value::string("response.function_call_arguments.delta"));
            chunk.set("item_id", json::Value::string(active_item_->id));
            chunk.set("output_index", json::Value::integer(active_item_->output_index));
            chunk.set("delta", json::Value::string(ij->partial_json));
            return Conversion<std::vector<json::Value>>{{std::move(chunk)}, {}};
        }
        return invalid_argument("responses: unknown active block kind");
    }

    if (const auto* cbs = std::get_if<ir::ContentBlockStop>(&event)) {
        if (!active_block_.has_value() || cbs->index != active_block_->index) {
            return invalid_argument("responses: ContentBlockStop out of grammar order");
        }
        if (active_block_->kind == OutputItemKind::Message) {
            OXA_ASSIGN_OR_RETURN(auto res, stop_text_block());
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (active_block_->kind == OutputItemKind::Reasoning) {
            OXA_ASSIGN_OR_RETURN(auto res, stop_thinking_block());
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (active_block_->kind == OutputItemKind::CustomToolCall) {
            OXA_ASSIGN_OR_RETURN(auto res, stop_custom_tool_call_block());
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (active_block_->kind == OutputItemKind::ApplyPatchCall) {
            OXA_ASSIGN_OR_RETURN(auto res, stop_apply_patch_call_block());
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        if (active_block_->kind == OutputItemKind::ApplyPatchResult) {
            OXA_ASSIGN_OR_RETURN(auto res, stop_apply_patch_result_block());
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        OXA_ASSIGN_OR_RETURN(auto res, stop_function_call_block());
        return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
    }

    if (const auto* md = std::get_if<ir::MessageDelta>(&event)) {
        if (!started_ || active_block_.has_value() || delta_) {
            return invalid_argument("responses: MessageDelta out of grammar order");
        }
        std::vector<json::Value> out;
        if (active_item_.has_value()) {
            if (active_item_->kind == OutputItemKind::Message) {
                out.push_back(close_message_item());
            } else if (active_item_->kind == OutputItemKind::Reasoning) {
                out.push_back(close_reasoning_item());
            } else {
                return invalid_argument("responses: MessageDelta with an uncompleted item");
            }
        }
        OXA_ASSIGN_OR_RETURN(auto res, terminal(*md));
        delta_ = true;
        out.push_back(std::move(res.first));
        return Conversion<std::vector<json::Value>>{std::move(out), std::move(res.second)};
    }

    if (std::holds_alternative<ir::MessageDone>(event)) {
        if (!delta_) return invalid_argument("responses: MessageDone out of grammar order");
        done_ = true;
        return Conversion<std::vector<json::Value>>{{}, {}};
    }

    return invalid_argument("responses: unknown event type");
}

}  // namespace oxa::openai::responses
