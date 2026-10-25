#include "oxa/openai/chatcompletions.hpp"
#include "chatcompletions_audit.hpp"

#include <algorithm>

namespace oxa::openai::chatcompletions {
namespace {

ir::Loss make_cc_loss(std::string path, std::string field, std::string_view reason,
                      std::string detail) {
    return ir::make_loss(std::move(path), std::move(field), reason, std::move(detail));
}

StatusOr<std::optional<ir::ResponseFormat>> decode_response_format(
    const json::Value& value, std::vector<ir::Loss>& losses) {
    if (!value.is_object()) return invalid_argument("chatcompletions: response_format must be an object");
    const auto* type = value.find("type");
    if (type == nullptr || !type->is_string()) {
        return invalid_argument("chatcompletions: response_format requires string type");
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
        losses.push_back(make_cc_loss("response_format.type", "type", ir::LOSS_UNMAPPED_VALUE,
                                      "unknown response_format type \"" + kind + "\""));
        return std::optional<ir::ResponseFormat>{};
    }

    const auto* schema_config = value.find("json_schema");
    if (schema_config == nullptr || !schema_config->is_object()) {
        return invalid_argument("chatcompletions: response_format of type json_schema requires json_schema object");
    }
    const auto* name = schema_config->find("name");
    if (name == nullptr || !name->is_string() || name->as_string().empty()) {
        return invalid_argument("chatcompletions: response_format.json_schema requires non-empty name");
    }
    const auto* schema = schema_config->find("schema");
    if (schema == nullptr || !schema->is_object()) {
        return invalid_argument("chatcompletions: response_format.json_schema requires schema object");
    }
    ir::ResponseFormat format;
    format.type = "json_schema";
    format.name = name->as_string();
    format.schema = *schema;
    if (const auto* description = schema_config->find("description"); description != nullptr) {
        if (!description->is_string()) {
            return invalid_argument("chatcompletions: response_format.json_schema description must be a string");
        }
        format.description = description->as_string();
    }
    if (const auto* strict = schema_config->find("strict"); strict != nullptr) {
        if (!strict->is_bool()) {
            return invalid_argument("chatcompletions: response_format.json_schema strict must be a boolean");
        }
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

StatusOr<std::pair<std::vector<ir::Block>, std::vector<ir::Loss>>> decode_content(
    const json::Value* content, const std::string& path) {
    if (!content || content->is_null()) {
        return std::make_pair(std::vector<ir::Block>{ir::TextBlock{""}}, std::vector<ir::Loss>{});
    }
    if (content->is_string()) {
        return std::make_pair(std::vector<ir::Block>{ir::TextBlock{content->as_string()}},
                              std::vector<ir::Loss>{});
    }
    if (content->is_array()) {
        std::vector<ir::Block> blocks;
        std::vector<ir::Loss> losses;
        const auto& arr = content->as_array();
        for (std::size_t i = 0; i < arr.size(); ++i) {
            const auto& part = arr[i];
            const auto* type_val = part.find("type");
            std::string ptype = type_val && type_val->is_string() ? type_val->as_string() : "";
            if (ptype == "text") {
                const auto* txt_val = part.find("text");
                std::string t = txt_val && txt_val->is_string() ? txt_val->as_string() : "";
                blocks.push_back(ir::TextBlock{std::move(t)});
            } else if (ptype == "image_url") {
                const auto* iu = part.find("image_url");
                const auto* url_val = iu ? iu->find("url") : nullptr;
                std::string raw_url = url_val && url_val->is_string() ? url_val->as_string() : "";
                std::string part_path = path + "[" + std::to_string(i) + "].image_url";

                if (raw_url.starts_with("https:")) {
                    if (is_valid_https_url(raw_url)) {
                        ir::ImageBlock img;
                        img.url = raw_url;
                        blocks.push_back(std::move(img));
                    } else {
                        losses.push_back(make_cc_loss(part_path, "image_url",
                                                      ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                      "malformed https image URL has no IR equivalent"));
                    }
                } else if (raw_url.starts_with("data:")) {
                    std::string_view data_part = std::string_view(raw_url).substr(5);
                    std::size_t comma = data_part.find(',');
                    if (comma == std::string_view::npos) {
                        losses.push_back(make_cc_loss(part_path, "image_url",
                                                      ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                      "malformed data image URL has no IR equivalent"));
                    } else {
                        std::string_view meta = data_part.substr(0, comma);
                        std::string_view b64 = data_part.substr(comma + 1);
                        constexpr std::string_view kBase64Suffix = ";base64";
                        if (!meta.ends_with(kBase64Suffix)) {
                            losses.push_back(make_cc_loss(part_path, "image_url",
                                                          ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                          "malformed data image URL has no IR equivalent"));
                        } else {
                            std::string media_type(meta.substr(0, meta.size() - kBase64Suffix.size()));
                            std::string lower_mt = media_type;
                            for (char& c : lower_mt) c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
                            if (!lower_mt.starts_with("image/") || lower_mt == "image/") {
                                losses.push_back(make_cc_loss(part_path, "image_url",
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
                    losses.push_back(make_cc_loss(part_path, "image_url",
                                                  ir::LOSS_UNSUPPORTED_SEMANTIC,
                                                  "only https and base64 data image URLs are supported"));
                }
            } else {
                losses.push_back(make_cc_loss(
                    path + "[" + std::to_string(i) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "Chat Completions content part type \"" + ptype + "\" has no IR equivalent"));
            }
        }
        return std::make_pair(std::move(blocks), std::move(losses));
    }
    return invalid_argument(path + ": content must be a string or parts array");
}

std::pair<std::vector<ir::Block>, std::vector<ir::Loss>> decode_tool_calls(
    const json::Value* tool_calls, const std::string& path) {
    std::vector<ir::Block> blocks;
    std::vector<ir::Loss> losses;
    if (!tool_calls || !tool_calls->is_array()) return {blocks, losses};

    const auto& arr = tool_calls->as_array();
    for (std::size_t i = 0; i < arr.size(); ++i) {
        const auto& tc = arr[i];
        const auto* type_val = tc.find("type");
        std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";
        if (kind == "custom") {
            std::string id;
            if (const auto* id_val = tc.find("id"); id_val && id_val->is_string()) id = id_val->as_string();
            std::string name;
            std::string input;
            if (const auto* cu = tc.find("custom"); cu && cu->is_object()) {
                if (const auto* nv = cu->find("name"); nv && nv->is_string()) name = nv->as_string();
                if (const auto* iv = cu->find("input"); iv && iv->is_string()) input = iv->as_string();
            }
            blocks.push_back(ir::CustomToolUseBlock{std::move(id), std::move(name), std::move(input)});
            continue;
        }
        if (kind != "function") {
            losses.push_back(make_cc_loss(
                path + "[" + std::to_string(i) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "Chat Completions tool call type \"" + kind + "\" has no IR equivalent"));
            continue;
        }
        std::string id;
        if (const auto* id_val = tc.find("id"); id_val && id_val->is_string()) id = id_val->as_string();
        std::string name;
        std::string args;
        if (const auto* fn = tc.find("function"); fn && fn->is_object()) {
            if (const auto* nv = fn->find("name"); nv && nv->is_string()) name = nv->as_string();
            if (const auto* av = fn->find("arguments"); av && av->is_string()) args = av->as_string();
        }
        blocks.push_back(ir::ToolUseBlock{std::move(id), std::move(name), std::move(args)});
    }
    return {blocks, losses};
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
                {make_cc_loss("tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
                              "Chat Completions tool_choice \"" + s + "\" has no IR equivalent")}};
    }
    if (tc->is_object()) {
        const auto* type_val = tc->find("type");
        std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";
        std::string name;
        if (const auto* cu = tc->find("custom"); cu && cu->is_object()) {
            if (const auto* nv = cu->find("name"); nv && nv->is_string()) name = nv->as_string();
        }
        if (kind == "custom" && !name.empty()) {
            return {ir::ToolChoice{std::string(ir::TOOL_CHOICE_TOOL), name, std::string("custom")}, {}};
        }
        if (const auto* fn = tc->find("function"); fn && fn->is_object()) {
            if (const auto* nv = fn->find("name"); nv && nv->is_string()) name = nv->as_string();
        }
        if (kind == "function" && !name.empty()) {
            return {ir::ToolChoice{std::string(ir::TOOL_CHOICE_TOOL), name}, {}};
        }
        return {std::nullopt,
                {make_cc_loss("tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
                              "only named function Chat Completions tool_choice values are supported")}};
    }
    return {std::nullopt,
            {make_cc_loss("tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
                          "Chat Completions tool_choice has no IR equivalent")}};
}

std::pair<std::string, std::optional<ir::Loss>> decode_finish_reason(std::string_view fr) {
    if (fr == "stop") return {std::string(ir::STOP_END_TURN), std::nullopt};
    if (fr == "length") return {std::string(ir::STOP_MAX_TOKENS), std::nullopt};
    if (fr == "tool_calls") return {std::string(ir::STOP_TOOL_USE), std::nullopt};
    if (fr == "content_filter") return {std::string(ir::STOP_REFUSAL), std::nullopt};
    return {std::string(ir::STOP_OTHER),
            make_cc_loss("choices[0].finish_reason", "finish_reason", ir::LOSS_UNMAPPED_VALUE,
                         "Chat Completions finish_reason \"" + std::string(fr) +
                             "\" has no IR equivalent; mapped to other")};
}

using FinishReasonResult = std::pair<std::string, std::optional<ir::Loss>>;

StatusOr<FinishReasonResult> encode_finish_reason(std::string_view stop_reason) {
    if (stop_reason == ir::STOP_END_TURN) return FinishReasonResult{"stop", std::nullopt};
    if (stop_reason == ir::STOP_MAX_TOKENS) return FinishReasonResult{"length", std::nullopt};
    if (stop_reason == ir::STOP_REFUSAL) return FinishReasonResult{"content_filter", std::nullopt};
    if (stop_reason == ir::STOP_TOOL_USE) return FinishReasonResult{"tool_calls", std::nullopt};
    if (stop_reason == ir::STOP_STOP_SEQUENCE) {
        return FinishReasonResult{
            "stop",
            make_cc_loss("", "stop_sequence", ir::LOSS_UNMAPPED_VALUE,
                         "Chat Completions finish_reason \"stop\" does not identify the matched stop sequence")};
    }
    return invalid_argument("chatcompletions: stop reason \"" + std::string(stop_reason) +
                            "\" has no Chat Completions equivalent");
}

}  // namespace

static StatusOr<Conversion<ir::Request>> decode_request_unchecked(const json::Value& wire,
                                                 const Options& opts) {
    if (!wire.is_object()) return invalid_argument("chatcompletions: request must be an object");
    std::vector<ir::Loss> losses;

    const char* dropped_fields[] = {
        "parallel_tool_calls", "functions", "function_call", "logprobs", "top_logprobs", "metadata"};
    for (const char* f : dropped_fields) {
        if (wire.find(f) != nullptr) {
            std::string detail = "Chat Completions " + std::string(f) + " has no IR equivalent in v1.";
            if (std::string_view(f) == "parallel_tool_calls") {
                detail = "Chat Completions parallel tool calls have no IR equivalent in v1.";
            } else if (std::string_view(f) == "functions") {
                detail = "legacy Chat Completions functions have no IR equivalent in v1.";
            } else if (std::string_view(f) == "function_call") {
                detail = "legacy Chat Completions function_call has no IR equivalent in v1.";
            } else if (std::string_view(f) == "logprobs" || std::string_view(f) == "top_logprobs") {
                detail = "Chat Completions log-probability sampling has no IR equivalent in v1.";
            } else if (std::string_view(f) == "metadata") {
                detail = "Chat Completions request metadata has no IR equivalent in v1.";
            }
            losses.push_back(make_cc_loss(f, f, ir::LOSS_UNMAPPED_FIELD, detail));
        }
    }

    ir::Request req;
    const auto* mv = wire.find("model");
    if (!mv || !mv->is_string()) return invalid_argument("chatcompletions: request missing model");
    req.model = opts.model_map.map(mv->as_string());

    if (const auto* tools_val = wire.find("tools"); tools_val && tools_val->is_array()) {
        std::vector<ir::Tool> tools;
        const auto& tarr = tools_val->as_array();
        for (std::size_t i = 0; i < tarr.size(); ++i) {
            const auto& t = tarr[i];
            const auto* type_val = t.find("type");
            std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";
            if (kind == "custom") {
                ir::Tool tool;
                tool.type = "custom";
                if (const auto* cu = t.find("custom"); cu && cu->is_object()) {
                    if (const auto* nv = cu->find("name"); nv && nv->is_string()) tool.name = nv->as_string();
                    if (const auto* dv = cu->find("description"); dv && dv->is_string() && !dv->as_string().empty()) {
                        tool.description = dv->as_string();
                        tool.has_description = true;
                    }
                    if (const auto* fv = cu->find("format"); fv && fv->is_object()) {
                        const auto* ft = fv->find("type");
                        std::string ftype = ft && ft->is_string() ? ft->as_string() : "";
                        if (ftype == "grammar") {
                            const auto* st = fv->find("syntax");
                            std::string syntax = st && st->is_string() ? st->as_string() : "";
                            if (syntax == "lark" || syntax == "regex") {
                                tool.format = *fv;
                            } else {
                                losses.push_back(make_cc_loss(
                                    "tools[" + std::to_string(i) + "].custom.format", "syntax", ir::LOSS_UNSUPPORTED_SEMANTIC,
                                    "unknown custom grammar syntax " + syntax));
                            }
                        } else if (ftype == "text") {
                            json::Value def_fmt = json::Value::object();
                            def_fmt.set("type", json::Value::string("text"));
                            tool.format = std::move(def_fmt);
                        } else {
                            losses.push_back(make_cc_loss(
                                "tools[" + std::to_string(i) + "].custom.format", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                                "unknown custom format " + ftype));
                        }
                    }
                }
                tools.push_back(std::move(tool));
                continue;
            }
            if (kind != "function") {
                losses.push_back(make_cc_loss(
                    "tools[" + std::to_string(i) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "Chat Completions tool type \"" + kind + "\" has no IR equivalent"));
                continue;
            }
            ir::Tool tool;
            if (const auto* fn = t.find("function"); fn && fn->is_object()) {
                if (const auto* nv = fn->find("name"); nv && nv->is_string()) tool.name = nv->as_string();
                if (const auto* dv = fn->find("description"); dv && dv->is_string() && !dv->as_string().empty()) {
                    tool.description = dv->as_string();
                    tool.has_description = true;
                }
                if (const auto* pv = fn->find("parameters")) {
                    tool.input_schema = *pv;
                } else {
                    tool.input_schema = json::Value::null();
                }
            }
            tools.push_back(std::move(tool));
        }
        if (!tools.empty()) req.tools = std::move(tools);
    }

    auto [tool_choice, tc_losses] = decode_tool_choice(wire.find("tool_choice"));
    req.tool_choice = std::move(tool_choice);
    losses.insert(losses.end(), tc_losses.begin(), tc_losses.end());

    const auto* msgs_val = wire.find("messages");
    if (!msgs_val || !msgs_val->is_array()) {
        return invalid_argument("chatcompletions: request missing messages array");
    }
    const auto& marr = msgs_val->as_array();

    std::set<std::string> custom_call_ids;
    for (const auto& m : marr) {
        const auto* rv = m.find("role");
        if (rv && rv->is_string() && rv->as_string() == "assistant") {
            if (const auto* tcs = m.find("tool_calls"); tcs && tcs->is_array()) {
                for (const auto& tc : tcs->as_array()) {
                    const auto* ty = tc.find("type");
                    if (ty && ty->is_string() && ty->as_string() == "custom") {
                        if (const auto* idv = tc.find("id"); idv && idv->is_string()) {
                            custom_call_ids.insert(idv->as_string());
                        }
                    }
                }
            }
        }
    }

    std::size_t index = 0;
    while (index < marr.size()) {
        const auto& m = marr[index];
        const auto* role_val = m.find("role");
        std::string role = role_val && role_val->is_string() ? role_val->as_string() : "";

        if (role == "tool") {
            // Consecutive tool messages merged into one user message (N-CC-4).
            std::vector<ir::Block> tr_blocks;
            while (index < marr.size()) {
                const auto& cur = marr[index];
                const auto* cr_val = cur.find("role");
                std::string cr = cr_val && cr_val->is_string() ? cr_val->as_string() : "";
                if (cr != "tool") break;

                std::string content_path = "messages[" + std::to_string(index) + "].content";
                OXA_ASSIGN_OR_RETURN(auto c_res, decode_content(cur.find("content"), content_path));
                losses.insert(losses.end(), c_res.second.begin(), c_res.second.end());

                std::string cid;
                if (const auto* tcid = cur.find("tool_call_id"); tcid && tcid->is_string()) {
                    cid = tcid->as_string();
                }

                if (custom_call_ids.count(cid)) {
                    ir::CustomToolResultBlock ctr;
                    ctr.tool_use_id = std::move(cid);
                    if (const auto* cv = cur.find("content"); cv && cv->is_string()) {
                        ctr.output = cv->as_string();
                    } else {
                        std::vector<ir::BlockHolder> holders;
                        for (auto& b : c_res.first) holders.push_back(ir::BlockHolder{std::move(b)});
                        ctr.output = std::move(holders);
                    }
                    tr_blocks.push_back(std::move(ctr));
                } else {
                    ir::ToolResultBlock tr;
                    tr.tool_use_id = std::move(cid);
                    for (auto& b : c_res.first) {
                        tr.content.push_back(ir::BlockHolder{std::move(b)});
                    }
                    tr_blocks.push_back(std::move(tr));
                }

                if (cur.find("function_call") != nullptr) {
                    losses.push_back(make_cc_loss(
                        "messages[" + std::to_string(index) + "].function_call", "function_call",
                        ir::LOSS_UNMAPPED_FIELD, "legacy Chat Completions function_call has no IR equivalent"));
                }
                ++index;
            }
            req.messages.push_back(ir::Message{std::string(ir::ROLE_USER), std::move(tr_blocks)});
            continue;
        }

        std::string content_path = "messages[" + std::to_string(index) + "].content";
        OXA_ASSIGN_OR_RETURN(auto c_res, decode_content(m.find("content"), content_path));
        losses.insert(losses.end(), c_res.second.begin(), c_res.second.end());
        auto content = std::move(c_res.first);

        if (role == "system") {
            for (auto& b : content) {
                if (auto* tb = std::get_if<ir::TextBlock>(&b)) {
                    req.system.push_back(std::move(*tb));
                } else {
                    losses.push_back(make_cc_loss(
                        content_path, "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "non-text content in Chat Completions system message has no IR equivalent"));
                }
            }
        } else if (role == "user") {
            if (content.empty()) {
                content.push_back(ir::TextBlock{""});
            }
            req.messages.push_back(ir::Message{std::string(ir::ROLE_USER), std::move(content)});
        } else if (role == "assistant") {
            const auto* reasoning = m.find("reasoning_content");
            if (reasoning != nullptr && !reasoning->is_null()) {
                if (!reasoning->is_string()) {
                    return invalid_argument("chatcompletions: reasoning_content must be a string");
                }
                content.insert(content.begin(), ir::ThinkingBlock{reasoning->as_string(), std::nullopt});
            }
            auto [tc_blocks, tc_losses] =
                decode_tool_calls(m.find("tool_calls"), "messages[" + std::to_string(index) + "].tool_calls");
            const auto* c_val = m.find("content");
            if ((!c_val || c_val->is_null()) && !tc_blocks.empty()) {
                content.erase(std::remove_if(content.begin(), content.end(), [](const ir::Block& block) {
                                  return !std::holds_alternative<ir::ThinkingBlock>(block);
                              }),
                              content.end());
            }
            content.insert(content.end(), std::make_move_iterator(tc_blocks.begin()),
                           std::make_move_iterator(tc_blocks.end()));
            if (content.empty()) {
                content.push_back(ir::TextBlock{""});
            }
            req.messages.push_back(ir::Message{std::string(ir::ROLE_ASSISTANT), std::move(content)});
            losses.insert(losses.end(), tc_losses.begin(), tc_losses.end());
        } else {
            return invalid_argument("chatcompletions: messages[" + std::to_string(index) +
                                    "]: unknown role \"" + role + "\"");
        }

        if (m.find("function_call") != nullptr) {
            losses.push_back(make_cc_loss(
                "messages[" + std::to_string(index) + "].function_call", "function_call",
                ir::LOSS_UNMAPPED_FIELD, "legacy Chat Completions function_call has no IR equivalent"));
        }
        ++index;
    }

    if (req.messages.empty()) {
        return invalid_argument("chatcompletions: request carries no conversation messages");
    }

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
    const auto* mt = wire.find("max_tokens");
    const auto* mct = wire.find("max_completion_tokens");
    if (mt != nullptr && !mt->is_null() && mct != nullptr && !mct->is_null()) {
        return invalid_argument("chatcompletions: cannot specify both max_tokens and max_completion_tokens");
    }
    if (mct != nullptr && !mct->is_null()) {
        if (!mct->is_int() || mct->as_int() <= 0) {
            return invalid_argument("chatcompletions: max_completion_tokens must be a positive integer");
        }
        params.max_tokens = mct->as_int();
        has_params = true;
        req.native.push_back(ir::NativeAnnotation{
            std::string(ir::NATIVE_SOURCE_OPENAI),
            std::string(ir::NATIVE_KIND_MAX_COMPLETION_TOKENS),
            "true",
            std::nullopt,
            std::nullopt,
        });
    } else if (mt && mt->is_int()) {
        params.max_tokens = mt->as_int();
        has_params = true;
    }

    if (const auto* st = wire.find("store")) {
        if (!st->is_bool() && !st->is_null()) {
            return invalid_argument("chatcompletions: store must be a boolean or null");
        }
        std::string val = st->is_null() ? "null" : (st->as_bool() ? "true" : "false");
        req.native.push_back(ir::NativeAnnotation{
            std::string(ir::NATIVE_SOURCE_OPENAI),
            std::string(ir::NATIVE_KIND_STORE),
            std::move(val),
            std::nullopt,
            std::nullopt,
        });
    }

    if (const auto* stream = wire.find("stream")) {
        if (!stream->is_bool() && !stream->is_null()) {
            return invalid_argument("chatcompletions: stream must be a boolean or null");
        }
        std::string val = stream->is_null() ? "null" : (stream->as_bool() ? "true" : "false");
        req.native.push_back(ir::NativeAnnotation{
            std::string(ir::NATIVE_SOURCE_OPENAI),
            std::string(ir::NATIVE_KIND_STREAM),
            std::move(val),
            std::nullopt,
            std::nullopt,
        });
    }

    if (const auto* so = wire.find("stream_options")) {
        if (!so->is_object() && !so->is_null()) {
            return invalid_argument("chatcompletions: stream_options must be an object or null");
        }
        if (so->is_object()) {
            if (const auto* u = so->find("include_usage"); u && !u->is_bool()) {
                return invalid_argument("chatcompletions: stream_options.include_usage must be a boolean");
            }
            if (const auto* ob = so->find("include_obfuscation"); ob && !ob->is_bool()) {
                return invalid_argument("chatcompletions: stream_options.include_obfuscation must be a boolean");
            }
            json::Value clean_so = json::Value::object();
            bool has_usage_true = false;
            if (const auto* ob = so->find("include_obfuscation")) {
                clean_so.set("include_obfuscation", json::Value::boolean(ob->as_bool()));
            }
            if (const auto* u = so->find("include_usage")) {
                clean_so.set("include_usage", json::Value::boolean(u->as_bool()));
                if (u->as_bool()) has_usage_true = true;
            }
            std::optional<ir::NativeOmission> omission;
            if (has_usage_true) {
                omission = ir::NativeOmission{"stream_options", std::string(ir::LOSS_DEGRADED), std::string(ir::STREAM_OPTIONS_LOSS_DETAIL)};
            }
            req.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_OPENAI),
                std::string(ir::NATIVE_KIND_STREAM_OPTIONS),
                json::serialize(clean_so),
                std::move(omission),
                std::nullopt,
            });
        } else {
            req.native.push_back(ir::NativeAnnotation{
                std::string(ir::NATIVE_SOURCE_OPENAI),
                std::string(ir::NATIVE_KIND_STREAM_OPTIONS),
                "null",
                std::nullopt,
                std::nullopt,
            });
        }
    }
    if (const auto* stop = wire.find("stop")) {
        std::vector<std::string> stops;
        if (stop->is_string()) {
            stops.push_back(stop->as_string());
        } else if (stop->is_array()) {
            for (const auto& s : stop->as_array()) {
                if (s.is_string()) stops.push_back(s.as_string());
            }
        }
        if (!stops.empty()) {
            params.stop_sequences = std::move(stops);
            has_params = true;
        }
    }
    if (const auto* effort = wire.find("reasoning_effort"); effort && !effort->is_null()) {
        if (!effort->is_string()) {
            return invalid_argument("chatcompletions: reasoning_effort must be a string");
        }
        const std::string value = effort->as_string();
        if (value == ir::REASONING_EFFORT_MINIMAL || value == ir::REASONING_EFFORT_LOW ||
            value == ir::REASONING_EFFORT_MEDIUM || value == ir::REASONING_EFFORT_HIGH ||
            value == ir::REASONING_EFFORT_XHIGH || value == ir::REASONING_EFFORT_MAX) {
            params.reasoning_effort = value;
            has_params = true;
        } else {
            losses.push_back(make_cc_loss(
                "reasoning_effort", "reasoning_effort", ir::LOSS_UNMAPPED_VALUE,
                "Chat Completions reasoning_effort \"" + value + "\" has no IR equivalent"));
        }
    }
    if (const auto* response_format = wire.find("response_format"); response_format != nullptr) {
        OXA_ASSIGN_OR_RETURN(auto decoded_format, decode_response_format(*response_format, losses));
        if (decoded_format.has_value()) {
            params.response_format = std::move(*decoded_format);
            has_params = true;
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

StatusOr<Conversion<json::Value>> encode_request(const ir::Request& req,
                                                 const Options& opts) {
    std::vector<ir::Loss> losses;
    if (req.metadata.has_value() && !req.metadata->empty()) {
        losses.push_back(make_cc_loss(
            "metadata", "metadata", ir::LOSS_UNMAPPED_FIELD,
            "Chat Completions requests have no metadata field; the IR metadata map is dropped."));
    }

    json::Value out = json::Value::object();
    out.set("model", json::Value::string(opts.model_map.map(req.model)));

    if (req.tools.has_value() && !req.tools->empty()) {
        json::Value tools_arr = json::Value::array();
        for (std::size_t ti = 0; ti < req.tools->size(); ++ti) {
            const auto& t = (*req.tools)[ti];
            if (t.type == "custom") {
                json::Value tw = json::Value::object();
                tw.set("type", json::Value::string("custom"));
                json::Value c = json::Value::object();
                c.set("name", json::Value::string(t.name));
                if (t.has_description && !t.description.empty()) {
                    c.set("description", json::Value::string(t.description));
                }
                if (t.format.has_value()) {
                    c.set("format", *t.format);
                }
                tw.set("custom", std::move(c));
                tools_arr.push_back(std::move(tw));
                continue;
            }
            if (t.type == "native") {
                losses.push_back(make_cc_loss(
                    "tools[" + std::to_string(ti) + "]", "type",
                    ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "native tool \"" + t.name + "\" from \"" + t.source.value_or("") + "\" cannot be represented by the target protocol"));
                continue;
            }
            if (t.type == ir::TOOL_TYPE_APPLY_PATCH) {
                losses.push_back(make_cc_loss("tools[" + std::to_string(ti) + "]", "type",
                    ir::LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tool declarations are not implemented by this Chat Completions profile"));
                continue;
            }
            json::Value tw = json::Value::object();
            tw.set("type", json::Value::string("function"));
            json::Value fn = json::Value::object();
            fn.set("name", json::Value::string(t.name));
            if (t.has_description && !t.description.empty()) {
                fn.set("description", json::Value::string(t.description));
            }
            if (!t.input_schema.is_null()) {
                fn.set("parameters", t.input_schema);
            }
            tw.set("function", std::move(fn));
            tools_arr.push_back(std::move(tw));
        }
        if (!tools_arr.as_array().empty()) {
            out.set("tools", std::move(tools_arr));
        }
    }

    bool all_tools_dropped = req.tools.has_value() && !req.tools->empty() && (!out.find("tools") || out.find("tools")->as_array().empty());
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
        bool name_retained = false;
        if (const auto* tw_arr = out.find("tools"); tw_arr && tw_arr->is_array()) {
            for (const auto& tw : tw_arr->as_array()) {
                if (const auto* fn = tw.find("function"); fn && fn->is_object()) {
                    if (const auto* nv = fn->find("name"); nv && nv->is_string() && nv->as_string() == cname) {
                        name_retained = true;
                        break;
                    }
                }
                if (const auto* cu = tw.find("custom"); cu && cu->is_object()) {
                    if (const auto* nv = cu->find("name"); nv && nv->is_string() && nv->as_string() == cname) {
                        name_retained = true;
                        break;
                    }
                }
            }
        }
        if (is_native && !name_retained) {
            native_named_choice_dropped = true;
        }
    }

    if (native_named_choice_dropped) {
        losses.push_back(make_cc_loss(
            "tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
            "native tool choice \"" + *req.tool_choice->name + "\" cannot be represented by the target protocol"));
    } else if (req.tool_choice.has_value()) {
        const auto& tc = *req.tool_choice;
        if (tc.mode == ir::TOOL_CHOICE_AUTO) {
            out.set("tool_choice", json::Value::string("auto"));
        } else if (tc.mode == ir::TOOL_CHOICE_NONE) {
            out.set("tool_choice", json::Value::string("none"));
        } else if (tc.mode == ir::TOOL_CHOICE_ANY) {
            if (all_tools_dropped) losses.push_back(make_cc_loss("tool_choice", "tool_choice",
                ir::LOSS_UNSUPPORTED_SEMANTIC, "all tool declarations were dropped"));
            else out.set("tool_choice", json::Value::string("required"));
        } else if (tc.mode == ir::TOOL_CHOICE_TOOL) {
            if (tc.tool_type.has_value() && *tc.tool_type == ir::TOOL_TYPE_APPLY_PATCH) {
                losses.push_back(make_cc_loss("tool_choice", "tool_choice", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "apply_patch tool choice is not implemented by this Chat Completions profile"));
            } else if (tc.tool_type.has_value() && *tc.tool_type == "custom") {
                if (tc.name.has_value() && !tc.name->empty()) {
                    json::Value tco = json::Value::object();
                    tco.set("type", json::Value::string("custom"));
                    json::Value cu = json::Value::object();
                    cu.set("name", json::Value::string(*tc.name));
                    tco.set("custom", std::move(cu));
                    out.set("tool_choice", std::move(tco));
                }
            } else if (tc.name.has_value() && !tc.name->empty()) {
                json::Value tco = json::Value::object();
                tco.set("type", json::Value::string("function"));
                json::Value fn = json::Value::object();
                fn.set("name", json::Value::string(*tc.name));
                tco.set("function", std::move(fn));
                out.set("tool_choice", std::move(tco));
            } else {
                losses.push_back(make_cc_loss("tool_choice", "tool_choice",
                                              ir::LOSS_UNSUPPORTED_SEMANTIC,
                                              "IR named tool choice has no function name"));
            }
        }
    }

    json::Value messages_arr = json::Value::array();
    if (!req.system.empty()) {
        std::string sys_txt;
        for (const auto& s : req.system) sys_txt += s.text;
        json::Value sm = json::Value::object();
        sm.set("role", json::Value::string("system"));
        sm.set("content", json::Value::string(std::move(sys_txt)));
        messages_arr.push_back(std::move(sm));
    }

    for (std::size_t i = 0; i < req.messages.size(); ++i) {
        const auto& m = req.messages[i];
        if (m.role == ir::ROLE_ASSISTANT) {
            json::Value am = json::Value::object();
            am.set("role", json::Value::string("assistant"));
            std::string text;
            std::string reasoning;
            bool has_reasoning = false;
            json::Value tc_arr = json::Value::array();
            for (std::size_t bi = 0; bi < m.content.size(); ++bi) {
                const auto& b = m.content[bi];
                if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
                    text += tb->text;
                    if (tb->citations.has_value() && !tb->citations->empty()) {
                        losses.push_back(make_cc_loss(
                            "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "].citations",
                            "citations", ir::LOSS_UNSUPPORTED_SEMANTIC,
                            "anthropic text citations; the target face cannot preserve source locations and replay metadata"));
                    }
                } else if (std::holds_alternative<ir::RedactedThinkingBlock>(b)) {
                    losses.push_back(make_cc_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "anthropic encrypted thinking replay state; the target face cannot preserve this block"));
                } else if (const auto* thinking = std::get_if<ir::ThinkingBlock>(&b)) {
                    has_reasoning = true;
                    reasoning += thinking->thinking;
                    if (thinking->signature.has_value() && !thinking->signature->empty()) {
                        losses.push_back(make_cc_loss(
                            "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "].signature",
                            "signature", ir::LOSS_UNMAPPED_FIELD,
                            "Chat Completions reasoning_content has no signature field; the opaque signature is dropped"));
                    }
                } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
                    json::Value tcw = json::Value::object();
                    tcw.set("id", json::Value::string(tu->id));
                    tcw.set("type", json::Value::string("function"));
                    json::Value fn = json::Value::object();
                    fn.set("name", json::Value::string(tu->name));
                    fn.set("arguments", json::Value::string(tu->input));
                    tcw.set("function", std::move(fn));
                    tc_arr.push_back(std::move(tcw));
                } else if (const auto* ctu = std::get_if<ir::CustomToolUseBlock>(&b)) {
                    json::Value tcw = json::Value::object();
                    tcw.set("id", json::Value::string(ctu->id));
                    tcw.set("type", json::Value::string("custom"));
                    json::Value cu = json::Value::object();
                    cu.set("name", json::Value::string(ctu->name));
                    cu.set("input", json::Value::string(ctu->input));
                    tcw.set("custom", std::move(cu));
                    tc_arr.push_back(std::move(tcw));
                } else if (std::holds_alternative<ir::ApplyPatchToolUseBlock>(b)) {
                    losses.push_back(make_cc_loss("messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "content", ir::LOSS_UNSUPPORTED_SEMANTIC, "IR apply_patch_tool_use block cannot be rendered in a Chat Completions assistant message"));
                } else if (std::holds_alternative<ir::ApplyPatchToolResultBlock>(b)) {
                    losses.push_back(make_cc_loss("messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "content", ir::LOSS_UNSUPPORTED_SEMANTIC, "IR apply_patch_tool_result block cannot be rendered in a Chat Completions assistant message"));
                } else {
                    losses.push_back(make_cc_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "IR image block cannot be rendered in a Chat Completions assistant message"));
                }
            }
            am.set("content", json::Value::string(std::move(text)));
            if (has_reasoning) {
                am.set("reasoning_content", json::Value::string(std::move(reasoning)));
            }
            if (!tc_arr.as_array().empty()) {
                am.set("tool_calls", std::move(tc_arr));
            }
            messages_arr.push_back(std::move(am));
        } else {
            // User turn: split tool results and normal blocks (N-CC-9 hoisting)
            std::vector<const ir::Block*> results;
            std::vector<const ir::Block*> normal;
            std::optional<std::size_t> first_normal;
            std::optional<std::size_t> last_result;

            for (std::size_t bi = 0; bi < m.content.size(); ++bi) {
                const auto& b = m.content[bi];
                if (std::holds_alternative<ir::ToolResultBlock>(b) || std::holds_alternative<ir::CustomToolResultBlock>(b)) {
                    results.push_back(&b);
                    last_result = bi;
                } else if (std::holds_alternative<ir::ApplyPatchToolResultBlock>(b)) {
                    losses.push_back(make_cc_loss("messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "type", ir::LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tool results are not implemented by this profile"));
                } else if (std::holds_alternative<ir::DocumentBlock>(b)) {
                    losses.push_back(make_cc_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "IR ir.DocumentBlock cannot be rendered in a Chat Completions user message"));
                } else if (std::holds_alternative<ir::RedactedThinkingBlock>(b)) {
                    losses.push_back(make_cc_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "anthropic encrypted thinking replay state; the target face cannot preserve this block"));
                } else {
                    if (const auto* utb = std::get_if<ir::TextBlock>(&b);
                        utb && utb->citations.has_value() && !utb->citations->empty()) {
                        losses.push_back(make_cc_loss(
                            "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "].citations",
                            "citations", ir::LOSS_UNSUPPORTED_SEMANTIC,
                            "anthropic text citations; the target face cannot preserve source locations and replay metadata"));
                    }
                    if (!first_normal.has_value()) first_normal = bi;
                    normal.push_back(&b);
                }
            }

            if (first_normal.has_value() && last_result.has_value() && *first_normal < *last_result) {
                losses.push_back(make_cc_loss(
                    "messages[" + std::to_string(i) + "]", "ordering", ir::LOSS_DEGRADED,
                    "N-CC-9: tool messages are hoisted ahead of the trailing user content; source order is not preserved"));
            }

            for (std::size_t ri = 0; ri < results.size(); ++ri) {
                const auto* rb = results[ri];
                if (const auto* ctr = std::get_if<ir::CustomToolResultBlock>(rb)) {
                    json::Value tm = json::Value::object();
                    tm.set("role", json::Value::string("tool"));
                    tm.set("tool_call_id", json::Value::string(ctr->tool_use_id));
                    if (const auto* s = std::get_if<std::string>(&ctr->output)) {
                        tm.set("content", json::Value::string(*s));
                    } else if (const auto* vec = std::get_if<std::vector<ir::BlockHolder>>(&ctr->output)) {
                        json::Value parts = json::Value::array();
                        for (std::size_t bi = 0; bi < vec->size(); ++bi) {
                            const auto& inner = (*vec)[bi].block;
                            if (const auto* tb = std::get_if<ir::TextBlock>(&inner)) {
                                json::Value part = json::Value::object();
                                part.set("type", json::Value::string(std::string(CONTENT_PART_TYPE_TEXT)));
                                part.set("text", json::Value::string(tb->text));
                                parts.push_back(std::move(part));
                            } else if (const auto* img = std::get_if<ir::ImageBlock>(&inner)) {
                                json::Value part = json::Value::object();
                                part.set("type", json::Value::string(std::string(CONTENT_PART_TYPE_IMAGE_URL)));
                                json::Value iuo = json::Value::object();
                                if (img->data.has_value() && !img->data->empty()) {
                                    std::string mt = img->media_type.value_or("image/jpeg");
                                    iuo.set("url", json::Value::string("data:" + mt + ";base64," + *img->data));
                                } else if (img->url.has_value()) {
                                    iuo.set("url", json::Value::string(*img->url));
                                }
                                part.set("image_url", std::move(iuo));
                                parts.push_back(std::move(part));
                            } else {
                                losses.push_back(make_cc_loss(
                                    "messages[" + std::to_string(i) + "].output[" + std::to_string(bi) + "]", "content",
                                    ir::LOSS_UNSUPPORTED_SEMANTIC, "this IR block cannot be rendered in a Chat Completions tool result"));
                            }
                        }
                        tm.set("content", std::move(parts));
                    }
                    messages_arr.push_back(std::move(tm));
                    continue;
                }
                const auto* tr = std::get_if<ir::ToolResultBlock>(rb);
                json::Value tm = json::Value::object();
                tm.set("role", json::Value::string("tool"));
                tm.set("tool_call_id", json::Value::string(tr->tool_use_id));
                std::string res_txt;
                for (const auto& inner : tr->content) {
                    if (const auto* itb = std::get_if<ir::TextBlock>(&inner.block)) {
                        res_txt += itb->text;
                    } else {
                        losses.push_back(make_cc_loss(
                            "messages[" + std::to_string(i) + "].content[" + std::to_string(ri) + "]",
                            "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                            "IR tool_result content part cannot be rendered in a Chat Completions tool message"));
                    }
                }
                if (tr->is_error) {
                    losses.push_back(make_cc_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(ri) + "].is_error",
                        "is_error", ir::LOSS_UNMAPPED_FIELD,
                        "Chat Completions tool messages have no is_error field"));
                }
                tm.set("content", json::Value::string(std::move(res_txt)));
                messages_arr.push_back(std::move(tm));
            }

            if (!normal.empty() || results.empty()) {
                json::Value um = json::Value::object();
                um.set("role", json::Value::string("user"));
                bool has_image = false;
                for (const auto* b : normal) {
                    if (std::holds_alternative<ir::ImageBlock>(*b)) {
                        has_image = true;
                        break;
                    }
                }
                if (!has_image) {
                    std::string u_txt;
                    for (const auto* b : normal) {
                        if (const auto* tb = std::get_if<ir::TextBlock>(b)) u_txt += tb->text;
                    }
                    um.set("content", json::Value::string(std::move(u_txt)));
                } else {
                    json::Value parts = json::Value::array();
                    for (std::size_t ni = 0; ni < normal.size(); ++ni) {
                        const auto* b = normal[ni];
                        if (const auto* tb = std::get_if<ir::TextBlock>(b)) {
                            json::Value part = json::Value::object();
                            part.set("type", json::Value::string("text"));
                            part.set("text", json::Value::string(tb->text));
                            parts.push_back(std::move(part));
                        } else if (const auto* img = std::get_if<ir::ImageBlock>(b)) {
                            json::Value part = json::Value::object();
                            part.set("type", json::Value::string("image_url"));
                            json::Value iuo = json::Value::object();
                            if (img->data.has_value() && !img->data->empty()) {
                                std::string mt = img->media_type.value_or("image/jpeg");
                                iuo.set("url", json::Value::string("data:" + mt + ";base64," + *img->data));
                            } else if (img->url.has_value()) {
                                iuo.set("url", json::Value::string(*img->url));
                            }
                            part.set("image_url", std::move(iuo));
                            parts.push_back(std::move(part));
                        }
                    }
                    if (parts.as_array().empty()) um.set("content", json::Value::string(""));
                    else um.set("content", std::move(parts));
                }
                messages_arr.push_back(std::move(um));
            }
        }
    }
    out.set("messages", std::move(messages_arr));

    bool has_max_completion_tokens = false;
    for (const auto& n : req.native) {
        if (n.source == ir::NATIVE_SOURCE_OPENAI && n.kind == ir::NATIVE_KIND_MAX_COMPLETION_TOKENS) {
            has_max_completion_tokens = true;
            break;
        }
    }

    if (req.params.has_value()) {
        const auto& p = *req.params;
        if (p.temperature.has_value()) out.set("temperature", json::Value::real(*p.temperature));
        if (p.top_p.has_value()) out.set("top_p", json::Value::real(*p.top_p));
        if (p.max_tokens.has_value()) {
            if (has_max_completion_tokens) {
                out.set("max_completion_tokens", json::Value::integer(*p.max_tokens));
            } else {
                out.set("max_tokens", json::Value::integer(*p.max_tokens));
            }
        }
        if (p.stop_sequences.has_value() && !p.stop_sequences->empty()) {
            json::Value stops = json::Value::array();
            for (const auto& s : *p.stop_sequences) stops.push_back(json::Value::string(s));
            out.set("stop", std::move(stops));
        }
        if (p.reasoning_effort.has_value()) {
            out.set("reasoning_effort", json::Value::string(*p.reasoning_effort));
        }
        if (p.response_format.has_value()) {
            const auto& rf = *p.response_format;
            json::Value format = json::Value::object();
            format.set("type", json::Value::string(rf.type));
            if (rf.type == "json_schema") {
                json::Value schema_config = json::Value::object();
                schema_config.set("name", json::Value::string(rf.name));
                if (rf.description.has_value()) {
                    schema_config.set("description", json::Value::string(*rf.description));
                }
                if (rf.schema.has_value()) schema_config.set("schema", *rf.schema);
                if (rf.strict.has_value()) schema_config.set("strict", json::Value::boolean(*rf.strict));
                format.set("json_schema", std::move(schema_config));
            }
            out.set("response_format", std::move(format));
        }
    }

    for (const auto& n : req.native) {
        if (n.source == ir::NATIVE_SOURCE_OPENAI) {
            if (n.kind == ir::NATIVE_KIND_STORE) {
                if (n.value == "null") {
                    out.set("store", json::Value::null());
                } else {
                    auto parsed = json::parse(n.value);
                    if (parsed.ok() && parsed->is_bool()) {
                        out.set("store", json::Value::boolean(parsed->as_bool()));
                    }
                }
            } else if (n.kind == ir::NATIVE_KIND_STREAM) {
                if (n.value == "null") {
                    out.set("stream", json::Value::null());
                } else {
                    auto parsed = json::parse(n.value);
                    if (parsed.ok() && parsed->is_bool()) {
                        out.set("stream", json::Value::boolean(parsed->as_bool()));
                    }
                }
            } else if (n.kind == ir::NATIVE_KIND_STREAM_OPTIONS) {
                if (n.value == "null") {
                    out.set("stream_options", json::Value::null());
                } else {
                    auto parsed = json::parse(n.value);
                    if (parsed.ok() && parsed->is_object()) {
                        out.set("stream_options", std::move(*parsed));
                    }
                }
            }
        }
    }

    auto omitted = ir::omitted_native_request_losses(req, "chatcompletions");
    if (!omitted.ok()) return omitted.status();
    losses.insert(losses.end(), omitted->begin(), omitted->end());

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

static StatusOr<Conversion<ir::Response>> decode_response_unchecked(const json::Value& wire,
                                                  const Options& opts) {
    if (!wire.is_object()) return invalid_argument("chatcompletions: response must be an object");
    const auto* choices = wire.find("choices");
    if (!choices || !choices->is_array() || choices->as_array().empty()) {
        return invalid_argument("chatcompletions: response carries no choices");
    }

    std::vector<ir::Loss> losses;
    const auto& choice = choices->as_array()[0];
    const auto* msg = choice.find("message");
    if (!msg || !msg->is_object()) return invalid_argument("chatcompletions: choice missing message");

    OXA_ASSIGN_OR_RETURN(auto c_res, decode_content(msg->find("content"), "choices[0].message.content"));
    losses.insert(losses.end(), c_res.second.begin(), c_res.second.end());
    auto blocks = std::move(c_res.first);
    if (const auto* reasoning = msg->find("reasoning_content"); reasoning && !reasoning->is_null()) {
        if (!reasoning->is_string()) {
            return invalid_argument("chatcompletions: reasoning_content must be a string");
        }
        blocks.insert(blocks.begin(), ir::ThinkingBlock{reasoning->as_string(), std::nullopt});
    }

    auto [tool_blocks, tool_losses] = decode_tool_calls(msg->find("tool_calls"), "choices[0].message.tool_calls");
    const auto* content_val = msg->find("content");
    if ((!content_val || content_val->is_null()) && !tool_blocks.empty()) {
        blocks.erase(std::remove_if(blocks.begin(), blocks.end(), [](const ir::Block& block) {
                         return !std::holds_alternative<ir::ThinkingBlock>(block);
                     }),
                     blocks.end());
    }
    blocks.insert(blocks.end(), std::make_move_iterator(tool_blocks.begin()),
                  std::make_move_iterator(tool_blocks.end()));
    losses.insert(losses.end(), tool_losses.begin(), tool_losses.end());

    if (msg->find("function_call") != nullptr) {
        losses.push_back(make_cc_loss(
            "choices[0].message.function_call", "function_call", ir::LOSS_UNMAPPED_FIELD,
            "legacy Chat Completions function_call has no IR equivalent"));
    }

    ir::Response resp;
    if (const auto* id_val = wire.find("id"); id_val && id_val->is_string()) resp.id = id_val->as_string();
    if (const auto* m_val = wire.find("model"); m_val && m_val->is_string()) {
        resp.model = opts.model_map.map(m_val->as_string());
    }
    resp.content = std::move(blocks);

    std::string fr;
    if (const auto* fr_val = choice.find("finish_reason"); fr_val && fr_val->is_string()) {
        fr = fr_val->as_string();
    }
    auto [stop_reason, fr_loss] = decode_finish_reason(fr);
    resp.stop_reason = std::move(stop_reason);
    if (fr_loss.has_value()) losses.push_back(std::move(*fr_loss));

    if (const auto* u_val = wire.find("usage"); u_val && u_val->is_object()) {
        if (const auto* pt = u_val->find("prompt_tokens"); pt && pt->is_int()) {
            resp.usage.input_tokens = pt->as_int();
        }
        if (const auto* ct = u_val->find("completion_tokens"); ct && ct->is_int()) {
            resp.usage.output_tokens = ct->as_int();
        }
        if (const auto* details = u_val->find("prompt_tokens_details"); details && details->is_object()) {
            if (const auto* cached = details->find("cached_tokens"); cached && cached->is_int()) {
                resp.usage.input_tokens_details = ir::InputTokensDetails{cached->as_int()};
            }
        }
        if (const auto* details = u_val->find("completion_tokens_details"); details && details->is_object()) {
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
    out.set("object", json::Value::string("chat.completion"));
    out.set("created", json::Value::integer(0));
    out.set("model", json::Value::string(opts.model_map.map(resp.model)));

    json::Value msg = json::Value::object();
    msg.set("role", json::Value::string("assistant"));
    std::string text;
    std::string reasoning;
    bool has_reasoning = false;
    json::Value tc_arr = json::Value::array();
    for (std::size_t i = 0; i < resp.content.size(); ++i) {
        const auto& b = resp.content[i];
        if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
            text += tb->text;
            if (tb->citations.has_value() && !tb->citations->empty()) {
                losses.push_back(make_cc_loss(
                    "content[" + std::to_string(i) + "].citations", "citations",
                    ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "anthropic text citations; the target face cannot preserve source locations and replay metadata"));
            }
        } else if (std::holds_alternative<ir::RedactedThinkingBlock>(b)) {
            losses.push_back(make_cc_loss(
                "content[" + std::to_string(i) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "anthropic encrypted thinking replay state; the target face cannot preserve this block"));
        } else if (const auto* thinking = std::get_if<ir::ThinkingBlock>(&b)) {
            has_reasoning = true;
            reasoning += thinking->thinking;
            if (thinking->signature.has_value() && !thinking->signature->empty()) {
                losses.push_back(make_cc_loss(
                    "content[" + std::to_string(i) + "].signature", "signature",
                    ir::LOSS_UNMAPPED_FIELD,
                    "Chat Completions reasoning_content has no signature field; the opaque signature is dropped"));
            }
        } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
            json::Value tcw = json::Value::object();
            tcw.set("id", json::Value::string(tu->id));
            tcw.set("type", json::Value::string("function"));
            json::Value fn = json::Value::object();
            fn.set("name", json::Value::string(tu->name));
            fn.set("arguments", json::Value::string(tu->input));
            tcw.set("function", std::move(fn));
            tc_arr.push_back(std::move(tcw));
        } else if (const auto* ctu = std::get_if<ir::CustomToolUseBlock>(&b)) {
            json::Value tcw = json::Value::object();
            tcw.set("id", json::Value::string(ctu->id));
            tcw.set("type", json::Value::string("custom"));
            json::Value cu = json::Value::object();
            cu.set("name", json::Value::string(ctu->name));
            cu.set("input", json::Value::string(ctu->input));
            tcw.set("custom", std::move(cu));
            tc_arr.push_back(std::move(tcw));
        } else if (std::holds_alternative<ir::ApplyPatchToolUseBlock>(b)) {
            losses.push_back(make_cc_loss("content[" + std::to_string(i) + "]", "content",
                ir::LOSS_UNSUPPORTED_SEMANTIC, "IR apply_patch_tool_use block cannot be rendered in a Chat Completions assistant message"));
        } else if (std::holds_alternative<ir::ApplyPatchToolResultBlock>(b)) {
            losses.push_back(make_cc_loss("content[" + std::to_string(i) + "]", "content",
                ir::LOSS_UNSUPPORTED_SEMANTIC, "IR apply_patch_tool_result block cannot be rendered in a Chat Completions assistant message"));
        } else {
            losses.push_back(make_cc_loss(
                "content[" + std::to_string(i) + "]", "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "IR block cannot be rendered in a Chat Completions assistant response"));
        }
    }
    msg.set("content", json::Value::string(std::move(text)));
    if (has_reasoning) {
        msg.set("reasoning_content", json::Value::string(std::move(reasoning)));
    }
    const bool has_tool_calls = !tc_arr.as_array().empty();
    if (has_tool_calls) {
        msg.set("tool_calls", std::move(tc_arr));
    }

    std::string effective_reason = resp.stop_reason;
    if (effective_reason == ir::STOP_TOOL_USE && !has_tool_calls) {
        effective_reason = std::string(ir::STOP_END_TURN);
        losses.push_back(make_cc_loss("stop_reason", "stop_reason", ir::LOSS_DEGRADED,
            "tool_use was downgraded to end_turn because no supported function call remains"));
    }
    OXA_ASSIGN_OR_RETURN(auto fr_res, encode_finish_reason(effective_reason));
    if (fr_res.second.has_value()) losses.push_back(std::move(*fr_res.second));

    json::Value choice = json::Value::object();
    choice.set("index", json::Value::integer(0));
    choice.set("message", std::move(msg));
    choice.set("finish_reason", json::Value::string(fr_res.first));

    json::Value choices = json::Value::array();
    choices.push_back(std::move(choice));
    out.set("choices", std::move(choices));

    json::Value usage = json::Value::object();
    usage.set("prompt_tokens", json::Value::integer(resp.usage.input_tokens));
    usage.set("completion_tokens", json::Value::integer(resp.usage.output_tokens));
    usage.set("total_tokens", json::Value::integer(resp.usage.input_tokens + resp.usage.output_tokens));
    if (resp.usage.input_tokens_details.has_value()) {
        json::Value details = json::Value::object();
        details.set("cached_tokens", json::Value::integer(resp.usage.input_tokens_details->cached_tokens));
        usage.set("prompt_tokens_details", std::move(details));
    }
    if (resp.usage.output_tokens_details.has_value()) {
        json::Value details = json::Value::object();
        details.set("reasoning_tokens", json::Value::integer(resp.usage.output_tokens_details->reasoning_tokens));
        usage.set("completion_tokens_details", std::move(details));
    }
    out.set("usage", std::move(usage));

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

// ---- StreamDecoder --------------------------------------------------------

StreamDecoder::StreamDecoder(Options opts) : opts_(std::move(opts)) {}

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
    if (flushed_) return invalid_argument("chatcompletions: chunk fed after stream flush");
    if (!chunk.is_object()) return invalid_argument("chatcompletions: chunk must be an object");

    if (const auto* u = chunk.find("usage"); u && u->is_object()) {
        ir::Usage us;
        if (const auto* pt = u->find("prompt_tokens"); pt && pt->is_int()) us.input_tokens = pt->as_int();
        if (const auto* ct = u->find("completion_tokens"); ct && ct->is_int()) us.output_tokens = ct->as_int();
        if (const auto* details = u->find("prompt_tokens_details"); details && details->is_object()) {
            if (const auto* cached = details->find("cached_tokens"); cached && cached->is_int()) {
                us.input_tokens_details = ir::InputTokensDetails{cached->as_int()};
            }
        }
        if (const auto* details = u->find("completion_tokens_details"); details && details->is_object()) {
            if (const auto* reasoning = details->find("reasoning_tokens"); reasoning && reasoning->is_int()) {
                us.output_tokens_details = ir::OutputTokensDetails{reasoning->as_int()};
            }
        }
        usage_ = us;
    }

    const auto* choices = chunk.find("choices");
    if (!choices || !choices->is_array() || choices->as_array().empty()) {
        return std::vector<ir::Event>{};
    }

    const auto& choice = choices->as_array()[0];
    const auto* delta = choice.find("delta");
    if (!delta || !delta->is_object()) return std::vector<ir::Event>{};

    const auto* role_val = delta->find("role");
    if (started_ && role_val && role_val->is_string() && !role_val->as_string().empty()) {
        if (finish_seen_) {
            return invalid_argument("chatcompletions: chunk stream restarted after finish_reason");
        }
        return invalid_argument("chatcompletions: chunk stream already started");
    }

    std::vector<ir::Event> events;
    if (!started_) {
        started_ = true;
        if (const auto* id_val = chunk.find("id"); id_val && id_val->is_string()) id_ = id_val->as_string();
        if (const auto* m_val = chunk.find("model"); m_val && m_val->is_string()) {
            model_ = opts_.model_map.map(m_val->as_string());
        }
        events.push_back(ir::MessageStart{id_, model_});
    }

    // Process tool calls in delta
    if (const auto* tc_arr = delta->find("tool_calls"); tc_arr && tc_arr->is_array()) {
        for (const auto& tc : tc_arr->as_array()) {
            std::int64_t idx = 0;
            if (const auto* iv = tc.find("index"); iv && iv->is_int()) idx = iv->as_int();
            auto& record = tools_[idx];
            record.index = idx;
            if (const auto* idv = tc.find("id"); idv && idv->is_string()) record.id = idv->as_string();
            std::string t_type = "function";
            if (const auto* tv = tc.find("type"); tv && tv->is_string()) t_type = tv->as_string();
            if (t_type == "custom" || tc.find("custom")) record.tool_type = "custom";
            if (const auto* cu = tc.find("custom"); cu && cu->is_object()) {
                if (const auto* nv = cu->find("name"); nv && nv->is_string()) record.name += nv->as_string();
                if (const auto* iv = cu->find("input"); iv && iv->is_string()) {
                    std::string a = iv->as_string();
                    record.arguments += a;
                    record.fragments.push_back(std::move(a));
                }
            }
            if (const auto* fn = tc.find("function"); fn && fn->is_object()) {
                if (const auto* nv = fn->find("name"); nv && nv->is_string()) record.name += nv->as_string();
                if (const auto* av = fn->find("arguments"); av && av->is_string()) {
                    std::string a = av->as_string();
                    record.arguments += a;
                    record.fragments.push_back(std::move(a));
                }
            }
        }
    }

    if (const auto* reasoning = delta->find("reasoning_content"); reasoning && reasoning->is_string()) {
        if (text_open_) {
            events.push_back(ir::ContentBlockStop{text_index_});
            text_open_ = false;
        }
        if (!thinking_open_) {
            thinking_open_ = true;
            thinking_index_ = next_block_index_++;
            events.push_back(ir::ContentBlockStart{
                thinking_index_, ir::ThinkingBlock{"", std::nullopt}});
        }
        events.push_back(ir::ContentBlockDelta{
            thinking_index_, ir::ThinkingDelta{reasoning->as_string()}});
    }

    if (const auto* cv = delta->find("content"); cv && cv->is_string()) {
        std::string txt = cv->as_string();
        if (thinking_open_) {
            events.push_back(ir::ContentBlockStop{thinking_index_});
            thinking_open_ = false;
        }
        if (!text_open_) {
            text_open_ = true;
            text_index_ = next_block_index_++;
            events.push_back(ir::ContentBlockStart{text_index_, ir::TextBlock{""}});
        }
        events.push_back(ir::ContentBlockDelta{text_index_, ir::TextDelta{std::move(txt)}});
    }

    if (const auto* frv = choice.find("finish_reason"); frv && frv->is_string()) {
        if (finish_seen_) {
            return invalid_argument("chatcompletions: duplicate finish_reason");
        }
        finish_seen_ = true;
        finish_reason_ = frv->as_string();
    }

    return events;
}

StatusOr<std::vector<ir::Event>> StreamDecoder::flush() {
    if (flushed_) return invalid_argument("chatcompletions: stream flushed twice");
    if (!finish_seen_) return invalid_argument("chatcompletions: stream ended without finish_reason");
    flushed_ = true;

    std::vector<ir::Event> events;
    if (thinking_open_) {
        events.push_back(ir::ContentBlockStop{thinking_index_});
        thinking_open_ = false;
    }
    if (text_open_) {
        events.push_back(ir::ContentBlockStop{text_index_});
        text_open_ = false;
    }

    for (auto& [_, call] : tools_) {
        std::int64_t idx = next_block_index_++;
        if (call.tool_type == "custom") {
            events.push_back(ir::ContentBlockStart{
                idx, ir::CustomToolUseBlock{call.id, call.name, call.arguments}});
            for (const auto& frag : call.fragments) {
                events.push_back(ir::ContentBlockDelta{
                    idx, ir::CustomToolInputDelta{frag}});
            }
            events.push_back(ir::ContentBlockStop{idx});
        } else {
            events.push_back(ir::ContentBlockStart{
                idx, ir::ToolUseBlock{call.id, call.name, call.arguments}});
            for (const auto& frag : call.fragments) {
                events.push_back(ir::ContentBlockDelta{
                    idx, ir::InputJsonDelta{frag}});
            }
            events.push_back(ir::ContentBlockStop{idx});
        }
    }

    auto [stop_reason, fr_loss] = decode_finish_reason(finish_reason_);
    if (fr_loss.has_value()) losses_.push_back(std::move(*fr_loss));

    ir::Usage us = usage_.value_or(ir::Usage{0, 0});
    events.push_back(ir::MessageDelta{stop_reason, std::nullopt, us});
    events.push_back(ir::MessageDone{});

    return events;
}

// ---- StreamEncoder --------------------------------------------------------

StreamEncoder::StreamEncoder(Options opts) : opts_(std::move(opts)) {}

StatusOr<Conversion<std::vector<json::Value>>> StreamEncoder::apply(const ir::Event& event) {
    if (done_ || (finished_ && !std::holds_alternative<ir::MessageDone>(event))) {
        return invalid_argument("chatcompletions: event applied after stream termination");
    }

    std::vector<json::Value> chunks;
    std::vector<ir::Loss> losses;

    auto make_base_chunk = [&](json::Value delta, const std::string& finish_reason = "",
                               const std::optional<ir::Usage>& usage = std::nullopt) -> json::Value {
        json::Value chunk = json::Value::object();
        chunk.set("id", json::Value::string(id_));
        chunk.set("object", json::Value::string("chat.completion.chunk"));
        chunk.set("created", json::Value::integer(0));
        chunk.set("model", json::Value::string(model_));
        json::Value choices = json::Value::array();
        json::Value ch = json::Value::object();
        ch.set("index", json::Value::integer(0));
        ch.set("delta", std::move(delta));
        if (!finish_reason.empty()) {
            ch.set("finish_reason", json::Value::string(finish_reason));
        } else {
            ch.set("finish_reason", json::Value::null());
        }
        choices.push_back(std::move(ch));
        chunk.set("choices", std::move(choices));
        if (usage.has_value()) {
            json::Value u = json::Value::object();
            u.set("prompt_tokens", json::Value::integer(usage->input_tokens));
            u.set("completion_tokens", json::Value::integer(usage->output_tokens));
            u.set("total_tokens", json::Value::integer(usage->input_tokens + usage->output_tokens));
            if (usage->input_tokens_details.has_value()) {
                json::Value details = json::Value::object();
                details.set("cached_tokens", json::Value::integer(usage->input_tokens_details->cached_tokens));
                u.set("prompt_tokens_details", std::move(details));
            }
            if (usage->output_tokens_details.has_value()) {
                json::Value details = json::Value::object();
                details.set("reasoning_tokens", json::Value::integer(usage->output_tokens_details->reasoning_tokens));
                u.set("completion_tokens_details", std::move(details));
            }
            chunk.set("usage", std::move(u));
        }
        return chunk;
    };

    if (const auto* ms = std::get_if<ir::MessageStart>(&event)) {
        if (started_) return invalid_argument("chatcompletions: duplicate MessageStart");
        id_ = ms->id;
        model_ = opts_.model_map.map(ms->model);
        started_ = true;
        json::Value delta = json::Value::object();
        delta.set("role", json::Value::string("assistant"));
        chunks.push_back(make_base_chunk(std::move(delta)));
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    if (const auto* cbs = std::get_if<ir::ContentBlockStart>(&event)) {
        if (!started_ || active_.has_value() || finished_) {
            return invalid_argument("chatcompletions: ContentBlockStart out of grammar order");
        }
        if (cbs->index != next_ir_index_) {
            return invalid_argument("chatcompletions: ContentBlockStart index " + std::to_string(cbs->index) +
                                    ", want " + std::to_string(next_ir_index_));
        }
        next_ir_index_++;
        if (const auto* tb = std::get_if<ir::TextBlock>(&cbs->block)) {
            if (tool_seen_) {
                ordering_degrade_ = true;
            }
            ActiveBlock act;
            act.kind = ActiveBlock::Kind::Text;
            act.index = cbs->index;
            if (tb->citations.has_value() && !tb->citations->empty()) {
                act.citation_loss_seen = true;
                losses.push_back(make_cc_loss(
                    "events[" + std::to_string(cbs->index) + "].block.citations",
                    "citations", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "anthropic text citations; the target face cannot preserve source locations and replay metadata"));
            }
            active_ = std::move(act);
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (std::holds_alternative<ir::RedactedThinkingBlock>(cbs->block)) {
            ActiveBlock act;
            act.kind = ActiveBlock::Kind::Redacted;
            act.index = cbs->index;
            active_ = std::move(act);
            losses.push_back(make_cc_loss("events[" + std::to_string(cbs->index) + "].block", "type",
                ir::LOSS_UNSUPPORTED_SEMANTIC, "anthropic encrypted thinking replay state; the target face cannot preserve this block"));
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (std::holds_alternative<ir::ThinkingBlock>(cbs->block)) {
            ActiveBlock act;
            act.kind = ActiveBlock::Kind::Thinking;
            act.index = cbs->index;
            active_ = std::move(act);
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (const auto* tu = std::get_if<ir::ToolUseBlock>(&cbs->block)) {
            if (tu->id.empty() || tu->name.empty()) {
                return invalid_argument("chatcompletions: ToolUseBlock requires nonempty ID and name");
            }
            tool_seen_ = true;
            ActiveBlock act;
            act.kind = ActiveBlock::Kind::Tool;
            act.index = cbs->index;
            act.tool_id = tu->id;
            act.tool_name = tu->name;
            act.tool_input = tu->input;
            act.native_index = next_native_tool_++;
            active_ = std::move(act);
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (const auto* custom = std::get_if<ir::CustomToolUseBlock>(&cbs->block)) {
            if (custom->id.empty() || custom->name.empty()) {
                return invalid_argument("chatcompletions: CustomToolUseBlock requires nonempty ID and name");
            }
            tool_seen_ = true;
            ActiveBlock act;
            act.kind = ActiveBlock::Kind::CustomTool;
            act.index = cbs->index;
            act.tool_id = custom->id;
            act.tool_name = custom->name;
            act.tool_input = custom->input;
            act.native_index = next_native_tool_++;
            active_ = std::move(act);
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (const auto* ap = std::get_if<ir::ApplyPatchToolUseBlock>(&cbs->block)) {
            (void)ap;
            losses.push_back(make_cc_loss("events[" + std::to_string(cbs->index) + "].block", "type",
                ir::LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tools are not implemented by this profile"));
            ActiveBlock act; act.kind = ActiveBlock::Kind::SkippedAtomic; act.index = cbs->index;
            active_ = std::move(act);
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (const auto* apr = std::get_if<ir::ApplyPatchToolResultBlock>(&cbs->block)) {
            (void)apr;
            losses.push_back(make_cc_loss("events[" + std::to_string(cbs->index) + "].block", "type",
                ir::LOSS_UNSUPPORTED_SEMANTIC, "apply_patch tool results are not implemented by this profile"));
            ActiveBlock act; act.kind = ActiveBlock::Kind::SkippedAtomic; act.index = cbs->index;
            active_ = std::move(act);
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        return invalid_argument("chatcompletions: ContentBlockStart carries unsupported block");
    }

    if (const auto* cbd = std::get_if<ir::ContentBlockDelta>(&event)) {
        if (!active_.has_value() || cbd->index != active_->index) {
            return invalid_argument("chatcompletions: ContentBlockDelta out of grammar order");
        }
        if (active_->kind == ActiveBlock::Kind::SkippedAtomic) {
            return invalid_argument("chatcompletions: atomic block does not accept deltas");
        }
        if (active_->kind == ActiveBlock::Kind::CustomTool) {
            const auto* ctd = std::get_if<ir::CustomToolInputDelta>(&cbd->delta);
            if (!ctd) return invalid_argument("chatcompletions: CustomToolUseBlock received non-custom delta");
            active_->fragments.push_back(ctd->text);

            json::Value tc_delta = json::Value::object();
            tc_delta.set("index", json::Value::integer(static_cast<std::int64_t>(active_->native_index)));
            json::Value cobj = json::Value::object();
            cobj.set("input", json::Value::string(ctd->text));
            if (!active_->tool_started) {
                active_->tool_started = true;
                tc_delta.set("id", json::Value::string(active_->tool_id));
                tc_delta.set("type", json::Value::string("custom"));
                cobj.set("name", json::Value::string(active_->tool_name));
            }
            tc_delta.set("custom", std::move(cobj));
            json::Value tcs = json::Value::array();
            tcs.push_back(std::move(tc_delta));
            json::Value delta = json::Value::object();
            delta.set("tool_calls", std::move(tcs));
            pending_tools_.push_back(make_base_chunk(std::move(delta)));
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (active_->kind == ActiveBlock::Kind::Redacted) {
            return invalid_argument("chatcompletions: RedactedThinkingBlock received unexpected delta");
        }
        if (active_->kind == ActiveBlock::Kind::Text) {
            if (const auto* td = std::get_if<ir::TextDelta>(&cbd->delta)) {
                json::Value delta = json::Value::object();
                delta.set("content", json::Value::string(td->text));
                chunks.push_back(make_base_chunk(std::move(delta)));
                return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
            }
            if (std::holds_alternative<ir::CitationDelta>(cbd->delta)) {
                if (!active_->citation_loss_seen) {
                    active_->citation_loss_seen = true;
                    losses.push_back(make_cc_loss(
                        "events[" + std::to_string(cbd->index) + "].delta.citation",
                        "citations", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "anthropic text citations; the target face cannot preserve source locations and replay metadata"));
                }
                return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
            }
            return invalid_argument("chatcompletions: TextBlock received non-text delta");
        }
        if (active_->kind == ActiveBlock::Kind::Thinking) {
            if (const auto* td = std::get_if<ir::ThinkingDelta>(&cbd->delta)) {
                json::Value delta = json::Value::object();
                delta.set("reasoning_content", json::Value::string(td->text));
                chunks.push_back(make_base_chunk(std::move(delta)));
                return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
            }
            if (std::holds_alternative<ir::SignatureDelta>(cbd->delta)) {
                losses.push_back(make_cc_loss(
                    "events[" + std::to_string(cbd->index) + "].delta.signature",
                    "signature", ir::LOSS_UNMAPPED_FIELD,
                    "Chat Completions carries no signature delta"));
                return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
            }
            return invalid_argument("chatcompletions: ThinkingBlock received non-thinking delta");
        }
        if (active_->kind == ActiveBlock::Kind::Tool) {
            const auto* ij = std::get_if<ir::InputJsonDelta>(&cbd->delta);
            if (!ij) return invalid_argument("chatcompletions: ToolUseBlock received non-input-json delta");
            active_->fragments.push_back(ij->partial_json);
            json::Value delta = json::Value::object();
            json::Value tcs = json::Value::array();
            json::Value tc = json::Value::object();
            tc.set("index", json::Value::integer(active_->native_index));
            if (!active_->tool_started) {
                active_->tool_started = true;
                tc.set("id", json::Value::string(active_->tool_id));
                tc.set("type", json::Value::string("function"));
                json::Value fn = json::Value::object();
                fn.set("name", json::Value::string(active_->tool_name));
                fn.set("arguments", json::Value::string(ij->partial_json));
                tc.set("function", std::move(fn));
            } else {
                json::Value fn = json::Value::object();
                fn.set("arguments", json::Value::string(ij->partial_json));
                tc.set("function", std::move(fn));
            }
            tcs.push_back(std::move(tc));
            delta.set("tool_calls", std::move(tcs));
            pending_tools_.push_back(make_base_chunk(std::move(delta)));
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        return invalid_argument("chatcompletions: unknown active block kind");
    }

    if (const auto* cst = std::get_if<ir::ContentBlockStop>(&event)) {
        if (!active_.has_value() || cst->index != active_->index) {
            return invalid_argument("chatcompletions: ContentBlockStop out of grammar order");
        }
        if (active_->kind == ActiveBlock::Kind::SkippedCustom || active_->kind == ActiveBlock::Kind::SkippedAtomic ||
            active_->kind == ActiveBlock::Kind::Redacted) {
            active_.reset();
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (active_->kind == ActiveBlock::Kind::CustomTool) {
            if (active_->fragments.empty()) {
                active_->fragments.push_back(active_->tool_input);
                json::Value delta = json::Value::object();
                json::Value tcs = json::Value::array();
                json::Value tc = json::Value::object();
                tc.set("index", json::Value::integer(static_cast<std::int64_t>(active_->native_index)));
                tc.set("id", json::Value::string(active_->tool_id));
                tc.set("type", json::Value::string("custom"));
                json::Value cobj = json::Value::object();
                cobj.set("name", json::Value::string(active_->tool_name));
                cobj.set("input", json::Value::string(active_->tool_input));
                tc.set("custom", std::move(cobj));
                tcs.push_back(std::move(tc));
                delta.set("tool_calls", std::move(tcs));
                pending_tools_.push_back(make_base_chunk(std::move(delta)));
            } else {
                std::string joined;
                for (const auto& f : active_->fragments) joined += f;
                if (joined != active_->tool_input) {
                    return invalid_argument("chatcompletions: CustomToolUseBlock input does not equal concatenated fragments");
                }
            }
            active_.reset();
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
        }
        if (active_->kind == ActiveBlock::Kind::Tool) {
            if (active_->fragments.empty()) {
                active_->fragments.push_back(active_->tool_input);
                json::Value delta = json::Value::object();
                json::Value tcs = json::Value::array();
                json::Value tc = json::Value::object();
                tc.set("index", json::Value::integer(active_->native_index));
                tc.set("id", json::Value::string(active_->tool_id));
                tc.set("type", json::Value::string("function"));
                json::Value fn = json::Value::object();
                fn.set("name", json::Value::string(active_->tool_name));
                fn.set("arguments", json::Value::string(active_->tool_input));
                tc.set("function", std::move(fn));
                tcs.push_back(std::move(tc));
                delta.set("tool_calls", std::move(tcs));
                pending_tools_.push_back(make_base_chunk(std::move(delta)));
            } else {
                std::string joined;
                for (const auto& f : active_->fragments) joined += f;
                if (joined != active_->tool_input) {
                    return invalid_argument("chatcompletions: ToolUseBlock input does not equal concatenated InputJSONDelta fragments");
                }
            }
        }
        active_.reset();
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    if (const auto* md = std::get_if<ir::MessageDelta>(&event)) {
        if (!started_ || active_.has_value() || finished_) {
            return invalid_argument("chatcompletions: MessageDelta out of grammar order");
        }
        std::string effective_reason = md->stop_reason;
        if (effective_reason == ir::STOP_TOOL_USE && !tool_seen_) {
            effective_reason = std::string(ir::STOP_END_TURN);
            losses.push_back(make_cc_loss("stop_reason", "stop_reason", ir::LOSS_DEGRADED,
                "tool_use was downgraded to end_turn because no supported function call remains"));
        }
        OXA_ASSIGN_OR_RETURN(auto fr_res, encode_finish_reason(effective_reason));
        if (fr_res.second.has_value()) losses.push_back(std::move(*fr_res.second));
        if (ordering_degrade_) {
            losses.push_back(make_cc_loss(
                "events", "ordering", ir::LOSS_DEGRADED,
                "N-S-10: the text block after a tool block is normalized ahead of the tool calls; IR source order is not preserved"));
        }
        finished_ = true;
        chunks = std::move(pending_tools_);
        json::Value delta = json::Value::object();
        chunks.push_back(make_base_chunk(std::move(delta), fr_res.first, md->usage));
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    if (std::holds_alternative<ir::MessageDone>(event)) {
        if (!finished_) return invalid_argument("chatcompletions: MessageDone out of grammar order");
        done_ = true;
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    return invalid_argument("chatcompletions: unknown event");
}

}  // namespace oxa::openai::chatcompletions
