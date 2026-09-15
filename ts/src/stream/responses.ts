import type { Event } from "../ir/index.js";
import {
  ResponsesStreamDecoder,
  ResponsesStreamEncoder,
  type ResponsesStreamDecoderOptions,
  type ResponsesStreamEncoderOptions,
  type ResponsesStreamEvent,
} from "../openai/responses/index.js";
import { decodeStream, type DecodedStream } from "./decode.js";
import { encodeStream, type EncodedStream } from "./encode.js";

export function decodeResponses(
  source: AsyncIterable<ResponsesStreamEvent>,
  options: ResponsesStreamDecoderOptions = {},
): DecodedStream<Event> {
  return decodeStream(source, new ResponsesStreamDecoder(options));
}

export function encodeResponses(
  source: AsyncIterable<Event>,
  options: ResponsesStreamEncoderOptions = {},
): EncodedStream<ResponsesStreamEvent> {
  return encodeStream(source, new ResponsesStreamEncoder(options));
}
