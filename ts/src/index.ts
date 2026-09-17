/** The npm package coordinate, exposed for installation smoke tests. */
export const packageName = "@elkpi/oxa";

export { OxaError, type OxaErrorCode } from "./error.js";
export { type ConversionResult, type Loss, type LossReason } from "./loss.js";
export { mapModel, type ModelMapper } from "./modelmap.js";

export * as json from "./json/index.js";

export * as ir from "./ir/index.js";
export * as sse from "./sse/index.js";
export * as stream from "./stream/index.js";
export * as chatcompletions from "./openai/chatcompletions/index.js";
export * as responses from "./openai/responses/index.js";
export * as anthropic from "./anthropic/messages/index.js";
