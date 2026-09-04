#include "oxa/openai/chatcompletions.hpp"

#include <algorithm>

namespace oxa::openai::chatcompletions {
namespace {

ir::Loss make_cc_loss(std::string path, std::string field, std::string_view reason,
                      std::string detail) {
    return ir::make_loss(std::move(path), std::move(field), reason, std::move(detail));
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

StatusOr<Conversion<ir::Request>> decode_request(const json::Value& wire,
                                                 const Options& opts) {
    if (!wire.is_object()) return invalid_argument("chatcompletions: request must be an object");
    std::vector<ir::Loss> losses;

    const char* dropped_fields[] = {
        "parallel_tool_calls", "functions", "function_call", "response_format",
        "logprobs", "top_logprobs", "metadata"};
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

                ir::ToolResultBlock tr;
                if (const auto* tcid = cur.find("tool_call_id"); tcid && tcid->is_string()) {
                    tr.tool_use_id = tcid->as_string();
                }
                for (auto& b : c_res.first) {
                    tr.content.push_back(ir::BlockHolder{std::move(b)});
                }
                tr_blocks.push_back(std::move(tr));

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
            auto [tc_blocks, tc_losses] =
                decode_tool_calls(m.find("tool_calls"), "messages[" + std::to_string(index) + "].tool_calls");
            const auto* c_val = m.find("content");
            if ((!c_val || c_val->is_null()) && !tc_blocks.empty()) {
                content.clear();
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
    if (const auto* mt = wire.find("max_tokens"); mt && mt->is_int()) {
        params.max_tokens = mt->as_int();
        has_params = true;
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
    if (has_params) req.params = std::move(params);

    return Conversion<ir::Request>{std::move(req), std::move(losses)};
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
        for (const auto& t : *req.tools) {
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
        out.set("tools", std::move(tools_arr));
    }

    if (req.tool_choice.has_value()) {
        const auto& tc = *req.tool_choice;
        if (tc.mode == ir::TOOL_CHOICE_AUTO) {
            out.set("tool_choice", json::Value::string("auto"));
        } else if (tc.mode == ir::TOOL_CHOICE_NONE) {
            out.set("tool_choice", json::Value::string("none"));
        } else if (tc.mode == ir::TOOL_CHOICE_ANY) {
            out.set("tool_choice", json::Value::string("required"));
        } else if (tc.mode == ir::TOOL_CHOICE_TOOL) {
            if (tc.name.has_value() && !tc.name->empty()) {
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
            json::Value tc_arr = json::Value::array();
            for (std::size_t bi = 0; bi < m.content.size(); ++bi) {
                const auto& b = m.content[bi];
                if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
                    text += tb->text;
                } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
                    json::Value tcw = json::Value::object();
                    tcw.set("id", json::Value::string(tu->id));
                    tcw.set("type", json::Value::string("function"));
                    json::Value fn = json::Value::object();
                    fn.set("name", json::Value::string(tu->name));
                    fn.set("arguments", json::Value::string(tu->input));
                    tcw.set("function", std::move(fn));
                    tc_arr.push_back(std::move(tcw));
                } else {
                    losses.push_back(make_cc_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(bi) + "]",
                        "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                        "IR image block cannot be rendered in a Chat Completions assistant message"));
                }
            }
            am.set("content", json::Value::string(std::move(text)));
            if (!tc_arr.as_array().empty()) {
                am.set("tool_calls", std::move(tc_arr));
            }
            messages_arr.push_back(std::move(am));
        } else {
            // User turn: split tool results and normal blocks (N-CC-9 hoisting)
            std::vector<const ir::ToolResultBlock*> results;
            std::vector<const ir::Block*> normal;
            std::optional<std::size_t> first_normal;
            std::optional<std::size_t> last_result;

            for (std::size_t bi = 0; bi < m.content.size(); ++bi) {
                const auto& b = m.content[bi];
                if (const auto* tr = std::get_if<ir::ToolResultBlock>(&b)) {
                    results.push_back(tr);
                    last_result = bi;
                } else {
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
                const auto* tr = results[ri];
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
                    um.set("content", std::move(parts));
                }
                messages_arr.push_back(std::move(um));
            }
        }
    }
    out.set("messages", std::move(messages_arr));

    if (req.params.has_value()) {
        const auto& p = *req.params;
        if (p.temperature.has_value()) out.set("temperature", json::Value::real(*p.temperature));
        if (p.top_p.has_value()) out.set("top_p", json::Value::real(*p.top_p));
        if (p.max_tokens.has_value()) out.set("max_tokens", json::Value::integer(*p.max_tokens));
        if (p.stop_sequences.has_value() && !p.stop_sequences->empty()) {
            json::Value stops = json::Value::array();
            for (const auto& s : *p.stop_sequences) stops.push_back(json::Value::string(s));
            out.set("stop", std::move(stops));
        }
    }

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

StatusOr<Conversion<ir::Response>> decode_response(const json::Value& wire,
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

    auto [tool_blocks, tool_losses] = decode_tool_calls(msg->find("tool_calls"), "choices[0].message.tool_calls");
    const auto* content_val = msg->find("content");
    if ((!content_val || content_val->is_null()) && !tool_blocks.empty()) {
        blocks.clear();
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
    }

    return Conversion<ir::Response>{std::move(resp), std::move(losses)};
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
    json::Value tc_arr = json::Value::array();
    for (std::size_t i = 0; i < resp.content.size(); ++i) {
        const auto& b = resp.content[i];
        if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
            text += tb->text;
        } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
            json::Value tcw = json::Value::object();
            tcw.set("id", json::Value::string(tu->id));
            tcw.set("type", json::Value::string("function"));
            json::Value fn = json::Value::object();
            fn.set("name", json::Value::string(tu->name));
            fn.set("arguments", json::Value::string(tu->input));
            tcw.set("function", std::move(fn));
            tc_arr.push_back(std::move(tcw));
        } else {
            losses.push_back(make_cc_loss(
                "content[" + std::to_string(i) + "]", "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "IR block cannot be rendered in a Chat Completions assistant response"));
        }
    }
    msg.set("content", json::Value::string(std::move(text)));
    if (!tc_arr.as_array().empty()) {
        msg.set("tool_calls", std::move(tc_arr));
    }

    OXA_ASSIGN_OR_RETURN(auto fr_res, encode_finish_reason(resp.stop_reason));
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
    out.set("usage", std::move(usage));

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

// ---- StreamDecoder --------------------------------------------------------

StreamDecoder::StreamDecoder(Options opts) : opts_(std::move(opts)) {}

StatusOr<std::vector<ir::Event>> StreamDecoder::feed(const json::Value& chunk) {
    if (flushed_) return invalid_argument("chatcompletions: chunk fed after stream flush");
    if (!chunk.is_object()) return invalid_argument("chatcompletions: chunk must be an object");

    if (const auto* u = chunk.find("usage"); u && u->is_object()) {
        ir::Usage us;
        if (const auto* pt = u->find("prompt_tokens"); pt && pt->is_int()) us.input_tokens = pt->as_int();
        if (const auto* ct = u->find("completion_tokens"); ct && ct->is_int()) us.output_tokens = ct->as_int();
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

    if (const auto* cv = delta->find("content"); cv && cv->is_string()) {
        std::string txt = cv->as_string();
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
    if (text_open_) {
        events.push_back(ir::ContentBlockStop{text_index_});
        text_open_ = false;
    }

    for (auto& [_, call] : tools_) {
        std::int64_t idx = next_block_index_++;
        events.push_back(ir::ContentBlockStart{
            idx, ir::ToolUseBlock{call.id, call.name, call.arguments}});
        for (const auto& frag : call.fragments) {
            events.push_back(ir::ContentBlockDelta{
                idx, ir::InputJsonDelta{frag}});
        }
        events.push_back(ir::ContentBlockStop{idx});
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
        if (std::holds_alternative<ir::TextBlock>(cbs->block)) {
            if (tool_seen_) {
                ordering_degrade_ = true;
            }
            ActiveBlock act;
            act.kind = ActiveBlock::Kind::Text;
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
        return invalid_argument("chatcompletions: ContentBlockStart carries unsupported block");
    }

    if (const auto* cbd = std::get_if<ir::ContentBlockDelta>(&event)) {
        if (!active_.has_value() || cbd->index != active_->index) {
            return invalid_argument("chatcompletions: ContentBlockDelta out of grammar order");
        }
        if (active_->kind == ActiveBlock::Kind::Text) {
            const auto* td = std::get_if<ir::TextDelta>(&cbd->delta);
            if (!td) return invalid_argument("chatcompletions: TextBlock received non-text delta");
            json::Value delta = json::Value::object();
            delta.set("content", json::Value::string(td->text));
            chunks.push_back(make_base_chunk(std::move(delta)));
            return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
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
        OXA_ASSIGN_OR_RETURN(auto fr_res, encode_finish_reason(md->stop_reason));
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
