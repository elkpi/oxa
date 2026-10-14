import type { WireAuditSpec } from "../../json/wire-audit.js";

const PART: WireAuditSpec = {
  variant: (part) => {
    if (part.type === "text")
      return { fields: { type: undefined, text: undefined } };
    if (part.type === "image_url")
      return {
        fields: { type: undefined, image_url: { fields: { url: undefined } } },
      };
    return undefined;
  },
};

const FUNCTION: WireAuditSpec = {
  fields: {
    name: undefined,
    description: undefined,
    parameters: undefined,
    arguments: undefined,
  },
};

const TOOL_CALL: WireAuditSpec = {
  fields: { id: undefined, type: undefined, function: FUNCTION },
};

const MESSAGE: WireAuditSpec = {
  fields: {
    role: undefined,
    content: { items: PART },
    reasoning_content: undefined,
    tool_calls: { items: TOOL_CALL },
    tool_call_id: undefined,
  },
};

const TOOL: WireAuditSpec = {
  variant: (tool) => {
    if (tool.type !== "function") return undefined;
    return { fields: { type: undefined, function: FUNCTION } };
  },
};

const FUNCTION_CHOICE: WireAuditSpec = {
  fields: { name: undefined },
};

const TOOL_CHOICE: WireAuditSpec = {
  variant: (choice) => {
    if (choice.type === "function") {
      return { fields: { type: undefined, function: FUNCTION_CHOICE } };
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

export const CHAT_REQUEST: WireAuditSpec = {
  fields: {
    model: undefined,
    messages: { items: MESSAGE },
    temperature: undefined,
    top_p: undefined,
    max_tokens: undefined,
    stop: { items: undefined },
    reasoning_effort: undefined,
    tools: { items: TOOL },
    tool_choice: TOOL_CHOICE,
    response_format: RESPONSE_FORMAT,
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
    function: FUNCTION,
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
