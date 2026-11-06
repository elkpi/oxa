package messages

import (
	"fmt"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v3/ir"
)

func TestPositionalToolDefinitionReportsDecodeLoss(t *testing.T) {
	input := []byte(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"go"},{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"f","input_schema":{"type":"object"},"cache_control":{"type":"bad"}}}}]}]}`)
	_, losses, err := DecodeRequestJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Path != "messages[1].content[0].tool.definition.cache_control.type" || losses[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses=%+v", losses)
	}
}

func TestPositionalToolDefinitionReportsContainerLoss(t *testing.T) {
	max := int64(32)
	tool := ir.Tool{Name: "ns.f", InputSchema: []byte(`{"type":"object"}`), Native: []ir.NativeAnnotation{{Source: ir.NativeSourceOpenAI, Kind: ir.NativeKindContainer, Value: `{"path":[{"type":"namespace","name":"ns"}]}`, Omission: &ir.NativeOmission{Field: "type", Reason: ir.LossDegraded, Detail: "tool container grouping was flattened into dotted tool names"}}}}
	req := &ir.Request{Model: "m", Params: ir.Params{MaxTokens: &max}, Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "go"}}}}, ToolChangeGroups: []ir.ToolChangeGroup{{MessageIndex: 1, ContentIndex: -1, Parts: []ir.ToolChangePart{{Type: ir.ToolChangePartTypeToolChange, Change: &ir.ToolChange{Op: ir.ToolChangeOpAdd, Tool: &tool}}}}}}
	_, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Path != "tool_change_groups[0].parts[0].change.tool.native[0]" || losses[0].Reason != ir.LossDegraded {
		t.Fatalf("losses=%+v", losses)
	}
}

func TestPositionalToolDefinitionSchemaBytesRoundTrip(t *testing.T) {
	schema := `{ "type" : "object", "properties" : { "x" : {"type":"string"} } }`
	change := fmt.Sprintf(`{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"f","input_schema":%s}}}`, schema)
	for _, messages := range []string{
		fmt.Sprintf(`[{"role":"user","content":"go"},{"role":"system","content":[%s]}]`, change),
		fmt.Sprintf(`[{"role":"user","content":[{"type":"compaction","tool_changes":[%s]}]}]`, change),
	} {
		input := fmt.Sprintf(`{"model":"m","max_tokens":32,"messages":%s}`, messages)
		req, losses, err := DecodeRequestJSON([]byte(input))
		if err != nil || len(losses) != 0 {
			t.Fatalf("decode: losses=%+v err=%v", losses, err)
		}
		out, losses, err := EncodeRequestJSON(req)
		if err != nil || len(losses) != 0 {
			t.Fatalf("encode: losses=%+v err=%v", losses, err)
		}
		if !strings.Contains(string(out), schema) {
			t.Errorf("schema bytes lost: %s", out)
		}
	}
}

func TestPositionalFunctionDefinitionDeterminesCallType(t *testing.T) {
	input := []byte(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"go"},{"role":"system","content":[{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"f","input_schema":{"type":"object","properties":{"input":{"type":"string"},"other":{"type":"string"}}}}}}]},{"role":"assistant","content":[{"type":"tool_use","id":"c","name":"f","input":{"input":"hello"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"c","content":"ok"}]}]}`)
	req, losses, err := DecodeRequestJSON(input)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: losses=%+v err=%v", losses, err)
	}
	if _, ok := req.Messages[1].Content[0].(ir.ToolUseBlock); !ok {
		t.Fatalf("call type=%T, want ToolUseBlock", req.Messages[1].Content[0])
	}
	if _, ok := req.Messages[2].Content[0].(ir.ToolResultBlock); !ok {
		t.Fatalf("result type=%T, want ToolResultBlock", req.Messages[2].Content[0])
	}
}

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
