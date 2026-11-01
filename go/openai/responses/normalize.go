package responses

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

func loss(path, field string, reason ir.LossReason, detail string) ir.Loss {
	return ir.Loss{Path: path, Field: field, Reason: reason, Detail: detail}
}

func ptrLoss(value ir.Loss) *ir.Loss { return &value }

// decodeItemContent normalizes a message item's content (string shorthand or
// parts array) into IR blocks (N-R-3).
func decodeItemContent(content any, path string, isUser ...bool) ([]ir.Block, []ir.Loss, error) {
	user := false
	if len(isUser) > 0 {
		user = isUser[0]
	}
	switch v := content.(type) {
	case nil:
		return []ir.Block{ir.TextBlock{Text: ""}}, nil, nil
	case string:
		return []ir.Block{ir.TextBlock{Text: v}}, nil, nil
	case []ContentPart:
		return decodeParts(v, path, user)
	case []any:
		parts := make([]ContentPart, 0, len(v))
		for i, raw := range v {
			part, ok := raw.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("responses: %s[%d]: part is not an object", path, i)
			}
			var p ContentPart
			enc, err := json.Marshal(part)
			if err != nil {
				return nil, nil, fmt.Errorf("responses: %s[%d]: part re-encode failed: %w", path, i, err)
			}
			if err := json.Unmarshal(enc, &p); err != nil {
				return nil, nil, fmt.Errorf("responses: %s[%d]: malformed part: %w", path, i, err)
			}
			parts = append(parts, p)
		}
		return decodeParts(parts, path, user)
	default:
		return nil, nil, fmt.Errorf("responses: %s: content is neither string nor parts array", path)
	}
}

func decodeFileData(raw, path string) (string, string, error) {
	if strings.HasPrefix(raw, "data:") {
		rest := strings.TrimPrefix(raw, "data:")
		idx := strings.Index(rest, ";base64,")
		if idx == -1 {
			return "", "", fmt.Errorf("responses: %s: malformed data file URL", path)
		}
		mt := rest[:idx]
		if mt == "" {
			mt = "application/pdf"
		}
		payload := rest[idx+len(";base64,"):]
		return payload, mt, nil
	}
	return raw, "application/pdf", nil
}

func decodeParts(parts []ContentPart, path string, isUser ...bool) ([]ir.Block, []ir.Loss, error) {
	user := false
	if len(isUser) > 0 {
		user = isUser[0]
	}
	blocks := make([]ir.Block, 0, len(parts))
	var losses []ir.Loss
	for i, part := range parts {
		switch part.Type {
		case "refusal":
			if user {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
					"Responses content part type \"refusal\" has no IR equivalent in user messages",
				))
				continue
			}
			blocks = append(blocks, ir.RefusalBlock{Refusal: part.Refusal})
		case PartTypeInputText, PartTypeOutputText:
			blocks = append(blocks, ir.TextBlock{Text: part.Text})
		case PartTypeInputImage:
			image, imageLoss := decodeImageURL(part.ImageURL, fmt.Sprintf("%s[%d].image_url", path, i))
			if imageLoss != nil {
				losses = append(losses, *imageLoss)
				continue
			}
			blocks = append(blocks, image)
		case PartTypeInputFile:
			if !user {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
					"Responses content part type \"input_file\" has no IR equivalent",
				))
				continue
			}
			var count int
			if part.FileID != nil {
				count++
			}
			if part.FileURL != nil {
				count++
			}
			if part.FileData != nil {
				count++
			}
			if count != 1 {
				return nil, nil, fmt.Errorf("responses: %s[%d]: input_file must specify exactly one of file_id, file_url, or file_data", path, i)
			}
			if part.Filename != nil {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d].filename", path, i), "filename", ir.LossUnmappedField,
					"Responses input_file filename has no IR equivalent",
				))
			}
			if part.Detail != nil {
				losses = append(losses, loss(
					fmt.Sprintf("%s[%d].detail", path, i), "detail", ir.LossUnmappedField,
					"Responses input_file detail has no IR equivalent",
				))
			}
			if part.FileID != nil {
				if *part.FileID == "" {
					return nil, nil, fmt.Errorf("responses: %s[%d].file_id cannot be empty", path, i)
				}
				blocks = append(blocks, ir.DocumentBlock{FileID: *part.FileID})
			} else if part.FileURL != nil {
				if *part.FileURL == "" {
					return nil, nil, fmt.Errorf("responses: %s[%d].file_url cannot be empty", path, i)
				}
				blocks = append(blocks, ir.DocumentBlock{URL: *part.FileURL})
			} else {
				if *part.FileData == "" {
					return nil, nil, fmt.Errorf("responses: %s[%d].file_data cannot be empty", path, i)
				}
				data, mt, err := decodeFileData(*part.FileData, fmt.Sprintf("%s[%d].file_data", path, i))
				if err != nil {
					return nil, nil, err
				}
				blocks = append(blocks, ir.DocumentBlock{Data: data, MediaType: mt})
			}
		default:
			losses = append(losses, loss(
				fmt.Sprintf("%s[%d]", path, i), "type", ir.LossUnsupportedSemantic,
				fmt.Sprintf("Responses content part type %q has no IR equivalent", part.Type),
			))
		}
	}
	return blocks, losses, nil
}

