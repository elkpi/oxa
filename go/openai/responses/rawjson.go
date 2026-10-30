package responses

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
	opaque  bool
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
		return nil, nil, fmt.Errorf("responses: request JSON: %w", err)
	}
	schema := schemaForRawType(reflect.TypeOf(Request{}))
	cleanJSON := filterKnownJSON(raw, root, schema)
	cleanRoot, err := parseRawJSON(cleanJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("responses: request JSON: %w", err)
	}
	masked, format := maskRawFormat(cleanJSON, cleanRoot, []string{"text", "format"})
	var wire Request
	if err := json.Unmarshal(masked, &wire); err != nil {
		return nil, nil, fmt.Errorf("responses: request JSON: %w", err)
	}
	if wire.Text != nil && len(format) > 0 && string(format) != "null" {
		wire.Text.Format = format
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
		return nil, nil, fmt.Errorf("responses: response JSON: %w", err)
	}
	schema := schemaForRawType(reflect.TypeOf(Response{}))
	cleanJSON := filterKnownJSON(raw, root, schema)
	var wire Response
	if err := json.Unmarshal(cleanJSON, &wire); err != nil {
		return nil, nil, fmt.Errorf("responses: response JSON: %w", err)
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
		return nil, fmt.Errorf("responses: stream event JSON: %w", err)
	}
	kind, _ := rawJSONStringField(root, "type")
	if kind == EventTypeResponseCustomToolInputDelta || kind == EventTypeResponseCustomToolInputDone {
		var required struct {
			Delta       *string `json:"delta"`
			Input       *string `json:"input"`
			OutputIndex *int    `json:"output_index"`
			ItemID      *string `json:"item_id"`
		}
		if err := json.Unmarshal(raw, &required); err != nil {
			return nil, err
		}
		if required.OutputIndex == nil || required.ItemID == nil || (kind == EventTypeResponseCustomToolInputDelta && required.Delta == nil) || (kind == EventTypeResponseCustomToolInputDone && required.Input == nil) {
			return nil, fmt.Errorf("responses: custom input event requires typed identity and input/delta fields")
		}
	}
	var event StreamEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("responses: stream event JSON: %w", err)
	}
	audited, locations := inspectRawJSON(root, schemaForRawType(reflect.TypeOf(StreamEvent{})), "")
	before := len(d.losses)
	events, err := d.Feed(&event)
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
	case reflect.TypeOf(Input{}):
		schema := &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(InputItem{}), seen)}
		seen[t] = schema
		return schema
	case reflect.TypeOf(InputItem{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, present := rawJSONStringField(node, "type")
			if !present || kind == "" || kind == ItemTypeMessage {
				return rawObjectSchema(t, []string{"type", "role", "content"}, map[string]*rawJSONSchema{
					"content": {items: schemaForRawTypeSeen(reflect.TypeOf(ContentPart{}), seen)},
				}, seen)
			}
			switch kind {
			case ItemTypeCustomToolCall:
				return rawObjectSchema(t, []string{"type", "call_id", "name", "namespace", "input", "id"}, nil, seen)
			case ItemTypeCustomToolCallOutput:
				return rawObjectSchema(t, []string{"type", "call_id", "output", "id"}, map[string]*rawJSONSchema{"output": rawCustomOutputSchema(seen)}, seen)
			case ItemTypeApplyPatchCall:
				return rawObjectSchema(t, []string{"type", "id", "call_id", "status", "operation"}, map[string]*rawJSONSchema{
					"operation": rawApplyPatchOperationSchema(seen),
				}, seen)
			case ItemTypeApplyPatchCallOutput:
				return rawObjectSchema(t, []string{"type", "id", "call_id", "status", "output"}, nil, seen)
			case ItemTypeFunctionCall:
				return rawObjectSchema(t, []string{"type", "call_id", "name", "namespace", "arguments"}, nil, seen)
			case ItemTypeFunctionCallOutput:
				return rawObjectSchema(t, []string{"type", "call_id", "output"}, nil, seen)
			case ItemTypeReasoning:
				return rawObjectSchema(t, []string{"type", "summary"}, nil, seen)
			case ItemTypeWebSearchCall:
				return rawObjectSchema(t, []string{"type", "id", "status", "action"}, nil, seen)
			case ItemTypeAdditionalTools:
				return rawObjectSchema(t, []string{"type", "role", "tools"}, map[string]*rawJSONSchema{
					"tools": {items: schemaForRawTypeSeen(reflect.TypeOf(ToolDef{}), seen)},
				}, seen)
			default:
				return nil
			}
		}
		return schema
	case reflect.TypeOf(ContentPart{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case PartTypeInputText, PartTypeOutputText, PartTypeInputImage:
				return rawObjectSchema(t, []string{"type", "text", "image_url"}, nil, seen)
			default:
				return nil
			}
		}
		return schema
	case reflect.TypeOf(ToolDef{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			if kind == ToolTypeApplyPatch {
				return rawObjectSchema(t, []string{"type"}, nil, seen)
			}
			if kind == ToolTypeCustom {
				return rawObjectSchema(t, []string{"type", "name", "description", "format", "defer_loading"}, nil, seen)
			}
			if kind == ToolTypeNamespace || kind == ToolTypeToolbox {
				return rawObjectSchema(t, []string{"type", "name", "description", "tools"}, map[string]*rawJSONSchema{
					"tools": {items: schemaForRawTypeSeen(t, seen)},
				}, seen)
			}
			if kind == ToolTypeFunction {
				return rawObjectSchema(t, []string{"type", "name", "description", "parameters", "strict", "defer_loading"}, nil, seen)
			}
			if isResponsesNativeTool(kind, node) {
				return &rawJSONSchema{opaque: true}
			}
			return nil
		}
		return schema
	case reflect.TypeOf(CustomToolFormatWire{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			if kind == "text" {
				return rawObjectSchema(t, []string{"type"}, nil, seen)
			}
			if kind == "grammar" {
				return rawObjectSchema(t, []string{"type", "syntax", "definition"}, nil, seen)
			}
			return nil
		}
		return schema
	case reflect.TypeOf(OutputItem{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case ItemTypeApplyPatchCall:
				return rawObjectSchema(t, []string{"type", "id", "status", "call_id", "operation"}, map[string]*rawJSONSchema{
					"operation": rawApplyPatchOperationSchema(seen),
				}, seen)
			case ItemTypeApplyPatchCallOutput:
				return rawObjectSchema(t, []string{"type", "id", "status", "call_id", "output"}, nil, seen)
			case ItemTypeMessage:
				return rawObjectSchema(t, []string{"type", "id", "status", "role", "content"}, nil, seen)
			case ItemTypeCustomToolCall:
				return rawObjectSchema(t, []string{"type", "id", "call_id", "name", "namespace", "input"}, nil, seen)
			case ItemTypeFunctionCall:
				return rawObjectSchema(t, []string{"type", "id", "status", "call_id", "name", "namespace", "arguments"}, nil, seen)
			case ItemTypeReasoning:
				return rawObjectSchema(t, []string{"type", "id", "status", "summary"}, nil, seen)
			case ItemTypeWebSearchCall:
				return rawObjectSchema(t, []string{"type", "id", "status", "action"}, nil, seen)
			default:
				return nil
			}
		}
		return schema
	case reflect.TypeOf(OutputContent{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case PartTypeOutputText, PartTypeSummaryText:
				return rawObjectSchema(t, []string{"type", "text", "annotations"}, nil, seen)
			default:
				return nil
			}
		}
		return schema
	case reflect.TypeOf(StreamEvent{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case EventTypeResponseCreated, EventTypeResponseCompleted, EventTypeResponseIncomplete, EventTypeResponseFailed:
				return rawObjectSchema(t, []string{"type", "response", "sequence_number"}, nil, seen)
			case EventTypeResponseOutputItemAdded, EventTypeResponseOutputItemDone:
				return rawObjectSchema(t, []string{"type", "output_index", "item", "sequence_number"}, nil, seen)
			case EventTypeResponseContentPartAdded, EventTypeResponseContentPartDone,
				EventTypeResponseReasoningSummaryPartAdded, EventTypeResponseReasoningSummaryPartDone:
				return rawObjectSchema(t, []string{"type", "item_id", "output_index", "content_index", "part", "sequence_number"}, nil, seen)
			case EventTypeResponseOutputTextDelta, EventTypeResponseReasoningSummaryTextDelta:
				return rawObjectSchema(t, []string{"type", "item_id", "output_index", "content_index", "delta", "sequence_number"}, nil, seen)
			case EventTypeResponseOutputTextDone, EventTypeResponseReasoningSummaryTextDone:
				return rawObjectSchema(t, []string{"type", "item_id", "output_index", "content_index", "text", "sequence_number"}, nil, seen)
			case EventTypeResponseCustomToolInputDone:
				return rawObjectSchema(t, []string{"type", "item_id", "output_index", "input", "sequence_number"}, nil, seen)
			case EventTypeResponseFunctionCallArgsDelta, EventTypeResponseCustomToolInputDelta:
				return rawObjectSchema(t, []string{"type", "item_id", "output_index", "delta", "sequence_number"}, nil, seen)
			case EventTypeResponseFunctionCallArgsDone:
				return rawObjectSchema(t, []string{"type", "item_id", "output_index", "call_id", "name", "arguments", "sequence_number"}, nil, seen)
			default:
				return nil
			}
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
			schema.fields["input"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(InputItem{}), seen)}
			schema.fields["tool_choice"] = &rawJSONSchema{variant: func(node *rawJSONNode) *rawJSONSchema {
				if node == nil || node.kind != '{' {
					return nil
				}
				kind, _ := rawJSONStringField(node, "type")
				if kind == ToolTypeApplyPatch {
					return rawObjectSchema(reflect.TypeOf(ToolChoiceWire{}), []string{"type"}, nil, seen)
				}
				return rawObjectSchema(reflect.TypeOf(ToolChoiceWire{}), []string{"type", "name", "namespace"}, nil, seen)
			}}
			schema.fields["reasoning"] = &rawJSONSchema{fields: map[string]*rawJSONSchema{"effort": nil, "summary": nil}}
			delete(schema.fields, "metadata")
			delete(schema.fields, "parallel_tool_calls")
			delete(schema.fields, "store")
		case reflect.TypeOf(TextParams{}):
			schema.fields["format"] = schemaForRawTypeSeen(reflect.TypeOf(TextFormatWire{}), seen)
			delete(schema.fields, "verbosity")
		case reflect.TypeOf(InputItem{}):
			schema.fields["content"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(ContentPart{}), seen)}
		}
		return schema
	default:
		return nil
	}
}

