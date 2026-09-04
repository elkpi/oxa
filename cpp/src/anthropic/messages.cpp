#include "oxa/anthropic/messages.hpp"

namespace oxa::anthropic::messages {
namespace {

ir::Loss make_ant_loss(std::string path, std::string field, std::string_view reason,
                       std::string detail) {
    return ir::make_loss(std::move(path), std::move(field), reason, std::move(detail));
}

StatusOr<std::pair<std::vector<ir::Block>, std::vector<ir::Loss>>> decode_content_blocks(
    const json::Value* content, const std::string& path, bool keep_raw_input = false) {
    std::vector<ir::Block> blocks;
    std::vector<ir::Loss> losses;
    if (!content || content->is_null()) {
        return std::make_pair(blocks, losses);
    }
    if (content->is_string()) {
        blocks.push_back(ir::TextBlock{content->as_string()});
        return std::make_pair(std::move(blocks), std::move(losses));
    }
    if (!content->is_array()) {
        return invalid_argument(path + ": content must be a string or array of blocks");
    }

    const auto& arr = content->as_array();
    for (std::size_t i = 0; i < arr.size(); ++i) {
        const auto& item = arr[i];
        if (!item.is_object()) {
            return invalid_argument(path + "[" + std::to_string(i) + "]: block must be an object");
        }
        std::string block_path = path + "[" + std::to_string(i) + "]";

        const auto* t = item.find("type");
        std::string kind = t && t->is_string() ? t->as_string() : "";
        if (kind == "text") {
            const auto* txt = item.find("text");
            std::string text = txt && txt->is_string() ? txt->as_string() : "";
            blocks.push_back(ir::TextBlock{std::move(text)});
        } else if (kind == "image") {
            const auto* src = item.find("source");
            if (!src || !src->is_object()) {
                return invalid_argument(block_path + ": image missing source object");
            }
            const auto* st = src->find("type");
            std::string stype = st && st->is_string() ? st->as_string() : "";
            if (stype == "base64") {
                ir::ImageBlock img;
                if (const auto* mt = src->find("media_type"); mt && mt->is_string()) img.media_type = mt->as_string();
                if (const auto* dt = src->find("data"); dt && dt->is_string()) img.data = dt->as_string();
                blocks.push_back(std::move(img));
            } else if (stype == "url") {
                ir::ImageBlock img;
                if (const auto* u = src->find("url"); u && u->is_string()) img.url = u->as_string();
                blocks.push_back(std::move(img));
            } else {
                losses.push_back(make_ant_loss(
                    block_path + ".source.type", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                    "Anthropic image source type \"" + stype + "\" has no IR equivalent"));
            }
        } else if (kind == "tool_use") {
            ir::ToolUseBlock tu;
            if (const auto* id_val = item.find("id"); id_val && id_val->is_string()) tu.id = id_val->as_string();
            if (const auto* name_val = item.find("name"); name_val && name_val->is_string()) tu.name = name_val->as_string();
            const auto* inp = item.find("input");
            if (inp) {
                if (keep_raw_input) {
                    std::string_view raw = inp->raw_slice();
                    if (!raw.empty()) {
                        tu.input = std::string(raw);
                    } else {
                        tu.input = json::serialize(*inp);
                    }
                } else {
                    tu.input = json::serialize(*inp);
                }
            } else {
                tu.input = "{}";
            }
            blocks.push_back(std::move(tu));
        } else if (kind == "tool_result") {
            ir::ToolResultBlock tr;
            if (const auto* tcid = item.find("tool_use_id"); tcid && tcid->is_string()) {
                tr.tool_use_id = tcid->as_string();
            }
            if (const auto* ie = item.find("is_error"); ie && ie->is_bool()) {
                tr.is_error = ie->as_bool();
            }
            const auto* c_val = item.find("content");
            if (c_val) {
                OXA_ASSIGN_OR_RETURN(auto inner_res, decode_content_blocks(c_val, block_path + ".content"));
                losses.insert(losses.end(), inner_res.second.begin(), inner_res.second.end());
                for (auto& b : inner_res.first) {
                    tr.content.push_back(ir::BlockHolder{std::move(b)});
                }
            }
            blocks.push_back(std::move(tr));
        } else {
            losses.push_back(make_ant_loss(
                block_path, "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "Anthropic block type \"" + kind + "\" has no IR equivalent"));
            continue;
        }

        if (item.find("cache_control") != nullptr) {
            losses.push_back(make_ant_loss(
                block_path + ".cache_control", "cache_control", ir::LOSS_UNMAPPED_FIELD,
                "Anthropic prompt caching annotations have no IR equivalent in v1."));
        }
    }
    return std::make_pair(std::move(blocks), std::move(losses));
}

std::pair<std::string, std::optional<ir::Loss>> decode_stop_reason(std::string_view sr) {
    if (sr == "end_turn") return {std::string(ir::STOP_END_TURN), std::nullopt};
    if (sr == "max_tokens") return {std::string(ir::STOP_MAX_TOKENS), std::nullopt};
    if (sr == "stop_sequence") return {std::string(ir::STOP_STOP_SEQUENCE), std::nullopt};
    if (sr == "tool_use") return {std::string(ir::STOP_TOOL_USE), std::nullopt};
    if (sr == "refusal") return {std::string(ir::STOP_REFUSAL), std::nullopt};
    return {std::string(ir::STOP_OTHER),
            make_ant_loss("stop_reason", "stop_reason", ir::LOSS_UNMAPPED_VALUE,
                          "Anthropic stop_reason \"" + std::string(sr) + "\" has no IR equivalent")};
}

StatusOr<std::string> encode_stop_reason(std::string_view stop_reason) {
    if (stop_reason == ir::STOP_END_TURN) return std::string("end_turn");
    if (stop_reason == ir::STOP_MAX_TOKENS) return std::string("max_tokens");
    if (stop_reason == ir::STOP_STOP_SEQUENCE) return std::string("stop_sequence");
    if (stop_reason == ir::STOP_TOOL_USE) return std::string("tool_use");
    if (stop_reason == ir::STOP_REFUSAL) return std::string("refusal");
    return invalid_argument("anthropic: stop reason \"" + std::string(stop_reason) +
                            "\" has no Anthropic equivalent");
}

}  // namespace

StatusOr<Conversion<ir::Request>> decode_request(const json::Value& wire,
                                                 const Options& opts) {
    if (!wire.is_object()) return invalid_argument("anthropic: request must be an object");
    std::vector<ir::Loss> losses;

    const auto* mt = wire.find("max_tokens");
    if (!mt || !mt->is_int() || mt->as_int() <= 0) {
        return invalid_argument("anthropic: max_tokens is required and must be positive");
    }

    if (wire.find("metadata") != nullptr) {
        losses.push_back(make_ant_loss(
            "metadata", "metadata", ir::LOSS_UNMAPPED_FIELD,
            "Anthropic request metadata (user_id) has no IR equivalent in v1."));
    }

    ir::Request req;
    const auto* mv = wire.find("model");
    if (!mv || !mv->is_string()) return invalid_argument("anthropic: request missing model");
    req.model = opts.model_map.map(mv->as_string());

    if (const auto* tools_val = wire.find("tools"); tools_val && tools_val->is_array()) {
        std::vector<ir::Tool> tools;
        for (std::size_t i = 0; i < tools_val->as_array().size(); ++i) {
            const auto& t = tools_val->as_array()[i];
            if (!t.is_object()) continue;
            std::string tool_path = "tools[" + std::to_string(i) + "]";
            if (t.find("cache_control") != nullptr) {
                losses.push_back(make_ant_loss(
                    tool_path + ".cache_control", "cache_control", ir::LOSS_UNMAPPED_FIELD,
                    "Anthropic prompt caching (cache_control) has no IR equivalent in v1."));
            }
            ir::Tool tool;
            if (const auto* nv = t.find("name"); nv && nv->is_string()) tool.name = nv->as_string();
            if (const auto* dv = t.find("description"); dv && dv->is_string() && !dv->as_string().empty()) {
                tool.description = dv->as_string();
                tool.has_description = true;
            }
            const auto* is_val = t.find("input_schema");
            if (!is_val || !is_val->is_object()) {
                return invalid_argument(tool_path + ".input_schema: must be a JSON object");
            }
            tool.input_schema = *is_val;
            tools.push_back(std::move(tool));
        }
        if (!tools.empty()) req.tools = std::move(tools);
    }

    if (const auto* tc_val = wire.find("tool_choice"); tc_val && tc_val->is_object()) {
        const auto* type_val = tc_val->find("type");
        std::string kind = type_val && type_val->is_string() ? type_val->as_string() : "";
        if (kind == "auto") {
            req.tool_choice = ir::ToolChoice{std::string(ir::TOOL_CHOICE_AUTO), std::nullopt};
        } else if (kind == "any") {
            req.tool_choice = ir::ToolChoice{std::string(ir::TOOL_CHOICE_ANY), std::nullopt};
        } else if (kind == "none") {
            req.tool_choice = ir::ToolChoice{std::string(ir::TOOL_CHOICE_NONE), std::nullopt};
        } else if (kind == "tool") {
            std::string name;
            if (const auto* nv = tc_val->find("name"); nv && nv->is_string()) name = nv->as_string();
            req.tool_choice = ir::ToolChoice{std::string(ir::TOOL_CHOICE_TOOL), name};
        }
        if (tc_val->find("disable_parallel_tool_use") != nullptr) {
            losses.push_back(make_ant_loss(
                "tool_choice.disable_parallel_tool_use", "disable_parallel_tool_use",
                ir::LOSS_UNMAPPED_FIELD,
                "Anthropic disable_parallel_tool_use has no IR equivalent in v1."));
        }
    }

    if (const auto* sys_val = wire.find("system")) {
        if (sys_val->is_string()) {
            req.system.push_back(ir::TextBlock{sys_val->as_string()});
        } else if (sys_val->is_array()) {
            for (std::size_t i = 0; i < sys_val->as_array().size(); ++i) {
                const auto& item = sys_val->as_array()[i];
                if (item.find("cache_control") != nullptr) {
                    losses.push_back(make_ant_loss(
                        "system[" + std::to_string(i) + "].cache_control", "cache_control",
                        ir::LOSS_UNMAPPED_FIELD,
                        "Anthropic prompt caching (cache_control) has no IR equivalent in v1."));
                }
                const auto* t = item.find("type");
                std::string kind = t && t->is_string() ? t->as_string() : "";
                if (kind == "text") {
                    const auto* txt = item.find("text");
                    std::string text = txt && txt->is_string() ? txt->as_string() : "";
                    req.system.push_back(ir::TextBlock{std::move(text)});
                }
            }
        }
    }

    const auto* msgs_val = wire.find("messages");
    if (!msgs_val || !msgs_val->is_array() || msgs_val->as_array().empty()) {
        return invalid_argument("anthropic: request carries no messages");
    }
    for (std::size_t i = 0; i < msgs_val->as_array().size(); ++i) {
        const auto& m = msgs_val->as_array()[i];
        const auto* role_val = m.find("role");
        std::string role = role_val && role_val->is_string() ? role_val->as_string() : "";
        if (role != "user" && role != "assistant") {
            return invalid_argument("anthropic: messages[" + std::to_string(i) + "]: unknown role \"" + role + "\"");
        }
        std::string content_path = "messages[" + std::to_string(i) + "].content";
        OXA_ASSIGN_OR_RETURN(auto c_res, decode_content_blocks(m.find("content"), content_path));
        losses.insert(losses.end(), c_res.second.begin(), c_res.second.end());
        req.messages.push_back(ir::Message{role, std::move(c_res.first)});
    }

    ir::Params params;
    params.max_tokens = mt->as_int();
    if (const auto* temp = wire.find("temperature"); temp && temp->is_number()) {
        params.temperature = temp->is_int() ? static_cast<double>(temp->as_int()) : temp->as_double();
    }
    if (const auto* top_p = wire.find("top_p"); top_p && top_p->is_number()) {
        params.top_p = top_p->is_int() ? static_cast<double>(top_p->as_int()) : top_p->as_double();
    }
    if (const auto* ss = wire.find("stop_sequences"); ss && ss->is_array()) {
        std::vector<std::string> stops;
        for (const auto& s : ss->as_array()) {
            if (s.is_string()) stops.push_back(s.as_string());
        }
        if (!stops.empty()) params.stop_sequences = std::move(stops);
    }
    req.params = std::move(params);

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
        losses.push_back(make_ant_loss(
            "metadata", "metadata", ir::LOSS_UNMAPPED_FIELD,
            "Anthropic request metadata is the user_id semantic, not an arbitrary string map; the IR metadata map is dropped."));
    }