// decodeImageURL normalizes supported https and data image URLs into
// ImageBlocks (N-R-4), with the same rules as the Chat Completions spoke.
func decodeImageURL(raw, path string) (ir.ImageBlock, *ir.Loss) {
	if strings.HasPrefix(raw, "https:") {
		u, err := url.ParseRequestURI(raw)
		if err == nil && u.Scheme == "https" && u.Host != "" {
			return ir.ImageBlock{URL: raw}, nil
		}
		return ir.ImageBlock{}, ptrLoss(loss(path, "image_url", ir.LossUnsupportedSemantic,
			"malformed https image URL has no IR equivalent"))
	}
	if !strings.HasPrefix(raw, "data:") {
		return ir.ImageBlock{}, ptrLoss(loss(path, "image_url", ir.LossUnsupportedSemantic,
			"only https and base64 data image URLs are supported"))
	}
	metadata, data, found := strings.Cut(strings.TrimPrefix(raw, "data:"), ",")
	if !found || !strings.HasSuffix(metadata, ";base64") {
		return ir.ImageBlock{}, ptrLoss(loss(path, "image_url", ir.LossUnsupportedSemantic,
			"malformed data image URL has no IR equivalent"))
	}
	mediaType := strings.TrimSuffix(metadata, ";base64")
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") || mediaType == "image/" {
		return ir.ImageBlock{}, ptrLoss(loss(path, "image_url", ir.LossUnsupportedSemantic,
			"non-image data URL has no IR equivalent"))
	}
	return ir.ImageBlock{MediaType: mediaType, Data: data}, nil
}

// encodeImagePart renders an ImageBlock as an input_image part (N-R-4).
func encodeImagePart(image ir.ImageBlock, path string) (ContentPart, *ir.Loss) {
	if image.Data != "" && image.URL != "" {
		return ContentPart{}, ptrLoss(loss(path, "image", ir.LossUnsupportedSemantic,
			"IR image contains both data and URL"))
	}
	if image.Data != "" {
		if !strings.HasPrefix(strings.ToLower(image.MediaType), "image/") || image.MediaType == "image/" {
			return ContentPart{}, ptrLoss(loss(path, "media_type", ir.LossUnsupportedSemantic,
				"IR image media type has no Responses input_image equivalent"))
		}
		return ContentPart{Type: PartTypeInputImage, ImageURL: "data:" + image.MediaType + ";base64," + image.Data}, nil
	}
	if image.URL != "" {
		u, err := url.ParseRequestURI(image.URL)
		if err == nil && u.Scheme == "https" && u.Host != "" {
			return ContentPart{Type: PartTypeInputImage, ImageURL: image.URL}, nil
		}
	}
	return ContentPart{}, ptrLoss(loss(path, "image", ir.LossUnsupportedSemantic,
		"IR image has no supported Responses input_image equivalent"))
}

// wrapToolArguments wraps the wire arguments string into the IR raw JSON
// string token without parsing it (INV-1, N-R-5).
func wrapToolArguments(arguments string) (json.RawMessage, error) {
	return json.Marshal(arguments)
}

// unwrapToolArguments extracts the wire arguments string from the IR raw
// JSON string token, unwrapping only the JSON string envelope (INV-1, N-R-5).
func unwrapToolArguments(input json.RawMessage) (string, error) {
	var arguments string
	if err := json.Unmarshal(input, &arguments); err != nil {
		return "", fmt.Errorf("tool input is not a JSON string: %w", err)
	}
	return arguments, nil
}
