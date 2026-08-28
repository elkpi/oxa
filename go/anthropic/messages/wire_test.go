package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oxa-protocol/oxa/go/ir"
	"github.com/oxa-protocol/oxa/go/modelmap"
)

func ptr[T any](v T) *T { return &v }

func TestDecodeRequestSystemStringAndBlocks(t *testing.T) {
	wire := &Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 512,
		System:    "You are terse.",
		Messages:  []Message{{Role: "user", Content: "Hi"}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if len(req.System) != 1 || req.System[0].Text != "You are terse." {
		t.Fatalf("system string not mapped: %+v", req.System)
	}
	if req.Params.MaxTokens == nil || *req.Params.MaxTokens != 512 {
		t.Fatalf("max_tokens not mapped: %+v", req.Params)
	}
}

func TestDecodeRequestCacheControlLoss(t *testing.T) {
	wire := &Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 512,
		System: []any{
			map[string]any{
				"type":          "text",
				"text":          "You are a terse assistant.",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
		Messages: []Message{{Role: "user", Content: "Hi"}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.System) != 1 || req.System[0].Text != "You are a terse assistant." {
		t.Fatalf("system not mapped: %+v", req.System)
	}
	if len(losses) != 1 || losses[0].Path != "system[0].cache_control" ||
		losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("losses: %+v", losses)
	}
}

func TestDecodeRequestParams(t *testing.T) {
	wire := &Request{
		Model:         "claude-sonnet-4-5",
		MaxTokens:     300,
		Temperature:   ptr(0.5),
		TopP:          ptr(0.9),
		StopSequences: []string{"\n\n", "END"},
		Messages:      []Message{{Role: "user", Content: "Hello"}},
	}
	req, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Params.Temperature == nil || *req.Params.Temperature != 0.5 ||
		req.Params.TopP == nil || *req.Params.TopP != 0.9 ||
		len(req.Params.StopSequences) != 2 {
		t.Fatalf("params not mapped: %+v", req.Params)
	}
}

func TestDecodeRequestToolsAndBlocks(t *testing.T) {
	input := json.RawMessage("{\"city\":\"P" + string('\\') + "u0041ris\"}")
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)
	wire := &Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 512,
		Tools: []ToolWire{{
			Name: "weather", Description: "Get weather", InputSchema: schema,
		}},
		ToolChoice: &ToolChoiceWire{Type: "tool", Name: "weather"},
		Messages: []Message{
			{Role: "assistant", Content: []any{
				BlockWire{Type: "tool_use", ID: "toolu_1", Name: "weather", Input: input},
			}},
			{Role: "user", Content: []any{
				BlockWire{Type: "tool_result", ToolUseID: "toolu_1", Content: "Rain", IsError: true},
				BlockWire{Type: "tool_result", ToolUseID: "toolu_2", Content: []any{
					BlockWire{Type: "text", Text: "Windy"},
				}},
				BlockWire{Type: "image", Source: &SourceWire{Type: "base64", MediaType: "image/png", Data: "aGVsbG8="}},
				BlockWire{Type: "image", Source: &SourceWire{Type: "url", URL: "https://example.test/image.png"}},
			}},
		},
	}

	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "weather" ||
		req.Tools[0].Description != "Get weather" || string(req.Tools[0].InputSchema) != string(schema) {
		t.Fatalf("tool not mapped byte-faithfully: %+v", req.Tools)
	}
	if req.ToolChoice == nil || req.ToolChoice.Mode != "tool" || req.ToolChoice.Name != "weather" {
		t.Fatalf("tool choice not mapped: %+v", req.ToolChoice)
	}
	if len(req.Messages) != 2 || len(req.Messages[0].Content) != 1 || len(req.Messages[1].Content) != 4 {
		t.Fatalf("message blocks not mapped: %+v", req.Messages)
	}
	toolUse, ok := req.Messages[0].Content[0].(ir.ToolUseBlock)
	if !ok || toolUse.ID != "toolu_1" || toolUse.Name != "weather" {
		t.Fatalf("tool use not mapped: %+v", req.Messages[0].Content)
	}
	var decodedInput string
	if err := json.Unmarshal(toolUse.Input, &decodedInput); err != nil || decodedInput != string(input) {
		t.Fatalf("tool use input changed: err=%v want=%q got=%q", err, input, decodedInput)
	}
	resultString, ok := req.Messages[1].Content[0].(ir.ToolResultBlock)
	if !ok || resultString.ToolUseID != "toolu_1" || !resultString.IsError ||
		len(resultString.Content) != 1 || resultString.Content[0].(ir.TextBlock).Text != "Rain" {
		t.Fatalf("string tool result not mapped: %+v", req.Messages[1].Content[0])
	}
	resultBlocks, ok := req.Messages[1].Content[1].(ir.ToolResultBlock)
	if !ok || resultBlocks.ToolUseID != "toolu_2" || resultBlocks.IsError ||
		len(resultBlocks.Content) != 1 || resultBlocks.Content[0].(ir.TextBlock).Text != "Windy" {
		t.Fatalf("block tool result not mapped: %+v", req.Messages[1].Content[1])
	}
	base64Image, ok := req.Messages[1].Content[2].(ir.ImageBlock)
	if !ok || base64Image.MediaType != "image/png" || base64Image.Data != "aGVsbG8=" || base64Image.URL != "" {
		t.Fatalf("base64 image not mapped: %+v", req.Messages[1].Content[2])
	}
	urlImage, ok := req.Messages[1].Content[3].(ir.ImageBlock)
	if !ok || urlImage.URL != "https://example.test/image.png" || urlImage.MediaType != "" || urlImage.Data != "" {
		t.Fatalf("URL image not mapped: %+v", req.Messages[1].Content[3])
	}
}

func TestDecodeRequestToolChoiceModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire ToolChoiceWire
		mode string
		tool string
	}{
		{name: "auto", wire: ToolChoiceWire{Type: "auto"}, mode: "auto"},
		{name: "any", wire: ToolChoiceWire{Type: "any"}, mode: "any"},
		{name: "named", wire: ToolChoiceWire{Type: "tool", Name: "weather"}, mode: "tool", tool: "weather"},
		{name: "none", wire: ToolChoiceWire{Type: "none"}, mode: "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, losses, err := DecodeRequest(&Request{
				Model: "claude-sonnet-4-5", MaxTokens: 512,
				ToolChoice: &tc.wire,
				Messages:   []Message{{Role: "user", Content: "Hi"}},
			})
			if err != nil || len(losses) != 0 {
				t.Fatalf("decode: err=%v losses=%+v", err, losses)
			}
			if req.ToolChoice == nil || req.ToolChoice.Mode != tc.mode || req.ToolChoice.Name != tc.tool {
				t.Fatalf("tool choice not mapped: %+v", req.ToolChoice)
			}
		})
	}
}

func TestDecodeRequestDisableParallelToolUseLoss(t *testing.T) {
	wire := &Request{
		Model:      "claude-sonnet-4-5",
		MaxTokens:  512,
		ToolChoice: &ToolChoiceWire{Type: "auto", DisableParallelToolUse: true},
		Messages:   []Message{{Role: "user", Content: "Hi"}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.ToolChoice == nil || req.ToolChoice.Mode != "auto" {
		t.Fatalf("tool choice not mapped: %+v", req.ToolChoice)
	}
	if len(losses) != 1 || losses[0].Path != "tool_choice.disable_parallel_tool_use" ||
		losses[0].Field != "disable_parallel_tool_use" || losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("disable parallel tool use loss wrong: %+v", losses)
	}
}

func TestDecodeRequestUnknownBlockAndImageSourceAreLosses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block BlockWire
	}{
		{name: "block", block: BlockWire{Type: "document"}},
		{name: "image source", block: BlockWire{Type: "image", Source: &SourceWire{Type: "file"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, losses, err := DecodeRequest(&Request{
				Model: "claude-sonnet-4-5", MaxTokens: 512,
				Messages: []Message{{Role: "user", Content: []any{tc.block}}},
			})
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(req.Messages) != 1 || len(req.Messages[0].Content) != 0 {
				t.Fatalf("unknown semantic must not emit a block: %+v", req.Messages)
			}
			if len(losses) != 1 || losses[0].Field != "type" || losses[0].Reason != ir.LossUnsupportedSemantic {
				t.Fatalf("unknown semantic loss wrong: %+v", losses)
			}
		})
	}
}

