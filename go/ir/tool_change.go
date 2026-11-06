package ir

import (
	"encoding/json"
	"fmt"
)

type wireToolChange struct {
	Op              string                     `json:"op"`
	Tool            *wireTool                  `json:"tool,omitempty"`
	Ref             *ToolChangeRef             `json:"ref,omitempty"`
	Native          []NativeAnnotation         `json:"native,omitempty"`
	Extra           map[string]json.RawMessage `json:"extra,omitempty"`
	ToolExtra       map[string]json.RawMessage `json:"tool_extra,omitempty"`
	DefinitionExtra map[string]json.RawMessage `json:"definition_extra,omitempty"`
}

func (g *ToolChangeGroup) UnmarshalJSON(data []byte) error {
	type group ToolChangeGroup
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, key := range []string{"message_index", "content_index", "parts"} {
		if v, ok := raw[key]; !ok || string(v) == "null" {
			return fmt.Errorf("ir: tool change group requires %s", key)
		}
	}
	var decoded group
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*g = ToolChangeGroup(decoded)
	return nil
}

func (c ToolChange) MarshalJSON() ([]byte, error) {
	w := wireToolChange{Op: c.Op, Ref: c.Ref, Native: c.Native, Extra: c.Extra, ToolExtra: c.ToolExtra, DefinitionExtra: c.DefinitionExtra}
	if c.Tool != nil {
		t := wireTool(*c.Tool)
		if t.Type == ToolTypeFunction {
			t.Type = ""
		}
		w.Tool = &t
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(w)
}

func (c *ToolChange) UnmarshalJSON(data []byte) error {
	var w wireToolChange
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*c = ToolChange{Op: w.Op, Ref: w.Ref, Native: w.Native, Extra: w.Extra, ToolExtra: w.ToolExtra, DefinitionExtra: w.DefinitionExtra}
	if w.Tool != nil {
		t := Tool(*w.Tool)
		c.Tool = &t
	}
	return c.validate()
}

func (c ToolChange) validate() error {
	if c.Op != ToolChangeOpAdd && c.Op != ToolChangeOpRemove {
		return fmt.Errorf("ir: invalid tool change op %q", c.Op)
	}
	if (c.Tool == nil) == (c.Ref == nil) || (c.Op == ToolChangeOpRemove && c.Tool != nil) {
		return fmt.Errorf("ir: tool change requires a valid tool or reference")
	}
	if c.Tool != nil {
		return validateTool(*c.Tool)
	}
	switch c.Ref.Type {
	case ToolChangeRefTypeToolReference:
		if c.Ref.Name == "" || c.Ref.ServerName != "" {
			return fmt.Errorf("ir: invalid tool reference")
		}
	case ToolChangeRefTypeMCPToolReference:
		if c.Ref.Name == "" || c.Ref.ServerName == "" {
			return fmt.Errorf("ir: invalid MCP tool reference")
		}
	case ToolChangeRefTypeMCPToolsetReference:
		if c.Ref.Name != "" || c.Ref.ServerName == "" {
			return fmt.Errorf("ir: invalid MCP toolset reference")
		}
	default:
		return fmt.Errorf("ir: unknown tool change reference type %q", c.Ref.Type)
	}
	return nil
}

func (p ToolChangePart) MarshalJSON() ([]byte, error) {
	switch p.Type {
	case ToolChangePartTypeText:
		if p.Change != nil {
			return nil, fmt.Errorf("ir: text part cannot contain change")
		}
		return json.Marshal(struct {
			Type   string             `json:"type"`
			Text   string             `json:"text"`
			Native []NativeAnnotation `json:"native,omitempty"`
		}{p.Type, p.Text, p.Native})
	case ToolChangePartTypeToolChange:
		if p.Change == nil {
			return nil, fmt.Errorf("ir: tool change part requires change")
		}
		return json.Marshal(struct {
			Type   string      `json:"type"`
			Change *ToolChange `json:"change"`
		}{p.Type, p.Change})
	default:
		return nil, fmt.Errorf("ir: unknown tool change part type %q", p.Type)
	}
}

func (p *ToolChangePart) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if err := json.Unmarshal(fields["type"], &p.Type); err != nil {
		return fmt.Errorf("ir: tool change part type is required")
	}
	switch p.Type {
	case ToolChangePartTypeText:
		raw, ok := fields["text"]
		if !ok || string(raw) == "null" {
			return fmt.Errorf("ir: text part requires text")
		}
		if _, ok := fields["change"]; ok {
			return fmt.Errorf("ir: text part cannot contain change")
		}
		if err := json.Unmarshal(raw, &p.Text); err != nil {
			return fmt.Errorf("ir: text part requires string text")
		}
		if raw, ok := fields["native"]; ok {
			if err := json.Unmarshal(raw, &p.Native); err != nil {
				return fmt.Errorf("ir: text part native: %w", err)
			}
		}
	case ToolChangePartTypeToolChange:
		raw, ok := fields["change"]
		if !ok || string(raw) == "null" {
			return fmt.Errorf("ir: tool change part requires change")
		}
		if _, ok := fields["text"]; ok {
			return fmt.Errorf("ir: tool change part cannot contain text")
		}
		p.Change = new(ToolChange)
		if err := json.Unmarshal(raw, p.Change); err != nil {
			return err
		}
	default:
		return fmt.Errorf("ir: unknown tool change part type %q", p.Type)
	}
	return nil
}
