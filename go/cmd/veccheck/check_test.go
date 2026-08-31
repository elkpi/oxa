package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/ir"
)

func TestValidateEventStream(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "missing message_start",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[0]",
		},
		{
			name: "content delta without a start",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}},
				{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[1]",
		},
		{
			name: "second open block before first stop",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"text","text":""}},
				{"type":"content_block_start","index":1,"block":{"type":"text","text":""}},
				{"type":"content_block_stop","index":0},
				{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[2]",
		},
		{
			name: "first IR block index is not zero",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":1,"block":{"type":"text","text":""}},
				{"type":"content_block_stop","index":1},
				{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[1].index",
		},
		{
			name: "text delta applied to tool use block",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"tool_use","id":"call","name":"tool","input":"{}"}},
				{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}},
				{"type":"content_block_stop","index":0},
				{"type":"message_delta","stop_reason":"tool_use","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[2].delta",
		},
		{
			name: "input JSON delta applied to text block",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"text","text":""}},
				{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}},
				{"type":"content_block_stop","index":0},
				{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[2].delta",
		},
		{
			name: "tool input does not equal exact fragment concatenation",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"tool_use","id":"call","name":"tool","input":"{}"}},
				{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"x\":1}"}},
				{"type":"content_block_stop","index":0},
				{"type":"message_delta","stop_reason":"tool_use","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[3].input",
		},
		{
			name: "tool input is not a JSON string token",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"tool_use","id":"call","name":"tool","input":{}}},
				{"type":"content_block_stop","index":0},
				{"type":"message_delta","stop_reason":"tool_use","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[1].block.input",
		},
		{
			name: "input JSON delta is not a JSON string token",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"tool_use","id":"call","name":"tool","input":"{}"}},
				{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":{}}},
				{"type":"content_block_stop","index":0},
				{"type":"message_delta","stop_reason":"tool_use","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[2].delta.partial_json",
		},
		{
			name: "content delta index differs from open block",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"text","text":""}},
				{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}},
				{"type":"content_block_stop","index":0},
				{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[2].index",
		},
		{
			name: "content stop index differs from open block",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"text","text":""}},
				{"type":"content_block_stop","index":1},
				{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
			want: "events[2].index",
		},
		{
			name: "message done without immediately preceding message delta",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"message_done"}
			]}`,
			want: "events[1]",
		},
		{
			name: "event after message done",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"},
				{"type":"message_done"}
			]}`,
			want: "events[3]",
		},
		{
			name: "accepts text and tool block with empty and incomplete raw fragments",
			doc: `{"specVersion":"0.1.0","events":[
				{"type":"message_start","id":"m","model":"model"},
				{"type":"content_block_start","index":0,"block":{"type":"text","text":""}},
				{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}},
				{"type":"content_block_stop","index":0},
				{"type":"content_block_start","index":1,"block":{"type":"tool_use","id":"call","name":"tool","input":"{\"x\":1"}},
				{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":""}},
				{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"x\":1"}},
				{"type":"content_block_stop","index":1},
				{"type":"message_delta","stop_reason":"tool_use","usage":{"input_tokens":0,"output_tokens":0}},
				{"type":"message_done"}
			]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			es, err := ir.UnmarshalEventStream([]byte(tt.doc))
			if err != nil {
				t.Fatalf("ir.UnmarshalEventStream() error = %v", err)
			}

			err = validateEventStream(es)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateEventStream() unexpected error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateEventStream() error = nil, want substring %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateEventStream() error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestCheckVectorFileValidatesSelectedStreamIR(t *testing.T) {
	s := testSchemas(t)
	invalidStream := `{"specVersion":"0.1.0","events":[
		{"type":"message_start","id":"m","model":"model"},
		{"type":"content_block_start","index":1,"block":{"type":"text","text":""}},
		{"type":"content_block_stop","index":1},
		{"type":"message_delta","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}},
		{"type":"message_done"}
	]}`

	for _, conversion := range []string{"to-ir", "from-ir"} {
		t.Run(conversion, func(t *testing.T) {
			vectorsDir := t.TempDir()
			relFile := conversion + ".json"
			raw := streamVectorJSON(conversion, conversion, invalidStream)
			if err := os.WriteFile(filepath.Join(vectorsDir, relFile), raw, 0o600); err != nil {
				t.Fatalf("write test vector: %v", err)
			}

			_, errs := checkVectorFile(s, vectorsDir, relFile, map[string]bool{})
			if len(errs) != 1 {
				t.Fatalf("checkVectorFile() returned %d errors, want 1: %v", len(errs), errs)
			}
			want := conversionField(conversion) + " violates stream invariants: events[1].index"
			if !strings.Contains(errs[0].Error(), want) {
				t.Fatalf("checkVectorFile() error = %q, want substring %q", errs[0], want)
			}
		})
	}
}

func TestCheckVectorFileReportsSchemaAndManualStreamErrors(t *testing.T) {
	s := testSchemas(t)
	vectorsDir := t.TempDir()
	invalidIR := `{"specVersion":"0.1.0","events":[
		{"type":"message_start","id":"m","model":"model"},
		{"type":"content_block_start","index":0,"block":{"type":"tool_use","id":"call","name":"tool","input":{}}},
		{"type":"content_block_stop","index":0},
		{"type":"message_delta","stop_reason":"tool_use","usage":{"input_tokens":0,"output_tokens":0}},
		{"type":"message_done"}
	]}`
	relFile := "invalid.json"
	if err := os.WriteFile(filepath.Join(vectorsDir, relFile), streamVectorJSON("to-ir", "invalid", invalidIR), 0o600); err != nil {
		t.Fatalf("write test vector: %v", err)
	}

	_, errs := checkVectorFile(s, vectorsDir, relFile, map[string]bool{})
	if len(errs) != 2 {
		t.Fatalf("checkVectorFile() returned %d errors, want schema and manual errors: %v", len(errs), errs)
	}
	joined := make([]string, 0, len(errs))
	for _, err := range errs {
		joined = append(joined, err.Error())
	}
	errors := strings.Join(joined, "\n")
	for _, want := range []string{
		"expected_ir does not validate against ir.schema.json",
		"expected_ir violates stream invariants: events[1].block.input",
	} {
		if !strings.Contains(errors, want) {
			t.Errorf("combined errors = %q, want substring %q", errors, want)
		}
	}
}

func testSchemas(t *testing.T) *schemas {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.."))
	s, err := loadSchemas(root)
	if err != nil {
		t.Fatalf("loadSchemas() error = %v", err)
	}
	return s
}

func conversionField(conversion string) string {
	if conversion == "to-ir" {
		return "expected_ir"
	}
	return "input"
}

func streamVectorJSON(conversion, name, irDoc string) []byte {
	if conversion == "to-ir" {
		return []byte(fmt.Sprintf(`{"name":%q,"description":"test","spec_version":"0.1.0","mode":"stream","conversion":"to-ir","source":{"protocol":"anthropic"},"input":{},"expected_ir":%s,"expected_losses":[],"tags":["stream"]}`, name, irDoc))
	}
	return []byte(fmt.Sprintf(`{"name":%q,"description":"test","spec_version":"0.1.0","mode":"stream","conversion":"from-ir","source":{"protocol":"anthropic"},"target":{"protocol":"anthropic"},"input":%s,"expected_output":{},"expected_losses":[],"tags":["stream"]}`, name, irDoc))
}
