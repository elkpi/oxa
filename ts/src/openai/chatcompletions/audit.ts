import type { WireAuditSpec } from "../../json/wire-audit.js";

const PART: WireAuditSpec = {
  variant: (part) => {
    if (part.type === "text")
      return { fields: { type: undefined, text: undefined } };
    if (part.type === "image_url")
      return {
        fields: { type: undefined, image_url: { fields: { url: undefined } } },
      };
    if (part.type === "file")
      return {
        fields: {
          type: undefined,
          file: {
            fields: {
              file_id: undefined,
              file_data: undefined,
              filename: undefined,
            },
          },
        },
      };
    return undefined;
  },
};

const FUNCTION: WireAuditSpec = {
  fields: {
    name: undefined,
    description: undefined,
    parameters: undefined,
    strict: undefined,
    arguments: undefined,
  },
};

const FUNCTION_CALL: WireAuditSpec = {
  fields: {
    name: undefined,
    description: undefined,
    parameters: undefined,
    arguments: undefined,
  },
};

const CUSTOM: WireAuditSpec = {
  fields: {
    name: undefined,
    description: undefined,
    format: {
      fields: {
        type: undefined,
        syntax: undefined,
        definition: undefined,
      },
    },
    input: undefined,
  },
};

const TOOL_CALL: WireAuditSpec = {
  fields: {
    id: undefined,
    type: undefined,
    function: FUNCTION_CALL,
    custom: CUSTOM,
  },
};

const MESSAGE: WireAuditSpec = {
  fields: {
    role: undefined,
    content: { items: PART },
    refusal: undefined,
    reasoning_content: undefined,
    tool_calls: { items: TOOL_CALL },
    tool_call_id: undefined,
  },
};

const TOOL: WireAuditSpec = {
  variant: (tool) => {
    if (tool.type === "function") {
      return { fields: { type: undefined, function: FUNCTION } };
    }
    if (tool.type === "custom") {
      return { fields: { type: undefined, custom: CUSTOM } };
    }
    return undefined;
  },
};

const FUNCTION_CHOICE: WireAuditSpec = {
  fields: { name: undefined },
};

const CUSTOM_CHOICE: WireAuditSpec = {
  fields: { name: undefined },
};

const TOOL_CHOICE: WireAuditSpec = {
  variant: (choice) => {
    if (choice.type === "function") {
      return { fields: { type: undefined, function: FUNCTION_CHOICE } };
    }
    if (choice.type === "custom") {
      return { fields: { type: undefined, custom: CUSTOM_CHOICE } };
    }
    return undefined;
  },
};

const RESPONSE_FORMAT: WireAuditSpec = {
  variant: (format) => {
    if (format === null || typeof format !== "object" || Array.isArray(format))
      return undefined;
    return {
      fields: {
        type: undefined,
        json_schema: {
          fields: {
            name: undefined,
            description: undefined,
            schema: undefined,
            strict: undefined,
          },
        },
      },
    };
  },
};

const USAGE: WireAuditSpec = {
  fields: {
    prompt_tokens: undefined,
    completion_tokens: undefined,
    total_tokens: undefined,
    prompt_tokens_details: { fields: { cached_tokens: undefined } },
    completion_tokens_details: { fields: { reasoning_tokens: undefined } },
  },
};

const CHOICE: WireAuditSpec = {
  fields: { index: undefined, message: MESSAGE, finish_reason: undefined },
};

const STREAM_OPTIONS: WireAuditSpec = {
  fields: {
    include_usage: undefined,
    include_obfuscation: undefined,
  },
};

export const CHAT_REQUEST: WireAuditSpec = {
  fields: {
    model: undefined,
    messages: { items: MESSAGE },
    temperature: undefined,
    top_p: undefined,
    max_tokens: undefined,
    max_completion_tokens: undefined,
    store: undefined,
    stream: undefined,
    stream_options: STREAM_OPTIONS,
    stop: { items: undefined },
    reasoning_effort: undefined,
    verbosity: undefined,
    tools: { items: TOOL },
    tool_choice: TOOL_CHOICE,
    response_format: RESPONSE_FORMAT,
    metadata: undefined,
  },
};

export const CHAT_RESPONSE: WireAuditSpec = {
  fields: {
    id: undefined,
    object: undefined,
    created: undefined,
    model: undefined,
    choices: { items: CHOICE },
    usage: USAGE,
  },
};

const TOOL_CALL_DELTA: WireAuditSpec = {
  fields: {
    index: undefined,
    id: undefined,
    type: undefined,
    function: FUNCTION_CALL,
    custom: CUSTOM,
  },
};

const DELTA: WireAuditSpec = {
  fields: {
    role: undefined,
    content: undefined,
    reasoning_content: undefined,
    tool_calls: { items: TOOL_CALL_DELTA },
  },
};

const CHOICE_DELTA: WireAuditSpec = {
  fields: { index: undefined, delta: DELTA, finish_reason: undefined },
};

export const CHAT_CHUNK: WireAuditSpec = {
  fields: {
    id: undefined,
    object: undefined,
    created: undefined,
    model: undefined,
    choices: { items: CHOICE_DELTA },
    usage: USAGE,
  },
};
