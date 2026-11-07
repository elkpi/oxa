package jev

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elkpi/oxa-protocol/go/v3/ir"
)

func TestDecodeRequestJSONLossSourceOrder(t *testing.T) {
	// Unknown fields interleave with semantic losses in source encounter
	// order (spec/02 §4): z_ext, state degraded, a_ext, assets.
	raw := `{"z_ext": 1, "model": "m", "state": {}, "a_ext": 2, "questions": {"q": {"type": "noul"}}, "assets": {}}`
	_, losses, err := DecodeRequestJSON([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeRequestJSON: %v", err)
	}
	want := []struct{ path, field string }{
		{"z_ext", "z_ext"}, {"state", "state"}, {"a_ext", "a_ext"}, {"assets", "assets"},
	}
	if len(losses) != len(want) {
		t.Fatalf("losses = %+v, want %d entries", losses, len(want))
	}
	for i, w := range want {
		if losses[i].Path != w.path || losses[i].Field != w.field {
			t.Errorf("loss[%d] = %s/%s, want %s/%s", i, losses[i].Path, losses[i].Field, w.path, w.field)
		}
	}
}

func TestDecodeResponseJSONRemovesUnknownAnswerFields(t *testing.T) {
	raw := `{"model": "m", "answers": {"q": {"type": "noul", "noul": 1, "secret_ext": "x"}}, "usage": {"input_tokens": 1, "output_tokens": 2}}`
	resp, losses, err := DecodeResponseJSON([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeResponseJSON: %v", err)
	}
	if strings.Contains(resp.Content[0].(ir.TextBlock).Text, "secret_ext") {
		t.Errorf("unknown answer field passed through: %s", resp.Content[0].(ir.TextBlock).Text)
	}
	if len(losses) != 1 || losses[0].Path != "answers.q" || losses[0].Field != "secret_ext" || losses[0].Reason != ir.LossUnmappedField {
		t.Errorf("losses = %+v, want one unmapped-field for secret_ext", losses)
	}
}

func TestDecodeResponseJSONRejectsNonObjectKnownFields(t *testing.T) {
	if _, _, err := DecodeResponseJSON([]byte(`{"model": "m", "answers": {"q": null}, "usage": {}}`)); err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Fatalf("null answer: err = %v", err)
	}
	if _, _, err := DecodeResponseJSON([]byte(`{"model": "m", "answers": {"q": {}}, "usage": null}`)); err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Fatalf("null usage: err = %v", err)
	}
}

func TestDecodeRequestRejectsScalarState(t *testing.T) {
	for _, raw := range []string{
		`{"model": "m", "state": true, "questions": {"q": {"type": "noul"}}}`,
		`{"model": "m", "state": 12, "questions": {"q": {"type": "noul"}}}`,
	} {
		if _, _, err := DecodeRequestJSON([]byte(raw)); err == nil || !strings.Contains(err.Error(), "must be a string, object, or array") {
			t.Fatalf("state %s: err = %v", raw, err)
		}
	}
}

func TestEncodeResponseScalarAnswersUseContext(t *testing.T) {
	resp := &ir.Response{
		Model:      "m",
		Content:    []ir.Block{ir.TextBlock{Text: `{"urgent": 0.95, "mood": "surprise", "level": 1.5}`}},
		StopReason: ir.StopEndTurn,
	}
	qs := map[string]Question{
		"urgent": {Type: QuestionTypeNoul},
		"mood":   {Type: QuestionTypeChoice, Criteria: json.RawMessage(`{"good":"g","bad":"b"}`)},
		"level":  {Type: QuestionTypeScore, Criteria: json.RawMessage(`["low","high"]`)},
	}
	wire, losses, err := EncodeResponse(resp, WithQuestions(qs))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if got := string(wire.Answers["urgent"]); got != `{"noul":0.95,"type":"noul"}` {
		t.Errorf("urgent = %s, want context-typed noul", got)
	}
	if got := string(wire.Answers["mood"]); got != `{"choice":"surprise","type":"choice"}` {
		t.Errorf("mood = %s, want context-typed choice", got)
	}
	if got := string(wire.Answers["level"]); got != `{"score":1.5,"type":"score"}` {
		t.Errorf("level = %s, want context-typed score", got)
	}
	// mood outside criteria and fractional level outside the discrete
	// levels: two N-JEV-14 losses; urgent is in range and silent.
	var degraded []string
	for _, l := range losses {
		if l.Reason == ir.LossDegraded {
			degraded = append(degraded, l.Path)
		}
	}
	if len(degraded) != 2 {
		t.Errorf("expected exactly 2 N-JEV-14 losses, got %+v (%+v)", degraded, losses)
	}
}