func TestDecodeRequestMalformedBlockReturnsError(t *testing.T) {
	_, _, err := DecodeRequest(&Request{
		Model: "claude-sonnet-4-5", MaxTokens: 512,
		Messages: []Message{{Role: "assistant", Content: []any{
			BlockWire{Type: "tool_use", ID: "toolu_1", Name: "weather", Input: json.RawMessage(`[]`)},
		}}},
	})
	if err == nil {
		t.Fatal("malformed tool_use input must fail")
	}
}

func TestEncodeRequestMaxTokensDefault(t *testing.T) {
	req := &ir.Request{
		Model:    "claude-sonnet-4-5",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hello"}}}},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if wire.MaxTokens != defaultMaxTokens {
		t.Fatalf("max_tokens default: %d", wire.MaxTokens)
	}
	if len(losses) != 1 || losses[0].Reason != ir.LossDegraded ||
		losses[0].Field != "max_tokens" {
		t.Fatalf("degraded loss missing: %+v", losses)
	}
	// Single message, single text block, no system: string shorthand.
	b, _ := json.Marshal(wire)
	if want := `"content":"Hello"`; !strings.Contains(string(b), want) {
		t.Fatalf("string shorthand not used: %s", b)
	}
}

func TestEncodeRequestBlockArrayRendering(t *testing.T) {
	// System present: block-array rendering even for a single text block.
	req := &ir.Request{
		Model:  "claude-sonnet-4-5",
		System: []ir.SystemBlock{{Text: "You are a terse assistant."}},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}},
		},
		Params: ir.Params{MaxTokens: ptr(int64(512))},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	b, _ := json.Marshal(wire)
	if strings.Contains(string(b), `"content":"Hi"`) {
		t.Fatalf("string shorthand must not be used with a system prompt: %s", b)
	}
	if !strings.Contains(string(b), `"content":[{"type":"text","text":"Hi"}]`) {
		t.Fatalf("block array not rendered: %s", b)
	}
	// Multiple messages also render block arrays.
	req2 := &ir.Request{
		Model: "claude-sonnet-4-5",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "a"}}},
			{Role: ir.RoleAssistant, Content: []ir.Block{ir.TextBlock{Text: "b"}}},
		},
		Params: ir.Params{MaxTokens: ptr(int64(512))},
	}
	wire2, _, err := EncodeRequest(req2)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b2, _ := json.Marshal(wire2)
	if strings.Contains(string(b2), `"content":"a"`) {
		t.Fatalf("string shorthand must not be used with multiple messages: %s", b2)
	}
}

func TestDecodeResponse(t *testing.T) {
	wire := &Response{
		ID: "msg_01ABCdefGHI", Type: "message", Role: "assistant",
		Model:      "claude-sonnet-4-5",
		Content:    []BlockWire{{Type: "text", Text: "Hello there."}},
		StopReason: "end_turn",
		Usage:      &UsageWire{InputTokens: 9, OutputTokens: 3},
	}
	resp, losses, err := DecodeResponse(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if resp.StopReason != ir.StopEndTurn || resp.ID != wire.ID ||
		resp.Usage.InputTokens != 9 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("response not mapped: %+v", resp)
	}
}

