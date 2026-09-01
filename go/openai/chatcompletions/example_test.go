package chatcompletions_test

import (
	"encoding/json"
	"fmt"
	"log"

	messages "github.com/elkpi/oxa/go/anthropic/messages"
	"github.com/elkpi/oxa/go/openai/chatcompletions"
)

// This example converts a Chat Completions request into an Anthropic
// Messages request by routing through the face-neutral intermediate
// representation. The same two-step pattern composes every pair of
// supported protocols; anything the target dialect cannot express is
// reported in the returned losses instead of failing the conversion.
func Example() {
	ccReq := &chatcompletions.Request{
		Model:     "gpt-4o-mini",
		MaxTokens: int64p(512),
		Messages: []chatcompletions.Message{
			{Role: "user", Content: "Translate 'good morning' to French."},
		},
	}

	irReq, losses, err := chatcompletions.DecodeRequest(ccReq)
	if err != nil {
		log.Fatal(err)
	}

	anReq, encodeLosses, err := messages.EncodeRequest(irReq)
	if err != nil {
		log.Fatal(err)
	}
	losses = append(losses, encodeLosses...)

	out, _ := json.Marshal(anReq)
	fmt.Printf("%s\nlosses: %d\n", out, len(losses))
	// Output:
	// {"model":"gpt-4o-mini","messages":[{"role":"user","content":"Translate 'good morning' to French."}],"max_tokens":512}
	// losses: 0
}

func int64p(v int64) *int64 { return &v }
