#include <cassert>
#include <iostream>
#include <string_view>

#include "oxa/anthropic/messages.hpp"
#include "oxa/ir.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/openai/responses.hpp"

int main() {
    // IR constants (spec/01, spec/02)
    assert(oxa::ir::SPEC_VERSION == "0.2.0");
    assert(oxa::ir::LEGACY_SPEC_VERSION == "0.1.0");

    assert(oxa::ir::REASONING_EFFORT_MINIMAL == "minimal");
    assert(oxa::ir::REASONING_EFFORT_LOW == "low");
    assert(oxa::ir::REASONING_EFFORT_MEDIUM == "medium");
    assert(oxa::ir::REASONING_EFFORT_HIGH == "high");

    assert(oxa::ir::ROLE_USER == "user");
    assert(oxa::ir::ROLE_ASSISTANT == "assistant");

    assert(oxa::ir::BLOCK_TYPE_TEXT == "text");
    assert(oxa::ir::BLOCK_TYPE_THINKING == "thinking");
    assert(oxa::ir::BLOCK_TYPE_IMAGE == "image");
    assert(oxa::ir::BLOCK_TYPE_TOOL_USE == "tool_use");
    assert(oxa::ir::BLOCK_TYPE_TOOL_RESULT == "tool_result");

    assert(oxa::ir::TOOL_CHOICE_AUTO == "auto");
    assert(oxa::ir::TOOL_CHOICE_ANY == "any");
    assert(oxa::ir::TOOL_CHOICE_TOOL == "tool");
    assert(oxa::ir::TOOL_CHOICE_NONE == "none");

    assert(oxa::ir::STOP_END_TURN == "end_turn");
    assert(oxa::ir::STOP_MAX_TOKENS == "max_tokens");
    assert(oxa::ir::STOP_STOP_SEQUENCE == "stop_sequence");
    assert(oxa::ir::STOP_TOOL_USE == "tool_use");
    assert(oxa::ir::STOP_REFUSAL == "refusal");
    assert(oxa::ir::STOP_OTHER == "other");

    assert(oxa::ir::EVENT_TYPE_MESSAGE_START == "message_start");
    assert(oxa::ir::EVENT_TYPE_CONTENT_BLOCK_START == "content_block_start");
    assert(oxa::ir::EVENT_TYPE_CONTENT_BLOCK_DELTA == "content_block_delta");
    assert(oxa::ir::EVENT_TYPE_CONTENT_BLOCK_STOP == "content_block_stop");
    assert(oxa::ir::EVENT_TYPE_MESSAGE_DELTA == "message_delta");
    assert(oxa::ir::EVENT_TYPE_MESSAGE_DONE == "message_done");

    assert(oxa::ir::DELTA_TYPE_TEXT_DELTA == "text_delta");
    assert(oxa::ir::DELTA_TYPE_INPUT_JSON_DELTA == "input_json_delta");
    assert(oxa::ir::DELTA_TYPE_THINKING_DELTA == "thinking_delta");
    assert(oxa::ir::DELTA_TYPE_SIGNATURE_DELTA == "signature_delta");

    assert(oxa::ir::LOSS_UNMAPPED_FIELD == "unmapped-field");
    assert(oxa::ir::LOSS_UNMAPPED_VALUE == "unmapped-value");
    assert(oxa::ir::LOSS_UNSUPPORTED_SEMANTIC == "unsupported-semantic");
    assert(oxa::ir::LOSS_DEGRADED == "degraded");

    // Chat Completions wire constants (spec/10)
    assert(oxa::openai::chatcompletions::ROLE_SYSTEM == "system");
    assert(oxa::openai::chatcompletions::ROLE_USER == "user");
    assert(oxa::openai::chatcompletions::ROLE_ASSISTANT == "assistant");
    assert(oxa::openai::chatcompletions::ROLE_TOOL == "tool");
    assert(oxa::openai::chatcompletions::TOOL_TYPE_FUNCTION == "function");
    assert(oxa::openai::chatcompletions::CONTENT_PART_TYPE_TEXT == "text");
    assert(oxa::openai::chatcompletions::CONTENT_PART_TYPE_IMAGE_URL == "image_url");
    assert(oxa::openai::chatcompletions::TOOL_CHOICE_AUTO == "auto");
    assert(oxa::openai::chatcompletions::TOOL_CHOICE_NONE == "none");
    assert(oxa::openai::chatcompletions::TOOL_CHOICE_REQUIRED == "required");
    assert(oxa::openai::chatcompletions::FINISH_REASON_STOP == "stop");
    assert(oxa::openai::chatcompletions::FINISH_REASON_LENGTH == "length");
    assert(oxa::openai::chatcompletions::FINISH_REASON_CONTENT_FILTER == "content_filter");
    assert(oxa::openai::chatcompletions::FINISH_REASON_TOOL_CALLS == "tool_calls");
    assert(oxa::openai::chatcompletions::OBJECT_CHAT_COMPLETION == "chat.completion");
    assert(oxa::openai::chatcompletions::OBJECT_CHAT_COMPLETION_CHUNK == "chat.completion.chunk");

    // Responses wire constants (spec/11)
    assert(oxa::openai::responses::ROLE_SYSTEM == "system");
    assert(oxa::openai::responses::ROLE_USER == "user");
    assert(oxa::openai::responses::ROLE_ASSISTANT == "assistant");
    assert(oxa::openai::responses::ROLE_DEVELOPER == "developer");
    assert(oxa::openai::responses::TOOL_TYPE_FUNCTION == "function");
    assert(oxa::openai::responses::TOOL_CHOICE_AUTO == "auto");
    assert(oxa::openai::responses::TOOL_CHOICE_NONE == "none");
    assert(oxa::openai::responses::TOOL_CHOICE_REQUIRED == "required");
    assert(oxa::openai::responses::ITEM_TYPE_MESSAGE == "message");
    assert(oxa::openai::responses::ITEM_TYPE_FUNCTION_CALL == "function_call");
    assert(oxa::openai::responses::ITEM_TYPE_FUNCTION_CALL_OUTPUT == "function_call_output");
    assert(oxa::openai::responses::PART_TYPE_INPUT_TEXT == "input_text");
    assert(oxa::openai::responses::PART_TYPE_OUTPUT_TEXT == "output_text");
    assert(oxa::openai::responses::PART_TYPE_INPUT_IMAGE == "input_image");
    assert(oxa::openai::responses::STATUS_IN_PROGRESS == "in_progress");
    assert(oxa::openai::responses::STATUS_COMPLETED == "completed");
    assert(oxa::openai::responses::STATUS_INCOMPLETE == "incomplete");
    assert(oxa::openai::responses::STATUS_FAILED == "failed");
    assert(oxa::openai::responses::INCOMPLETE_REASON_MAX_OUTPUT_TOKENS == "max_output_tokens");
    assert(oxa::openai::responses::ERROR_CODE_REFUSAL == "refusal");
    assert(oxa::openai::responses::OBJECT_RESPONSE == "response");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_CREATED == "response.created");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_OUTPUT_ITEM_ADDED == "response.output_item.added");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_OUTPUT_ITEM_DONE == "response.output_item.done");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_CONTENT_PART_ADDED == "response.content_part.added");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_CONTENT_PART_DONE == "response.content_part.done");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DELTA == "response.output_text.delta");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_OUTPUT_TEXT_DONE == "response.output_text.done");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DELTA ==
           "response.function_call_arguments.delta");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_FUNCTION_CALL_ARGS_DONE ==
           "response.function_call_arguments.done");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_COMPLETED == "response.completed");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_INCOMPLETE == "response.incomplete");
    assert(oxa::openai::responses::EVENT_TYPE_RESPONSE_FAILED == "response.failed");

    // Anthropic wire constants (spec/12)
    assert(oxa::anthropic::messages::ROLE_USER == "user");
    assert(oxa::anthropic::messages::ROLE_ASSISTANT == "assistant");
    assert(oxa::anthropic::messages::BLOCK_TYPE_TEXT == "text");
    assert(oxa::anthropic::messages::BLOCK_TYPE_IMAGE == "image");
    assert(oxa::anthropic::messages::BLOCK_TYPE_TOOL_USE == "tool_use");
    assert(oxa::anthropic::messages::BLOCK_TYPE_TOOL_RESULT == "tool_result");
    assert(oxa::anthropic::messages::SOURCE_TYPE_BASE64 == "base64");
    assert(oxa::anthropic::messages::SOURCE_TYPE_URL == "url");
    assert(oxa::anthropic::messages::TOOL_CHOICE_TYPE_AUTO == "auto");
    assert(oxa::anthropic::messages::TOOL_CHOICE_TYPE_ANY == "any");
    assert(oxa::anthropic::messages::TOOL_CHOICE_TYPE_NONE == "none");
    assert(oxa::anthropic::messages::TOOL_CHOICE_TYPE_TOOL == "tool");
    assert(oxa::anthropic::messages::STOP_REASON_END_TURN == "end_turn");
    assert(oxa::anthropic::messages::STOP_REASON_MAX_TOKENS == "max_tokens");
    assert(oxa::anthropic::messages::STOP_REASON_STOP_SEQUENCE == "stop_sequence");
    assert(oxa::anthropic::messages::STOP_REASON_TOOL_USE == "tool_use");
    assert(oxa::anthropic::messages::STOP_REASON_REFUSAL == "refusal");
    assert(oxa::anthropic::messages::EVENT_TYPE_MESSAGE_START == "message_start");
    assert(oxa::anthropic::messages::EVENT_TYPE_CONTENT_BLOCK_START == "content_block_start");
    assert(oxa::anthropic::messages::EVENT_TYPE_CONTENT_BLOCK_DELTA == "content_block_delta");
    assert(oxa::anthropic::messages::EVENT_TYPE_CONTENT_BLOCK_STOP == "content_block_stop");
    assert(oxa::anthropic::messages::EVENT_TYPE_MESSAGE_DELTA == "message_delta");
    assert(oxa::anthropic::messages::EVENT_TYPE_MESSAGE_STOP == "message_stop");
    assert(oxa::anthropic::messages::DELTA_TYPE_TEXT_DELTA == "text_delta");
    assert(oxa::anthropic::messages::DELTA_TYPE_INPUT_JSON_DELTA == "input_json_delta");
    assert(oxa::anthropic::messages::TYPE_MESSAGE == "message");

    std::cout << "All C++ constants verified successfully.\n";
    return 0;
}
