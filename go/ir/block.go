// Package ir implements the oxa intermediate representation (spec/01).
//
// The IR is the hub of the conversion architecture: every converter either
// produces IR (face -> IR) or consumes it (IR -> face); no converter between
// two faces exists. The JSON shape of every type here is defined normatively
// by spec/schema/ir.schema.json (INV-9); the canonical codec lives in json.go.
package ir

import "encoding/json"

// Block is a content block (spec/01 s3.4). Sealed: the variant set is fixed
// for v1; adding a variant is a breaking spec change.
type Block interface {
	isBlock()
}

const (
	BlockTypeText             = "text"
	BlockTypeImage            = "image"
	BlockTypeToolUse          = "tool_use"
	BlockTypeToolResult       = "tool_result"
	BlockTypeThinking         = "thinking"
	BlockTypeRedactedThinking = "redacted_thinking"
	BlockTypeDocument         = "document"
)

// TextCitation is a citation reference attached to a TextBlock (since 2.4.11).
type TextCitation struct {
	Source string
	Value  string
}

// TextBlock is a run of text.
type TextBlock struct {
	Text      string
	Citations []TextCitation
	Native    []NativeAnnotation
}

// RedactedThinkingBlock is Anthropic encrypted thinking replay state (since 2.4.11).
// Data is opaque provider payload and MUST NOT be logged or inspected (INV-1).
type RedactedThinkingBlock struct {
	Data   string
	Native []NativeAnnotation
}

func (RedactedThinkingBlock) isBlock() {}

func (r RedactedThinkingBlock) String() string {
	return "[RedactedThinkingBlock]"
}

func (r RedactedThinkingBlock) GoString() string {
	return "ir.RedactedThinkingBlock{Data: [REDACTED]}"
}

// ThinkingBlock is the model's reasoning content with optional opaque provider signature (since 2.0).
type ThinkingBlock struct {
	Thinking  string
	Signature string
	Native    []NativeAnnotation
}

// ImageBlock is an image input. Unused in this milestone (M4 wires it);
// exactly one of Data / URL must be present.
type ImageBlock struct {
	MediaType string
	Data      string
	URL       string
	Native    []NativeAnnotation
}

// ToolUseBlock is a tool invocation produced by the model. Input is the raw
// JSON string token carried as opaque JSON text: implementations MUST NOT
// parse or re-serialize it on any conversion path (INV-1). The raw message
// holds the exact source JSON token (including the surrounding quotes), so
// round-trips are byte-faithful down to the escape style.
type ToolUseBlock struct {
	ID     string
	Name   string
	Input  json.RawMessage
	Native []NativeAnnotation
}

// DocumentBlock is a document input (since 2.4.8, request-side only):
// exactly one of Data / URL / FileID identifies the source form. Data
// carries base64 bytes with MediaType (INV-1: never decoded or
// transformed); URL is a document URL; FileID is a provider Files-API
// reference. Document-level provider metadata (title, context,
// citations) travels as NativeAnnotations per spec/15 §3.5.
type DocumentBlock struct {
	MediaType string
	Data      string
	URL       string
	FileID    string
	Native    []NativeAnnotation
}

// ToolResultBlock is the outcome of a tool invocation, supplied by the caller.
type ToolResultBlock struct {
	ToolUseID string
	Content   []Block
	IsError   bool
	Native    []NativeAnnotation
}

func (TextBlock) isBlock()       {}
func (ImageBlock) isBlock()      {}
func (DocumentBlock) isBlock()   {}
func (ToolUseBlock) isBlock()    {}
func (ToolResultBlock) isBlock() {}
func (ThinkingBlock) isBlock()   {}

// SystemBlock is system prompt content (spec/01 s3.2). Sealed; exactly one
// variant (text) in v1.
type SystemBlock struct {
	Text   string
	Native []NativeAnnotation
}
