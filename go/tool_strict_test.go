package oxa_test

import (
	"testing"

	"github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
	"github.com/elkpi/oxa/go/v2/openai/responses"
)

func TestDirectNonFunctionToolStrictRejectedByEncoders(t *testing.T) {
	strict := false
	for _, kind := range []string{ir.ToolTypeCustom, ir.ToolTypeApplyPatch, ir.ToolTypeNative} {
		tool := ir.Tool{Type: kind, Name: "x", Strict: &strict}
		if kind == ir.ToolTypeApplyPatch {
			tool.Name = ""
		}
		if kind == ir.ToolTypeNative {
			tool.Source = "anthropic"
			tool.Definition = `{"type":"provider_tool","name":"x"}`
		}
		req := &ir.Request{Model: "m", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "hi"}}}}, Tools: []ir.Tool{tool}}
		if _, _, err := chatcompletions.EncodeRequest(req); err == nil {
			t.Errorf("chatcompletions accepted %s strict", kind)
		}
		if _, _, err := responses.EncodeRequest(req); err == nil {
			t.Errorf("responses accepted %s strict", kind)
		}
		if _, _, err := messages.EncodeRequest(req); err == nil {
			t.Errorf("anthropic accepted %s strict", kind)
		}
	}
}
