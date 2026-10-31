package chatcompletions

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

// DecodeRequest converts a Chat Completions wire request to the IR (face ->
// IR). Semantic unmappables are losses, never errors; errors are reserved for
// structural type violations of known fields (spec/02 s4).
func DecodeRequest(wire *Request, opts ...Option) (*ir.Request, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil request")
	}
	var losses []ir.Loss
	for _, field := range []struct {
		name    string
		present bool
		detail  string
	}{
		{"parallel_tool_calls", wire.ParallelToolCalls != nil, "Chat Completions parallel tool calls have no IR equivalent in v1."},
		{"functions", wire.Functions != nil, "legacy Chat Completions functions have no IR equivalent in v1."},
		{"function_call", wire.FunctionCall != nil, "legacy Chat Completions function_call has no IR equivalent in v1."},
		{"logprobs", wire.Logprobs != nil, "Chat Completions log-probability sampling has no IR equivalent in v1."},
		{"top_logprobs", wire.TopLogprobs != nil, "Chat Completions log-probability sampling has no IR equivalent in v1."},
	} {
		if field.present {
			losses = append(losses, loss(field.name, field.name, ir.LossUnmappedField, field.detail))
		}
	}

	o := newOptions(opts...)
	metadata, err := openAIMetadata(wire.Metadata)
	if err != nil {
		return nil, nil, err
	}
	req := &ir.Request{Model: o.models.Map(wire.Model), Metadata: metadata}
	for i, tool := range wire.Tools {
		if tool.Type == ir.ToolTypeCustom {
			if tool.Custom == nil || tool.Custom.Name == "" {
				return nil, nil, fmt.Errorf("chatcompletions: custom tool requires custom.name")
			}
			var format *ir.CustomToolFormat
			if tool.Custom.Format != nil {
				switch tool.Custom.Format.Type {
				case "text":
					format = &ir.CustomToolFormat{Type: "text"}
				case "grammar":
					if tool.Custom.Format.Syntax == "lark" || tool.Custom.Format.Syntax == "regex" {
						format = tool.Custom.Format
					} else {
						losses = append(losses, loss(
							fmt.Sprintf("tools[%d].custom.format", i), "syntax", ir.LossUnsupportedSemantic,
							fmt.Sprintf("unknown custom grammar syntax %q", tool.Custom.Format.Syntax),
						))
					}
				default:
					losses = append(losses, loss(
						fmt.Sprintf("tools[%d].custom.format", i), "type", ir.LossUnsupportedSemantic,
						fmt.Sprintf("unknown custom format %q", tool.Custom.Format.Type),
					))
				}
			}
			req.Tools = append(req.Tools, ir.Tool{
				Type:        ir.ToolTypeCustom,
				Name:        tool.Custom.Name,
				Description: tool.Custom.Description,
				Format:      format,
			})
			continue
		}
		if tool.Type != ToolTypeFunction {
			losses = append(losses, loss(
				fmt.Sprintf("tools[%d]", i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Chat Completions tool type %q has no IR equivalent", tool.Type),
			))
			continue
		}
		if tool.Function.Name == "" {
			return nil, nil, fmt.Errorf("chatcompletions: function tool requires function.name")
		}
		req.Tools = append(req.Tools, ir.Tool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
			Strict:      tool.Function.Strict,
		})
	}
	choice, choiceLosses := decodeToolChoice(wire.ToolChoice)
	req.ToolChoice = choice
	losses = append(losses, choiceLosses...)

	customCallIDs := make(map[string]bool)
	for _, m := range wire.Messages {
		if m.Role == RoleAssistant {
			for _, tc := range m.ToolCalls {
				if tc.Type == ir.ToolTypeCustom {
					customCallIDs[tc.ID] = true
				}
			}
		}
	}

	for i := 0; i < len(wire.Messages); {
		message := wire.Messages[i]
		if message.Role == RoleTool {
			merged, next, resultLosses, err := decodeToolResultRun(wire.Messages, i, customCallIDs)
			if err != nil {
				return nil, nil, err
			}
			req.Messages = append(req.Messages, merged)
			losses = append(losses, resultLosses...)
			i = next
			continue
		}

		content, contentLosses, err := decodeContent(message.Content, fmt.Sprintf("messages[%d].content", i), message.Role == RoleUser)
		if err != nil {
			return nil, nil, err
		}
		losses = append(losses, contentLosses...)
		if message.Role != RoleSystem && len(content) == 0 {
			// IR conversation messages cannot have empty content (spec/01 s3.3).
			content = []ir.Block{ir.TextBlock{Text: ""}}
		}
		switch message.Role {
		case RoleSystem, RoleDeveloper:
			system, systemLosses := contentSystem(content, fmt.Sprintf("messages[%d].content", i))
			if message.Role == RoleDeveloper {
				for j := range system {
					system[j].Role = ir.SystemRoleDeveloper
				}
			}
			req.System = append(req.System, system...)
			losses = append(losses, systemLosses...)
		case RoleUser:
			req.Messages = append(req.Messages, ir.Message{Role: ir.RoleUser, Content: content})
		case RoleAssistant:
			toolCalls, toolLosses := decodeToolCalls(message.ToolCalls, fmt.Sprintf("messages[%d].tool_calls", i))
			if message.Content == nil && len(toolCalls) > 0 {
				// A tool-only assistant message has no normal content to prepend.
				content = nil
			}
			if message.ReasoningContent != "" {
				content = append([]ir.Block{ir.ThinkingBlock{Thinking: message.ReasoningContent}}, content...)
			}
			content = append(content, toolCalls...)
			req.Messages = append(req.Messages, ir.Message{Role: ir.RoleAssistant, Content: content})
			losses = append(losses, toolLosses...)
		default:
			return nil, nil, fmt.Errorf("chatcompletions: messages[%d]: unknown role %q", i, message.Role)
		}
		if message.FunctionCall != nil {
			losses = append(losses, loss(
				fmt.Sprintf("messages[%d].function_call", i), "function_call", ir.LossUnmappedField,
				"legacy Chat Completions function_call has no IR equivalent",
			))
		}
		i++
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("chatcompletions: request carries no conversation messages")
	}
	if wire.MaxTokens != nil && wire.MaxCompletionTokens != nil {
		return nil, nil, fmt.Errorf("chatcompletions: cannot specify both max_tokens and max_completion_tokens")
	}
	if wire.MaxCompletionTokens != nil && *wire.MaxCompletionTokens <= 0 {
		return nil, nil, fmt.Errorf("chatcompletions: max_completion_tokens must be a positive integer")
	}
	req.Params = ir.Params{
		Temperature:   wire.Temperature,
		TopP:          wire.TopP,
		MaxTokens:     wire.MaxTokens,
		StopSequences: wire.Stop,
	}
	if wire.MaxCompletionTokens != nil {
		req.Params.MaxTokens = wire.MaxCompletionTokens
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source: ir.NativeSourceOpenAI,
			Kind:   ir.NativeKindMaxCompletionTokens,
			Value:  "true",
		})
	}
	if len(wire.rawStore) > 0 {
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source: ir.NativeSourceOpenAI,
			Kind:   ir.NativeKindStore,
			Value:  strings.TrimSpace(string(wire.rawStore)),
		})
	} else if wire.Store != nil {
		val := "false"
		if *wire.Store {
			val = "true"
		}
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source: ir.NativeSourceOpenAI,
			Kind:   ir.NativeKindStore,
			Value:  val,
		})
	}
	if len(wire.rawStream) > 0 {
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source: ir.NativeSourceOpenAI,
			Kind:   ir.NativeKindStream,
			Value:  strings.TrimSpace(string(wire.rawStream)),
		})
	} else if wire.Stream != nil {
		val := "false"
		if *wire.Stream {
			val = "true"
		}
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source: ir.NativeSourceOpenAI,
			Kind:   ir.NativeKindStream,
			Value:  val,
		})
	}
	if len(wire.rawStreamOptions) > 0 && strings.TrimSpace(string(wire.rawStreamOptions)) == "null" {
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source: ir.NativeSourceOpenAI,
			Kind:   ir.NativeKindStreamOptions,
			Value:  "null",
		})
	} else if wire.StreamOptions != nil {
		b, err := json.Marshal(wire.StreamOptions)
		if err != nil {
			return nil, nil, fmt.Errorf("chatcompletions: stream_options: %w", err)
		}
		var om *ir.NativeOmission
		if wire.StreamOptions.IncludeUsage != nil && *wire.StreamOptions.IncludeUsage {
			om = &ir.NativeOmission{
				Field:  "stream_options",
				Reason: ir.LossDegraded,
				Detail: ir.StreamOptionsLossDetail,
			}
		}
		req.Native = append(req.Native, ir.NativeAnnotation{
			Source:   ir.NativeSourceOpenAI,
			Kind:     ir.NativeKindStreamOptions,
			Value:    string(b),
			Omission: om,
		})
	}
	if wire.Verbosity != nil {
		switch *wire.Verbosity {
		case "low", "medium", "high":
			req.Params.Verbosity = *wire.Verbosity
		default:
			losses = append(losses, loss("verbosity", "verbosity", ir.LossUnmappedValue, "unknown output verbosity"))
		}
	}
	switch wire.ReasoningEffort {
	case "", ir.ReasoningEffortMinimal, ir.ReasoningEffortLow, ir.ReasoningEffortMedium, ir.ReasoningEffortHigh, ir.ReasoningEffortXHigh, ir.ReasoningEffortMax:
		req.Params.ReasoningEffort = wire.ReasoningEffort
	default:
		losses = append(losses, loss(
			"reasoning_effort", "reasoning_effort", ir.LossUnmappedValue,
			fmt.Sprintf("unknown reasoning_effort value %q", wire.ReasoningEffort),
		))
	}
	if wire.ResponseFormat != nil {
		rf, rfLosses, err := decodeResponseFormat(wire.ResponseFormat)
		if err != nil {
			return nil, nil, err
		}
		losses = append(losses, rfLosses...)
		req.Params.ResponseFormat = rf
	}
	return req, losses, nil
}

