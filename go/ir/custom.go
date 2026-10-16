package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	BlockTypeCustomToolUse    = "custom_tool_use"
	BlockTypeCustomToolResult = "custom_tool_result"
	DeltaTypeCustomToolInput  = "custom_tool_input_delta"
)

type CustomToolFormat struct {
	Type       string `json:"type"`
	Syntax     string `json:"syntax,omitempty"`
	Definition string `json:"definition,omitempty"`
}

func (f CustomToolFormat) MarshalJSON() ([]byte, error) {
	switch f.Type {
	case "text":
		if f.Syntax != "" || f.Definition != "" {
			return nil, fmt.Errorf("ir: text format cannot carry grammar fields")
		}
		return []byte(`{"type":"text"}`), nil
	case "grammar":
		if f.Syntax != "lark" && f.Syntax != "regex" {
			return nil, fmt.Errorf("ir: unknown custom grammar syntax %q", f.Syntax)
		}
		return json.Marshal(struct {
			Type       string `json:"type"`
			Syntax     string `json:"syntax"`
			Definition string `json:"definition"`
		}{f.Type, f.Syntax, f.Definition})
	default:
		return nil, fmt.Errorf("ir: unknown custom format %q", f.Type)
	}
}

func (f *CustomToolFormat) UnmarshalJSON(data []byte) error {
	var w struct {
		Type       string  `json:"type"`
		Syntax     string  `json:"syntax"`
		Definition *string `json:"definition"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	if w.Type == "grammar" && w.Definition == nil {
		return fmt.Errorf("ir: grammar definition is required")
	}
	f.Type, f.Syntax = w.Type, w.Syntax
	if w.Definition != nil {
		f.Definition = *w.Definition
	}
	_, err := f.MarshalJSON()
	return err
}

// Input 和 Text 保存 JSON 字符串外壳，不将其中的任意文本解释为 JSON 参数。
type CustomToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type CustomToolResultBlock struct {
	ToolUseID string           `json:"tool_use_id"`
	Output    CustomToolOutput `json:"output"`
}

type CustomToolOutput struct {
	Text    *string
	Content []Block
}

type CustomToolInputDelta struct {
	Text json.RawMessage `json:"text"`
}

func (CustomToolUseBlock) isBlock()    {}
func (CustomToolResultBlock) isBlock() {}
func (CustomToolInputDelta) isDelta()  {}

func (o CustomToolOutput) MarshalJSON() ([]byte, error) {
	if o.Text != nil {
		if o.Content != nil {
			return nil, fmt.Errorf("ir: custom output must be string or content list, not both")
		}
		return json.Marshal(*o.Text)
	}
	blocks := make([]json.RawMessage, 0, len(o.Content))
	for _, b := range o.Content {
		switch b.(type) {
		case TextBlock, ImageBlock:
		default:
			return nil, fmt.Errorf("ir: custom output cannot contain %T", b)
		}
		raw, err := MarshalBlock(b)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, raw)
	}
	return json.Marshal(blocks)
}

func (o *CustomToolOutput) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return fmt.Errorf("ir: custom output is required")
	}
	*o = CustomToolOutput{}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		o.Text = &text
		return nil
	}
	if data[0] != '[' {
		return fmt.Errorf("ir: custom output must be a string or content list")
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	o.Content = make([]Block, 0, len(blocks))
	for _, raw := range blocks {
		b, err := UnmarshalBlock(raw)
		if err != nil {
			return err
		}
		switch b.(type) {
		case TextBlock, ImageBlock:
		default:
			return fmt.Errorf("ir: custom output cannot contain %T", b)
		}
		o.Content = append(o.Content, b)
	}
	return nil
}

func validateOpaqueString(raw json.RawMessage) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '"' {
		return fmt.Errorf("ir: input must be a JSON string token")
	}
	var text string
	return json.Unmarshal(raw, &text)
}

func validateTool(t Tool) error {
	if t.Type == ToolTypeCustom {
		if t.Name == "" || len(t.InputSchema) != 0 {
			return fmt.Errorf("ir: custom tool requires a name and cannot carry input_schema")
		}
		if t.Format != nil {
			_, err := t.Format.MarshalJSON()
			return err
		}
		return nil
	}
	if t.Type != "" && t.Type != ToolTypeFunction {
		return fmt.Errorf("ir: unknown tool type %q", t.Type)
	}
	if t.Format != nil {
		return fmt.Errorf("ir: function tool cannot carry custom format")
	}
	return nil
}

func validateToolChoice(t *ToolChoice) error {
	if t == nil {
		return nil
	}
	if t.ToolType != "" && (t.Mode != ToolChoiceTool || (t.ToolType != ToolTypeFunction && t.ToolType != ToolTypeCustom)) {
		return fmt.Errorf("ir: tool_type requires a named function/custom choice")
	}
	if t.ToolType == ToolTypeCustom && t.Name == "" {
		return fmt.Errorf("ir: custom tool choice requires name")
	}
	return nil
}

func ValidateCustomConversation(req *Request) error {
	hasCustom := false
	for _, m := range req.Messages {
		for _, b := range m.Content {
			switch b.(type) {
			case CustomToolUseBlock, CustomToolResultBlock:
				hasCustom = true
			}
		}
	}
	if !hasCustom {
		return nil
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != RoleUser {
		return fmt.Errorf("ir: first message must be user (INV-2)")
	}
	type call struct{ id, kind string }
	var pending []call
	for i, m := range req.Messages {
		var calls, results []call
		for _, b := range m.Content {
			switch v := b.(type) {
			case ToolUseBlock:
				calls = append(calls, call{v.ID, "function"})
			case CustomToolUseBlock:
				calls = append(calls, call{v.ID, "custom"})
			case ToolResultBlock:
				results = append(results, call{v.ToolUseID, "function"})
			case CustomToolResultBlock:
				results = append(results, call{v.ToolUseID, "custom"})
			}
		}
		if len(results) > 0 || len(pending) > 0 {
			if m.Role != RoleUser || len(results) != len(pending) {
				return fmt.Errorf("ir: messages[%d] tool results do not match preceding calls (INV-3)", i)
			}
			for j, r := range results {
				if r != pending[j] {
					return fmt.Errorf("ir: messages[%d] tool result identity/type/order mismatch (INV-3)", i)
				}
			}
		}
		if len(calls) > 0 && m.Role != RoleAssistant {
			return fmt.Errorf("ir: tool calls require assistant role (INV-3)")
		}
		seen := map[string]bool{}
		for _, c := range calls {
			if c.id == "" || seen[c.id] {
				return fmt.Errorf("ir: duplicate or empty call id (INV-3)")
			}
			seen[c.id] = true
		}
		pending = calls
	}
	if len(pending) > 0 {
		return fmt.Errorf("ir: tool calls require following results (INV-3)")
	}
	return nil
}
