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

StatusOr<std::string> require_string(const json::Value& v, std::string_view key, const char* ctx = "document") {
    const json::Value* out = v.find(key);
    if (out == nullptr) {
        return invalid_argument(std::string(ctx) + " missing \"" + std::string(key) + "\"");
    }
    if (!out->is_string()) {
        return invalid_argument(std::string(ctx) + " field \"" + std::string(key) + "\" must be a string");
    }
    return out->as_string();
}

StatusOr<std::int64_t> require_int(const json::Value& v, std::string_view key, const char* ctx = "document") {
    const json::Value* out = v.find(key);
    if (out == nullptr) {
        return invalid_argument(std::string(ctx) + " missing \"" + std::string(key) + "\"");
    }
    if (!out->is_int()) {
        return invalid_argument(std::string(ctx) + " field \"" + std::string(key) + "\" must be an integer");
    }
    return out->as_int();
}

StatusOr<std::reference_wrapper<const json::Value>> require_object(
    const json::Value& v, std::string_view key, const char* ctx = "document") {
    const json::Value* out = v.find(key);
    if (out == nullptr) {
        return invalid_argument(std::string(ctx) + " missing \"" + std::string(key) + "\"");
    }
    if (!out->is_object()) {
        return invalid_argument(std::string(ctx) + " field \"" + std::string(key) + "\" must be an object");
    }
    return std::cref(*out);
}

StatusOr<std::reference_wrapper<const json::Value>> require_array(
    const json::Value& v, std::string_view key, const char* ctx = "document") {
    const json::Value* out = v.find(key);
    if (out == nullptr) {
        return invalid_argument(std::string(ctx) + " missing \"" + std::string(key) + "\"");
    }
    if (!out->is_array()) {
        return invalid_argument(std::string(ctx) + " field \"" + std::string(key) + "\" must be an array");
    }
    return std::cref(*out);
}

Status check_spec_version(const json::Value& v) {
    const json::Value* sv = v.find("specVersion");
    if (sv == nullptr || !sv->is_string() ||
        (sv->as_string() != SPEC_VERSION && sv->as_string() != LEGACY_SPEC_VERSION)) {
        return invalid_argument(
            "unsupported specVersion, want \"0.1.0\" or \"" + std::string(SPEC_VERSION) + "\"");
    }
    return ok_status();
}

bool has(const json::Value& v, std::string_view key) {
    const json::Value* f = v.find(key);
    return f != nullptr && !f->is_null();
}

Status require_string_field(const json::Value& obj, std::string_view key, const char* ctx) {
    const auto* v = obj.find(key);
    if (!v || !v->is_string()) {
        return invalid_argument(std::string(ctx) + ": citation requires string \"" + std::string(key) + "\"");
    }
    return ok_status();
}

Status require_nonneg_int(const json::Value& obj, std::string_view key, const char* ctx) {
    const auto* v = obj.find(key);
    if (!v || !v->is_int() || v->as_int() < 0) {
        return invalid_argument(std::string(ctx) + ": citation requires non-negative integer \"" + std::string(key) + "\"");
    }
    return ok_status();
}

Status require_nullable_string(const json::Value& obj, std::string_view key, const char* ctx) {
    const auto* v = obj.find(key);
    if (!v || (!v->is_null() && !v->is_string())) {
        return invalid_argument(std::string(ctx) + ": citation field \"" + std::string(key) + "\" must be string or null");
    }
    return ok_status();
}

}  // namespace

// Validates the canonical citation JSON object carried in TextCitation::value
// (spec/01 §3.4): one of the five locked location shapes with no extra
// properties.
Status validate_citation_value(std::string_view val) {
    if (val.empty()) {
        return invalid_argument("ir: citation value cannot be empty");
    }
    auto parsed = json::parse(val);
    if (!parsed.ok() || !parsed->is_object()) {
        return invalid_argument("ir: citation value must be a JSON object");
    }
    const json::Value& obj = *parsed;
    const auto* type_val = obj.find("type");
    if (type_val == nullptr || !type_val->is_string()) {
        return invalid_argument("ir: citation value requires type field");
    }
    const std::string& cite_type = type_val->as_string();
    if (cite_type == "char_location") {
        for (const auto& [k, _] : obj.as_object()) {
            if (k != "type" && k != "cited_text" && k != "document_index" && k != "document_title" &&
                k != "start_char_index" && k != "end_char_index" && k != "file_id") {
                return invalid_argument("ir: unknown property \"" + k + "\" on char_location citation");
            }
        }
        OXA_RETURN_IF_ERROR(require_string_field(obj, "cited_text", "char_location"));
        OXA_RETURN_IF_ERROR(require_nonneg_int(obj, "document_index", "char_location"));
        OXA_RETURN_IF_ERROR(require_nullable_string(obj, "document_title", "char_location"));
        OXA_RETURN_IF_ERROR(require_nonneg_int(obj, "start_char_index", "char_location"));
        const auto* end_val = obj.find("end_char_index");
        if (!end_val || !end_val->is_int() || end_val->as_int() <= obj.find("start_char_index")->as_int()) {
            return invalid_argument("char_location: end_char_index must be > start_char_index");
        }
        if (const auto* fid = obj.find("file_id"); fid && !fid->is_null() && !fid->is_string()) {
            return invalid_argument("char_location: file_id must be string or null");
        }
    } else if (cite_type == "page_location") {
        for (const auto& [k, _] : obj.as_object()) {
            if (k != "type" && k != "cited_text" && k != "document_index" && k != "document_title" &&
                k != "start_page_number" && k != "end_page_number" && k != "file_id") {
                return invalid_argument("ir: unknown property \"" + k + "\" on page_location citation");
            }
        }
        OXA_RETURN_IF_ERROR(require_string_field(obj, "cited_text", "page_location"));
        OXA_RETURN_IF_ERROR(require_nonneg_int(obj, "document_index", "page_location"));
        OXA_RETURN_IF_ERROR(require_nullable_string(obj, "document_title", "page_location"));
        const auto* start_val = obj.find("start_page_number");
        if (!start_val || !start_val->is_int() || start_val->as_int() < 1) {
            return invalid_argument("page_location: start_page_number must be integer >= 1");
        }
        const auto* end_val = obj.find("end_page_number");
        if (!end_val || !end_val->is_int() || end_val->as_int() < start_val->as_int()) {
            return invalid_argument("page_location: end_page_number must be >= start_page_number");
        }
        if (const auto* fid = obj.find("file_id"); fid && !fid->is_null() && !fid->is_string()) {
            return invalid_argument("page_location: file_id must be string or null");
        }
    } else if (cite_type == "content_block_location") {
        for (const auto& [k, _] : obj.as_object()) {
            if (k != "type" && k != "cited_text" && k != "document_index" && k != "document_title" &&
                k != "start_block_index" && k != "end_block_index" && k != "file_id") {
                return invalid_argument("ir: unknown property \"" + k + "\" on content_block_location citation");
            }
        }
        OXA_RETURN_IF_ERROR(require_string_field(obj, "cited_text", "content_block_location"));
        OXA_RETURN_IF_ERROR(require_nonneg_int(obj, "document_index", "content_block_location"));
        OXA_RETURN_IF_ERROR(require_nullable_string(obj, "document_title", "content_block_location"));
        OXA_RETURN_IF_ERROR(require_nonneg_int(obj, "start_block_index", "content_block_location"));
        const auto* end_val = obj.find("end_block_index");
        if (!end_val || !end_val->is_int() || end_val->as_int() <= obj.find("start_block_index")->as_int()) {
            return invalid_argument("content_block_location: end_block_index must be > start_block_index");
        }
        if (const auto* fid = obj.find("file_id"); fid && !fid->is_null() && !fid->is_string()) {
            return invalid_argument("content_block_location: file_id must be string or null");
        }
    } else if (cite_type == "web_search_result_location") {
        for (const auto& [k, _] : obj.as_object()) {
            if (k != "type" && k != "cited_text" && k != "encrypted_index" && k != "title" && k != "url") {
                return invalid_argument("ir: unknown property \"" + k + "\" on web_search_result_location citation");
            }
        }
        OXA_RETURN_IF_ERROR(require_string_field(obj, "cited_text", "web_search_result_location"));
        OXA_RETURN_IF_ERROR(require_string_field(obj, "encrypted_index", "web_search_result_location"));
        OXA_RETURN_IF_ERROR(require_nullable_string(obj, "title", "web_search_result_location"));
        OXA_RETURN_IF_ERROR(require_string_field(obj, "url", "web_search_result_location"));
    } else if (cite_type == "search_result_location") {
        for (const auto& [k, _] : obj.as_object()) {
            if (k != "type" && k != "cited_text" && k != "search_result_index" && k != "source" &&
                k != "title" && k != "start_block_index" && k != "end_block_index") {
                return invalid_argument("ir: unknown property \"" + k + "\" on search_result_location citation");
            }
        }
        OXA_RETURN_IF_ERROR(require_string_field(obj, "cited_text", "search_result_location"));
        OXA_RETURN_IF_ERROR(require_nonneg_int(obj, "search_result_index", "search_result_location"));
        OXA_RETURN_IF_ERROR(require_string_field(obj, "source", "search_result_location"));
        OXA_RETURN_IF_ERROR(require_nullable_string(obj, "title", "search_result_location"));
        OXA_RETURN_IF_ERROR(require_nonneg_int(obj, "start_block_index", "search_result_location"));
        const auto* end_val = obj.find("end_block_index");
        if (!end_val || !end_val->is_int() || end_val->as_int() <= obj.find("start_block_index")->as_int()) {
            return invalid_argument("search_result_location: end_block_index must be > start_block_index");
        }
    } else {
        return invalid_argument("ir: unknown citation type \"" + cite_type + "\"");
    }
    return ok_status();
}

