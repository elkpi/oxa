package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

func ptr[T any](v T) *T { return &v }

func TestDecodeRequestSystemAndParams(t *testing.T) {
	wire := &Request{
		Model: "gpt-4o-mini",
		Messages: []Message{
			{Role: "system", Content: "You are terse."},
			{Role: "user", Content: "Hi"},
		},
		Temperature: ptr(0.5),
		TopP:        ptr(0.9),
		MaxTokens:   ptr(int64(256)),
		Stop:        []string{"\n\n", "END"},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if len(req.System) != 1 || req.System[0].Text != "You are terse." {
		t.Fatalf("system not mapped: %+v", req.System)
	}
	if req.Params.Temperature == nil || *req.Params.Temperature != 0.5 {
		t.Fatalf("temperature not mapped: %+v", req.Params)
	}
	if len(req.Params.StopSequences) != 2 {
		t.Fatalf("stop not mapped: %+v", req.Params)
	}
}

func TestDecodeRequestLogprobsLosses(t *testing.T) {
	wire := &Request{
		Model:       "gpt-4o-mini",
		Messages:    []Message{{Role: "user", Content: "Hello"}},
		Logprobs:    true,
		TopLogprobs: 5,
	}
	_, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(losses) != 2 {
		t.Fatalf("expected 2 losses, got %+v", losses)
	}
	for _, l := range losses {
		if l.Reason != ir.LossUnmappedField {
			t.Fatalf("wrong reason: %+v", l)
		}
	}
}

func TestDecodeRequestPartsArray(t *testing.T) {
	wire := &Request{Model: "m", Messages: []Message{{
		Role:    "user",
		Content: []any{map[string]any{"type": "text", "text": "a"}, map[string]any{"type": "text", "text": "b"}},
	}}}
	req, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := req.Messages[0].Content
	if len(got) != 2 || got[0].(ir.TextBlock).Text != "a" || got[1].(ir.TextBlock).Text != "b" {
		t.Fatalf("parts not mapped: %+v", got)
	}
}

func TestDecodeResponseUsageAndFinish(t *testing.T) {
	wire := &Response{
		ID: "chatcmpl-abc123", Object: "chat.completion", Created: 1720000000,
		Model: "gpt-4o-mini-2024-07-18",
		Choices: []Choice{{
			Index: 0, Message: Message{Role: "assistant", Content: "Hello there."},
			FinishReason: "stop",
		}},
		Usage: &UsageWire{PromptTokens: 9, CompletionTokens: 3, TotalTokens: 12},
	}
	resp, losses, err := DecodeResponse(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if resp.StopReason != ir.StopEndTurn {
		t.Fatalf("finish_reason: %v", resp.StopReason)
	}
	if resp.Usage.InputTokens != 9 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("usage: %+v", resp.Usage)
	}
}

func TestDecodeResponseUnknownFinishReason(t *testing.T) {
	wire := &Response{
		ID: "id", Model: "m",
		Choices: []Choice{{Message: Message{Role: "assistant", Content: "x"}, FinishReason: "cosmic_ray"}},
	}
	resp, losses, err := DecodeResponse(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StopReason != ir.StopOther {
		t.Fatalf("want other, got %v", resp.StopReason)
	}
	if len(losses) != 1 || losses[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses: %+v", losses)
	}
}

func TestEncodeRequest(t *testing.T) {
	req := &ir.Request{
		Model:  "gpt-4o-mini",
		System: []ir.SystemBlock{{Text: "You are a terse assistant."}},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}},
		},
		Params: ir.Params{MaxTokens: ptr(int64(100))},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if len(wire.Messages) != 2 || wire.Messages[0].Role != "system" ||
		wire.Messages[0].Content != "You are a terse assistant." {
		t.Fatalf("system not rendered: %+v", wire.Messages)
	}
	if wire.MaxTokens == nil || *wire.MaxTokens != 100 {
		t.Fatalf("max_tokens not rendered: %+v", wire)
	}
}

