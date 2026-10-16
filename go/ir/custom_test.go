package ir

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCustomOpaqueStringTokens(t *testing.T) {
	const token = `"你\/\r\nnot JSON"`
	b, err := UnmarshalBlock([]byte(`{"type":"custom_tool_use","id":"c","name":"run","input":` + token + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(b.(CustomToolUseBlock).Input) != token {
		t.Fatal("custom input token normalized")
	}
	raw, err := MarshalBlock(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(token)) {
		t.Fatalf("custom token changed: %s", raw)
	}
	d, err := UnmarshalDelta([]byte(`{"type":"custom_tool_input_delta","text":` + token + `}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(d.(CustomToolInputDelta).Text) != token {
		t.Fatal("custom delta token normalized")
	}
	for _, bad := range []string{`{"type":"custom_tool_use","id":"c","name":"run","input":{}}`, `{"type":"custom_tool_use","id":"c","name":"run"}`} {
		if _, err := UnmarshalBlock([]byte(bad)); err == nil {
			t.Fatalf("invalid input accepted: %s", bad)
		}
	}
}

func TestCustomOutputIsRestrictedAndPreservesVariant(t *testing.T) {
	for _, output := range []string{`""`, `[]`, `[{"type":"text","text":""}]`} {
		b, err := UnmarshalBlock([]byte(`{"type":"custom_tool_result","tool_use_id":"c","output":` + output + `}`))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := MarshalBlock(b)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["output"]) != output {
			t.Fatalf("output variant changed: %s", raw)
		}
	}
	for _, bad := range []string{`null`, `{}`, `[{"type":"thinking","thinking":"not output"}]`, `[{"type":"tool_use","id":"x","name":"x","input":"{}"}]`} {
		if _, err := UnmarshalBlock([]byte(`{"type":"custom_tool_result","tool_use_id":"c","output":` + bad + `}`)); err == nil {
			t.Fatalf("invalid output accepted: %s", bad)
		}
	}
}

func TestCustomGrammarEmptyDefinitionAndChoiceConstraints(t *testing.T) {
	f := CustomToolFormat{Type: "grammar", Syntax: "regex", Definition: ""}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"definition":""`)) {
		t.Fatalf("empty definition omitted: %s", raw)
	}
	req := &Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: ""}}}}, Tools: []Tool{{Type: ToolTypeCustom, Name: "run", Format: &f}}, ToolChoice: &ToolChoice{Mode: ToolChoiceTool, Name: "run", ToolType: ToolTypeCustom}}
	raw, err = MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"specVersion":"0.2.0"`)) || bytes.Contains(raw, []byte(`input_schema`)) {
		t.Fatalf("custom declaration invalid: %s", raw)
	}
	req.ToolChoice.Mode = ToolChoiceAuto
	if _, err := MarshalRequest(req); err == nil {
		t.Fatal("custom type accepted outside named choice")
	}
}

func TestCustomConversationRejectsOrphanAndWrongKind(t *testing.T) {
	for _, messages := range [][]Message{
		{{Role: RoleUser, Content: []Block{CustomToolResultBlock{ToolUseID: "c"}}}},
		{{Role: RoleUser, Content: []Block{TextBlock{Text: "go"}}}, {Role: RoleAssistant, Content: []Block{CustomToolUseBlock{ID: "c", Name: "run", Input: json.RawMessage(`"code"`)}}}},
		{{Role: RoleUser, Content: []Block{TextBlock{Text: "go"}}}, {Role: RoleAssistant, Content: []Block{CustomToolUseBlock{ID: "c", Name: "run", Input: json.RawMessage(`"code"`)}}}, {Role: RoleUser, Content: []Block{ToolResultBlock{ToolUseID: "c"}}}},
	} {
		if err := ValidateCustomConversation(&Request{Messages: messages}); err == nil {
			t.Fatalf("invalid custom conversation accepted: %+v", messages)
		}
	}
}
