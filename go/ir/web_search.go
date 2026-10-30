package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// WebSearchCallBlock is one Responses web_search_call history item (spec/11 N-R-16).
// Action is the provider action object, including unknown keys.
type WebSearchCallBlock struct {
	ID     string
	Status string
	Action json.RawMessage
	Extra  map[string]json.RawMessage
}

// InspectWebSearchCall validates a web_search_call and lists inapplicable action keys.
// The returned action bytes are the original object. Unknown keys are not removed.
func InspectWebSearchCall(id, status string, action json.RawMessage, path string) (json.RawMessage, []Loss, error) {
	if id == "" || status == "" {
		return nil, nil, fmt.Errorf("ir: web_search_call requires non-empty id and status")
	}
	raw := bytes.TrimSpace(action)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil, fmt.Errorf("ir: web_search_call action is required")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, nil, fmt.Errorf("ir: web_search_call action must be an object")
	}
	typeRaw, ok := fields["type"]
	if !ok || bytes.Equal(bytes.TrimSpace(typeRaw), []byte("null")) {
		return nil, nil, fmt.Errorf("ir: web_search_call action.type is required")
	}
	var actionType string
	if err := json.Unmarshal(typeRaw, &actionType); err != nil || actionType == "" {
		return nil, nil, fmt.Errorf("ir: web_search_call action.type is required")
	}
	applicable := map[string]func(json.RawMessage) error{}
	switch actionType {
	case "search":
		applicable["query"] = jsonString
		applicable["queries"] = jsonStringArray
		applicable["sources"] = jsonSearchSources
	case "open_page":
		applicable["url"] = func(value json.RawMessage) error {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil
			}
			return jsonString(value)
		}
	case "find_in_page":
		applicable["url"] = jsonString
		applicable["pattern"] = jsonString
		if _, ok := fields["url"]; !ok {
			return nil, nil, fmt.Errorf("ir: web_search_call find_in_page requires url")
		}
		if _, ok := fields["pattern"]; !ok {
			return nil, nil, fmt.Errorf("ir: web_search_call find_in_page requires pattern")
		}
	default:
		return nil, nil, fmt.Errorf("ir: web_search_call action.type %q is unknown", actionType)
	}
	var unknown []string
	for key, value := range fields {
		if key == "type" {
			continue
		}
		check, ok := applicable[key]
		if !ok {
			unknown = append(unknown, key)
			continue
		}
		if err := check(value); err != nil {
			return nil, nil, fmt.Errorf("ir: web_search_call action.%s: %w", key, err)
		}
	}
	sort.Strings(unknown)
	losses := make([]Loss, 0, len(unknown))
	for _, key := range unknown {
		losses = append(losses, Loss{
			Path:   path + "." + key,
			Field:  key,
			Reason: LossUnmappedField,
		})
	}
	if actionType == "search" {
		if sourceRaw, present := fields["sources"]; present {
			var sources []map[string]json.RawMessage
			if err := json.Unmarshal(sourceRaw, &sources); err != nil {
				return nil, nil, err
			}
			for index, source := range sources {
				var extra []string
				for field := range source {
					if field != "type" && field != "url" {
						extra = append(extra, field)
					}
				}
				sort.Strings(extra)
				for _, field := range extra {
					losses = append(losses, Loss{Path: fmt.Sprintf("%s.sources[%d].%s", path, index, field), Field: field, Reason: LossUnmappedField})
				}
			}
		}
	}
	return append(json.RawMessage(nil), raw...), losses, nil
}

func ValidateWebSearchExtra(extra map[string]json.RawMessage) error {
	for key, value := range extra {
		switch key {
		case "type", "id", "status", "action":
			return fmt.Errorf("ir: web_search_call extra must not shadow %q", key)
		}
		if !json.Valid(value) {
			return fmt.Errorf("ir: web_search_call extra.%s must be valid JSON", key)
		}
	}
	return nil
}

func marshalWebSearchCall(v WebSearchCallBlock) (json.RawMessage, error) {
	if err := ValidateWebSearchExtra(v.Extra); err != nil {
		return nil, err
	}
	action, _, err := InspectWebSearchCall(v.ID, v.Status, v.Action, "action")
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Type   string                     `json:"type"`
		ID     string                     `json:"id"`
		Status string                     `json:"status"`
		Action json.RawMessage            `json:"action"`
		Extra  map[string]json.RawMessage `json:"extra,omitempty"`
	}{BlockTypeWebSearchCall, v.ID, v.Status, action, v.Extra})
}

func unmarshalWebSearchCall(data []byte) (WebSearchCallBlock, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return WebSearchCallBlock{}, err
	}
	if _, ok := fields["native"]; ok {
		return WebSearchCallBlock{}, fmt.Errorf("ir: web_search_call does not support native annotations")
	}
	if raw, ok := fields["extra"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return WebSearchCallBlock{}, fmt.Errorf("ir: web_search_call extra must be an object")
	}
	var wire struct {
		ID     string                     `json:"id"`
		Status string                     `json:"status"`
		Action json.RawMessage            `json:"action"`
		Extra  map[string]json.RawMessage `json:"extra"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return WebSearchCallBlock{}, err
	}
	action, _, err := InspectWebSearchCall(wire.ID, wire.Status, wire.Action, "action")
	if err != nil {
		return WebSearchCallBlock{}, err
	}
	if err := ValidateWebSearchExtra(wire.Extra); err != nil {
		return WebSearchCallBlock{}, err
	}
	return WebSearchCallBlock{ID: wire.ID, Status: wire.Status, Action: action, Extra: wire.Extra}, nil
}

func jsonSearchSources(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("must be an array of sources")
	}
	var sources []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sources); err != nil {
		return fmt.Errorf("must be an array of sources")
	}
	for _, source := range sources {
		var kind string
		if err := json.Unmarshal(source["type"], &kind); err != nil || kind != "url" {
			return fmt.Errorf("source type must be url")
		}
		if err := jsonString(source["url"]); err != nil {
			return fmt.Errorf("source url must be a string")
		}
	}
	return nil
}

func jsonString(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("must be a string")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("must be a string")
	}
	return nil
}

func jsonStringArray(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("must be an array of strings")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("must be an array of strings")
	}
	for _, item := range items {
		if err := jsonString(item); err != nil {
			return fmt.Errorf("must be an array of strings")
		}
	}
	return nil
}