func TestEncodeResponseDefaultsAndDerivedUsage(t *testing.T) {
	resp := &ir.Response{
		ID: "chatcmpl-abc123", Model: "gpt-4o-mini-2024-07-18",
		Content:    []ir.Block{ir.TextBlock{Text: "Hello there."}},
		StopReason: ir.StopEndTurn,
		Usage:      ir.Usage{InputTokens: 9, OutputTokens: 3},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if wire.Object != "chat.completion" || wire.Created != 0 || len(wire.Choices) != 1 {
		t.Fatalf("envelope defaults wrong: %+v", wire)
	}
	if wire.Usage == nil || wire.Usage.TotalTokens != 12 {
		t.Fatalf("total_tokens not derived: %+v", wire.Usage)
	}
}

func TestEncodeResponseStopSequenceLoss(t *testing.T) {
	resp := &ir.Response{
		ID: "id", Model: "m", StopReason: ir.StopSequence, StopSequence: "END",
		Usage: ir.Usage{InputTokens: 1, OutputTokens: 1},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if wire.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason: %q", wire.Choices[0].FinishReason)
	}
	if len(losses) != 1 || losses[0].Field != "stop_sequence" || losses[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses: %+v", losses)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	orig := &Request{
		Model: "gpt-4o-mini",
		Messages: []Message{
			{Role: "system", Content: "s"},
			{Role: "user", Content: "u"},
			{Role: "assistant", Content: "a"},
			{Role: "user", Content: "u2"},
		},
		Temperature: ptr(0.1),
	}
	req, _, err := DecodeRequest(orig)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	back, _, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if back.Model != orig.Model || back.Temperature == nil || *back.Temperature != 0.1 {
		t.Fatalf("round trip drifted: %+v", back)
	}
	if len(back.Messages) != 4 || back.Messages[0].Role != "system" || back.Messages[3].Content != "u2" {
		t.Fatalf("messages drifted: %+v", back.Messages)
	}
}

func TestMetadataLossesBothDirections(t *testing.T) {
	// face -> IR: wire metadata present is dropped with one unmapped-field loss.
	wire := &Request{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "Hi"}},
		Metadata: map[string]any{"user_id": "u1"},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Metadata != nil {
		t.Fatalf("wire metadata must be dropped: %+v", req.Metadata)
	}
	if len(losses) != 1 || losses[0].Path != "metadata" || losses[0].Field != "metadata" ||
		losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("decode metadata loss wrong: %+v", losses)
	}
	// IR -> face: IR metadata map is dropped with one unmapped-field loss.
	irReq := &ir.Request{
		Model:    "gpt-4o-mini",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}},
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

