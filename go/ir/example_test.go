package ir_test

import (
	"fmt"

	"github.com/elkpi/oxa/go/ir"
)

func ExampleRequest() {
	req := &ir.Request{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []ir.Message{
			{
				Role:    ir.RoleUser,
				Content: []ir.Block{ir.TextBlock{Text: "Hello!"}},
			},
		},
	}

	data, err := ir.MarshalRequest(req)
	if err != nil {
		panic(err)
	}

	decoded, err := ir.UnmarshalRequest(data)
	if err != nil {
		panic(err)
	}

	fmt.Println(decoded.Model)
	fmt.Println(len(decoded.Messages))
	// Output:
	// claude-3-5-sonnet-20241022
	// 1
}
