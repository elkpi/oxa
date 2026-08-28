package responses

import (
	"encoding/json"
	"testing"

	"github.com/oxa-protocol/oxa/go/ir"
	"github.com/oxa-protocol/oxa/go/modelmap"
)

func ptr[T any](v T) *T { return &v }

func TestDecodeRequestInstructionsAndSystemOrdering(t *testing.T) {
	wire := &Request{
		Model:        "gpt-4o-mini",
		Instructions: ptr("Instructions first."),
		Input: Input{Items: []InputItem{
			{Role: "system", Content: "System item second."},
			{Role: "system", Content: "System item third."},
			{Role: "user", Content: "Hi"},
		}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	want := []string{"Instructions first.", "System item second.", "System item third."}
	if len(req.System) != len(want) {
		t.Fatalf("system blocks: %+v", req.System)
	}
	for i, text := range want {
		if req.System[i].Text != text {
			t.Fatalf("system order wrong at %d: %+v", i, req.System)
		}
	}
}

func TestDecodeRequestStringShorthand(t *testing.T) {
	wire := &Request{Model: "gpt-4o-mini", Input: Input{Text: ptr("Hello")}}
	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != ir.RoleUser {
		t.Fatalf("messages: %+v", req.Messages)
	}
	if text, ok := req.Messages[0].Content[0].(ir.TextBlock); !ok || text.Text != "Hello" {
		t.Fatalf("shorthand not mapped: %+v", req.Messages[0].Content)
	}
}

func TestDecodeRequestParamsNameMapping(t *testing.T) {
	wire := &Request{
		Model:           "gpt-4o-mini",
		Input:           Input{Text: ptr("Hello")},
		Temperature:     ptr(0.5),
		TopP:            ptr(0.9),
		MaxOutputTokens: ptr(int64(256)),
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%v", err, losses)
	}
	if req.Params.MaxTokens == nil || *req.Params.MaxTokens != 256 {
		t.Fatalf("max_output_tokens not mapped to MaxTokens: %+v", req.Params)
	}
}

func TestDecodeRequestStopSequenceLossOnEncode(t *testing.T) {
	req := &ir.Request{
		Model:    "gpt-4o-mini",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}},
		Params:   ir.Params{StopSequences: []string{"END"}},
	}
	out, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(losses) != 1 || losses[0].Field != "stop_sequences" || losses[0].Reason != ir.LossUnmappedField {
		t.Fatalf("losses: %+v", losses)
	}
	if out.MaxOutputTokens != nil {
		t.Fatalf("unexpected max_output_tokens: %+v", out)
	}
}

