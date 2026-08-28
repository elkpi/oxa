package chatcompletions

import (
	"fmt"

	"github.com/oxa-protocol/oxa/go/ir"
)

// EncodeRequest converts an IR request to a Chat Completions wire request
// (IR -> face). ir.System renders as one leading system message; message
// content renders as a plain string (text blocks concatenate, this milestone
// is text-only); Params map back to temperature/top_p/max_tokens/stop.
func EncodeRequest(req *ir.Request, opts ...Option) (*Request, []ir.Loss, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil request")
	}
	var losses []ir.Loss
	if len(req.Tools) > 0 || req.ToolChoice != nil {
		return nil, nil, fmt.Errorf("chatcompletions: tool requests are not supported in this milestone")
	}
	if len(req.Metadata) > 0 {
		losses = append(losses, ir.Loss{
			Path:   "metadata",
			Field:  "metadata",
			Reason: ir.LossUnmappedField,
			Detail: "Chat Completions requests have no metadata field; the IR metadata map is dropped.",
		})
	}
	o := newOptions(opts...)
	out := &Request{Model: o.models.Map(req.Model)}
	if len(req.System) > 0 {
		text := ""
		for _, s := range req.System {
			text += s.Text
		}
		out.Messages = append(out.Messages, Message{Role: "system", Content: text})
	}
	for i, m := range req.Messages {
		role, err := encodeRole(m.Role, i)
		if err != nil {
			return nil, nil, err
		}
		text := ""
		for _, b := range m.Content {
			t, ok := b.(ir.TextBlock)
			if !ok {
				return nil, nil, fmt.Errorf("chatcompletions: messages[%d]: non-text blocks are not supported in this milestone", i)
			}
			text += t.Text
		}
		out.Messages = append(out.Messages, Message{Role: role, Content: text})
	}
	if len(req.Params.StopSequences) > 0 {
		out.Stop = req.Params.StopSequences
	}
	out.Temperature = req.Params.Temperature
	out.TopP = req.Params.TopP
	out.MaxTokens = req.Params.MaxTokens
	return out, losses, nil
}

func encodeRole(role ir.Role, index int) (string, error) {
	switch role {
	case ir.RoleUser:
		return "user", nil
	case ir.RoleAssistant:
		return "assistant", nil
	default:
		return "", fmt.Errorf("chatcompletions: messages[%d]: unknown role %q", index, role)
	}
}

// EncodeResponse converts an IR response to a Chat Completions wire response
// (IR -> face). Envelope fields absent from the IR are rendered with the
// documented defaults (object "chat.completion", created 0, single choice
// index 0, message role assistant) and record no loss: this direction renders
// defaults, it drops nothing (vectors/README.md "From-ir rendering
// defaults"). usage.total_tokens is derived and recomputed.
func EncodeResponse(resp *ir.Response, opts ...Option) (*Response, []ir.Loss, error) {
	if resp == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil response")
	}
	var losses []ir.Loss
	if resp.StopReason == ir.StopToolUse {
		return nil, nil, fmt.Errorf("chatcompletions: tool_use stop reason is not reachable in this milestone")
	}
	text := ""
	for _, b := range resp.Content {
		t, ok := b.(ir.TextBlock)
		if !ok {
			return nil, nil, fmt.Errorf("chatcompletions: non-text blocks are not supported in this milestone")
		}
		text += t.Text
	}
	finish := ""
	switch resp.StopReason {
	case ir.StopEndTurn:
		finish = "stop"
	case ir.StopMaxTokens:
		finish = "length"
	case ir.StopRefusal:
		finish = "content_filter"
	case ir.StopSequence:
		// Chat Completions reports only finish_reason "stop" without
		// identifying which stop sequence matched, so the sequence value is
		// lost (spec/01 s4.1 note).
		finish = "stop"
		losses = append(losses, ir.Loss{
			Path:   "",
			Field:  "stop_sequence",
			Reason: ir.LossUnmappedValue,
			Detail: "Chat Completions finish_reason \"stop\" does not identify the matched stop sequence",
		})
	default:
		return nil, nil, fmt.Errorf("chatcompletions: stop reason %q has no Chat Completions equivalent", resp.StopReason)
	}
	o := newOptions(opts...)
	return &Response{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: 0,
		Model:   o.models.Map(resp.Model),
		Choices: []Choice{{
			Index:        0,
			Message:      Message{Role: "assistant", Content: text},
			FinishReason: finish,
		}},
		Usage: &UsageWire{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}, losses, nil
}
