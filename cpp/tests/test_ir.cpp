// Codec alignment tests against the normative spec/01 §8 examples, plus the
// event-stream invariant checker tests (INV-1, INV-5, INV-6).

#include "oxa/ir.hpp"
#include "test_util.hpp"

using namespace oxa::ir;
using oxa::json::Value;

namespace {

const char* kSpecRequest = R"({
  "specVersion": "0.1.0",
  "model": "claude-sonnet-4-5",
  "system": [{ "type": "text", "text": "You are a concise assistant." }],
  "messages": [
    { "role": "user", "content": [
      { "type": "text", "text": "What is the weather in Paris?" }
    ]},
    { "role": "assistant", "content": [
      { "type": "text", "text": "Let me check." },
      { "type": "tool_use", "id": "toolu_01", "name": "get_weather",
        "input": "{\"city\":\"Paris\"}" }
    ]},
    { "role": "user", "content": [
      { "type": "tool_result", "tool_use_id": "toolu_01", "content": [
        { "type": "text", "text": "18 C, clear" }
      ] }
    ]}
  ],
  "tools": [
    { "name": "get_weather", "description": "Current weather for a city",
      "input_schema": { "type": "object",
        "properties": { "city": { "type": "string" } },
        "required": ["city"] } }
  ],
  "tool_choice": { "mode": "auto" },
  "params": { "temperature": 0.7, "max_tokens": 1024 }
})";

const char* kSpecResponse = R"({
  "specVersion": "0.1.0",
  "id": "msg_017Y2hvcv",
  "model": "claude-sonnet-4-5",
  "content": [
    { "type": "text", "text": "It is 18 C and clear in Paris." }
  ],
  "stop_reason": "end_turn",
  "usage": { "input_tokens": 120, "output_tokens": 12 }
})";

const char* kSpecEventStream = R"({
  "specVersion": "0.1.0",
  "events": [
    { "type": "message_start", "id": "msg_017Y2hvcv", "model": "claude-sonnet-4-5" },
    { "type": "content_block_start", "index": 0,
      "block": { "type": "text", "text": "" } },
    { "type": "content_block_delta", "index": 0,
      "delta": { "type": "text_delta", "text": "It is 18 C" } },
    { "type": "content_block_delta", "index": 0,
      "delta": { "type": "text_delta", "text": " and clear in Paris." } },
    { "type": "content_block_stop", "index": 0 },
    { "type": "message_delta", "stop_reason": "end_turn",
      "usage": { "input_tokens": 120, "output_tokens": 12 } },
    { "type": "message_done" }
  ]
})";