func decodeResponseFormat(raw any) (*ir.ResponseFormat, []ir.Loss, error) {
	if raw == nil {
		return nil, nil, nil
	}
	var wire ResponseFormatWire
	switch v := raw.(type) {
	case *ResponseFormatWire:
		if v == nil {
			return nil, nil, nil
		}
		wire = *v
	case ResponseFormatWire:
		wire = v
	case json.RawMessage:
		if err := json.Unmarshal(v, &wire); err != nil {
			return nil, nil, fmt.Errorf("chatcompletions: response_format: %w", err)
		}
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("chatcompletions: response_format: %w", err)
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, nil, fmt.Errorf("chatcompletions: response_format: %w", err)
		}
	}

	switch wire.Type {
	case ir.ResponseFormatText:
		return &ir.ResponseFormat{Type: ir.ResponseFormatText}, nil, nil
	case ir.ResponseFormatJSONObject:
		return &ir.ResponseFormat{Type: ir.ResponseFormatJSONObject}, nil, nil
	case ir.ResponseFormatJSONSchema:
		if wire.JSONSchema == nil {
			return nil, nil, fmt.Errorf("chatcompletions: response_format of type json_schema requires json_schema object")
		}
		if wire.JSONSchema.Name == "" {
			return nil, nil, fmt.Errorf("chatcompletions: response_format.json_schema requires non-empty name")
		}
		if len(wire.JSONSchema.Schema) == 0 {
			return nil, nil, fmt.Errorf("chatcompletions: response_format.json_schema requires schema")
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(wire.JSONSchema.Schema, &obj); err != nil || obj == nil {
			return nil, nil, fmt.Errorf("chatcompletions: response_format.json_schema schema must be a JSON object")
		}
		return &ir.ResponseFormat{
			Type:        ir.ResponseFormatJSONSchema,
			Name:        wire.JSONSchema.Name,
			Description: wire.JSONSchema.Description,
			Schema:      wire.JSONSchema.Schema,
			Strict:      wire.JSONSchema.Strict,
		}, nil, nil
	case "":
		return nil, nil, fmt.Errorf("chatcompletions: response_format requires non-empty type")
	default:
		return nil, []ir.Loss{
			loss("response_format.type", "type", ir.LossUnmappedValue,
				fmt.Sprintf("unknown response_format type %q", wire.Type)),
		}, nil
	}
}

