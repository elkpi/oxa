package oxa_test

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/elkpi/oxa-protocol/go/v3"
	"github.com/elkpi/oxa-protocol/go/v3/anthropic/messages"
	"github.com/elkpi/oxa-protocol/go/v3/openai/chatcompletions"
)

func TestVersion(t *testing.T) {
	if oxa.Version != "3.1.0" {
		t.Errorf("oxa.Version = %q, want 3.1.0", oxa.Version)
	}
}

func TestReleaseVersionAgreement(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	major := strings.Split(oxa.Version, ".")[0]
	if !regexp.MustCompile(`(?m)^module github\.com/elkpi/oxa-protocol/go/v` + regexp.QuoteMeta(major) + `$`).Match(mod) {
		t.Fatalf("module path does not match exported major version %q", major)
	}
	tag := os.Getenv("OXA_RELEASE_TAG")
	if tag == "" {
		return
	}
	if tag != "v"+oxa.Version && tag != "go/v"+oxa.Version {
		t.Fatalf("release tag %q does not match exported Go version %q", tag, oxa.Version)
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
