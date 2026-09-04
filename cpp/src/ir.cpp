#include "oxa/ir.hpp"

#include <unordered_set>

namespace oxa::ir {
namespace {

StatusOr<std::string> kind_of(const json::Value& v) {
    const json::Value* t = v.find("type");
    if (t == nullptr || !t->is_string()) {
        return invalid_argument("block/event missing \"type\"");
    }
    return t->as_string();
}

StatusOr<std::reference_wrapper<const json::Value>> required(
    const json::Value& v, std::string_view key, const char* ctx = "document") {
    const json::Value* out = v.find(key);
    if (out == nullptr) {
        return invalid_argument(std::string(ctx) + " missing \"" + std::string(key) + "\"");
    }
    return std::cref(*out);
}

Status check_spec_version(const json::Value& v) {
    const json::Value* sv = v.find("specVersion");
    if (sv == nullptr || !sv->is_string() || sv->as_string() != SPEC_VERSION) {
        return invalid_argument("unsupported specVersion, want \"" + std::string(SPEC_VERSION) + "\"");
    }
    return ok_status();
}

StatusOr<std::string> opt_string(const json::Value& v, std::string_view key) {
    const json::Value* f = v.find(key);
    if (f == nullptr || f->is_null()) return std::string{};
    if (!f->is_string()) {
        return invalid_argument("field \"" + std::string(key) + "\" must be a string");
    }
    return f->as_string();
}

bool has(const json::Value& v, std::string_view key) {
    const json::Value* f = v.find(key);
    return f != nullptr && !f->is_null();
}

}  // namespace

// ---- blocks ----------------------------------------------------------------

json::Value dump_block(const Block& b) {
    json::Value out = json::Value::object();
    if (const auto* t = std::get_if<TextBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_TEXT)));
        out.set("text", json::Value::string(t->text));
        return out;
    }
    if (const auto* img = std::get_if<ImageBlock>(&b)) {
        const bool has_data = img->data.has_value() && !img->data->empty();
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_IMAGE)));
        if (has_data) {
            if (img->media_type.has_value()) {
                out.set("media_type", json::Value::string(*img->media_type));
            }
            out.set("data", json::Value::string(*img->data));
        } else if (img->url.has_value()) {
            out.set("url", json::Value::string(*img->url));
        }
        return out;
    }
    if (const auto* tu = std::get_if<ToolUseBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_TOOL_USE)));
        out.set("id", json::Value::string(tu->id));
        out.set("name", json::Value::string(tu->name));
        out.set("input", json::Value::string(tu->input));
        return out;
    }
    const auto& tr = std::get<ToolResultBlock>(b);
    out.set("type", json::Value::string(std::string(BLOCK_TYPE_TOOL_RESULT)));
    out.set("tool_use_id", json::Value::string(tr.tool_use_id));
    json::Value content = json::Value::array();
    for (const auto& inner : tr.content) {
        content.push_back(dump_block(inner.block));
    }
    out.set("content", std::move(content));
    if (tr.is_error) out.set("is_error", json::Value::boolean(true));
    return out;
}

