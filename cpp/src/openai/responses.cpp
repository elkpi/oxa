#include "oxa/openai/responses.hpp"

namespace oxa::openai::responses {
namespace {

ir::Loss make_resp_loss(std::string path, std::string field, std::string_view reason,
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
        std::string name;
        if (const auto* nv = tc->find("name"); nv && nv->is_string()) name = nv->as_string();
        if (kind == "function" && !name.empty()) {
            return {ir::ToolChoice{std::string(ir::TOOL_CHOICE_TOOL), name}, {}};
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

}  // namespace

StatusOr<Conversion<ir::Request>> decode_request(const json::Value& wire,
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
        if (txt->find("format") != nullptr) {
            losses.push_back(make_resp_loss("text.format", "format", ir::LOSS_UNMAPPED_FIELD,
                                            "Responses text output format has no IR equivalent in v1."));
        }
    }
    if (wire.find("reasoning") != nullptr) {
        losses.push_back(make_resp_loss("reasoning", "reasoning", ir::LOSS_UNMAPPED_FIELD,
                                        "Responses reasoning effort configuration has no IR equivalent in v1."));
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

    if (const auto* tools_val = wire.find("tools"); tools_val && tools_val->is_array()) {
        std::vector<ir::Tool> tools;
        for (std::size_t i = 0; i < tools_val->as_array().size(); ++i) {
            const auto& t = tools_val->as_array()[i];
            const auto* type_val = t.find("type");
            std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";
            if (kind != "function") {
                losses.push_back(make_resp_loss(
                    "tools[" + std::to_string(i) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "Responses tool type \"" + kind + "\" has no IR equivalent"));
                continue;
            }
            ir::Tool tool;
            if (const auto* nv = t.find("name"); nv && nv->is_string()) tool.name = nv->as_string();
            if (const auto* dv = t.find("description"); dv && dv->is_string() && !dv->as_string().empty()) {
                tool.description = dv->as_string();
                tool.has_description = true;
            }
            if (const auto* pv = t.find("parameters")) {
                tool.input_schema = *pv;
            } else {
                tool.input_schema = json::Value::null();
            }
            if (t.find("strict") != nullptr) {
                losses.push_back(make_resp_loss(
                    "tools[" + std::to_string(i) + "].strict", "strict", ir::LOSS_UNMAPPED_FIELD,
                    "Responses function tool strict mode has no IR equivalent in v1."));
            }
            tools.push_back(std::move(tool));
        }
        if (!tools.empty()) req.tools = std::move(tools);
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
        std::size_t index = 0;
        while (index < items.size()) {
            const auto& item = items[index];
            const auto* type_val = item.find("type");
            std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";

            if (kind.empty() || kind == "message") {
                const auto* role_val = item.find("role");
                std::string role = role_val && role_val->is_string() ? role_val->as_string() : "";
                if (role == "system") {
                    std::string path = "input[" + std::to_string(index) + "].content";
                    auto [blocks, bl_losses] = decode_content(item.find("content"), path);
                    losses.insert(losses.end(), bl_losses.begin(), bl_losses.end());
                    for (auto& b : blocks) {
                        if (auto* tb = std::get_if<ir::TextBlock>(&b)) {
                            req.system.push_back(std::move(*tb));
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
                        if (ckind == "function_call") {
                            ir::ToolUseBlock tu;
                            if (const auto* idv = cur.find("call_id"); idv && idv->is_string()) tu.id = idv->as_string();
                            if (const auto* nv = cur.find("name"); nv && nv->is_string()) tu.name = nv->as_string();
                            if (const auto* av = cur.find("arguments"); av && av->is_string()) tu.input = av->as_string();
                            call_blocks.push_back(std::move(tu));
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
            } else if (kind == "function_call") {
                // Standalone function call starting an assistant run
                std::vector<ir::Block> text_blocks;
                std::vector<ir::Block> call_blocks;
                while (index < items.size()) {
                    const auto& cur = items[index];
                    const auto* ct = cur.find("type");
                    std::string ckind = ct && ct->is_string() ? ct->as_string() : "";
                    if (ckind == "function_call") {
                        ir::ToolUseBlock tu;
                        if (const auto* idv = cur.find("call_id"); idv && idv->is_string()) tu.id = idv->as_string();
                        if (const auto* nv = cur.find("name"); nv && nv->is_string()) tu.name = nv->as_string();
                        if (const auto* av = cur.find("arguments"); av && av->is_string()) tu.input = av->as_string();
                        call_blocks.push_back(std::move(tu));
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
            } else if (kind == "function_call_output") {
                // Maximal run of function_call_output items merged into one user message
                std::vector<ir::Block> tr_blocks;
                while (index < items.size()) {
                    const auto& cur = items[index];
                    const auto* ct = cur.find("type");
                    std::string ckind = ct && ct->is_string() ? ct->as_string() : "";
                    if (ckind != "function_call_output") break;

                    ir::ToolResultBlock tr;
                    if (const auto* cid = cur.find("call_id"); cid && cid->is_string()) tr.tool_use_id = cid->as_string();
                    std::string out_txt;
                    if (const auto* ov = cur.find("output"); ov && ov->is_string()) out_txt = ov->as_string();
                    tr.content.push_back(ir::BlockHolder{ir::TextBlock{std::move(out_txt)}});
                    tr_blocks.push_back(std::move(tr));
                    ++index;
                }
                req.messages.push_back(ir::Message{std::string(ir::ROLE_USER), std::move(tr_blocks)});
            } else {
                losses.push_back(make_resp_loss(
                    "input[" + std::to_string(index) + "]", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "Responses input item type \"" + kind + "\" has no IR equivalent"));
                ++index;
            }
        }
    }

    if (req.messages.empty()) {
        return invalid_argument("responses: request carries no conversation input");
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
    if (const auto* mo = wire.find("max_output_tokens"); mo && mo->is_int()) {
        params.max_tokens = mo->as_int();
        has_params = true;
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

    if (req.tools.has_value() && !req.tools->empty()) {
        json::Value tools_arr = json::Value::array();
        for (const auto& t : *req.tools) {
            json::Value tw = json::Value::object();
            tw.set("type", json::Value::string("function"));
            tw.set("name", json::Value::string(t.name));
            tw.set("description", json::Value::string(t.description));
            tw.set("parameters", t.input_schema.is_null() ? json::Value::object() : t.input_schema);
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
                tco.set("name", json::Value::string(*tc.name));
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
                losses.push_back(make_resp_loss(
                    "messages[" + std::to_string(i) + "]", "ordering", ir::LOSS_DEGRADED,
                    "N-R-10: function_call_output items are hoisted ahead of the trailing user content; source order is not preserved"));
            }

            for (std::size_t ri = 0; ri < results.size(); ++ri) {
                const auto* tr = results[ri];
                json::Value fco = json::Value::object();
                fco.set("type", json::Value::string("function_call_output"));
                fco.set("call_id", json::Value::string(tr->tool_use_id));
                std::string res_txt;
                for (const auto& inner : tr->content) {
                    if (const auto* itb = std::get_if<ir::TextBlock>(&inner.block)) {
                        res_txt += itb->text;
                    }
                }
                fco.set("output", json::Value::string(std::move(res_txt)));
                if (tr->is_error) {
                    losses.push_back(make_resp_loss(
                        "messages[" + std::to_string(i) + "].content[" + std::to_string(ri) + "].is_error",
                        "is_error", ir::LOSS_UNMAPPED_FIELD,
                        "Responses function_call_output items have no is_error field"));
                }
                items.push_back(std::move(fco));
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
            // Assistant turn: text parts then function_call items
            std::string a_txt;
            bool has_text = false;
            std::vector<json::Value> fc_items;

            for (const auto& b : m.content) {
                if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
                    a_txt += tb->text;
                    has_text = true;
                } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
                    json::Value fc = json::Value::object();
                    fc.set("type", json::Value::string("function_call"));
                    fc.set("call_id", json::Value::string(tu->id));
                    fc.set("name", json::Value::string(tu->name));
                    fc.set("arguments", json::Value::string(tu->input));
                    fc_items.push_back(std::move(fc));
                }
            }
            if (has_text || fc_items.empty()) {
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
        if (p.stop_sequences.has_value() && !p.stop_sequences->empty()) {
            losses.push_back(make_resp_loss(
                "params.stop_sequences", "stop_sequences", ir::LOSS_UNMAPPED_FIELD,
                "Responses requests have no stop-sequences parameter; the IR stop sequences are dropped."));
        }
    }

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

StatusOr<Conversion<ir::Response>> decode_response(const json::Value& wire,
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
            } else if (kind == "function_call") {
                ir::ToolUseBlock tu;
                if (const auto* cid = item.find("call_id"); cid && cid->is_string()) tu.id = cid->as_string();
                if (const auto* nv = item.find("name"); nv && nv->is_string()) tu.name = nv->as_string();
                if (const auto* av = item.find("arguments"); av && av->is_string()) tu.input = av->as_string();
                resp.content.push_back(std::move(tu));
                has_tool_use = true;
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
    out.set("object", json::Value::string("response"));
    out.set("model", json::Value::string(opts.model_map.map(resp.model)));

    std::string text;
    bool has_text = false;
    std::vector<json::Value> calls;

    for (std::size_t i = 0; i < resp.content.size(); ++i) {
        const auto& b = resp.content[i];
        if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
            text += tb->text;
            has_text = true;
        } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
            json::Value fc = json::Value::object();
            fc.set("type", json::Value::string("function_call"));
            fc.set("id", json::Value::string("fc_abc123"));
            fc.set("status", json::Value::string("completed"));
            fc.set("call_id", json::Value::string(tu->id));
            fc.set("name", json::Value::string(tu->name));
            fc.set("arguments", json::Value::string(tu->input));
            calls.push_back(std::move(fc));
        } else {
            losses.push_back(make_resp_loss(
                "content[" + std::to_string(i) + "]", "content", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "this IR block cannot be rendered in a Responses output item"));
        }
    }

    json::Value output_arr = json::Value::array();
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

StatusOr<std::vector<ir::Event>> StreamDecoder::feed(const json::Value& chunk) {
    if (flushed_) return invalid_argument("responses: event fed after stream flush");
    if (terminated_) return invalid_argument("responses: event fed after terminal response");
    if (!chunk.is_object()) return invalid_argument("responses: event must be an object");

    const auto* t = chunk.find("type");
    std::string type = t && t->is_string() ? t->as_string() : "";

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

        std::string role;
        if (const auto* rv = item->find("role"); rv && rv->is_string()) role = rv->as_string();

        if (ikind == "message" && role == "assistant") {
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

        auto [stop_reason, s_losses] = decode_status(*resp, tool_use_seen_);
        losses_.insert(losses_.end(), s_losses.begin(), s_losses.end());
        terminated_ = true;

        ir::Usage usage;
        if (const auto* uv = resp->find("usage"); uv && uv->is_object()) {
            if (const auto* in = uv->find("input_tokens"); in && in->is_int()) usage.input_tokens = in->as_int();
            if (const auto* out = uv->find("output_tokens"); out && out->is_int()) usage.output_tokens = out->as_int();
        }

        events.push_back(ir::MessageDelta{stop_reason, std::nullopt, usage});
        events.push_back(ir::MessageDone{});
        return events;
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
    if (!active_item_.has_value()) {
        auto [item, added] = open_message_item();
        active_item_ = std::move(item);
        out.push_back(std::move(added));
    }
    if (active_item_->kind != OutputItemKind::Message) {
        return invalid_argument("responses: TextBlock cannot open before the active function_call item completes");
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

StatusOr<std::pair<std::vector<json::Value>, std::vector<ir::Loss>>> StreamEncoder::start_function_call_block(
    std::int64_t index, const ir::ToolUseBlock& block) {
    if (block.id.empty() || block.name.empty()) {
        return invalid_argument("responses: ToolUseBlock requires nonempty ID and name");
    }
    std::vector<json::Value> out;
    if (active_item_.has_value()) {
        if (active_item_->kind != OutputItemKind::Message) {
            return invalid_argument("responses: ToolUseBlock cannot open before the active function_call item completes");
        }
        out.push_back(close_message_item());
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
        if (const auto* tu = std::get_if<ir::ToolUseBlock>(&cbs->block)) {
            OXA_ASSIGN_OR_RETURN(auto res, start_function_call_block(cbs->index, *tu));
            return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
        }
        return invalid_argument("responses: ContentBlockStart carries unsupported block");
    }

    if (const auto* cbd = std::get_if<ir::ContentBlockDelta>(&event)) {
        if (!active_block_.has_value() || cbd->index != active_block_->index) {
            return invalid_argument("responses: ContentBlockDelta out of grammar order");
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
        OXA_ASSIGN_OR_RETURN(auto res, stop_function_call_block());
        return Conversion<std::vector<json::Value>>{std::move(res.first), std::move(res.second)};
    }

    if (const auto* md = std::get_if<ir::MessageDelta>(&event)) {
        if (!started_ || active_block_.has_value() || delta_) {
            return invalid_argument("responses: MessageDelta out of grammar order");
        }
        std::vector<json::Value> out;
        if (active_item_.has_value()) {
            if (active_item_->kind != OutputItemKind::Message) {
                return invalid_argument("responses: MessageDelta with an uncompleted function_call item");
            }
            out.push_back(close_message_item());
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
