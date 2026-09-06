package vectest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/anthropic/messages"
	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/openai/chatcompletions"
	"github.com/elkpi/oxa/go/openai/responses"
)

type fragmentCorpus struct {
	Version int                  `json:"version"`
	Cases   []fragmentCorpusCase `json:"cases"`
}

type fragmentCorpusCase struct {
	ID        string   `json:"id"`
	Protocol  string   `json:"protocol"`
	Argument  string   `json:"argument"`
	Fragments []string `json:"fragments"`
}

func TestFragmentCorpus(t *testing.T) {
	root := findFragmentCorpusRoot(t)
	var corpus fragmentCorpus
	data, err := os.ReadFile(filepath.Join(root, "testdata", "stream-fragment-corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Version != 1 {
		t.Fatalf("corpus version = %d, want 1", corpus.Version)
	}
	for _, tc := range corpus.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			if got := strings.Join(tc.Fragments, ""); got != tc.Argument {
				t.Fatalf("fragments concatenate to %q, want %q", got, tc.Argument)
			}
			var events []ir.Event
			switch tc.Protocol {
			case "chatcompletions":
				events = decodeChatFragmentCase(t, tc)
			case "responses":
				events = decodeResponsesFragmentCase(t, tc)
			case "anthropic":
				events = decodeAnthropicFragmentCase(t, tc)
			default:
				t.Fatalf("unsupported protocol %q", tc.Protocol)
			}
			validateFragmentEvents(t, events, tc)
		})
	}
}

func findFragmentCorpusRoot(t *testing.T) string {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := working; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "testdata", "stream-fragment-corpus.json")); err == nil {
				return dir
			}
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Skip("repository root with testdata/stream-fragment-corpus.json not found")
		}
	}
}

func marshalFragmentWire(t *testing.T, wire any) []byte {
	t.Helper()
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeChatFragmentCase(t *testing.T, tc fragmentCorpusCase) []ir.Event {
	t.Helper()
	decoder := chatcompletions.NewStreamDecoder()
	var events []ir.Event
	feed := func(wire any) {
		var chunk chatcompletions.Chunk
		if err := json.Unmarshal(marshalFragmentWire(t, wire), &chunk); err != nil {
			t.Fatal(err)
		}
		more, err := decoder.Feed(&chunk)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, more...)
	}
	base := func(delta any, finish any) map[string]any {
		return map[string]any{
			"id":      "chatcmpl-corpus",
			"object":  "chat.completion.chunk",
			"created": 0,
			"model":   "gpt-4o-mini",
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         delta,
				"finish_reason": finish,
			}},
		}
	}
	feed(base(map[string]any{"role": "assistant"}, nil))
	for index, fragment := range tc.Fragments {
		call := map[string]any{
			"index":    0,
			"function": map[string]any{"arguments": fragment},
		}
		if index == 0 {
			call["id"] = "call-corpus"
			call["type"] = "function"
			call["function"] = map[string]any{
				"name":      "corpus_tool",
				"arguments": fragment,
			}
		}
		feed(base(map[string]any{"tool_calls": []any{call}}, nil))
	}
	feed(base(map[string]any{}, "tool_calls"))
	terminal, err := decoder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	return append(events, terminal...)
}

func decodeResponsesFragmentCase(t *testing.T, tc fragmentCorpusCase) []ir.Event {
	t.Helper()
	decoder := responses.NewStreamDecoder()
	var events []ir.Event
	feed := func(wire any) {
		var event responses.StreamEvent
		if err := json.Unmarshal(marshalFragmentWire(t, wire), &event); err != nil {
			t.Fatal(err)
		}
		more, err := decoder.Feed(&event)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, more...)
	}
	feed(map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id": "resp-corpus", "object": "response", "status": "in_progress",
			"model": "gpt-4o-mini", "output": []any{},
		},
	})
	feed(map[string]any{
		"type": "response.output_item.added", "output_index": 0,
		"item": map[string]any{
			"type": "function_call", "id": "fc-corpus", "call_id": "call-corpus",
			"name": "corpus_tool", "status": "in_progress", "arguments": tc.Fragments[0],
		},
	})
	for _, fragment := range tc.Fragments[1:] {
		feed(map[string]any{
			"type": "response.function_call_arguments.delta", "item_id": "fc-corpus",
			"output_index": 0, "delta": fragment,
		})
	}
	feed(map[string]any{
		"type": "response.function_call_arguments.done", "item_id": "fc-corpus",
		"output_index": 0, "call_id": "call-corpus", "name": "corpus_tool", "arguments": tc.Argument,
	})
	feed(map[string]any{
		"type": "response.output_item.done", "output_index": 0,
		"item": map[string]any{
			"type": "function_call", "id": "fc-corpus", "call_id": "call-corpus",
			"name": "corpus_tool", "status": "completed", "arguments": tc.Argument,
		},
	})
	feed(map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "resp-corpus", "object": "response", "status": "completed",
			"model": "gpt-4o-mini", "output": []any{},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
		},
	})
	terminal, err := decoder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	return append(events, terminal...)
}

