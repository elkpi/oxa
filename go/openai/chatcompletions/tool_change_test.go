package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa-protocol/go/v3/ir"
)

func TestToolChangeDoesNotChangeInitialTools(t *testing.T) {
	tool := &ir.Tool{Name: "later", InputSchema: json.RawMessage(`{"type":"object"}`)}
	req := &ir.Request{Model: "m", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}}, ToolChangeGroups: []ir.ToolChangeGroup{{MessageIndex: 1, ContentIndex: -1, Parts: []ir.ToolChangePart{{Type: ir.ToolChangePartTypeToolChange, Change: &ir.ToolChange{Op: ir.ToolChangeOpAdd, Tool: tool}}}}}}
	out, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) != 0 {
		t.Fatalf("positional tool promoted: %+v", out.Tools)
	}
	if len(losses) != 1 || losses[0].Path != "tool_change_groups[0].parts[0]" || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("unexpected losses: %+v", losses)
	}
}

func TestCompactionProducesWholeBlockLoss(t *testing.T) {
	request := &ir.Request{Model: "m", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.CompactionBlock{NullContent: true}, ir.TextBlock{Text: "hi"}}}}}
	_, losses, err := EncodeRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 1 || losses[0].Path != "messages[0].content[0]" || losses[0].Detail != "Anthropic compaction block cannot be represented on the target face" {
		t.Fatalf("unexpected losses: %+v", losses)
	}
}