// DecodeResponse converts a Chat Completions wire response to the IR (face ->
// IR). Envelope fields (object, created, choices[].index, message.role) are
// exempt from losses (vectors/README.md loss conventions); usage.total_tokens
// is derived. Unknown finish_reason values map to other plus an unmapped-value
// loss (spec/01 s4.1).
func DecodeResponse(wire *Response, opts ...Option) (*ir.Response, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil response")
	}
	if len(wire.Choices) == 0 {
		return nil, nil, fmt.Errorf("chatcompletions: response carries no choices")
	}
	var losses []ir.Loss
	choice := wire.Choices[0]
	blocks, contentLosses, err := decodeContent(choice.Message.Content, "choices[0].message.content", false)
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, contentLosses...)
	toolCalls, toolLosses := decodeToolCalls(choice.Message.ToolCalls, "choices[0].message.tool_calls")
	if choice.Message.Content == nil && len(toolCalls) > 0 {
		// A tool-only assistant response has no normal content to prepend.
		blocks = nil
	}
	blocks = append(blocks, toolCalls...)
	if choice.Message.ReasoningContent != "" {
		blocks = append([]ir.Block{ir.ThinkingBlock{Thinking: choice.Message.ReasoningContent}}, blocks...)
	}
	losses = append(losses, toolLosses...)
	if choice.Message.FunctionCall != nil {
		losses = append(losses, loss(
			"choices[0].message.function_call", "function_call", ir.LossUnmappedField,
			"legacy Chat Completions function_call has no IR equivalent",
		))
	}
	stop, finishLoss, err := decodeFinishReason(choice.FinishReason)
	if err != nil {
		return nil, nil, err
	}
	if finishLoss != nil {
		losses = append(losses, *finishLoss)
	}
	o := newOptions(opts...)
	resp := &ir.Response{
		ID:         wire.ID,
		Model:      o.models.Map(wire.Model),
		Content:    blocks,
		StopReason: stop,
	}
	if wire.Usage != nil {
		resp.Usage = ir.Usage{
			InputTokens:  wire.Usage.PromptTokens,
			OutputTokens: wire.Usage.CompletionTokens,
		}
		if wire.Usage.PromptTokensDetails != nil {
			cachedTokens := wire.Usage.PromptTokensDetails.CachedTokens
			resp.Usage.InputTokensDetails = &ir.InputTokensDetails{
				CachedTokens: &cachedTokens,
			}
		}
		if wire.Usage.CompletionTokensDetails != nil {
			reasoningTokens := wire.Usage.CompletionTokensDetails.ReasoningTokens
			resp.Usage.OutputTokensDetails = &ir.OutputTokensDetails{
				ReasoningTokens: &reasoningTokens,
			}
		}
	}
	return resp, losses, nil
}