    json::Value out = json::Value::object();
    out.set("model", json::Value::string(opts.model_map.map(req.model)));

    if (!req.system.empty()) {
        json::Value sys_arr = json::Value::array();
        for (const auto& s : req.system) {
            json::Value sb = json::Value::object();
            sb.set("type", json::Value::string("text"));
            sb.set("text", json::Value::string(s.text));
            sys_arr.push_back(std::move(sb));
        }
        out.set("system", std::move(sys_arr));
    }

    if (req.tools.has_value() && !req.tools->empty()) {
        json::Value tools_arr = json::Value::array();
        for (const auto& t : *req.tools) {
            json::Value tw = json::Value::object();
            tw.set("name", json::Value::string(t.name));
            if (t.has_description && !t.description.empty()) {
                tw.set("description", json::Value::string(t.description));
            }
            tw.set("input_schema", t.input_schema.is_null() ? json::Value::object() : t.input_schema);
            tools_arr.push_back(std::move(tw));
        }
        out.set("tools", std::move(tools_arr));
    }

    if (req.tool_choice.has_value()) {
        const auto& tc = *req.tool_choice;
        if (tc.mode == ir::TOOL_CHOICE_AUTO) {
            json::Value tco = json::Value::object();
            tco.set("type", json::Value::string("auto"));
            out.set("tool_choice", std::move(tco));
        } else if (tc.mode == ir::TOOL_CHOICE_ANY) {
            json::Value tco = json::Value::object();
            tco.set("type", json::Value::string("any"));
            out.set("tool_choice", std::move(tco));
        } else if (tc.mode == ir::TOOL_CHOICE_NONE) {
            json::Value tco = json::Value::object();
            tco.set("type", json::Value::string("none"));
            out.set("tool_choice", std::move(tco));
        } else if (tc.mode == ir::TOOL_CHOICE_TOOL) {
            json::Value tco = json::Value::object();
            tco.set("type", json::Value::string("tool"));
            if (tc.name.has_value()) tco.set("name", json::Value::string(*tc.name));
            out.set("tool_choice", std::move(tco));
        }
    }

