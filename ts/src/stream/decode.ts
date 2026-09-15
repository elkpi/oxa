import type { Loss } from "../loss.js";

export interface DecodedStream<Output> extends AsyncIterable<Output> {
  losses(): readonly Loss[];
}

export interface StreamDecoder<Input, Output> {
  Feed(input: Input): readonly Output[];
  Flush(): readonly Output[];
  Losses(): readonly Loss[];
}

export function decodeStream<Input, Output>(
  source: AsyncIterable<Input>,
  decoder: StreamDecoder<Input, Output>,
): DecodedStream<Output> {
  return {
    losses: () => [...decoder.Losses()],
    async *[Symbol.asyncIterator]() {
      for await (const input of source) {
        yield* decoder.Feed(input);
      }
      yield* decoder.Flush();
    },
  };
}
