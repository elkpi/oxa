// Tests for the modelmap table (spec/03).

#include "oxa/modelmap.hpp"

#include "test_util.hpp"

using oxa::modelmap::Table;

int main() {
    {
        Table t;
        CHECK(t.map("gpt-4o-mini") == "gpt-4o-mini");
    }
    {
        Table t;
        t.insert("gpt-4o-mini", "claude-haiku-4-5");
        CHECK(t.map("gpt-4o-mini") == "claude-haiku-4-5");
    }
    {
        Table t;
        t.insert("gpt-4o-mini", "claude-haiku-4-5");
        CHECK(t.map("gpt-4o") == "gpt-4o");
    }
    {
        Table t{{{"a", "b"}}};
        CHECK(t.map("a") == "b");
        CHECK(t.map("c") == "c");
    }
    std::puts("test_modelmap: all checks passed");
    return 0;
}
