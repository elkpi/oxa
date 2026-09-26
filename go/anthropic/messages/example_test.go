package messages_test

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/ir"
)

func ExampleDecodeRequest() {
	wireJSON := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello world"}
		]
	}`)

	var wire messages.Request
	if err := json.Unmarshal(wireJSON, &wire); err != nil {
		panic(err)
	}

	req, losses, err := messages.DecodeRequest(&wire)
	if err != nil {
		panic(err)
	}

	fmt.Println(req.Model)
	fmt.Println(len(req.Messages))
	fmt.Println(len(losses))
	// Output:
	// claude-3-5-sonnet-20241022
	// 1
	// 0
}

func ExampleEncodeRequest() {
	maxTokens := int64(1024)
	req := &ir.Request{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.Block{ir.TextBlock{Text: "Hello world"}},
			},
		},
		Params: ir.Params{
			MaxTokens: &maxTokens,
		},
	}

	wire, losses, err := messages.EncodeRequest(req)
	if err != nil {
		panic(err)
	}

	fmt.Println(wire.Model)
	fmt.Println(wire.MaxTokens)
	fmt.Println(len(losses))
	// Output:
	// claude-3-5-sonnet-20241022
	// 1024
	// 0
}
