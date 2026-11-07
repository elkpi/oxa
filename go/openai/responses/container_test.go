package responses_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa-protocol/go/v3/ir"
	"github.com/elkpi/oxa-protocol/go/v3/openai/responses"
)

func TestDecodeToolContainers(t *testing.T) {
	raw := []byte(`{
		"model": "gpt-5.5",
		"input": [
			{"role": "user", "content": "use tools"},
			{"type": "additional_tools", "role": "developer", "tools": [
				{"type": "namespace", "name": "editor", "tools": [
					{"type": "custom", "name": "submit_patch", "description": "submit opaque patch"}
				]}
			]}
		],
		"tools": [
			{"type": "toolbox", "name": "repo", "tools": [
				{"type": "function", "name": "read", "parameters": {"type": "object"}}
			]}
		]
	}`)

	req, losses, err := responses.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses, got %d: %+v", len(losses), losses)
	}
	if len(req.Tools) != 1 || len(req.ToolChangeGroups) != 1 || len(req.ToolChangeGroups[0].Parts) != 1 {
		t.Fatalf("expected one initial tool and one positional change, got %+v", req)
	}

	// 1. repo.read
	t1 := req.Tools[0]
	if t1.Name != "repo.read" || t1.Type != ir.ToolTypeFunction {
		t.Errorf("expected repo.read function tool, got %+v", t1)
	}

	// 2. editor.submit_patch
	t2 := *req.ToolChangeGroups[0].Parts[0].Change.Tool
	if t2.Name != "editor.submit_patch" || t2.Type != ir.ToolTypeCustom {
		t.Errorf("expected editor.submit_patch custom tool, got %+v", t2)
	}
}

func TestDecodeToolContainerStructuralErrors(t *testing.T) {
	cases := []struct {
		name string
		json string
		err  string
	}{
		{
			name: "empty container name",
			json: `{"model":"m","input":"hi","tools":[{"type":"namespace","name":"","tools":[{"type":"function","name":"f","parameters":{}}]}]}`,
			err:  "invalid namespace name",
		},
		{
			name: "malformed container name dot prefix",
			json: `{"model":"m","input":"hi","tools":[{"type":"namespace","name":".editor","tools":[{"type":"function","name":"f","parameters":{}}]}]}`,
			err:  "invalid namespace name",
		},
		{
			name: "malformed container name consecutive dots",
			json: `{"model":"m","input":"hi","tools":[{"type":"namespace","name":"repo..tools","tools":[{"type":"function","name":"f","parameters":{}}]}]}`,
			err:  "invalid namespace name",
		},
		{
			name: "empty tools array in container",
			json: `{"model":"m","input":"hi","tools":[{"type":"namespace","name":"editor","tools":[]}]}`,
			err:  "requires non-empty tools array",
		},
		{
			name: "duplicate tool name after flattening",
			json: `{"model":"m","input":"hi","tools":[{"type":"function","name":"repo.read","parameters":{}},{"type":"toolbox","name":"repo","tools":[{"type":"function","name":"read","parameters":{}}]}]}`,
			err:  "duplicate tool name \"repo.read\"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := responses.DecodeRequestJSON([]byte(tc.json))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.err)
			}
			if !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("expected error containing %q, got %q", tc.err, err.Error())
			}
		})
	}
}

func TestDecodeEmptyAdditionalToolsIsPreserved(t *testing.T) {
	withEmpty := []byte(`{
		"model": "m",
		"input": [
			{"type": "additional_tools", "role": "developer", "tools": []},
			{"type": "message", "role": "user", "content": "hello"}
		],
		"tools": [
			{"type": "function", "name": "existing", "parameters": {"type": "object"}}
		]
	}`)
	withoutEmpty := []byte(`{
		"model": "m",
		"input": [{"type": "message", "role": "user", "content": "hello"}],
		"tools": [
			{"type": "function", "name": "existing", "parameters": {"type": "object"}}
		]
	}`)

	req, losses, err := responses.DecodeRequestJSON(withEmpty)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, _, err := responses.DecodeRequestJSON(withoutEmpty)
	if err != nil {
		t.Fatalf("unexpected baseline error: %v", err)
	}
	if len(losses) != 0 || len(req.ToolChangeGroups) != 1 || req.ToolChangeGroups[0].MessageIndex != 0 || len(req.ToolChangeGroups[0].Parts) != 0 {
		t.Fatalf("expected preserved empty group without loss, got %+v %+v", req.ToolChangeGroups, losses)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "existing" {
		t.Fatalf("expected existing tool to remain unchanged, got %+v", req.Tools)
	}
	req.ToolChangeGroups = nil
	reqJSON, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal decoded request: %v", err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal baseline request: %v", err)
	}
	if string(reqJSON) != string(expectedJSON) {
		t.Fatalf("empty additional_tools changed IR:\nwith: %s\nwithout: %s", reqJSON, expectedJSON)
	}
}

