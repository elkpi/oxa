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

// TextBlock is a run of text.
type TextBlock struct {
	Text string
}

// ImageBlock is an image input. Unused in this milestone (M4 wires it);
// exactly one of Data / URL must be present.
type ImageBlock struct {
	MediaType string
	Data      string
	URL       string
}

// ToolUseBlock is a tool invocation produced by the model. Input is the raw
// JSON string token carried as opaque JSON text: implementations MUST NOT
// parse or re-serialize it on any conversion path (INV-1). The raw message
// holds the exact source JSON token (including the surrounding quotes), so
// round-trips are byte-faithful down to the escape style.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultBlock is the outcome of a tool invocation, supplied by the caller.
type ToolResultBlock struct {
	ToolUseID string
	Content   []Block
	IsError   bool
}

func (TextBlock) isBlock()       {}
func (ImageBlock) isBlock()      {}
func (ToolUseBlock) isBlock()    {}
func (ToolResultBlock) isBlock() {}

// SystemBlock is system prompt content (spec/01 s3.2). Sealed; exactly one
// variant (text) in v1.
type SystemBlock struct {
	Text string
}
