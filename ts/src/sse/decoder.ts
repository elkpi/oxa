import { OxaError } from "../error.js";

export interface SseEvent {
  readonly data: string;
  readonly event?: string;
}

/** Incrementally frames opaque SSE event/data records from bytes. */
export class SseDecoder {
  #buffer: Uint8Array<ArrayBufferLike> = new Uint8Array();
  #data: string[] = [];
  #event = "";
  #haveFields = false;

  feed(chunk: Uint8Array): readonly SseEvent[] {
    this.#buffer = append(this.#buffer, chunk);
    const output: SseEvent[] = [];
    let start = 0;
    for (let index = 0; index < this.#buffer.length; index += 1) {
      if (this.#buffer[index] !== 10) continue;
      let end = index;
      if (end > start && this.#buffer[end - 1] === 13) end -= 1;
      const event = this.#line(this.#buffer.slice(start, end));
      if (event !== undefined) output.push(event);
      start = index + 1;
    }
    this.#buffer = this.#buffer.slice(start);
    return output;
  }

  flush(): readonly SseEvent[] {
    const output: SseEvent[] = [];
    if (this.#buffer.length > 0) {
      let end = this.#buffer.length;
      if (this.#buffer[end - 1] === 13) end -= 1;
      const event = this.#line(this.#buffer.slice(0, end));
      if (event !== undefined) output.push(event);
      this.#buffer = new Uint8Array();
    }
    const event = this.#dispatch();
    if (event !== undefined) output.push(event);
    return output;
  }

  #line(bytes: Uint8Array): SseEvent | undefined {
    const line = decode(bytes);
    if (line === "") return this.#dispatch();
    if (line.startsWith(":")) return undefined;
    const separator = line.indexOf(":");
    const field = separator === -1 ? line : line.slice(0, separator);
    let value = separator === -1 ? "" : line.slice(separator + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "data") {
      this.#haveFields = true;
      this.#data.push(value);
    } else if (field === "event") {
      this.#haveFields = true;
      this.#event = value;
    }
    return undefined;
  }

  #dispatch(): SseEvent | undefined {
    if (!this.#haveFields) return undefined;
    const result: SseEvent = {
      data: this.#data.join("\n"),
      ...(this.#event === "" ? {} : { event: this.#event }),
    };
    this.#data = [];
    this.#event = "";
    this.#haveFields = false;
    return result;
  }
}

function append(
  left: Uint8Array<ArrayBufferLike>,
  right: Uint8Array<ArrayBufferLike>,
): Uint8Array<ArrayBufferLike> {
  if (left.length === 0) return right.slice();
  const result = new Uint8Array(left.length + right.length);
  result.set(left);
  result.set(right, left.length);
  return result;
}

function decode(bytes: Uint8Array<ArrayBufferLike>): string {
  try {
    return new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(
      bytes,
    );
  } catch {
    throw new OxaError("invalid-json", "invalid UTF-8 in SSE field line");
  }
}
