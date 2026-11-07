package jev

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v3/ir"
)

func TestEncodeResponseWithContextRecoversTypes(t *testing.T) {
	resp := &ir.Response{
		Model:      "jev-latest",
		Content:    []ir.Block{ir.TextBlock{Text: `{"urgent": {"noul": 1.5}, "sentiment": {"choice": "angry"}}`}},
		StopReason: ir.StopEndTurn,
	}
	qs := map[string]Question{
		"urgent":    {Type: QuestionTypeNoul},
		"sentiment": {Type: QuestionTypeChoice, Criteria: json.RawMessage(`{"mixed":"m","negative":"n","positive":"p"}`)},
	}
	wire, losses, err := EncodeResponse(resp, WithQuestions(qs))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if got := string(wire.Answers["urgent"]); got != `{"noul":1.5,"type":"noul"}` {
		t.Errorf("urgent answer = %s, want typed noul", got)
	}
	if got := string(wire.Answers["sentiment"]); got != `{"choice":"angry","type":"choice"}` {
		t.Errorf("sentiment answer = %s, want typed choice", got)
	}
	// urgent noul out of [0,1] and sentiment choice outside criteria: two
	// N-JEV-14 losses; no N-JEV-13 (context resolves the type).
	var degraded int
	for _, l := range losses {
		if l.Reason == ir.LossDegraded && l.Path == "answers.urgent" {
			degraded++
		}
		if l.Reason == ir.LossDegraded && l.Path == "answers.sentiment" {
			degraded++
		}
		if l.Reason == ir.LossUnmappedValue {
			t.Errorf("unexpected N-JEV-12 loss for matching-type answers: %+v", l)
		}
	}
	if degraded != 2 {
		t.Errorf("expected 2 N-JEV-14 losses, got %d (%+v)", degraded, losses)
	}
}

func TestEncodeResponseWithContextTypeMismatchCarriesVerbatim(t *testing.T) {
	resp := &ir.Response{
		Model:      "jev-latest",
		Content:    []ir.Block{ir.TextBlock{Text: `{"lvl": {"type": "noul", "noul": 0.5}}`}},
		StopReason: ir.StopEndTurn,
	}
	qs := map[string]Question{"lvl": {Type: QuestionTypeScore, Criteria: json.RawMessage(`["low","high"]`)}}
	wire, losses, err := EncodeResponse(resp, WithQuestions(qs))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if got := string(wire.Answers["lvl"]); got != `{"type":"noul","noul":0.5}` {
		t.Errorf("answer was rewritten: %s", got)
	}
	if len(losses) != 1 || losses[0].Reason != ir.LossUnmappedValue || losses[0].Field != "type" || losses[0].Path != "answers.lvl" {
		t.Errorf("expected one N-JEV-12 loss, got %+v", losses)
	}
}

