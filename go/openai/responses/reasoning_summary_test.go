package responses

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
)

func TestDecodeReasoningSummaryTypedAndJSON(t *testing.T) {
	for _, test := range []struct {
		name   string
		parts  string
		want   []string
		field  string
		reason ir.LossReason
	}{
		{name: "standard", parts: `{"type":"summary_text","text":"T"}`, want: []string{"T"}},
		{name: "mixed", parts: `{"type":"output_text","text":"A"},{"type":"summary_text","text":"B"}`, want: []string{"A", "B"}},
		{name: "unknown", parts: `{"type":"future_summary","text":"T","annotations":[{}]}`, field: "type", reason: ir.LossUnsupportedSemantic},
		{name: "missing_type", parts: `{"text":"T"}`, field: "type", reason: ir.LossUnsupportedSemantic},
		{name: "annotations", parts: `{"type":"summary_text","text":"T","annotations":[{}]}`, want: []string{"T"}, field: "annotations", reason: ir.LossUnmappedField},
		{name: "empty_annotations", parts: `{"type":"summary_text","text":"T","annotations":[]}`, want: []string{"T"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			check := func(t *testing.T, blocks []ir.Block, losses []ir.Loss, path string) {
				t.Helper()
				var thinking []string
				for _, block := range blocks {
					if block, ok := block.(ir.ThinkingBlock); ok {
						thinking = append(thinking, block.Thinking)
					}
				}
				if !reflect.DeepEqual(thinking, test.want) {
					t.Fatalf("thinking = %#v, want %#v", thinking, test.want)
				}
				if test.field == "" {
					if len(losses) != 0 {
						t.Fatalf("losses = %#v, want none", losses)
					}
					return
				}
				if test.field == "annotations" {
					path += ".annotations"
				}
				if len(losses) != 1 || losses[0].Path != path || losses[0].Field != test.field || losses[0].Reason != test.reason {
					t.Fatalf("losses = %#v, want one %s/%s/%s", losses, path, test.field, test.reason)
				}
			}
			t.Run("request", func(t *testing.T) {
				raw := []byte(`{"model":"m","input":[{"type":"reasoning","summary":[` + test.parts + `]},{"role":"user","content":"u"}]}`)
				var wire Request
				if err := json.Unmarshal(raw, &wire); err != nil {
					t.Fatal(err)
				}
				typed, typedLosses, err := DecodeRequest(&wire)
				if err != nil {
					t.Fatal(err)
				}
				decoded, losses, err := DecodeRequestJSON(raw)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(decoded, typed) || !slices.Equal(losses, typedLosses) {
					t.Fatalf("JSON and typed paths differ: JSON=%#v/%#v typed=%#v/%#v", decoded, losses, typed, typedLosses)
				}
				var blocks []ir.Block
				for _, message := range decoded.Messages {
					blocks = append(blocks, message.Content...)
				}
				check(t, blocks, losses, "input[0].summary[0]")
			})
			t.Run("response", func(t *testing.T) {
				raw := []byte(`{"id":"resp_summary","object":"response","model":"m","status":"completed","output":[{"type":"reasoning","summary":[` + test.parts + `]}]}`)
				var wire Response
				if err := json.Unmarshal(raw, &wire); err != nil {
					t.Fatal(err)
				}
				typed, typedLosses, err := DecodeResponse(&wire)
				if err != nil {
					t.Fatal(err)
				}
				decoded, losses, err := DecodeResponseJSON(raw)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(decoded, typed) || !slices.Equal(losses, typedLosses) {
					t.Fatalf("JSON and typed paths differ: JSON=%#v/%#v typed=%#v/%#v", decoded, losses, typed, typedLosses)
				}
				check(t, decoded.Content, losses, "output[0].summary[0]")
			})
		})
	}
}
