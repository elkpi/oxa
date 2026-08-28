package ir

import "encoding/json"

// Event is a streaming event (spec/01 s5.1). Sealed: exactly six variants in
// v1. Type definitions only in this milestone; streaming logic arrives in
// M6/M7.
type Event interface {
	isEvent()
}

// MessageStart opens the stream.
type MessageStart struct {
	ID    string
	Model string
}

// ContentBlockStart opens a content block at Index.
type ContentBlockStart struct {
	Index int
	Block Block
}

// ContentBlockDelta carries a delta for the currently open block.
type ContentBlockDelta struct {
	Index int
	Delta Delta
}

// ContentBlockStop closes the currently open block.
type ContentBlockStop struct {
	Index int
}

// MessageDelta carries the final stop reason and usage, immediately before
// MessageDone (INV-5).
type MessageDelta struct {
	StopReason   StopReason
	StopSequence string // same conditional rule as Response.StopSequence
	Usage        Usage
}

// MessageDone terminates the stream.
type MessageDone struct{}

func (MessageStart) isEvent()      {}
func (ContentBlockStart) isEvent() {}
func (ContentBlockDelta) isEvent() {}
func (ContentBlockStop) isEvent()  {}
func (MessageDelta) isEvent()      {}
func (MessageDone) isEvent()       {}

// Delta is the payload of a ContentBlockDelta (spec/01 s5.2). Sealed: exactly
// two variants in v1.
type Delta interface {
	isDelta()
}

// TextDelta is a text fragment.
type TextDelta struct {
	Text string
}

// InputJSONDelta is a fragment of the tool-argument string. PartialJSON is
// raw JSON text held as the exact source JSON string token (INV-1): it is
// never parsed or re-serialized, and concatenation of all fragments of a
// block is the block's input.
type InputJSONDelta struct {
	PartialJSON json.RawMessage
}

func (TextDelta) isDelta()      {}
func (InputJSONDelta) isDelta() {}

// EventStream is the JSON document form of a streamed response (spec/01
// s5.3): specVersion plus the totally ordered event sequence.
type EventStream struct {
	Events []Event
}
