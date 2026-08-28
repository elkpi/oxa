package messages

import (
	"encoding/json"
	"fmt"

	"github.com/oxa-protocol/oxa/go/ir"
)

// DecodeRequest converts an Anthropic Messages wire request to the IR (face
// -> IR). The mapping is near-identity: system (string or block array) becomes
// ir.System, required max_tokens becomes Params.MaxTokens, and
// temperature/top_p/stop_sequences map 1:1. cache_control annotations have no
// IR equivalent in v1 and are dropped with unmapped-field losses.
func DecodeRequest(wire *Request) (*ir.Request, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("anthropic: nil request")
	}
	var losses []ir.Loss
	if wire.MaxTokens <= 0 {
		return nil, nil, fmt.Errorf("anthropic: max_tokens is required and must be positive")
	}
	maxTokens := wire.MaxTokens
	req := &ir.Request{
		Model:  defaultOptions.models.Map(wire.Model),
		Params: ir.Params{MaxTokens: &maxTokens},
	}

	system, sysLosses, err := decodeSystem(wire.System)
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, sysLosses...)
	req.System = system

	for i, m := range wire.Messages {
		var role ir.Role
		switch m.Role {
		case "user":
			role = ir.RoleUser
		case "assistant":
			role = ir.RoleAssistant
		default:
			return nil, nil, fmt.Errorf("anthropic: messages[%d]: unknown role %q", i, m.Role)
		}
		blocks, blockLosses, err := decodeBlocks(m.Content, fmt.Sprintf("messages[%d].content", i))
		if err != nil {
			return nil, nil, err
		}
		losses = append(losses, blockLosses...)
		req.Messages = append(req.Messages, ir.Message{Role: role, Content: blocks})
	}
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("anthropic: request carries no messages")
	}

	if wire.Temperature != nil {
		req.Params.Temperature = wire.Temperature
	}
	if wire.TopP != nil {
		req.Params.TopP = wire.TopP
	}
	if len(wire.StopSequences) > 0 {
		req.Params.StopSequences = wire.StopSequences
	}
	if md, ok := wire.Metadata.(map[string]any); ok {
		if _, present := md["user_id"]; present {
			losses = append(losses, ir.Loss{
				Path:   "metadata.user_id",
				Field:  "user_id",
				Reason: ir.LossUnmappedField,
				Detail: "Anthropic metadata.user_id has no IR equivalent in v1.",
			})
		}
	}
	return req, losses, nil
}

func decodeSystem(system any) ([]ir.SystemBlock, []ir.Loss, error) {
	if system == nil {
		return nil, nil, nil
	}
	switch v := system.(type) {
	case string:
		return []ir.SystemBlock{{Text: v}}, nil, nil
	case []any:
		blocks := make([]ir.SystemBlock, 0, len(v))
		var losses []ir.Loss
		for i, part := range v {
			raw, err := json.Marshal(part)
			if err != nil {
				return nil, nil, err
			}
			var w SystemBlockWire
			if err := json.Unmarshal(raw, &w); err != nil {
				return nil, nil, fmt.Errorf("anthropic: system[%d]: %w", i, err)
			}
			if w.Type != "text" {
				return nil, nil, fmt.Errorf("anthropic: system[%d]: unsupported block type %q", i, w.Type)
			}
			blocks = append(blocks, ir.SystemBlock{Text: w.Text})
			if len(w.CacheControl) > 0 {
				losses = append(losses, ir.Loss{
					Path:   fmt.Sprintf("system[%d].cache_control", i),
					Field:  "cache_control",
					Reason: ir.LossUnmappedField,
					Detail: "Anthropic prompt caching annotations have no IR equivalent in v1.",
				})
			}
		}
		return blocks, losses, nil
	default:
		return nil, nil, fmt.Errorf("anthropic: system is neither string nor block array")
	}
}

func decodeBlocks(content any, path string) ([]ir.Block, []ir.Loss, error) {
	switch v := content.(type) {
	case nil:
		return nil, nil, fmt.Errorf("anthropic: %s is missing", path)
	case string:
		return []ir.Block{ir.TextBlock{Text: v}}, nil, nil
	case []any:
		blocks := make([]ir.Block, 0, len(v))
		var losses []ir.Loss
		for j, part := range v {
			raw, err := json.Marshal(part)
			if err != nil {
				return nil, nil, err
			}
			var w BlockWire
			if err := json.Unmarshal(raw, &w); err != nil {
				return nil, nil, fmt.Errorf("anthropic: %s[%d]: %w", path, j, err)
			}
			if w.Type != "text" {
				return nil, nil, fmt.Errorf("anthropic: %s[%d]: unsupported block type %q (later milestone)", path, j, w.Type)
			}
			blocks = append(blocks, ir.TextBlock{Text: w.Text})
			if len(w.CacheControl) > 0 {
				losses = append(losses, ir.Loss{
					Path:   fmt.Sprintf("%s[%d].cache_control", path, j),
					Field:  "cache_control",
					Reason: ir.LossUnmappedField,
					Detail: "Anthropic prompt caching annotations have no IR equivalent in v1.",
				})
			}
		}
		return blocks, losses, nil
	default:
		return nil, nil, fmt.Errorf("anthropic: %s is neither string nor block array", path)
	}
}

// DecodeResponse converts an Anthropic Messages wire response to the IR
// (face -> IR). Near-identity: text blocks map 1:1, stop_reason maps by
// value (stop_sequence also carries the matched sequence), usage maps 1:1.
// The envelope fields type and role are exempt from losses
// (vectors/README.md loss conventions).
func DecodeResponse(wire *Response) (*ir.Response, []ir.Loss, error) {
	if wire == nil {
		return nil, nil, fmt.Errorf("anthropic: nil response")
	}
	var losses []ir.Loss
	resp := &ir.Response{
		ID:    wire.ID,
		Model: defaultOptions.models.Map(wire.Model),
	}
	for i, b := range wire.Content {
		if b.Type != "text" {
			return nil, nil, fmt.Errorf("anthropic: content[%d]: unsupported block type %q (later milestone)", i, b.Type)
		}
		resp.Content = append(resp.Content, ir.TextBlock{Text: b.Text})
	}
	stop, loss, err := decodeStopReason(wire.StopReason, wire.StopSequence)
	if err != nil {
		return nil, nil, err
	}
	if loss != nil {
		losses = append(losses, *loss)
	}
	resp.StopReason = stop
	resp.StopSequence = wire.StopSequence
	if wire.Usage != nil {
		resp.Usage = ir.Usage{
			InputTokens:  wire.Usage.InputTokens,
			OutputTokens: wire.Usage.OutputTokens,
		}
	}
	return resp, losses, nil
}

func decodeStopReason(stop, stopSequence string) (ir.StopReason, *ir.Loss, error) {
	switch stop {
	case "end_turn":
		return ir.StopEndTurn, nil, nil
	case "max_tokens":
		return ir.StopMaxTokens, nil, nil
	case "stop_sequence":
		return ir.StopSequence, nil, nil
	case "tool_use":
		return ir.StopToolUse, nil, nil
	case "refusal":
		return ir.StopRefusal, nil, nil
	case "":
		return "", nil, fmt.Errorf("anthropic: stop_reason is missing")
	default:
		return ir.StopOther, &ir.Loss{
			Path:   "stop_reason",
			Field:  "stop_reason",
			Reason: ir.LossUnmappedValue,
			Detail: fmt.Sprintf("Anthropic stop_reason %q has no IR equivalent", stop),
		}, nil
	}
}
