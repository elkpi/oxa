// Package chatcompletions implements the OpenAI Chat Completions face of the
// oxa conversion library: the non-streaming TEXT/SYSTEM/MULTI-TURN/PARAMS
// subset of this milestone, converted to and from the IR (spec/01). Wire
// types are strictly separate from IR types; this package imports ir and the
// standard library only, never another face.
package chatcompletions

// Request is the Chat Completions wire request (the non-tool subset of the
// fields). Tools and image inputs arrive in later milestones and are not
// declared here.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	MaxTokens   *int64    `json:"max_tokens,omitempty"`
	Stop        []string  `json:"stop,omitempty"`

	// Logprobs/TopLogprobs have no IR equivalent in v1 and are dropped with
	// unmapped-field losses (spec/02 s4).
	Logprobs    any `json:"logprobs,omitempty"`
	TopLogprobs any `json:"top_logprobs,omitempty"`

	// Metadata has no Chat Completions request equivalent in v1; presence is
	// dropped with an unmapped-field loss (vectors/README.md bucket 3).
	Metadata any `json:"metadata,omitempty"`
}

// Message is a wire message. Content is either a string or an array of parts
// ({"type":"text","text":...}); decode handles both, encode renders strings.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content,omitempty"`
}

// ContentPart is one element of a parts-array message content.
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Response is the Chat Completions wire response object.
type Response struct {
	ID      string     `json:"id"`
	Object  string     `json:"object"`
	Created int64      `json:"created"`
	Model   string     `json:"model"`
	Choices []Choice   `json:"choices"`
	Usage   *UsageWire `json:"usage"`
}

// Choice is one element of a wire response's choices array.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// UsageWire is the wire usage object. total_tokens is derived (prompt +
// completion) and recomputed on encode, so its absence on the IR side carries
// no loss (vectors/README.md loss conventions, DERIVED fields).
type UsageWire struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}
