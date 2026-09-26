package oxa_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/elkpi/oxa/go/v2"
	"github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
)

func TestVersion(t *testing.T) {
	if oxa.Version != "2.0.0" {
		t.Errorf("oxa.Version = %q, want 2.0.0", oxa.Version)
	}
}

// Example demonstrates end-to-end protocol conversion between two distinct
// provider faces via the face-neutral Intermediate Representation (IR):
// OpenAI Chat Completions -> oxa IR -> Anthropic Messages.
func Example() {
	// 1. Inbound OpenAI Chat Completions payload.
	wireJSON := []byte(`{
		"model": "gpt-4o",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Translate 'Hello' to French"}
		]
	}`)

	var openAIReq chatcompletions.Request
	if err := json.Unmarshal(wireJSON, &openAIReq); err != nil {
		panic(err)
	}

	// 2. Decode: OpenAI Chat Completions (Face) -> IR.
	irReq, decodeLosses, err := chatcompletions.DecodeRequest(&openAIReq)
	if err != nil {
		panic(err)
	}

	// 3. Encode: IR -> Anthropic Messages (Face).
	anthropicReq, encodeLosses, err := messages.EncodeRequest(irReq)
	if err != nil {
		panic(err)
	}

	fmt.Println("IR Model:", irReq.Model)
	fmt.Println("Anthropic Model:", anthropicReq.Model)
	fmt.Println("Anthropic MaxTokens:", anthropicReq.MaxTokens)
	fmt.Println("Total Losses:", len(decodeLosses)+len(encodeLosses))
	// Output:
	// IR Model: gpt-4o
	// Anthropic Model: gpt-4o
	// Anthropic MaxTokens: 1024
	// Total Losses: 0
}
