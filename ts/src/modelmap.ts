/** Optionally substitutes a model name; undefined selects identity fallback. */
export type ModelMapper = (model: string) => string | undefined;

/** Applies an optional mapper with the spec-required identity fallback. */
export function mapModel(
  mapper: ModelMapper | undefined,
  model: string,
): string {
  return mapper?.(model) ?? model;
}
