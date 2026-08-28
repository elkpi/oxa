package ir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// findRepoRoot walks up from dir looking for a directory containing both
// vectors/ and .git/ (vectors/README.md "How implementations locate
// vectors"). Returns "" when none is found (module consumed as a dependency
// outside the monorepo).
func findRepoRoot(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "vectors")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func mustRequestDoc(t *testing.T, req *Request) []byte {
	t.Helper()
	doc, err := MarshalRequest(req)
	if err != nil {
		t.Fatalf("MarshalRequest: %v", err)
	}
	return doc
}

func mustResponseDoc(t *testing.T, resp *Response) []byte {
	t.Helper()
	doc, err := MarshalResponse(resp)
	if err != nil {
		t.Fatalf("MarshalResponse: %v", err)
	}
	return doc
}

func mustEventStreamDoc(t *testing.T, es *EventStream) []byte {
	t.Helper()
	doc, err := MarshalEventStream(es)
	if err != nil {
		t.Fatalf("MarshalEventStream: %v", err)
	}
	return doc
}

func ptr[T any](v T) *T { return &v }

func sampleRequest() *Request {
	return &Request{
		Model:  "claude-sonnet-4-5",
		System: []SystemBlock{{Text: "You are a concise assistant."}},
		Messages: []Message{
			{Role: RoleUser, Content: []Block{TextBlock{Text: "What is the weather in Paris?"}}},
			{Role: RoleAssistant, Content: []Block{
				TextBlock{Text: "Let me check."},
				ToolUseBlock{ID: "toolu_01", Name: "get_weather",
					Input: json.RawMessage(`"{\"city\":\"Paris\"}"`)},
			}},
			{Role: RoleUser, Content: []Block{
				ToolResultBlock{ToolUseID: "toolu_01",
					Content: []Block{TextBlock{Text: "18 C, clear"}}},
			}},
		},
		Tools: []Tool{{
			Name:        "get_weather",
			Description: "Current weather for a city",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		}},
		ToolChoice: &ToolChoice{Mode: "auto"},
		Params: Params{
			Temperature:   ptr(0.7),
			TopP:          ptr(0.9),
			MaxTokens:     ptr(int64(1024)),
			StopSequences: []string{"\n\n"},
		},
		Metadata: map[string]string{"k": "v"},
	}
}

func sampleResponse() *Response {
	return &Response{
		ID:         "msg_017Y2hvcv",
		Model:      "claude-sonnet-4-5",
		Content:    []Block{TextBlock{Text: "It is 18 C and clear in Paris."}},
		StopReason: StopEndTurn,
		Usage:      Usage{InputTokens: 120, OutputTokens: 12},
	}
}

func sampleEventStream() *EventStream {
	return &EventStream{Events: []Event{
		MessageStart{ID: "msg_017Y2hvcv", Model: "claude-sonnet-4-5"},
		ContentBlockStart{Index: 0, Block: TextBlock{Text: ""}},
		ContentBlockDelta{Index: 0, Delta: TextDelta{Text: "It is 18 C"}},
		ContentBlockDelta{Index: 0, Delta: TextDelta{Text: " and clear in Paris."}},
		ContentBlockStop{Index: 0},
		MessageDelta{StopReason: StopEndTurn, Usage: Usage{InputTokens: 120, OutputTokens: 12}},
		MessageDone{},
	}}
}

func TestRequestRoundTrip(t *testing.T) {
	doc := mustRequestDoc(t, sampleRequest())
	back, err := UnmarshalRequest(doc)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if again := mustRequestDoc(t, back); string(doc) != string(again) {
		t.Fatalf("request round-trip mismatch:\nfirst:  %s\nsecond: %s", doc, again)
	}
}

func TestRequestOmitsEmptyOptionals(t *testing.T) {
	doc := mustRequestDoc(t, &Request{
		Model:    "m",
		Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}},
	})
	for _, unwanted := range []string{"system", "tools", "tool_choice", "params", "metadata"} {
		if strings.Contains(string(doc), `"`+unwanted+`"`) {
			t.Fatalf("minimal request must omit %q: %s", unwanted, doc)
		}
	}
}

func TestResponseRoundTrip(t *testing.T) {
	doc := mustResponseDoc(t, sampleResponse())
	back, err := UnmarshalResponse(doc)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if again := mustResponseDoc(t, back); string(doc) != string(again) {
		t.Fatalf("response round-trip mismatch:\nfirst:  %s\nsecond: %s", doc, again)
	}
	// stop_sequence may appear only with stop_reason stop_sequence.
	if strings.Contains(string(doc), "stop_sequence") {
		t.Fatalf("stop_sequence must be absent unless matched: %s", doc)
	}
}

func TestResponseStopSequenceRoundTrip(t *testing.T) {
	doc := mustResponseDoc(t, &Response{
		ID: "m", Model: "m", StopReason: StopSequence,
		StopSequence: "END", Usage: Usage{InputTokens: 1, OutputTokens: 2},
	})
	back, err := UnmarshalResponse(doc)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.StopSequence != "END" || back.StopReason != StopSequence {
		t.Fatalf("stop_sequence lost: %+v", back)
	}
}

