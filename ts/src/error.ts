/** Stable structural and lifecycle failure categories. */
export type OxaErrorCode =
  | "invalid-input"
  | "invalid-json"
  | "type-violation"
  | "stream-grammar"
  | "stream-lifecycle"
  | "ir-invariant";

/** A conversion failure that callers can classify without matching messages. */
export class OxaError extends Error {
  readonly code: OxaErrorCode;

  constructor(code: OxaErrorCode, message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "OxaError";
    this.code = code;
  }
}