    bool shorthand = req.system.empty() && req.messages.size() == 1 &&
                     req.messages[0].content.size() == 1 &&
                     std::holds_alternative<ir::TextBlock>(req.messages[0].content[0]);

    json::Value messages_arr = json::Value::array();
    for (std::size_t i = 0; i < req.messages.size(); ++i) {
        const auto& m = req.messages[i];
        json::Value mw = json::Value::object();
        mw.set("role", json::Value::string(m.role));

        if (shorthand) {
            const auto& tb = std::get<ir::TextBlock>(m.content[0]);
            mw.set("content", json::Value::string(tb.text));
        } else {
            json::Value content_arr = json::Value::array();
            for (std::size_t bi = 0; bi < m.content.size(); ++bi) {
                const auto& b = m.content[bi];
                if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
                    json::Value bw = json::Value::object();
                    bw.set("type", json::Value::string("text"));
                    bw.set("text", json::Value::string(tb->text));
                    content_arr.push_back(std::move(bw));
                } else if (const auto* img = std::get_if<ir::ImageBlock>(&b)) {
                    json::Value bw = json::Value::object();
                    bw.set("type", json::Value::string("image"));
                    json::Value src = json::Value::object();
                    if (img->data.has_value() && !img->data->empty()) {
                        src.set("type", json::Value::string("base64"));
                        src.set("media_type", json::Value::string(img->media_type.value_or("image/jpeg")));
                        src.set("data", json::Value::string(*img->data));
                    } else if (img->url.has_value()) {
                        src.set("type", json::Value::string("url"));
                        src.set("url", json::Value::string(*img->url));
                    }
                    bw.set("source", std::move(src));
                    content_arr.push_back(std::move(bw));
                } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
                    json::Value bw = json::Value::object();
                    bw.set("type", json::Value::string("tool_use"));
                    bw.set("id", json::Value::string(tu->id));
                    bw.set("name", json::Value::string(tu->name));
                    auto inp_res = json::parse(tu->input);
                    if (inp_res.ok() && inp_res->is_object()) {
                        bw.set("input", std::move(*inp_res));
                    } else {
                        bw.set("input", json::Value::raw(tu->input));
                    }
                    content_arr.push_back(std::move(bw));
                } else if (const auto* tr = std::get_if<ir::ToolResultBlock>(&b)) {
                    json::Value bw = json::Value::object();
                    bw.set("type", json::Value::string("tool_result"));
                    bw.set("tool_use_id", json::Value::string(tr->tool_use_id));
                    if (tr->is_error) bw.set("is_error", json::Value::boolean(true));
                    json::Value inner_arr = json::Value::array();
                    for (const auto& inner : tr->content) {
                        if (const auto* itb = std::get_if<ir::TextBlock>(&inner.block)) {
                            json::Value ibw = json::Value::object();
                            ibw.set("type", json::Value::string("text"));
                            ibw.set("text", json::Value::string(itb->text));
                            inner_arr.push_back(std::move(ibw));
                        }
                    }
                    bw.set("content", std::move(inner_arr));
                    content_arr.push_back(std::move(bw));
                }
            }
            mw.set("content", std::move(content_arr));
        }
        messages_arr.push_back(std::move(mw));
    }
    out.set("messages", std::move(messages_arr));

    std::int64_t max_tokens = 4096;
    if (req.params.has_value() && req.params->max_tokens.has_value()) {
        max_tokens = *req.params->max_tokens;
    } else {
        losses.push_back(make_ant_loss(
            "params", "max_tokens", ir::LOSS_DEGRADED,
            "Anthropic Messages requires max_tokens; defaulting to 4096"));
    }
    out.set("max_tokens", json::Value::integer(max_tokens));

    if (req.params.has_value()) {
        const auto& p = *req.params;
        if (p.temperature.has_value()) out.set("temperature", json::Value::real(*p.temperature));
        if (p.top_p.has_value()) out.set("top_p", json::Value::real(*p.top_p));
        if (p.stop_sequences.has_value() && !p.stop_sequences->empty()) {
            json::Value stops = json::Value::array();
            for (const auto& s : *p.stop_sequences) stops.push_back(json::Value::string(s));
            out.set("stop_sequences", std::move(stops));
        }
    }

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

