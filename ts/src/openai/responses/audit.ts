import type { WireAuditSpec } from "../../json/wire-audit.js";

const SUMMARY_PART: WireAuditSpec = {
  variant: (part) => {
    if (part.type === "output_text" || part.type === "summary_text")
      return {
        fields: {
          type: undefined,
          text: undefined,
          annotations: { items: undefined },
        },
      };
    return undefined;
  },
};

const INPUT_PART: WireAuditSpec = {
  variant: (part) => {
    if (part.type === "input_text")
      return { fields: { type: undefined, text: undefined } };
    if (part.type === "input_image")
      return {
        fields: { type: undefined, image_url: undefined, detail: undefined },
      };
    return undefined;
  },
};

const INPUT_CONTENT: WireAuditSpec = { items: INPUT_PART };

const APPLY_PATCH_OPERATION: WireAuditSpec = {
  variant: (op) => {
    switch (op.type) {
      case "create_file":
      case "update_file":
        return {
          fields: {
            type: undefined,
            path: undefined,
            diff: undefined,
          },
        };
      case "delete_file":
        return {
          fields: {
            type: undefined,
            path: undefined,
          },
        };
      default:
        return undefined;
    }
  },
};

const INPUT_ITEM: WireAuditSpec = {
  variant: (item) => {
    const type = item.type === undefined ? "message" : item.type;
    switch (type) {
      case "message":
        return {
          fields: { type: undefined, role: undefined, content: INPUT_CONTENT },
        };
      case "apply_patch_call":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            call_id: undefined,
            operation: APPLY_PATCH_OPERATION,
          },
        };
      case "apply_patch_call_output":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            call_id: undefined,
            output: undefined,
          },
        };
      case "function_call":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            call_id: undefined,
            name: undefined,
            arguments: undefined,
          },
        };
      case "function_call_output":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            call_id: undefined,
            output: undefined,
          },
        };
      case "custom_tool_call":
        return {
          fields: {
            type: undefined,
            call_id: undefined,
            name: undefined,
            input: undefined,
          },
        };
      case "custom_tool_call_output":
        return {
          fields: {
            type: undefined,
            call_id: undefined,
            output: { items: INPUT_PART },
          },
        };
      case "reasoning":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            summary: { items: SUMMARY_PART },
          },
        };
      default:
        return undefined;
    }
  },
};

const CUSTOM_FORMAT: WireAuditSpec = {
  variant: (format) =>
    format.type === "text"
      ? { fields: { type: undefined } }
      : format.type === "grammar"
        ? {
            fields: {
              type: undefined,
              syntax: undefined,
              definition: undefined,
            },
          }
        : undefined,
};

const TOOL: WireAuditSpec = {
  variant: (tool) =>
    tool.type === "apply_patch"
      ? {
          fields: {
            type: undefined,
          },
        }
      : tool.type === "function"
        ? {
            fields: {
              type: undefined,
              name: undefined,
              description: undefined,
              parameters: undefined,
            },
          }
        : tool.type === "custom"
          ? {
              fields: {
                type: undefined,
                name: undefined,
                description: undefined,
                format: CUSTOM_FORMAT,
              },
            }
          : undefined,
};

const TOOL_CHOICE: WireAuditSpec = {
  variant: (choice) => {
    if (choice.type === "apply_patch") return { fields: { type: undefined } };
    if (choice.type === "function" || choice.type === "custom")
      return { fields: { type: undefined, name: undefined } };
    return undefined;
  },
};

const FORMAT: WireAuditSpec = {
  variant: (format) => {
    if (format === null || typeof format !== "object" || Array.isArray(format))
      return undefined;
    return {
      fields: {
        type: undefined,
        name: undefined,
        description: undefined,
        schema: undefined,
        strict: undefined,
      },
    };
  },
};

const RESPONSE_PART: WireAuditSpec = {
  variant: (part) => {
    if (part.type === "input_text")
      return { fields: { type: undefined, text: undefined } };
    if (part.type === "input_image")
      return {
        fields: { type: undefined, image_url: undefined, detail: undefined },
      };
    if (part.type !== "output_text") return undefined;
    return {
      fields: {
        type: undefined,
        text: undefined,
        annotations: { items: undefined },
      },
    };
  },
};

