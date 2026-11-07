package messages

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/elkpi/oxa-protocol/go/v3/ir"
)

func TestCompactionToolDefinitionReportsDecodeLoss(t *testing.T) {
	input := []byte(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":[{"type":"compaction","tool_changes":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"f","input_schema":{"type":"object"},"cache_control":{"type":"bad"}}}}]}]}]}`)
	_, losses, err := DecodeRequestJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Path != "messages[0].content[0].tool_changes[0].tool.definition.cache_control.type" || losses[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses=%+v", losses)
	}
}

func TestCompactionReplayPreservesPresenceAndExtensions(t *testing.T) {
	for _, block := range []string{
		`{"type":"compaction","content":null,"encrypted_content":"","signature":"","tool_changes":[],"cache_control":{"type":"ephemeral"},"future":false}`,
		`{"type":"compaction","content":"summary","tool_changes":[{"type":"tool_removal","tool":{"type":"tool_reference","name":"x","future_ref":1},"future_change":true}]}`,
		`{"type":"compaction","tool_changes":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"x","input_schema":{"type":"object"},"future_def":true},"future_wrapper":"value"}}]}`,
	} {
		input := fmt.Sprintf(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":[%s,{"type":"text","text":"next"}]}]}`, block)
		req, losses, err := DecodeRequestJSON([]byte(input))
		if err != nil || len(losses) != 0 {
			t.Fatalf("decode: %v, %+v", err, losses)
		}
		encodedIR, err := ir.MarshalRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		req, err = ir.UnmarshalRequest(encodedIR)
		if err != nil {
			t.Fatal(err)
		}
		output, losses, err := EncodeRequestJSON(req)
		if err != nil || len(losses) != 0 {
			t.Fatalf("encode: %v, %+v", err, losses)
		}
		var decoded struct {
			Messages []struct {
				Content []json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(output, &decoded); err != nil {
			t.Fatal(err)
		}
		var want, got any
		if err := json.Unmarshal([]byte(block), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(decoded.Messages[0].Content[0], &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("want %s, got %s", block, decoded.Messages[0].Content[0])
		}
	}
}

func TestCompactionRejectsInvalidPositionAndContent(t *testing.T) {
	for _, content := range []string{
		`[{"type":"text","text":"first"},{"type":"compaction","content":"summary"}]`,
		`[{"type":"compaction","content":""}]`,
		`[{"type":"compaction","tool_changes":null}]`,
		`[{"type":"compaction","tool_changes":[{"type":"tool_removal","tool":{"type":"tool_reference","name":""}}]}]`,
	} {
		input := fmt.Sprintf(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":%s}]}`, content)
		if _, _, err := DecodeRequestJSON([]byte(input)); err == nil {
			t.Errorf("accepted invalid content: %s", content)
		}
	}
}