StatusOr<Conversion<ir::Response>> decode_response(const json::Value& wire,
                                                  const Options& opts) {
    if (!wire.is_object()) return invalid_argument("anthropic: response must be an object");
    std::vector<ir::Loss> losses;

    ir::Response resp;
    if (const auto* id_val = wire.find("id"); id_val && id_val->is_string()) resp.id = id_val->as_string();
    if (const auto* m_val = wire.find("model"); m_val && m_val->is_string()) {
        resp.model = opts.model_map.map(m_val->as_string());
    }

    OXA_ASSIGN_OR_RETURN(auto c_res, decode_content_blocks(wire.find("content"), "content", true));
    losses.insert(losses.end(), c_res.second.begin(), c_res.second.end());
    resp.content = std::move(c_res.first);

    std::string sr;
    if (const auto* sr_val = wire.find("stop_reason"); sr_val && sr_val->is_string()) {
        sr = sr_val->as_string();
    }
    auto [stop_reason, sr_loss] = decode_stop_reason(sr);
    resp.stop_reason = std::move(stop_reason);
    if (sr_loss.has_value()) losses.push_back(std::move(*sr_loss));

    if (const auto* seq = wire.find("stop_sequence"); seq && seq->is_string() && !seq->as_string().empty()) {
        resp.stop_sequence = seq->as_string();
    }

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
    out.set("type", json::Value::string("message"));
    out.set("role", json::Value::string("assistant"));
    out.set("model", json::Value::string(opts.model_map.map(resp.model)));

    json::Value content_arr = json::Value::array();
    for (std::size_t i = 0; i < resp.content.size(); ++i) {
        const auto& b = resp.content[i];
        if (const auto* tb = std::get_if<ir::TextBlock>(&b)) {
            json::Value bw = json::Value::object();
            bw.set("type", json::Value::string("text"));
            bw.set("text", json::Value::string(tb->text));
            content_arr.push_back(std::move(bw));
        } else if (const auto* tu = std::get_if<ir::ToolUseBlock>(&b)) {
            json::Value bw = json::Value::object();
            bw.set("type", json::Value::string("tool_use"));
            bw.set("id", json::Value::string(tu->id));
            bw.set("name", json::Value::string(tu->name));
            auto inp_res = json::parse(tu->input);
            if (inp_res.ok() && inp_res->is_object()) {
                bw.set("input", std::move(*inp_res));
            } else {
                bw.set("input", json::Value::raw(tu->input));
            }
            content_arr.push_back(std::move(bw));
        }
    }
    out.set("content", std::move(content_arr));

    OXA_ASSIGN_OR_RETURN(std::string ant_sr, encode_stop_reason(resp.stop_reason));
    out.set("stop_reason", json::Value::string(ant_sr));
    if (resp.stop_sequence.has_value() && !resp.stop_sequence->empty()) {
        out.set("stop_sequence", json::Value::string(*resp.stop_sequence));
    }

    json::Value usage = json::Value::object();
    usage.set("input_tokens", json::Value::integer(resp.usage.input_tokens));
    usage.set("output_tokens", json::Value::integer(resp.usage.output_tokens));
    out.set("usage", std::move(usage));

    return Conversion<json::Value>{std::move(out), std::move(losses)};
}