func TestMetadataLossesBothDirections(t *testing.T) {
	// face -> IR: wire metadata present is dropped with one unmapped-field loss.
	wire := &Request{
		Model:    "gpt-4o-mini",
		Input:    Input{Text: ptr("Hi")},
		Metadata: map[string]string{"user_id": "u1"},
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

func TestDecodeRequestToolConversationAndMerging(t *testing.T) {
	var wire Request
	if err := json.Unmarshal([]byte(`{
		"model":"gpt-4o-mini",
		"tools":[{"type":"function","name":"weather","description":"Get weather","parameters":{"type":"object"},"strict":true}],
		"tool_choice":"required",
		"input":[
			{"role":"user","content":"Weather?"},
			{"type":"function_call","call_id":"call_1","name":"weather","arguments":"{\"city\":\"Paris\"}"},
			{"type":"function_call","call_id":"call_2","name":"weather","arguments":"{\"city\":\"Lyon\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"Sunny"},
			{"type":"function_call_output","call_id":"call_2","output":"Rainy"},
			{"role":"assistant","content":"Done."}
		]
	}`), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	req, losses, err := DecodeRequest(&wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// one strict loss; tool_choice required -> any is loss-free
	if len(losses) != 1 || losses[0].Field != "strict" {
		t.Fatalf("losses: %+v", losses)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "weather" || req.ToolChoice == nil || req.ToolChoice.Mode != "any" {
		t.Fatalf("tools/tool choice: %+v %+v", req.Tools, req.ToolChoice)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("message count: %+v", req.Messages)
	}
	assistant := req.Messages[1]
	if assistant.Role != ir.RoleAssistant || len(assistant.Content) != 2 {
		t.Fatalf("function calls did not merge: %+v", assistant)
	}
	call, ok := assistant.Content[0].(ir.ToolUseBlock)
	if !ok || call.ID != "call_1" || string(call.Input) != `"{\"city\":\"Paris\"}"` {
		t.Fatalf("function call not mapped: %+v", assistant.Content)
	}
	results := req.Messages[2]
	if results.Role != ir.RoleUser || len(results.Content) != 2 {
		t.Fatalf("outputs did not merge: %+v", results)
	}
	first, ok := results.Content[0].(ir.ToolResultBlock)
	if !ok || first.ToolUseID != "call_1" || first.Content[0].(ir.TextBlock).Text != "Sunny" {
		t.Fatalf("output not mapped: %+v", results.Content)
	}
}

func TestToolArgumentsPreserveRawText(t *testing.T) {
	const arguments = "{\"city\":\"\\u0041BC\"}"
	var wire Request
	if err := json.Unmarshal([]byte(`{
		"model":"gpt-4o-mini",
		"input":[
			{"role":"user","content":"Weather?"},
			{"type":"function_call","call_id":"call_1","name":"weather","arguments":"{\"city\":\"\\u0041BC\"}"}
		]
	}`), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	req, losses, err := DecodeRequest(&wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	call, ok := req.Messages[1].Content[0].(ir.ToolUseBlock)
	if !ok {
		t.Fatalf("function call not mapped: %+v", req.Messages[1].Content)
	}
	if got, want := string(call.Input), `"{\"city\":\"\\u0041BC\"}"`; got != want {
		t.Fatalf("tool input token changed:\nwant %s\ngot  %s", want, got)
	}
	out, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%+v", err, losses)
	}
	if got := out.Input.Items[1].Arguments; got != arguments {
		t.Fatalf("arguments changed:\nwant %s\ngot  %s", arguments, got)
	}
}

func TestDecodeResponseStatuses(t *testing.T) {
	base := func() *Response {
		return &Response{
			ID: "resp_1", Object: "response", Model: "gpt-4o-mini",
			Output: []OutputItem{{
				Type: "message", ID: "msg_1", Status: "completed", Role: "assistant",
				Content: []OutputContent{{Type: "output_text", Text: "Hi", Annotations: []json.RawMessage{}}},
			}},
			Usage: &UsageWire{InputTokens: 9, OutputTokens: 3},
		}
	}
	cases := []struct {
		name       string
		mutate     func(*Response)
		stop       ir.StopReason
		wantLosses int
	}{
		{"completed", func(*Response) {}, ir.StopEndTurn, 0},
		{"incomplete max_output_tokens", func(w *Response) {
			w.Status = "incomplete"
			w.IncompleteDetails = &IncompleteWire{Reason: "max_output_tokens"}
		}, ir.StopMaxTokens, 0},
		{"incomplete content_filter", func(w *Response) {
			w.Status = "incomplete"
			w.IncompleteDetails = &IncompleteWire{Reason: "content_filter"}
		}, ir.StopOther, 1},
		{"failed with error", func(w *Response) {
			w.Status = "failed"
			w.Error = &ErrorWire{Code: "rate_limit_exceeded", Message: "slow down"}
		}, ir.StopOther, 1},
		{"missing usage zeros", func(w *Response) { w.Usage = nil }, ir.StopEndTurn, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire := base()
			wire.Status = "completed"
			tc.mutate(wire)
			if wire.Status == "" {
				wire.Status = "completed"
			}
			resp, losses, err := DecodeResponse(wire)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.StopReason != tc.stop {
				t.Fatalf("stop reason: %v", resp.StopReason)
			}
			if len(losses) != tc.wantLosses {
				t.Fatalf("losses: %+v", losses)
			}
			if wire.Usage == nil && (resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0) {
				t.Fatalf("usage must be zeros: %+v", resp.Usage)
			}
		})
	}
}

func TestDecodeResponseToolCallAndReasoningLoss(t *testing.T) {
	var wire Response
	if err := json.Unmarshal([]byte(`{
		"id":"resp_2","object":"response","status":"completed","model":"gpt-4o-mini",
		"output":[
			{"type":"reasoning","id":"rs_1","summary":[]},
			{"type":"function_call","id":"fc_1","call_id":"call_9","name":"weather","arguments":"{\"city\":\"Paris\"}"},
			{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[
				{"type":"output_text","text":"Done.","annotations":[{"type":"url_citation"}]}
			]}
		],
		"usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}
	}`), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp, losses, err := DecodeResponse(&wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StopReason != ir.StopToolUse {
		t.Fatalf("stop reason: %v", resp.StopReason)
	}
	// one reasoning-item loss, one annotations loss
	if len(losses) != 2 {
		t.Fatalf("losses: %+v", losses)
	}
	var blocks []ir.Block = resp.Content
	if len(blocks) != 2 {
		t.Fatalf("content: %+v", blocks)
	}
	if _, ok := blocks[0].(ir.ToolUseBlock); !ok {
		t.Fatalf("function call must precede text: %+v", blocks)
	}
}

func TestEncodeResponseDefaultsAndStopReasons(t *testing.T) {
	resp := &ir.Response{
		ID: "resp_abc123", Model: "gpt-4o-mini",
		Content:    []ir.Block{ir.TextBlock{Text: "Hello there."}},
		StopReason: ir.StopEndTurn,
		Usage:      ir.Usage{InputTokens: 9, OutputTokens: 3},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if wire.Object != "response" || wire.Status != "completed" || len(wire.Output) != 1 {
		t.Fatalf("envelope defaults wrong: %+v", wire)
	}
	item := wire.Output[0]
	if item.ID != "msg_abc123" || item.Status != "completed" || item.Role != "assistant" ||
		len(item.Content) != 1 || len(item.Content[0].Annotations) != 0 {
		t.Fatalf("message item defaults wrong: %+v", item)
	}
	if wire.Usage == nil || wire.Usage.TotalTokens != 12 {
		t.Fatalf("total_tokens not derived: %+v", wire.Usage)
	}

	maxTokens, _, err := EncodeResponse(&ir.Response{
		ID: "r", Model: "m", Content: []ir.Block{ir.TextBlock{Text: "x"}}, StopReason: ir.StopMaxTokens,
	})
	if err != nil || maxTokens.Status != "incomplete" ||
		maxTokens.IncompleteDetails == nil || maxTokens.IncompleteDetails.Reason != "max_output_tokens" {
		t.Fatalf("max_tokens status: %+v", maxTokens)
	}
	refusal, _, err := EncodeResponse(&ir.Response{
		ID: "r", Model: "m", Content: []ir.Block{ir.TextBlock{Text: "x"}}, StopReason: ir.StopRefusal,
	})
	if err != nil || refusal.Status != "failed" || refusal.Error == nil || refusal.Error.Code != "refusal" {
		t.Fatalf("refusal status: %+v", refusal)
	}
	sequence, seqLosses, err := EncodeResponse(&ir.Response{
		ID: "r", Model: "m", Content: []ir.Block{ir.TextBlock{Text: "x"}}, StopReason: ir.StopSequence,
	})
	if err != nil || sequence.Status != "completed" || len(seqLosses) != 1 || seqLosses[0].Field != "stop_sequence" {
		t.Fatalf("stop_sequence: %+v %+v", sequence, seqLosses)
	}
}

func TestEncodeResponseToolUse(t *testing.T) {
	resp := &ir.Response{
		ID: "resp_3", Model: "gpt-4o-mini",
		Content: []ir.Block{
			ir.TextBlock{Text: "Checking."},
			ir.ToolUseBlock{ID: "call_9", Name: "weather", Input: json.RawMessage(`"{\"city\":\"Paris\"}"`)},
		},
		StopReason: ir.StopToolUse,
		Usage:      ir.Usage{InputTokens: 10, OutputTokens: 5},
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if len(wire.Output) != 2 {
		t.Fatalf("output: %+v", wire.Output)
	}
	if wire.Output[0].Type != "message" || wire.Output[1].Type != "function_call" {
		t.Fatalf("output order: %+v", wire.Output)
	}
	call := wire.Output[1]
	if call.ID != "fc_abc123" || call.CallID != "call_9" || call.Name != "weather" ||
		call.Arguments != `{"city":"Paris"}` {
		t.Fatalf("function_call item: %+v", call)
	}
}

func TestEncodeRequestStringShorthandAndImages(t *testing.T) {
	// One user text message renders as the string shorthand.
	req := &ir.Request{
		Model:    "gpt-4o-mini",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hello"}}}},
	}
	out, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: err=%v losses=%v", err, losses)
	}
	if out.Input.Text == nil || *out.Input.Text != "Hello" {
		t.Fatalf("shorthand not rendered: %+v", out.Input)
	}
	// Images render as input_image parts in an item array.
	req.Messages[0].Content = []ir.Block{
		ir.TextBlock{Text: "Inspect"},
		ir.ImageBlock{URL: "https://example.test/image.jpg"},
		ir.ImageBlock{MediaType: "image/png", Data: "aGVsbG8="},
	}
	out, _, err = EncodeRequest(req)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Input.Text != nil || len(out.Input.Items) != 1 {
		t.Fatalf("item array not rendered: %+v", out.Input)
	}
	parts, ok := out.Input.Items[0].Content.([]ContentPart)
	if !ok || len(parts) != 3 || parts[1].ImageURL != "https://example.test/image.jpg" ||
		parts[2].ImageURL != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("parts: %+v", out.Input.Items[0].Content)
	}
}

func TestUnknownInputItemAndVerbosityLosses(t *testing.T) {
	wire := &Request{
		Model: "gpt-4o-mini",
		Input: Input{Items: []InputItem{
			{Type: "web_search_call"},
			{Role: "user", Content: "Hi"},
		}},
		Text: &TextParams{Verbosity: ptr("low"), Format: map[string]any{"type": "json_object"}},
	}
	req, losses, err := DecodeRequest(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("messages: %+v", req.Messages)
	}
	if len(losses) != 3 {
		t.Fatalf("want three losses (item type, verbosity, format), got %+v", losses)
	}
}

func TestWithModelMap(t *testing.T) {
	wire := &Request{Model: "gpt-4o-mini", Input: Input{Text: ptr("Hi")}}
	req, _, err := DecodeRequest(wire, WithModelMap(modelmap.Table{"gpt-4o-mini": "gpt-4o"}))
	if err != nil || req.Model != "gpt-4o" {
		t.Fatalf("decode model not mapped: %q err=%v", req.Model, err)
	}
	irReq := &ir.Request{
		Model:    "gpt-4o",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "Hi"}}}},
	}
	out, _, err := EncodeRequest(irReq, WithModelMap(modelmap.Table{"gpt-4o": "gpt-4o-mini"}))
	if err != nil || out.Model != "gpt-4o-mini" {
		t.Fatalf("encode model not mapped: %q err=%v", out.Model, err)
	}
	irResp := &ir.Response{ID: "i", Model: "gpt-4o", StopReason: ir.StopEndTurn}
	respOut, _, err := EncodeResponse(irResp, WithModelMap(modelmap.Table{"gpt-4o": "gpt-4o-mini"}))
	if err != nil || respOut.Model != "gpt-4o-mini" {
		t.Fatalf("encode response model not mapped: %q err=%v", respOut.Model, err)
	}
	wireResp := &Response{
		ID: "i", Object: "response", Status: "completed", Model: "gpt-4o-mini",
		Output: []OutputItem{{Type: "message", Content: []OutputContent{{Type: "output_text", Text: "x"}}}},
	}
	respIn, _, err := DecodeResponse(wireResp, WithModelMap(modelmap.Table{"gpt-4o-mini": "gpt-4o"}))
	if err != nil || respIn.Model != "gpt-4o" {
		t.Fatalf("decode response model not mapped: %q err=%v", respIn.Model, err)
	}
}

func TestEncodeRequestShorthandConditions(t *testing.T) {
	single := func(content []ir.Block, system []ir.SystemBlock) *ir.Request {
		return &ir.Request{
			Model:    "gpt-4o-mini",
			System:   system,
			Messages: []ir.Message{{Role: ir.RoleUser, Content: content}},
		}
	}
	// One text block, no system: the string shorthand.
	out, _, err := EncodeRequest(single([]ir.Block{ir.TextBlock{Text: "Hi"}}, nil))
	if err != nil || out.Input.Text == nil || *out.Input.Text != "Hi" {
		t.Fatalf("shorthand must render: err=%v input=%+v", err, out.Input)
	}
	// Instructions present: the array form even for a single text block.
	out, _, err = EncodeRequest(single([]ir.Block{ir.TextBlock{Text: "Hi"}},
		[]ir.SystemBlock{{Text: "You are terse."}}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Input.Text != nil || len(out.Input.Items) != 1 || out.Input.Items[0].Role != "user" {
		t.Fatalf("instructions must force the array form: %+v", out.Input)
	}
	if out.Instructions == nil || *out.Instructions != "You are terse." {
		t.Fatalf("instructions not rendered: %+v", out.Instructions)
	}
	// One user message, two text blocks: the array form with a parts array.
	out, _, err = EncodeRequest(single([]ir.Block{
		ir.TextBlock{Text: "Hello, "}, ir.TextBlock{Text: "world."},
	}, nil))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if out.Input.Text != nil || len(out.Input.Items) != 1 {
		t.Fatalf("two text blocks must force the array form: %+v", out.Input)
	}
	parts, ok := out.Input.Items[0].Content.([]ContentPart)
	if !ok || len(parts) != 2 || parts[0].Type != "input_text" || parts[1].Text != "world." {
		t.Fatalf("two text blocks must render as parts: %+v", out.Input.Items[0].Content)
	}
}

func TestDecodeAssistantRunOrdersTextBeforeToolUse(t *testing.T) {
	var wire Request
	if err := json.Unmarshal([]byte(`{
		"model":"gpt-4o-mini",
		"input":[
			{"role":"user","content":"Weather?"},
			{"type":"function_call","call_id":"call_1","name":"weather","arguments":"{\"city\":\"Paris\"}"},
			{"role":"assistant","content":"Checking."}
		]
	}`), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	req, losses, err := DecodeRequest(&wire)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: err=%v losses=%+v", err, losses)
	}
	if len(req.Messages) != 2 || req.Messages[1].Role != ir.RoleAssistant {
		t.Fatalf("messages: %+v", req.Messages)
	}
	content := req.Messages[1].Content
	if len(content) != 2 {
		t.Fatalf("run did not merge: %+v", content)
	}
	if _, ok := content[0].(ir.TextBlock); !ok {
		t.Fatalf("text must come first: %+v", content)
	}
	if _, ok := content[1].(ir.ToolUseBlock); !ok {
		t.Fatalf("tool use must come second: %+v", content)
	}
}
