// Nonstream behavior beyond the golden vectors: structural error boundaries,
// the question context recovery, exact decimal range validation, verbatim
// carries, and the full client-side round trip.

#include <cstdint>
#include <optional>
#include <string>
#include <utility>
#include <vector>

#include "oxa/status.hpp"
#include "oxa/systemone/jev.hpp"
#include "test_util.hpp"

namespace {

using oxa::Conversion;
using oxa::StatusOr;
using oxa::ir::Loss;
using oxa::systemone::jev::Options;
using oxa::systemone::jev::Question;
using oxa::systemone::jev::QuestionMap;

const oxa::json::Value& pick(const oxa::json::Value& value, const char* key) {
    const oxa::json::Value* found = value.find(key);
    if (found == nullptr) oxatest::fail("pick", __FILE__, __LINE__, std::string("missing key: ") + key);
    return *found;
}

std::string str_of(const oxa::json::Value& value) {
    CHECK(value.is_string());
    return value.as_string();
}

std::int64_t int_of(const oxa::json::Value& value) {
    // Verbatim carries wrap numbers as raw source tokens.
    if (value.is_raw()) {
        auto parsed = oxa::json::parse_lossless(value.as_raw_text());
        CHECK(parsed.ok() && parsed->is_int());
        return parsed->as_int();
    }
    CHECK(value.is_int());
    return value.as_int();
}

oxa::ir::Response response_with(const std::string& text) {
    oxa::ir::Response response;
    response.id = "r";
    response.model = "m";
    response.content.push_back(oxa::ir::TextBlock(text));
    response.stop_reason = std::string(oxa::ir::STOP_END_TURN);
    response.usage = oxa::ir::Usage(1, 2);
    return response;
}

std::vector<std::string> degraded_fields(const std::vector<Loss>& losses) {
    std::vector<std::string> fields;
    for (const auto& loss : losses) {
        if (loss.reason == std::string(oxa::ir::LOSS_DEGRADED)) {
            fields.push_back(loss.field);
        }
    }
    return fields;
}

QuestionMap context() {
    QuestionMap questions;
    questions.emplace("urgent", Question{std::string(oxa::systemone::jev::QUESTION_TYPE_NOUL),
                                         std::nullopt, std::nullopt});
    questions.emplace(
        "mood",
        Question{std::string(oxa::systemone::jev::QUESTION_TYPE_CHOICE), std::nullopt,
                 oxa::json::parse(R"json({"good":"g","bad":"b"})json").value()});
    questions.emplace(
        "level",
        Question{std::string(oxa::systemone::jev::QUESTION_TYPE_SCORE), std::nullopt,
                 oxa::json::parse(R"json(["low","high"])json").value()});
    return questions;
}

void test_scalar_context_recovery() {
    auto wire = oxa::systemone::jev::encode_response(
        response_with(R"json({"urgent": 0.95, "mood": "surprise", "level": 1.5})json"),
        Options{oxa::modelmap::Table(), context()});
    CHECK(wire.ok());
    const auto& answers = pick(wire->value, "answers");
    CHECK(str_of(pick(pick(answers, "urgent"), "type")) ==
          std::string(oxa::systemone::jev::QUESTION_TYPE_NOUL));
    CHECK(str_of(pick(pick(answers, "mood"), "type")) ==
          std::string(oxa::systemone::jev::QUESTION_TYPE_CHOICE));
    CHECK(str_of(pick(pick(answers, "level"), "type")) ==
          std::string(oxa::systemone::jev::QUESTION_TYPE_SCORE));
    CHECK(degraded_fields(wire->losses).size() == 2);
}

void test_structural_errors() {
    auto missing = oxa::systemone::jev::encode_response(
        response_with(R"json({"mood": "good"})json"),
        Options{oxa::modelmap::Table(), context()});
    CHECK(!missing.ok());
    CHECK(std::string(missing.status().message()).find("is missing from the response") !=
          std::string::npos);
    CHECK(!oxa::systemone::jev::encode_response(response_with("null"), Options{}).ok());
    auto empty = oxa::systemone::jev::encode_response(response_with("{}"), Options{});
    CHECK(!empty.ok());
    CHECK(std::string(empty.status().message()).find("non-empty JSON object") != std::string::npos);
}

void test_unknown_answer_fields() {
    auto decoded = oxa::systemone::jev::decode_response(std::string_view(
        R"json({"model":"m","answers":{"q":{"type":"noul","noul":1,"ext":"x"}},"usage":{"input_tokens":1,"output_tokens":2}})json"));
    CHECK(decoded.ok());
    CHECK(decoded->losses.size() == 1);
    CHECK(decoded->losses[0].field == "ext");
}

void test_source_order() {
    auto request = oxa::systemone::jev::decode_request(std::string_view(
        R"json({"z_ext": 1, "model": "m", "state": {}, "a_ext": 2, "questions": {"q": {"type": "noul"}}, "assets": {}})json"));
    CHECK(request.ok());
    std::vector<std::string> fields;
    for (const auto& loss : request->losses) fields.push_back(loss.field);
    const std::vector<std::string> expected{"z_ext", "state", "a_ext", "assets"};
    CHECK(fields == expected);

    auto response = oxa::systemone::jev::decode_response(std::string_view(
        R"json({"model":"m","answers":{"z":{"type":"box","x":1},"a":{"type":"noul","y":1}},"usage":{"other":1}})json"));
    CHECK(response.ok());
    std::vector<std::pair<std::string, std::string>> response_fields;
    for (const auto& loss : response->losses) {
        response_fields.emplace_back(loss.path, loss.field);
    }
    const std::vector<std::pair<std::string, std::string>> expected_response{
        {"answers.z", "type"}, {"answers.z", "x"}, {"answers.a", "y"}, {"usage", "other"}};
    CHECK(response_fields == expected_response);
}

void test_scalar_state() {
    auto decoded = oxa::systemone::jev::decode_request(std::string_view(
        R"json({"model":"m","state":true,"questions":{"q":{"type":"noul"}}})json"));
    CHECK(!decoded.ok());
    CHECK(std::string(decoded.status().message())
              .find("state must be a string, object, or array") != std::string::npos);
}

void test_empty_model() {
    auto request = oxa::systemone::jev::decode_request(std::string_view(
        R"json({"model":"","state":"s","questions":{"q":{"type":"noul"}}})json"));
    CHECK(!request.ok());
    CHECK(std::string(request.status().message()).find("model is required") != std::string::npos);
    auto response = oxa::systemone::jev::decode_response(std::string_view(
        R"json({"model":"","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}})json"));
    CHECK(!response.ok());
    CHECK(std::string(response.status().message()).find("model is required") != std::string::npos);
}

