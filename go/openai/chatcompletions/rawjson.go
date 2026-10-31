package chatcompletions

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

type rawJSONField struct {
	name      string
	keyOffset int
	value     *rawJSONNode
}

type rawJSONNode struct {
	kind   byte
	start  int
	end    int
	text   string
	fields []rawJSONField
	items  []*rawJSONNode
}

type rawJSONSchema struct {
	fields  map[string]*rawJSONSchema
	items   *rawJSONSchema
	variant func(*rawJSONNode) *rawJSONSchema
}

type rawJSONLoss struct {
	loss   ir.Loss
	offset int
}

type rawJSONLocations struct {
	keys   map[string]int
	values map[string]int
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func DecodeRequestJSON(raw []byte, opts ...Option) (*ir.Request, []ir.Loss, error) {
	root, err := parseRawJSON(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("chatcompletions: request JSON: %w", err)
	}
	schema := schemaForRawType(reflect.TypeOf(Request{}))
	cleanJSON := filterKnownJSON(raw, root, schema)
	cleanRoot, err := parseRawJSON(cleanJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("chatcompletions: request JSON: %w", err)
	}
	masked, format := maskRawFormat(cleanJSON, cleanRoot, []string{"response_format"})
	var wire Request
	if err := json.Unmarshal(masked, &wire); err != nil {
		return nil, nil, fmt.Errorf("chatcompletions: request JSON: %w", err)
	}
	if len(format) > 0 && string(format) != "null" {
		wire.ResponseFormat = format
	}
	req, losses, err := DecodeRequest(&wire, opts...)
	if err != nil {
		return nil, nil, err
	}
	audited, locations := inspectRawJSON(root, schema, "")
	return req, orderRawLosses(losses, audited, locations), nil
}

func DecodeResponseJSON(raw []byte, opts ...Option) (*ir.Response, []ir.Loss, error) {
	root, err := parseRawJSON(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("chatcompletions: response JSON: %w", err)
	}
	schema := schemaForRawType(reflect.TypeOf(Response{}))
	cleanJSON := filterKnownJSON(raw, root, schema)
	var wire Response
	if err := json.Unmarshal(cleanJSON, &wire); err != nil {
		return nil, nil, fmt.Errorf("chatcompletions: response JSON: %w", err)
	}
	resp, losses, err := DecodeResponse(&wire, opts...)
	if err != nil {
		return nil, nil, err
	}
	audited, locations := inspectRawJSON(root, schema, "")
	return resp, orderRawLosses(losses, audited, locations), nil
}

func (d *StreamDecoder) FeedJSON(raw []byte) ([]ir.Event, error) {
	root, err := parseRawJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("chatcompletions: chunk JSON: %w", err)
	}
	var chunk Chunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return nil, fmt.Errorf("chatcompletions: chunk JSON: %w", err)
	}
	audited, locations := inspectRawJSON(root, schemaForRawType(reflect.TypeOf(Chunk{})), "")
	before := len(d.losses)
	events, err := d.Feed(&chunk)
	if err != nil {
		return nil, err
	}
	ordered := orderRawLosses(d.losses[before:], audited, locations)
	d.losses = append(d.losses[:before], ordered...)
	return events, nil
}

func parseRawJSON(raw []byte) (*rawJSONNode, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("malformed JSON")
	}
	position := 0
	node, err := parseRawJSONNode(raw, &position)
	if err != nil {
		return nil, err
	}
	skipRawJSONSpace(raw, &position)
	if position != len(raw) {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return node, nil
}

