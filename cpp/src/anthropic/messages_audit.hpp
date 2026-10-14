#pragma once
#include "../wire_audit.hpp"
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
        {{"block", {"type", {{"text", "text"}, {"thinking", "thinking"}, {"tool_use", "tool_use"}, {"tool_result", "tool_result"}, {"image", "image"}}, ""}},
         {"source", {"type", {{"base64", "base64"}, {"url", "url"}}, ""}},
         {"event", {"type", {{"message_start", "start"}, {"content_block_start", "block_start"}, {"content_block_delta", "block_delta"}, {"content_block_stop", "block_stop"}, {"message_delta", "message_delta"}, {"message_stop", "message_stop"}}, ""}},
         {"stream_block", {"type", {{"text", "text"}, {"thinking", "thinking"}, {"tool_use", "tool_use"}}, ""}},
         {"delta", {"type", {{"text_delta", "text_delta"}, {"input_json_delta", "json_delta"}, {"thinking_delta", "thinking_delta"}, {"signature_delta", "signature_delta"}}, ""}}}
    };
    return profile;
}
}
