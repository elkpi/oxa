package ir

import (
	"bytes"
	"testing"
)

func TestToolStrictCanonicalRoundTrip(t *testing.T) {
	for _, field := range []string{"", `,"strict":false`, `,"strict":true`} {
		doc := `{"specVersion":"0.2.0","model":"m","messages":[],"tools":[{"name":"f","input_schema":{}` + field + `}]}`
		req, err := UnmarshalRequest([]byte(doc))
		if err != nil {
			t.Fatalf("unmarshal %s: %v", field, err)
		}
		encoded, err := MarshalRequest(req)
		if err != nil {
			t.Fatalf("marshal %s: %v", field, err)
		}
		if bytes.Contains(encoded, []byte(`"strict"`)) != (field != "") || (field != "" && !bytes.Contains(encoded, []byte(field[1:]))) {
			t.Fatalf("strict round trip %s: %s", field, encoded)
		}
	}
}

func TestNonFunctionToolStrictRejected(t *testing.T) {
	for _, tool := range []string{
		`{"name":"f","input_schema":{},"strict":null}`,
		`{"type":"custom","name":"f","strict":null}`,
		`{"type":"custom","name":"f","strict":false}`,
		`{"type":"apply_patch","strict":true}`,
		`{"type":"native","name":"f","source":"x","definition":"{\"type\":\"other\",\"name\":\"f\"}","strict":false}`,
	} {
		doc := `{"specVersion":"0.2.0","model":"m","messages":[],"tools":[` + tool + `]}`
		if _, err := UnmarshalRequest([]byte(doc)); err == nil {
			t.Fatalf("accepted non-function strict: %s", tool)
		}
	}
}