// ---- blocks ----------------------------------------------------------------

namespace {

#if defined(__EXCEPTIONS) || defined(__cpp_exceptions)
bool block_has_native(const Block& b) {
    return std::visit([](const auto& val) {
        return !val.native.empty();
    }, b);
}
#endif

template <typename F>
json::Value dump_block_impl(const Block& b, F&& recurse) {
    json::Value out = json::Value::object();
    if (const auto* t = std::get_if<TextBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_TEXT)));
        out.set("text", json::Value::string(t->text));
        if (t->citations.has_value() && !t->citations->empty()) {
            json::Value cites = json::Value::array();
            for (const auto& c : *t->citations) {
                json::Value cw = json::Value::object();
                cw.set("source", json::Value::string(c.source));
                cw.set("value", json::Value::string(c.value));
                cites.push_back(std::move(cw));
            }
            out.set("citations", std::move(cites));
        }
        return out;
    }
    if (const auto* rt = std::get_if<RedactedThinkingBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_REDACTED_THINKING)));
        out.set("data", json::Value::string(rt->data));
        return out;
    }
    if (const auto* t = std::get_if<ThinkingBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_THINKING)));
        out.set("thinking", json::Value::string(t->thinking));
        if (t->signature.has_value()) out.set("signature", json::Value::string(*t->signature));
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
    if (const auto* doc = std::get_if<DocumentBlock>(&b)) {
        const bool has_data = doc->data.has_value() && !doc->data->empty();
        const bool has_url = doc->url.has_value() && !doc->url->empty();
        const bool has_file = doc->file_id.has_value() && !doc->file_id->empty();
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_DOCUMENT)));
        if (has_data) {
            if (doc->media_type.has_value()) {
                out.set("media_type", json::Value::string(*doc->media_type));
            }
            out.set("data", json::Value::string(*doc->data));
        } else if (has_url) {
            out.set("url", json::Value::string(*doc->url));
        } else if (has_file) {
            out.set("file_id", json::Value::string(*doc->file_id));
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
    if (const auto* cu = std::get_if<CustomToolUseBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_CUSTOM_TOOL_USE)));
        out.set("id", json::Value::string(cu->id)); out.set("name", json::Value::string(cu->name));
        out.set("input", json::Value::string(cu->input)); return out;
    }
    if (const auto* cr = std::get_if<CustomToolResultBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_CUSTOM_TOOL_RESULT)));
        out.set("tool_use_id", json::Value::string(cr->tool_use_id));
        if (const auto* text = std::get_if<std::string>(&cr->output)) out.set("output", json::Value::string(*text));
        else { json::Value output = json::Value::array();
            for (const auto& inner : std::get<std::vector<BlockHolder>>(cr->output)) output.push_back(recurse(inner.block));
            out.set("output", std::move(output)); }
        return out;
    }
    if (const auto* ap = std::get_if<ApplyPatchToolUseBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_APPLY_PATCH_TOOL_USE)));
        out.set("id", json::Value::string(ap->id));
        out.set("status", json::Value::string(ap->status));
        json::Value op = json::Value::object();
        if (const auto* cf = std::get_if<CreateFileOperation>(&ap->operation)) {
            op.set("type", json::Value::string(std::string(APPLY_PATCH_OP_CREATE_FILE)));
            op.set("path", json::Value::string(cf->path));
            op.set("diff", json::Value::string(cf->diff));
        } else if (const auto* df = std::get_if<DeleteFileOperation>(&ap->operation)) {
            op.set("type", json::Value::string(std::string(APPLY_PATCH_OP_DELETE_FILE)));
            op.set("path", json::Value::string(df->path));
        } else if (const auto* uf = std::get_if<UpdateFileOperation>(&ap->operation)) {
            op.set("type", json::Value::string(std::string(APPLY_PATCH_OP_UPDATE_FILE)));
            op.set("path", json::Value::string(uf->path));
            op.set("diff", json::Value::string(uf->diff));
        }
        out.set("operation", std::move(op));
        return out;
    }
    if (const auto* apr = std::get_if<ApplyPatchToolResultBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT)));
        out.set("tool_use_id", json::Value::string(apr->tool_use_id));
        out.set("status", json::Value::string(apr->status));
        if (apr->output.has_value()) {
            out.set("output", json::Value::string(*apr->output));
        }
        return out;
    }
    if (const auto* tr = std::get_if<ToolResultBlock>(&b)) {
        out.set("type", json::Value::string(std::string(BLOCK_TYPE_TOOL_RESULT)));
        out.set("tool_use_id", json::Value::string(tr->tool_use_id));
        json::Value content = json::Value::array();
        for (const auto& inner : tr->content) content.push_back(recurse(inner.block));
        out.set("content", std::move(content));
        if (tr->is_error) out.set("is_error", json::Value::boolean(true));
        return out;
    }
    return out;
}

}  // namespace

json::Value dump_block(const Block& b) {
#if defined(__EXCEPTIONS) || defined(__cpp_exceptions)
    if (block_has_native(b)) {
        throw std::invalid_argument("native is only permitted on request blocks");
    }
    if (std::holds_alternative<DocumentBlock>(b)) {
        throw std::invalid_argument("document block is request-side only and forbidden in response content");
    }
#endif
    return dump_block_impl(b, [](const Block& inner) { return dump_block(inner); });
}

