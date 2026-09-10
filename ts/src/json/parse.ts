import { OxaError } from "../error.js";
import type { JsonNumber, JsonObject, JsonValue } from "./types.js";

const numberPattern = /-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/y;

/** Parses JSON while retaining every number token exactly as supplied. */
export function parseJson(source: string): JsonValue {
  return new Parser(source).parse();
}

class Parser {
  #position = 0;

  constructor(private readonly source: string) {}

  parse(): JsonValue {
    this.skipWhitespace();
    const value = this.parseValue();
    this.skipWhitespace();
    if (this.#position !== this.source.length) {
      this.fail("unexpected trailing JSON input");
    }
    return value;
  }

  private parseValue(): JsonValue {
    const next = this.peek();
    if (next === '"') return this.parseString();
    if (next === "{") return this.parseObject();
    if (next === "[") return this.parseArray();
    if (next === "t") return this.parseKeyword("true", true);
    if (next === "f") return this.parseKeyword("false", false);
    if (next === "n") return this.parseKeyword("null", null);
    if (next === "-" || (next !== undefined && next >= "0" && next <= "9")) {
      return this.parseNumber();
    }
    this.fail("expected a JSON value");
  }

  private parseKeyword<T extends null | boolean>(keyword: string, value: T): T {
    if (!this.source.startsWith(keyword, this.#position)) {
      this.fail(`expected ${keyword}`);
    }
    this.#position += keyword.length;
    return value;
  }

  private parseObject(): JsonObject {
    this.consume("{");
    this.skipWhitespace();
    const object: Record<string, JsonValue> = {};
    if (this.tryConsume("}")) return object;
    for (;;) {
      if (this.peek() !== '"') this.fail("object key must be a string");
      const key = this.parseString();
      if (Object.prototype.hasOwnProperty.call(object, key)) {
        this.fail("duplicate object key");
      }
      this.skipWhitespace();
      this.consume(":");
      this.skipWhitespace();
      object[key] = this.parseValue();
      this.skipWhitespace();
      if (this.tryConsume("}")) return object;
      this.consume(",");
      this.skipWhitespace();
    }
  }

  private parseArray(): JsonValue[] {
    this.consume("[");
    this.skipWhitespace();
    const array: JsonValue[] = [];
    if (this.tryConsume("]")) return array;
    for (;;) {
      array.push(this.parseValue());
      this.skipWhitespace();
      if (this.tryConsume("]")) return array;
      this.consume(",");
      this.skipWhitespace();
    }
  }

  private parseString(): string {
    this.consume('"');
    let output = "";
    for (;;) {
      const character = this.peek();
      if (character === undefined) this.fail("unterminated string");
      this.#position += 1;
      if (character === '"') return output;
      if (character === "\\") {
        output += this.parseEscape();
        continue;
      }
      if (character < " ") this.fail("unescaped control character in string");
      output += character;
    }
  }

  private parseEscape(): string {
    const escape = this.peek();
    if (escape === undefined) this.fail("unterminated string escape");
    this.#position += 1;
    switch (escape) {
      case '"':
      case "\\":
      case "/":
        return escape;
      case "b":
        return "\b";
      case "f":
        return "\f";
      case "n":
        return "\n";
      case "r":
        return "\r";
      case "t":
        return "\t";
      case "u":
        return this.parseUnicodeEscape();
      default:
        this.fail("invalid string escape");
    }
  }

  private parseUnicodeEscape(): string {
    const hex = this.source.slice(this.#position, this.#position + 4);
    if (!/^[0-9a-fA-F]{4}$/.test(hex)) this.fail("invalid unicode escape");
    this.#position += 4;
    return String.fromCharCode(Number.parseInt(hex, 16));
  }

  private parseNumber(): JsonNumber {
    numberPattern.lastIndex = this.#position;
    const match = numberPattern.exec(this.source);
    if (match?.[0] === undefined) this.fail("invalid number");
    const token = match[0];
    this.#position += token.length;
    return { kind: "number", token, isInteger: !/[.eE]/.test(token) };
  }

  private skipWhitespace(): void {
    while (
      this.peek() === " " ||
      this.peek() === "\n" ||
      this.peek() === "\r" ||
      this.peek() === "\t"
    ) {
      this.#position += 1;
    }
  }

  private consume(expected: string): void {
    if (!this.tryConsume(expected)) this.fail(`expected ${expected}`);
  }

  private tryConsume(expected: string): boolean {
    if (this.source.startsWith(expected, this.#position)) {
      this.#position += expected.length;
      return true;
    }
    return false;
  }

  private peek(): string | undefined {
    return this.source[this.#position];
  }

  private fail(message: string): never {
    throw new OxaError(
      "invalid-json",
      `${message} at offset ${this.#position}`,
    );
  }
}
