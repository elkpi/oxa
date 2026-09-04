// Tests for the oxa JSON value model, parser, serializer, and comparator.

#include "oxa/json.hpp"
#include "test_util.hpp"

using oxa::json::Value;

int main() {
    // --- parsing basics --------------------------------------------------
    {
        auto v_res = oxa::json::parse(R"({"a": 1, "b": "two", "c": [1, 2, 3]})");
        CHECK(v_res.ok());
        const Value& v = *v_res;
        CHECK(v.is_object());
        CHECK(v.find("a")->as_int() == 1);
        CHECK(v.find("b")->as_string() == "two");
        CHECK(v.find("c")->as_array().size() == 3);
    }
    {
        auto v_res = oxa::json::parse("  true  ");
        CHECK(v_res.ok());
        CHECK(v_res->as_bool());
    }
    {
        auto v_res = oxa::json::parse("-42");
        CHECK(v_res.ok());
        CHECK(v_res->as_int() == -42);
    }
    {
        // Malformed inputs return error Status (no throw)
        CHECK(!oxa::json::parse("{\"a\":}").ok());
        CHECK(!oxa::json::parse("[1, 2").ok());
        CHECK(!oxa::json::parse("1 2").ok());
        CHECK(!oxa::json::parse("\"abc").ok());
        // Leading zeros not allowed in numbers (RFC 8259)
        CHECK(!oxa::json::parse("01").ok());
        CHECK(!oxa::json::parse("-01").ok());
        CHECK(!oxa::json::parse("00").ok());
        // Single zero and signed zero allowed
        CHECK(oxa::json::parse("0").ok() && oxa::json::parse("0")->as_int() == 0);
        CHECK(oxa::json::parse("-0").ok());
        CHECK(oxa::json::parse("0.5").ok() && oxa::json::parse("0.5")->as_double() == 0.5);
        // Incomplete numbers rejected
        CHECK(!oxa::json::parse("-").ok());
        CHECK(!oxa::json::parse("1.").ok());
        CHECK(!oxa::json::parse("1e").ok());
        CHECK(!oxa::json::parse("1e+").ok());
        // Integer overflow rejected (not converted to float)
        auto overflow_res = oxa::json::parse("9223372036854775808");
        CHECK(!overflow_res.ok());
    }

    // --- duplicate keys rejected (Q15) -----------------------------------
    {
        auto dup_res = oxa::json::parse(R"({"x": 1, "x": 2})");
        CHECK(!dup_res.ok());
        CHECK(dup_res.code() == oxa::StatusCode::kInvalidArgument);
    }

    // --- integer fidelity (INV-7) ----------------------------------------
    {
        auto i_val = oxa::json::parse("1");
        auto d_val1 = oxa::json::parse("1.0");
        auto d_val2 = oxa::json::parse("1e+01");
        CHECK(i_val.ok() && i_val->is_int());
        CHECK(d_val1.ok() && d_val1->is_double());
        CHECK(d_val2.ok() && d_val2->is_double());
        CHECK(!oxa::json::structurally_equal(*i_val, *d_val1));
        auto d_15 = oxa::json::parse("1.5");
        auto d_150 = oxa::json::parse("1.50");
        CHECK(oxa::json::structurally_equal(*d_15, *d_150));
        auto b_true = oxa::json::parse("true");
        CHECK(!oxa::json::structurally_equal(*b_true, *i_val));
    }

    // --- structural equality ---------------------------------------------
    {
        auto a = oxa::json::parse(R"({"a": 1, "b": {"x": "y"}})");
        auto b = oxa::json::parse(R"({"b": {"x": "y"}, "a": 1})");
        CHECK(a.ok() && b.ok());
        CHECK(oxa::json::structurally_equal(*a, *b));
        auto arr1 = oxa::json::parse("[1,2,3]");
        auto arr2 = oxa::json::parse("[1,3,2]");
        CHECK(!oxa::json::structurally_equal(*arr1, *arr2));
        auto o1 = oxa::json::parse(R"({"a":1})");
        auto o2 = oxa::json::parse(R"({"a":1,"b":2})");
        CHECK(!oxa::json::structurally_equal(*o1, *o2));
    }

    // --- unicode escapes ---------------------------------------------------
    {
        auto v = oxa::json::parse("\"\\u0050\\u0041\"");
        CHECK(v.ok());
        CHECK(v->as_string() == "PA");
    }
    {
        auto v = oxa::json::parse(R"("😀")");
        CHECK(v.ok());
        CHECK(v->as_string().size() == 4);  // U+1F600 encodes to 4 UTF-8 bytes
        auto round = oxa::json::parse(oxa::json::serialize(*v));
        CHECK(round.ok());
        CHECK(round->as_string() == v->as_string());
    }

    // --- source spans & safe retention (INV-1 & Q7) -----------------------
    {
        // Notice: doc is parsed from a temporary string, but input->raw_slice()
        // is valid even after the original string expression goes out of scope!
        Value doc;
        {
            std::string temp_json = R"({"input": {"city": "Paris", "n": 1e+01}})";
            auto parsed = oxa::json::parse(temp_json);
            CHECK(parsed.ok());
            doc = std::move(*parsed);
        }
        const Value* input = doc.find("input");
        CHECK(input != nullptr);
        std::string_view slice = input->raw_slice();
        CHECK(slice == R"({"city": "Paris", "n": 1e+01})");
    }

    // --- serialization ------------------------------------------------------
    {
        Value v = Value::object();
        v.set("type", Value::string("text"));
        v.set("text", Value::string("hi\n\"there\""));
        std::string out = oxa::json::serialize(v);
        auto back = oxa::json::parse(out);
        CHECK(back.ok());
        CHECK(back->find("type")->as_string() == "text");
        CHECK(back->find("text")->as_string() == "hi\n\"there\"");

        Value d = Value::real(10.0);
        auto dback = oxa::json::parse(oxa::json::serialize(d));
        CHECK(dback.ok());
        CHECK(dback->is_double());
        CHECK(dback->as_double() == 10.0);
    }
    {
        // Raw values are emitted verbatim and compare structurally.
        Value out = Value::object();
        out.set("input", Value::raw(R"({"city": "Paris"})"));
        std::string text = oxa::json::serialize(out);
        auto parsed = oxa::json::parse(text);
        auto expected = oxa::json::parse(R"({"input": {"city": "Paris"}})");
        CHECK(parsed.ok() && expected.ok());
        CHECK(oxa::json::structurally_equal(*expected, *parsed));

        Value a = Value::raw(R"({"x": 1})");
        auto b = oxa::json::parse(R"({"x": 1})");
        CHECK(b.ok());
        CHECK(oxa::json::structurally_equal(a, *b));
    }

    std::puts("test_json: all checks passed");
    return 0;
}
