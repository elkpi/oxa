// Package chatcompletions implements the OpenAI Chat Completions face of the
// oxa conversion library: the non-streaming TEXT/SYSTEM/MULTI-TURN/PARAMS/
// TOOLS/IMAGE subset of this milestone, converted to and from the IR
// (spec/01). Wire types are strictly separate from IR types; this package
// imports ir and the standard library only, never another face.
package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

// Request is the Chat Completions wire request for the supported non-streaming
// subset.
type Request struct {
	Model               string             `json:"model"`
	Messages            []Message          `json:"messages"`
	Temperature         *float64           `json:"temperature,omitempty"`
	TopP                *float64           `json:"top_p,omitempty"`
	MaxTokens           *int64             `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int64             `json:"max_completion_tokens,omitempty"`
	Store               *bool              `json:"store,omitempty"`
	Stream              *bool              `json:"stream,omitempty"`
	StreamOptions       *StreamOptionsWire `json:"stream_options,omitempty"`
	Stop                []string           `json:"stop,omitempty"`
	ReasoningEffort     string             `json:"reasoning_effort,omitempty"`
	Verbosity           *string            `json:"verbosity,omitempty"`
	Tools               []ToolWire         `json:"tools,omitempty"`
	ToolChoice          any                `json:"tool_choice,omitempty"` // auto | none | required | ToolChoiceWire

	// ParallelToolCalls has no IR equivalent in v1 and is dropped with an
	// unmapped-field loss (N-CC-9).
	ParallelToolCalls  *bool   `json:"parallel_tool_calls,omitempty"`
	PromptCacheKey     *string `json:"prompt_cache_key,omitempty"`
	PromptCacheOptions any     `json:"prompt_cache_options,omitempty"`

	// The legacy functions/function_call shapes have no IR representation.
	Functions    any `json:"functions,omitempty"`
	FunctionCall any `json:"function_call,omitempty"`

	// ResponseFormat specifies output formatting constraints (spec/10 s3).
	ResponseFormat any `json:"response_format,omitempty"`

	// Logprobs/TopLogprobs have no IR equivalent in v1 and are dropped with
	// unmapped-field losses (spec/02 s4).
	Logprobs    any `json:"logprobs,omitempty"`
	TopLogprobs any `json:"top_logprobs,omitempty"`

	// Metadata has no Chat Completions request equivalent in v1; presence is
	// dropped with an unmapped-field loss (vectors/README.md bucket 3).
	Metadata any `json:"metadata,omitempty"`

	rawStore              json.RawMessage
	rawStream             json.RawMessage
	rawStreamOptions      json.RawMessage
	rawPromptCacheOptions json.RawMessage
}

func (r *Request) UnmarshalJSON(data []byte) error {
	type shadow struct {
		Model               string          `json:"model"`
		Messages            []Message       `json:"messages"`
		Temperature         *float64        `json:"temperature,omitempty"`
		TopP                *float64        `json:"top_p,omitempty"`
		MaxTokens           *int64          `json:"max_tokens,omitempty"`
		MaxCompletionTokens *int64          `json:"max_completion_tokens,omitempty"`
		Store               json.RawMessage `json:"store,omitempty"`
		Stream              json.RawMessage `json:"stream,omitempty"`
		StreamOptions       json.RawMessage `json:"stream_options,omitempty"`
		Stop                []string        `json:"stop,omitempty"`
		ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
		Verbosity           *string         `json:"verbosity,omitempty"`
		Tools               []ToolWire      `json:"tools,omitempty"`
		ToolChoice          any             `json:"tool_choice,omitempty"`
		ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
		PromptCacheKey      *string         `json:"prompt_cache_key,omitempty"`
		PromptCacheOptions  json.RawMessage `json:"prompt_cache_options,omitempty"`
		Functions           any             `json:"functions,omitempty"`
		FunctionCall        any             `json:"function_call,omitempty"`
		ResponseFormat      any             `json:"response_format,omitempty"`
		Logprobs            any             `json:"logprobs,omitempty"`
		TopLogprobs         any             `json:"top_logprobs,omitempty"`
		Metadata            any             `json:"metadata,omitempty"`
	}
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawFields); err != nil {
		return err
	}
	if rawPTC, ok := rawFields["parallel_tool_calls"]; ok && strings.TrimSpace(string(rawPTC)) == "null" {
		return fmt.Errorf("chatcompletions: parallel_tool_calls must be a boolean")
	}
	if rawPCK, ok := rawFields["prompt_cache_key"]; ok && strings.TrimSpace(string(rawPCK)) == "null" {
		return fmt.Errorf("chatcompletions: prompt_cache_key must be a string")
	}
	r.Model = s.Model
	r.Messages = s.Messages
	r.Temperature = s.Temperature
	r.TopP = s.TopP
	r.MaxTokens = s.MaxTokens
	r.MaxCompletionTokens = s.MaxCompletionTokens
	r.Stop = s.Stop
	r.ReasoningEffort = s.ReasoningEffort
	r.Verbosity = s.Verbosity
	r.Tools = s.Tools
	r.ToolChoice = s.ToolChoice
	r.ParallelToolCalls = s.ParallelToolCalls
	r.PromptCacheKey = s.PromptCacheKey
	r.Functions = s.Functions
	r.FunctionCall = s.FunctionCall
	r.ResponseFormat = s.ResponseFormat
	r.Logprobs = s.Logprobs
	r.TopLogprobs = s.TopLogprobs
	r.Metadata = s.Metadata

	if s.MaxCompletionTokens != nil && *s.MaxCompletionTokens <= 0 {
		return fmt.Errorf("chatcompletions: max_completion_tokens must be a positive integer")
	}

	if len(s.Store) > 0 {
		r.rawStore = s.Store
		trimmed := strings.TrimSpace(string(s.Store))
		if trimmed != "true" && trimmed != "false" && trimmed != "null" {
			return fmt.Errorf("chatcompletions: store must be a boolean or null")
		}
		switch trimmed {
		case "true":
			b := true
			r.Store = &b
		case "false":
			b := false
			r.Store = &b
		}
	}

	if len(s.Stream) > 0 {
		r.rawStream = s.Stream
		trimmed := strings.TrimSpace(string(s.Stream))
		if trimmed != "true" && trimmed != "false" && trimmed != "null" {
			return fmt.Errorf("chatcompletions: stream must be a boolean or null")
		}
		switch trimmed {
		case "true":
			b := true
			r.Stream = &b
		case "false":
			b := false
			r.Stream = &b
		}
	}

	if len(s.StreamOptions) > 0 {
		r.rawStreamOptions = s.StreamOptions
		trimmed := strings.TrimSpace(string(s.StreamOptions))
		if trimmed == "null" {
			// explicit null is allowed
		} else {
			var so map[string]any
			if err := json.Unmarshal(s.StreamOptions, &so); err != nil || so == nil {
				return fmt.Errorf("chatcompletions: stream_options must be an object or null")
			}
			if u, ok := so["include_usage"]; ok {
				if _, isBool := u.(bool); !isBool {
					return fmt.Errorf("chatcompletions: stream_options.include_usage must be a boolean")
				}
			}
			if ob, ok := so["include_obfuscation"]; ok {
				if _, isBool := ob.(bool); !isBool {
					return fmt.Errorf("chatcompletions: stream_options.include_obfuscation must be a boolean")
				}
			}
			var soWire StreamOptionsWire
			_ = json.Unmarshal(s.StreamOptions, &soWire)
			r.StreamOptions = &soWire
		}
	}
	if len(s.PromptCacheOptions) > 0 {
		r.rawPromptCacheOptions = s.PromptCacheOptions
		trimmed := strings.TrimSpace(string(s.PromptCacheOptions))
		if trimmed != "null" {
			var pco map[string]any
			if err := json.Unmarshal(s.PromptCacheOptions, &pco); err != nil || pco == nil {
				return fmt.Errorf("chatcompletions: prompt_cache_options must be an object or null")
			}
		}
	}
	return nil
}

func (r Request) MarshalJSON() ([]byte, error) {
	type shadow struct {
		Model               string          `json:"model"`
		Messages            []Message       `json:"messages"`
		Temperature         *float64        `json:"temperature,omitempty"`
		TopP                *float64        `json:"top_p,omitempty"`
		MaxTokens           *int64          `json:"max_tokens,omitempty"`
		MaxCompletionTokens *int64          `json:"max_completion_tokens,omitempty"`
		Store               json.RawMessage `json:"store,omitempty"`
		Stream              json.RawMessage `json:"stream,omitempty"`
		StreamOptions       json.RawMessage `json:"stream_options,omitempty"`
		Stop                []string        `json:"stop,omitempty"`
		ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
		Verbosity           *string         `json:"verbosity,omitempty"`
		Tools               []ToolWire      `json:"tools,omitempty"`
		ToolChoice          any             `json:"tool_choice,omitempty"`
		ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
		PromptCacheKey      *string         `json:"prompt_cache_key,omitempty"`
		PromptCacheOptions  json.RawMessage `json:"prompt_cache_options,omitempty"`
		Functions           any             `json:"functions,omitempty"`
		FunctionCall        any             `json:"function_call,omitempty"`
		ResponseFormat      any             `json:"response_format,omitempty"`
		Logprobs            any             `json:"logprobs,omitempty"`
		TopLogprobs         any             `json:"top_logprobs,omitempty"`
		Metadata            any             `json:"metadata,omitempty"`
	}
	s := shadow{
		Model:               r.Model,
		Messages:            r.Messages,
		Temperature:         r.Temperature,
		TopP:                r.TopP,
		MaxTokens:           r.MaxTokens,
		MaxCompletionTokens: r.MaxCompletionTokens,
		Stop:                r.Stop,
		ReasoningEffort:     r.ReasoningEffort,
		Verbosity:           r.Verbosity,
		Tools:               r.Tools,
		ToolChoice:          r.ToolChoice,
		ParallelToolCalls:   r.ParallelToolCalls,
		PromptCacheKey:      r.PromptCacheKey,
		Functions:           r.Functions,
		FunctionCall:        r.FunctionCall,
		ResponseFormat:      r.ResponseFormat,
		Logprobs:            r.Logprobs,
		TopLogprobs:         r.TopLogprobs,
		Metadata:            r.Metadata,
	}
	if len(r.rawStore) > 0 {
		s.Store = r.rawStore
	} else if r.Store != nil {
		if *r.Store {
			s.Store = json.RawMessage("true")
		} else {
			s.Store = json.RawMessage("false")
		}
	}
	if len(r.rawStream) > 0 {
		s.Stream = r.rawStream
	} else if r.Stream != nil {
		if *r.Stream {
			s.Stream = json.RawMessage("true")
		} else {
			s.Stream = json.RawMessage("false")
		}
	}
	if len(r.rawStreamOptions) > 0 {
		s.StreamOptions = r.rawStreamOptions
	} else if r.StreamOptions != nil {
		b, _ := json.Marshal(r.StreamOptions)
		s.StreamOptions = b
	}
	if len(r.rawPromptCacheOptions) > 0 {
		s.PromptCacheOptions = r.rawPromptCacheOptions
	} else if r.PromptCacheOptions != nil {
		b, _ := json.Marshal(r.PromptCacheOptions)
		s.PromptCacheOptions = b
	}
	return json.Marshal(s)
}

// StreamOptionsWire is the wire shape for stream_options.
type StreamOptionsWire struct {
	IncludeObfuscation *bool `json:"include_obfuscation,omitempty"`
	IncludeUsage       *bool `json:"include_usage,omitempty"`
}

// ResponseFormatWire is the wire shape for Chat Completions response_format.
type ResponseFormatWire struct {
	Type       string          `json:"type"`
	JSONSchema *JSONSchemaWire `json:"json_schema,omitempty"`
}

// JSONSchemaWire is the nested configuration for response_format with type json_schema.
type JSONSchemaWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// ToolWire is one element of a wire tools array.
type ToolWire struct {
	Type     string          `json:"type"`
	Function FunctionWire    `json:"function"`
	Custom   *CustomToolWire `json:"custom,omitempty"`
}

func (w ToolWire) MarshalJSON() ([]byte, error) {
	if w.Type == ir.ToolTypeCustom {
		type wire struct {
			Type   string          `json:"type"`
			Custom *CustomToolWire `json:"custom"`
		}
		return json.Marshal(wire{Type: w.Type, Custom: w.Custom})
	}
	type wire struct {
		Type     string       `json:"type"`
		Function FunctionWire `json:"function"`
	}
	return json.Marshal(wire{Type: w.Type, Function: w.Function})
}

func (w *ToolWire) UnmarshalJSON(data []byte) error {
	type wire struct {
		Type     string          `json:"type"`
		Function FunctionWire    `json:"function"`
		Custom   *CustomToolWire `json:"custom"`
	}
	var aux wire
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	w.Type = aux.Type
	w.Function = aux.Function
	w.Custom = aux.Custom
	return nil
}

type CustomToolWire struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Format      *ir.CustomToolFormat `json:"format,omitempty"`
}

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleDeveloper = "developer"

	ToolTypeFunction = "function"

	ContentPartTypeText     = "text"
	ContentPartTypeImageURL = "image_url"
	ContentPartTypeFile     = "file"

	ToolChoiceAuto     = "auto"
	ToolChoiceNone     = "none"
	ToolChoiceRequired = "required"

	FinishReasonStop          = "stop"
	FinishReasonLength        = "length"
	FinishReasonContentFilter = "content_filter"
	FinishReasonToolCalls     = "tool_calls"

	ObjectChatCompletion      = "chat.completion"
	ObjectChatCompletionChunk = "chat.completion.chunk"
)

// FunctionWire is a Chat Completions function definition or call payload.
// Parameters is a JSON-Schema-shaped object carried verbatim.
type FunctionWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
}

// ToolChoiceWire forces one named function or custom tool.
type ToolChoiceWire struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
	Custom *struct {
		Name string `json:"name"`
	} `json:"custom,omitempty"`
}

func (c ToolChoiceWire) MarshalJSON() ([]byte, error) {
	if c.Type == ir.ToolTypeCustom {
		type wire struct {
			Type   string `json:"type"`
			Custom *struct {
				Name string `json:"name"`
			} `json:"custom"`
		}
		return json.Marshal(wire{Type: c.Type, Custom: c.Custom})
	}
	type wire struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	return json.Marshal(wire{Type: c.Type, Function: c.Function})
}

func (c *ToolChoiceWire) UnmarshalJSON(data []byte) error {
	type wire struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
		Custom *struct {
			Name string `json:"name"`
		} `json:"custom"`
	}
	var aux wire
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	c.Type = aux.Type
	c.Function = aux.Function
	c.Custom = aux.Custom
	return nil
}

// Message is a wire message. Content is a string or an array of ContentPart
// values. ToolCalls is populated on assistant messages; ToolCallID is
// populated on role:"tool" result messages.
type Message struct {
	Role             string     `json:"role"`
	Content          any        `json:"content,omitempty"`
	Refusal          string     `json:"refusal,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	FunctionCall     any        `json:"function_call,omitempty"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	type shadow Message
	if m.Refusal != "" && m.Content == nil {
		return json.Marshal(struct {
			shadow
			Content any `json:"content"`
		}{
			shadow:  shadow(m),
			Content: nil,
		})
	}
	return json.Marshal(shadow(m))
}