json::Value dump_request_block(const Block& b) {
    json::Value out = dump_block_impl(b, [](const Block& inner) { return dump_request_block(inner); });
    const auto* nat = std::visit([](const auto& val) -> const std::vector<NativeAnnotation>* {
        return &val.native;
    }, b);
    if (nat != nullptr && !nat->empty()) {
        json::Value native_arr = json::Value::array();
        for (const auto& a : *nat) {
            native_arr.push_back(dump_native_annotation(a));
        }
        out.set("native", std::move(native_arr));
    }
    return out;
}

template <typename Recurse>
StatusOr<Block> load_block_impl(const json::Value& v, Recurse&& recurse, std::vector<NativeAnnotation> native) {
    OXA_ASSIGN_OR_RETURN(std::string kind, kind_of(v));
    if (kind == BLOCK_TYPE_TEXT) {
        OXA_ASSIGN_OR_RETURN(std::string txt, require_string(v, "text", "text block"));
        TextBlock block{std::move(txt), std::move(native)};
        if (const auto* cites_val = v.find("citations")) {
            if (!cites_val->is_array()) {
                return invalid_argument("text block citations must be an array");
            }
            std::vector<TextCitation> cites;
            for (const auto& c : cites_val->as_array()) {
                if (!c.is_object()) return invalid_argument("citation must be an object");
                OXA_ASSIGN_OR_RETURN(std::string source, require_string(c, "source", "citation"));
                if (source != "anthropic") {
                    return invalid_argument("ir: unknown citation source \"" + source + "\"");
                }
                OXA_ASSIGN_OR_RETURN(std::string value, require_string(c, "value", "citation"));
                OXA_RETURN_IF_ERROR(validate_citation_value(value));
                cites.push_back(TextCitation{std::move(source), std::move(value)});
            }
            if (!cites.empty()) block.citations = std::move(cites);
        }
        return Block(std::move(block));
    }
    if (kind == BLOCK_TYPE_REDACTED_THINKING) {
        OXA_ASSIGN_OR_RETURN(std::string data, require_string(v, "data", "redacted_thinking block"));
        return Block(RedactedThinkingBlock{std::move(data), std::move(native)});
    }
    if (kind == BLOCK_TYPE_THINKING) {
        OXA_ASSIGN_OR_RETURN(std::string thinking, require_string(v, "thinking", "thinking block"));
        ThinkingBlock block{std::move(thinking), std::nullopt, std::move(native)};
        if (has(v, "signature")) {
            OXA_ASSIGN_OR_RETURN(std::string signature, require_string(v, "signature", "thinking block"));
            block.signature = std::move(signature);
        }
        return Block(std::move(block));
    }
    if (kind == BLOCK_TYPE_IMAGE) {
        ImageBlock img;
        img.native = std::move(native);
        if (has(v, "media_type")) {
            OXA_ASSIGN_OR_RETURN(std::string mt, require_string(v, "media_type", "image block"));
            img.media_type = std::move(mt);
        }
        if (has(v, "data")) {
            OXA_ASSIGN_OR_RETURN(std::string dt, require_string(v, "data", "image block"));
            img.data = std::move(dt);
        }
        if (has(v, "url")) {
            OXA_ASSIGN_OR_RETURN(std::string u, require_string(v, "url", "image block"));
            img.url = std::move(u);
        }
        const bool has_data = img.data.has_value() && !img.data->empty();
        const bool has_url = img.url.has_value() && !img.url->empty();
        if (has_data == has_url) {
            return invalid_argument("image block must have either data or url, not both or neither");
        }
        if (has_data && !img.media_type.has_value()) {
            return invalid_argument("image block with data requires media_type");
        }
        return Block(std::move(img));
    }
    if (kind == BLOCK_TYPE_DOCUMENT) {
        DocumentBlock doc;
        doc.native = std::move(native);
        if (has(v, "media_type")) {
            OXA_ASSIGN_OR_RETURN(std::string mt, require_string(v, "media_type", "document block"));
            doc.media_type = std::move(mt);
        }
        if (has(v, "data")) {
            OXA_ASSIGN_OR_RETURN(std::string dt, require_string(v, "data", "document block"));
            doc.data = std::move(dt);
        }
        if (has(v, "url")) {
            OXA_ASSIGN_OR_RETURN(std::string u, require_string(v, "url", "document block"));
            doc.url = std::move(u);
        }
        if (has(v, "file_id")) {
            OXA_ASSIGN_OR_RETURN(std::string fid, require_string(v, "file_id", "document block"));
            doc.file_id = std::move(fid);
        }
        const bool has_data = doc.data.has_value() && !doc.data->empty();
        const bool has_url = doc.url.has_value() && !doc.url->empty();
        const bool has_file = doc.file_id.has_value() && !doc.file_id->empty();
        const int count = (has_data ? 1 : 0) + (has_url ? 1 : 0) + (has_file ? 1 : 0);
        if (count != 1) {
            return invalid_argument("document block must have exactly one of data, url, or file_id");
        }
        if (has_data && !doc.media_type.has_value()) {
            return invalid_argument("document block with data requires media_type");
        }
        if (!has_data && doc.media_type.has_value()) {
            return invalid_argument("document block without data must not have media_type");
        }
        return Block(std::move(doc));
    }
    if (kind == BLOCK_TYPE_TOOL_USE) {
        ToolUseBlock tu;
        tu.native = std::move(native);
        OXA_ASSIGN_OR_RETURN(tu.id, require_string(v, "id", "tool_use block"));
        OXA_ASSIGN_OR_RETURN(tu.name, require_string(v, "name", "tool_use block"));
        OXA_ASSIGN_OR_RETURN(tu.input, require_string(v, "input", "tool_use block"));
        if (tu.id.empty() || tu.name.empty()) return invalid_argument("tool_use id and name must be non-empty");
        return Block(std::move(tu));
    }
    if (kind == BLOCK_TYPE_CUSTOM_TOOL_USE) {
        CustomToolUseBlock cu;
        cu.native = std::move(native);
        OXA_ASSIGN_OR_RETURN(cu.id, require_string(v, "id", "custom_tool_use block"));
        OXA_ASSIGN_OR_RETURN(cu.name, require_string(v, "name", "custom_tool_use block"));
        OXA_ASSIGN_OR_RETURN(cu.input, require_string(v, "input", "custom_tool_use block"));
        if (cu.id.empty() || cu.name.empty()) return invalid_argument("custom_tool_use id and name must be non-empty");
        return Block(std::move(cu));
    }
    if (kind == BLOCK_TYPE_CUSTOM_TOOL_RESULT) {
        CustomToolResultBlock cr;
        cr.native = std::move(native);
        OXA_ASSIGN_OR_RETURN(cr.tool_use_id, require_string(v, "tool_use_id", "custom_tool_result block"));
        if (cr.tool_use_id.empty()) return invalid_argument("custom_tool_result tool_use_id must be non-empty");
        if (v.find("is_error") != nullptr) return invalid_argument("custom_tool_result has no is_error field");
        const json::Value* output = v.find("output");
        if (output == nullptr) return invalid_argument("custom_tool_result missing output");
        if (output->is_string()) cr.output = output->as_string();
        else if (output->is_array()) { std::vector<BlockHolder> parts;
            for (const auto& part : output->as_array()) {
                OXA_ASSIGN_OR_RETURN(Block inner, recurse(part));
                if (!std::holds_alternative<TextBlock>(inner) && !std::holds_alternative<ImageBlock>(inner))
                    return invalid_argument("custom_tool_result output accepts only text and image blocks");
                parts.push_back(BlockHolder{std::move(inner)});
            }
            cr.output = std::move(parts);
        } else return invalid_argument("custom_tool_result output must be a string or array");
        return Block(std::move(cr));
    }
    if (kind == BLOCK_TYPE_APPLY_PATCH_TOOL_USE) {
        ApplyPatchToolUseBlock ap;
        ap.native = std::move(native);
        OXA_ASSIGN_OR_RETURN(ap.id, require_string(v, "id", "apply_patch_tool_use block"));
        OXA_ASSIGN_OR_RETURN(ap.status, require_string(v, "status", "apply_patch_tool_use block"));
        if (ap.status != APPLY_PATCH_CALL_STATUS_IN_PROGRESS && ap.status != APPLY_PATCH_CALL_STATUS_COMPLETED) {
            return invalid_argument("invalid apply_patch_tool_use status: " + ap.status);
        }
        OXA_ASSIGN_OR_RETURN(auto op_val, require_object(v, "operation", "apply_patch_tool_use block"));
        const json::Value& op = op_val.get();
        OXA_ASSIGN_OR_RETURN(std::string op_type, require_string(op, "type", "operation"));
        if (op_type == APPLY_PATCH_OP_CREATE_FILE) {
            OXA_ASSIGN_OR_RETURN(std::string path, require_string(op, "path", "create_file operation"));
            OXA_ASSIGN_OR_RETURN(std::string diff, require_string(op, "diff", "create_file operation"));
            ap.operation = CreateFileOperation{std::move(path), std::move(diff)};
        } else if (op_type == APPLY_PATCH_OP_DELETE_FILE) {
            OXA_ASSIGN_OR_RETURN(std::string path, require_string(op, "path", "delete_file operation"));
            if (op.find("diff") != nullptr) {
                return invalid_argument("delete_file operation must not carry diff");
            }
            ap.operation = DeleteFileOperation{std::move(path)};
        } else if (op_type == APPLY_PATCH_OP_UPDATE_FILE) {
            OXA_ASSIGN_OR_RETURN(std::string path, require_string(op, "path", "update_file operation"));
            OXA_ASSIGN_OR_RETURN(std::string diff, require_string(op, "diff", "update_file operation"));
            ap.operation = UpdateFileOperation{std::move(path), std::move(diff)};
        } else {
            return invalid_argument("unknown apply_patch operation type: " + op_type);
        }
        return Block(std::move(ap));
    }
    if (kind == BLOCK_TYPE_APPLY_PATCH_TOOL_RESULT) {
        ApplyPatchToolResultBlock apr;
        apr.native = std::move(native);
        OXA_ASSIGN_OR_RETURN(apr.tool_use_id, require_string(v, "tool_use_id", "apply_patch_tool_result block"));
        OXA_ASSIGN_OR_RETURN(apr.status, require_string(v, "status", "apply_patch_tool_result block"));
        if (apr.status != APPLY_PATCH_RESULT_STATUS_COMPLETED && apr.status != APPLY_PATCH_RESULT_STATUS_FAILED) {
            return invalid_argument("invalid apply_patch_tool_result status: " + apr.status);
        }
        const json::Value* out_val = v.find("output");
        if (out_val != nullptr && !out_val->is_null()) {
            if (!out_val->is_string()) return invalid_argument("apply_patch_tool_result output must be a string");
            apr.output = out_val->as_string();
        }
        return Block(std::move(apr));
    }
    if (kind == BLOCK_TYPE_TOOL_RESULT) {
        ToolResultBlock tr;
        tr.native = std::move(native);
        OXA_ASSIGN_OR_RETURN(tr.tool_use_id, require_string(v, "tool_use_id", "tool_result block"));
        OXA_ASSIGN_OR_RETURN(auto content, require_array(v, "content", "tool_result block"));
        for (const auto& inner : content.get().as_array()) {
            OXA_ASSIGN_OR_RETURN(Block b, recurse(inner));
            tr.content.push_back(BlockHolder{std::move(b)});
        }
        const json::Value* is_err = v.find("is_error");
        if (is_err != nullptr) {
            if (!is_err->is_bool()) return invalid_argument("tool_result is_error must be a boolean");
            tr.is_error = is_err->as_bool();
        }
        return Block(std::move(tr));
    }
    return invalid_argument("unknown block discriminant: " + kind);
}

