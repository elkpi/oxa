#pragma once
#include "../wire_audit.hpp"
namespace oxa::openai::responses {
inline const detail::AuditProfile& wire_profile() {
    static const detail::AuditProfile profile{
        {{"request", {{"model", ""}, {"input", "input_item[]"}, {"instructions", ""}, {"temperature", ""}, {"top_p", ""}, {"max_output_tokens", ""}, {"tools", "tool[]"}, {"tool_choice", "tool_choice"}, {"metadata", ""}, {"text", "text_params"}, {"reasoning", "reasoning"}, {"parallel_tool_calls", ""}}},
         {"response", {{"id", ""}, {"object", ""}, {"status", ""}, {"model", ""}, {"output", "output_item[]"}, {"usage", "usage"}, {"incomplete_details", "incomplete"}, {"error", ""}}},
         {"input_message", {{"type", ""}, {"id", ""}, {"status", ""}, {"role", ""}, {"content", "input_part[]"}}},
         {"function_call", {{"type", ""}, {"id", ""}, {"status", ""}, {"call_id", ""}, {"name", ""}, {"arguments", ""}}},
         {"function_output", {{"type", ""}, {"id", ""}, {"status", ""}, {"call_id", ""}, {"output", ""}}},
         {"reasoning_item", {{"type", ""}, {"id", ""}, {"status", ""}, {"summary", "summary_part[]"}, {"encrypted_content", ""}}},
         {"plain_part", {{"type", ""}, {"text", ""}, {"annotations", ""}}},
         {"input_image", {{"type", ""}, {"image_url", ""}}},
         {"output_message", {{"type", ""}, {"id", ""}, {"status", ""}, {"role", ""}, {"content", "output_part[]"}}},
         {"function_tool", {{"type", ""}, {"name", ""}, {"description", ""}, {"parameters", ""}, {"strict", ""}}},
         {"named_choice", {{"type", ""}, {"name", ""}}},
         {"text_params", {{"verbosity", ""}, {"format", "format"}}},
         {"plain_format", {{"type", ""}}},
         {"schema_format", {{"type", ""}, {"name", ""}, {"description", ""}, {"schema", ""}, {"strict", ""}}},
         {"reasoning", {{"effort", ""}, {"summary", ""}}},
         {"usage", {{"input_tokens", ""}, {"output_tokens", ""}, {"total_tokens", ""}, {"input_token_details", "input_detail"}, {"output_token_details", "output_detail"}}},
         {"input_detail", {{"cached_tokens", ""}}},
         {"output_detail", {{"reasoning_tokens", ""}}},
         {"incomplete", {{"reason", ""}}},
         {"response_event", {{"type", ""}, {"response", "response"}, {"sequence_number", ""}}},
         {"item_event", {{"type", ""}, {"output_index", ""}, {"item", "output_item"}, {"sequence_number", ""}}},
         {"part_event", {{"type", ""}, {"item_id", ""}, {"output_index", ""}, {"content_index", ""}, {"part", "output_part"}, {"sequence_number", ""}}},
         {"summary_part_event", {{"type", ""}, {"item_id", ""}, {"output_index", ""}, {"content_index", ""}, {"part", "summary_part"}, {"sequence_number", ""}}},
         {"text_delta_event", {{"type", ""}, {"item_id", ""}, {"output_index", ""}, {"content_index", ""}, {"delta", ""}, {"sequence_number", ""}}},
         {"text_done_event", {{"type", ""}, {"item_id", ""}, {"output_index", ""}, {"content_index", ""}, {"text", ""}, {"sequence_number", ""}}},
         {"args_delta_event", {{"type", ""}, {"item_id", ""}, {"output_index", ""}, {"delta", ""}, {"sequence_number", ""}}},
         {"args_done_event", {{"type", ""}, {"item_id", ""}, {"output_index", ""}, {"call_id", ""}, {"name", ""}, {"arguments", ""}, {"sequence_number", ""}}}},
        {{"input_item", {"type", {{"", "input_message"}, {"message", "input_message"}, {"function_call", "function_call"}, {"function_call_output", "function_output"}, {"reasoning", "reasoning_item"}}, "input_message"}},
         {"input_part", {"type", {{"input_text", "plain_part"}, {"output_text", "plain_part"}, {"input_image", "input_image"}}, ""}},
         {"summary_part", {"type", {{"summary_text", "plain_part"}, {"output_text", "plain_part"}}, ""}},
         {"output_part", {"type", {{"output_text", "plain_part"}}, ""}},
         {"output_item", {"type", {{"message", "output_message"}, {"function_call", "function_call"}, {"reasoning", "reasoning_item"}}, ""}},
         {"tool", {"type", {{"function", "function_tool"}}, ""}},
         {"tool_choice", {"type", {{"function", "named_choice"}}, ""}},
         {"format", {"type", {{"text", "plain_format"}, {"json_object", "plain_format"}, {"json_schema", "schema_format"}}, ""}},
         {"event", {"type", {{"response.created", "response_event"}, {"response.completed", "response_event"}, {"response.incomplete", "response_event"}, {"response.failed", "response_event"}, {"response.output_item.added", "item_event"}, {"response.output_item.done", "item_event"}, {"response.content_part.added", "part_event"}, {"response.content_part.done", "part_event"}, {"response.reasoning_summary_part.added", "summary_part_event"}, {"response.reasoning_summary_part.done", "summary_part_event"}, {"response.output_text.delta", "text_delta_event"}, {"response.reasoning_summary_text.delta", "text_delta_event"}, {"response.output_text.done", "text_done_event"}, {"response.reasoning_summary_text.done", "text_done_event"}, {"response.function_call_arguments.delta", "args_delta_event"}, {"response.function_call_arguments.done", "args_done_event"}}, ""}}}
    };
    return profile;
}
}
