#pragma once
#include "../wire_audit.hpp"
#include "oxa/anthropic/messages.hpp"

namespace oxa::anthropic::messages {
inline const detail::AuditProfile& wire_profile() {
    static const detail::AuditProfile profile{
        {{"request", {{"model", ""}, {"system", "system_block[]"}, {"messages", "message[]"}, {"max_tokens", ""}, {"temperature", ""}, {"top_p", ""}, {"stop_sequences", ""}, {"tools", "tool[]"}, {"tool_choice", "tool_choice"}, {"thinking", "thinking_config"}}},
         {"response", {{"id", ""}, {"type", ""}, {"role", ""}, {"model", ""}, {"content", "block[]"}, {"stop_reason", ""}, {"stop_sequence", ""}, {"usage", "usage"}}},
         {"message", {{"role", ""}, {"content", "block[]"}}},
         {"system_block", {{"type", ""}, {"text", ""}}},
         {"tool", {{"name", ""}, {"description", ""}, {"input_schema", ""}}},
         {"tool_choice", {{"type", ""}, {"name", ""}}},
         {"thinking_config", {{"type", ""}, {"budget_tokens", ""}}},
         {"text", {{"type", ""}, {"text", ""}}},
         {"thinking", {{"type", ""}, {"thinking", ""}, {"signature", ""}}},
         {"tool_use", {{"type", ""}, {"id", ""}, {"name", ""}, {"input", ""}}},
         {"tool_result", {{"type", ""}, {"tool_use_id", ""}, {"content", "block[]"}, {"is_error", ""}}},
         {"image", {{"type", ""}, {"source", "source"}}},
         {"base64", {{"type", ""}, {"media_type", ""}, {"data", ""}}},
         {"url", {{"type", ""}, {"url", ""}}},
         {"usage", {{"input_tokens", ""}, {"output_tokens", ""}, {"cache_creation_input_tokens", ""}, {"cache_read_input_tokens", ""}}},
         {"start", {{"type", ""}, {"message", "response"}}},
         {"block_start", {{"type", ""}, {"index", ""}, {"content_block", "stream_block"}}},
         {"block_delta", {{"type", ""}, {"index", ""}, {"delta", "delta"}}},
         {"block_stop", {{"type", ""}, {"index", ""}}},
         {"message_delta", {{"type", ""}, {"delta", "stop_delta"}, {"usage", "usage"}}},
         {"message_stop", {{"type", ""}}},
         {"stop_delta", {{"stop_reason", ""}, {"stop_sequence", ""}}},
         {"text_delta", {{"type", ""}, {"text", ""}}},
         {"json_delta", {{"type", ""}, {"partial_json", ""}}},
         {"thinking_delta", {{"type", ""}, {"thinking", ""}}},
         {"signature_delta", {{"type", ""}, {"signature", ""}}}},
        {{"block", {"type", {{BLOCK_TYPE_TEXT, "text"},
                             {BLOCK_TYPE_THINKING, "thinking"},
                             {BLOCK_TYPE_TOOL_USE, "tool_use"},
                             {BLOCK_TYPE_TOOL_RESULT, "tool_result"},
                             {BLOCK_TYPE_IMAGE, "image"}}, ""}},
         {"source", {"type", {{SOURCE_TYPE_BASE64, "base64"},
                              {SOURCE_TYPE_URL, "url"}}, ""}},
         {"event", {"type", {{EVENT_TYPE_MESSAGE_START, "start"},
                             {EVENT_TYPE_CONTENT_BLOCK_START, "block_start"},
                             {EVENT_TYPE_CONTENT_BLOCK_DELTA, "block_delta"},
                             {EVENT_TYPE_CONTENT_BLOCK_STOP, "block_stop"},
                             {EVENT_TYPE_MESSAGE_DELTA, "message_delta"},
                             {EVENT_TYPE_MESSAGE_STOP, "message_stop"}}, ""}},
         {"stream_block", {"type", {{BLOCK_TYPE_TEXT, "text"},
                                    {BLOCK_TYPE_THINKING, "thinking"},
                                    {BLOCK_TYPE_TOOL_USE, "tool_use"}}, ""}},
         {"delta", {"type", {{DELTA_TYPE_TEXT_DELTA, "text_delta"},
                             {DELTA_TYPE_INPUT_JSON_DELTA, "json_delta"},
                             {DELTA_TYPE_THINKING_DELTA, "thinking_delta"},
                             {DELTA_TYPE_SIGNATURE_DELTA, "signature_delta"}}, ""}}}
    };
    return profile;
}
}
