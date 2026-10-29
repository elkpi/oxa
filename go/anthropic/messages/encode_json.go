package messages

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
)

// EncodeRequestJSON 将 IR 请求编码为 Anthropic JSON，并保留响应格式 schema 的原始字节。
// 对 EncodeRequest 返回的 wire 值使用 encoding/json.Marshal 可能压缩不透明 JSON。
func EncodeRequestJSON(req *ir.Request, opts ...Option) ([]byte, []ir.Loss, error) {
	wire, losses, err := EncodeRequest(req, opts...)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return nil, nil, err
	}
	if req.Params.ResponseFormat == nil || req.Params.ResponseFormat.Type != ir.ResponseFormatJSONSchema {
		return encoded, losses, nil
	}
	root, err := parseRawJSON(encoded)
	if err != nil {
		return nil, nil, err
	}
	node := root
	for _, name := range []string{"output_config", "format", "schema"} {
		var next *rawJSONNode
		if node != nil {
			for _, field := range node.fields {
				if field.name == name {
					next = field.value
					break
				}
			}
		}
		node = next
	}
	if node == nil {
		return nil, nil, fmt.Errorf("anthropic: encoded schema not found")
	}
	result := make([]byte, 0, len(encoded)-(node.end-node.start)+len(req.Params.ResponseFormat.Schema))
	result = append(result, encoded[:node.start]...)
	result = append(result, req.Params.ResponseFormat.Schema...)
	result = append(result, encoded[node.end:]...)
	return result, losses, nil
}