// ToolCall is an assistant function or custom tool invocation.
type ToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function FunctionWire    `json:"function"`
	Custom   *CustomCallWire `json:"custom,omitempty"`
}

func (c ToolCall) MarshalJSON() ([]byte, error) {
	if c.Type == ir.ToolTypeCustom {
		type wire struct {
			ID     string          `json:"id"`
			Type   string          `json:"type"`
			Custom *CustomCallWire `json:"custom"`
		}
		return json.Marshal(wire{ID: c.ID, Type: c.Type, Custom: c.Custom})
	}
	type wire struct {
		ID       string       `json:"id"`
		Type     string       `json:"type"`
		Function FunctionWire `json:"function"`
	}
	return json.Marshal(wire{ID: c.ID, Type: c.Type, Function: c.Function})
}

func (c *ToolCall) UnmarshalJSON(data []byte) error {
	type wire struct {
		ID       string          `json:"id"`
		Type     string          `json:"type"`
		Function FunctionWire    `json:"function"`
		Custom   *CustomCallWire `json:"custom"`
	}
	var aux wire
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	c.ID = aux.ID
	c.Type = aux.Type
	c.Function = aux.Function
	c.Custom = aux.Custom
	return nil
}

type CustomCallWire struct {
	Name  string `json:"name"`
	Input string `json:"input"`
}