StatusOr<Block> load_block(const json::Value& v) {
    if (!v.is_object()) return invalid_argument("block must be an object");
    OXA_ASSIGN_OR_RETURN(std::string kind, kind_of(v));
    if (kind == BLOCK_TYPE_TEXT) {
        OXA_ASSIGN_OR_RETURN(std::string txt, opt_string(v, "text"));
        return Block(TextBlock{std::move(txt)});
    }
    if (kind == BLOCK_TYPE_IMAGE) {
        ImageBlock img;
        if (has(v, "media_type")) {
            OXA_ASSIGN_OR_RETURN(std::string mt, opt_string(v, "media_type"));
            img.media_type = std::move(mt);
        }
        if (has(v, "data")) {
            OXA_ASSIGN_OR_RETURN(std::string dt, opt_string(v, "data"));
            img.data = std::move(dt);
        }
        if (has(v, "url")) {
            OXA_ASSIGN_OR_RETURN(std::string u, opt_string(v, "url"));
            img.url = std::move(u);
        }
        return Block(std::move(img));
    }
    if (kind == BLOCK_TYPE_TOOL_USE) {
        ToolUseBlock tu;
        OXA_ASSIGN_OR_RETURN(auto id_val, required(v, "id", "tool_use block"));
        tu.id = id_val.get().as_string();
        OXA_ASSIGN_OR_RETURN(auto name_val, required(v, "name", "tool_use block"));
        tu.name = name_val.get().as_string();
        OXA_ASSIGN_OR_RETURN(auto input_val, required(v, "input", "tool_use block"));
        if (!input_val.get().is_string()) return invalid_argument("tool_use input must be a string");
        tu.input = input_val.get().as_string();
        return Block(std::move(tu));
    }
    if (kind == BLOCK_TYPE_TOOL_RESULT) {
        ToolResultBlock tr;
        OXA_ASSIGN_OR_RETURN(auto id_val, required(v, "tool_use_id", "tool_result block"));
        tr.tool_use_id = id_val.get().as_string();
        const json::Value* content = v.find("content");
        if (content != nullptr && content->is_array()) {
            for (const auto& inner : content->as_array()) {
                OXA_ASSIGN_OR_RETURN(Block b, load_block(inner));
                tr.content.push_back(BlockHolder{std::move(b)});
            }
        }
        const json::Value* is_err = v.find("is_error");
        tr.is_error = is_err != nullptr && is_err->is_bool() && is_err->as_bool();
        return Block(std::move(tr));
    }
    return invalid_argument("unknown block discriminant: " + kind);
}

// ---- request -----------------------------------------------------------------

json::Value dump_request(const Request& r) {
    json::Value out = json::Value::object();
    out.set("specVersion", json::Value::string(std::string(SPEC_VERSION)));
    out.set("model", json::Value::string(r.model));
    json::Value messages = json::Value::array();
    for (const auto& m : r.messages) {
        json::Value mv = json::Value::object();
        mv.set("role", json::Value::string(m.role));
        json::Value content = json::Value::array();
        for (const auto& b : m.content) content.push_back(dump_block(b));
        mv.set("content", std::move(content));
        messages.push_back(std::move(mv));
    }
    out.set("messages", std::move(messages));

    if (!r.system.empty()) {
        json::Value system = json::Value::array();
        for (const auto& s : r.system) {
            system.push_back(dump_block(TextBlock{s.text}));
        }
        out.set("system", std::move(system));
    }
    if (r.tools.has_value() && !r.tools->empty()) {
        json::Value tools = json::Value::array();
        for (const auto& t : *r.tools) {
            json::Value tv = json::Value::object();
            tv.set("name", json::Value::string(t.name));
            if (t.has_description) tv.set("description", json::Value::string(t.description));
            tv.set("input_schema", t.input_schema);
            tools.push_back(std::move(tv));
        }
        out.set("tools", std::move(tools));
    }
    if (r.tool_choice.has_value()) {
        json::Value tc = json::Value::object();
        tc.set("mode", json::Value::string(r.tool_choice->mode));
        if (r.tool_choice->mode == TOOL_CHOICE_TOOL && r.tool_choice->name.has_value()) {
            tc.set("name", json::Value::string(*r.tool_choice->name));
        }
        out.set("tool_choice", std::move(tc));
    }
    if (r.params.has_value()) {
        json::Value p = json::Value::object();
        if (r.params->temperature.has_value()) {
            p.set("temperature", json::Value::real(*r.params->temperature));
        }
        if (r.params->top_p.has_value()) p.set("top_p", json::Value::real(*r.params->top_p));
        if (r.params->max_tokens.has_value()) {
            p.set("max_tokens", json::Value::integer(*r.params->max_tokens));
        }
        if (r.params->stop_sequences.has_value()) {
            json::Value stops = json::Value::array();
            for (const auto& s : *r.params->stop_sequences) {
                stops.push_back(json::Value::string(s));
            }
            p.set("stop_sequences", std::move(stops));
        }
        if (!p.as_object().empty()) out.set("params", std::move(p));
    }
    if (r.metadata.has_value() && !r.metadata->empty()) {
        json::Value md = json::Value::object();
        for (const auto& [k, v] : *r.metadata) md.set(k, json::Value::string(v));
        out.set("metadata", std::move(md));
    }
    return out;
}