void codec_tests() {
    auto req_parsed = oxa::json::parse(kSpecRequest);
    CHECK(req_parsed.ok());
    auto req_res = load_request(*req_parsed);
    CHECK(req_res.ok());
    CHECK(oxa::json::structurally_equal(*req_parsed, dump_request(*req_res)));

    auto resp_parsed = oxa::json::parse(kSpecResponse);
    CHECK(resp_parsed.ok());
    auto resp_res = load_response(*resp_parsed);
    CHECK(resp_res.ok());
    CHECK(oxa::json::structurally_equal(*resp_parsed, dump_response(*resp_res)));

    auto events_parsed = oxa::json::parse(kSpecEventStream);
    CHECK(events_parsed.ok());
    auto events_res = load_event_stream(*events_parsed);
    CHECK(events_res.ok());
    CHECK(events_res->size() == 7);
    CHECK(std::holds_alternative<MessageStart>((*events_res)[0]));
    CHECK(oxa::json::structurally_equal(*events_parsed, dump_event_stream(*events_res)));

    std::string bad = std::string(kSpecResponse);
    bad.replace(bad.find("0.1.0"), 5, "9.9.9");
    auto bad_parsed = oxa::json::parse(bad);
    CHECK(bad_parsed.ok());
    auto bad_res = load_response(*bad_parsed);
    CHECK(!bad_res.ok());
    CHECK(bad_res.code() == oxa::StatusCode::kInvalidArgument);

    // Block discriminant shapes are pinned.
    {
        auto bp = oxa::json::parse(R"({"type":"text","text":"hi"})");
        CHECK(bp.ok());
        auto b = load_block(*bp);
        CHECK(b.ok());
        CHECK(std::holds_alternative<TextBlock>(*b));
        CHECK(oxa::json::structurally_equal(*bp, dump_block(*b)));
    }
    {
        auto bp = oxa::json::parse(R"({"type":"image","url":"https://e.com/c.png"})");
        CHECK(bp.ok());
        auto b = load_block(*bp);
        CHECK(b.ok());
        CHECK(std::holds_alternative<ImageBlock>(*b));
    }
    {
        auto bp = oxa::json::parse(R"({"type":"tool_use","id":"c1","name":"f","input":"{}"})");
        CHECK(bp.ok());
        auto b = load_block(*bp);
        CHECK(b.ok());
        CHECK(std::holds_alternative<ToolUseBlock>(*b));
        CHECK(std::get<ToolUseBlock>(*b).input == "{}");
    }

    // Absent and zero are distinct in params.
    {
        auto rp = oxa::json::parse(R"({
            "specVersion": "0.1.0",
            "model": "m",
            "messages": [{ "role": "user", "content": [{ "type": "text", "text": "hi" }] }],
            "params": { "max_tokens": 0 }
        })");
        CHECK(rp.ok());
        auto r_res = load_request(*rp);
        CHECK(r_res.ok());
        const Request& r = *r_res;
        CHECK(r.params.has_value());
        CHECK(r.params->max_tokens.has_value());
        CHECK(*r.params->max_tokens == 0);
        CHECK(!r.params->temperature.has_value());
        Value out = dump_request(r);
        const Value* p = out.find("params");
        CHECK(p != nullptr);
        const Value* mt = p->find("max_tokens");
        CHECK(mt != nullptr && mt->is_int() && mt->as_int() == 0);
        CHECK(p->find("temperature") == nullptr);
    }

    // Strict type error checking (no silent defaults)
    {
        // message_start with numeric id/model must be rejected
        auto ep1 = oxa::json::parse(R"({"type":"message_start","id":7,"model":"m"})");
        CHECK(ep1.ok());
        CHECK(!load_event(*ep1).ok());

        // content_block_start with string index must be rejected
        auto ep2 = oxa::json::parse(R"({"type":"content_block_start","index":"0","block":{"type":"text","text":""}})");
        CHECK(ep2.ok());
        CHECK(!load_event(*ep2).ok());

        // message_delta with string usage tokens must be rejected
        auto ep3 = oxa::json::parse(R"({"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":"4","output_tokens":2}})");
        CHECK(ep3.ok());
        CHECK(!load_event(*ep3).ok());

        // unknown delta discriminant must be rejected
        auto ep4 = oxa::json::parse(R"({"type":"content_block_delta","index":0,"delta":{"type":"unknown_delta","text":"hi"}})");
        CHECK(ep4.ok());
        CHECK(!load_event(*ep4).ok());

        // unknown block discriminant must be rejected
        auto bp = oxa::json::parse(R"({"type":"unknown_block"})");
        CHECK(bp.ok());
        CHECK(!load_block(*bp).ok());

        // text block with non-string text must be rejected
        auto bp2 = oxa::json::parse(R"({"type":"text","text":123})");
        CHECK(bp2.ok());
        CHECK(!load_block(*bp2).ok());

        // tool_result with non-array content must be rejected
        auto bp3 = oxa::json::parse(R"({"type":"tool_result","tool_use_id":"t1","content":"not an array"})");
        CHECK(bp3.ok());
        CHECK(!load_block(*bp3).ok());

        // request with non-array messages must be rejected
        auto rp_bad = oxa::json::parse(R"({"specVersion":"0.1.0","model":"m","messages":"not array"})");
        CHECK(rp_bad.ok());
        CHECK(!load_request(*rp_bad).ok());
    }
}

// ---- checker helpers -------------------------------------------------------

Event start() { return MessageStart{"m", "model"}; }
Event done() { return MessageDone{}; }
Event text_start(std::int64_t i) { return ContentBlockStart{i, TextBlock{""}}; }
Event text_delta(std::int64_t i, std::string t) {
    return ContentBlockDelta{i, TextDelta{std::move(t)}};
}
Event tool_start(std::int64_t i, std::string input) {
    return ContentBlockStart{i, ToolUseBlock{"call_1", "get_weather", std::move(input)}};
}
Event input_delta(std::int64_t i, std::string frag) {
    return ContentBlockDelta{i, InputJsonDelta{std::move(frag)}};
}
Event block_stop(std::int64_t i) { return ContentBlockStop{i}; }
Event msg_delta(std::string stop, std::optional<std::string> seq = std::nullopt) {
    return MessageDelta{std::move(stop), std::move(seq), Usage{}};
}