func TestDecodeRequestToolCallsAndConsecutiveResults(t *testing.T) {
	var wire Request
	if err := json.Unmarshal([]byte(`{
		"model":"gpt-4o-mini",
		"tools":[{"type":"function","function":{"name":"weather","description":"Get weather","parameters":{"type":"object"}}}],
		"tool_choice":"required",
		"messages":[
			{"role":"user","content":"Weather?"},
			{"role":"assistant","content":"Checking.","tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}},
				{"id":"call_2","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Lyon\"}"}}
			]},
			{"role":"tool","tool_call_id":"call_1","content":"Sunny"},
			{"role":"tool","tool_call_id":"call_2","content":"Rainy"},
			{"role":"assistant","content":"Done."}
		]
	}`), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	req, losses, err := DecodeRequest(&wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "weather" || req.ToolChoice == nil || req.ToolChoice.Mode != "any" {
		t.Fatalf("tools/tool choice not mapped: %+v %+v", req.Tools, req.ToolChoice)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("message count: %+v", req.Messages)
	}
	call, ok := req.Messages[1].Content[1].(ir.ToolUseBlock)
	if !ok || call.ID != "call_1" || string(call.Input) != `"{\"city\":\"Paris\"}"` {
		t.Fatalf("first tool call not mapped: %+v", req.Messages[1].Content)
	}
	if req.Messages[2].Role != ir.RoleUser || len(req.Messages[2].Content) != 2 {
		t.Fatalf("tool messages did not merge: %+v", req.Messages[2])
	}
	firstResult, ok := req.Messages[2].Content[0].(ir.ToolResultBlock)
	if !ok || firstResult.ToolUseID != "call_1" || len(firstResult.Content) != 1 || firstResult.Content[0].(ir.TextBlock).Text != "Sunny" {
		t.Fatalf("first tool result not mapped: %+v", req.Messages[2].Content)
	}
}

func TestDecodeRequestImageParts(t *testing.T) {
	var wire Request
	if err := json.Unmarshal([]byte(`{
		"model":"gpt-4o-mini",
		"messages":[{"role":"user","content":[
			{"type":"text","text":"Inspect these"},
			{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}},
			{"type":"image_url","image_url":{"url":"https://example.test/image.jpg"}}
		]}]
	}`), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	req, losses, err := DecodeRequest(&wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	blocks := req.Messages[0].Content
	if len(blocks) != 3 {
		t.Fatalf("image blocks: %+v", blocks)
	}
	dataImage, ok := blocks[1].(ir.ImageBlock)
	if !ok || dataImage.MediaType != "image/png" || dataImage.Data != "aGVsbG8=" {
		t.Fatalf("data image: %+v", blocks[1])
	}
	urlImage, ok := blocks[2].(ir.ImageBlock)
	if !ok || urlImage.URL != "https://example.test/image.jpg" {
		t.Fatalf("URL image: %+v", blocks[2])
	}
}

func TestEncodeRequestToolResultsAndImage(t *testing.T) {
	req := &ir.Request{
		Model:      "gpt-4o-mini",
		Tools:      []ir.Tool{{Name: "weather", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ToolChoice: &ir.ToolChoice{Mode: "tool", Name: "weather"},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{
				ir.ImageBlock{URL: "https://example.test/image.jpg"},
				ir.ImageBlock{MediaType: "image/png", Data: "aGVsbG8="},
			}},
			{Role: ir.RoleAssistant, Content: []ir.Block{ir.ToolUseBlock{ID: "call_1", Name: "weather", Input: json.RawMessage(`"{\"city\":\"Paris\"}"`)}}},
			{Role: ir.RoleUser, Content: []ir.Block{ir.ToolResultBlock{ToolUseID: "call_1", Content: []ir.Block{ir.TextBlock{Text: "Sunny"}}}}},
		},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%+v", err, losses)
	}
	out, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got["tool_choice"].(map[string]any)["function"].(map[string]any)["name"] != "weather" {
		t.Fatalf("tool choice: %s", out)
	}
	messages := got["messages"].([]any)
	userParts := messages[0].(map[string]any)["content"].([]any)
	if userParts[0].(map[string]any)["image_url"].(map[string]any)["url"] != "https://example.test/image.jpg" ||
		userParts[1].(map[string]any)["image_url"].(map[string]any)["url"] != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("images not rendered: %s", out)
	}
	assistant := messages[1].(map[string]any)
	if assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"] != `{"city":"Paris"}` {
		t.Fatalf("tool call: %s", out)
	}
	tool := messages[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != "Sunny" {
		t.Fatalf("tool result: %s", out)
	}
}

func TestEncodeRequestToolResultOrderingLoss(t *testing.T) {
	// N-CC-9: tool messages are hoisted ahead of the trailing user content.
	// A user turn mixing ordinary content with tool results records exactly
	// one degraded loss when the hoisting does not preserve the source order;
	// a turn carrying only tool results (the normal post-tool-call case)
	// records none.
	cases := []struct {
		name       string
		content    []ir.Block
		wantTurn   []Message
		wantLosses int
	}{
		{
			name: "mixed turn records one degraded loss",
			content: []ir.Block{
				ir.TextBlock{Text: "Answer:"},
				ir.ToolResultBlock{ToolUseID: "call_1", Content: []ir.Block{ir.TextBlock{Text: "Sunny"}}},
			},
			wantTurn: []Message{
				{Role: "tool", Content: "Sunny", ToolCallID: "call_1"},
				{Role: "user", Content: "Answer:"},
			},
			wantLosses: 1,
		},
		{
			name: "pure tool-result turn records no loss",
			content: []ir.Block{
				ir.ToolResultBlock{ToolUseID: "call_1", Content: []ir.Block{ir.TextBlock{Text: "Sunny"}}},
			},
			wantTurn: []Message{
				{Role: "tool", Content: "Sunny", ToolCallID: "call_1"},
			},
			wantLosses: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ir.Request{
				Model: "gpt-4o-mini",
				Messages: []ir.Message{
					{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Weather?"}}},
					{Role: ir.RoleAssistant, Content: []ir.Block{
						ir.TextBlock{Text: "Checking."},
						ir.ToolUseBlock{ID: "call_1", Name: "weather", Input: json.RawMessage(`"{\"city\":\"Paris\"}"`)},
					}},
					{Role: ir.RoleUser, Content: tc.content},
				},
			}
			out, losses, err := EncodeRequest(req)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			// The turn under test is messages[2]; the fixed prefix renders as
			// exactly two wire messages before it.
			got := out.Messages[2:]
			if len(got) != len(tc.wantTurn) {
				t.Fatalf("turn wire messages:\nwant %+v\ngot  %+v", tc.wantTurn, got)
			}
			for i := range tc.wantTurn {
				if got[i].Role != tc.wantTurn[i].Role || got[i].Content != tc.wantTurn[i].Content ||
					got[i].ToolCallID != tc.wantTurn[i].ToolCallID {
					t.Fatalf("turn wire message %d:\nwant %+v\ngot  %+v", i, tc.wantTurn[i], got[i])
				}
			}
			if len(losses) != tc.wantLosses {
				t.Fatalf("losses:\nwant %d\ngot  %+v", tc.wantLosses, losses)
			}
			if tc.wantLosses > 0 {
				l := losses[0]
				if l.Reason != ir.LossDegraded || l.Path != "messages[2]" || l.Field != "ordering" {
					t.Fatalf("degraded loss: %+v", l)
				}
			}
		})
	}
}

func TestMalformedAndNonImageDataURLsProduceLosses(t *testing.T) {
	wire := &Request{Model: "m", Messages: []Message{{
		Role: "user",
		Content: []any{
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:text/plain;base64,aGVsbG8="}},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png,aGVsbG8="}},
		},
	}}}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Messages[0].Content) != 1 || req.Messages[0].Content[0] != (ir.TextBlock{}) {
		t.Fatalf("unsupported images must leave an empty text block: %+v", req.Messages[0].Content)
	}
	if len(losses) != 2 {
		t.Fatalf("want two semantic losses, got %+v", losses)
	}
	for _, loss := range losses {
		if loss.Reason != ir.LossUnsupportedSemantic {
			t.Fatalf("wrong loss: %+v", loss)
		}
	}
}

