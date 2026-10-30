package responses

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
)

func decodeWebSearchCall(id, status string, action json.RawMessage, extra map[string]json.RawMessage, path string) (ir.WebSearchCallBlock, []ir.Loss, error) {
	action, losses, err := ir.InspectWebSearchCall(id, status, action, path+".action")
	if err != nil {
		return ir.WebSearchCallBlock{}, nil, fmt.Errorf("responses: %s: %w", path, err)
	}
	if err := ir.ValidateWebSearchExtra(extra); err != nil {
		return ir.WebSearchCallBlock{}, nil, err
	}
	return ir.WebSearchCallBlock{ID: id, Status: status, Action: action, Extra: extra}, losses, nil
}

func webSearchExtra(data []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, key := range []string{"type", "id", "status", "action"} {
		delete(fields, key)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return fields, nil
}

func mergeWebSearchExtra(encoded []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if err := ir.ValidateWebSearchExtra(extra); err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return encoded, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	for key, value := range extra {
		fields[key] = value
	}
	return json.Marshal(fields)
}

func (item *InputItem) UnmarshalJSON(data []byte) error {
	var header struct {
		Type   string          `json:"type"`
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Action json.RawMessage `json:"action"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if header.Type == ItemTypeWebSearchCall {
		extra, err := webSearchExtra(data)
		if err != nil {
			return err
		}
		*item = InputItem{Type: header.Type, ID: header.ID, Status: header.Status, Action: header.Action, Extra: extra}
		return nil
	}
	type alias InputItem
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*item = InputItem(decoded)
	return nil
}

func (item InputItem) MarshalJSON() ([]byte, error) {
	type alias InputItem
	encoded, err := json.Marshal(alias(item))
	if err != nil || item.Type != ItemTypeWebSearchCall {
		return encoded, err
	}
	return mergeWebSearchExtra(encoded, item.Extra)
}

func (item *OutputItem) UnmarshalJSON(data []byte) error {
	var header struct {
		Type   string          `json:"type"`
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Action json.RawMessage `json:"action"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if header.Type == ItemTypeWebSearchCall {
		extra, err := webSearchExtra(data)
		if err != nil {
			return err
		}
		*item = OutputItem{Type: header.Type, ID: header.ID, Status: header.Status, Action: header.Action, Extra: extra}
		return nil
	}
	type alias OutputItem
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*item = OutputItem(decoded)
	return nil
}

func (item OutputItem) MarshalJSON() ([]byte, error) {
	type alias OutputItem
	encoded, err := json.Marshal(alias(item))
	if err != nil || item.Type != ItemTypeWebSearchCall {
		return encoded, err
	}
	return mergeWebSearchExtra(encoded, item.Extra)
}

func encodeWebSearchCall(block ir.WebSearchCallBlock, path string) (InputItem, error) {
	if err := ir.ValidateWebSearchExtra(block.Extra); err != nil {
		return InputItem{}, err
	}
	action, _, err := ir.InspectWebSearchCall(block.ID, block.Status, block.Action, path+".action")
	if err != nil {
		return InputItem{}, fmt.Errorf("responses: %s: %w", path, err)
	}
	return InputItem{Type: ItemTypeWebSearchCall, ID: block.ID, Status: block.Status, Action: action, Extra: block.Extra}, nil
}