void test_whole_unit_skip() {
    auto decoded = oxa::systemone::jev::decode_request(std::string_view(
        R"json({"model":"m","state":"s","questions":{"box":{"type":"box","bogus":1},"ok":{"type":"noul"}}})json"));
    CHECK(decoded.ok());
    std::vector<const Loss*> box_losses;
    for (const auto& loss : decoded->losses) {
        if (loss.path == "questions.box") box_losses.push_back(&loss);
    }
    CHECK(box_losses.size() == 1);
    CHECK(box_losses[0]->field == "type");
}

void test_exact_decimal() {
    auto wire = oxa::systemone::jev::encode_response(
        response_with(R"json({"urgent": 1.0000000000000001, "mood": "surprise", "level": 1.0000000000000001})json"),
        Options{oxa::modelmap::Table(), context()});
    CHECK(wire.ok());
    CHECK(degraded_fields(wire->losses).size() == 3);
}

void test_verbatim_carry() {
    auto wire = oxa::systemone::jev::encode_response(
        response_with(
            R"json({"q":{"type":"choice","choice":"yes","probabilities":{"z":0.2,"a":0.8}}})json"),
        Options{});
    CHECK(wire.ok());
    CHECK(wire->losses.empty());
    auto text = oxa::systemone::jev::encode_response_json(
        response_with(R"json({"q":{"type":"noul","noul":1.0000000000000001}})json"), Options{});
    CHECK(text.ok());
    CHECK(text->value.find("\"noul\":1.0000000000000001") != std::string::npos);
    auto overflow = oxa::systemone::jev::encode_response_json(
        response_with(R"json({"q":{"type":"noul","noul":1e400}})json"), Options{});
    CHECK(overflow.ok());
    CHECK(overflow->value.find("\"noul\":1e400") != std::string::npos);
    auto decoded = oxa::systemone::jev::decode_response(std::string_view(
        R"json({"model":"m","answers":{"q":{"type":"noul","noul":1e-2}},"usage":{"input_tokens":1,"output_tokens":2}})json"));
    CHECK(decoded.ok());
    const auto* canon = std::get_if<oxa::ir::TextBlock>(&decoded->value.content[0]);
    CHECK(canon != nullptr);
    CHECK(canon->text.find("\"noul\":1e-2") != std::string::npos);
}

