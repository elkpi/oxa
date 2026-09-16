import type { JsonNumber } from "../../json/index.js";

export interface ResponsesUsage {
  readonly input_tokens: bigint | JsonNumber;
  readonly output_tokens: bigint | JsonNumber;
  readonly total_tokens: bigint | JsonNumber;
}

export interface ResponsesOutputTextPart {
  readonly type: "output_text";
  readonly text: string;
  readonly annotations: readonly unknown[];
}

/** An opaque content part unsupported by the current Responses profile. */
export interface ResponsesUnknownContentPart {
  readonly type: "output_image";
  readonly [field: string]: unknown;
}

export type ResponsesOutputContentPart =
  | ResponsesOutputTextPart
  | ResponsesUnknownContentPart;

export interface ResponsesOutputItem {
  readonly type: string;
  readonly id?: string;
  readonly status?: string;
  readonly role?: string;
  readonly content?: readonly ResponsesOutputContentPart[];
  readonly call_id?: string;
  readonly name?: string;
  /** Opaque JSON text for function_call items. */
  readonly arguments?: string;
  readonly output?: string;
}

export interface ResponsesResponse {
  readonly id: string;
  readonly object: string;
  readonly status: string;
  readonly model: string;
  readonly output: readonly ResponsesOutputItem[];
  readonly usage?: ResponsesUsage;
  readonly incomplete_details?: { readonly reason?: string };
  readonly error?: { readonly code?: string; readonly message?: string };
}

/** A typed Responses streaming event envelope. */
export interface ResponsesStreamEvent {
  readonly type: string;
  readonly response?: ResponsesResponse;
  readonly item_id?: string;
  readonly output_index?: number;
  readonly content_index?: number;
  readonly item?: ResponsesOutputItem;
  readonly part?: ResponsesOutputContentPart;
  readonly delta?: string;
  readonly text?: string;
  readonly call_id?: string;
  readonly name?: string;
  /** Opaque JSON text used only by arguments.done. */
  readonly arguments?: string;
  readonly sequence_number?: number;
}
