import type { Event } from "../ir/index.js";
import {
  AnthropicStreamDecoder,
  AnthropicStreamEncoder,
  type AnthropicStreamDecoderOptions,
  type AnthropicStreamEncoderOptions,
  type AnthropicStreamEvent,
} from "../anthropic/messages/index.js";
import { decodeStream, type DecodedStream } from "./decode.js";
import { encodeStream, type EncodedStream } from "./encode.js";

export function decodeAnthropic(
  source: AsyncIterable<AnthropicStreamEvent>,
  options: AnthropicStreamDecoderOptions = {},
): DecodedStream<Event> {
  return decodeStream(source, new AnthropicStreamDecoder(options));
}

export function encodeAnthropic(
  source: AsyncIterable<Event>,
  options: AnthropicStreamEncoderOptions = {},
): EncodedStream<AnthropicStreamEvent> {
  return encodeStream(source, new AnthropicStreamEncoder(options));
}
