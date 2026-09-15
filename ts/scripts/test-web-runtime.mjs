import { sse, stream } from "../dist-web/index.js";

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

const sseDecoder = new sse.SseDecoder();
const frames = [
  ...sseDecoder.feed(new TextEncoder().encode("data: web\n\n")),
  ...sseDecoder.flush(),
];
assert(frames.length === 1, "Web Runtime SSE smoke emitted the wrong count");
assert(frames[0]?.data === "web", "Web Runtime SSE smoke changed data");

async function* chunks() {
  yield {
    id: "chatcmpl-web",
    model: "gpt-4o-mini",
    choices: [
      {
        delta: { role: "assistant", content: "ok" },
        finish_reason: "stop",
      },
    ],
  };
}

const eventTypes = [];
for await (const event of stream.decodeChatCompletions(chunks())) {
  eventTypes.push(event.type);
}
assert(
  eventTypes.join(",") ===
    "message_start,content_block_start,content_block_delta,content_block_stop,message_delta,message_done",
  "Web Runtime async stream smoke lost or reordered events",
);

console.log("Web Runtime smoke passed");