StatusOr<Request> load_request(const json::Value& v) {
    if (!v.is_object()) return invalid_argument("request must be an object");
    OXA_RETURN_IF_ERROR(check_spec_version(v));
    Request r;
    OXA_ASSIGN_OR_RETURN(auto model_val, required(v, "model", "request"));
    r.model = model_val.get().as_string();

    const json::Value* system = v.find("system");
    if (system != nullptr && system->is_array()) {
        for (const auto& sb : system->as_array()) {
            OXA_ASSIGN_OR_RETURN(Block b, load_block(sb));
            if (!std::holds_alternative<TextBlock>(b)) {
                return invalid_argument("system block must be text");
            }
            r.system.push_back(std::get<TextBlock>(b));
        }
    }

    OXA_ASSIGN_OR_RETURN(auto messages_val, required(v, "messages", "request"));
    if (!messages_val.get().is_array()) return invalid_argument("request messages must be an array");
    for (const auto& mv : messages_val.get().as_array()) {
        Message m;
        OXA_ASSIGN_OR_RETURN(auto role_val, required(mv, "role", "message"));
        m.role = role_val.get().as_string();
        OXA_ASSIGN_OR_RETURN(auto content_val, required(mv, "content", "message"));
        if (!content_val.get().is_array()) return invalid_argument("message content must be an array");
        for (const auto& cb : content_val.get().as_array()) {
            OXA_ASSIGN_OR_RETURN(Block b, load_block(cb));
            m.content.push_back(std::move(b));
        }
        r.messages.push_back(std::move(m));
    }

    const json::Value* tools = v.find("tools");
    if (tools != nullptr && tools->is_array()) {
        std::vector<Tool> out;
        for (const auto& tv : tools->as_array()) {
            Tool t;
            OXA_ASSIGN_OR_RETURN(auto name_val, required(tv, "name", "tool"));
            t.name = name_val.get().as_string();
            const json::Value* desc = tv.find("description");
            if (desc != nullptr && desc->is_string()) {
                t.description = desc->as_string();
                t.has_description = true;
            }
            OXA_ASSIGN_OR_RETURN(auto is_val, required(tv, "input_schema", "tool"));
            t.input_schema = is_val.get();
            out.push_back(std::move(t));
        }
        if (!out.empty()) r.tools = std::move(out);
    }

    const json::Value* tc = v.find("tool_choice");
    if (tc != nullptr && tc->is_object()) {
        ToolChoice choice;
        OXA_ASSIGN_OR_RETURN(auto mode_val, required(*tc, "mode", "tool_choice"));
        choice.mode = mode_val.get().as_string();
        const json::Value* name = tc->find("name");
        if (name != nullptr && name->is_string()) choice.name = name->as_string();
        r.tool_choice = std::move(choice);
    }

    const json::Value* params = v.find("params");
    if (params != nullptr && params->is_object()) {
        Params p;
        const json::Value* f;
        if ((f = params->find("temperature")) != nullptr && f->is_number()) {
            p.temperature = f->is_int() ? static_cast<double>(f->as_int()) : f->as_double();
        }
        if ((f = params->find("top_p")) != nullptr && f->is_number()) {
            p.top_p = f->is_int() ? static_cast<double>(f->as_int()) : f->as_double();
        }
        if ((f = params->find("max_tokens")) != nullptr && f->is_int()) {
            p.max_tokens = f->as_int();
        }
        if ((f = params->find("stop_sequences")) != nullptr && f->is_array()) {
            std::vector<std::string> stops;
            for (const auto& s : f->as_array()) stops.push_back(s.as_string());
            p.stop_sequences = std::move(stops);
        }
        r.params = std::move(p);
    }

    const json::Value* md = v.find("metadata");
    if (md != nullptr && md->is_object()) {
        std::map<std::string, std::string> out;
        for (const auto& [k, val] : md->as_object()) {
            if (val.is_string()) out.emplace(k, val.as_string());
        }
        if (!out.empty()) r.metadata = std::move(out);
    }
    return r;
}

