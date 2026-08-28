package messages

import (
	"fmt"

	"github.com/oxa-protocol/oxa/go/ir"
)

// defaultMaxTokens is applied when an IR request carries no Params.MaxTokens
// and the Anthropic Messages API's required max_tokens must still be emitted.
// 4096 is the value the Anthropic docs recommend as the default maximum token
// count ("Set max_tokens", docs.claude.com; the docs' own examples use 4096).
const defaultMaxTokens = 4096

// EncodeRequest converts an IR request to an Anthropic Messages wire request
// (IR -> face). ir.System renders as the system block array; params map back
// to temperature/top_p/stop_sequences; max_tokens is required on the wire, so
// a missing Params.MaxTokens is filled with defaultMaxTokens and recorded as
// a degraded loss naming the default (spec/03 s3).
//
// Message content rendering follows the from-ir rendering defaults pinned by
// the seed vectors (vectors/README.md): the string shorthand is used only
// for a request whose entire conversation is a single message of a single
// text block and that carries no system prompt; every other request renders
// block arrays.
func EncodeRequest(req *ir.Request, opts ...Option) (*Request, []ir.Loss, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("anthropic: nil request")
	}
	var losses []ir.Loss
	if len(req.Tools) > 0 || req.ToolChoice != nil {
		return nil, nil, fmt.Errorf("anthropic: tool requests are not supported in this milestone")
	}

	if len(req.Metadata) > 0 {
		losses = append(losses, ir.Loss{
			Path:   "metadata",
			Field:  "metadata",
			Reason: ir.LossUnmappedField,
			Detail: "Anthropic request metadata is the user_id semantic, not an arbitrary string map; the IR metadata map is dropped.",
		})
	}
	o := newOptions(opts...)
	out := &Request{Model: o.models.Map(req.Model)}
	if len(req.System) > 0 {
		blocks := make([]SystemBlockWire, 0, len(req.System))
		for _, s := range req.System {
			blocks = append(blocks, SystemBlockWire{Type: "text", Text: s.Text})
		}
		out.System = blocks
	}

	shorthand := len(req.System) == 0 && len(req.Messages) == 1 &&
		len(req.Messages[0].Content) == 1
	for i, m := range req.Messages {
		var role string
		switch m.Role {
		case ir.RoleUser:
			role = "user"
		case ir.RoleAssistant:
			role = "assistant"
		default:
			return nil, nil, fmt.Errorf("anthropic: messages[%d]: unknown role %q", i, m.Role)
		}
		blocks := make([]BlockWire, 0, len(m.Content))
		text := ""
		for _, b := range m.Content {
			t, ok := b.(ir.TextBlock)
			if !ok {
				return nil, nil, fmt.Errorf("anthropic: messages[%d]: non-text blocks are not supported in this milestone", i)
			}
			text += t.Text
			blocks = append(blocks, BlockWire{Type: "text", Text: t.Text})
		}
		if shorthand {
			out.Messages = append(out.Messages, Message{Role: role, Content: text})
		} else {
			out.Messages = append(out.Messages, Message{Role: role, Content: blocks})
		}
	}

	if req.Params.MaxTokens != nil {
		out.MaxTokens = *req.Params.MaxTokens
	} else {
		out.MaxTokens = defaultMaxTokens
		losses = append(losses, ir.Loss{
			Path:   "params",
			Field:  "max_tokens",
			Reason: ir.LossDegraded,
			Detail: fmt.Sprintf("Anthropic Messages requires max_tokens; defaulting to %d", defaultMaxTokens),
		})
	}
	out.Temperature = req.Params.Temperature
	out.TopP = req.Params.TopP
	if len(req.Params.StopSequences) > 0 {
		out.StopSequences = req.Params.StopSequences
	}
	return out, losses, nil
}

// EncodeResponse converts an IR response to an Anthropic Messages wire
// response (IR -> face). Near-identity; the envelope fields type ("message")
// and role ("assistant") are rendered defaults and record no loss
// (vectors/README.md "From-ir rendering defaults").
func EncodeResponse(resp *ir.Response, opts ...Option) (*Response, []ir.Loss, error) {
	if resp == nil {
		return nil, nil, fmt.Errorf("anthropic: nil response")
	}
	if resp.StopReason == ir.StopToolUse {
		return nil, nil, fmt.Errorf("anthropic: tool_use stop reason is not reachable in this milestone")
	}
	o := newOptions(opts...)
	out := &Response{
		ID:    resp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: o.models.Map(resp.Model),
		Usage: &UsageWire{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	}
	for i, b := range resp.Content {
		t, ok := b.(ir.TextBlock)
		if !ok {
			return nil, nil, fmt.Errorf("anthropic: content[%d]: non-text blocks are not supported in this milestone", i)
		}
		out.Content = append(out.Content, BlockWire{Type: "text", Text: t.Text})
	}
	switch resp.StopReason {
	case ir.StopEndTurn:
		out.StopReason = "end_turn"
	case ir.StopMaxTokens:
		out.StopReason = "max_tokens"
	case ir.StopSequence:
		out.StopReason = "stop_sequence"
		out.StopSequence = resp.StopSequence
	case ir.StopRefusal:
		out.StopReason = "refusal"
	default:
		return nil, nil, fmt.Errorf("anthropic: stop reason %q has no Anthropic equivalent", resp.StopReason)
	}
	return out, nil, nil
}