func TestEncodeResponseRequiresAllContextAnswers(t *testing.T) {
	qs := map[string]Question{
		"a":   {Type: QuestionTypeNoul},
		"b":   {Type: QuestionTypeChoice},
		"box": {Type: QuestionTypeBox},
	}
	resp := &ir.Response{
		Model:      "m",
		Content:    []ir.Block{ir.TextBlock{Text: `{"a": true}`}},
		StopReason: ir.StopEndTurn,
	}
	if _, _, err := EncodeResponse(resp, WithQuestions(qs)); err == nil || !strings.Contains(err.Error(), "answer b is missing") {
		t.Fatalf("missing answer: err = %v", err)
	}
	for _, text := range []string{`null`, `{}`} {
		r := &ir.Response{Model: "m", Content: []ir.Block{ir.TextBlock{Text: text}}, StopReason: ir.StopEndTurn}
		if _, _, err := EncodeResponse(r, WithQuestions(qs)); err == nil {
			t.Fatalf("answers %s accepted", text)
		}
	}
}

func TestEncodeResponseReportsAnswersBeyondQuestions(t *testing.T) {
	qs := map[string]Question{"a": {Type: QuestionTypeNoul}}
	resp := &ir.Response{
		Model:      "m",
		Content:    []ir.Block{ir.TextBlock{Text: `{"a": true, "extra": 0.5}`}},
		StopReason: ir.StopEndTurn,
	}
	wire, losses, err := EncodeResponse(resp, WithQuestions(qs))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if _, ok := wire.Answers["extra"]; !ok {
		t.Errorf("extra answer dropped")
	}
	var found bool
	for _, l := range losses {
		if l.Path == "answers" && l.Field == "extra" && l.Reason == ir.LossUnmappedField {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unmapped-field loss for extra answer, got %+v", losses)
	}
}

func TestEncodeRequestDropsNonDerivedUserContent(t *testing.T) {
	req := &ir.Request{
		Model:  "m",
		System: []ir.SystemBlock{{Text: "state text\n\n" + instructionTemplate}},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "question q (noul): "}}},
			{Role: ir.RoleUser, Content: []ir.Block{ir.TextBlock{Text: "UNIQUE CUSTOMER INPUT"}}},
		},
		Params: ir.Params{ResponseFormat: &ir.ResponseFormat{
			Type:   ir.ResponseFormatJSONSchema,
			Name:   ResponseFormatName,
			Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"number","minimum":0,"maximum":1}},"required":["q"]}`),
		}},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	if strings.Contains(string(wire.State), "CUSTOMER INPUT") || strings.Contains(string(wire.State), "question q") {
		t.Errorf("state = %s, want template-stripped system text only", wire.State)
	}
	if len(losses) != 1 || losses[0].Path != "messages[1]" || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Errorf("losses = %+v, want one N-JEV-10 loss for messages[1]", losses)
	}
}

func TestEncodeRequestSkipsSchemaPropertiesWithExtraConstraints(t *testing.T) {
	req := &ir.Request{
		Model:  "m",
		System: []ir.SystemBlock{{Text: "state text\n\n" + instructionTemplate}},
		Params: ir.Params{ResponseFormat: &ir.ResponseFormat{
			Type: ir.ResponseFormatJSONSchema,
			Name: ResponseFormatName,
			Schema: json.RawMessage(`{"type":"object","properties":{
				"ok":{"type":"number","minimum":0,"maximum":1},
				"weird":{"type":"number","minimum":0,"maximum":1,"multipleOf":0.5}},"required":["ok"]}`),
		}},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	if _, ok := wire.Questions["weird"]; ok {
		t.Errorf("weird property reconstructed despite extra constraint")
	}
	if len(losses) != 1 || losses[0].Path != "questions.weird" || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Errorf("losses = %+v, want one N-JEV-6 loss for questions.weird", losses)
	}
}

func TestEncodeRequestRejectsContradictingBoundsAlongsideEnum(t *testing.T) {
	req := &ir.Request{
		Model:  "m",
		System: []ir.SystemBlock{{Text: "state text\n\n" + instructionTemplate}},
		Params: ir.Params{ResponseFormat: &ir.ResponseFormat{
			Type: ir.ResponseFormatJSONSchema,
			Name: ResponseFormatName,
			Schema: json.RawMessage(`{"type":"object","properties":{
				"ok":{"type":"number","minimum":0,"maximum":1},
				"weird":{"type":"number","minimum":9,"maximum":9,"enum":[1,2]}},"required":["ok"]}`),
		}},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	if _, ok := wire.Questions["weird"]; ok {
		t.Errorf("weird property reconstructed despite contradicting bounds")
	}
	if len(losses) != 1 || losses[0].Path != "questions.weird" || losses[0].Reason != ir.LossUnsupportedSemantic {
		t.Errorf("losses = %+v, want one N-JEV-6 loss for questions.weird", losses)
	}
}

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

func TestDecodeRequestWholeUnitSkipSuppressesFieldAudit(t *testing.T) {
	raw := `{"model":"m","state":"s","questions":{"box":{"type":"box","bogus":1},"ok":{"type":"noul"}}}`
	_, losses, err := DecodeRequestJSON([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeRequestJSON: %v", err)
	}
	var boxLosses []ir.Loss
	for _, l := range losses {
		if l.Path == "questions.box" {
			boxLosses = append(boxLosses, l)
		}
	}
	if len(boxLosses) != 1 || boxLosses[0].Field != "type" {
		t.Errorf("box losses = %+v, want exactly the whole-unit N-JEV-5 loss", boxLosses)
	}
}

func TestDecodeRequestMixedUserContentIsNeverDerived(t *testing.T) {
	req := &ir.Request{
		Model:  "jev-latest",
		System: []ir.SystemBlock{{Text: "state text\n\n" + instructionTemplate}},
		Messages: []ir.Message{{
			Role: ir.RoleUser,
			Content: []ir.Block{
				ir.TextBlock{Text: "question q (noul): "},
				ir.ImageBlock{URL: "https://example.com/i.png"},
			},
		}},
		Params: ir.Params{ResponseFormat: &ir.ResponseFormat{
			Type:   ir.ResponseFormatJSONSchema,
			Name:   ResponseFormatName,
			Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"number","minimum":0,"maximum":1}},"required":["q"]}`),
		}},
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}
	if strings.Contains(string(wire.State), "question q") {
		t.Errorf("mixed-content message was treated as derived: state = %s", wire.State)
	}
	if len(losses) != 1 || losses[0].Path != "messages[0]" {
		t.Errorf("losses = %+v, want one N-JEV-10 loss for messages[0]", losses)
	}
}