// ---- response ------------------------------------------------------------------

namespace {

Usage load_usage(const json::Value& v) {
    Usage u;
    const json::Value* f = v.find("usage");
    if (f != nullptr && f->is_object()) {
        const json::Value* in = f->find("input_tokens");
        const json::Value* out = f->find("output_tokens");
        if (in != nullptr && in->is_int()) u.input_tokens = in->as_int();
        if (out != nullptr && out->is_int()) u.output_tokens = out->as_int();
    }
    return u;
}

json::Value dump_usage(const Usage& u) {
    json::Value out = json::Value::object();
    out.set("input_tokens", json::Value::integer(u.input_tokens));
    out.set("output_tokens", json::Value::integer(u.output_tokens));
    return out;
}

}  // namespace

json::Value dump_response(const Response& r) {
    json::Value out = json::Value::object();
    out.set("specVersion", json::Value::string(std::string(SPEC_VERSION)));
    out.set("id", json::Value::string(r.id));
    out.set("model", json::Value::string(r.model));
    json::Value content = json::Value::array();
    for (const auto& b : r.content) content.push_back(dump_block(b));
    out.set("content", std::move(content));
    out.set("stop_reason", json::Value::string(r.stop_reason));
    out.set("usage", dump_usage(r.usage));
    if (r.stop_reason == STOP_STOP_SEQUENCE && r.stop_sequence.has_value() &&
        !r.stop_sequence->empty()) {
        out.set("stop_sequence", json::Value::string(*r.stop_sequence));
    }
    return out;
}

StatusOr<Response> load_response(const json::Value& v) {
    if (!v.is_object()) return invalid_argument("response must be an object");
    OXA_RETURN_IF_ERROR(check_spec_version(v));
    Response r;
    OXA_ASSIGN_OR_RETURN(auto id_val, required(v, "id", "response"));
    r.id = id_val.get().as_string();
    OXA_ASSIGN_OR_RETURN(auto model_val, required(v, "model", "response"));
    r.model = model_val.get().as_string();
    OXA_ASSIGN_OR_RETURN(auto content_val, required(v, "content", "response"));
    if (!content_val.get().is_array()) return invalid_argument("response content must be an array");
    for (const auto& cb : content_val.get().as_array()) {
        OXA_ASSIGN_OR_RETURN(Block b, load_block(cb));
        r.content.push_back(std::move(b));
    }
    OXA_ASSIGN_OR_RETURN(auto sr_val, required(v, "stop_reason", "response"));
    r.stop_reason = sr_val.get().as_string();
    const json::Value* seq = v.find("stop_sequence");
    if (seq != nullptr && seq->is_string()) r.stop_sequence = seq->as_string();
    r.usage = load_usage(v);
    return r;
}

// ---- events ----------------------------------------------------------------------

