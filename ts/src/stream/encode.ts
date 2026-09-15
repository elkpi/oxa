import type { ConversionResult, Loss } from "../loss.js";

export interface EncodedStream<Output> extends AsyncIterable<Output> {
  losses(): readonly Loss[];
}

export interface StreamEncoder<Input, Output> {
  Apply(input: Input): ConversionResult<readonly Output[]>;
}

export function encodeStream<Input, Output>(
  source: AsyncIterable<Input>,
  encoder: StreamEncoder<Input, Output>,
): EncodedStream<Output> {
  const losses: Loss[] = [];
  return {
    losses: () => [...losses],
    async *[Symbol.asyncIterator]() {
      for await (const input of source) {
        const result = encoder.Apply(input);
        losses.push(...result.losses);
        yield* result.value;
      }
    },
  };
}
