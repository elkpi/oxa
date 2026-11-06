package messages

import (
	"fmt"
	"testing"

	"github.com/elkpi/oxa/go/v3/ir"
)

func TestDecodeSystemToolChangeReferences(t *testing.T) {
	for _, tool := range []string{`{"type":"tool_reference","name":"x"}`, `{"type":"mcp_tool_reference","server_name":"s","name":"x"}`, `{"type":"mcp_toolset_reference","server_name":"s"}`} {
		input := fmt.Sprintf(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"go"},{"role":"system","content":[{"type":"tool_removal","tool":%s,"cache_control":{"type":"ephemeral"}}]}]}`, tool)
		req, losses, err := DecodeRequestJSON([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if len(losses) != 0 || len(req.ToolChangeGroups) != 1 {
			t.Fatalf("groups=%+v losses=%+v", req.ToolChangeGroups, losses)
		}
		change := req.ToolChangeGroups[0].Parts[0].Change
		if change.Op != ir.ToolChangeOpRemove || change.Ref == nil || len(change.Native) != 1 {
			t.Fatalf("change=%+v", change)
		}
	}
}

func TestDecodeSystemToolChangeRejectsMalformed(t *testing.T) {
	for _, tool := range []string{`{}`, `{"type":"tool_reference","name":""}`, `{"type":"mcp_tool_reference","name":"x"}`, `{"type":"tool_definition","definition":{"name":"x","input_schema":{}}}`} {
		input := fmt.Sprintf(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"go"},{"role":"system","content":[{"type":"tool_removal","tool":%s}]}]}`, tool)
		if _, _, err := DecodeRequestJSON([]byte(input)); err == nil {
			t.Errorf("accepted %s", tool)
		}
	}
}

func TestMixedSystemChangePreservesTextCache(t *testing.T) {
	input := []byte(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"go"},{"role":"system","content":[{"type":"text","text":"instruction","cache_control":{"type":"ephemeral"}},{"type":"tool_removal","tool":{"type":"tool_reference","name":"Read"}}]}]}`)
	req, losses, err := DecodeRequestJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 0 || len(req.System) != 0 || len(req.ToolChangeGroups) != 1 || len(req.ToolChangeGroups[0].Parts[0].Native) != 1 {
		t.Fatalf("req=%+v losses=%+v", req, losses)
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode err=%v losses=%+v", err, losses)
	}
	blocks := wire.Messages[1].Content.([]BlockWire)
	if string(blocks[0].CacheControl) != `{"type":"ephemeral"}` {
		t.Fatalf("cache control: %s", blocks[0].CacheControl)
	}
}

func TestSystemMessageFieldLossDetails(t *testing.T) {
	input := []byte(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"go"},{"role":"system","clear_at":{"type":"input_tokens","value":1},"output_config":{"effort":"low"},"content":[{"type":"tool_addition","tool":{"type":"tool_reference","name":"x"}}]},{"role":"assistant","content":"ok"}]}`)
	req, losses, err := DecodeRequestJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.ToolChangeGroups) != 1 || len(req.System) != 0 {
		t.Fatalf("stored system fields: %+v", req)
	}
	want := map[string]string{
		"messages[1].clear_at":      "Anthropic system message clear_at has no IR equivalent",
		"messages[1].output_config": "Anthropic per-message output_config has no IR equivalent",
	}
	if len(losses) != len(want) {
		t.Fatalf("losses=%+v", losses)
	}
	for _, loss := range losses {
		if want[loss.Path] != loss.Detail || loss.Field == "" || loss.Reason != ir.LossUnmappedField {
			t.Fatalf("loss=%+v", loss)
		}
	}
}