func TestDecodeResponseToolUseBlocks(t *testing.T) {
	input := json.RawMessage("{\"city\":\"P" + string('\\') + "u0041ris\"}")
	wire := &Response{
		ID: "msg_01ABCdefGHI", Type: "message", Role: "assistant", Model: "claude-sonnet-4-5",
		Content: []BlockWire{
			{Type: "text", Text: "Checking."},
			{Type: "image", Source: &SourceWire{Type: "base64", MediaType: "image/png", Data: "aGVsbG8="}},
			{Type: "image", Source: &SourceWire{Type: "url", URL: "https://example.test/weather.png"}},
			{Type: "tool_use", ID: "toolu_1", Name: "weather", Input: input},
		},
		StopReason: "tool_use",
	}

	resp, losses, err := DecodeResponse(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	if resp.StopReason != ir.StopToolUse || len(resp.Content) != 4 {
		t.Fatalf("response blocks not mapped: %+v", resp)
	}
	base64Image, ok := resp.Content[1].(ir.ImageBlock)
	if !ok || base64Image.MediaType != "image/png" || base64Image.Data != "aGVsbG8=" || base64Image.URL != "" {
		t.Fatalf("base64 image not mapped: %+v", resp.Content[1])
	}
	urlImage, ok := resp.Content[2].(ir.ImageBlock)
	if !ok || urlImage.URL != "https://example.test/weather.png" || urlImage.MediaType != "" || urlImage.Data != "" {
		t.Fatalf("URL image not mapped: %+v", resp.Content[2])
	}
	toolUse, ok := resp.Content[3].(ir.ToolUseBlock)
	if !ok || toolUse.ID != "toolu_1" || toolUse.Name != "weather" {
		t.Fatalf("tool use not mapped: %+v", resp.Content[3])
	}
	var decodedInput string
	if err := json.Unmarshal(toolUse.Input, &decodedInput); err != nil || decodedInput != string(input) {
		t.Fatalf("tool use input changed: err=%v want=%q got=%q", err, input, decodedInput)
	}
}

func TestEncodeResponseEnvelopeDefaults(t *testing.T) {
	resp := &ir.Response{
		ID: "msg_01ABCdefGHI", Model: "claude-sonnet-4-5",
		Content:    []ir.Block{ir.TextBlock{Text: "Hello there."}},
		StopReason: ir.StopEndTurn,
		Usage:      ir.Usage{InputTokens: 9, OutputTokens: 3},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if wire.Type != "message" || wire.Role != "assistant" || wire.StopReason != "end_turn" {
		t.Fatalf("envelope wrong: %+v", wire)
	}
	if wire.Usage == nil || wire.Usage.InputTokens != 9 {
		t.Fatalf("usage wrong: %+v", wire.Usage)
	}
}

func TestEncodeRequestToolsChoicesAndImages(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)
	for _, tc := range []struct {
		name   string
		choice ir.ToolChoice
	}{
		{name: "auto", choice: ir.ToolChoice{Mode: "auto"}},
		{name: "any", choice: ir.ToolChoice{Mode: "any"}},
		{name: "named", choice: ir.ToolChoice{Mode: "tool", Name: "weather"}},
		{name: "none", choice: ir.ToolChoice{Mode: "none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &ir.Request{
				Model: "claude-sonnet-4-5",
				Tools: []ir.Tool{{
					Name: "weather", Description: "Get weather", InputSchema: schema,
				}},
				ToolChoice: &tc.choice,
				Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{
					ir.ImageBlock{MediaType: "image/png", Data: "aGVsbG8="},
					ir.ImageBlock{URL: "https://example.test/weather.png"},
				}}},
				Params: ir.Params{MaxTokens: ptr(int64(512))},
			}

			wire, losses, err := EncodeRequest(req)
			if err != nil || len(losses) != 0 {
				t.Fatalf("encode: err=%v losses=%+v", err, losses)
			}
			if len(wire.Tools) != 1 || wire.Tools[0].Name != "weather" ||
				wire.Tools[0].Description != "Get weather" || string(wire.Tools[0].InputSchema) != string(schema) {
				t.Fatalf("tools not encoded byte-faithfully: %+v", wire.Tools)
			}
			if wire.ToolChoice == nil || wire.ToolChoice.Type != tc.choice.Mode ||
				wire.ToolChoice.Name != tc.choice.Name {
				t.Fatalf("tool choice not encoded: %+v", wire.ToolChoice)
			}
			blocks, ok := wire.Messages[0].Content.([]BlockWire)
			if !ok || len(blocks) != 2 {
				t.Fatalf("image blocks not encoded: %#v", wire.Messages[0].Content)
			}
			if blocks[0].Type != "image" || blocks[0].Source == nil ||
				blocks[0].Source.Type != "base64" || blocks[0].Source.MediaType != "image/png" ||
				blocks[0].Source.Data != "aGVsbG8=" {
				t.Fatalf("base64 image not encoded: %+v", blocks[0])
			}
			if blocks[1].Type != "image" || blocks[1].Source == nil ||
				blocks[1].Source.Type != "url" || blocks[1].Source.URL != "https://example.test/weather.png" {
				t.Fatalf("URL image not encoded: %+v", blocks[1])
			}
		})
	}
}

