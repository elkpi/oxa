package responses_test

import (
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/responses"
)

func TestDecodeToolContainers(t *testing.T) {
	raw := []byte(`{
		"model": "gpt-5.5",
		"input": [
			{"role": "user", "content": "use tools"},
			{"type": "additional_tools", "tools": [
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
	if len(req.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(req.Tools))
	}

	// 1. repo.read
	t1 := req.Tools[0]
	if t1.Name != "repo.read" || t1.Type != ir.ToolTypeFunction {
		t.Errorf("expected repo.read function tool, got %+v", t1)
	}

	// 2. editor.submit_patch
	t2 := req.Tools[1]
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
		{
			name: "empty additional_tools tools array",
			json: `{"model":"m","input":[{"role":"user","content":"hi"},{"type":"additional_tools","tools":[]}]}`,
			err:  "requires non-empty tools array",
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