func TestDecodeAdditionalToolsRequiresExplicitArray(t *testing.T) {
	cases := []struct {
		name        string
		json        string
		errorString string
	}{
		{name: "missing tools", json: `{"model":"m","input":[{"type":"additional_tools","role":"developer"},{"role":"user","content":"hi"}]}`, errorString: "additional_tools requires tools array"},
		{name: "null tools", json: `{"model":"m","input":[{"type":"additional_tools","role":"developer","tools":null},{"role":"user","content":"hi"}]}`, errorString: "additional_tools requires tools array"},
		{name: "non-array tools", json: `{"model":"m","input":[{"type":"additional_tools","tools":"not-an-array"},{"role":"user","content":"hi"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := responses.DecodeRequestJSON([]byte(tc.json))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tc.errorString != "" && !strings.Contains(err.Error(), tc.errorString) {
				t.Fatalf("expected error containing %q, got %v", tc.errorString, err)
			}
		})
	}
}

func TestDecodeEmptyAdditionalToolsDoesNotSplitRuns(t *testing.T) {
	withEmpty := []byte(`{"model":"m","input":[{"role":"user","content":"hi"},{"type":"function_call","call_id":"a","name":"a","arguments":"{}"},{"type":"additional_tools","role":"developer","tools":[]},{"type":"function_call","call_id":"b","name":"b","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":"a"},{"type":"function_call_output","call_id":"b","output":"b"}]}`)
	withoutEmpty := []byte(`{"model":"m","input":[{"role":"user","content":"hi"},{"type":"function_call","call_id":"a","name":"a","arguments":"{}"},{"type":"function_call","call_id":"b","name":"b","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":"a"},{"type":"function_call_output","call_id":"b","output":"b"}]}`)
	withResults := []byte(`{"model":"m","input":[{"role":"user","content":"hi"},{"type":"function_call","call_id":"a","name":"a","arguments":"{}"},{"type":"function_call","call_id":"b","name":"b","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":"a"},{"type":"additional_tools","role":"developer","tools":[]},{"type":"function_call_output","call_id":"b","output":"b"}]}`)
	withResultsBaseline := []byte(`{"model":"m","input":[{"role":"user","content":"hi"},{"type":"function_call","call_id":"a","name":"a","arguments":"{}"},{"type":"function_call","call_id":"b","name":"b","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":"a"},{"type":"function_call_output","call_id":"b","output":"b"}]}`)
	with, losses, err := responses.DecodeRequestJSON(withEmpty)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	without, _, err := responses.DecodeRequestJSON(withoutEmpty)
	if err != nil {
		t.Fatalf("unexpected baseline error: %v", err)
	}
	if len(losses) != 0 || len(with.ToolChangeGroups) != 1 || with.ToolChangeGroups[0].MessageIndex != 1 || with.ToolChangeGroups[0].ContentIndex != 1 {
		t.Fatalf("expected positioned empty group and no loss, got %+v %+v", with.ToolChangeGroups, losses)
	}
	with.ToolChangeGroups = nil
	withJSON, _ := json.Marshal(with)
	withoutJSON, _ := json.Marshal(without)
	if string(withJSON) != string(withoutJSON) {
		t.Fatalf("empty inventory split calls:\nwith: %s\nwithout: %s", withJSON, withoutJSON)
	}

	results, losses, err := responses.DecodeRequestJSON(withResults)
	if err != nil {
		t.Fatalf("unexpected results error: %v", err)
	}
	resultsBaseline, _, err := responses.DecodeRequestJSON(withResultsBaseline)
	if err != nil {
		t.Fatalf("unexpected results baseline error: %v", err)
	}
	if len(losses) != 0 || len(results.ToolChangeGroups) != 1 || results.ToolChangeGroups[0].MessageIndex != 2 || results.ToolChangeGroups[0].ContentIndex != 1 {
		t.Fatalf("expected positioned empty group and no loss, got %+v %+v", results.ToolChangeGroups, losses)
	}
	results.ToolChangeGroups = nil
	resultsJSON, _ := json.Marshal(results)
	resultsBaselineJSON, _ := json.Marshal(resultsBaseline)
	if string(resultsJSON) != string(resultsBaselineJSON) {
		t.Fatalf("empty inventory split results:\nwith: %s\nwithout: %s", resultsJSON, resultsBaselineJSON)
	}
}

func TestRoundTripToolContainers(t *testing.T) {
	raw := []byte(`{
		"model": "gpt-5.5",
		"input": "test containers",
		"tools": [
			{
				"type": "toolbox",
				"name": "repo",
				"tools": [
					{"type": "function", "name": "read", "description": "read file", "parameters": {"type": "object"}}
				]
			},
			{
				"type": "namespace",
				"name": "editor",
				"tools": [
					{"type": "custom", "name": "patch", "description": "patch code"}
				]
			}
		]
	}`)

	req, losses, err := responses.DecodeRequestJSON(raw)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(losses) != 0 {
		t.Fatalf("expected 0 losses, got: %+v", losses)
	}

	encoded, encLosses, err := responses.EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if len(encLosses) != 0 {
		t.Fatalf("expected 0 encode losses, got: %+v", encLosses)
	}

	if len(encoded.Tools) != 2 {
		t.Fatalf("expected 2 tools on wire, got %d", len(encoded.Tools))
	}
	if encoded.Tools[0].Type != "toolbox" || encoded.Tools[0].Name != "repo" {
		t.Errorf("expected toolbox repo, got %+v", encoded.Tools[0])
	}
	if encoded.Tools[1].Type != "namespace" || encoded.Tools[1].Name != "editor" {
		t.Errorf("expected namespace editor, got %+v", encoded.Tools[1])
	}
}
