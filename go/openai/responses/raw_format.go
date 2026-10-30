package responses

import "encoding/json"

func maskRawFormat(data []byte, root *rawJSONNode, path []string) ([]byte, json.RawMessage) {
	var matches []*rawJSONNode
	var walk func(*rawJSONNode, int)
	walk = func(node *rawJSONNode, depth int) {
		if node == nil {
			return
		}
		if depth == len(path) {
			matches = append(matches, node)
			return
		}
		for _, field := range node.fields {
			if field.name == path[depth] {
				walk(field.value, depth+1)
			}
		}
	}
	walk(root, 0)
	if len(matches) == 0 {
		return data, nil
	}
	result := make([]byte, 0, len(data))
	pos := 0
	var format json.RawMessage
	for _, node := range matches {
		result = append(result, data[pos:node.start]...)
		result = append(result, "null"...)
		format = append(format[:0], data[node.start:node.end]...)
		pos = node.end
	}
	return append(result, data[pos:]...), format
}