void test_criteria_shapes() {
    const std::pair<const char*, const char*> invalid[] = {
        {"score", R"json({"yes":"ok"})json"}, {"score", "null"},
        {"score", "[]"},                     {"choice", "[]"},
        {"noul", R"json(["b"])json"},        {"noul", "42"},
    };
    for (const auto& [kind, criteria] : invalid) {
        const std::string wire =
            std::string(R"json({"model":"m","state":"s","questions":{"q":{"type":")json") +
                        kind + R"json(","criteria":)json" + criteria + "}}}";
        auto decoded = oxa::systemone::jev::decode_request(std::string_view(wire));
        CHECK(!decoded.ok());
    }
    auto valid = oxa::systemone::jev::decode_request(std::string_view(
        R"json({"model":"m","state":"s","questions":{"q":{"type":"choice","criteria":{"yes":"ok"}}}})json"));
    CHECK(valid.ok());
}

void test_contradiction_reason() {
    QuestionMap questions;
    questions.emplace("q", Question{std::string(oxa::systemone::jev::QUESTION_TYPE_NOUL),
                                    std::nullopt, std::nullopt});
    auto wire = oxa::systemone::jev::encode_response(response_with(R"json({"q": "wrong"})json"),
                                                     Options{oxa::modelmap::Table(), questions});
    CHECK(wire.ok());
    CHECK(wire->losses.size() == 1);
    CHECK(wire->losses[0].reason == std::string(oxa::ir::LOSS_UNMAPPED_VALUE));
}

void test_empty_criteria_description() {
    auto decoded = oxa::systemone::jev::decode_request(std::string_view(
        R"json({"model":"m","state":"s","questions":{"q":{"type":"choice","criteria":{"yes":""}}}})json"));
    CHECK(decoded.ok());
    const auto* text = std::get_if<oxa::ir::TextBlock>(&decoded->value.messages[0].content[0]);
    CHECK(text != nullptr);
    CHECK(text->text.ends_with("- yes"));
    CHECK(text->text.find("- yes:") == std::string::npos);
}

oxa::ir::Request request_with_schema(const std::string& properties) {
    const std::string schema =
        R"json({"type":"object","properties":{)json" + properties + R"json(},"required":["ok"]})json";
    oxa::ir::Request request;
    request.model = "m";
    request.system.push_back(oxa::ir::TextBlock(
        "state text\n\n" + std::string(oxa::systemone::jev::INSTRUCTION_TEMPLATE)));
    oxa::ir::ResponseFormat format;
    format.type = "json_schema";
    format.name = std::string(oxa::systemone::jev::RESPONSE_FORMAT_NAME);
    format.schema = oxa::json::parse(schema).value();
    oxa::ir::Params params;
    params.response_format = std::move(format);
    request.params = std::move(params);
    return request;
}