type CustomCallDeltaWire struct {
	Name  string  `json:"name,omitempty"`
	Input *string `json:"input,omitempty"`
}

// ContentPart is one element of a parts-array message content.
type ContentPart struct {
	Type                  string          `json:"type"`
	Text                  string          `json:"text,omitempty"`
	ImageURL              *ImageURLWire   `json:"image_url,omitempty"`
	File                  *FileWire       `json:"file,omitempty"`
	PromptCacheBreakpoint json.RawMessage `json:"prompt_cache_breakpoint,omitempty"`
}

// FileWire holds the payload of a file content part (since 2.5.0).
type FileWire struct {
	FileID   *string `json:"file_id,omitempty"`
	FileData *string `json:"file_data,omitempty"`
	Filename *string `json:"filename,omitempty"`
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

// Chunk is one Chat Completions streamed chunk (object
// "chat.completion.chunk"). Choices carries incremental deltas; the final
// usage-only chunk has empty Choices and a populated Usage (when
// stream_options.include_usage was set).
type Chunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChoiceDelta `json:"choices"`
	Usage   *UsageWire    `json:"usage,omitempty"`
}

// ChoiceDelta is one element of a wire chunk's choices array. FinishReason is
// a pointer to distinguish absent (null while streaming) from a mapped value;
// Content likewise distinguishes absent from empty text.
type ChoiceDelta struct {
	Index        int          `json:"index"`
	Delta        DeltaPayload `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
}

// DeltaPayload is the incremental delta object of a chunk choice. Pointer
// fields distinguish an absent wire field from a present empty fragment.
type DeltaPayload struct {
	Role             string          `json:"role,omitempty"`
	Content          *string         `json:"content,omitempty"`
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta is one incremental Chat Completions tool call. Index is always
// present in the wire shape, including index zero; its other fields are
// pointers so absent fields remain distinct from present empty strings.
type ToolCallDelta struct {
	Index    int                  `json:"index"`
	ID       *string              `json:"id,omitempty"`
	Type     *string              `json:"type,omitempty"`
	Function *FunctionDelta       `json:"function,omitempty"`
	Custom   *CustomCallDeltaWire `json:"custom,omitempty"`
}

// FunctionDelta is the incremental function payload of a tool call. Arguments
// is opaque raw JSON text carried as a Go string; it is never parsed here.
type FunctionDelta struct {
	Name      *string `json:"name,omitempty"`
	Arguments *string `json:"arguments,omitempty"`
}

// UsageWire is the wire usage object. total_tokens is derived (prompt +
// completion) and recomputed on encode, so its absence on the IR side carries
// no loss (vectors/README.md loss conventions, DERIVED fields).
type UsageWire struct {
	PromptTokens            int64                        `json:"prompt_tokens"`
	CompletionTokens        int64                        `json:"completion_tokens"`
	TotalTokens             int64                        `json:"total_tokens"`
	PromptTokensDetails     *PromptTokensDetailsWire     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetailsWire `json:"completion_tokens_details,omitempty"`
}

// PromptTokensDetailsWire carries fine-grained prompt token counts.
type PromptTokensDetailsWire struct {
	CachedTokens int64 `json:"cached_tokens"`
}

// CompletionTokensDetailsWire carries fine-grained completion token counts.
type CompletionTokensDetailsWire struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}
