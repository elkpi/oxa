package messages

import (
	"encoding/json"
	"testing"
)

func TestNativeToolOpaqueStrictConfiguration(t *testing.T) {
	definition := `{"type":"future_tool_20270101","name":"native","strict":{"mode":"provider-specific"}}`
	raw := []byte(`{"model":"m","max_tokens":32,"tools":[` + definition + `],"messages":[{"role":"user","content":"hi"}]}`)
	req, losses, err := DecodeRequestJSON(raw)
	if err != nil || len(losses) != 0 {
		t.Fatalf("decode: losses=%+v err=%v", losses, err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Definition != definition {
		t.Fatalf("native definition: %+v", req.Tools)
	}
	wire, losses, err := EncodeRequest(req)
	if err != nil || len(losses) != 0 {
		t.Fatalf("encode: losses=%+v err=%v", losses, err)
	}
	got, err := json.Marshal(wire.Tools[0])
	if err != nil || string(got) != definition {
		t.Fatalf("wire: %s err=%v", got, err)
	}
}
