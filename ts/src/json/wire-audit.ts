import { isJsonNumber, parseJson, type JsonObject } from "./index.js";
import {
  sourceKeysOf,
  sourceTextOf,
  withSourceKeys,
  withSourceText,
} from "./value.js";
import { OxaError } from "../error.js";
import type { Loss } from "../loss.js";

/**
 * A field audit spec for one wire object shape. `fields` maps known property
 * names to the spec of their value; a missing key means the property is
 * unknown (one `unmapped-field` loss), and a `undefined` value means the
 * property is known but its value is opaque and never traversed. `variant`
 * derives the effective spec for discriminated unions from the node itself;
 * returning `undefined` stops the walk (the whole unit is unsupported and is
 * reported elsewhere as one loss).
 */
export interface WireAuditSpec {
  readonly fields?:
    | Readonly<Record<string, WireAuditSpec | undefined>>
    | undefined;
  readonly items?: WireAuditSpec | undefined;
  readonly variant?:
    | ((node: JsonObject) => WireAuditSpec | undefined)
    | undefined;
}

export interface WireAudit {
  readonly losses: readonly Loss[];
  readonly keyOrder: ReadonlyMap<string, number>;
}

/**
 * Walks a parsed wire document and reports every unrecognized property as one
 * `unmapped-field` loss. Presence alone triggers a loss; values are never
 * type-checked or recursed into for unknown keys. Array elements and object
 * members are visited in source encounter order.
 */
export function auditWire(root: unknown, spec: WireAuditSpec): WireAudit {
  const losses: Loss[] = [];
  const keyOrder = new Map<string, number>();
  const visit = (
    node: unknown,
    shape: WireAuditSpec | undefined,
    path: string,
  ): void => {
    if (node === null || typeof node !== "object" || shape === undefined)
      return;
    if (Array.isArray(node)) {
      if (shape.items === undefined) return;
      node.forEach((item, index) =>
        visit(item, shape.items, `${path}[${index}]`),
      );
      return;
    }
    const object = node as JsonObject;
    recordKey(keyOrder, path);
    let effective = shape;
    if (shape.variant !== undefined) {
      const variant = shape.variant(object);
      if (variant === undefined) return;
      effective = variant;
    }
    if (effective.fields === undefined) return;
    for (const key of sourceKeysOf(object)) {
      const fieldPath = joinPath(path, key);
      recordKey(keyOrder, fieldPath);
      const child = effective.fields[key];
      if (!Object.prototype.hasOwnProperty.call(effective.fields, key)) {
        losses.push({ path: fieldPath, field: key, reason: "unmapped-field" });
        continue;
      }
      visit(object[key], child, fieldPath);
    }
  };
  visit(root, spec, "");
  return { losses, keyOrder };
}

function recordKey(keyOrder: Map<string, number>, path: string): void {
  if (!keyOrder.has(path)) keyOrder.set(path, keyOrder.size);
}

const lossKey = (loss: Loss): string =>
  `${loss.path}\u0000${loss.field}\u0000${loss.reason}`;

const maxOrder = Number.MAX_SAFE_INTEGER;

/**
 * Merges converter losses with audit losses in source encounter order.
 * Converter losses keep their relative order; audit losses already follow key
 * order. A loss the converter already reported suppresses the duplicate audit
 * loss for the same (path, field, reason); distinct source constructs are
 * never deduplicated.
 */
export function mergeWireLosses(
  converterLosses: readonly Loss[],
  audit: WireAudit,
): Loss[] {
  const seen = new Set<string>();
  const entries = converterLosses.map((loss, index) => ({
    loss,
    order: lossKeyOrder(loss, audit.keyOrder),
    index,
  }));
  for (const entry of entries) {
    seen.add(lossKey(entry.loss));
  }
  for (const loss of audit.losses) {
    const key = lossKey(loss);
    if (seen.has(key)) continue;
    seen.add(key);
    entries.push({
      loss,
      order: lossKeyOrder(loss, audit.keyOrder),
      index: entries.length,
    });
  }
  entries.sort((left, right) =>
    left.order === right.order
      ? left.index - right.index
      : left.order - right.order,
  );
  return entries.map((entry) => entry.loss);
}

function lossKeyOrder(
  loss: Loss,
  keyOrder: ReadonlyMap<string, number>,
): number {
  if (loss.field !== "") {
    const order = keyOrder.get(joinPath(loss.path, loss.field));
    if (order !== undefined) return order;
  }
  return keyOrder.get(loss.path) ?? maxOrder;
}

function joinPath(base: string, key: string): string {
  return base === "" ? key : `${base}.${key}`;
}

export function parseWireEvent(raw: string, spec: WireAuditSpec): unknown {
  const root = parseJson(raw);
  if (
    root === null ||
    typeof root !== "object" ||
    Array.isArray(root) ||
    isJsonNumber(root)
  ) {
    throw new OxaError("type-violation", "stream event must be an object");
  }
  const native = (node: unknown, shape: WireAuditSpec | undefined): unknown => {
    if (node === null || typeof node !== "object" || shape === undefined)
      return node;
    if (Array.isArray(node))
      return shape.items === undefined
        ? node
        : node.map((item) => native(item, shape.items));
    const object = node as JsonObject;
    const effective =
      shape.variant === undefined ? shape : shape.variant(object);
    if (effective?.fields === undefined) return node;
    const copy: Record<string, unknown> = {};
    for (const key of sourceKeysOf(object)) {
      const value = object[key];
      let mapped: unknown = value;
      if (Object.prototype.hasOwnProperty.call(effective.fields, key)) {
        if (
          ["index", "output_index", "content_index"].includes(key) &&
          isJsonNumber(value)
        ) {
          if (!value.isInteger || !Number.isSafeInteger(Number(value.token))) {
            throw new OxaError(
              "type-violation",
              `${key} must be a safe integer`,
            );
          }
          mapped = Number(value.token);
        } else {
          mapped = native(value, effective.fields[key]);
        }
      }
      Object.defineProperty(copy, key, {
        value: mapped,
        enumerable: true,
        writable: true,
        configurable: true,
      });
    }
    withSourceKeys(copy as JsonObject, sourceKeysOf(object));
    const source = sourceTextOf(object);
    if (source !== undefined) withSourceText(copy as JsonObject, source);
    return copy;
  };
  return native(root, spec);
}
