package messages

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

// rawMessageShadow mirrors the wire Request while keeping message and system
// content as raw JSON so tool_use.input source bytes survive (INV-1, N-AN-4).
type rawMessageShadow struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type rawRequestShadow struct {
	Model             string                 `json:"model"`
	System            json.RawMessage        `json:"system"`
	Messages          []rawMessageShadow     `json:"messages"`
	MaxTokens         int64                  `json:"max_tokens"`
	Temperature       *float64               `json:"temperature"`
	TopP              *float64               `json:"top_p"`
	StopSequences     []string               `json:"stop_sequences"`
	Metadata          json.RawMessage        `json:"metadata"`
	Tools             []ToolWire             `json:"tools"`
	ToolChoice        *ToolChoiceWire        `json:"tool_choice"`
	Thinking          *ThinkingWire          `json:"thinking"`
	CacheControl      json.RawMessage        `json:"cache_control"`
	OutputConfig      *OutputConfigWire      `json:"output_config"`
	ContextManagement *ContextManagementWire `json:"context_management"`
}

func DecodeRequestJSON(raw []byte, opts ...Option) (*ir.Request, []ir.Loss, error) {
	root, err := parseRawJSON(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: request JSON: %w", err)
	}
	schema := schemaForRawType(reflect.TypeOf(Request{}))
	cleanJSON := filterKnownJSON(raw, root, schema)
	var shadow rawRequestShadow
	if err := json.Unmarshal(cleanJSON, &shadow); err != nil {
		return nil, nil, fmt.Errorf("anthropic: request JSON: %w", err)
	}
	wire := Request{
		Model:             shadow.Model,
		MaxTokens:         shadow.MaxTokens,
		Temperature:       shadow.Temperature,
		TopP:              shadow.TopP,
		StopSequences:     shadow.StopSequences,
		Tools:             shadow.Tools,
		ToolChoice:        shadow.ToolChoice,
		Thinking:          shadow.Thinking,
		CacheControl:      shadow.CacheControl,
		OutputConfig:      shadow.OutputConfig,
		ContextManagement: shadow.ContextManagement,
	}
	if len(shadow.Metadata) > 0 && string(shadow.Metadata) != "null" {
		wire.Metadata = shadow.Metadata
	}
	system, err := systemFromRaw(shadow.System)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: request JSON: %w", err)
	}
	wire.System = system
	for _, message := range shadow.Messages {
		content, err := contentFromRaw(message.Content)
		if err != nil {
			return nil, nil, fmt.Errorf("anthropic: request JSON: %w", err)
		}
		wire.Messages = append(wire.Messages, Message{Role: message.Role, Content: content})
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
		return nil, nil, fmt.Errorf("anthropic: response JSON: %w", err)
	}
	var wire Response
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, nil, fmt.Errorf("anthropic: response JSON: %w", err)
	}
	resp, losses, err := DecodeResponse(&wire, opts...)
	if err != nil {
		return nil, nil, err
	}
	audited, locations := inspectRawJSON(root, schemaForRawType(reflect.TypeOf(Response{})), "")
	return resp, orderRawLosses(losses, audited, locations), nil
}

func (d *StreamDecoder) FeedJSON(raw []byte) ([]ir.Event, error) {
	root, err := parseRawJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("anthropic: stream event JSON: %w", err)
	}
	schema := schemaForRawType(reflect.TypeOf(StreamEvent{}))
	cleanJSON := filterKnownJSON(raw, root, schema)
	var event StreamEvent
	if err := json.Unmarshal(cleanJSON, &event); err != nil {
		return nil, fmt.Errorf("anthropic: stream event JSON: %w", err)
	}
	isSkipped := false
	switch event.Type {
	case EventTypeContentBlockStart:
		if event.ContentBlock != nil && event.ContentBlock.Type != BlockTypeText && event.ContentBlock.Type != BlockTypeThinking && event.ContentBlock.Type != BlockTypeToolUse {
			isSkipped = true
		}
	case EventTypeContentBlockDelta, EventTypeContentBlockStop:
		if d.skipped[event.Index] {
			isSkipped = true
		}
	}
	audited, locations := inspectRawJSON(root, schema, "")
	if isSkipped {
		var filtered []rawJSONLoss
		for _, l := range audited {
			if l.loss.Path == "type" || l.loss.Path == "index" {
				filtered = append(filtered, l)
			}
		}
		audited = filtered
	}
	before := len(d.losses)
	events, err := d.Feed(&event)
	if err != nil {
		return nil, err
	}
	ordered := orderRawLosses(d.losses[before:], audited, locations)
	d.losses = append(d.losses[:before], ordered...)
	return events, nil
}

