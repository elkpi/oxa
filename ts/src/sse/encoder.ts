import type { SseEvent } from "./decoder.js";

/** Encodes an opaque SSE event/data record as UTF-8 bytes. */
export function encodeSse(event: SseEvent): Uint8Array {
  const lines: string[] = [];
  if (event.event !== undefined && event.event !== "")
    lines.push(`event: ${event.event}`);
  for (const line of event.data.split("\n")) lines.push(`data: ${line}`);
  lines.push("", "");
  return new TextEncoder().encode(lines.join("\n"));
}