func rawApplyPatchOperationSchema(seen map[reflect.Type]*rawJSONSchema) *rawJSONSchema {
	t := reflect.TypeOf(ApplyPatchOperationWire{})
	return &rawJSONSchema{variant: func(node *rawJSONNode) *rawJSONSchema {
		kind, _ := rawJSONStringField(node, "type")
		switch kind {
		case ir.ApplyPatchOperationCreateFile, ir.ApplyPatchOperationUpdateFile:
			return rawObjectSchema(t, []string{"type", "path", "diff"}, nil, seen)
		case ir.ApplyPatchOperationDeleteFile:
			return rawObjectSchema(t, []string{"type", "path"}, nil, seen)
		default:
			return nil
		}
	}}
}

func rawCustomOutputSchema(seen map[reflect.Type]*rawJSONSchema) *rawJSONSchema {
	return &rawJSONSchema{items: &rawJSONSchema{variant: func(node *rawJSONNode) *rawJSONSchema {
		kind, _ := rawJSONStringField(node, "type")
		switch kind {
		case PartTypeInputText:
			return rawObjectSchema(reflect.TypeOf(ContentPart{}), []string{"type", "text"}, nil, seen)
		case PartTypeInputImage:
			return rawObjectSchema(reflect.TypeOf(ContentPart{}), []string{"type", "image_url"}, nil, seen)
		default:
			return nil
		}
	}}}
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
			if shape == nil || shape.opaque {
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
		absorbed := false
		for _, parent := range existing {
			if parent.Reason == ir.LossUnsupportedSemantic && (strings.HasPrefix(loss.loss.Path, parent.Path+".") || strings.HasPrefix(loss.loss.Path, parent.Path+"[")) {
				absorbed = true
				break
			}
		}
		if absorbed {
			continue
		}
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

func isResponsesNativeTool(kind string, node *rawJSONNode) bool {
	if kind == "" || kind == ToolTypeFunction || kind == ToolTypeCustom || kind == ToolTypeApplyPatch || kind == ToolTypeNamespace || kind == ToolTypeToolbox {
		return false
	}
	if node != nil {
		for _, f := range node.fields {
			if f.name == "name" && f.value != nil && f.value.kind == '"' && len(f.value.text) > 0 {
				return true
			}
			if f.name == "server_label" && f.value != nil && f.value.kind == '"' && len(f.value.text) > 0 {
				return true
			}
		}
	}
	return isKnownOpenAINativeType(kind)
}

func isKnownOpenAINativeType(kind string) bool {
	switch kind {
	case "web_search", "file_search", "code_interpreter", "computer", "computer_use_preview", "mcp", "tool_search", "image_generation", "local_shell", "shell", "programmatic_tool_calling":
		return true
	}
	if strings.HasPrefix(kind, "web_search_") || strings.HasPrefix(kind, "file_search_") || strings.HasPrefix(kind, "computer_") {
		return true
	}
	return false
}

func filterKnownJSON(raw []byte, node *rawJSONNode, schema *rawJSONSchema) []byte {
	if node == nil || schema == nil {
		return raw
	}
	if schema.opaque {
		return raw[node.start:node.end]
	}
	if schema.variant != nil {
		if kind, ok := rawJSONStringField(node, "type"); ok && kind == ItemTypeWebSearchCall {
			return raw[node.start:node.end]
		}
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
		if v.opaque {
			return raw[node.start:node.end]
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
