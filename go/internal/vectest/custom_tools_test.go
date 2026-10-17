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

func TestAnthropicCustomToolsMultiFragmentStream(t *testing.T) {
	encoder := an.NewStreamEncoder()
	events := []ir.Event{
		ir.MessageStart{ID: "r", Model: "m"},
		ir.ContentBlockStart{Index: 0, Block: ir.CustomToolUseBlock{ID: "c", Name: "exec", Input: json.RawMessage(`"ab"`)}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.CustomToolInputDelta{Text: json.RawMessage(`"a"`)}},
		ir.ContentBlockDelta{Index: 0, Delta: ir.CustomToolInputDelta{Text: json.RawMessage(`"b"`)}},
		ir.ContentBlockStop{Index: 0},
		ir.MessageDelta{StopReason: ir.StopToolUse},
		ir.MessageDone{},
	}
	var wire []*an.StreamEvent
	for _, ev := range events {
		chunks, losses, err := encoder.Apply(ev)
		if err != nil {
			t.Fatal(err)
		}
		if len(losses) != 0 {
			t.Fatalf("unexpected losses: %+v", losses)
		}
		wire = append(wire, chunks...)
	}
	var joined string
	for _, w := range wire {
		if w.Type == an.EventTypeContentBlockDelta && w.Delta != nil {
			joined += w.Delta.PartialJSON
		}
	}
	if !json.Valid([]byte(joined)) {
		t.Fatalf("concatenated partial_json is invalid JSON: %s", joined)
	}
	decoder := an.NewStreamDecoder()
	var decoded []ir.Event
	for _, w := range wire {
		evs, err := decoder.Feed(w)
		if err != nil {
			t.Fatal(err)
		}
		decoded = append(decoded, evs...)
	}
	if len(decoded) < 3 {
		t.Fatalf("unexpected decoded events count: %d", len(decoded))
	}
	cbs, ok := decoded[1].(ir.ContentBlockStart)
	if !ok {
		t.Fatalf("expected ContentBlockStart, got %T", decoded[1])
	}
	ctu, ok := cbs.Block.(ir.CustomToolUseBlock)
	if !ok {
		t.Fatalf("expected CustomToolUseBlock, got %T", cbs.Block)
	}
	if string(ctu.Input) != `"ab"` {
		t.Fatalf("expected input 'ab', got %s", ctu.Input)
	}
}

func TestConflictingFunctionDeclarationNotPromotedToCustom(t *testing.T) {
	wireJSON := `{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"tools": [
			{
				"name": "echo",
				"input_schema": {
					"type": "object",
					"properties": {"input": {"type": "string"}, "extra": {"type": "string"}},
					"required": ["input"]
				}
			}
		],
		"messages": [
			{"role": "user", "content": "hi"},
			{
				"role": "assistant",
				"content": [
					{"type": "tool_use", "id": "call_1", "name": "echo", "input": {"input": "hello"}}
				]
			},
			{
				"role": "user",
				"content": [
					{"type": "tool_result", "tool_use_id": "call_1", "content": "ok"}
				]
			}
		]
	}`
	var wire an.Request
	if err := json.Unmarshal([]byte(wireJSON), &wire); err != nil {
		t.Fatal(err)
	}
	req, losses, err := an.DecodeRequest(&wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode err=%v losses=%+v", err, losses)
	}
	if req.Tools[0].Type == ir.ToolTypeCustom {
		t.Fatalf("expected function tool, got custom")
	}
	call, ok := req.Messages[1].Content[0].(ir.ToolUseBlock)
	if !ok {
		t.Fatalf("expected ToolUseBlock, got %T", req.Messages[1].Content[0])
	}
	if call.Name != "echo" {
		t.Fatalf("wrong tool name: %s", call.Name)
	}
	_, ok = req.Messages[2].Content[0].(ir.ToolResultBlock)
	if !ok {
		t.Fatalf("expected ToolResultBlock, got %T", req.Messages[2].Content[0])
	}
}

func TestCustomToolResultArrayPreserved(t *testing.T) {
	outputBlocks := []ir.Block{ir.TextBlock{Text: "line 1"}, ir.TextBlock{Text: "line 2"}}
	req := &ir.Request{
		Model: "m",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}},
			{Role: ir.RoleAssistant, Content: []ir.Block{ir.CustomToolUseBlock{ID: "c", Name: "run", Input: json.RawMessage(`"cmd"`)}}},
			{Role: ir.RoleUser, Content: []ir.Block{ir.CustomToolResultBlock{ToolUseID: "c", Output: ir.CustomToolOutput{Content: outputBlocks}}}},
		},
	}
	wire, losses, err := cc.EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode err=%v losses=%+v", err, losses)
	}
	parts, ok := wire.Messages[2].Content.([]cc.ContentPart)
	if !ok {
		t.Fatalf("expected []ContentPart, got %T", wire.Messages[2].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	decoded, decodeLosses, err := cc.DecodeRequest(wire)
	if err != nil || len(decodeLosses) != 0 {
		t.Fatalf("decode err=%v losses=%+v", err, decodeLosses)
	}
	ctr, ok := decoded.Messages[2].Content[0].(ir.CustomToolResultBlock)
	if !ok {
		t.Fatalf("expected CustomToolResultBlock, got %T", decoded.Messages[2].Content[0])
	}
	if ctr.Output.Content == nil || len(ctr.Output.Content) != 2 {
		t.Fatalf("expected content slice of len 2, got %+v", ctr.Output)
	}
}

func TestGrammarFormatLossOnAnthropicEncode(t *testing.T) {
	req := &ir.Request{
		Model: "m",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}},
		},
		Tools: []ir.Tool{
			{
				Type: ir.ToolTypeCustom,
				Name: "calc",
				Format: &ir.CustomToolFormat{
					Type:       "grammar",
					Syntax:     "regex",
					Definition: "[0-9]+",
				},
			},
		},
	}
	_, losses, err := an.EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range losses {
		if l.Path == "tools[0].format" && l.Reason == ir.LossUnsupportedSemantic {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unsupported-semantic loss for grammar format on Anthropic, got %+v", losses)
	}
}

func TestChatCompletionsFormatPreservationAndLoss(t *testing.T) {
	// Missing format remains nil on decode
	wireMissing := &cc.Request{
		Model:    "m",
		Messages: []cc.Message{{Role: "user", Content: "hi"}},
		Tools: []cc.ToolWire{
			{
				Type:   "custom",
				Custom: &cc.CustomToolWire{Name: "c"},
			},
		},
	}
	req, losses, err := cc.DecodeRequest(wireMissing)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode missing format err=%v losses=%+v", err, losses)
	}
	if req.Tools[0].Format != nil {
		t.Fatalf("expected nil format, got %+v", req.Tools[0].Format)
	}

	// Unknown format produces unsupported-semantic loss
	wireUnknown := &cc.Request{
		Model:    "m",
		Messages: []cc.Message{{Role: "user", Content: "hi"}},
		Tools: []cc.ToolWire{
			{
				Type: "custom",
				Custom: &cc.CustomToolWire{
					Name:   "c",
					Format: &ir.CustomToolFormat{Type: "unknown_fmt"},
				},
			},
		},
	}
	req2, losses2, err2 := cc.DecodeRequest(wireUnknown)
	if err2 != nil {
		t.Fatal(err2)
	}
	if len(losses2) != 1 || losses2[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("expected unsupported-semantic loss, got %+v", losses2)
	}
	if req2.Tools[0].Format != nil {
		t.Fatalf("expected nil format after unknown format loss, got %+v", req2.Tools[0].Format)
	}
}