func parseRawJSONNode(raw []byte, position *int) (*rawJSONNode, error) {
	skipRawJSONSpace(raw, position)
	if *position >= len(raw) {
		return nil, fmt.Errorf("unexpected end of JSON")
	}
	node := &rawJSONNode{kind: raw[*position], start: *position}
	switch node.kind {
	case '{':
		(*position)++
		skipRawJSONSpace(raw, position)
		if *position < len(raw) && raw[*position] == '}' {
			(*position)++
			node.end = *position
			return node, nil
		}
		for {
			skipRawJSONSpace(raw, position)
			keyStart := *position
			keyEnd := scanRawJSONString(raw, keyStart)
			if keyEnd < 0 {
				return nil, fmt.Errorf("invalid object key")
			}
			var key string
			if err := json.Unmarshal(raw[keyStart:keyEnd], &key); err != nil {
				return nil, err
			}
			*position = keyEnd
			skipRawJSONSpace(raw, position)
			if *position >= len(raw) || raw[*position] != ':' {
				return nil, fmt.Errorf("missing object colon")
			}
			(*position)++
			value, err := parseRawJSONNode(raw, position)
			if err != nil {
				return nil, err
			}
			node.fields = append(node.fields, rawJSONField{name: key, keyOffset: keyStart, value: value})
			skipRawJSONSpace(raw, position)
			if *position < len(raw) && raw[*position] == ',' {
				(*position)++
				continue
			}
			if *position < len(raw) && raw[*position] == '}' {
				(*position)++
				node.end = *position
				return node, nil
			}
			return nil, fmt.Errorf("unterminated object")
		}
	case '[':
		(*position)++
		skipRawJSONSpace(raw, position)
		if *position < len(raw) && raw[*position] == ']' {
			(*position)++
			node.end = *position
			return node, nil
		}
		for {
			item, err := parseRawJSONNode(raw, position)
			if err != nil {
				return nil, err
			}
			node.items = append(node.items, item)
			skipRawJSONSpace(raw, position)
			if *position < len(raw) && raw[*position] == ',' {
				(*position)++
				continue
			}
			if *position < len(raw) && raw[*position] == ']' {
				(*position)++
				node.end = *position
				return node, nil
			}
			return nil, fmt.Errorf("unterminated array")
		}
	case '"':
		end := scanRawJSONString(raw, *position)
		if end < 0 {
			return nil, fmt.Errorf("unterminated string")
		}
		if err := json.Unmarshal(raw[*position:end], &node.text); err != nil {
			return nil, err
		}
		*position = end
		node.end = end
		return node, nil
	default:
		for *position < len(raw) {
			b := raw[*position]
			if b == ',' || b == ']' || b == '}' || b == ' ' || b == '\t' || b == '\n' || b == '\r' {
				break
			}
			(*position)++
		}
		if *position == node.start {
			return nil, fmt.Errorf("invalid JSON value")
		}
		node.end = *position
		return node, nil
	}
}

func scanRawJSONString(raw []byte, start int) int {
	if start >= len(raw) || raw[start] != '"' {
		return -1
	}
	for index := start + 1; index < len(raw); index++ {
		switch raw[index] {
		case '\\':
			index++
		case '"':
			return index + 1
		}
	}
	return -1
}

func skipRawJSONSpace(raw []byte, position *int) {
	for *position < len(raw) {
		switch raw[*position] {
		case ' ', '\t', '\n', '\r':
			(*position)++
		default:
			return
		}
	}
}

func schemaForRawType(t reflect.Type) *rawJSONSchema {
	return schemaForRawTypeSeen(t, make(map[reflect.Type]*rawJSONSchema))
}

