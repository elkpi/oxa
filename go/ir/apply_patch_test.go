package ir

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestApplyPatchCanonicalBlocks(t *testing.T) {
	for _, doc := range []string{
		`{"type":"apply_patch_tool_use","id":"c","status":"in_progress","operation":{"type":"create_file","path":"","diff":""}}`,
		`{"type":"apply_patch_tool_use","id":"c","status":"completed","operation":{"type":"delete_file","path":"../x"}}`,
		`{"type":"apply_patch_tool_use","id":"c","status":"completed","operation":{"type":"update_file","path":"C:\\a","diff":"not a patch\r\n\u0000"}}`,
		`{"type":"apply_patch_tool_result","tool_use_id":"c","status":"failed"}`,
		`{"type":"apply_patch_tool_result","tool_use_id":"c","status":"completed","output":""}`,
	} {
		b, err := UnmarshalBlock([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		out, err := MarshalBlock(b)
		if err != nil {
			t.Fatal(err)
		}
		var expected, actual any
		if err := json.Unmarshal([]byte(doc), &expected); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(out, &actual); err != nil {
			t.Fatal(err)
		}
		want, _ := json.Marshal(expected)
		got, _ := json.Marshal(actual)
		if !bytes.Equal(want, got) {
			t.Fatalf("canonical block changed: %s != %s", want, got)
		}
	}
}

func TestApplyPatchCanonicalRejectsInvalidValues(t *testing.T) {
	for _, doc := range []string{
		`{"type":"apply_patch_tool_use","id":"c","status":"failed","operation":{"type":"delete_file","path":"a"}}`,
		`{"type":"apply_patch_tool_use","id":"","status":"completed","operation":{"type":"delete_file","path":"a"}}`,
		`{"type":"apply_patch_tool_use","id":"c","operation":{"type":"delete_file","path":"a"}}`,
		`{"type":"apply_patch_tool_use","id":"c","status":"completed","operation":{"type":"create_file","path":"a"}}`,
		`{"type":"apply_patch_tool_use","id":"c","status":"completed","operation":{"type":"update_file","path":"a","diff":null}}`,
		`{"type":"apply_patch_tool_use","id":"c","status":"completed","operation":{"type":"delete_file","path":"a","diff":null}}`,
		`{"type":"apply_patch_tool_use","id":"c","status":"completed","operation":{"type":"future","path":"a"}}`,
		`{"type":"apply_patch_tool_result","tool_use_id":"c","status":"in_progress"}`,
		`{"type":"apply_patch_tool_result","tool_use_id":"c","status":"failed","output":null}`,
		`{"type":"apply_patch_tool_result","tool_use_id":"c","status":"completed","output":[]}`,
		`{"type":"apply_patch_tool_result","tool_use_id":"c","status":"completed","is_error":false}`,
	} {
		if _, err := UnmarshalBlock([]byte(doc)); err == nil {
			t.Fatalf("invalid canonical block accepted: %s", doc)
		}
	}
	if _, err := MarshalBlock(ApplyPatchToolUseBlock{ID: "c", Status: ApplyPatchCallCompleted}); err == nil {
		t.Fatal("nil operation accepted")
	}
}

func TestApplyPatchCanonicalDeclarationAndChoice(t *testing.T) {
	for _, extra := range []string{
		`"tools":[{"type":"apply_patch"}],"tool_choice":{"mode":"tool","tool_type":"apply_patch"}`,
		`"tools":[{"name":"f","input_schema":{}}],"tool_choice":{"mode":"tool","name":"f"}`,
	} {
		doc := `{"specVersion":"0.2.0","model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],` + extra + `}`
		req, err := UnmarshalRequest([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := MarshalRequest(req); err != nil {
			t.Fatal(err)
		}
	}
	for _, extra := range []string{
		`"tools":[{"type":"apply_patch","name":""}]`,
		`"tools":[{"type":"apply_patch","input_schema":null}]`,
		`"tool_choice":{"mode":"tool","tool_type":"apply_patch","name":""}`,
		`"tool_choice":{"mode":"auto","tool_type":"apply_patch"}`,
	} {
		doc := `{"specVersion":"0.2.0","model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],` + extra + `}`
		if _, err := UnmarshalRequest([]byte(doc)); err == nil {
			t.Fatalf("invalid declaration/choice accepted: %s", doc)
		}
	}
}