StatusOr<Block> load_block(const json::Value& v) {
    if (!v.is_object()) return invalid_argument("block must be an object");
    if (v.find("native") != nullptr) {
        return invalid_argument("native is only permitted on request blocks");
    }
    if (const auto* t = v.find("type"); t && t->is_string() && t->as_string() == BLOCK_TYPE_DOCUMENT) {
        return invalid_argument("unknown block discriminant: document");
    }
    return load_block_impl(v, [](const json::Value& inner) { return load_block(inner); }, {});
}

StatusOr<Block> load_request_block(const json::Value& v) {
    if (!v.is_object()) return invalid_argument("block must be an object");
    std::vector<NativeAnnotation> native;
    if (const auto* nat = v.find("native")) {
        if (!nat->is_array()) return invalid_argument("native must be an array");
        for (const auto& item : nat->as_array()) {
            OXA_ASSIGN_OR_RETURN(auto ann, load_native_annotation(item));
            native.push_back(std::move(ann));
        }
    }
    return load_block_impl(v, [](const json::Value& inner) { return load_request_block(inner); }, std::move(native));
}

// ---- request -----------------------------------------------------------------

Status validate_tool_strict(const Request& req) {
    if (req.tools.has_value()) {
        for (const auto& tool : *req.tools) {
            if (tool.strict.has_value() && tool.type != "function") {
                return invalid_argument("ir: only function tools may carry strict");
            }
        }
    }
    return ok_status();
}

