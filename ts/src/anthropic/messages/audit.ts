import type { WireAuditSpec } from "../../json/wire-audit.js";

const SYSTEM_BLOCK: WireAuditSpec = {
  fields: { type: undefined, text: undefined, cache_control: undefined },
};

const SOURCE: WireAuditSpec = {
  fields: {
    type: undefined,
    media_type: undefined,
    data: undefined,
    url: undefined,
  },
};

const BLOCK: WireAuditSpec = {
  variant: (block) => {
    switch (block.type) {
      case "text":
        return {
          fields: {
            type: undefined,
            text: undefined,
            cache_control: undefined,
          },
        };
      case "thinking":
        return {
          fields: {
            type: undefined,
            thinking: undefined,
            signature: undefined,
            cache_control: undefined,
          },
        };
      case "image":
        return {
          fields: { type: undefined, source: SOURCE, cache_control: undefined },
        };
      case "tool_use":
        return {
          fields: {
            type: undefined,
            id: undefined,
            name: undefined,
            input: undefined,
            cache_control: undefined,
          },
        };
      case "tool_result":
        return {
          fields: {
            type: undefined,
            tool_use_id: undefined,
            content: { items: BLOCK },
            is_error: undefined,
            cache_control: undefined,
          },
        };
      default:
        return undefined;
    }
  },
};

const TOOL: WireAuditSpec = {
  fields: { name: undefined, description: undefined, input_schema: undefined },
};

const TOOL_CHOICE: WireAuditSpec = {
  fields: {
    type: undefined,
    name: undefined,
    disable_parallel_tool_use: undefined,
  },
};

const THINKING: WireAuditSpec = {
  fields: { type: undefined, budget_tokens: undefined },
};

const MESSAGE: WireAuditSpec = {
  fields: {
    role: undefined,
    content: { items: BLOCK },
  },
};

const USAGE: WireAuditSpec = {
  fields: {
    input_tokens: undefined,
    output_tokens: undefined,
    cache_read_input_tokens: undefined,
    cache_creation_input_tokens: undefined,
  },
};

const RESPONSE: WireAuditSpec = {
  fields: {
    id: undefined,
    type: undefined,
    role: undefined,
    model: undefined,
    content: { items: BLOCK },
    stop_reason: undefined,
    stop_sequence: undefined,
    usage: USAGE,
  },
};

export const ANTHROPIC_REQUEST: WireAuditSpec = {
  fields: {
    model: undefined,
    system: { items: SYSTEM_BLOCK },
    messages: { items: MESSAGE },
    max_tokens: undefined,
    temperature: undefined,
    top_p: undefined,
    stop_sequences: { items: undefined },
    metadata: undefined,
    tools: { items: TOOL },
    tool_choice: TOOL_CHOICE,
    thinking: THINKING,
  },
};

export const ANTHROPIC_RESPONSE: WireAuditSpec = RESPONSE;

const STREAM_DELTA: WireAuditSpec = {
  fields: {
    type: undefined,
    text: undefined,
    partial_json: undefined,
    thinking: undefined,
    signature: undefined,
    stop_reason: undefined,
    stop_sequence: undefined,
  },
};

export const ANTHROPIC_STREAM_EVENT: WireAuditSpec = {
  variant: (event) => {
    if (
      ![
        "message_start",
        "content_block_start",
        "content_block_delta",
        "content_block_stop",
        "message_delta",
        "message_stop",
      ].includes(String(event.type))
    )
      return undefined;
    return {
      fields: {
        type: undefined,
        message: RESPONSE,
        index: undefined,
        content_block: BLOCK,
        delta: STREAM_DELTA,
        usage: USAGE,
      },
    };
  },
};
