package ir

import "testing"

func TestRequestVerbosityRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{`"future"`, `null`, `42`} {
		raw := []byte(`{"specVersion":"0.2.0","model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"params":{"verbosity":` + value + `}}`)
		if _, err := UnmarshalRequest(raw); err == nil {
			t.Errorf("verbosity %s: expected error", value)
		}
	}
	req := &Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: []Block{TextBlock{Text: "hi"}}}}, Params: Params{Verbosity: "future"}}
	if _, err := MarshalRequest(req); err == nil {
		t.Error("invalid in-memory verbosity should be rejected")
	}
}