func TestEncodeRequestToolResultBlocks(t *testing.T) {
	input := json.RawMessage(`"{\"city\":\"P\\u0041ris\"}"`)
	req := &ir.Request{
		Model: "claude-sonnet-4-5",
		Messages: []ir.Message{
			{Role: ir.RoleAssistant, Content: []ir.Block{
				ir.ToolUseBlock{ID: "toolu_1", Name: "weather", Input: input},
			}},
			{Role: ir.RoleUser, Content: []ir.Block{
				ir.ToolResultBlock{ToolUseID: "toolu_1", IsError: true, Content: []ir.Block{
					ir.TextBlock{Text: "Rain"},
					ir.ImageBlock{URL: "https://example.test/rain.png"},
				}},
			}},
		},
		Params: ir.Params{MaxTokens: ptr(int64(512))},
	}

	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%+v", err, losses)
	}
	wantInput := "{\"city\":\"P" + string('\\') + "u0041ris\"}"
	assistant, ok := wire.Messages[0].Content.([]BlockWire)
	if !ok || len(assistant) != 1 || string(assistant[0].Input) != wantInput {
		t.Fatalf("tool use not encoded byte-faithfully: %#v", wire.Messages[0].Content)
	}
	user, ok := wire.Messages[1].Content.([]BlockWire)
	if !ok || len(user) != 1 || user[0].Type != "tool_result" || user[0].ToolUseID != "toolu_1" || !user[0].IsError {
		t.Fatalf("tool result envelope not encoded: %#v", wire.Messages[1].Content)
	}
	content, ok := user[0].Content.([]BlockWire)
	if !ok || len(content) != 2 || content[0].Type != "text" || content[0].Text != "Rain" ||
		content[1].Type != "image" || content[1].Source == nil || content[1].Source.Type != "url" ||
		content[1].Source.URL != "https://example.test/rain.png" {
		t.Fatalf("tool result content not encoded: %#v", user[0].Content)
	}
}

func TestEncodeRequestToolUseInputRoundTripByteFidelity(t *testing.T) {
	input := json.RawMessage(`{"city":"PAris"}`)
	wire := &Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 512,
		Messages: []Message{{Role: "assistant", Content: []BlockWire{
			{Type: "tool_use", ID: "toolu_1", Name: "weather", Input: input},
		}}},
	}

	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	back, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%+v", err, losses)
	}
	blocks, ok := back.Messages[0].Content.([]BlockWire)
	if !ok || len(blocks) != 1 || string(blocks[0].Input) != string(input) {
		t.Fatalf("tool_use input changed: want=%q got=%#v", input, back.Messages[0].Content)
	}
}

func TestEncodeResponseToolUseAndStopSequence(t *testing.T) {
	input := json.RawMessage(`"{\"city\":\"P\\u0041ris\"}"`)
	resp := &ir.Response{
		ID: "msg_01ABCdefGHI", Model: "claude-sonnet-4-5",
		Content: []ir.Block{
			ir.TextBlock{Text: "Checking."},
			ir.ImageBlock{MediaType: "image/png", Data: "aGVsbG8="},
			ir.ToolUseBlock{ID: "toolu_1", Name: "weather", Input: input},
		},
		StopReason: ir.StopToolUse,
		Usage:      ir.Usage{InputTokens: 9, OutputTokens: 3},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%+v", err, losses)
	}
	// The escaped A in the source token must survive encode verbatim.
	wantRespInput := "{\"city\":\"P" + string('\\') + "u0041ris\"}"
	if wire.StopReason != "tool_use" || len(wire.Content) != 3 ||
		wire.Content[1].Source == nil || wire.Content[1].Source.Type != "base64" ||
		string(wire.Content[2].Input) != wantRespInput {
		t.Fatalf("response blocks not encoded: %+v", wire)
	}

	resp.StopReason = ir.StopSequence
	resp.StopSequence = "END"
	wire, losses, err = EncodeResponse(resp)
	if err != nil || len(losses) != 0 || wire.StopReason != "stop_sequence" || wire.StopSequence != "END" {
		t.Fatalf("stop sequence not encoded: wire=%+v err=%v losses=%+v", wire, err, losses)
	}
	resp.StopReason = ir.StopEndTurn
	wire, losses, err = EncodeResponse(resp)
	if err != nil || len(losses) != 0 || wire.StopSequence != "" {
		t.Fatalf("stop sequence must be omitted outside stop_sequence: wire=%+v err=%v losses=%+v", wire, err, losses)
	}
}