func decodeAnthropicFragmentCase(t *testing.T, tc fragmentCorpusCase) []ir.Event {
	t.Helper()
	decoder := messages.NewStreamDecoder()
	var events []ir.Event
	feed := func(wire any) {
		var event messages.StreamEvent
		if err := json.Unmarshal(marshalFragmentWire(t, wire), &event); err != nil {
			t.Fatal(err)
		}
		more, err := decoder.Feed(&event)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, more...)
	}
	feed(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": "msg-corpus", "type": "message", "role": "assistant",
			"model": "claude-sonnet-4-5", "content": []any{}, "stop_reason": nil,
			"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
	feed(map[string]any{
		"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "tool_use", "id": "toolu-corpus", "name": "corpus_tool", "input": map[string]any{}},
	})
	for _, fragment := range tc.Fragments {
		feed(map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": fragment},
		})
	}
	feed(map[string]any{"type": "content_block_stop", "index": 0})
	feed(map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "tool_use"},
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	feed(map[string]any{"type": "message_stop"})
	terminal, err := decoder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	return append(events, terminal...)
}

func validateFragmentEvents(t *testing.T, events []ir.Event, tc fragmentCorpusCase) {
	t.Helper()
	if len(events) < 3 {
		t.Fatalf("got %d events, want terminal stream", len(events))
	}
	nextIndex := 0
	openIndex := -1
	toolInputs := 0
	fragmentCount := 0
	for index, event := range events {
		switch event := event.(type) {
		case ir.MessageStart:
			if index != 0 {
				t.Fatalf("MessageStart at event %d", index)
			}
		case ir.ContentBlockStart:
			if openIndex >= 0 || event.Index != nextIndex {
				t.Fatalf("invalid block start index %d at event %d", event.Index, index)
			}
			openIndex = event.Index
			nextIndex++
			if block, ok := event.Block.(ir.ToolUseBlock); ok {
				var input string
				if err := json.Unmarshal(block.Input, &input); err != nil {
					t.Fatalf("tool input is not an IR string token: %v", err)
				}
				if input != tc.Argument {
					t.Fatalf("tool input = %q, want %q", input, tc.Argument)
				}
				toolInputs++
			}
		case ir.ContentBlockDelta:
			if openIndex < 0 || event.Index != openIndex {
				t.Fatalf("invalid block delta index %d at event %d", event.Index, index)
			}
			if delta, ok := event.Delta.(ir.InputJSONDelta); ok {
				var fragment string
				if err := json.Unmarshal(delta.PartialJSON, &fragment); err != nil {
					t.Fatalf("input delta is not an IR string token: %v", err)
				}
				if fragment != tc.Fragments[fragmentCount] {
					t.Fatalf("fragment %d = %q, want %q", fragmentCount, fragment, tc.Fragments[fragmentCount])
				}
				fragmentCount++
			}
		case ir.ContentBlockStop:
			if openIndex < 0 || event.Index != openIndex {
				t.Fatalf("invalid block stop index %d at event %d", event.Index, index)
			}
			openIndex = -1
		case ir.MessageDelta:
			if openIndex >= 0 {
				t.Fatalf("MessageDelta while block %d is open", openIndex)
			}
		case ir.MessageDone:
			if index != len(events)-1 {
				t.Fatalf("MessageDone at event %d, want final event", index)
			}
		default:
			t.Fatalf("unexpected event %T", event)
		}
	}
	if openIndex >= 0 || toolInputs != 1 || fragmentCount != len(tc.Fragments) {
		t.Fatalf("tool inputs=%d fragments=%d, want 1 and %d", toolInputs, fragmentCount, len(tc.Fragments))
	}
}

func (c fragmentCorpusCase) String() string {
	return fmt.Sprintf("%s/%s", c.Protocol, c.ID)
}
