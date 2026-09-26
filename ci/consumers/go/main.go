package main

import (
	"fmt"
	"os"

	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
)

func stringPtr(value string) *string { return &value }

func main() {
	req := &chatcompletions.Request{
		Model: "gpt-4o-mini",
		Messages: []chatcompletions.Message{
			{Role: chatcompletions.RoleUser, Content: "hello"},
		},
	}
	decoded, _, err := chatcompletions.DecodeRequest(req)
	if err != nil || len(decoded.Messages) != 1 {
		fmt.Fprintf(os.Stderr, "request conversion failed: %v\n", err)
		os.Exit(1)
	}

	decoder := chatcompletions.NewStreamDecoder()
	var events []ir.Event
	feed := func(chunk *chatcompletions.Chunk) {
		more, err := decoder.Feed(chunk)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stream conversion failed: %v\n", err)
			os.Exit(1)
		}
		events = append(events, more...)
	}

	feed(&chatcompletions.Chunk{
		ID:    "stream-1",
		Model: "gpt-4o-mini",
		Choices: []chatcompletions.ChoiceDelta{{
			Index: 0,
			Delta: chatcompletions.DeltaPayload{Role: chatcompletions.RoleAssistant},
		}},
	})
	firstID := "call-1"
	firstType := chatcompletions.ToolTypeFunction
	firstName := "lookup"
	firstArguments := "{\"q\""
	feed(&chatcompletions.Chunk{
		ID:    "stream-1",
		Model: "gpt-4o-mini",
		Choices: []chatcompletions.ChoiceDelta{{
			Index: 0,
			Delta: chatcompletions.DeltaPayload{ToolCalls: []chatcompletions.ToolCallDelta{{
				Index: 0,
				ID:    &firstID,
				Type:  &firstType,
				Function: &chatcompletions.FunctionDelta{
					Name:      &firstName,
					Arguments: &firstArguments,
				},
			}}},
		}},
	})
	secondArguments := ":1}"
	feed(&chatcompletions.Chunk{
		ID:    "stream-1",
		Model: "gpt-4o-mini",
		Choices: []chatcompletions.ChoiceDelta{{
			Index: 0,
			Delta: chatcompletions.DeltaPayload{ToolCalls: []chatcompletions.ToolCallDelta{{
				Index:    0,
				Function: &chatcompletions.FunctionDelta{Arguments: &secondArguments},
			}}},
		}},
	})
	finish := chatcompletions.FinishReasonToolCalls
	feed(&chatcompletions.Chunk{
		ID:    "stream-1",
		Model: "gpt-4o-mini",
		Choices: []chatcompletions.ChoiceDelta{{
			Index:        0,
			Delta:        chatcompletions.DeltaPayload{},
			FinishReason: &finish,
		}},
	})
	terminal, err := decoder.Flush()
	if err != nil {
		fmt.Fprintf(os.Stderr, "stream flush failed: %v\n", err)
		os.Exit(1)
	}
	events = append(events, terminal...)
	if len(events) == 0 {
		fmt.Fprintln(os.Stderr, "stream conversion returned no events")
		os.Exit(1)
	}
	fmt.Println("go consumer: OK")
}