func TestEventStreamRoundTrip(t *testing.T) {
	doc := mustEventStreamDoc(t, sampleEventStream())
	back, err := UnmarshalEventStream(doc)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if again := mustEventStreamDoc(t, back); string(doc) != string(again) {
		t.Fatalf("event-stream round-trip mismatch:\nfirst:  %s\nsecond: %s", doc, again)
	}
}

// TestToolInputByteFidelity pins INV-1: the raw JSON text of tool_use.input
// and input_json_delta.partial_json is an opaque string token; the codec must
// carry the exact source bytes, never decoding and re-encoding them. The
// samples use A-style escapes, which Go's JSON string encoder normalizes
// back to the literal rune on re-encode; a codec that decoded and re-encoded
// the text would therefore emit different bytes and fail these byte-equality
// assertions.
func TestToolInputByteFidelity(t *testing.T) {
	const inputToken = `"{\"city\":\"\u0041BC\"}"` // decodes to {"city":"ABC"}
	const partialToken = `"{\"city\":\"P\u0041r"`  // decodes to {"city":"PAr

	reqDoc := `{"specVersion":"0.1.0","model":"m","messages":[{"role":"user","content":[` +
		`{"type":"tool_use","id":"t1","name":"n","input":` + inputToken + `}]}]}`
	req, err := UnmarshalRequest([]byte(reqDoc))
	if err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if got := string(req.Messages[0].Content[0].(ToolUseBlock).Input); got != inputToken {
		t.Fatalf("tool input token not byte-faithful:\nwant %s\ngot  %s", inputToken, got)
	}

	esDoc := `{"specVersion":"0.1.0","events":[` +
		`{"type":"message_start","id":"m1","model":"m"},` +
		`{"type":"content_block_start","index":0,"block":{"type":"tool_use","id":"t1","name":"n","input":""}},` +
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":` + partialToken + `}},` +
		`{"type":"content_block_stop","index":0},` +
		`{"type":"message_delta","stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}},` +
		`{"type":"message_done"}]}`
	es, err := UnmarshalEventStream([]byte(esDoc))
	if err != nil {
		t.Fatalf("unmarshal event stream: %v", err)
	}
	delta := string(es.Events[2].(ContentBlockDelta).Delta.(InputJSONDelta).PartialJSON)
	if delta != partialToken {
		t.Fatalf("partial_json token not byte-faithful:\nwant %s\ngot  %s", partialToken, delta)
	}

	// And a full codec round-trip preserves both tokens byte-for-byte.
	reqAgain, err := UnmarshalRequest(mustRequestDoc(t, req))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(reqAgain.Messages[0].Content[0].(ToolUseBlock).Input); got != inputToken {
		t.Fatalf("tool input token altered across full round-trip:\nwant %s\ngot  %s", inputToken, got)
	}
	esAgain, err := UnmarshalEventStream(mustEventStreamDoc(t, es))
	if err != nil {
		t.Fatal(err)
	}
	got := string(esAgain.Events[2].(ContentBlockDelta).Delta.(InputJSONDelta).PartialJSON)
	if got != partialToken {
		t.Fatalf("partial_json token altered across full round-trip:\nwant %s\ngot  %s", partialToken, got)
	}
}

func TestUnmarshalRejectsWrongSpecVersion(t *testing.T) {
	bad := `{"specVersion":"9.9.9","model":"m","messages":[{"role":"user","content":[{"type":"text","text":"x"}]}]}`
	if _, err := UnmarshalRequest([]byte(bad)); err == nil {
		t.Fatal("expected specVersion mismatch error")
	}
}

func TestUnmarshalRejectsUnknownBlockType(t *testing.T) {
	bad := `{"specVersion":"0.1.0","model":"m","messages":[{"role":"user","content":[{"type":"mystery","text":"x"}]}]}`
	if _, err := UnmarshalRequest([]byte(bad)); err == nil {
		t.Fatal("expected unknown block type error")
	}
}

// TestCanonicalJSONValidatesAgainstSchema marshals representative values of
// every document kind and validates them against spec/schema/ir.schema.json
// (INV-9). The test skips only when the repo root cannot be located (module
// consumed as a dependency); in-repo it must always run.
func TestCanonicalJSONValidatesAgainstSchema(t *testing.T) {
	root := findRepoRoot(".")
	if root == "" {
		t.Skip("repo root not found; schema validation skipped (dependency mode)")
	}
	f, err := os.Open(filepath.Join(root, "spec", "schema", "ir.schema.json"))
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	defer f.Close()
	schemaDoc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("ir.schema.json", schemaDoc); err != nil {
		t.Fatalf("add schema: %v", err)
	}
	schema, err := compiler.Compile("ir.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	docs := map[string][]byte{
		"request":  mustRequestDoc(t, sampleRequest()),
		"minimal":  mustRequestDoc(t, &Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: ""}}}}}),
		"response": mustResponseDoc(t, sampleResponse()),
		"eventStream": mustEventStreamDoc(t, &EventStream{Events: []Event{
			MessageStart{ID: "m1", Model: "m"},
			MessageDelta{StopReason: StopEndTurn, Usage: Usage{InputTokens: 1, OutputTokens: 1}},
			MessageDone{},
		}}),
	}
	for name, doc := range docs {
		inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(doc)))
		if err != nil {
			t.Fatalf("%s: parse instance: %v", name, err)
		}
		if err := schema.Validate(inst); err != nil {
			t.Errorf("%s document does not validate: %v\ndocument: %s", name, err, doc)
		}
	}
}
