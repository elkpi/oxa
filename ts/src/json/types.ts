/** A JSON numeric token retained without JavaScript number coercion. */
export interface JsonNumber {
  readonly kind: "number";
  readonly token: string;
  readonly isInteger: boolean;
}

export interface JsonArray extends ReadonlyArray<JsonValue> {}

export interface JsonObject {
  readonly [key: string]: JsonValue;
}

export type JsonValue =
  | null
  | boolean
  | string
  | JsonNumber
  | JsonArray
  | JsonObject;

/** Opaque JSON source text that converters must not parse incidentally. */
export type JsonText = string & { readonly __jsonTextBrand: unique symbol };

/** Values accepted by the explicit canonicalizing construction helper. */
export interface JsonInputArray extends ReadonlyArray<JsonInput> {}

export interface JsonInputObject {
  readonly [key: string]: JsonInput;
}

export type JsonInput =
  | JsonValue
  | number
  | bigint
  | JsonInputArray
  | JsonInputObject;

const sourceObjects = new WeakSet<object>();

export function markSourceObject(value: object): void {
  sourceObjects.add(value);
}

export function isJsonNumber(value: unknown): value is JsonNumber {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    !sourceObjects.has(value) &&
    "kind" in value &&
    (value as { readonly kind?: unknown }).kind === "number" &&
    typeof (value as { readonly token?: unknown }).token === "string" &&
    typeof (value as { readonly isInteger?: unknown }).isInteger === "boolean"
  );
}

export function isJsonArray(value: unknown): value is JsonArray {
  return Array.isArray(value);
}