json::Value dump_event(const Event& e) {
    json::Value out = json::Value::object();
    if (const auto* ms = std::get_if<MessageStart>(&e)) {
        out.set("type", json::Value::string(std::string(EVENT_TYPE_MESSAGE_START)));
        out.set("id", json::Value::string(ms->id));
        out.set("model", json::Value::string(ms->model));
        return out;
    }
    if (const auto* cbs = std::get_if<ContentBlockStart>(&e)) {
        out.set("type", json::Value::string(std::string(EVENT_TYPE_CONTENT_BLOCK_START)));
        out.set("index", json::Value::integer(cbs->index));
        out.set("block", dump_block(cbs->block));
        return out;
    }
    if (const auto* cbd = std::get_if<ContentBlockDelta>(&e)) {
        out.set("type", json::Value::string(std::string(EVENT_TYPE_CONTENT_BLOCK_DELTA)));
        out.set("index", json::Value::integer(cbd->index));
        json::Value d = json::Value::object();
        if (const auto* td = std::get_if<TextDelta>(&cbd->delta)) {
            d.set("type", json::Value::string(std::string(DELTA_TYPE_TEXT_DELTA)));
            d.set("text", json::Value::string(td->text));
        } else {
            const auto& ij = std::get<InputJsonDelta>(cbd->delta);
            d.set("type", json::Value::string(std::string(DELTA_TYPE_INPUT_JSON_DELTA)));
            d.set("partial_json", json::Value::string(ij.partial_json));
        }
        out.set("delta", std::move(d));
        return out;
    }
    if (const auto* cst = std::get_if<ContentBlockStop>(&e)) {
        out.set("type", json::Value::string(std::string(EVENT_TYPE_CONTENT_BLOCK_STOP)));
        out.set("index", json::Value::integer(cst->index));
        return out;
    }
    if (const auto* md = std::get_if<MessageDelta>(&e)) {
        out.set("type", json::Value::string(std::string(EVENT_TYPE_MESSAGE_DELTA)));
        out.set("stop_reason", json::Value::string(md->stop_reason));
        out.set("usage", dump_usage(md->usage));
        if (md->stop_reason == STOP_STOP_SEQUENCE && md->stop_sequence.has_value() &&
            !md->stop_sequence->empty()) {
            out.set("stop_sequence", json::Value::string(*md->stop_sequence));
        }
        return out;
    }
    out.set("type", json::Value::string(std::string(EVENT_TYPE_MESSAGE_DONE)));
    return out;
}

StatusOr<Event> load_event(const json::Value& v) {
    if (!v.is_object()) return invalid_argument("event must be an object");
    OXA_ASSIGN_OR_RETURN(std::string kind, kind_of(v));
    if (kind == EVENT_TYPE_MESSAGE_START) {
        OXA_ASSIGN_OR_RETURN(auto id_val, required(v, "id", "message_start"));
        OXA_ASSIGN_OR_RETURN(auto model_val, required(v, "model", "message_start"));
        return Event(MessageStart{id_val.get().as_string(), model_val.get().as_string()});
    }
    if (kind == EVENT_TYPE_CONTENT_BLOCK_START) {
        OXA_ASSIGN_OR_RETURN(auto idx_val, required(v, "index", "content_block_start"));
        OXA_ASSIGN_OR_RETURN(auto block_val, required(v, "block", "content_block_start"));
        OXA_ASSIGN_OR_RETURN(Block b, load_block(block_val.get()));
        return Event(ContentBlockStart{idx_val.get().as_int(), std::move(b)});
    }
    if (kind == EVENT_TYPE_CONTENT_BLOCK_DELTA) {
        OXA_ASSIGN_OR_RETURN(auto d_val, required(v, "delta", "content_block_delta"));
        const json::Value& d = d_val.get();
        OXA_ASSIGN_OR_RETURN(std::string dkind, kind_of(d));
        Delta delta;
        if (dkind == DELTA_TYPE_TEXT_DELTA) {
            OXA_ASSIGN_OR_RETURN(std::string t, opt_string(d, "text"));
            delta = TextDelta{std::move(t)};
        } else {
            OXA_ASSIGN_OR_RETURN(std::string pj, opt_string(d, "partial_json"));
            delta = InputJsonDelta{std::move(pj)};
        }
        OXA_ASSIGN_OR_RETURN(auto idx_val, required(v, "index", "content_block_delta"));
        return Event(ContentBlockDelta{idx_val.get().as_int(), std::move(delta)});
    }
    if (kind == EVENT_TYPE_CONTENT_BLOCK_STOP) {
        OXA_ASSIGN_OR_RETURN(auto idx_val, required(v, "index", "content_block_stop"));
        return Event(ContentBlockStop{idx_val.get().as_int()});
    }
    if (kind == EVENT_TYPE_MESSAGE_DELTA) {
        MessageDelta md;
        OXA_ASSIGN_OR_RETURN(auto sr_val, required(v, "stop_reason", "message_delta"));
        md.stop_reason = sr_val.get().as_string();
        md.usage = load_usage(v);
        const json::Value* seq = v.find("stop_sequence");
        if (seq != nullptr && seq->is_string()) md.stop_sequence = seq->as_string();
        return Event(std::move(md));
    }
    if (kind == EVENT_TYPE_MESSAGE_DONE) {
        return Event(MessageDone{});
    }
    return invalid_argument("unknown event discriminant: " + kind);
}