func TestToolArgumentsPreserveRawText(t *testing.T) {
	const arguments = "{\"city\":\"\\u0041BC\"}"
	wire := &Request{
		Model: "gpt-4o-mini",
		Messages: []Message{
			{Role: "user", Content: "Weather?"},
			{Role: "assistant", ToolCalls: []ToolCall{{
				ID: "call_1", Type: "function",
				Function: FunctionWire{Name: "weather", Arguments: arguments},
			}}},
		},
	}

	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	call, ok := req.Messages[1].Content[0].(ir.ToolUseBlock)
	if !ok {
		t.Fatalf("assistant tool call not mapped: %+v", req.Messages[1].Content)
	}
	if got, want := string(call.Input), `"{\"city\":\"\\u0041BC\"}"`; got != want {
		t.Fatalf("tool input token changed:\nwant %s\ngot  %s", want, got)
	}

	out, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%+v", err, losses)
	}
	if got := out.Messages[1].ToolCalls[0].Function.Arguments; got != arguments {
		t.Fatalf("function arguments changed:\nwant %s\ngot  %s", arguments, got)
	}
}

func TestToolChoiceMappings(t *testing.T) {
	named := ToolChoiceWire{Type: "function"}
	named.Function.Name = "weather"
	cases := []struct {
		name     string
		wire     any
		mode     string
		toolName string
	}{
		{name: "auto", wire: "auto", mode: "auto"},
		{name: "none", wire: "none", mode: "none"},
		{name: "required", wire: "required", mode: "any"},
		{name: "named", wire: named, mode: "tool", toolName: "weather"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &Request{Model: "m", ToolChoice: tc.wire, Messages: []Message{{Role: "user", Content: "x"}}}
			req, losses, err := DecodeRequest(in)
			if err != nil || len(losses) != 0 {
				t.Fatalf("decode: err=%v losses=%+v", err, losses)
			}
			if req.ToolChoice == nil || req.ToolChoice.Mode != tc.mode || req.ToolChoice.Name != tc.toolName {
				t.Fatalf("decoded choice: %+v", req.ToolChoice)
			}
			out, losses, err := EncodeRequest(req)
			if err != nil || len(losses) != 0 {
				t.Fatalf("encode: err=%v losses=%+v", err, losses)
			}
			switch want := tc.wire.(type) {
			case string:
				if got, ok := out.ToolChoice.(string); !ok || got != want {
					t.Fatalf("encoded choice: %#v", out.ToolChoice)
				}
			case ToolChoiceWire:
				got, ok := out.ToolChoice.(ToolChoiceWire)
				if !ok || got.Type != want.Type || got.Function.Name != want.Function.Name {
					t.Fatalf("encoded choice: %#v", out.ToolChoice)
				}
			}
		})
	}
}