func TestEncodeResponseErrors(t *testing.T) {
	cases := []struct {
		name string
		resp *ir.Response
		want string
	}{
		{
			name: "no text content",
			resp: &ir.Response{Model: "m", Content: nil, StopReason: ir.StopEndTurn},
			want: "no text content",
		},
		{
			name: "not a JSON object",
			resp: &ir.Response{Model: "m", Content: []ir.Block{ir.TextBlock{Text: "prose answer"}}, StopReason: ir.StopEndTurn},
			want: "not a JSON object",
		},
		{
			name: "answer without recognized value",
			resp: &ir.Response{Model: "m", Content: []ir.Block{ir.TextBlock{Text: `{"a": {"other": 1}}`}}, StopReason: ir.StopEndTurn},
			want: "no recognized value",
		},
		{
			name: "scalar object missing value",
			resp: &ir.Response{Model: "m", Content: []ir.Block{ir.TextBlock{Text: `{"a": {"type": "choice"}}`}}, StopReason: ir.StopEndTurn},
			want: "no choice value",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := EncodeResponse(tc.resp)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestDecodeRequestErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"missing model", `{"state": "s", "questions": {"q": {"type": "noul"}}}`, "model is required"},
		{"missing state", `{"model": "m", "questions": {"q": {"type": "noul"}}}`, "state is required"},
		{"null state", `{"model": "m", "state": null, "questions": {"q": {"type": "noul"}}}`, "state is required"},
		{"empty questions", `{"model": "m", "state": "s", "questions": {}}`, "must not be empty"},
		{"choice criteria missing", `{"model": "m", "state": "s", "questions": {"q": {"type": "choice", "instructions": "i"}}}`, "criteria is required"},
		{"score criteria wrong shape", `{"model": "m", "state": "s", "questions": {"q": {"type": "score", "criteria": "x"}}}`, "must be an array"},
		{"noul criteria wrong shape", `{"model": "m", "state": "s", "questions": {"q": {"type": "noul", "criteria": 3}}}`, "must be an object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := DecodeRequestJSON([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestEncodeRequestErrors(t *testing.T) {
	base := ir.Request{
		Model:  "jev-latest",
		System: []ir.SystemBlock{{Text: "state text"}},
	}
	if _, _, err := EncodeRequest(&base); err == nil || !strings.Contains(err.Error(), "json_schema response format") {
		t.Fatalf("missing response format: err = %v", err)
	}
	rf := base
	rf.System = nil
	rf.Params.ResponseFormat = &ir.ResponseFormat{
		Type:   ir.ResponseFormatJSONSchema,
		Name:   ResponseFormatName,
		Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"number","minimum":0,"maximum":1}},"required":["q"]}`),
	}
	if _, _, err := EncodeRequest(&rf); err == nil || !strings.Contains(err.Error(), "system text") {
		t.Fatalf("missing system: err = %v", err)
	}
}

func TestDecodeResponseErrors(t *testing.T) {
	if _, _, err := DecodeResponseJSON([]byte(`{"model": "m", "answers": {}, "usage": {"input_tokens": 0, "output_tokens": 0}}`)); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("empty answers: err = %v", err)
	}
	if _, _, err := DecodeResponseJSON([]byte(`{"model": "m", "answers": {"a": "scalar"}, "usage": {"input_tokens": 0, "output_tokens": 0}}`)); err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Fatalf("scalar answer: err = %v", err)
	}
}

// TestJevRoundTrip pins the full client-side loop: a Jev request decodes to
// the IR, re-encodes back to Jev, the simulated model output (the answers
// text) decodes to an IR response, and that response re-encodes to typed
// answers with the original question keys.
func TestJevRoundTrip(t *testing.T) {
	requestRaw := []byte(`{
		"model": "jev-latest",
		"state": "The customer said the delivery was late.",
		"questions": {
			"sentiment": {
				"type": "choice",
				"instructions": "What is the overall sentiment?",
				"criteria": {"positive": "happy", "negative": "unhappy", "mixed": "both"}
			},
			"is_urgent": {"type": "noul", "instructions": "Does this convey urgency?"}
		}
	}`)
	req, _, err := DecodeRequestJSON(requestRaw)
	if err != nil {
		t.Fatalf("DecodeRequestJSON: %v", err)
	}
	wireReq, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	if wireReq.Model != "jev-latest" || string(wireReq.State) != `"The customer said the delivery was late."` {
		t.Fatalf("reconstructed request = %s / state %s", wireReq.Model, wireReq.State)
	}
	if _, ok := wireReq.Questions["is_urgent"]; !ok {
		t.Fatalf("is_urgent question missing after round trip")
	}
	if len(losses) != 1 {
		t.Fatalf("expected exactly the sentiment N-JEV-9 criteria loss (noul has no criteria), got %+v", losses)
	}
	for _, l := range losses {
		if l.Reason != ir.LossDegraded {
			t.Errorf("round-trip loss = %+v, want degraded N-JEV-9", l)
		}
	}

	// Simulated model output: the JSON the instruction template asks for.
	answersJSON := `{"is_urgent": {"type": "noul", "noul": 0}, "sentiment": {"type": "choice", "choice": "mixed"}}`
	resp := &ir.Response{
		Model:      "jev-latest",
		Content:    []ir.Block{ir.TextBlock{Text: answersJSON}},
		StopReason: ir.StopEndTurn,
		Usage:      ir.Usage{InputTokens: 10, OutputTokens: 5},
	}
	wireResp, respLosses, err := EncodeResponse(resp, WithQuestions(wireReq.Questions))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if len(respLosses) != 0 {
		t.Fatalf("expected no losses for well-typed answers, got %+v", respLosses)
	}
	var sentiment struct {
		Type   string `json:"type"`
		Choice string `json:"choice"`
	}
	if err := json.Unmarshal(wireResp.Answers["sentiment"], &sentiment); err != nil {
		t.Fatalf("sentiment answer: %v", err)
	}
	if sentiment.Type != "choice" || sentiment.Choice != "mixed" {
		t.Errorf("sentiment = %+v", sentiment)
	}
	var urgent struct {
		Type string  `json:"type"`
		Noul float64 `json:"noul"`
	}
	if err := json.Unmarshal(wireResp.Answers["is_urgent"], &urgent); err != nil {
		t.Fatalf("urgent answer: %v", err)
	}
	if urgent.Type != "noul" || urgent.Noul != 0 {
		t.Errorf("urgent = %+v", urgent)
	}
}
