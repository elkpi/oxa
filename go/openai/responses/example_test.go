package responses_test

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/responses"
)

func ExampleDecodeRequest() {
	wireJSON := []byte(`{
		"model": "gpt-4o",
		"input": "Hello world"
	}`)

	var wire responses.Request
	if err := json.Unmarshal(wireJSON, &wire); err != nil {
		panic(err)
	}

	req, losses, err := responses.DecodeRequest(&wire)
	if err != nil {
		panic(err)
	}

	fmt.Println(req.Model)
	fmt.Println(len(req.Messages))
	fmt.Println(len(losses))
	// Output:
	// gpt-4o
	// 1
	// 0
}

func ExampleEncodeRequest() {
	req := &ir.Request{
		Model: "gpt-4o",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.Block{ir.TextBlock{Text: "Hello world"}},
			},
		},
	}

	wire, losses, err := responses.EncodeRequest(req)
	if err != nil {
		panic(err)
	}

	fmt.Println(wire.Model)
	fmt.Println(len(losses))
	// Output:
	// gpt-4o
	// 0
}