func TestUnsupportedRequestContentProducesLoss(t *testing.T) {
	wire := &Request{
		Model:             "m",
		ParallelToolCalls: ptr(true),
		FunctionCall:      map[string]any{"name": "legacy"},
		Messages: []Message{{
			Role:    "user",
			Content: []any{map[string]any{"type": "audio_url", "audio_url": map[string]any{"url": "https://example.test/a.wav"}}},
		}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 1 || req.Messages[0].Content[0] != (ir.TextBlock{}) {
		t.Fatalf("unsupported content must leave an empty text block: %+v", req.Messages)
	}
	if len(losses) != 3 {
		t.Fatalf("want three losses, got %+v", losses)
	}
	for _, loss := range losses {
		if loss.Reason != ir.LossUnmappedField && loss.Reason != ir.LossUnsupportedSemantic {
			t.Fatalf("unexpected loss: %+v", loss)
		}
	}
}

func TestEncodeToolResultUnsupportedContentRecordsLoss(t *testing.T) {
	req := &ir.Request{
		Model: "m",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "x"}}},
			{Role: ir.RoleAssistant, Content: []ir.Block{ir.ToolUseBlock{ID: "call_1", Name: "weather", Input: json.RawMessage(`"{}"`)}}},
			{Role: ir.RoleUser, Content: []ir.Block{ir.ToolResultBlock{
				ToolUseID: "call_1",
				Content:   []ir.Block{ir.TextBlock{Text: "sunny"}, ir.ImageBlock{URL: "https://example.test/weather.png"}},
			}}},
		},
	}
	out, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(out.Messages) != 3 || out.Messages[2].Content != "sunny" {
		t.Fatalf("tool result not rendered: %+v", out.Messages)
	}
	if len(losses) != 1 || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Fatalf("unsupported tool result content needs semantic loss: %+v", losses)
	}
}

func TestWithModelMap(t *testing.T) {
	wire := &Request{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	}
	// face -> IR: model name mapped through the table.
	req, _, err := DecodeRequest(wire, WithModelMap(modelmap.Table{"gpt-4o-mini": "gpt-4o"}))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Model != "gpt-4o" {
		t.Fatalf("decode model not mapped: %q", req.Model)
	}
	// No options: identity passthrough (spec/03 s2).
	reqDefault, _, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode default: %v", err)
	}
	if reqDefault.Model != "gpt-4o-mini" {
		t.Fatalf("default must be identity: %q", reqDefault.Model)
	}
	// IR -> face: model name mapped back through the table.
	irReq := &ir.Request{
		Model:    "gpt-4o",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}},
	}
	out, _, err := EncodeRequest(irReq, WithModelMap(modelmap.Table{"gpt-4o": "gpt-4o-mini"}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Model != "gpt-4o-mini" {
		t.Fatalf("encode model not mapped: %q", out.Model)
	}
	// The table applies to both response directions too.
	irResp := &ir.Response{ID: "i", Model: "gpt-4o", StopReason: ir.StopEndTurn}
	respOut, _, err := EncodeResponse(irResp, WithModelMap(modelmap.Table{"gpt-4o": "gpt-4o-mini"}))
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	if respOut.Model != "gpt-4o-mini" {
		t.Fatalf("encode response model not mapped: %q", respOut.Model)
	}
	wireResp := &Response{
		ID: "i", Model: "gpt-4o-mini",
		Choices: []Choice{{Message: Message{Role: "assistant", Content: "x"}, FinishReason: "stop"}},
	}
	respIn, _, err := DecodeResponse(wireResp, WithModelMap(modelmap.Table{"gpt-4o-mini": "gpt-4o"}))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if respIn.Model != "gpt-4o" {
		t.Fatalf("decode response model not mapped: %q", respIn.Model)
	}
}
