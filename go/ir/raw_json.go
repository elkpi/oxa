package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func replaceRawJSONValue(document []byte, path []string, raw json.RawMessage) ([]byte, error) {
	start, end := 0, len(document)
	for _, key := range path {
		decoder := json.NewDecoder(bytes.NewReader(document[start:end]))
		token, err := decoder.Token()
		if err != nil || token != json.Delim('{') {
			return nil, fmt.Errorf("ir: expected object at %q", key)
		}
		found := false
		for decoder.More() {
			field, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			before := int(decoder.InputOffset())
			var value json.RawMessage
			if err := decoder.Decode(&value); err != nil {
				return nil, err
			}
			after := int(decoder.InputOffset())
			if field == key {
				offset := bytes.Index(document[start+before:start+after], value)
				if offset < 0 {
					return nil, fmt.Errorf("ir: encoded JSON value %q not found", key)
				}
				start, end = start+before+offset, start+after
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("ir: encoded JSON field %q not found", key)
		}
	}
	result := append([]byte(nil), document[:start]...)
	result = append(result, raw...)
	result = append(result, document[end:]...)
	return result, nil
}
