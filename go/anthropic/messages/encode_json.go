package messages

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/elkpi/oxa-protocol/go/v3/ir"
)

// EncodeRequestJSON preserves the original bytes of opaque schemas.
func EncodeRequestJSON(req *ir.Request, opts ...Option) ([]byte, []ir.Loss, error) {
	wire, losses, err := EncodeRequest(req, opts...)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return nil, nil, err
	}
	root, err := parseRawJSON(encoded)
	if err != nil {
		return nil, nil, err
	}
	type replacement struct {
		node *rawJSONNode
		raw  []byte
	}
	var replacements []replacement
	if req.Params.ResponseFormat != nil && req.Params.ResponseFormat.Type == ir.ResponseFormatJSONSchema {
		node := rawField(rawField(rawField(root, "output_config"), "format"), "schema")
		if node == nil {
			return nil, nil, fmt.Errorf("anthropic: encoded schema not found")
		}
		replacements = append(replacements, replacement{node, req.Params.ResponseFormat.Schema})
	}
	messages := rawField(root, "messages")
	for i, message := range wire.Messages {
		blocks, ok := message.Content.([]BlockWire)
		if !ok || messages == nil || i >= len(messages.items) {
			continue
		}
		content := rawField(messages.items[i], "content")
		for j, block := range blocks {
			if content == nil || j >= len(content.items) {
				continue
			}
			for index, schema := range block.rawSchemas {
				node := content.items[j]
				if index >= 0 {
					changes := rawField(node, "tool_changes")
					if changes == nil || index >= len(changes.items) {
						return nil, nil, fmt.Errorf("anthropic: encoded tool change not found")
					}
					node = changes.items[index]
				}
				node = rawField(rawField(rawField(node, "tool"), "definition"), "input_schema")
				if node == nil {
					return nil, nil, fmt.Errorf("anthropic: encoded tool schema not found")
				}
				replacements = append(replacements, replacement{node, schema})
			}
		}
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].node.start > replacements[j].node.start })
	for _, r := range replacements {
		result := append([]byte(nil), encoded[:r.node.start]...)
		result = append(result, r.raw...)
		encoded = append(result, encoded[r.node.end:]...)
	}
	return encoded, losses, nil
}

func rawField(node *rawJSONNode, name string) *rawJSONNode {
	if node != nil {
		for _, field := range node.fields {
			if field.name == name {
				return field.value
			}
		}
	}
	return nil
}
