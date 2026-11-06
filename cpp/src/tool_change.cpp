#include "oxa/ir.hpp"

namespace oxa::ir {
namespace {
json::Value tool_document(const json::Value& tool) {
    auto parsed = json::parse(R"({"specVersion":"0.3.0","model":"m","messages":[{"role":"user","content":[{"type":"text","text":"x"}]}]})");
    json::Value document = std::move(*parsed);
    json::Value tools = json::Value::array();
    tools.push_back(tool);
    document.set("tools", std::move(tools));
    return document;
}

Status read_extra(const json::Value& object, const char* name, json::Value& out) {
    if (const auto* field = object.find(name)) {
        if (!field->is_object()) return invalid_argument(std::string("tool change ") + name + " must be an object");
        out = *field;
    }
    return ok_status();
}
}

json::Value dump_tool_change(const ToolChange& change) {
    json::Value out = json::Value::object();
    out.set("op", json::Value::string(change.op));
    if (change.tool) {
        Request r;
        r.model = "m";
        r.messages.push_back({"user", {TextBlock{"x"}}});
        r.tools = std::vector<Tool>{*change.tool};
        auto doc = dump_request(r);
        const auto* tools = doc.find("tools");
        if (tools && !tools->as_array().empty()) out.set("tool", tools->as_array()[0]);
    }
    if (change.ref) {
        json::Value ref = json::Value::object();
        ref.set("type", json::Value::string(change.ref->type));
        if (!change.ref->name.empty()) ref.set("name", json::Value::string(change.ref->name));
        if (!change.ref->server_name.empty()) ref.set("server_name", json::Value::string(change.ref->server_name));
        if (!change.ref->extra.as_object().empty()) ref.set("extra", change.ref->extra);
        out.set("ref", std::move(ref));
    }
    if (!change.native.empty()) {
        json::Value native = json::Value::array();
        for (const auto& ann : change.native) native.push_back(dump_native_annotation(ann));
        out.set("native", std::move(native));
    }
    if (!change.extra.as_object().empty()) out.set("extra", change.extra);
    if (!change.tool_extra.as_object().empty()) out.set("tool_extra", change.tool_extra);
    if (!change.definition_extra.as_object().empty()) out.set("definition_extra", change.definition_extra);
    return out;
}

StatusOr<ToolChange> load_tool_change(const json::Value& value) {
    if (!value.is_object()) return invalid_argument("tool change must be an object");
    const auto* op = value.find("op");
    if (!op || !op->is_string() || (op->as_string() != "add" && op->as_string() != "remove")) return invalid_argument("invalid tool change op");
    ToolChange change;
    change.op = op->as_string();
    const auto* tool = value.find("tool");
    const auto* ref = value.find("ref");
    if ((tool == nullptr) == (ref == nullptr) || (change.op == "remove" && tool)) return invalid_argument("tool change requires a valid tool or reference");
    if (tool) {
        auto parsed = load_request(tool_document(*tool));
        if (!parsed.ok()) return parsed.status();
        if (!parsed->tools || parsed->tools->size() != 1) return invalid_argument("invalid tool change definition");
        change.tool = parsed->tools->front();
    }
    if (ref) {
        if (!ref->is_object()) return invalid_argument("tool change reference must be an object");
        const auto* type = ref->find("type");
        if (!type || !type->is_string()) return invalid_argument("tool change reference type required");
        ToolChangeRef r;
        r.type = type->as_string();
        if (const auto* name = ref->find("name")) {
            if (!name->is_string()) return invalid_argument("tool change reference name must be a string");
            r.name = name->as_string();
        }
        if (const auto* server = ref->find("server_name")) {
            if (!server->is_string()) return invalid_argument("tool change reference server_name must be a string");
            r.server_name = server->as_string();
        }
        bool valid = (r.type == "tool_reference" && !r.name.empty() && r.server_name.empty()) ||
                     (r.type == "mcp_tool_reference" && !r.name.empty() && !r.server_name.empty()) ||
                     (r.type == "mcp_toolset_reference" && r.name.empty() && !r.server_name.empty());
        if (!valid) return invalid_argument("invalid tool change reference");
        OXA_RETURN_IF_ERROR(read_extra(*ref, "extra", r.extra));
        change.ref = std::move(r);
    }
    if (const auto* native = value.find("native")) {
        if (!native->is_array()) return invalid_argument("tool change native must be an array");
        for (const auto& item : native->as_array()) {
            OXA_ASSIGN_OR_RETURN(auto annotation, load_native_annotation(item));
            change.native.push_back(std::move(annotation));
        }
    }
    OXA_RETURN_IF_ERROR(read_extra(value, "extra", change.extra));
    OXA_RETURN_IF_ERROR(read_extra(value, "tool_extra", change.tool_extra));
    OXA_RETURN_IF_ERROR(read_extra(value, "definition_extra", change.definition_extra));
    return change;
}
Status validate_tool_availability(const Request& request) {
    auto validate_change = [](const ToolChange& change) -> Status {
        if (!change.extra.is_object() || !change.tool_extra.is_object() || !change.definition_extra.is_object() || (change.ref && !change.ref->extra.is_object())) return invalid_argument("invalid tool change extension");
        auto decoded = load_tool_change(dump_tool_change(change));
        return decoded.ok() ? ok_status() : decoded.status();
    };
    for (std::size_t i = 0; i < request.messages.size(); ++i) {
        for (std::size_t j = 0; j < request.messages[i].content.size(); ++j) {
            if (const auto* block = std::get_if<CompactionBlock>(&request.messages[i].content[j])) {
                if (i != 0 || j != 0) return invalid_argument("compaction must be first block of first message");
                if (block->content && ((!block->content->is_string() && !block->content->is_null()) || (block->content->is_string() && block->content->as_string().empty()))) return invalid_argument("invalid compaction content");
                if (!block->extra.is_object()) return invalid_argument("compaction extra must be an object");
                if (block->tool_changes) for (const auto& change : *block->tool_changes) OXA_RETURN_IF_ERROR(validate_change(change));
            }
        }
    }
    for (const auto& group : request.tool_change_groups) {
        if (group.message_index > request.messages.size() || group.content_index < -1 ||
            (group.content_index >= 0 && (group.message_index >= request.messages.size() || static_cast<std::size_t>(group.content_index) > request.messages[group.message_index].content.size()))) return invalid_argument("invalid tool change group position");
        if (!group.extra.is_object()) return invalid_argument("tool change group extra must be an object");
        for (const auto& part : group.parts) {
            if (part.type == "text" && !part.change) continue;
            if (part.type != "tool_change" || !part.change) return invalid_argument("invalid tool change part");
            OXA_RETURN_IF_ERROR(validate_change(*part.change));
        }
    }
    return ok_status();
}

}  // namespace oxa::ir