func TestEncodeResponseExactDecimalRangeValidation(t *testing.T) {
	// 1.0000000000000001 collapses to 1.0 as a float64 but is greater than 1
	// in decimal terms; the exact comparison must report the loss.
	resp := &ir.Response{
		Model:      "m",
		Content:    []ir.Block{ir.TextBlock{Text: `{"q": 1.0000000000000001}`}},
		StopReason: ir.StopEndTurn,
	}
	_, losses, err := EncodeResponse(resp, WithQuestions(map[string]Question{
		"q": {Type: QuestionTypeNoul},
	}))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	var degraded []ir.Loss
	for _, l := range losses {
		if l.Reason == ir.LossDegraded {
			degraded = append(degraded, l)
		}
	}
	if len(degraded) != 1 || degraded[0].Field != "noul" {
		t.Errorf("losses = %+v, want exactly one N-JEV-14 noul loss", losses)
	}

	scored := &ir.Response{
		Model:      "m",
		Content:    []ir.Block{ir.TextBlock{Text: `{"s": 1.0000000000000001}`}},
		StopReason: ir.StopEndTurn,
	}
	scoreWire, scoreLosses, err := EncodeResponse(scored, WithQuestions(map[string]Question{
		"s": {Type: QuestionTypeScore, Criteria: json.RawMessage(`["low","high"]`)},
	}))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	_ = scoreWire
	var scoreDegraded []ir.Loss
	for _, l := range scoreLosses {
		if l.Reason == ir.LossDegraded {
			scoreDegraded = append(scoreDegraded, l)
		}
	}
	if len(scoreDegraded) != 1 || scoreDegraded[0].Field != "score" {
		t.Errorf("score losses = %+v, want exactly one N-JEV-14 score loss", scoreLosses)
	}
}

func TestEncodeResponseTypedAnswersCarryVerbatim(t *testing.T) {
	resp := &ir.Response{
		Model:      "m",
		Content:    []ir.Block{ir.TextBlock{Text: `{"q":{"type":"choice","choice":"yes","probabilities":{"z":0.2,"a":0.8}}}`}},
		StopReason: ir.StopEndTurn,
	}
	wire, losses, err := EncodeResponse(resp)
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if len(losses) != 0 {
		t.Errorf("losses = %+v, want none for a typed carry", losses)
	}
	if !strings.Contains(string(wire.Answers["q"]), `"probabilities":{"z":0.2,"a":0.8}`) {
		t.Errorf("nested probabilities were reordered: %s", wire.Answers["q"])
	}
}

func TestDecodeRequestErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"missing model", `{"state": "s", "questions": {"q": {"type": "noul"}}}`, "model is required"},
		{"empty model", `{"model": "", "state": "s", "questions": {"q": {"type": "noul"}}}`, "model is required"},
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
