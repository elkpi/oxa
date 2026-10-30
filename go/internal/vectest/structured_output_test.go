package vectest

import (
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
	"github.com/elkpi/oxa/go/v2/openai/responses"
)

func TestCrossFaceSchemaRawBytes(t *testing.T) {
	schema := `{ "type" : "object", "maximum" : 1e400 }`
	cases := []struct {
		name   string
		raw    string
		decode func([]byte) (*ir.Request, []ir.Loss, error)
	}{
		{"chat", `{"model":"m","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":` + schema + `}}}`, func(raw []byte) (*ir.Request, []ir.Loss, error) { return chatcompletions.DecodeRequestJSON(raw) }},
		{"responses", `{"model":"m","max_output_tokens":64,"input":"hi","text":{"format":{"type":"json_schema","name":"result","schema":` + schema + `}}}`, func(raw []byte) (*ir.Request, []ir.Loss, error) { return responses.DecodeRequestJSON(raw) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _, err := tc.decode([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if string(req.Params.ResponseFormat.Schema) != schema {
				t.Fatalf("schema changed: %s", req.Params.ResponseFormat.Schema)
			}
			encoded, losses, err := messages.EncodeRequestJSON(req)
			if err != nil || len(losses) != 1 || !strings.Contains(string(encoded), schema) {
				t.Fatalf("encode: %s %v %+v", encoded, err, losses)
			}
		})
	}
}
