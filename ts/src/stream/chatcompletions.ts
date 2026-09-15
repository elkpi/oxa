import type { Event } from "../ir/index.js";
import {
  ChatCompletionsStreamDecoder,
  ChatCompletionsStreamEncoder,
  type ChatCompletionsChunk,
  type ChatCompletionsStreamDecoderOptions,
  type ChatCompletionsStreamEncoderOptions,
} from "../openai/chatcompletions/index.js";
import { decodeStream, type DecodedStream } from "./decode.js";
import { encodeStream, type EncodedStream } from "./encode.js";

export function decodeChatCompletions(
  source: AsyncIterable<ChatCompletionsChunk>,
  options: ChatCompletionsStreamDecoderOptions = {},
): DecodedStream<Event> {
  return decodeStream(source, new ChatCompletionsStreamDecoder(options));
}

export function encodeChatCompletions(
  source: AsyncIterable<Event>,
  options: ChatCompletionsStreamEncoderOptions = {},
): EncodedStream<ChatCompletionsChunk> {
  return encodeStream(source, new ChatCompletionsStreamEncoder(options));
}
