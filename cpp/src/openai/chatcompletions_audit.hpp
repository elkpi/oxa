#pragma once
#include "../wire_audit.hpp"
#include "oxa/openai/chatcompletions.hpp"

namespace oxa::openai::chatcompletions {
inline const detail::AuditProfile& wire_profile() {
    static const detail::AuditProfile profile{
        {{"request", {{"model", ""}, {"messages", "message[]"}, {"temperature", ""}, {"top_p", ""}, {"max_tokens", ""}, {"max_completion_tokens", ""}, {"store", ""}, {"stream", ""}, {"stream_options", "stream_options"}, {"stop", ""}, {"reasoning_effort", ""}, {"verbosity", ""}, {"tools", "tool[]"}, {"tool_choice", "tool_choice"}, {"parallel_tool_calls", ""}, {"prompt_cache_key", ""}, {"response_format", "format"}, {"metadata", ""}}},
         {"stream_options", {{"include_usage", ""}, {"include_obfuscation", ""}}},
         {"response", {{"id", ""}, {"object", ""}, {"created", ""}, {"model", ""}, {"choices", "choice[]"}, {"usage", "usage"}}},
         {"event", {{"id", ""}, {"object", ""}, {"created", ""}, {"model", ""}, {"choices", "choice_delta[]"}, {"usage", "usage"}}},
         {"message", {{"role", ""}, {"content", "part[]"}, {"refusal", ""}, {"reasoning_content", ""}, {"tool_calls", "tool_call[]"}, {"tool_call_id", ""}}},
         {"text", {{"type", ""}, {"text", ""}}},
         {"image", {{"type", ""}, {"image_url", "image_url"}}},
         {"image_url", {{"url", ""}}},
         {"file_part", {{"type", ""}, {"file", "file_def"}}},
         {"file_def", {{"file_id", ""}, {"file_data", ""}, {"filename", ""}}},
         {"function_tool", {{"type", ""}, {"function", "function_definition"}}},
         {"custom_tool", {{"type", ""}, {"custom", "custom_decl"}}},
         {"custom_decl", {{"name", ""}, {"description", ""}, {"format", "custom_format"}}},
         {"custom_format", {{"type", ""}, {"syntax", ""}, {"definition", ""}}},
         {"function", {{"name", ""}, {"description", ""}, {"parameters", ""}, {"arguments", ""}}},
         {"function_definition", {{"name", ""}, {"description", ""}, {"parameters", ""}, {"strict", ""}, {"arguments", ""}}},
         {"tool_call", {{"id", ""}, {"type", ""}, {"function", "function"}, {"custom", "custom_args"}}},
         {"function_call", {{"id", ""}, {"type", ""}, {"function", "function"}}},
         {"custom_call", {{"id", ""}, {"type", ""}, {"custom", "custom_args"}}},
         {"custom_args", {{"name", ""}, {"input", ""}}},
         {"named_choice", {{"type", ""}, {"function", "named_function"}}},
         {"named_custom_choice", {{"type", ""}, {"custom", "named_function"}}},
         {"named_function", {{"name", ""}}},
         {"plain_format", {{"type", ""}}},
         {"schema_format", {{"type", ""}, {"json_schema", "json_schema"}}},
         {"json_schema", {{"name", ""}, {"description", ""}, {"schema", ""}, {"strict", ""}}},
         {"choice", {{"index", ""}, {"message", "message"}, {"finish_reason", ""}}},
         {"choice_delta", {{"index", ""}, {"delta", "delta"}, {"finish_reason", ""}}},
         {"delta", {{"role", ""}, {"content", ""}, {"reasoning_content", ""}, {"tool_calls", "tool_delta[]"}}},
         {"tool_delta", {{"index", ""}, {"id", ""}, {"type", ""}, {"function", "function_delta"}, {"custom", "custom_args"}}},
         {"function_delta", {{"name", ""}, {"arguments", ""}}},
         {"usage", {{"prompt_tokens", ""}, {"completion_tokens", ""}, {"total_tokens", ""}, {"prompt_tokens_details", "input_detail"}, {"completion_tokens_details", "output_detail"}}},
         {"input_detail", {{"cached_tokens", ""}}},
         {"output_detail", {{"reasoning_tokens", ""}}}},
        {{"part", {"type", {{CONTENT_PART_TYPE_TEXT, "text"},
                            {CONTENT_PART_TYPE_IMAGE_URL, "image"},
                            {CONTENT_PART_TYPE_FILE, "file_part"}}, ""}},
         {"tool", {"type", {{TOOL_TYPE_FUNCTION, "function_tool"},
                            {"custom", "custom_tool"}}, ""}},
         {"tool_call", {"type", {{TOOL_TYPE_FUNCTION, "function_call"},
                                 {"custom", "custom_call"}}, "function_call"}},
         {"tool_choice", {"type", {{TOOL_TYPE_FUNCTION, "named_choice"},
                                   {"custom", "named_custom_choice"}}, ""}},
         {"format", {"type", {{"text", "plain_format"},
                              {"json_object", "plain_format"},
                              {"json_schema", "schema_format"}}, ""}}}
    };
    return profile;
}
}