func schemaForRawTypeSeen(t reflect.Type, seen map[reflect.Type]*rawJSONSchema) *rawJSONSchema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == rawMessageType || t.Kind() == reflect.Interface || t.Kind() == reflect.Map {
		return nil
	}
	if schema, ok := seen[t]; ok {
		return schema
	}
	switch t {
	case reflect.TypeOf(ContentPart{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case ContentPartTypeText, ContentPartTypeImageURL:
				return rawObjectSchema(t, []string{"type", "text", "image_url"}, nil, seen)
			default:
				return nil
			}
		}
		return schema
	case reflect.TypeOf(Message{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			return rawObjectSchema(t,
				[]string{"role", "content", "reasoning_content", "tool_calls", "tool_call_id", "function_call"},
				map[string]*rawJSONSchema{
					"content": {variant: func(content *rawJSONNode) *rawJSONSchema {
						if content != nil && content.kind == '[' {
							return &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(ContentPart{}), seen)}
						}
						return nil
					}},
					"tool_calls": {items: schemaForRawTypeSeen(reflect.TypeOf(ToolCall{}), seen)},
				},
				seen)
		}
		return schema
	case reflect.TypeOf(ToolWire{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case ToolTypeFunction:
				return rawObjectSchema(t, []string{"type", "function"}, nil, seen)
			case ir.ToolTypeCustom:
				return rawObjectSchema(t, []string{"type", "custom"}, nil, seen)
			default:
				return nil
			}
		}
		return schema
	case reflect.TypeOf(ToolCall{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			hasCustom := false
			for _, f := range node.fields {
				if f.name == "custom" {
					hasCustom = true
					break
				}
			}
			if kind == ir.ToolTypeCustom || hasCustom {
				return rawObjectSchema(t, []string{"id", "type", "custom"}, nil, seen)
			}
			return rawObjectSchema(t, []string{"id", "type", "function"}, map[string]*rawJSONSchema{
				"function": rawObjectSchema(reflect.TypeOf(FunctionWire{}), []string{"name", "description", "parameters", "arguments"}, nil, seen),
			}, seen)
		}
		return schema
	case reflect.TypeOf(ToolCallDelta{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			hasCustom := false
			for _, f := range node.fields {
				if f.name == "custom" {
					hasCustom = true
					break
				}
			}
			if kind == ir.ToolTypeCustom || hasCustom {
				return rawObjectSchema(t, []string{"index", "id", "type", "custom"}, nil, seen)
			}
			return rawObjectSchema(t, []string{"index", "id", "type", "function"}, nil, seen)
		}
		return schema
	case reflect.TypeOf(Choice{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			return rawObjectSchema(t, []string{"index", "message", "finish_reason"}, nil, seen)
		}
		return schema
	case reflect.TypeOf(ChoiceDelta{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			return rawObjectSchema(t, []string{"index", "delta", "finish_reason"}, nil, seen)
		}
		return schema
	}

	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.items = schemaForRawTypeSeen(t.Elem(), seen)
		return schema
	case reflect.Struct:
		schema := &rawJSONSchema{fields: make(map[string]*rawJSONSchema)}
		seen[t] = schema
		for index := 0; index < t.NumField(); index++ {
			field := t.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			schema.fields[name] = schemaForRawTypeSeen(field.Type, seen)
		}
		switch t {
		case reflect.TypeOf(Request{}):
			schema.fields["tools"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(ToolWire{}), seen)}
			schema.fields["messages"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(Message{}), seen)}
			schema.fields["tool_choice"] = &rawJSONSchema{variant: func(node *rawJSONNode) *rawJSONSchema {
				if node == nil || node.kind != '{' {
					return nil
				}
				kind, _ := rawJSONStringField(node, "type")
				switch kind {
				case ir.ToolTypeCustom:
					return rawObjectSchema(reflect.TypeOf(ToolChoiceWire{}),
						[]string{"type", "custom"},
						map[string]*rawJSONSchema{
							"custom": {fields: map[string]*rawJSONSchema{"name": nil}},
						},
						seen)
				default:
					return rawObjectSchema(reflect.TypeOf(ToolChoiceWire{}),
						[]string{"type", "function"},
						map[string]*rawJSONSchema{
							"function": {fields: map[string]*rawJSONSchema{"name": nil}},
						},
						seen)
				}
			}}
			schema.fields["response_format"] = &rawJSONSchema{variant: func(node *rawJSONNode) *rawJSONSchema {
				if node == nil || node.kind != '{' {
					return nil
				}
				return rawObjectSchema(reflect.TypeOf(ResponseFormatWire{}),
					[]string{"type", "json_schema"},
					map[string]*rawJSONSchema{
						"json_schema": rawObjectSchema(reflect.TypeOf(JSONSchemaWire{}), []string{"name", "description", "schema", "strict"}, nil, seen),
					},
					seen)
			}}
			schema.fields["stream_options"] = &rawJSONSchema{variant: func(node *rawJSONNode) *rawJSONSchema {
				if node == nil || node.kind != '{' {
					return nil
				}
				return rawObjectSchema(reflect.TypeOf(StreamOptionsWire{}),
					[]string{"include_usage", "include_obfuscation"},
					nil,
					seen)
			}}
			delete(schema.fields, "logprobs")
			delete(schema.fields, "top_logprobs")
			delete(schema.fields, "parallel_tool_calls")
			delete(schema.fields, "functions")
			delete(schema.fields, "function_call")
		case reflect.TypeOf(Response{}):
			schema.fields["choices"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(Choice{}), seen)}
		case reflect.TypeOf(Chunk{}):
			schema.fields["choices"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(ChoiceDelta{}), seen)}
		}
		return schema
	default:
		return nil
	}
}

func rawObjectSchema(t reflect.Type, allowed []string, overrides map[string]*rawJSONSchema, seen map[reflect.Type]*rawJSONSchema) *rawJSONSchema {
	allow := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allow[name] = true
	}
	schema := &rawJSONSchema{fields: make(map[string]*rawJSONSchema, len(allowed))}
	for index := 0; index < t.NumField(); index++ {
		field := t.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if !allow[name] {
			continue
		}
		child := schemaForRawTypeSeen(field.Type, seen)
		if override, ok := overrides[name]; ok {
			child = override
		}
		schema.fields[name] = child
	}
	return schema
}