json::Value dump_request(const Request& r) {
    json::Value out = json::Value::object();
    out.set("specVersion", json::Value::string(std::string(SPEC_VERSION)));
    out.set("model", json::Value::string(r.model));
    json::Value messages = json::Value::array();
    for (const auto& m : r.messages) {
        json::Value mv = json::Value::object();
        mv.set("role", json::Value::string(m.role));
        json::Value content = json::Value::array();
        for (const auto& b : m.content) content.push_back(dump_request_block(b));
        mv.set("content", std::move(content));
        messages.push_back(std::move(mv));
    }
    out.set("messages", std::move(messages));

    if (!r.native.empty()) {
        json::Value native_arr = json::Value::array();
        for (const auto& a : r.native) {
            native_arr.push_back(dump_native_annotation(a));
        }
        out.set("native", std::move(native_arr));
    }

    if (!r.system.empty()) {
        json::Value system = json::Value::array();
        for (const auto& s : r.system) {
            system.push_back(dump_request_block(TextBlock{s.text, s.native}));
        }
        out.set("system", std::move(system));
    }
    if (r.tools.has_value() && !r.tools->empty()) {
        json::Value tools = json::Value::array();
        for (const auto& t : *r.tools) {
            json::Value tv = json::Value::object();
            if (t.type == TOOL_TYPE_APPLY_PATCH) {
                tv.set("type", json::Value::string(std::string(TOOL_TYPE_APPLY_PATCH)));
            } else if (t.type == "native") {
                tv.set("type", json::Value::string("native"));
                tv.set("name", json::Value::string(t.name));
                tv.set("source", json::Value::string(t.source.value_or("")));
                tv.set("definition", json::Value::string(t.definition.value_or("")));
            } else if (t.type == "custom") {
                tv.set("type", json::Value::string("custom")); tv.set("name", json::Value::string(t.name));
                if (t.has_description) tv.set("description", json::Value::string(t.description));
                if (t.format.has_value()) tv.set("format", *t.format);
            } else {
                tv.set("name", json::Value::string(t.name));
                if (t.has_description) tv.set("description", json::Value::string(t.description));
                tv.set("input_schema", t.input_schema);
            }
            if (t.strict.has_value()) tv.set("strict", json::Value::boolean(*t.strict));
            if (!t.native.empty()) {
                json::Value t_nat = json::Value::array();
                for (const auto& a : t.native) {
                    t_nat.push_back(dump_native_annotation(a));
                }
                tv.set("native", std::move(t_nat));
            }
            tools.push_back(std::move(tv));
        }
        out.set("tools", std::move(tools));
    }
    if (r.tool_choice.has_value()) {
        json::Value tc = json::Value::object();
        tc.set("mode", json::Value::string(r.tool_choice->mode));
        if (r.tool_choice->mode == TOOL_CHOICE_TOOL) {
            if (r.tool_choice->tool_type == TOOL_TYPE_APPLY_PATCH) {
                tc.set("tool_type", json::Value::string(std::string(TOOL_TYPE_APPLY_PATCH)));
            } else if (r.tool_choice->name.has_value()) {
                tc.set("name", json::Value::string(*r.tool_choice->name));
                if (r.tool_choice->tool_type.has_value()) tc.set("tool_type", json::Value::string(*r.tool_choice->tool_type));
            }
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
        if (r.params->reasoning_effort.has_value()) {
            p.set("reasoning_effort", json::Value::string(*r.params->reasoning_effort));
        }
        if (r.params->response_format.has_value()) {
            const auto& rf = *r.params->response_format;
            json::Value format = json::Value::object();
            format.set("type", json::Value::string(rf.type));
            if (rf.type == "json_schema") {
                if (!rf.name.empty()) format.set("name", json::Value::string(rf.name));
                if (rf.description.has_value()) {
                    format.set("description", json::Value::string(*rf.description));
                }
                if (rf.schema.has_value()) format.set("schema", *rf.schema);
                if (rf.strict.has_value()) format.set("strict", json::Value::boolean(*rf.strict));
            }
            p.set("response_format", std::move(format));
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

StatusOr<json::Value> dump_request_checked(const Request& r) {
    OXA_RETURN_IF_ERROR(validate_tool_strict(r));
    OXA_RETURN_IF_ERROR(validate_native_request(r));
    json::Value out = dump_request(r);
    auto parsed = load_request(out);
    if (!parsed.ok()) return parsed.status();
    return out;
}

StatusOr<std::string> dump_request_json(const Request& r) {
    OXA_ASSIGN_OR_RETURN(json::Value out, dump_request_checked(r));
    if (r.params && r.params->response_format && r.params->response_format->type == "json_schema" && r.params->response_format->schema) {
        OXA_ASSIGN_OR_RETURN(json::Value schema, json::preserve_source(*r.params->response_format->schema));
        auto* params = out.find("params");
        auto* format = params ? params->find("response_format") : nullptr;
        auto* target = format ? format->find("schema") : nullptr;
        if (!target) return invalid_argument("IR response schema missing from encoded request");
        *target = std::move(schema);
    }
    return json::serialize(out);
}

StatusOr<Request> load_request(const json::Value& v) {
    if (!v.is_object()) return invalid_argument("request must be an object");
    OXA_RETURN_IF_ERROR(check_spec_version(v));
    Request r;
    OXA_ASSIGN_OR_RETURN(r.model, require_string(v, "model", "request"));

    const json::Value* native_val = v.find("native");
    if (native_val != nullptr) {
        if (!native_val->is_array()) return invalid_argument("request native must be an array");
        for (const auto& item : native_val->as_array()) {
            OXA_ASSIGN_OR_RETURN(auto ann, load_native_annotation(item));
            r.native.push_back(std::move(ann));
        }
    }

    const json::Value* system = v.find("system");
    if (system != nullptr) {
        if (!system->is_array()) return invalid_argument("request system must be an array");
        for (const auto& sb : system->as_array()) {
            OXA_ASSIGN_OR_RETURN(Block b, load_request_block(sb));
            if (!std::holds_alternative<TextBlock>(b)) {
                return invalid_argument("system block must be text");
            }
            r.system.push_back(std::get<TextBlock>(b));
        }
    }

    OXA_ASSIGN_OR_RETURN(auto messages_val, require_array(v, "messages", "request"));
    if (messages_val.get().as_array().empty()) {
        return invalid_argument("request messages cannot be empty");
    }
    for (const auto& mv : messages_val.get().as_array()) {
        if (!mv.is_object()) return invalid_argument("message must be an object");
        Message m;
        OXA_ASSIGN_OR_RETURN(m.role, require_string(mv, "role", "message"));
        if (m.role != ROLE_USER && m.role != ROLE_ASSISTANT) {
            return invalid_argument("invalid message role: " + m.role);
        }
        OXA_ASSIGN_OR_RETURN(auto content_val, require_array(mv, "content", "message"));
        if (content_val.get().as_array().empty()) {
            return invalid_argument("message content cannot be empty");
        }
        for (const auto& cb : content_val.get().as_array()) {
            OXA_ASSIGN_OR_RETURN(Block b, load_request_block(cb));
            m.content.push_back(std::move(b));
        }
        r.messages.push_back(std::move(m));
    }

    const json::Value* tools = v.find("tools");
    if (tools != nullptr) {
        if (!tools->is_array()) return invalid_argument("request tools must be an array");
        std::vector<Tool> out;
        for (const auto& tv : tools->as_array()) {
            if (!tv.is_object()) return invalid_argument("tool must be an object");
            Tool t;
            if (const auto* type = tv.find("type"); type != nullptr) {
                if (!type->is_string()) return invalid_argument("tool type must be a string");
                t.type = type->as_string();
            }
            if (t.type != "function" && t.type != "custom" && t.type != "native" && t.type != TOOL_TYPE_APPLY_PATCH)
                return invalid_argument("tool type must be function, custom, native, or apply_patch");
            if (const auto* strict = tv.find("strict")) {
                if (t.type != "function" || !strict->is_bool()) return invalid_argument("strict is only allowed as a boolean on function tools");
                t.strict = strict->as_bool();
            }
            if (t.type == TOOL_TYPE_APPLY_PATCH) {
                if (tv.find("name") != nullptr || tv.find("description") != nullptr ||
                    tv.find("input_schema") != nullptr || tv.find("format") != nullptr ||
                    tv.find("source") != nullptr || tv.find("definition") != nullptr) {
                    return invalid_argument("apply_patch tool must carry only type: apply_patch");
                }
            } else if (t.type == "native") {
                if (tv.find("description") != nullptr || tv.find("input_schema") != nullptr ||
                    tv.find("format") != nullptr || tv.find("native") != nullptr) {
                    return invalid_argument("native tool must only carry type, name, source, and definition");
                }
                OXA_ASSIGN_OR_RETURN(t.name, require_string(tv, "name", "tool"));
                if (t.name.empty()) return invalid_argument("tool name must be non-empty");
                OXA_ASSIGN_OR_RETURN(std::string src, require_string(tv, "source", "tool"));
                if (src.empty()) return invalid_argument("tool source must be non-empty");
                t.source = std::move(src);
                OXA_ASSIGN_OR_RETURN(std::string def, require_string(tv, "definition", "tool"));
                if (def.empty()) return invalid_argument("tool definition must be non-empty");
                auto def_json = json::parse(def);
                if (!def_json.ok() || !def_json.value().is_object()) {
                    return invalid_argument("native tool definition must be a valid JSON object");
                }
                const auto* def_type = def_json.value().find("type");
                if (def_type == nullptr || !def_type->is_string() || def_type->as_string().empty() ||
                    def_type->as_string() == "function" || def_type->as_string() == "custom") {
                    return invalid_argument("native tool definition requires a native type");
                }
                const auto* def_name = def_json.value().find("name");
                const auto* def_server_label = def_json.value().find("server_label");
                std::string expected_name = (def_name != nullptr && def_name->is_string() && !def_name->as_string().empty())
                    ? def_name->as_string()
                    : (def_server_label != nullptr && def_server_label->is_string() && !def_server_label->as_string().empty())
                    ? def_server_label->as_string()
                    : def_type->as_string();
                if (expected_name != t.name) {
                    return invalid_argument("native tool definition name must match tool name");
                }
                t.definition = std::move(def);
            } else {
                OXA_ASSIGN_OR_RETURN(t.name, require_string(tv, "name", "tool"));
                if (t.name.empty()) return invalid_argument("tool name must be non-empty");
                if (tv.find("source") != nullptr || tv.find("definition") != nullptr) {
                    return invalid_argument("only native tool may carry source/definition");
                }
                const json::Value* desc = tv.find("description");
                if (desc != nullptr) {
                    if (!desc->is_string()) return invalid_argument("tool description must be a string");
                    t.description = desc->as_string();
                    t.has_description = true;
                }
                const json::Value* format = tv.find("format");
                const json::Value* input_schema = tv.find("input_schema");
                if (t.type == "custom") {
                    if (input_schema != nullptr) return invalid_argument("custom tool must not carry input_schema");
                    if (format != nullptr) t.format = *format;
                } else {
                    if (format != nullptr) return invalid_argument("function tool must not carry format");
                    OXA_ASSIGN_OR_RETURN(auto is_val, require_object(tv, "input_schema", "tool"));
                    t.input_schema = is_val.get();
                }
            }
            const json::Value* t_nat = tv.find("native");
            if (t_nat != nullptr) {
                if (!t_nat->is_array()) return invalid_argument("tool native must be an array");
                for (const auto& item : t_nat->as_array()) {
                    OXA_ASSIGN_OR_RETURN(auto ann, load_native_annotation(item));
                    t.native.push_back(std::move(ann));
                }
            }
            out.push_back(std::move(t));
        }
        if (!out.empty()) r.tools = std::move(out);
    }

    const json::Value* tc = v.find("tool_choice");
    if (tc != nullptr) {
        if (!tc->is_object()) return invalid_argument("request tool_choice must be an object");
        ToolChoice choice;
        OXA_ASSIGN_OR_RETURN(choice.mode, require_string(*tc, "mode", "tool_choice"));
        const json::Value* name = tc->find("name");
        if (name != nullptr) {
            if (!name->is_string()) return invalid_argument("tool_choice name must be a string");
            choice.name = name->as_string();
        }
        if (const auto* tt = tc->find("tool_type"); tt != nullptr) {
            if (!tt->is_string() || (tt->as_string() != "function" && tt->as_string() != "custom" && tt->as_string() != "native" && tt->as_string() != TOOL_TYPE_APPLY_PATCH))
                return invalid_argument("tool_choice tool_type must be function, custom, native, or apply_patch");
            choice.tool_type = tt->as_string();
        }
        if (choice.mode != TOOL_CHOICE_TOOL && choice.tool_type.has_value()) return invalid_argument("tool_choice tool_type only applies to mode tool");
        if (choice.mode == TOOL_CHOICE_TOOL) {
            if (choice.tool_type == TOOL_TYPE_APPLY_PATCH) {
                if (choice.name.has_value()) return invalid_argument("apply_patch tool_choice must not carry name");
            } else if (!choice.name.has_value()) {
                return invalid_argument("tool_choice mode tool requires name");
            }
        }
        r.tool_choice = std::move(choice);
    }

    const json::Value* params = v.find("params");
    if (params != nullptr) {
        if (!params->is_object()) return invalid_argument("request params must be an object");
        Params p;
        const json::Value* f;
        if ((f = params->find("temperature")) != nullptr) {
            if (!f->is_number()) return invalid_argument("params temperature must be a number");
            p.temperature = f->is_int() ? static_cast<double>(f->as_int()) : f->as_double();
        }
        if ((f = params->find("top_p")) != nullptr) {
            if (!f->is_number()) return invalid_argument("params top_p must be a number");
            p.top_p = f->is_int() ? static_cast<double>(f->as_int()) : f->as_double();
        }
        if ((f = params->find("max_tokens")) != nullptr) {
            if (!f->is_int()) return invalid_argument("params max_tokens must be an integer");
            p.max_tokens = f->as_int();
        }
        if ((f = params->find("stop_sequences")) != nullptr) {
            if (!f->is_array()) return invalid_argument("params stop_sequences must be an array");
            std::vector<std::string> stops;
            for (const auto& s : f->as_array()) {
                if (!s.is_string()) return invalid_argument("stop_sequence item must be a string");
                stops.push_back(s.as_string());
            }
            p.stop_sequences = std::move(stops);
        }
        if ((f = params->find("reasoning_effort")) != nullptr) {
            if (!f->is_string()) return invalid_argument("params reasoning_effort must be a string");
            const std::string effort = f->as_string();
            if (effort != REASONING_EFFORT_MINIMAL && effort != REASONING_EFFORT_LOW &&
                effort != REASONING_EFFORT_MEDIUM && effort != REASONING_EFFORT_HIGH &&
                effort != REASONING_EFFORT_XHIGH && effort != REASONING_EFFORT_MAX) {
                return invalid_argument("invalid params reasoning_effort: " + effort);
            }
            p.reasoning_effort = effort;
        }
        if ((f = params->find("response_format")) != nullptr) {
            if (!f->is_object()) return invalid_argument("params response_format must be an object");
            const json::Value* type = f->find("type");
            if (type == nullptr || !type->is_string()) {
                return invalid_argument("params response_format requires string type");
            }
            ResponseFormat response_format;
            response_format.type = type->as_string();
            if (response_format.type == "json_schema") {
                const json::Value* name = f->find("name");
                if (name != nullptr) {
                    if (!name->is_string() || name->as_string().empty()) {
                        return invalid_argument("params response_format json_schema name must be non-empty when present");
                    }
                    response_format.name = name->as_string();
                }
                const json::Value* description = f->find("description");
                if (description != nullptr) {
                    if (!description->is_string()) {
                        return invalid_argument("params response_format description must be a string");
                    }
                    response_format.description = description->as_string();
                }
                const json::Value* schema = f->find("schema");
                if (schema == nullptr || !schema->is_object()) {
                    return invalid_argument("params response_format json_schema requires schema object");
                }
                response_format.schema = *schema;
                const json::Value* strict = f->find("strict");
                if (strict != nullptr) {
                    if (!strict->is_bool()) return invalid_argument("params response_format strict must be a boolean");
                    response_format.strict = strict->as_bool();
                }
            } else if (response_format.type != "text" && response_format.type != "json_object") {
                return invalid_argument("invalid params response_format type: " + response_format.type);
            }
            p.response_format = std::move(response_format);
        }
        r.params = std::move(p);
    }

    const json::Value* md = v.find("metadata");
    if (md != nullptr) {
        if (!md->is_object()) return invalid_argument("request metadata must be an object");
        std::map<std::string, std::string> out;
        for (const auto& [k, val] : md->as_object()) {
            if (!val.is_string()) return invalid_argument("metadata value must be a string");
            out.emplace(k, val.as_string());
        }
        if (!out.empty()) r.metadata = std::move(out);
    }
    return r;
}

// ---- response ------------------------------------------------------------------

namespace {

StatusOr<Usage> load_usage(const json::Value& v) {
    OXA_ASSIGN_OR_RETURN(auto f, require_object(v, "usage", "usage"));
    OXA_ASSIGN_OR_RETURN(std::int64_t in_tokens, require_int(f.get(), "input_tokens", "usage"));
    OXA_ASSIGN_OR_RETURN(std::int64_t out_tokens, require_int(f.get(), "output_tokens", "usage"));
    if (in_tokens < 0 || out_tokens < 0) {
        return invalid_argument("usage tokens must be non-negative");
    }
    Usage usage{in_tokens, out_tokens};
    for (const auto& [key, target] : {
             std::pair<std::string_view, std::optional<std::int64_t>*>(
                 "cache_read_input_tokens", &usage.cache_read_input_tokens),
             {"cache_creation_input_tokens", &usage.cache_creation_input_tokens},
         }) {
        if (has(f.get(), key)) {
            OXA_ASSIGN_OR_RETURN(std::int64_t value, require_int(f.get(), key, "usage"));
            if (value < 0) return invalid_argument("usage tokens must be non-negative");
            *target = value;
        }
    }
    if (has(f.get(), "input_tokens_details")) {
        OXA_ASSIGN_OR_RETURN(auto detail, require_object(f.get(), "input_tokens_details", "usage"));
        OXA_ASSIGN_OR_RETURN(std::int64_t cached, require_int(detail.get(), "cached_tokens", "input_tokens_details"));
        if (cached < 0) return invalid_argument("usage tokens must be non-negative");
        usage.input_tokens_details = InputTokensDetails{cached};
    }
    if (has(f.get(), "output_tokens_details")) {
        OXA_ASSIGN_OR_RETURN(auto detail, require_object(f.get(), "output_tokens_details", "usage"));
        OXA_ASSIGN_OR_RETURN(std::int64_t reasoning, require_int(detail.get(), "reasoning_tokens", "output_tokens_details"));
        if (reasoning < 0) return invalid_argument("usage tokens must be non-negative");
        usage.output_tokens_details = OutputTokensDetails{reasoning};
    }
    return usage;
}

json::Value dump_usage(const Usage& u) {
    json::Value out = json::Value::object();
    out.set("input_tokens", json::Value::integer(u.input_tokens));
    out.set("output_tokens", json::Value::integer(u.output_tokens));
    if (u.cache_read_input_tokens.has_value()) {
        out.set("cache_read_input_tokens", json::Value::integer(*u.cache_read_input_tokens));
    }
    if (u.cache_creation_input_tokens.has_value()) {
        out.set("cache_creation_input_tokens", json::Value::integer(*u.cache_creation_input_tokens));
    }
    if (u.input_tokens_details.has_value()) {
        json::Value details = json::Value::object();
        details.set("cached_tokens", json::Value::integer(u.input_tokens_details->cached_tokens));
        out.set("input_tokens_details", std::move(details));
    }
    if (u.output_tokens_details.has_value()) {
        json::Value details = json::Value::object();
        details.set("reasoning_tokens", json::Value::integer(u.output_tokens_details->reasoning_tokens));
        out.set("output_tokens_details", std::move(details));
    }
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
    OXA_ASSIGN_OR_RETURN(r.id, require_string(v, "id", "response"));
    OXA_ASSIGN_OR_RETURN(r.model, require_string(v, "model", "response"));
    OXA_ASSIGN_OR_RETURN(auto content_val, require_array(v, "content", "response"));
    for (const auto& cb : content_val.get().as_array()) {
        OXA_ASSIGN_OR_RETURN(Block b, load_block(cb));
        r.content.push_back(std::move(b));
    }
    OXA_ASSIGN_OR_RETURN(r.stop_reason, require_string(v, "stop_reason", "response"));
    const json::Value* seq = v.find("stop_sequence");
    if (seq != nullptr) {
        if (!seq->is_string()) return invalid_argument("response stop_sequence must be a string");
        r.stop_sequence = seq->as_string();
    }
    if (r.stop_sequence.has_value() && r.stop_reason != STOP_STOP_SEQUENCE) {
        return invalid_argument("stop_sequence only permitted when stop_reason is stop_sequence");
    }
    OXA_ASSIGN_OR_RETURN(r.usage, load_usage(v));
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
        } else if (const auto* ij = std::get_if<InputJsonDelta>(&cbd->delta)) {
            d.set("type", json::Value::string(std::string(DELTA_TYPE_INPUT_JSON_DELTA)));
            d.set("partial_json", json::Value::string(ij->partial_json));
        } else if (const auto* thinking = std::get_if<ThinkingDelta>(&cbd->delta)) {
            d.set("type", json::Value::string(std::string(DELTA_TYPE_THINKING_DELTA)));
            d.set("text", json::Value::string(thinking->text));
        } else if (const auto* custom = std::get_if<CustomToolInputDelta>(&cbd->delta)) {
            d.set("type", json::Value::string(std::string(DELTA_TYPE_CUSTOM_TOOL_INPUT_DELTA)));
            d.set("text", json::Value::string(custom->text));
        } else if (const auto* cite = std::get_if<CitationDelta>(&cbd->delta)) {
            d.set("type", json::Value::string(std::string(DELTA_TYPE_CITATION_DELTA)));
            json::Value cw = json::Value::object();
            cw.set("source", json::Value::string(cite->citation.source));
            cw.set("value", json::Value::string(cite->citation.value));
            d.set("citation", std::move(cw));
        } else {
            const auto& signature = std::get<SignatureDelta>(cbd->delta);
            d.set("type", json::Value::string(std::string(DELTA_TYPE_SIGNATURE_DELTA)));
            d.set("signature", json::Value::string(signature.signature));
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
        OXA_ASSIGN_OR_RETURN(std::string id, require_string(v, "id", "message_start"));
        OXA_ASSIGN_OR_RETURN(std::string model, require_string(v, "model", "message_start"));
        return Event(MessageStart{std::move(id), std::move(model)});
    }
    if (kind == EVENT_TYPE_CONTENT_BLOCK_START) {
        OXA_ASSIGN_OR_RETURN(std::int64_t idx, require_int(v, "index", "content_block_start"));
        OXA_ASSIGN_OR_RETURN(auto block_val, require_object(v, "block", "content_block_start"));
        OXA_ASSIGN_OR_RETURN(Block b, load_block(block_val.get()));
        return Event(ContentBlockStart{idx, std::move(b)});
    }
    if (kind == EVENT_TYPE_CONTENT_BLOCK_DELTA) {
        OXA_ASSIGN_OR_RETURN(std::int64_t idx, require_int(v, "index", "content_block_delta"));
        OXA_ASSIGN_OR_RETURN(auto d_val, require_object(v, "delta", "content_block_delta"));
        const json::Value& d = d_val.get();
        OXA_ASSIGN_OR_RETURN(std::string dkind, kind_of(d));
        Delta delta;
        if (dkind == DELTA_TYPE_TEXT_DELTA) {
            OXA_ASSIGN_OR_RETURN(std::string t, require_string(d, "text", "text_delta"));
            delta = TextDelta{std::move(t)};
        } else if (dkind == DELTA_TYPE_INPUT_JSON_DELTA) {
            OXA_ASSIGN_OR_RETURN(std::string pj, require_string(d, "partial_json", "input_json_delta"));
            delta = InputJsonDelta{std::move(pj)};
        } else if (dkind == DELTA_TYPE_THINKING_DELTA) {
            OXA_ASSIGN_OR_RETURN(std::string text, require_string(d, "text", "thinking_delta"));
            delta = ThinkingDelta{std::move(text)};
        } else if (dkind == DELTA_TYPE_SIGNATURE_DELTA) {
            OXA_ASSIGN_OR_RETURN(std::string signature, require_string(d, "signature", "signature_delta"));
            delta = SignatureDelta{std::move(signature)};
        } else if (dkind == DELTA_TYPE_CUSTOM_TOOL_INPUT_DELTA) {
            OXA_ASSIGN_OR_RETURN(std::string text, require_string(d, "text", "custom_tool_input_delta"));
            delta = CustomToolInputDelta{std::move(text)};
        } else if (dkind == DELTA_TYPE_CITATION_DELTA) {
            OXA_ASSIGN_OR_RETURN(auto cite_val, require_object(d, "citation", "citation_delta"));
            const json::Value& c = cite_val.get();
            OXA_ASSIGN_OR_RETURN(std::string source, require_string(c, "source", "citation"));
            if (source != "anthropic") {
                return invalid_argument("ir: unknown citation source \"" + source + "\"");
            }
            OXA_ASSIGN_OR_RETURN(std::string value, require_string(c, "value", "citation"));
            OXA_RETURN_IF_ERROR(validate_citation_value(value));
            delta = CitationDelta{TextCitation{std::move(source), std::move(value)}};
        } else {
            return invalid_argument("unknown delta discriminant: " + dkind);
        }
        return Event(ContentBlockDelta{idx, std::move(delta)});
    }
    if (kind == EVENT_TYPE_CONTENT_BLOCK_STOP) {
        OXA_ASSIGN_OR_RETURN(std::int64_t idx, require_int(v, "index", "content_block_stop"));
        return Event(ContentBlockStop{idx});
    }
    if (kind == EVENT_TYPE_MESSAGE_DELTA) {
        MessageDelta md;
        OXA_ASSIGN_OR_RETURN(md.stop_reason, require_string(v, "stop_reason", "message_delta"));
        OXA_ASSIGN_OR_RETURN(md.usage, load_usage(v));
        const json::Value* seq = v.find("stop_sequence");
        if (seq != nullptr) {
            if (!seq->is_string()) return invalid_argument("message_delta stop_sequence must be a string");
            md.stop_sequence = seq->as_string();
        }
        if (md.stop_sequence.has_value() && md.stop_reason != STOP_STOP_SEQUENCE) {
            return invalid_argument("stop_sequence only permitted when stop_reason is stop_sequence");
        }
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

struct OpenTool { std::string input; std::string fragments; std::size_t fragment_count = 0; bool custom = false; };

Status validate_tool_input(std::size_t i, const OpenTool& tool, bool allow_synthesized) {
    if (tool.fragment_count == 0) {
        if (tool.input.empty() || allow_synthesized) return ok_status();
        return invalid_argument("event " + std::to_string(i) +
                                (tool.custom ? ": custom tool input without custom_tool_input_delta fragments"
                                             : ": tool input without input_json_delta fragments") +
                                (allow_synthesized ? "" : "; encoder shorthand may synthesize them"));
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
    bool open_is_custom_tool = false;
    bool open_is_thinking = false;
    bool open_is_atomic = false;
    bool signature_seen = false;
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
                open_is_custom_tool = false;
                open_is_thinking = false;
                open_is_atomic = false;
            } else if (std::holds_alternative<ThinkingBlock>(cbs->block)) {
                open_is_tool = false;
                open_is_custom_tool = false;
                open_is_thinking = true;
                open_is_atomic = false;
                signature_seen = false;
            } else if (std::holds_alternative<RedactedThinkingBlock>(cbs->block)) {
                open_is_tool = false;
                open_is_custom_tool = false;
                open_is_thinking = false;
                open_is_atomic = true;
            } else if (const auto* tu = std::get_if<ToolUseBlock>(&cbs->block)) {
                open_is_tool = true; open_is_custom_tool = false; open_is_thinking = false; open_is_atomic = false;
                tool = OpenTool{tu->input, "", 0, false};
            } else if (const auto* custom = std::get_if<CustomToolUseBlock>(&cbs->block)) {
                open_is_tool = true; open_is_custom_tool = true; open_is_thinking = false; open_is_atomic = false;
                tool = OpenTool{custom->input, "", 0, true};
            } else if (const auto* ap = std::get_if<ApplyPatchToolUseBlock>(&cbs->block)) {
                if (ap->id.empty()) return invalid_argument("apply_patch_tool_use requires nonempty id");
                open_is_tool = false; open_is_custom_tool = false; open_is_thinking = false; open_is_atomic = true;
            } else if (const auto* apr = std::get_if<ApplyPatchToolResultBlock>(&cbs->block)) {
                if (apr->tool_use_id.empty()) return invalid_argument("apply_patch_tool_result requires nonempty tool_use_id");
                open_is_tool = false; open_is_custom_tool = false; open_is_thinking = false; open_is_atomic = true;
            } else {
                return invalid_argument(
                    "event " + std::to_string(i) +
                    ": content_block_start carries an unsupported block; streams carry "
                    "text, thinking, and tool_use only");
            }
        } else if (const auto* cbd = std::get_if<ContentBlockDelta>(&e)) {
            if (!open) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": content_block_delta without an open block");
            }
            if (open_is_atomic) {
                return invalid_argument("event " + std::to_string(i) + ": content_block_delta on atomic block");
            }
            if (cbd->index != open_index) {
                return invalid_argument("event " + std::to_string(i) +
                                        ": content_block_delta index " + std::to_string(cbd->index) +
                                        " does not match the open block " +
                                        std::to_string(open_index));
            }
            if (open_is_tool) {
                if (!open_is_custom_tool) {
                    if (const auto* ij = std::get_if<InputJsonDelta>(&cbd->delta)) { tool.fragments += ij->partial_json; ++tool.fragment_count; }
                    else return invalid_argument("event " + std::to_string(i) + ": wrong delta type for function tool");
                } else if (const auto* custom = std::get_if<CustomToolInputDelta>(&cbd->delta)) {
                    tool.fragments += custom->text; ++tool.fragment_count;
                } else {
                    return invalid_argument(
                        "event " + std::to_string(i) +
                        ": delta type text_delta does not match the open block kind");
                }
            } else if (open_is_thinking) {
                if (std::holds_alternative<ThinkingDelta>(cbd->delta)) {
                    if (signature_seen) {
                        return invalid_argument("event " + std::to_string(i) +
                                                ": thinking delta after signature_delta");
                    }
                } else if (std::holds_alternative<SignatureDelta>(cbd->delta)) {
                    if (signature_seen) {
                        return invalid_argument("event " + std::to_string(i) +
                                                ": multiple signatures in thinking block");
                    }
                    signature_seen = true;
                } else {
                    return invalid_argument(
                        "event " + std::to_string(i) +
                        ": delta type does not match the open thinking block kind");
                }
            } else {
                if (std::holds_alternative<TextDelta>(cbd->delta) ||
                    std::holds_alternative<CitationDelta>(cbd->delta)) {
                    // ok on text blocks
                } else {
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