const OUTPUT_ITEM: WireAuditSpec = {
  variant: (item) => {
    switch (item.type) {
      case "apply_patch_call":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            call_id: undefined,
            operation: APPLY_PATCH_OPERATION,
          },
        };
      case "apply_patch_call_output":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            call_id: undefined,
            output: undefined,
          },
        };
      case "message":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            role: undefined,
            content: { items: RESPONSE_PART },
          },
        };
      case "function_call":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            call_id: undefined,
            name: undefined,
            arguments: undefined,
          },
        };
      case "custom_tool_call":
        return {
          fields: {
            type: undefined,
            id: undefined,
            call_id: undefined,
            name: undefined,
            input: undefined,
          },
        };
      case "custom_tool_call_output":
        return {
          fields: {
            type: undefined,
            id: undefined,
            call_id: undefined,
            output: { items: INPUT_PART },
          },
        };
      case "reasoning":
        return {
          fields: {
            type: undefined,
            id: undefined,
            status: undefined,
            summary: { items: SUMMARY_PART },
          },
        };
      default:
        return undefined;
    }
  },
};

const USAGE: WireAuditSpec = {
  fields: {
    input_tokens: undefined,
    output_tokens: undefined,
    total_tokens: undefined,
    input_token_details: { fields: { cached_tokens: undefined } },
    output_token_details: { fields: { reasoning_tokens: undefined } },
  },
};

const RESPONSE: WireAuditSpec = {
  fields: {
    id: undefined,
    object: undefined,
    status: undefined,
    model: undefined,
    output: { items: OUTPUT_ITEM },
    usage: USAGE,
    incomplete_details: { fields: { reason: undefined } },
    error: { fields: { code: undefined, message: undefined } },
  },
};

export const RESPONSES_REQUEST: WireAuditSpec = {
  fields: {
    model: undefined,
    input: { items: INPUT_ITEM },
    instructions: undefined,
    temperature: undefined,
    top_p: undefined,
    max_output_tokens: undefined,
    tools: { items: TOOL },
    tool_choice: TOOL_CHOICE,
    text: { fields: { format: FORMAT } },
    reasoning: { fields: { effort: undefined, summary: undefined } },
  },
};

export const RESPONSES_RESPONSE: WireAuditSpec = RESPONSE;

const INDEXED_FIELDS = {
  type: undefined,
  item_id: undefined,
  output_index: undefined,
  content_index: undefined,
  sequence_number: undefined,
} as const;

export const RESPONSES_STREAM_EVENT: WireAuditSpec = {
  variant: (event) => {
    switch (event.type) {
      case "response.created":
      case "response.completed":
      case "response.incomplete":
      case "response.failed":
        return {
          fields: {
            type: undefined,
            response: RESPONSE,
            sequence_number: undefined,
          },
        };
      case "response.output_item.added":
      case "response.output_item.done":
        return {
          fields: {
            type: undefined,
            output_index: undefined,
            item: OUTPUT_ITEM,
            sequence_number: undefined,
          },
        };
      case "response.content_part.added":
      case "response.content_part.done":
      case "response.reasoning_summary_part.added":
      case "response.reasoning_summary_part.done":
        return {
          fields: {
            ...INDEXED_FIELDS,
            part: RESPONSE_PART,
          },
        };
      case "response.output_text.delta":
      case "response.reasoning_summary_text.delta":
        return {
          fields: {
            ...INDEXED_FIELDS,
            delta: undefined,
          },
        };
      case "response.output_text.done":
      case "response.reasoning_summary_text.done":
        return {
          fields: {
            ...INDEXED_FIELDS,
            text: undefined,
          },
        };
      case "response.custom_tool_call_input.delta":
        return {
          fields: {
            type: undefined,
            item_id: undefined,
            output_index: undefined,
            delta: undefined,
            sequence_number: undefined,
          },
        };
      case "response.custom_tool_call_input.done":
        return {
          fields: {
            type: undefined,
            item_id: undefined,
            output_index: undefined,
            input: undefined,
            sequence_number: undefined,
          },
        };
      case "response.function_call_arguments.delta":
        return {
          fields: {
            type: undefined,
            item_id: undefined,
            output_index: undefined,
            delta: undefined,
            sequence_number: undefined,
          },
        };
      case "response.function_call_arguments.done":
        return {
          fields: {
            type: undefined,
            item_id: undefined,
            output_index: undefined,
            call_id: undefined,
            name: undefined,
            arguments: undefined,
            sequence_number: undefined,
          },
        };
      default:
        return undefined;
    }
  },
};