json::Value dump_event_stream(const std::vector<Event>& events) {
    json::Value out = json::Value::object();
    out.set("specVersion", json::Value::string(std::string(SPEC_VERSION)));
    json::Value arr = json::Value::array();
    for (const auto& e : events) arr.push_back(dump_event(e));
    out.set("events", std::move(arr));
    return out;
}

StatusOr<std::vector<Event>> load_event_stream(const json::Value& v) {
    const json::Value* events = nullptr;
    if (v.is_array()) {
        events = &v;
    } else if (v.is_object()) {
        OXA_RETURN_IF_ERROR(check_spec_version(v));
        events = v.find("events");
    }
    if (events == nullptr || !events->is_array()) {
        return invalid_argument("event stream must carry events");
    }
    std::vector<Event> out;
    for (const auto& e : events->as_array()) {
        OXA_ASSIGN_OR_RETURN(Event ev, load_event(e));
        out.push_back(std::move(ev));
    }
    return out;
}

json::Value dump_loss(const Loss& l) {
    json::Value out = json::Value::object();
    out.set("path", json::Value::string(l.path));
    out.set("field", json::Value::string(l.field));
    out.set("reason", json::Value::string(l.reason));
    if (!l.detail.empty()) out.set("detail", json::Value::string(l.detail));
    return out;
}

// ---- event-stream invariant checking ---------------------------------------------