func TestEncodeRequestInvalidImageIsLoss(t *testing.T) {
	for _, image := range []ir.ImageBlock{
		{},
		{MediaType: "image/png", Data: "aGVsbG8=", URL: "https://example.test/image.png"},
	} {
		wire, losses, err := EncodeRequest(&ir.Request{
			Model:    "claude-sonnet-4-5",
			Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{image}}},
			Params:   ir.Params{MaxTokens: ptr(int64(512))},
		})
		if err != nil || wire == nil || len(losses) != 1 || losses[0].Reason != ir.LossUnsupportedSemantic {
			t.Fatalf("invalid image must be a loss: wire=%+v err=%v losses=%+v", wire, err, losses)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	wire := &Request{
		Model: "claude-sonnet-4-5",
		Messages: []Message{
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "What is 2+2?"},
			}},
			{Role: "assistant", Content: "4"},
			{Role: "user", Content: "And 3+3?"},
		},
		MaxTokens: 1024,
	}
	req, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	back, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if back.MaxTokens != 1024 || len(back.Messages) != 3 {
		t.Fatalf("round trip drifted: %+v", back)
	}
	b, _ := json.Marshal(back)
	if !strings.Contains(string(b), `"content":"And 3+3?"`) && !strings.Contains(string(b), `"And 3+3?"`) {
		t.Fatalf("third message drifted: %s", b)
	}
}

func TestMetadataLossesBothDirections(t *testing.T) {
	// face -> IR: wire metadata (user_id semantic) dropped with one loss.
	wire := &Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 512,
		Messages:  []Message{{Role: "user", Content: "Hi"}},
		Metadata:  map[string]any{"user_id": "u1"},
	}
	_, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(losses) != 1 || losses[0].Path != "metadata" || losses[0].Field != "metadata" ||
		losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("decode metadata loss wrong: %+v", losses)
	}
	// IR -> face: IR metadata map dropped with one loss.
	irReq := &ir.Request{
		Model:    "claude-sonnet-4-5",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}},
		Params:   ir.Params{MaxTokens: ptr(int64(512))},
		Metadata: map[string]string{"k": "v"},
	}
	out, losses, err := EncodeRequest(irReq)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Metadata != nil {
		t.Fatalf("IR metadata must not be rendered: %+v", out.Metadata)
	}
	if len(losses) != 1 || losses[0].Path != "metadata" || losses[0].Field != "metadata" ||
		losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("encode metadata loss wrong: %+v", losses)
	}
}

func TestWithModelMap(t *testing.T) {
	wire := &Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 512,
		Messages:  []Message{{Role: "user", Content: "Hi"}},
	}
	// face -> IR.
	req, _, err := DecodeRequest(wire, WithModelMap(modelmap.Table{"claude-sonnet-4-5": "claude-opus-4-1"}))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Model != "claude-opus-4-1" {
		t.Fatalf("decode model not mapped: %q", req.Model)
	}
	// No options: identity passthrough (spec/03 s2).
	reqDefault, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode default: %v", err)
	}
	if reqDefault.Model != "claude-sonnet-4-5" {
		t.Fatalf("default must be identity: %q", reqDefault.Model)
	}
	// IR -> face.
	irReq := &ir.Request{
		Model:    "claude-opus-4-1",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}},
		Params:   ir.Params{MaxTokens: ptr(int64(512))},
	}
	out, _, err := EncodeRequest(irReq, WithModelMap(modelmap.Table{"claude-opus-4-1": "claude-sonnet-4-5"}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Model != "claude-sonnet-4-5" {
		t.Fatalf("encode model not mapped: %q", out.Model)
	}
	// The table applies to both response directions too.
	irResp := &ir.Response{ID: "m", Model: "claude-sonnet-4-5", StopReason: ir.StopEndTurn}
	respOut, _, err := EncodeResponse(irResp, WithModelMap(modelmap.Table{"claude-sonnet-4-5": "claude-opus-4-1"}))
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	if respOut.Model != "claude-opus-4-1" {
		t.Fatalf("encode response model not mapped: %q", respOut.Model)
	}
	wireResp := &Response{
		ID: "m", Type: "message", Role: "assistant", Model: "claude-opus-4-1",
		StopReason: "end_turn",
	}
	respIn, _, err := DecodeResponse(wireResp, WithModelMap(modelmap.Table{"claude-opus-4-1": "claude-sonnet-4-5"}))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if respIn.Model != "claude-sonnet-4-5" {
		t.Fatalf("decode response model not mapped: %q", respIn.Model)
	}
}