// ---- StreamDecoder --------------------------------------------------------

StreamDecoder::StreamDecoder(Options opts) : opts_(std::move(opts)) {}

StatusOr<std::vector<ir::Event>> StreamDecoder::feed(const json::Value& chunk) {
    if (flushed_) return invalid_argument("anthropic: event fed after stream flush");
    if (stopped_) return invalid_argument("anthropic: event fed after message_stop");
    if (!chunk.is_object()) return invalid_argument("anthropic: event must be an object");
    const auto* t = chunk.find("type");
    std::string type = t && t->is_string() ? t->as_string() : "";

    std::vector<ir::Event> events;

    if (type == "message_start") {
        if (started_) return invalid_argument("anthropic: duplicate message_start");
        const auto* msg = chunk.find("message");
        if (!msg || !msg->is_object()) return invalid_argument("anthropic: message_start without message");
        started_ = true;
        if (const auto* idv = msg->find("id"); idv && idv->is_string()) id_ = idv->as_string();
        if (const auto* mv = msg->find("model"); mv && mv->is_string()) model_ = opts_.model_map.map(mv->as_string());
        if (const auto* uv = msg->find("usage"); uv && uv->is_object()) {
            if (const auto* it = uv->find("input_tokens"); it && it->is_int()) usage_.input_tokens = it->as_int();
            if (const auto* ot = uv->find("output_tokens"); ot && ot->is_int()) usage_.output_tokens = ot->as_int();
        }
        events.push_back(ir::MessageStart{id_, model_});
        return events;
    }

    if (type == "content_block_start") {
        if (!started_) return invalid_argument("anthropic: content_block_start before message_start");
        if (block_open_ || skipped_open_) return invalid_argument("anthropic: content_block_start with a block still open");
        std::int64_t idx = -1;
        if (const auto* iv = chunk.find("index"); iv && iv->is_int()) idx = iv->as_int();
        if (idx != next_index_) {
            return invalid_argument("anthropic: content_block_start index " + std::to_string(idx) +
                                    ", want " + std::to_string(next_index_));
        }
        next_index_++;

        const auto* cb = chunk.find("content_block");
        if (!cb || !cb->is_object()) {
            skipped_.insert(idx);
            skipped_open_ = true;
            losses_.push_back(make_ant_loss(
                "content_block_start[" + std::to_string(idx) + "].content_block.type",
                "content_block.type", ir::LOSS_UNSUPPORTED_SEMANTIC,
                "Anthropic streaming block has no content_block payload; the index is skipped"));
            return events;
        }

        const auto* bt = cb->find("type");
        std::string btype = bt && bt->is_string() ? bt->as_string() : "";

        if (btype == "tool_use") {
            std::string id, name;
            if (const auto* idv = cb->find("id"); idv && idv->is_string()) id = idv->as_string();
            if (const auto* nv = cb->find("name"); nv && nv->is_string()) name = nv->as_string();
            if (id.empty()) return invalid_argument("anthropic: content_block_start tool_use id is required");
            if (name.empty()) return invalid_argument("anthropic: content_block_start tool_use name is required");

            block_open_ = true;
            open_tool_ = true;
            open_index_ = idx;
            open_ir_index_ = next_ir_index_++;
            tool_id_ = std::move(id);
            tool_name_ = std::move(name);
            tool_input_.clear();
            tool_parts_.clear();
            if (const auto* inp = cb->find("input")) {
                std::string_view raw = inp->raw_slice();
                if (!raw.empty()) {
                    tool_input_ = std::string(raw);
                } else {
                    tool_input_ = json::serialize(*inp);
                }
            }
            return events;
        }

        if (btype == "text") {
            block_open_ = true;
            open_tool_ = false;
            open_index_ = idx;
            open_ir_index_ = next_ir_index_++;
            std::string text;
            if (const auto* tv = cb->find("text"); tv && tv->is_string()) text = tv->as_string();
            events.push_back(ir::ContentBlockStart{open_ir_index_, ir::TextBlock{std::move(text)}});
            return events;
        }

        // Unknown block type
        skipped_.insert(idx);
        skipped_open_ = true;
        losses_.push_back(make_ant_loss(
            "content_block_start[" + std::to_string(idx) + "].content_block.type",
            "content_block.type", ir::LOSS_UNSUPPORTED_SEMANTIC,
            "Anthropic streaming block type \"" + btype + "\" is not decodable in M7; the index is skipped"));
        return events;
    }

    if (type == "content_block_delta") {
        if (!started_) return invalid_argument("anthropic: content_block_delta before message_start");
        const auto* d = chunk.find("delta");
        if (!d || !d->is_object()) return invalid_argument("anthropic: content_block_delta without delta");
        std::int64_t idx = -1;
        if (const auto* iv = chunk.find("index"); iv && iv->is_int()) idx = iv->as_int();
        if (skipped_.count(idx)) {
            return events;
        }
        if (!block_open_ || idx != open_index_) {
            return invalid_argument("anthropic: content_block_delta index " + std::to_string(idx) +
                                    " does not match the open block");
        }

        const auto* dt = d->find("type");
        std::string dtype = dt && dt->is_string() ? dt->as_string() : "";

        if (open_tool_) {
            if (dtype == "text_delta") return invalid_argument("anthropic: text_delta on tool_use block");
            if (dtype == "input_json_delta") {
                const auto* pj = d->find("partial_json");
                std::string part = pj && pj->is_string() ? pj->as_string() : "";
                tool_parts_.push_back(std::move(part));
                return events;
            }
        } else {
            if (dtype == "text_delta") {
                const auto* txt = d->find("text");
                std::string t = txt && txt->is_string() ? txt->as_string() : "";
                events.push_back(ir::ContentBlockDelta{open_ir_index_, ir::TextDelta{std::move(t)}});
                return events;
            }
            if (dtype == "input_json_delta") {
                return invalid_argument("anthropic: input_json_delta on non-tool block");
            }
        }

        losses_.push_back(make_ant_loss(
            "content_block_delta[" + std::to_string(idx) + "].delta.type",
            "delta.type", ir::LOSS_UNSUPPORTED_SEMANTIC,
            "Anthropic delta type \"" + dtype + "\" has no IR equivalent"));
        return events;
    }

    if (type == "content_block_stop") {
        if (!started_) return invalid_argument("anthropic: content_block_stop before message_start");
        std::int64_t idx = -1;
        if (const auto* iv = chunk.find("index"); iv && iv->is_int()) idx = iv->as_int();
        if (skipped_.count(idx)) {
            skipped_.erase(idx);
            skipped_open_ = false;
            return events;
        }
        if (!block_open_ || idx != open_index_) {
            return invalid_argument("anthropic: content_block_stop index " + std::to_string(idx) +
                                    " does not match the open block");
        }

        if (open_tool_) {
            std::string full_input;
            std::vector<std::string> parts;
            if (!tool_parts_.empty()) {
                for (const auto& p : tool_parts_) full_input += p;
                parts = tool_parts_;
            } else if (!tool_input_.empty()) {
                full_input = tool_input_;
                parts.push_back(tool_input_);
            } else {
                return invalid_argument("anthropic: tool_use input is required");
            }
            events.push_back(ir::ContentBlockStart{
                open_ir_index_, ir::ToolUseBlock{tool_id_, tool_name_, full_input}});
            for (const auto& p : parts) {
                events.push_back(ir::ContentBlockDelta{
                    open_ir_index_, ir::InputJsonDelta{p}});
            }
            events.push_back(ir::ContentBlockStop{open_ir_index_});
            open_tool_ = false;
            tool_id_.clear();
            tool_name_.clear();
            tool_input_.clear();
            tool_parts_.clear();
        } else {
            events.push_back(ir::ContentBlockStop{open_ir_index_});
        }
        block_open_ = false;
        return events;
    }

    if (type == "message_delta") {
        if (!started_) return invalid_argument("anthropic: message_delta before message_start");
        if (block_open_ || skipped_open_) return invalid_argument("anthropic: message_delta with a block still open");
        const auto* d = chunk.find("delta");
        if (!d || !d->is_object()) return invalid_argument("anthropic: message_delta without delta");

        if (const auto* srv = d->find("stop_reason"); srv && srv->is_string()) stop_reason_ = srv->as_string();
        if (const auto* ssv = d->find("stop_sequence"); ssv && ssv->is_string()) stop_seq_ = ssv->as_string();

        if (const auto* uv = chunk.find("usage"); uv && uv->is_object()) {
            if (const auto* it = uv->find("input_tokens"); it && it->is_int()) usage_.input_tokens = it->as_int();
            if (const auto* ot = uv->find("output_tokens"); ot && ot->is_int()) usage_.output_tokens = ot->as_int();
        }
        delta_seen_ = true;
        return events;
    }

    if (type == "message_stop") {
        if (!started_) return invalid_argument("anthropic: message_stop before message_start");
        if (block_open_ || skipped_open_) return invalid_argument("anthropic: message_stop with a block still open");
        if (!delta_seen_) return invalid_argument("anthropic: message_stop without preceding message_delta");
        stopped_ = true;
        auto [sr, loss] = decode_stop_reason(stop_reason_);
        if (loss.has_value()) losses_.push_back(std::move(*loss));
        std::optional<std::string> seq;
        if (sr == ir::STOP_STOP_SEQUENCE) seq = stop_seq_;
        events.push_back(ir::MessageDelta{sr, seq, usage_});
        events.push_back(ir::MessageDone{});
        return events;
    }

    losses_.push_back(make_ant_loss(
        "type", "type", ir::LOSS_UNSUPPORTED_SEMANTIC,
        "Anthropic stream event type \"" + type + "\" is not decoded in this milestone"));
    return events;
}