namespace {

enum class Phase { NeedStart, Blocks, NeedMessageDone, Done };

struct OpenTool {
    std::string input;
    std::string fragments;
    std::size_t fragment_count = 0;
};

Status validate_tool_input(std::size_t i, const OpenTool& tool, bool allow_synthesized) {
    if (tool.fragment_count == 0) {
        if (tool.input.empty() || allow_synthesized) return ok_status();
        return invalid_argument("event " + std::to_string(i) +
                                ": tool block input without input_json_delta fragments; only encoder "
                                "shorthand may synthesize them");
    }
    if (tool.input != tool.fragments) {
        return invalid_argument("event " + std::to_string(i) +
                                ": tool block input does not equal the concatenation of its " +
                                std::to_string(tool.fragment_count) +
                                " fragments (INV-1 exact text)");
    }
    return ok_status();
}

Status validate_with(const std::vector<Event>& events, bool allow_synthesized) {
    Phase phase = Phase::NeedStart;
    std::int64_t next_index = 0;
    bool open = false;
    std::int64_t open_index = 0;
    bool open_is_tool = false;
    OpenTool tool;

    for (std::size_t i = 0; i < events.size(); ++i) {
        const Event& e = events[i];
        if (const auto* ms = std::get_if<MessageStart>(&e)) {
            (void)ms;
            if (phase != Phase::NeedStart) {
                return invalid_argument("event " + std::to_string(i) + ": duplicate message_start");
            }
            phase = Phase::Blocks;
        } else if (const auto* cbs = std::get_if<ContentBlockStart>(&e)) {
            if (phase != Phase::Blocks) {
                return invalid_argument(
                    "event " + std::to_string(i) +
                    ": content_block_start outside the block region (no message_start, "
                    "or a message_delta already seen)");
            }
            if (open) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": content_block_start with an open block");
            }
            if (cbs->index != next_index) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": content_block_start index " + std::to_string(cbs->index) +
                                        ", want " + std::to_string(next_index));
            }
            next_index += 1;
            open = true;
            open_index = cbs->index;
            if (const auto* tb = std::get_if<TextBlock>(&cbs->block)) {
                (void)tb;
                open_is_tool = false;
            } else if (const auto* tu = std::get_if<ToolUseBlock>(&cbs->block)) {
                open_is_tool = true;
                tool = OpenTool{tu->input, "", 0};
            } else {
                return invalid_argument(
                    "event " + std::to_string(i) +
                    ": content_block_start carries an unsupported block; streams carry "
                    "text and tool_use only");
            }
        } else if (const auto* cbd = std::get_if<ContentBlockDelta>(&e)) {
            if (!open) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": content_block_delta without an open block");
            }
            if (cbd->index != open_index) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": content_block_delta index " + std::to_string(cbd->index) +
                                        " does not match the open block " +
                                        std::to_string(open_index));
            }
            if (open_is_tool) {
                if (const auto* ij = std::get_if<InputJsonDelta>(&cbd->delta)) {
                    tool.fragments += ij->partial_json;
                    tool.fragment_count += 1;
                } else {
                    return invalid_argument(
                        "event " + std::to_string(i) +
                        ": delta type text_delta does not match the open block kind");
                }
            } else {
                if (!std::holds_alternative<TextDelta>(cbd->delta)) {
                    return invalid_argument(
                        "event " + std::to_string(i) +
                        ": delta type input_json_delta does not match the open block kind");
                }
            }
        } else if (const auto* cst = std::get_if<ContentBlockStop>(&e)) {
            if (!open) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": content_block_stop without an open block");
            }
            const std::int64_t idx = open_index;
            const bool was_tool = open_is_tool;
            open = false;
            if (cst->index != idx) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": content_block_stop index " + std::to_string(cst->index) +
                                        " does not match the open block " + std::to_string(idx));
            }
            if (was_tool) {
                OXA_RETURN_IF_ERROR(validate_tool_input(i, tool, allow_synthesized));
            }
        } else if (const auto* md = std::get_if<MessageDelta>(&e)) {
            if (phase != Phase::Blocks) {
                return invalid_argument(
                    "event " + std::to_string(i) +
                    ": message_delta outside the block region (missing message_start "
                    "or out of grammar order)");
            }
            if (open) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": message_delta with an open block");
            }
            if (md->stop_sequence.has_value() && md->stop_reason != STOP_STOP_SEQUENCE) {
                return invalid_argument(
                    "event " + std::to_string(i) +
                    ": stop_sequence is only permitted when stop_reason is "
                    "stop_sequence");
            }
            phase = Phase::NeedMessageDone;
        } else if (std::holds_alternative<MessageDone>(e)) {
            if (phase == Phase::Done) {
                return invalid_argument("event " + std::to_string(i) + ": event after message_done");
            }
            if (phase != Phase::NeedMessageDone) {
                return invalid_argument(
                    "event " + std::to_string(i) +
                    ": message_done without an immediately preceding message_delta");
            }
            phase = Phase::Done;
        }
    }

    if (phase == Phase::Done) return ok_status();
    const std::size_t total = events.size();
    if (open) {
        return invalid_argument("event " + std::to_string(total) + ": block index " +
                                std::to_string(open_index) + " is not stopped");
    }
    if (phase == Phase::NeedMessageDone) {
        return invalid_argument("event " + std::to_string(total) +
                                ": events: missing message_done (stream ends after " +
                                std::to_string(total) + " events)");
    }
    return invalid_argument("event " + std::to_string(total) + ": events: missing message_delta");
}

}  // namespace

Status validate_event_stream(const std::vector<Event>& events) {
    return validate_with(events, false);
}

Status validate_event_stream_for_encoder(const std::vector<Event>& events) {
    return validate_with(events, true);
}

}  // namespace oxa::ir