func decodeFinishReason(finish string) (ir.StopReason, *ir.Loss, error) {
	switch finish {
	case FinishReasonStop:
		return ir.StopEndTurn, nil, nil
	case FinishReasonLength:
		return ir.StopMaxTokens, nil, nil
	case FinishReasonContentFilter:
		return ir.StopRefusal, nil, nil
	case FinishReasonToolCalls:
		return ir.StopToolUse, nil, nil
	case "":
		return "", nil, fmt.Errorf("chatcompletions: choices[0].finish_reason is missing")
	default:
		return ir.StopOther, &ir.Loss{
			Path:   "choices[0].finish_reason",
			Field:  "finish_reason",
			Reason: ir.LossUnmappedValue,
			Detail: fmt.Sprintf("Chat Completions finish_reason %q has no IR equivalent", finish),
		}, nil
	}
}

func openAIMetadata(v any) (map[string]string, error) {
	if v == nil {
		return nil, nil
	}
	if metadata, ok := v.(map[string]string); ok {
		if len(metadata) == 0 {
			return nil, nil
		}
		out := make(map[string]string, len(metadata))
		for key, value := range metadata {
			if key == "" {
				return nil, fmt.Errorf("chatcompletions: metadata key cannot be empty")
			}
			out[key] = value
		}
		return out, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("chatcompletions: metadata must be an object")
	}
	if len(obj) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if key == "" {
			return nil, fmt.Errorf("chatcompletions: metadata key cannot be empty")
		}
		value, ok := obj[key].(string)
		if !ok {
			return nil, fmt.Errorf("chatcompletions: metadata.%s must be a string", key)
		}
		out[key] = value
	}
	return out, nil
}
