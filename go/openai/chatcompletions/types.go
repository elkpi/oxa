// Package chatcompletions implements the OpenAI Chat Completions face of the
// oxa conversion library: the non-streaming TEXT/SYSTEM/MULTI-TURN/PARAMS/
// TOOLS/IMAGE subset of this milestone, converted to and from the IR
// (spec/01). Wire types are strictly separate from IR types; this package
// imports ir and the standard library only, never another face.
package chatcompletions

import "encoding/json"

// Request is the Chat Completions wire request for the supported non-streaming
// subset.
type Request struct {
	Model       string     `json:"model"`
	Messages    []Message  `json:"messages"`
	Temperature *float64   `json:"temperature,omitempty"`
	TopP        *float64   `json:"top_p,omitempty"`
	MaxTokens   *int64     `json:"max_tokens,omitempty"`
	Stop        []string   `json:"stop,omitempty"`
	Tools       []ToolWire `json:"tools,omitempty"`
	ToolChoice  any        `json:"tool_choice,omitempty"` // auto | none | required | ToolChoiceWire

	// ParallelToolCalls has no IR equivalent in v1 and is dropped with an
	// unmapped-field loss (N-CC-9).
	ParallelToolCalls *bool `json:"parallel_tool_calls,omitempty"`

	// The legacy functions/function_call shapes have no IR representation.
	Functions      any `json:"functions,omitempty"`
	FunctionCall   any `json:"function_call,omitempty"`
	ResponseFormat any `json:"response_format,omitempty"`

	// Logprobs/TopLogprobs have no IR equivalent in v1 and are dropped with
	// unmapped-field losses (spec/02 s4).
	Logprobs    any `json:"logprobs,omitempty"`
	TopLogprobs any `json:"top_logprobs,omitempty"`

	// Metadata has no Chat Completions request equivalent in v1; presence is
	// dropped with an unmapped-field loss (vectors/README.md bucket 3).
	Metadata any `json:"metadata,omitempty"`
}

// ToolWire is one element of a wire tools array. The supported Chat
// Completions tool variant is type:function.
type ToolWire struct {
	Type     string       `json:"type"`
	Function FunctionWire `json:"function"`
}

// FunctionWire is a Chat Completions function definition or call payload.
// Parameters is a JSON-Schema-shaped object carried verbatim.
type FunctionWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}

// ToolChoiceWire forces one named function.
type ToolChoiceWire struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

// Message is a wire message. Content is a string or an array of ContentPart
// values. ToolCalls is populated on assistant messages; ToolCallID is
// populated on role:"tool" result messages.
type Message struct {
	Role         string     `json:"role"`
	Content      any        `json:"content,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID   string     `json:"tool_call_id,omitempty"`
	FunctionCall any        `json:"function_call,omitempty"`
}

// ToolCall is an assistant function invocation. Function.Arguments is raw
// JSON text held as a Go string and copied into the IR without parsing.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionWire `json:"function"`
}

// ContentPart is one element of a parts-array message content.
type ContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ImageURLWire `json:"image_url,omitempty"`
}

// ImageURLWire holds the URL of an image_url content part. URL is either an
// https URL or a data:<image MIME>;base64,<payload> URL in the supported set.
type ImageURLWire struct {
	URL string `json:"url"`
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
