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
  variant: (tool) => {
    if (
      typeof tool.type === "string" &&
      tool.type !== "" &&
      tool.type !== "function" &&
      tool.type !== "custom"
    ) {
      return {};
    }
    return {
      fields: {
        type: undefined,
        name: undefined,
        description: undefined,
        input_schema: undefined,
        cache_control: undefined,
      },
    };
  },
};

const TOOL_CHOICE: WireAuditSpec = {
  fields: {
    type: undefined,
    name: undefined,
  },
};

const THINKING: WireAuditSpec = {
  fields: { type: undefined, budget_tokens: undefined, display: undefined },
};

const OUTPUT_CONFIG: WireAuditSpec = {
  fields: { effort: undefined },
};

const CONTEXT_MANAGEMENT: WireAuditSpec = {
  fields: { edits: undefined },
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

const RESPONSE_BLOCK: WireAuditSpec = {
  variant: (block) => {
    switch (block.type) {
      case "text":
        return { fields: { type: undefined, text: undefined } };
      case "thinking":
        return {
          fields: {
            type: undefined,
            thinking: undefined,
            signature: undefined,
          },
        };
      case "image":
        return { fields: { type: undefined, source: SOURCE } };
      case "tool_use":
        return {
          fields: {
            type: undefined,
            id: undefined,
            name: undefined,
            input: undefined,
          },
        };
      case "tool_result":
        return {
          fields: {
            type: undefined,
            tool_use_id: undefined,
            content: { items: RESPONSE_BLOCK },
            is_error: undefined,
          },
        };
      default:
        return undefined;
    }
  },
};

const RESPONSE: WireAuditSpec = {
  fields: {
    id: undefined,
    type: undefined,
    role: undefined,
    model: undefined,
    content: { items: RESPONSE_BLOCK },
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
    tools: { items: TOOL },
    tool_choice: TOOL_CHOICE,
    thinking: THINKING,
    cache_control: undefined,
    output_config: OUTPUT_CONFIG,
    context_management: CONTEXT_MANAGEMENT,
  },
};

export const ANTHROPIC_RESPONSE: WireAuditSpec = RESPONSE;

const STREAM_DELTA: WireAuditSpec = {
  variant: (delta) => {
    switch (delta.type) {
      case "text_delta":
        return { fields: { type: undefined, text: undefined } };
      case "input_json_delta":
        return { fields: { type: undefined, partial_json: undefined } };
      case "thinking_delta":
        return { fields: { type: undefined, thinking: undefined } };
      case "signature_delta":
        return { fields: { type: undefined, signature: undefined } };
      default:
        return undefined;
    }
  },
};

const MESSAGE_DELTA: WireAuditSpec = {
  fields: { stop_reason: undefined, stop_sequence: undefined },
};

export const ANTHROPIC_STREAM_EVENT: WireAuditSpec = {
  variant: (event) => {
    switch (event.type) {
      case "message_start":
        return { fields: { type: undefined, message: RESPONSE } };
      case "content_block_start":
        return {
          fields: { type: undefined, index: undefined, content_block: BLOCK },
        };
      case "content_block_delta":
        return {
          fields: { type: undefined, index: undefined, delta: STREAM_DELTA },
        };
      case "content_block_stop":
        return { fields: { type: undefined, index: undefined } };
      case "message_delta":
        return {
          fields: { type: undefined, delta: MESSAGE_DELTA, usage: USAGE },
        };
      case "message_stop":
        return { fields: { type: undefined } };
      default:
        return undefined;
    }
  },
};
