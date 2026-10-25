package vectest

import (
	"github.com/elkpi/oxa/go/v2/anthropic/messages"
	"github.com/elkpi/oxa/go/v2/ir"
	"github.com/elkpi/oxa/go/v2/openai/chatcompletions"
	"github.com/elkpi/oxa/go/v2/openai/responses"
	"testing"
)

func TestRawReviewUnionUnknownValuesDoNotCauseErrors(t *testing.T) {
	raw := []byte(`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"hi","name":42}]}]}`)
	req, losses, err := messages.DecodeRequestJSON(raw)
	if err != nil || req == nil || len(losses) != 1 {
		t.Fatalf("request=%+v losses=%+v err=%v", req, losses, err)
	}
	if losses[0].Path != "messages[0].content[0].name" {
		t.Fatalf("losses=%+v", losses)
	}
	d := messages.NewStreamDecoder()
	if _, err := d.FeedJSON([]byte(`{"type":"new_event","index":"irrelevant","extension":null}`)); err != nil {
		t.Fatal(err)
	}
	if len(d.Losses()) != 1 {
		t.Fatalf("losses=%+v", d.Losses())
	}
}

func TestRawReviewKnownDroppedPresence(t *testing.T) {
	cases := []struct {
		raw    string
		decode func([]byte) (*ir.Request, []ir.Loss, error)
	}{
		{`{"model":"m","messages":[{"role":"user","content":"hi"}],"metadata":null,"logprobs":null}`, func(raw []byte) (*ir.Request, []ir.Loss, error) { return chatcompletions.DecodeRequestJSON(raw) }},
		{`{"model":"m","input":"hi","metadata":null}`, func(raw []byte) (*ir.Request, []ir.Loss, error) { return responses.DecodeRequestJSON(raw) }},
		{`{"model":"m","max_tokens":32,"messages":[{"role":"user","content":"hi"}],"metadata":null,"tool_choice":{"type":"auto","disable_parallel_tool_use":false}}`, func(raw []byte) (*ir.Request, []ir.Loss, error) { return messages.DecodeRequestJSON(raw) }},
	}
	for i, c := range cases {
		_, losses, err := c.decode([]byte(c.raw))
		expected := 2
		if i == 1 || i == 2 {
			expected = 1
		}
		if err != nil || len(losses) != expected {
			t.Fatalf("case %d losses=%+v err=%v", i, losses, err)
		}
	}
}

func TestRawReviewNamedChoiceAndVariantFields(t *testing.T) {
	_, losses, err := chatcompletions.DecodeRequestJSON([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"function","function":{"name":"f","extension":null},"extension":false}}`))
	if err != nil || len(losses) != 2 {
		t.Fatalf("losses=%+v err=%v", losses, err)
	}
	d := messages.NewStreamDecoder()
	if _, err := d.FeedJSON([]byte(`{"type":"message_start","index":7,"message":{"id":"m1","model":"m","content":[]}}`)); err != nil {
		t.Fatal(err)
	}
	if len(d.Losses()) != 1 || d.Losses()[0].Path != "index" {
		t.Fatalf("losses=%+v", d.Losses())
	}
}

func TestRawReviewSemanticOrderAndSkippedContainment(t *testing.T) {
	d := messages.NewStreamDecoder()
	if _, err := d.FeedJSON([]byte(`{"type":"message_start","message":{"id":"m1","model":"m","content":[]}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.FeedJSON([]byte(`{"type":"message_delta","delta":{"stop_reason":"new_reason","extension":false},"usage":{"input_tokens":1,"output_tokens":1}}`)); err != nil {
		t.Fatal(err)
	}
	if len(d.Losses()) != 2 || d.Losses()[0].Reason != ir.LossUnmappedValue {
		t.Fatalf("losses=%+v", d.Losses())
	}
	d = messages.NewStreamDecoder()
	_, _ = d.FeedJSON([]byte(`{"type":"message_start","message":{"id":"m1","model":"m","content":[]}}`))
	_, err := d.FeedJSON([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"image","extra":null,"source":{"type":"url","url":"example","extra":{}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.FeedJSON([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi","extra":false}}`))
	if err != nil || len(d.Losses()) != 1 {
		t.Fatalf("losses=%+v err=%v", d.Losses(), err)
	}
}

func TestRawReviewCaseLikeUnknownKeysNeverOverrideKnownFields(t *testing.T) {
	req, losses, err := responses.DecodeRequestJSON([]byte(`{"model":"m","input":"hi","Model":"changed","Input":false}`))
	if err != nil || req == nil || req.Model != "m" || len(losses) != 2 {
		t.Fatalf("request=%+v losses=%+v err=%v", req, losses, err)
	}
}
