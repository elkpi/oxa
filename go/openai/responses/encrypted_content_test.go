package responses

import (
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
)

func TestEncryptedContentRoundTrip(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1","object":"response","status":"completed","model":"gpt-5",
		"output":[{
			"type":"reasoning","id":"rs_1",
			"summary":[{"type":"summary_text","text":"hidden"}],
			"encrypted_content":"cipher-text"
		},{
			"type":"message","id":"msg_1","role":"assistant","status":"completed",
			"content":[{"type":"output_text","text":"done","annotations":[]}]
		}],
		"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}
	}`)
	resp, losses, err := DecodeResponseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 0 {
		t.Fatalf("losses = %+v", losses)
	}
	if len(resp.Content) != 3 {
		t.Fatalf("content = %#v", resp.Content)
	}
	cipher, ok := resp.Content[1].(ir.EncryptedContentBlock)
	if !ok || cipher.Data != "cipher-text" {
		t.Fatalf("cipher block = %#v", resp.Content[1])
	}
	wire, encLosses, err := EncodeResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(encLosses) != 0 {
		t.Fatalf("encode losses = %+v", encLosses)
	}
	if wire.Output[0].EncryptedContent != "cipher-text" {
		t.Fatalf("wire cipher = %q", wire.Output[0].EncryptedContent)
	}
	for _, loss := range encLosses {
		if strings.Contains(loss.Detail, "cipher-text") {
			t.Fatalf("ciphertext leaked into loss: %+v", loss)
		}
	}

	_, ccLosses, err := chatcompletions.EncodeResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, loss := range ccLosses {
		if loss.Detail == ir.EncryptedContentLossDetail {
			found = true
		}
		if strings.Contains(loss.Detail, "cipher-text") {
			t.Fatalf("ciphertext leaked into chat loss: %+v", loss)
		}
	}
	if !found {
		t.Fatalf("chat losses = %+v", ccLosses)
	}
}

func TestEncryptedContentOnlyReasoning(t *testing.T) {
	raw := []byte(`{
		"id":"resp_2","object":"response","status":"completed","model":"gpt-5",
		"output":[{"type":"reasoning","id":"rs_2","encrypted_content":"only-cipher"}],
		"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}
	}`)
	resp, losses, err := DecodeResponseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 0 {
		t.Fatalf("losses = %+v, want none", losses)
	}
	if _, ok := resp.Content[0].(ir.EncryptedContentBlock); !ok {
		t.Fatalf("content = %#v", resp.Content)
	}
}

func TestMultiReasoningItemsRoundTrip(t *testing.T) {
	raw := []byte(`{
		"id":"resp_3","object":"response","status":"completed","model":"gpt-5",
		"output":[
			{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"one"}],"encrypted_content":"TEST_ONE"},
			{"type":"reasoning","id":"rs_2","summary":[{"type":"summary_text","text":"two"}],"encrypted_content":"TEST_TWO"}
		],
		"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}
	}`)
	resp, losses, err := DecodeResponseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 0 {
		t.Fatalf("losses: %+v", losses)
	}
	encoded, encLosses, err := EncodeResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(encLosses) != 0 {
		t.Fatalf("encLosses: %+v", encLosses)
	}
	if len(encoded.Output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(encoded.Output))
	}
	if encoded.Output[0].EncryptedContent != "TEST_ONE" || len(encoded.Output[0].Summary) != 1 || encoded.Output[0].Summary[0].Text != "one" {
		t.Fatalf("output[0] = %+v", encoded.Output[0])
	}
	if encoded.Output[1].EncryptedContent != "TEST_TWO" || len(encoded.Output[1].Summary) != 1 || encoded.Output[1].Summary[0].Text != "two" {
		t.Fatalf("output[1] = %+v", encoded.Output[1])
	}
}