StatusOr<std::vector<ir::Event>> StreamDecoder::flush() {
    if (flushed_) return invalid_argument("anthropic: stream flushed twice");
    if (!stopped_) return invalid_argument("anthropic: stream ended without message_stop");
    flushed_ = true;
    return std::vector<ir::Event>{};
}

// ---- StreamEncoder --------------------------------------------------------

StreamEncoder::StreamEncoder(Options opts) : opts_(std::move(opts)) {}

StatusOr<Conversion<std::vector<json::Value>>> StreamEncoder::apply(const ir::Event& event) {
    if (done_) return invalid_argument("anthropic: event applied after MessageDone");

    std::vector<json::Value> chunks;
    std::vector<ir::Loss> losses;

    if (const auto* ms = std::get_if<ir::MessageStart>(&event)) {
        if (started_) return invalid_argument("anthropic: duplicate MessageStart");
        started_ = true;
        id_ = ms->id;
        model_ = opts_.model_map.map(ms->model);

        json::Value chunk = json::Value::object();
        chunk.set("type", json::Value::string("message_start"));
        json::Value msg = json::Value::object();
        msg.set("id", json::Value::string(id_));
        msg.set("type", json::Value::string("message"));
        msg.set("role", json::Value::string("assistant"));
        msg.set("model", json::Value::string(model_));
        msg.set("content", json::Value::array());
        msg.set("stop_reason", json::Value::null());
        json::Value usage = json::Value::object();
        usage.set("input_tokens", json::Value::integer(0));
        usage.set("output_tokens", json::Value::integer(0));
        msg.set("usage", std::move(usage));
        chunk.set("message", std::move(msg));
        chunks.push_back(std::move(chunk));
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    if (const auto* cbs = std::get_if<ir::ContentBlockStart>(&event)) {
        if (!started_ || block_open_ || delta_seen_) {
            return invalid_argument("anthropic: ContentBlockStart out of grammar order");
        }
        if (cbs->index != next_index_) {
            return invalid_argument("anthropic: ContentBlockStart index " + std::to_string(cbs->index) +
                                    ", want " + std::to_string(next_index_));
        }
        next_index_++;
        block_open_ = true;
        open_index_ = cbs->index;

        json::Value chunk = json::Value::object();
        chunk.set("type", json::Value::string("content_block_start"));
        chunk.set("index", json::Value::integer(open_index_));
        json::Value cb = json::Value::object();
        if (const auto* tu = std::get_if<ir::ToolUseBlock>(&cbs->block)) {
            if (tu->id.empty()) return invalid_argument("anthropic: ContentBlockStart tool_use id is required");
            if (tu->name.empty()) return invalid_argument("anthropic: ContentBlockStart tool_use name is required");
            open_tool_ = true;
            tool_input_ = tu->input;
            tool_parts_.clear();
            cb.set("type", json::Value::string("tool_use"));
            cb.set("id", json::Value::string(tu->id));
            cb.set("name", json::Value::string(tu->name));
            cb.set("input", json::Value::object());
        } else if (const auto* tb = std::get_if<ir::TextBlock>(&cbs->block)) {
            open_tool_ = false;
            cb.set("type", json::Value::string("text"));
            cb.set("text", json::Value::string(tb->text));
        } else {
            return invalid_argument("anthropic: ContentBlockStart carries an unsupported block");
        }
        chunk.set("content_block", std::move(cb));
        chunks.push_back(std::move(chunk));
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    if (const auto* cbd = std::get_if<ir::ContentBlockDelta>(&event)) {
        if (!block_open_ || cbd->index != open_index_) {
            return invalid_argument("anthropic: ContentBlockDelta out of grammar order");
        }
        json::Value chunk = json::Value::object();
        chunk.set("type", json::Value::string("content_block_delta"));
        chunk.set("index", json::Value::integer(cbd->index));
        json::Value delta = json::Value::object();
        if (open_tool_) {
            const auto* ij = std::get_if<ir::InputJsonDelta>(&cbd->delta);
            if (!ij) return invalid_argument("anthropic: ToolUseBlock received non-input-json delta");
            tool_parts_.push_back(ij->partial_json);
            delta.set("type", json::Value::string("input_json_delta"));
            delta.set("partial_json", json::Value::string(ij->partial_json));
        } else {
            const auto* td = std::get_if<ir::TextDelta>(&cbd->delta);
            if (!td) return invalid_argument("anthropic: TextBlock received non-text delta");
            delta.set("type", json::Value::string("text_delta"));
            delta.set("text", json::Value::string(td->text));
        }
        chunk.set("delta", std::move(delta));
        chunks.push_back(std::move(chunk));
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    if (const auto* cst = std::get_if<ir::ContentBlockStop>(&event)) {
        if (!block_open_ || cst->index != open_index_) {
            return invalid_argument("anthropic: ContentBlockStop out of grammar order");
        }
        if (open_tool_) {
            if (tool_parts_.empty()) {
                json::Value d_chunk = json::Value::object();
                d_chunk.set("type", json::Value::string("content_block_delta"));
                d_chunk.set("index", json::Value::integer(cst->index));
                json::Value delta = json::Value::object();
                delta.set("type", json::Value::string("input_json_delta"));
                delta.set("partial_json", json::Value::string(tool_input_));
                d_chunk.set("delta", std::move(delta));
                chunks.push_back(std::move(d_chunk));
            } else {
                std::string joined;
                for (const auto& p : tool_parts_) joined += p;
                if (joined != tool_input_) {
                    return invalid_argument("anthropic: ToolUseBlock input does not equal concatenated InputJSONDelta fragments");
                }
            }
            open_tool_ = false;
            tool_input_.clear();
            tool_parts_.clear();
        }
        block_open_ = false;
        json::Value chunk = json::Value::object();
        chunk.set("type", json::Value::string("content_block_stop"));
        chunk.set("index", json::Value::integer(cst->index));
        chunks.push_back(std::move(chunk));
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    if (const auto* md = std::get_if<ir::MessageDelta>(&event)) {
        if (!started_ || block_open_ || delta_seen_) {
            return invalid_argument("anthropic: MessageDelta out of grammar order");
        }
        delta_seen_ = true;
        OXA_ASSIGN_OR_RETURN(std::string asr, encode_stop_reason(md->stop_reason));
        json::Value chunk = json::Value::object();
        chunk.set("type", json::Value::string("message_delta"));
        json::Value delta = json::Value::object();
        delta.set("stop_reason", json::Value::string(asr));
        if (md->stop_sequence.has_value() && !md->stop_sequence->empty()) {
            delta.set("stop_sequence", json::Value::string(*md->stop_sequence));
        }
        chunk.set("delta", std::move(delta));
        json::Value usage = json::Value::object();
        usage.set("input_tokens", json::Value::integer(md->usage.input_tokens));
        usage.set("output_tokens", json::Value::integer(md->usage.output_tokens));
        chunk.set("usage", std::move(usage));
        chunks.push_back(std::move(chunk));
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    if (std::holds_alternative<ir::MessageDone>(event)) {
        if (!delta_seen_) return invalid_argument("anthropic: MessageDone out of grammar order");
        done_ = true;
        json::Value chunk = json::Value::object();
        chunk.set("type", json::Value::string("message_stop"));
        chunks.push_back(std::move(chunk));
        return Conversion<std::vector<json::Value>>{std::move(chunks), std::move(losses)};
    }

    return invalid_argument("anthropic: unknown event");
}

}  // namespace oxa::anthropic::messages
