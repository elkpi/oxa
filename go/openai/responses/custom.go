package responses

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
)

func decodeCustomFormat(f *CustomToolFormatWire, path string) (*ir.CustomToolFormat, []ir.Loss, error) {
	if f == nil {
		return nil, nil, nil
	}
	switch f.Type {
	case "text":
		return &ir.CustomToolFormat{Type: "text"}, nil, nil
	case "grammar":
		if f.Definition == nil {
			return nil, nil, fmt.Errorf("responses: %s.definition is required", path)
		}
		if f.Syntax == "lark" || f.Syntax == "regex" {
			return &ir.CustomToolFormat{Type: f.Type, Syntax: f.Syntax, Definition: *f.Definition}, nil, nil
		}
	}
	return nil, []ir.Loss{loss(path, "format", ir.LossUnsupportedSemantic, "custom tool format is not supported")}, nil
}

func encodeCustomFormat(f *ir.CustomToolFormat) *CustomToolFormatWire {
	if f == nil {
		return nil
	}
	out := &CustomToolFormatWire{Type: f.Type}
	if f.Type == "grammar" {
		out.Syntax = f.Syntax
		definition := f.Definition
		out.Definition = &definition
	}
	return out
}

func decodeCustomCall(id, name string, input *string) (ir.CustomToolUseBlock, error) {
	if id == "" || name == "" || input == nil {
		return ir.CustomToolUseBlock{}, fmt.Errorf("responses: custom call requires call_id, name, and string input")
	}
	token, err := json.Marshal(*input)
	return ir.CustomToolUseBlock{ID: id, Name: name, Input: token}, err
}

func decodeCustomOutput(value any, path string) (ir.CustomToolOutput, []ir.Loss, error) {
	if text, ok := value.(string); ok {
		return ir.CustomToolOutput{Text: &text}, nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ir.CustomToolOutput{}, nil, err
	}
	var parts []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' {
		return ir.CustomToolOutput{}, nil, fmt.Errorf("responses: %s must be a string or content list", path)
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ir.CustomToolOutput{}, nil, err
	}
	out := ir.CustomToolOutput{Content: make([]ir.Block, 0, len(parts))}
	var losses []ir.Loss
	for i, raw := range parts {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			return ir.CustomToolOutput{}, nil, err
		}
		partPath := fmt.Sprintf("%s[%d]", path, i)
		switch head.Type {
		case PartTypeInputText:
			var part struct {
				Text *string `json:"text"`
			}
			if err := json.Unmarshal(raw, &part); err != nil {
				return ir.CustomToolOutput{}, nil, err
			}
			if part.Text == nil {
				return ir.CustomToolOutput{}, nil, fmt.Errorf("responses: %s.text is required", partPath)
			}
			out.Content = append(out.Content, ir.TextBlock{Text: *part.Text})
		case PartTypeInputImage:
			var part struct {
				ImageURL string `json:"image_url"`
			}
			if err := json.Unmarshal(raw, &part); err != nil {
				return ir.CustomToolOutput{}, nil, err
			}
			image, imageLoss := decodeImageURL(part.ImageURL, partPath)
			if imageLoss != nil {
				losses = append(losses, loss(partPath, "type", ir.LossUnsupportedSemantic, "custom tool output image is not supported"))
				continue
			}
			out.Content = append(out.Content, image)
		default:
			losses = append(losses, loss(partPath, "type", ir.LossUnsupportedSemantic, "custom tool output content is not supported"))
		}
	}
	return out, losses, nil
}

func encodeCustomOutput(out ir.CustomToolOutput, path string) (any, []ir.Loss, error) {
	if out.Text != nil {
		if out.Content != nil {
			return nil, nil, fmt.Errorf("responses: output cannot contain text and list")
		}
		return *out.Text, nil, nil
	}
	parts := make([]map[string]any, 0, len(out.Content))
	var losses []ir.Loss
	for i, block := range out.Content {
		switch b := block.(type) {
		case ir.TextBlock:
			parts = append(parts, map[string]any{"type": PartTypeInputText, "text": b.Text})
		case ir.ImageBlock:
			p, imageLoss := encodeImagePart(b, fmt.Sprintf("%s[%d]", path, i))
			if imageLoss != nil {
				losses = append(losses, *imageLoss)
				continue
			}
			parts = append(parts, map[string]any{"type": PartTypeInputImage, "image_url": p.ImageURL})
		default:
			return nil, nil, fmt.Errorf("responses: invalid custom output content %T", block)
		}
	}
	return parts, losses, nil
}