// systemFromRaw converts raw system JSON into the typed shapes decodeSystem
// consumes without a generic round-trip: a string stays a string, and block
// arrays become []SystemBlockWire with cache_control source bytes intact.
func systemFromRaw(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return text, nil
	}
	var blocks []SystemBlockWire
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// contentFromRaw converts raw message content into the shapes decodeBlocks
// consumes: a string stays a string, and block arrays become []BlockWire whose
// tool_use.input keeps the exact source bytes of the input node.
func contentFromRaw(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return text, nil
	}
	var nodes []json.RawMessage
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, err
	}
	blocks := make([]BlockWire, 0, len(nodes))
	for _, node := range nodes {
		block, err := blockFromRaw(node)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func blockFromRaw(raw json.RawMessage) (BlockWire, error) {
	node, err := parseRawJSON(raw)
	if err != nil {
		var block BlockWire
		err := json.Unmarshal(raw, &block)
		return block, err
	}
	blockSchema := schemaForRawType(reflect.TypeOf(BlockWire{}))
	cleanJSON := filterKnownJSON(raw, node, blockSchema)
	var block BlockWire
	if err := json.Unmarshal(cleanJSON, &block); err != nil {
		return BlockWire{}, err
	}
	if block.Type == BlockTypeToolUse {
		for _, f := range node.fields {
			if f.name == "input" {
				block.Input = raw[f.value.start:f.value.end]
				break
			}
		}
	} else if block.Type == BlockTypeToolResult {
		var nodes []json.RawMessage
		if err := json.Unmarshal(raw, &shadowBlockContent{Content: &nodes}); err == nil && nodes != nil {
			nested := make([]BlockWire, 0, len(nodes))
			for _, childRaw := range nodes {
				child, err := blockFromRaw(childRaw)
				if err != nil {
					return BlockWire{}, err
				}
				nested = append(nested, child)
			}
			block.Content = nested
		}
	}
	return block, nil
}

// shadowBlockContent re-reads only a tool_result's content array, leaving the
// string shorthand untouched for the normal decode path.
type shadowBlockContent struct {
	Content *[]json.RawMessage `json:"content"`
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
	case reflect.TypeOf(SystemBlockWire{}):
		return rawObjectSchema(t, []string{"type", "text", "cache_control"}, nil, seen)
	case reflect.TypeOf(BlockWire{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case BlockTypeText:
				return rawObjectSchema(t, []string{"type", "text", "cache_control"}, nil, seen)
			case BlockTypeThinking:
				return rawObjectSchema(t, []string{"type", "thinking", "signature", "cache_control"}, nil, seen)
			case BlockTypeToolUse:
				return rawObjectSchema(t, []string{"type", "id", "name", "input", "cache_control"}, nil, seen)
			case BlockTypeToolResult:
				return rawObjectSchema(t, []string{"type", "tool_use_id", "content", "is_error", "cache_control"},
					map[string]*rawJSONSchema{
						"content": {variant: func(content *rawJSONNode) *rawJSONSchema {
							if content != nil && content.kind == '[' {
								return &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(BlockWire{}), seen)}
							}
							return nil
						}},
					},
					seen)
			case BlockTypeImage:
				return rawObjectSchema(t, []string{"type", "source", "cache_control"},
					map[string]*rawJSONSchema{
						"source": schemaForRawTypeSeen(reflect.TypeOf(SourceWire{}), seen),
					},
					seen)
			default:
				return nil
			}
		}
		return schema
	case reflect.TypeOf(SourceWire{}):
		return rawObjectSchema(t, []string{"type", "media_type", "data", "url"}, nil, seen)
	case reflect.TypeOf(ToolWire{}):
		return rawObjectSchema(t, []string{"name", "description", "input_schema", "cache_control"}, nil, seen)
	case reflect.TypeOf(ToolChoiceWire{}):
		return rawObjectSchema(t, []string{"type", "name"}, nil, seen)
	case reflect.TypeOf(ThinkingWire{}):
		return rawObjectSchema(t, []string{"type", "budget_tokens", "display"}, nil, seen)
	case reflect.TypeOf(OutputConfigWire{}):
		return rawObjectSchema(t, []string{"effort"}, nil, seen)
	case reflect.TypeOf(ContextManagementWire{}):
		return rawObjectSchema(t, []string{"edits"}, nil, seen)
	case reflect.TypeOf(Message{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			return rawObjectSchema(t, []string{"role", "content"},
				map[string]*rawJSONSchema{
					"content": {variant: func(content *rawJSONNode) *rawJSONSchema {
						if content != nil && content.kind == '[' {
							return &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(BlockWire{}), seen)}
						}
						return nil
					}},
				},
				seen)
		}
		return schema
	case reflect.TypeOf(UsageWire{}):
		return rawObjectSchema(t, []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"}, nil, seen)
	case reflect.TypeOf(Response{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			return rawObjectSchema(t, []string{"id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage"},
				map[string]*rawJSONSchema{
					"content": {items: schemaForRawTypeSeen(reflect.TypeOf(BlockWire{}), seen)},
				},
				seen)
		}
		return schema
	case reflect.TypeOf(StreamDelta{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case DeltaTypeTextDelta:
				return rawObjectSchema(t, []string{"type", "text"}, nil, seen)
			case DeltaTypeInputJSONDelta:
				return rawObjectSchema(t, []string{"type", "partial_json"}, nil, seen)
			case DeltaTypeThinking:
				return rawObjectSchema(t, []string{"type", "thinking"}, nil, seen)
			case DeltaTypeSignature:
				return rawObjectSchema(t, []string{"type", "signature"}, nil, seen)
			default:
				return rawObjectSchema(t, []string{"stop_reason", "stop_sequence"}, nil, seen)
			}
		}
		return schema
	case reflect.TypeOf(StreamEvent{}):
		schema := &rawJSONSchema{}
		seen[t] = schema
		schema.variant = func(node *rawJSONNode) *rawJSONSchema {
			kind, _ := rawJSONStringField(node, "type")
			switch kind {
			case EventTypeMessageStart:
				return rawObjectSchema(t, []string{"type", "message"},
					map[string]*rawJSONSchema{"message": schemaForRawTypeSeen(reflect.TypeOf(Response{}), seen)}, seen)
			case EventTypeContentBlockStart:
				return rawObjectSchema(t, []string{"type", "index", "content_block"},
					map[string]*rawJSONSchema{"content_block": schemaForRawTypeSeen(reflect.TypeOf(BlockWire{}), seen)}, seen)
			case EventTypeContentBlockDelta:
				return rawObjectSchema(t, []string{"type", "index", "delta"},
					map[string]*rawJSONSchema{"delta": schemaForRawTypeSeen(reflect.TypeOf(StreamDelta{}), seen)}, seen)
			case EventTypeContentBlockStop:
				return rawObjectSchema(t, []string{"type", "index"}, nil, seen)
			case EventTypeMessageDelta:
				return rawObjectSchema(t, []string{"type", "delta", "usage"},
					map[string]*rawJSONSchema{"delta": schemaForRawTypeSeen(reflect.TypeOf(StreamDelta{}), seen)}, seen)
			case EventTypeMessageStop:
				return rawObjectSchema(t, []string{"type"}, nil, seen)
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
			schema.fields["system"] = &rawJSONSchema{variant: func(node *rawJSONNode) *rawJSONSchema {
				if node != nil && node.kind == '[' {
					return &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(SystemBlockWire{}), seen)}
				}
				return nil
			}}
			schema.fields["messages"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(Message{}), seen)}
			schema.fields["tools"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(ToolWire{}), seen)}
			delete(schema.fields, "metadata")
		case reflect.TypeOf(Response{}):
			schema.fields["content"] = &rawJSONSchema{items: schemaForRawTypeSeen(reflect.TypeOf(BlockWire{}), seen)}
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
		if loss.Path == "stop_reason" {
			if offset, ok := locations.keys["delta.stop_reason"]; ok {
				return offset
			}
			if offset, ok := locations.values["delta.stop_reason"]; ok {
				return offset
			}
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
