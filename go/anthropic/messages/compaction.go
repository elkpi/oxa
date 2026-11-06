package messages

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v3/ir"
)

func decodeCompaction(w BlockWire, path string) (ir.CompactionBlock, error) {
	var fields map[string]json.RawMessage
	if len(w.rawChange) == 0 {
		raw, err := json.Marshal(w)
		if err != nil {
			return ir.CompactionBlock{}, err
		}
		w.rawChange = raw
	}
	if err := json.Unmarshal(w.rawChange, &fields); err != nil || fields == nil {
		return ir.CompactionBlock{}, fmt.Errorf("anthropic: %s must be an object", path)
	}
	b := ir.CompactionBlock{}
	if value, ok := fields["content"]; ok {
		if string(value) == "null" {
			b.NullContent = true
		} else {
			var text string
			if err := json.Unmarshal(value, &text); err != nil || text == "" {
				return b, fmt.Errorf("anthropic: %s.content must be a nonempty string or null", path)
			}
			b.Content = &text
		}
	}
	for _, f := range []struct {
		key     string
		value   *string
		present *bool
	}{{"encrypted_content", &b.EncryptedContent, &b.HasEncryptedContent}, {"signature", &b.Signature, &b.HasSignature}} {
		if value, ok := fields[f.key]; ok {
			if string(value) == "null" || json.Unmarshal(value, f.value) != nil {
				return b, fmt.Errorf("anthropic: %s.%s must be a string", path, f.key)
			}
			*f.present = true
		}
	}
	if raw, ok := fields["tool_changes"]; ok {
		var items []json.RawMessage
		if string(raw) == "null" || json.Unmarshal(raw, &items) != nil {
			return b, fmt.Errorf("anthropic: %s.tool_changes must be an array", path)
		}
		b.ToolChanges = make([]ir.ToolChange, 0, len(items))
		for i, item := range items {
			var change BlockWire
			if err := json.Unmarshal(item, &change); err != nil {
				return b, fmt.Errorf("anthropic: %s.tool_changes[%d]: %w", path, i, err)
			}
			if change.Type != "tool_addition" && change.Type != "tool_removal" {
				return b, fmt.Errorf("anthropic: %s.tool_changes[%d]: invalid change type", path, i)
			}
			decoded, losses, err := decodeToolChange(change, fmt.Sprintf("%s.tool_changes[%d]", path, i))
			if err != nil {
				return b, err
			}
			if len(losses) > 0 {
				return b, fmt.Errorf("anthropic: %s.tool_changes[%d]: cannot preserve cache control", path, i)
			}
			b.ToolChanges = append(b.ToolChanges, *decoded)
		}
	}
	for k, v := range fields {
		switch k {
		case "type", "content", "encrypted_content", "signature", "tool_changes", "cache_control":
		default:
			if b.Extra == nil {
				b.Extra = map[string]json.RawMessage{}
			}
			b.Extra[k] = v
		}
	}
	return b, nil
}

func encodeCompaction(b ir.CompactionBlock) (json.RawMessage, error) {
	obj := make(map[string]any, len(b.Extra)+6)
	for k, v := range b.Extra {
		obj[k] = v
	}
	obj["type"] = "compaction"
	if b.Content != nil {
		if *b.Content == "" || b.NullContent {
			return nil, fmt.Errorf("invalid compaction content")
		}
		obj["content"] = *b.Content
	} else if b.NullContent {
		obj["content"] = nil
	}
	if b.HasEncryptedContent || b.EncryptedContent != "" {
		obj["encrypted_content"] = b.EncryptedContent
	}
	if b.HasSignature || b.Signature != "" {
		obj["signature"] = b.Signature
	}
	if b.ToolChanges != nil {
		items := make([]json.RawMessage, 0, len(b.ToolChanges))
		for i := range b.ToolChanges {
			raw, err := encodeToolChange(&b.ToolChanges[i])
			if err != nil {
				return nil, fmt.Errorf("tool_changes[%d]: %w", i, err)
			}
			items = append(items, raw)
		}
		obj["tool_changes"] = items
	}
	for _, ann := range b.Native {
		if ann.Source == ir.NativeSourceAnthropic && ann.Kind == ir.NativeKindCacheControl {
			obj["cache_control"] = json.RawMessage(ann.Value)
		}
	}
	return json.Marshal(obj)
}
