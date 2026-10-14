#pragma once
#include "../wire_audit.hpp"
namespace oxa::openai::chatcompletions {
inline const detail::AuditProfile& wire_profile() {
    static const detail::AuditProfile profile{
        {{"request", {{"model", ""}, {"messages", "message[]"}, {"temperature", ""}, {"top_p", ""}, {"max_tokens", ""}, {"stop", ""}, {"reasoning_effort", ""}, {"tools", "tool[]"}, {"tool_choice", "tool_choice"}, {"response_format", "format"}}},
         {"response", {{"id", ""}, {"object", ""}, {"created", ""}, {"model", ""}, {"choices", "choice[]"}, {"usage", "usage"}}},
         {"event", {{"id", ""}, {"object", ""}, {"created", ""}, {"model", ""}, {"choices", "choice_delta[]"}, {"usage", "usage"}}},
         {"message", {{"role", ""}, {"content", "part[]"}, {"reasoning_content", ""}, {"tool_calls", "tool_call[]"}, {"tool_call_id", ""}}},
         {"text", {{"type", ""}, {"text", ""}}},
         {"image", {{"type", ""}, {"image_url", "image_url"}}},
         {"image_url", {{"url", ""}}},
         {"function_tool", {{"type", ""}, {"function", "function"}}},
         {"function", {{"name", ""}, {"description", ""}, {"parameters", ""}, {"arguments", ""}}},
         {"function_call", {{"id", ""}, {"type", ""}, {"function", "function"}}},
         {"named_choice", {{"type", ""}, {"function", "named_function"}}},
         {"named_function", {{"name", ""}}},
         {"plain_format", {{"type", ""}}},
         {"schema_format", {{"type", ""}, {"json_schema", "json_schema"}}},
         {"json_schema", {{"name", ""}, {"description", ""}, {"schema", ""}, {"strict", ""}}},
         {"choice", {{"index", ""}, {"message", "message"}, {"finish_reason", ""}}},
         {"choice_delta", {{"index", ""}, {"delta", "delta"}, {"finish_reason", ""}}},
         {"delta", {{"role", ""}, {"content", ""}, {"reasoning_content", ""}, {"tool_calls", "tool_delta[]"}}},
         {"tool_delta", {{"index", ""}, {"id", ""}, {"type", ""}, {"function", "function_delta"}}},
         {"function_delta", {{"name", ""}, {"arguments", ""}}},
         {"usage", {{"prompt_tokens", ""}, {"completion_tokens", ""}, {"total_tokens", ""}, {"prompt_tokens_details", "input_detail"}, {"completion_tokens_details", "output_detail"}}},
         {"input_detail", {{"cached_tokens", ""}}},
         {"output_detail", {{"reasoning_tokens", ""}}}},
        {{"part", {"type", {{"text", "text"}, {"image_url", "image"}}, ""}},
         {"tool", {"type", {{"function", "function_tool"}}, ""}},
         {"tool_call", {"type", {{"function", "function_call"}}, "function_call"}},
         {"tool_choice", {"type", {{"function", "named_choice"}}, ""}},
         {"format", {"type", {{"text", "plain_format"}, {"json_object", "plain_format"}, {"json_schema", "schema_format"}}, ""}}}
    };
    return profile;
}
}