func inspectRawJSON(node *rawJSONNode, schema *rawJSONSchema, path string) ([]rawJSONLoss, rawJSONLocations) {
	locations := rawJSONLocations{keys: make(map[string]int), values: make(map[string]int)}
	var losses []rawJSONLoss
	var visit func(*rawJSONNode, *rawJSONSchema, string)
	visit = func(current *rawJSONNode, shape *rawJSONSchema, currentPath string) {
		if current == nil {
			return
		}
		locations.values[currentPath] = current.start
		if shape == nil {
			return
		}
		if shape.variant != nil {
			for _, field := range current.fields {
				fieldPath := joinRawJSONPath(currentPath, field.name)
				locations.keys[fieldPath] = field.keyOffset
				locations.values[fieldPath] = field.value.start
			}
			shape = shape.variant(current)
			if shape == nil {
				return
			}
		}
		if current.kind == '[' {
			if shape.items == nil {
				return
			}
			for index, item := range current.items {
				visit(item, shape.items, fmt.Sprintf("%s[%d]", currentPath, index))
			}
			return
		}
		if current.kind != '{' || shape.fields == nil {
			return
		}
		for _, field := range current.fields {
			fieldPath := joinRawJSONPath(currentPath, field.name)
			locations.keys[fieldPath] = field.keyOffset
			locations.values[fieldPath] = field.value.start
			child, known := shape.fields[field.name]
			if !known {
				losses = append(losses, rawJSONLoss{
					loss:   ir.Loss{Path: fieldPath, Field: field.name, Reason: ir.LossUnmappedField},
					offset: field.keyOffset,
				})
				continue
			}
			if child != nil {
				visit(field.value, child, fieldPath)
			}
		}
	}
	visit(node, schema, path)
	return losses, locations
}

func orderRawLosses(existing []ir.Loss, audited []rawJSONLoss, locations rawJSONLocations) []ir.Loss {
	type keyedLoss struct {
		loss   ir.Loss
		offset int
		index  int
	}
	type lossKey struct {
		path, field string
		reason      ir.LossReason
	}
	seen := make(map[lossKey]bool, len(existing)+len(audited))
	ordered := make([]keyedLoss, 0, len(existing)+len(audited))
	maxInt := int(^uint(0) >> 1)
	findOffset := func(loss ir.Loss) int {
		if loss.Field != "" {
			if offset, ok := locations.keys[joinRawJSONPath(loss.Path, loss.Field)]; ok {
				return offset
			}
		}
		if offset, ok := locations.keys[loss.Path]; ok {
			return offset
		}
		if offset, ok := locations.values[loss.Path]; ok {
			return offset
		}
		return maxInt
	}
	appendLoss := func(loss ir.Loss, offset int) {
		ordered = append(ordered, keyedLoss{loss: loss, offset: offset, index: len(ordered)})
	}
	for _, loss := range existing {
		seen[lossKey{loss.Path, loss.Field, loss.Reason}] = true
		appendLoss(loss, findOffset(loss))
	}
	for _, loss := range audited {
		key := lossKey{loss.loss.Path, loss.loss.Field, loss.loss.Reason}
		if seen[key] {
			continue
		}
		seen[key] = true
		appendLoss(loss.loss, loss.offset)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].offset == ordered[j].offset {
			return ordered[i].index < ordered[j].index
		}
		return ordered[i].offset < ordered[j].offset
	})
	result := make([]ir.Loss, len(ordered))
	for index, item := range ordered {
		result[index] = item.loss
	}
	return result
}

func joinRawJSONPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func rawJSONStringField(node *rawJSONNode, name string) (string, bool) {
	if node == nil || node.kind != '{' {
		return "", false
	}
	for index := len(node.fields) - 1; index >= 0; index-- {
		field := node.fields[index]
		if field.name == name && field.value.kind == '"' {
			return field.value.text, true
		}
	}
	return "", false
}

func filterKnownJSON(raw []byte, node *rawJSONNode, schema *rawJSONSchema) []byte {
	if node == nil || schema == nil {
		return raw
	}
	if schema.variant != nil {
		v := schema.variant(node)
		if v == nil {
			var buf []byte
			buf = append(buf, '{')
			first := true
			for _, f := range node.fields {
				if f.name == "type" {
					if !first {
						buf = append(buf, ',')
					}
					first = false
					buf = append(buf, raw[f.keyOffset:f.value.end]...)
				}
			}
			buf = append(buf, '}')
			return buf
		}
		schema = v
	}
	if node.kind == '{' && schema.fields != nil {
		var buf []byte
		buf = append(buf, '{')
		first := true
		for _, f := range node.fields {
			childSchema, isKnown := schema.fields[f.name]
			if !isKnown {
				continue
			}
			if !first {
				buf = append(buf, ',')
			}
			first = false
			keyBytes := raw[f.keyOffset:scanRawJSONString(raw, f.keyOffset)]
			buf = append(buf, keyBytes...)
			buf = append(buf, ':')
			if childSchema != nil && (f.value.kind == '{' || f.value.kind == '[') {
				buf = append(buf, filterKnownJSON(raw, f.value, childSchema)...)
			} else {
				buf = append(buf, raw[f.value.start:f.value.end]...)
			}
		}
		buf = append(buf, '}')
		return buf
	}
	if node.kind == '[' && schema.items != nil {
		var buf []byte
		buf = append(buf, '[')
		for i, item := range node.items {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = append(buf, filterKnownJSON(raw, item, schema.items)...)
		}
		buf = append(buf, ']')
		return buf
	}
	return raw[node.start:node.end]
}