void assert_rejects(std::vector<Event> events, std::size_t event_index, const char* fragment) {
    oxa::Status s = validate_event_stream(events);
    CHECK(!s.ok());
    std::string prefix = "event " + std::to_string(event_index) + ":";
    CHECK_MSG(s.message().find(prefix) != std::string_view::npos, std::string(s.message()));
    CHECK_MSG(s.message().find(fragment) != std::string_view::npos, std::string(s.message()));
}

void checker_tests() {
    CHECK(validate_event_stream({start(), text_start(0), text_delta(0, "hello"), block_stop(0),
                                 msg_delta(std::string(STOP_END_TURN)), done()}).ok());

    CHECK(validate_event_stream({start(), text_start(0), text_delta(0, "hello"), block_stop(0),
                                 tool_start(1, "{\"x\":1"), input_delta(1, ""),
                                 input_delta(1, "{\"x\":1"), block_stop(1),
                                 msg_delta(std::string(STOP_TOOL_USE)), done()}).ok());

    CHECK(validate_event_stream({start(), tool_start(0, ""), block_stop(0),
                                 msg_delta(std::string(STOP_TOOL_USE)), done()}).ok());

    CHECK(validate_event_stream({start(), msg_delta(std::string(STOP_STOP_SEQUENCE), "END"), done()}).ok());

    // Strict check rejects synthesized tool input; encoder validation accepts.
    {
        std::vector<Event> events = {start(), tool_start(0, "{\"x\":1}"), block_stop(0),
                                     msg_delta(std::string(STOP_TOOL_USE)), done()};
        assert_rejects(events, 2, "without input_json_delta fragments");
        CHECK(validate_event_stream_for_encoder(events).ok());
    }

    assert_rejects({msg_delta(std::string(STOP_END_TURN)), done()}, 0, "message_start");
    assert_rejects({start(), text_start(0), text_start(1), block_stop(0),
                    msg_delta(std::string(STOP_END_TURN)), done()},
                   2, "open block");
    assert_rejects({start(), text_start(1), block_stop(1), msg_delta(std::string(STOP_END_TURN)),
                    done()},
                   1, "index");
    assert_rejects({start(), tool_start(0, "{}"), text_delta(0, "hello"), block_stop(0),
                    msg_delta(std::string(STOP_TOOL_USE)), done()},
                   2, "delta type");
    assert_rejects({start(), text_start(0), input_delta(0, "{}"), block_stop(0),
                    msg_delta(std::string(STOP_END_TURN)), done()},
                   2, "delta type");
    assert_rejects({start(), tool_start(0, "{\"x\":1}"), input_delta(0, "{\"x\":1"),
                    input_delta(0, "\"}"), block_stop(0), msg_delta(std::string(STOP_TOOL_USE)),
                    done()},
                   4, "concatenation");
    assert_rejects({start(), text_start(0), text_delta(1, "hi"), block_stop(0),
                    msg_delta(std::string(STOP_END_TURN)), done()},
                   2, "index");
    assert_rejects({start(), text_start(0), block_stop(1), msg_delta(std::string(STOP_END_TURN)),
                    done()},
                   2, "index");
    assert_rejects({start(), done()}, 1, "message_delta");
    assert_rejects({start(), msg_delta(std::string(STOP_END_TURN)), done(), done()}, 3,
                   "after message_done");
    assert_rejects({start()}, 1, "message_delta");
    assert_rejects({start(), msg_delta(std::string(STOP_END_TURN))}, 2, "message_done");
    assert_rejects({start(), text_start(0)}, 2, "not stopped");
    assert_rejects({start(), msg_delta(std::string(STOP_END_TURN), "END"), done()}, 1,
                   "stop_sequence");
}

}  // namespace

int main() {
    codec_tests();
    checker_tests();
    std::puts("test_ir: all checks passed");
    return 0;
}