void test_schema_exact_shape() {
    auto contradicting = oxa::systemone::jev::encode_request(request_with_schema(
        R"json("ok":{"type":"number","minimum":0,"maximum":1},"weird":{"type":"number","minimum":9,"maximum":9,"enum":[1,2]})json"));
    CHECK(contradicting.ok());
    CHECK(pick(contradicting->value, "questions").find("weird") == nullptr);
    CHECK(contradicting->losses.size() == 1);
    CHECK(contradicting->losses[0].path == "questions.weird");
    CHECK(contradicting->losses[0].reason == std::string(oxa::ir::LOSS_UNSUPPORTED_SEMANTIC));

    auto extra = oxa::systemone::jev::encode_request(request_with_schema(
        R"json("ok":{"type":"number","minimum":0,"maximum":1},"weird":{"type":"number","minimum":0,"maximum":1,"multipleOf":0.5})json"));
    CHECK(extra.ok());
    CHECK(pick(extra->value, "questions").find("weird") == nullptr);
    CHECK(extra->losses.size() == 1);
    CHECK(extra->losses[0].path == "questions.weird");
}

void test_round_trip() {
    auto decoded = oxa::systemone::jev::decode_request(std::string_view(
        R"json({"model":"jev-latest","state":"The customer said the delivery was late.","questions":{"sentiment":{"type":"choice","instructions":"What is the overall sentiment?","criteria":{"positive":"happy","negative":"unhappy","mixed":"both"}},"is_urgent":{"type":"noul","instructions":"Does this convey urgency?"}}})json"));
    CHECK(decoded.ok());
    auto wire = oxa::systemone::jev::encode_request(decoded->value, Options{});
    CHECK(wire.ok());
    CHECK(str_of(pick(wire->value, "state")) == std::string("The customer said the delivery was late."));
    CHECK(degraded_fields(wire->losses).size() == 1);

    QuestionMap questions;
    for (const auto& [id, question] : pick(wire->value, "questions").as_object()) {
        Question restored;
        restored.type = str_of(pick(question, "type"));
        if (const oxa::json::Value* instructions = question.find("instructions");
            instructions != nullptr && instructions->is_string()) {
            restored.instructions = instructions->as_string();
        }
        if (const oxa::json::Value* criteria = question.find("criteria");
            criteria != nullptr) {
            restored.criteria = *criteria;
        }
        questions.emplace(id, std::move(restored));
    }
    auto answers = oxa::systemone::jev::encode_response(
        response_with(
            R"json({"is_urgent": {"type": "noul", "noul": 0}, "sentiment": {"type": "choice", "choice": "mixed"}})json"),
        Options{oxa::modelmap::Table(), questions});
    CHECK(answers.ok());
    CHECK(answers->losses.empty());
    CHECK(str_of(pick(pick(pick(answers->value, "answers"), "sentiment"), "choice")) ==
          std::string("mixed"));
    CHECK(int_of(pick(pick(pick(answers->value, "answers"), "is_urgent"), "noul")) == 0);
    CHECK(str_of(pick(pick(pick(answers->value, "answers"), "is_urgent"), "type")) ==
          std::string(oxa::systemone::jev::QUESTION_TYPE_NOUL));
}

}  // namespace

int main() {
    test_scalar_context_recovery();
    test_structural_errors();
    test_unknown_answer_fields();
    test_source_order();
    test_scalar_state();
    test_empty_model();
    test_whole_unit_skip();
    test_exact_decimal();
    test_verbatim_carry();
    test_criteria_shapes();
    test_contradiction_reason();
    test_empty_criteria_description();
    test_schema_exact_shape();
    test_round_trip();
    return 0;
}
