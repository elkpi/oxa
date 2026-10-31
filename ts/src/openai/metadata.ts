import { OxaError } from "../error.js";
import { isJsonNumber, type JsonValue } from "../json/index.js";

export function stringMetadata(
  value: JsonValue | undefined,
  face: string,
): { metadata: Record<string, string> } | Record<string, never> {
  if (value === undefined || value === null) return {};
  if (
    typeof value !== "object" ||
    Array.isArray(value) ||
    isJsonNumber(value)
  ) {
    throw new OxaError("type-violation", `${face}: metadata must be an object`);
  }
  const metadata: Record<string, string> = Object.create(null);
  for (const [key, child] of Object.entries(value)) {
    if (key.length === 0) {
      throw new OxaError(
        "type-violation",
        `${face}: metadata key cannot be empty`,
      );
    }
    if (typeof child !== "string") {
      throw new OxaError(
        "type-violation",
        `${face}: metadata.${key} must be a string`,
      );
    }
    Object.defineProperty(metadata, key, {
      value: child,
      enumerable: true,
      writable: true,
      configurable: true,
    });
  }
  return Object.keys(metadata).length === 0 ? {} : { metadata };
}
