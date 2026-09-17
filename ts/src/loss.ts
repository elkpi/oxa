/** Why a valid source value could not be represented exactly. */
export type LossReason =
  | "unmapped-field"
  | "unmapped-value"
  | "unsupported-semantic"
  | "degraded";

/** An ordered record of a semantic conversion gap. */
export interface Loss {
  readonly path: string;
  readonly field: string;
  readonly reason: LossReason;
  readonly detail?: string;
}

/** Successful conversion output and all encountered semantic losses. */
export interface ConversionResult<T> {
  readonly value: T;
  readonly losses: readonly Loss[];
}
