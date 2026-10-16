package vectest_test

import (
	"encoding/json"
	"strings"
	"testing"

	an "github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/ir"
	cc "github.com/elkpi/oxa/go/v2/openai/chatcompletions"
)

func TestCustomToolsSupportedInCCAndAnthropic(t *testing.T) {
	max := int64(32)
	req := &ir.Request{
		Model: "m",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hello"}}},
		},
		Tools: []ir.Tool{
			{Type: ir.ToolTypeCustom, Name: "run"},
		},
		ToolChoice: &ir.ToolChoice{
			Mode:     ir.ToolChoiceTool,
			Name:     "run",
			ToolType: ir.ToolTypeCustom,
		},
		Params: ir.Params{MaxTokens: &max},
	}
	t.Run("cc", func(t *testing.T) {
		w, losses, err := cc.EncodeRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		if len(losses) != 0 {
			t.Fatalf("unexpected losses: %+v", losses)
		}
		raw, err := json.Marshal(w)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"type":"custom"`) {
			t.Fatalf("expected custom tool in cc wire: %s", raw)
		}
	})
	t.Run("an", func(t *testing.T) {
		w, losses, err := an.EncodeRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		if len(losses) != 0 {
			t.Fatalf("unexpected losses: %+v", losses)
		}
		raw, err := json.Marshal(w)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"input_schema"`) {
			t.Fatalf("expected input_schema wrapper in anthropic wire: %s", raw)
		}
	})
}

func TestCustomMixedResultsHandled(t *testing.T) {
	empty := ""
	req := &ir.Request{Model: "m", Messages: []ir.Message{
		{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "go"}}},
		{Role: ir.RoleAssistant, Content: []ir.Block{ir.CustomToolUseBlock{ID: "c", Name: "run", Input: json.RawMessage(`"code"`)}, ir.ToolUseBlock{ID: "f", Name: "clock", Input: json.RawMessage(`"{}"`)}}},
		{Role: ir.RoleUser, Content: []ir.Block{ir.CustomToolResultBlock{ToolUseID: "c", Output: ir.CustomToolOutput{Text: &empty}}, ir.ToolResultBlock{ToolUseID: "f", Content: []ir.Block{ir.TextBlock{Text: "ok"}}}}},
	}}
	wire, _, err := cc.EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire.Messages) != 4 || wire.Messages[2].Role != "tool" || wire.Messages[3].Role != "tool" {
		t.Fatalf("unexpected wire messages: %+v", wire.Messages)
	}
}

func TestUnsupportedApplyPatchToolsNeverBecomeFunctions(t *testing.T) {
	max := int64(32)
	req := &ir.Request{
		Model: "m",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hello"}}},
		},
		Tools: []ir.Tool{
			{Type: ir.ToolTypeApplyPatch, Name: "apply_patch"},
		},
		ToolChoice: &ir.ToolChoice{
			Mode:     ir.ToolChoiceTool,
			Name:     "apply_patch",
			ToolType: ir.ToolTypeApplyPatch,
		},
		Params: ir.Params{MaxTokens: &max},
	}
	for _, face := range []string{"cc", "an"} {
		t.Run(face, func(t *testing.T) {
			var raw []byte
			var losses []ir.Loss
			var err error
			if face == "cc" {
				w, l, e := cc.EncodeRequest(req)
				losses, err = l, e
				if e == nil {
					raw, err = json.Marshal(w)
				}
			} else {
				w, l, e := an.EncodeRequest(req)
				losses, err = l, e
				if e == nil {
					raw, err = json.Marshal(w)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), `"apply_patch"`) {
				t.Fatalf("apply_patch coerced: %s", raw)
			}
			if len(losses) != 2 || losses[0].Reason != ir.LossUnsupportedSemantic || losses[1].Reason != ir.LossUnsupportedSemantic {
				t.Fatalf("losses: %+v", losses)
			}
		})
	}
}

