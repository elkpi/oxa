package messages

import (
	"fmt"

	"github.com/elkpi/oxa/go/ir"
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

	for i, tool := range req.Tools {
		if err := requireJSONObject(tool.InputSchema, fmt.Sprintf("tools[%d].input_schema", i)); err != nil {
			return nil, nil, err
		}
		out.Tools = append(out.Tools, ToolWire{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	choice, choiceLosses, err := encodeToolChoice(req.ToolChoice)
	if err != nil {
		return nil, nil, err
	}
	out.ToolChoice = choice
	losses = append(losses, choiceLosses...)

	shorthand := len(req.System) == 0 && len(req.Messages) == 1 &&
		len(req.Messages[0].Content) == 1
	if shorthand {
		_, shorthand = req.Messages[0].Content[0].(ir.TextBlock)
	}
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
		blocks, blockLosses, err := encodeRequestBlocks(m.Content, fmt.Sprintf("messages[%d].content", i))
		if err != nil {
			return nil, nil, err
		}
		losses = append(losses, blockLosses...)
		if shorthand {
			out.Messages = append(out.Messages, Message{Role: role, Content: blocks[0].Text})
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

func encodeToolChoice(choice *ir.ToolChoice) (*ToolChoiceWire, []ir.Loss, error) {
	if choice == nil {
		return nil, nil, nil
	}
	switch choice.Mode {
	case "auto", "any", "none":
		return &ToolChoiceWire{Type: choice.Mode}, nil, nil
	case "tool":
		if choice.Name == "" {
			return nil, nil, fmt.Errorf("anthropic: tool_choice.name is required for mode tool")
		}
		return &ToolChoiceWire{Type: "tool", Name: choice.Name}, nil, nil
	default:
		return nil, []ir.Loss{{
			Path:   "tool_choice",
			Field:  "mode",
			Reason: ir.LossUnsupportedSemantic,
			Detail: fmt.Sprintf("IR tool_choice mode %q has no Anthropic equivalent", choice.Mode),
		}}, nil
	}
}

func encodeRequestBlocks(blocks []ir.Block, path string) ([]BlockWire, []ir.Loss, error) {
	out := make([]BlockWire, 0, len(blocks))
	var losses []ir.Loss
	for i, block := range blocks {
		encoded, blockLosses, mapped, err := encodeRequestBlock(block, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, nil, err
		}
		if mapped {
			out = append(out, encoded)
		}
		losses = append(losses, blockLosses...)
	}
	return out, losses, nil
}

func encodeRequestBlock(block ir.Block, path string) (BlockWire, []ir.Loss, bool, error) {
	switch b := block.(type) {
	case ir.TextBlock:
		return BlockWire{Type: BlockTypeText, Text: b.Text}, nil, true, nil
	case ir.ImageBlock:
		return encodeImageBlock(b, path)
	case ir.ToolUseBlock:
		if b.ID == "" {
			return BlockWire{}, nil, false, fmt.Errorf("anthropic: %s.id is required", path)
		}
		if b.Name == "" {
			return BlockWire{}, nil, false, fmt.Errorf("anthropic: %s.name is required", path)
		}
		input, err := inputFromIRString(b.Input)
		if err != nil {
			return BlockWire{}, nil, false, fmt.Errorf("anthropic: %s.input: %w", path, err)
		}
		if err := requireJSONObject(input, path+".input"); err != nil {
			return BlockWire{}, nil, false, err
		}
		return BlockWire{Type: BlockTypeToolUse, ID: b.ID, Name: b.Name, Input: input}, nil, true, nil
	case ir.ToolResultBlock:
		return encodeToolResultBlock(b, path)
	default:
		return BlockWire{}, []ir.Loss{unsupportedBlockLoss(path, block)}, false, nil
	}
}

func encodeImageBlock(image ir.ImageBlock, path string) (BlockWire, []ir.Loss, bool, error) {
	hasData := image.Data != ""
	hasURL := image.URL != ""
	if hasData == hasURL {
		return BlockWire{}, []ir.Loss{invalidImageLoss(path, "image must contain exactly one of data or url")}, false, nil
	}
	if hasData {
		if image.MediaType == "" {
			return BlockWire{}, []ir.Loss{invalidImageLoss(path, "base64 image data requires media_type")}, false, nil
		}
		return BlockWire{Type: BlockTypeImage, Source: &SourceWire{
			Type: SourceTypeBase64, MediaType: image.MediaType, Data: image.Data,
		}}, nil, true, nil
	}
	if image.MediaType != "" {
		return BlockWire{}, []ir.Loss{invalidImageLoss(path, "URL image must not carry media_type")}, false, nil
	}
	return BlockWire{Type: BlockTypeImage, Source: &SourceWire{Type: SourceTypeURL, URL: image.URL}}, nil, true, nil
}

func encodeToolResultBlock(result ir.ToolResultBlock, path string) (BlockWire, []ir.Loss, bool, error) {
	if result.ToolUseID == "" {
		return BlockWire{}, nil, false, fmt.Errorf("anthropic: %s.tool_use_id is required", path)
	}
	content := make([]BlockWire, 0, len(result.Content))
	var losses []ir.Loss
	for i, block := range result.Content {
		contentPath := fmt.Sprintf("%s.content[%d]", path, i)
		switch b := block.(type) {
		case ir.TextBlock:
			content = append(content, BlockWire{Type: BlockTypeText, Text: b.Text})
		case ir.ImageBlock:
			image, imageLosses, mapped, err := encodeImageBlock(b, contentPath)
			if err != nil {
				return BlockWire{}, nil, false, err
			}
			if mapped {
				content = append(content, image)
			}
			losses = append(losses, imageLosses...)
		default:
			losses = append(losses, unsupportedBlockLoss(contentPath, block))
		}
	}
	return BlockWire{
		Type: BlockTypeToolResult, ToolUseID: result.ToolUseID, Content: content, IsError: result.IsError,
	}, losses, true, nil
}

func invalidImageLoss(path, detail string) ir.Loss {
	return ir.Loss{
		Path:   path,
		Field:  "image",
		Reason: ir.LossUnsupportedSemantic,
		Detail: detail,
	}
}

func unsupportedBlockLoss(path string, block ir.Block) ir.Loss {
	return ir.Loss{
		Path:   path,
		Field:  "type",
		Reason: ir.LossUnsupportedSemantic,
		Detail: fmt.Sprintf("IR block type %T has no Anthropic equivalent in this position", block),
	}
}

// EncodeResponse converts an IR response to an Anthropic Messages wire
// response (IR -> face). Near-identity; the envelope fields type ("message")
// and role ("assistant") are rendered defaults and record no loss
// (vectors/README.md "From-ir rendering defaults").
func EncodeResponse(resp *ir.Response, opts ...Option) (*Response, []ir.Loss, error) {
	if resp == nil {
		return nil, nil, fmt.Errorf("anthropic: nil response")
	}
	o := newOptions(opts...)
	out := &Response{
		ID:    resp.ID,
		Type:  TypeMessage,
		Role:  RoleAssistant,
		Model: o.models.Map(resp.Model),
		Usage: &UsageWire{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	}
	var losses []ir.Loss
	for i, block := range resp.Content {
		encoded, blockLosses, mapped, err := encodeResponseBlock(block, fmt.Sprintf("content[%d]", i))
		if err != nil {
			return nil, nil, err
		}
		if mapped {
			out.Content = append(out.Content, encoded)
		}
		losses = append(losses, blockLosses...)
	}
	switch resp.StopReason {
	case ir.StopEndTurn:
		out.StopReason = StopReasonEndTurn
	case ir.StopMaxTokens:
		out.StopReason = StopReasonMaxTokens
	case ir.StopSequence:
		out.StopReason = StopReasonStopSequence
		if resp.StopSequence != "" {
			out.StopSequence = resp.StopSequence
		}
	case ir.StopToolUse:
		out.StopReason = StopReasonToolUse
	case ir.StopRefusal:
		out.StopReason = StopReasonRefusal
	default:
		return nil, nil, fmt.Errorf("anthropic: stop reason %q has no Anthropic equivalent", resp.StopReason)
	}
	return out, losses, nil
}

func encodeResponseBlock(block ir.Block, path string) (BlockWire, []ir.Loss, bool, error) {
	switch b := block.(type) {
	case ir.TextBlock:
		return BlockWire{Type: BlockTypeText, Text: b.Text}, nil, true, nil
	case ir.ImageBlock:
		return encodeImageBlock(b, path)
	case ir.ToolUseBlock:
		if b.ID == "" {
			return BlockWire{}, nil, false, fmt.Errorf("anthropic: %s.id is required", path)
		}
		if b.Name == "" {
			return BlockWire{}, nil, false, fmt.Errorf("anthropic: %s.name is required", path)
		}
		input, err := inputFromIRString(b.Input)
		if err != nil {
			return BlockWire{}, nil, false, fmt.Errorf("anthropic: %s.input: %w", path, err)
		}
		if err := requireJSONObject(input, path+".input"); err != nil {
			return BlockWire{}, nil, false, err
		}
		return BlockWire{Type: BlockTypeToolUse, ID: b.ID, Name: b.Name, Input: input}, nil, true, nil
	default:
		return BlockWire{}, []ir.Loss{unsupportedBlockLoss(path, block)}, false, nil
	}
}
