// Tests for the oxa.sse frame adapter (spec/20 §6).

#include "oxa/sse.hpp"

#include "test_util.hpp"

using oxa::sse::decode;
using oxa::sse::encode;
using oxa::sse::Frame;

int main() {
    {
        auto frames = decode("data: hello\n\ndata: world\n\n");
        CHECK(frames.size() == 2);
        CHECK(frames[0].event.empty());
        CHECK(frames[0].data == "hello");
        CHECK(frames[1].data == "world");
    }
    {
        auto frames = decode("data: a\r\n\r\ndata: b\n\r\n");
        CHECK(frames.size() == 2);
        CHECK(frames[0].data == "a");
        CHECK(frames[1].data == "b");
    }
    {
        auto frames = decode("data: line1\ndata: line2\n\n");
        CHECK(frames.size() == 1);
        CHECK(frames[0].data == "line1\nline2");
    }
    {
        auto frames = decode("event: first\nevent: second\ndata: x\n\n");
        CHECK(frames.size() == 1);
        CHECK(frames[0].event == "second");
        CHECK(frames[0].data == "x");
    }
    {
        auto frames = decode(": keepalive\nid: 42\nretry: 1000\nbogus: v\ndata: x\n\n");
        CHECK(frames.size() == 1);
        CHECK(frames[0].event.empty());
        CHECK(frames[0].data == "x");
    }
    {
        auto frames = decode("data:\n\n");
        CHECK(frames.size() == 1);
        CHECK(frames[0].data.empty());
        auto frames2 = decode("data: \n\n");
        CHECK(frames2.size() == 1);
        CHECK(frames2[0].data.empty());
    }
    {
        auto frames = decode("data: trailing");
        CHECK(frames.size() == 1);
        CHECK(frames[0].data == "trailing");
    }
    {
        Frame f{"message_delta", "{\"a\":1}\n{\"b\":2}"};
        std::string encoded = encode(f);
        CHECK(encoded == "event: message_delta\ndata: {\"a\":1}\ndata: {\"b\":2}\n\n");
        auto back = decode(encoded);
        CHECK(back.size() == 1);
        CHECK(back[0] == f);
    }
    {
        CHECK(encode(Frame{"", ""}) == "data: \n\n");
    }
    std::puts("test_sse: all checks passed");
    return 0;
}