func TestUnsupportedApplyPatchResponseNormalizesStop(t *testing.T) {
	resp := &ir.Response{
		ID:    "r",
		Model: "m",
		Content: []ir.Block{
			ir.ApplyPatchToolUseBlock{ID: "p", Status: "in_progress", Operation: ir.ApplyPatchCreateFileOperation{Path: "a.txt"}},
		},
		StopReason: ir.StopToolUse,
	}
	for _, face := range []string{"cc", "an"} {
		t.Run(face, func(t *testing.T) {
			var raw []byte
			var losses []ir.Loss
			var err error
			if face == "cc" {
				w, l, e := cc.EncodeResponse(resp)
				losses, err = l, e
				if e == nil {
					raw, err = json.Marshal(w)
				}
			} else {
				w, l, e := an.EncodeResponse(resp)
				losses, err = l, e
				if e == nil {
					raw, err = json.Marshal(w)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), `"tool_calls"`) || strings.Contains(string(raw), `"tool_use"`) {
				t.Fatalf("dangling tool stop: %s", raw)
			}
			if len(losses) != 2 || losses[1].Path != "stop_reason" || losses[1].Reason != ir.LossDegraded {
				t.Fatalf("losses: %+v", losses)
			}
		})
	}
}

func TestUnsupportedApplyPatchStreamDropsOnceAndCompacts(t *testing.T) {
	events := []ir.Event{
		ir.MessageStart{ID: "r", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.ApplyPatchToolUseBlock{ID: "p", Status: "in_progress", Operation: ir.ApplyPatchCreateFileOperation{Path: "a.txt"}}},
		ir.ContentBlockStop{Index: 0},
		ir.ContentBlockStart{Index: 1, Block: ir.TextBlock{Text: ""}},
		ir.ContentBlockDelta{Index: 1, Delta: ir.TextDelta{Text: "kept"}},
		ir.ContentBlockStop{Index: 1},
		ir.MessageDelta{StopReason: ir.StopToolUse}, ir.MessageDone{},
	}
	t.Run("cc", func(t *testing.T) {
		e, d := cc.NewStreamEncoder(), cc.NewStreamDecoder()
		var losses []ir.Loss
		var got []ir.Event
		for _, ev := range events {
			ws, ls, err := e.Apply(ev)
			if err != nil {
				t.Fatal(err)
			}
			losses = append(losses, ls...)
			for _, w := range ws {
				es, err := d.Feed(w)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, es...)
			}
		}
		es, err := d.Flush()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, es...)
		checkFilteredStream(t, got, losses)
	})
	t.Run("an", func(t *testing.T) {
		e, d := an.NewStreamEncoder(), an.NewStreamDecoder()
		var losses []ir.Loss
		var got []ir.Event
		for _, ev := range events {
			ws, ls, err := e.Apply(ev)
			if err != nil {
				t.Fatal(err)
			}
			losses = append(losses, ls...)
			for _, w := range ws {
				es, err := d.Feed(w)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, es...)
			}
		}
		es, err := d.Flush()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, es...)
		checkFilteredStream(t, got, losses)
	})
}

func checkFilteredStream(t *testing.T, events []ir.Event, losses []ir.Loss) {
	t.Helper()
	if len(losses) != 2 || losses[0].Reason != ir.LossUnsupportedSemantic || losses[1].Reason != ir.LossDegraded {
		t.Fatalf("losses: %+v", losses)
	}
	var text string
	for _, ev := range events {
		switch e := ev.(type) {
		case ir.ContentBlockStart:
			if e.Index != 0 {
				t.Fatalf("index not compact: %d", e.Index)
			}
		case ir.ContentBlockDelta:
			if d, ok := e.Delta.(ir.TextDelta); ok {
				text += d.Text
			}
		case ir.MessageDelta:
			if e.StopReason != ir.StopEndTurn {
				t.Fatalf("wrong stop: %+v", e)
			}
		}
	}
	if text != "kept" {
		t.Fatalf("text: %q", text)
	}
}
