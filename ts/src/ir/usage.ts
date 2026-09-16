import { OxaError } from "../error.js";
import type { JsonNumber } from "../json/index.js";

export const maxUsageTokens = 9_223_372_036_854_775_807n;

export function parseUsageInteger(
  value: bigint | JsonNumber,
  path: string,
): bigint {
  const parsed =
    typeof value === "bigint"
      ? value
      : value.isInteger
        ? BigInt(value.token)
        : 0n;
  if (
    (typeof value !== "bigint" && !value.isInteger) ||
    parsed < 0n ||
    parsed > maxUsageTokens
  )
    throw new OxaError(
      "invalid-input",
      `${path} must be a non-negative signed int64 integer`,
    );
  return parsed;
}

export function encodeUsageInteger(value: bigint, path: string): bigint {
  return parseUsageInteger(value, path);
}
