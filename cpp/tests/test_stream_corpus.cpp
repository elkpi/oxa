#include <filesystem>
#include <fstream>
#include <iterator>
#include <string>
#include <string_view>
#include <vector>

#include "oxa/anthropic/messages.hpp"
#include "oxa/ir.hpp"
#include "oxa/json.hpp"
#include "oxa/openai/chatcompletions.hpp"
#include "oxa/openai/responses.hpp"
#include "test_util.hpp"

namespace {

struct CorpusCase {
    std::string id;
    std::string protocol;
    std::string argument;
    std::vector<std::string> fragments;
};

oxa::json::Value object() { return oxa::json::Value::object(); }

oxa::json::Value array(std::initializer_list<oxa::json::Value> values) {
    oxa::json::Array result;
    result.reserve(values.size());
    for (auto value : values) result.push_back(std::move(value));
    return oxa::json::Value::array(std::move(result));
}

void add_base(oxa::json::Value& chunk, std::string_view id, std::string_view model) {
    chunk.set("id", oxa::json::Value::string(std::string(id)));
    chunk.set("object", oxa::json::Value::string("chat.completion.chunk"));
    chunk.set("created", oxa::json::Value::integer(0));
    chunk.set("model", oxa::json::Value::string(std::string(model)));
}

oxa::json::Value chat_chunk(const oxa::json::Value& delta, oxa::json::Value finish) {
    auto chunk = object();
    add_base(chunk, "chatcmpl-corpus", "gpt-4o-mini");
    auto choice = object();
    choice.set("index", oxa::json::Value::integer(0));
    choice.set("delta", delta);
    choice.set("finish_reason", std::move(finish));
    chunk.set("choices", array({std::move(choice)}));
    return chunk;
}

oxa::json::Value chat_tool_call(std::string_view fragment, bool first) {
    auto function = object();
    if (first) function.set("name", oxa::json::Value::string("corpus_tool"));
    function.set("arguments", oxa::json::Value::string(std::string(fragment)));
    auto call = object();
    call.set("index", oxa::json::Value::integer(0));
    if (first) {
        call.set("id", oxa::json::Value::string("call-corpus"));
        call.set("type", oxa::json::Value::string("function"));
    }
    call.set("function", std::move(function));
    return call;
}

void feed_chat(oxa::openai::chatcompletions::StreamDecoder& decoder,
               std::vector<oxa::ir::Event>& events, const oxa::json::Value& chunk) {
    auto result = decoder.feed(chunk);
    CHECK_MSG(result.ok(), result.status().to_string());
    events.insert(events.end(), result->begin(), result->end());
}

std::vector<oxa::ir::Event> decode_chat(const CorpusCase& test_case) {
    oxa::openai::chatcompletions::StreamDecoder decoder;
    std::vector<oxa::ir::Event> events;
    auto role = object();
    role.set("role", oxa::json::Value::string("assistant"));
    feed_chat(decoder, events, chat_chunk(role, oxa::json::Value::null()));
    for (std::size_t index = 0; index < test_case.fragments.size(); ++index) {
        auto delta = object();
        delta.set("tool_calls", array({chat_tool_call(test_case.fragments[index], index == 0)}));
        feed_chat(decoder, events, chat_chunk(delta, oxa::json::Value::null()));
    }
    feed_chat(decoder, events, chat_chunk(object(), oxa::json::Value::string("tool_calls")));
    auto result = decoder.flush();
    CHECK_MSG(result.ok(), result.status().to_string());
    events.insert(events.end(), result->begin(), result->end());
    return events;
}

oxa::json::Value response_envelope(std::string_view id, std::string_view status,
                                   std::string_view model) {
    auto response = object();
    response.set("id", oxa::json::Value::string(std::string(id)));
    response.set("object", oxa::json::Value::string("response"));
    response.set("status", oxa::json::Value::string(std::string(status)));
    response.set("model", oxa::json::Value::string(std::string(model)));
    response.set("output", oxa::json::Value::array());
    return response;
}

void feed_responses(oxa::openai::responses::StreamDecoder& decoder,
                    std::vector<oxa::ir::Event>& events, const oxa::json::Value& event) {
    auto result = decoder.feed(event);
    CHECK_MSG(result.ok(), result.status().to_string());
    events.insert(events.end(), result->begin(), result->end());
}

oxa::json::Value responses_item(std::string_view status, std::string_view arguments) {
    auto item = object();
    item.set("type", oxa::json::Value::string("function_call"));
    item.set("id", oxa::json::Value::string("fc-corpus"));
    item.set("call_id", oxa::json::Value::string("call-corpus"));
    item.set("name", oxa::json::Value::string("corpus_tool"));
    item.set("status", oxa::json::Value::string(std::string(status)));
    item.set("arguments", oxa::json::Value::string(std::string(arguments)));
    return item;
}

std::vector<oxa::ir::Event> decode_responses(const CorpusCase& test_case) {
    oxa::openai::responses::StreamDecoder decoder;
    std::vector<oxa::ir::Event> events;
    auto created = object();
    created.set("type", oxa::json::Value::string("response.created"));
    created.set("response", response_envelope("resp-corpus", "in_progress", "gpt-4o-mini"));
    feed_responses(decoder, events, created);

    auto added = object();
    added.set("type", oxa::json::Value::string("response.output_item.added"));
    added.set("output_index", oxa::json::Value::integer(0));
    added.set("item", responses_item("in_progress", test_case.fragments.front()));
    feed_responses(decoder, events, added);

    for (std::size_t index = 1; index < test_case.fragments.size(); ++index) {
        auto delta = object();
        delta.set("type", oxa::json::Value::string("response.function_call_arguments.delta"));
        delta.set("item_id", oxa::json::Value::string("fc-corpus"));
        delta.set("output_index", oxa::json::Value::integer(0));
        delta.set("delta", oxa::json::Value::string(test_case.fragments[index]));
        feed_responses(decoder, events, delta);
    }

    auto arguments_done = object();
    arguments_done.set("type", oxa::json::Value::string("response.function_call_arguments.done"));
    arguments_done.set("item_id", oxa::json::Value::string("fc-corpus"));
    arguments_done.set("output_index", oxa::json::Value::integer(0));
    arguments_done.set("call_id", oxa::json::Value::string("call-corpus"));
    arguments_done.set("name", oxa::json::Value::string("corpus_tool"));
    arguments_done.set("arguments", oxa::json::Value::string(test_case.argument));
    feed_responses(decoder, events, arguments_done);

    auto done = object();
    done.set("type", oxa::json::Value::string("response.output_item.done"));
    done.set("output_index", oxa::json::Value::integer(0));
    done.set("item", responses_item("completed", test_case.argument));
    feed_responses(decoder, events, done);

    auto completed = object();
    completed.set("type", oxa::json::Value::string("response.completed"));
    auto response = response_envelope("resp-corpus", "completed", "gpt-4o-mini");
    auto usage = object();
    usage.set("input_tokens", oxa::json::Value::integer(1));
    usage.set("output_tokens", oxa::json::Value::integer(1));
    usage.set("total_tokens", oxa::json::Value::integer(2));
    response.set("usage", std::move(usage));
    completed.set("response", std::move(response));
    feed_responses(decoder, events, completed);

    auto result = decoder.flush();
    CHECK_MSG(result.ok(), result.status().to_string());
    events.insert(events.end(), result->begin(), result->end());
    return events;
}

void feed_anthropic(oxa::anthropic::messages::StreamDecoder& decoder,
                    std::vector<oxa::ir::Event>& events, const oxa::json::Value& event) {
    auto result = decoder.feed(event);
    CHECK_MSG(result.ok(), result.status().to_string());
    events.insert(events.end(), result->begin(), result->end());
}

std::vector<oxa::ir::Event> decode_anthropic(const CorpusCase& test_case) {
    oxa::anthropic::messages::StreamDecoder decoder;
    std::vector<oxa::ir::Event> events;
    auto message = object();
    message.set("id", oxa::json::Value::string("msg-corpus"));
    message.set("type", oxa::json::Value::string("message"));
    message.set("role", oxa::json::Value::string("assistant"));
    message.set("model", oxa::json::Value::string("claude-sonnet-4-5"));
    message.set("content", oxa::json::Value::array());
    message.set("stop_reason", oxa::json::Value::null());
    auto start_usage = object();
    start_usage.set("input_tokens", oxa::json::Value::integer(0));
    start_usage.set("output_tokens", oxa::json::Value::integer(0));
    message.set("usage", std::move(start_usage));
    auto start = object();
    start.set("type", oxa::json::Value::string("message_start"));
    start.set("message", std::move(message));
    feed_anthropic(decoder, events, start);

    auto block = object();
    block.set("type", oxa::json::Value::string("tool_use"));
    block.set("id", oxa::json::Value::string("toolu-corpus"));
    block.set("name", oxa::json::Value::string("corpus_tool"));
    block.set("input", oxa::json::Value::object());
    auto block_start = object();
    block_start.set("type", oxa::json::Value::string("content_block_start"));
    block_start.set("index", oxa::json::Value::integer(0));
    block_start.set("content_block", std::move(block));
    feed_anthropic(decoder, events, block_start);

    for (const auto& fragment : test_case.fragments) {
        auto delta = object();
        delta.set("type", oxa::json::Value::string("input_json_delta"));
        delta.set("partial_json", oxa::json::Value::string(fragment));
        auto event = object();
        event.set("type", oxa::json::Value::string("content_block_delta"));
        event.set("index", oxa::json::Value::integer(0));
        event.set("delta", std::move(delta));
        feed_anthropic(decoder, events, event);
    }

    auto block_stop = object();
    block_stop.set("type", oxa::json::Value::string("content_block_stop"));
    block_stop.set("index", oxa::json::Value::integer(0));
    feed_anthropic(decoder, events, block_stop);

    auto message_delta = object();
    message_delta.set("type", oxa::json::Value::string("message_delta"));
    auto delta = object();
    delta.set("stop_reason", oxa::json::Value::string("tool_use"));
    message_delta.set("delta", std::move(delta));
    auto usage = object();
    usage.set("input_tokens", oxa::json::Value::integer(1));
    usage.set("output_tokens", oxa::json::Value::integer(1));
    message_delta.set("usage", std::move(usage));
    feed_anthropic(decoder, events, message_delta);

    auto stop = object();
    stop.set("type", oxa::json::Value::string("message_stop"));
    feed_anthropic(decoder, events, stop);
    auto result = decoder.flush();
    CHECK_MSG(result.ok(), result.status().to_string());
    events.insert(events.end(), result->begin(), result->end());
    return events;
}

std::vector<CorpusCase> load_cases() {
    const auto root = std::filesystem::path(__FILE__).parent_path().parent_path().parent_path();
    const auto path = root / "testdata" / "stream-fragment-corpus.json";
    std::ifstream input(path);
    CHECK_MSG(input.good(), "unable to open " + path.string());
    const std::string text((std::istreambuf_iterator<char>(input)), std::istreambuf_iterator<char>());
    auto parsed = oxa::json::parse(text);
    CHECK_MSG(parsed.ok(), parsed.status().to_string());
    const auto* version = parsed->find("version");
    const auto* cases = parsed->find("cases");
    CHECK(version != nullptr && version->is_int() && version->as_int() == 1);
    CHECK(cases != nullptr && cases->is_array());

    std::vector<CorpusCase> result;
    for (const auto& item : cases->as_array()) {
        const auto* id = item.find("id");
        const auto* protocol = item.find("protocol");
        const auto* argument = item.find("argument");
        const auto* fragments = item.find("fragments");
        CHECK(id != nullptr && id->is_string());
        CHECK(protocol != nullptr && protocol->is_string());
        CHECK(argument != nullptr && argument->is_string());
        CHECK(fragments != nullptr && fragments->is_array());
        CorpusCase test_case{.id = id->as_string(),
                             .protocol = protocol->as_string(),
                             .argument = argument->as_string(),
                             .fragments = {}};
        for (const auto& fragment : fragments->as_array()) {
            CHECK(fragment.is_string());
            test_case.fragments.push_back(fragment.as_string());
        }
        CHECK_MSG(!test_case.fragments.empty(), test_case.id + " has no fragments");
        std::string joined;
        for (const auto& fragment : test_case.fragments) joined += fragment;
        CHECK_MSG(joined == test_case.argument, test_case.id + " fragments do not concatenate");
        result.push_back(std::move(test_case));
    }
    return result;
}

void assert_events(const CorpusCase& test_case, const std::vector<oxa::ir::Event>& events) {
    CHECK_MSG(!events.empty(), test_case.id + " returned no events");
    auto valid = oxa::ir::validate_event_stream(events);
    CHECK_MSG(valid.ok(), valid.to_string());
    std::string input;
    std::vector<std::string> fragments;
    int tool_blocks = 0;
    for (const auto& event : events) {
        if (const auto* start = std::get_if<oxa::ir::ContentBlockStart>(&event)) {
            if (const auto* tool = std::get_if<oxa::ir::ToolUseBlock>(&start->block)) {
                ++tool_blocks;
                input = tool->input;
            }
        } else if (const auto* delta = std::get_if<oxa::ir::ContentBlockDelta>(&event)) {
            if (const auto* input_delta = std::get_if<oxa::ir::InputJsonDelta>(&delta->delta)) {
                fragments.push_back(input_delta->partial_json);
            }
        }
    }
    CHECK_MSG(tool_blocks == 1, test_case.id + " tool block count mismatch");
    CHECK_MSG(input == test_case.argument, test_case.id + " tool input mismatch");
    CHECK_MSG(fragments == test_case.fragments, test_case.id + " fragment order mismatch");
    CHECK(std::holds_alternative<oxa::ir::MessageStart>(events.front()));
    CHECK(std::holds_alternative<oxa::ir::MessageDone>(events.back()));
}

}  // namespace

int main() {
    for (const auto& test_case : load_cases()) {
        std::vector<oxa::ir::Event> events;
        if (test_case.protocol == "chatcompletions") {
            events = decode_chat(test_case);
        } else if (test_case.protocol == "responses") {
            events = decode_responses(test_case);
        } else if (test_case.protocol == "anthropic") {
            events = decode_anthropic(test_case);
        } else {
            CHECK_MSG(false, "unsupported protocol: " + test_case.protocol);
        }
        assert_events(test_case, events);
    }
    std::printf("test_stream_corpus: all cases passed\n");
    return 0;
}
