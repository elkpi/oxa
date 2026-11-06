package ir

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPositionalToolChangesCanonicalRoundTrip(t *testing.T) {
	input := []byte(`{"specVersion":"0.3.0","model":"m","messages":[{"role":"user","content":[{"type":"compaction","content":null,"encrypted_content":"","signature":"","tool_changes":[]},{"type":"text","text":"hi"}]}],"tool_change_groups":[{"message_index":1,"content_index":-1,"parts":[{"type":"tool_change","change":{"op":"add","tool":{"name":"calc","input_schema":{"type":"object"}}}}]}]}`)
	req, err := UnmarshalRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(input, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round-trip mismatch:\nwant %s\ngot  %s", input, output)
	}
}

func TestToolChangePartsRejectMalformedInput(t *testing.T) {
	for _, part := range []string{
		`{"type":"text"}`,
		`{"type":"tool_change"}`,
		`{"type":"text","text":"x","change":{"op":"remove","ref":{"type":"tool_reference","name":"x"}}}`,
	} {
		input := []byte(`{"specVersion":"0.3.0","model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"tool_change_groups":[{"message_index":1,"content_index":-1,"parts":[` + part + `]}]}`)
		if _, err := UnmarshalRequest(input); err == nil {
			t.Errorf("accepted malformed part %s", part)
		}
	}
}

func TestCompactionExtraIsContained(t *testing.T) {
	input := []byte(`{"specVersion":"0.3.0","model":"m","messages":[{"role":"user","content":[{"type":"compaction","extra":{"future":true}}]}]}`)
	req, err := UnmarshalRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	out, err := MarshalRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal(input, &want)
	json.Unmarshal(out, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %s, got %s", input, out)
	}
}

func TestCompactionIsRequestOnly(t *testing.T) {
	if _, err := UnmarshalBlock([]byte(`{"type":"compaction","content":"summary"}`)); err == nil {
		t.Fatal("response/event block decoder accepted request-only compaction")
	}
}

func TestToolChangeGroupRequiresPositionAndParts(t *testing.T) {
	for _, group := range []string{
		`{"content_index":-1,"parts":[]}`,
		`{"message_index":0,"parts":[]}`,
		`{"message_index":0,"content_index":-1}`,
		`{"message_index":0,"content_index":-1,"parts":null}`,
	} {
		input := []byte(`{"specVersion":"0.3.0","model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"tool_change_groups":[` + group + `]}`)
		if _, err := UnmarshalRequest(input); err == nil {
			t.Errorf("accepted invalid group: %s", group)
		}
	}
}

func TestToolReferencePreservesExtensionFields(t *testing.T) {
	input := []byte(`{"op":"remove","ref":{"type":"tool_reference","name":"Read","extra":{"future":true}}}`)
	var change ToolChange
	if err := json.Unmarshal(input, &change); err != nil {
		t.Fatal(err)
	}
	output, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal(input, &want)
	json.Unmarshal(output, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %s, got %s", input, output)
	}
}
