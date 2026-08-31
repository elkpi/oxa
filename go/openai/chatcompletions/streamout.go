package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/ir"
	"github.com/elkpi/oxa/go/modelmap"
)

type streamBlockKind uint8

const (
	streamTextBlock streamBlockKind = iota
	streamToolBlock
)

// streamEncodeBlock retains the current IR block. Tool input and fragments are
// decoded only from their outer IR JSON string tokens; their payloads remain
// opaque and are emitted as CC function.arguments strings without inspection.
type streamEncodeBlock struct {
	kind        streamBlockKind
	index       int
	toolID      string
	toolName    string
	toolInput   string
	fragments   []string
	nativeIndex int
	toolStarted bool
}

// StreamEncoder incrementally converts an IR event stream into Chat
// Completions chunks (IR -> face), enforcing the INV-5 grammar. Envelope
// fields absent from the IR render with the documented defaults (object
// "chat.completion.chunk", created 0, single choice index 0) and record no
// loss. Tool chunks remain buffered until MessageDelta so a later text block
// can be normalized before all native tool_calls.
type StreamEncoder struct {
	models          modelmap.Table
	id              string
	model           string
	started         bool
	active          *streamEncodeBlock
	nextIRIndex     int
	nextNativeTool  int
	toolSeen        bool
	orderingDegrade bool
	pendingTools    []*Chunk
	finished        bool // MessageDelta applied
	done            bool // MessageDone applied
}

// NewStreamEncoder returns an event-stream encoder. The variadic Options match
// the package conversion functions (WithModelMap applies to the chunk Model).
func NewStreamEncoder(opts ...Option) *StreamEncoder {
	o := newOptions(opts...)
	return &StreamEncoder{models: o.models}
}

// Apply pushes one IR event and returns the chunks it produces (possibly none:
// content-block lifecycle events are absorbed into the chunk deltas).
// Out-of-grammar orderings are structural errors.
func (e *StreamEncoder) Apply(ev ir.Event) ([]*Chunk, []ir.Loss, error) {
	if ev == nil {
		return nil, nil, fmt.Errorf("chatcompletions: nil event")
	}
	if e.done || (e.finished && !isMessageDone(ev)) {
		return nil, nil, fmt.Errorf("chatcompletions: event applied after stream termination (%T)", ev)
	}

	switch event := ev.(type) {
	case ir.MessageStart:
		if e.started {
			return nil, nil, fmt.Errorf("chatcompletions: duplicate MessageStart")
		}
		e.started = true
		e.id = event.ID
		e.model = e.models.Map(event.Model)
		return []*Chunk{e.chunk(DeltaPayload{Role: "assistant"})}, nil, nil

	case ir.ContentBlockStart:
		if !e.started || e.active != nil {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockStart out of grammar order")
		}
		if event.Index != e.nextIRIndex {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockStart index %d, want %d", event.Index, e.nextIRIndex)
		}
		e.nextIRIndex++
		switch block := event.Block.(type) {
		case ir.TextBlock:
			if e.toolSeen {
				e.orderingDegrade = true
			}
			e.active = &streamEncodeBlock{kind: streamTextBlock, index: event.Index}
			return nil, nil, nil
		case ir.ToolUseBlock:
			if block.ID == "" || block.Name == "" {
				return nil, nil, fmt.Errorf("chatcompletions: ToolUseBlock requires nonempty ID and name")
			}
			input, err := unwrapIRString(block.Input)
			if err != nil {
				return nil, nil, fmt.Errorf("chatcompletions: ToolUseBlock input: %w", err)
			}
			e.active = &streamEncodeBlock{
				kind:        streamToolBlock,
				index:       event.Index,
				toolID:      block.ID,
				toolName:    block.Name,
				toolInput:   input,
				nativeIndex: e.nextNativeTool,
			}
			e.nextNativeTool++
			e.toolSeen = true
			return nil, nil, nil
		default:
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockStart carries unsupported block %T", block)
		}

	case ir.ContentBlockDelta:
		if e.active == nil || event.Index != e.active.index {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockDelta out of grammar order")
		}
		switch e.active.kind {
		case streamTextBlock:
			delta, ok := event.Delta.(ir.TextDelta)
			if !ok {
				return nil, nil, fmt.Errorf("chatcompletions: TextBlock received non-text delta %T", event.Delta)
			}
			text := delta.Text
			return []*Chunk{e.chunk(DeltaPayload{Content: &text})}, nil, nil
		case streamToolBlock:
			delta, ok := event.Delta.(ir.InputJSONDelta)
			if !ok {
				return nil, nil, fmt.Errorf("chatcompletions: ToolUseBlock received non-input-json delta %T", event.Delta)
			}
			fragment, err := unwrapIRString(delta.PartialJSON)
			if err != nil {
				return nil, nil, fmt.Errorf("chatcompletions: InputJSONDelta partial_json: %w", err)
			}
			e.active.fragments = append(e.active.fragments, fragment)
			e.queueToolArguments(e.active, fragment)
			return nil, nil, nil
		default:
			return nil, nil, fmt.Errorf("chatcompletions: unknown active block kind")
		}

	case ir.ContentBlockStop:
		if e.active == nil || event.Index != e.active.index {
			return nil, nil, fmt.Errorf("chatcompletions: ContentBlockStop out of grammar order")
		}
		if e.active.kind == streamToolBlock {
			if len(e.active.fragments) == 0 {
				// M7 permits an IR-to-CC ToolUseBlock with no fragments. Emit one
				// synthesized native arguments delta so CC receives the full input.
				e.active.fragments = append(e.active.fragments, e.active.toolInput)
				e.queueToolArguments(e.active, e.active.toolInput)
			}
			if strings.Join(e.active.fragments, "") != e.active.toolInput {
				return nil, nil, fmt.Errorf("chatcompletions: ToolUseBlock input does not equal concatenated InputJSONDelta fragments")
			}
		}
		e.active = nil
		return nil, nil, nil

	case ir.MessageDelta:
		if !e.started || e.active != nil {
			return nil, nil, fmt.Errorf("chatcompletions: MessageDelta out of grammar order")
		}
		finish, finishLoss, err := encodeFinishReason(event.StopReason)
		if err != nil {
			return nil, nil, err
		}
		var losses []ir.Loss
		if finishLoss != nil {
			losses = append(losses, *finishLoss)
		}
		if e.orderingDegrade {
			losses = append(losses, loss(
				"events", "ordering", ir.LossDegraded,
				"N-S-10: the text block after a tool block is normalized ahead of the tool calls; IR source order is not preserved",
			))
		}
		e.finished = true
		chunks := append([]*Chunk(nil), e.pendingTools...)
		e.pendingTools = nil
		chunks = append(chunks, &Chunk{
			ID:      e.id,
			Object:  "chat.completion.chunk",
			Created: 0,
			Model:   e.model,
			Choices: []ChoiceDelta{{Index: 0, Delta: DeltaPayload{}, FinishReason: &finish}},
			Usage: &UsageWire{
				PromptTokens:     event.Usage.InputTokens,
				CompletionTokens: event.Usage.OutputTokens,
				TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
			},
		})
		return chunks, losses, nil

	case ir.MessageDone:
		if !e.finished {
			return nil, nil, fmt.Errorf("chatcompletions: MessageDone out of grammar order")
		}
		e.done = true
		return nil, nil, nil

	default:
		return nil, nil, fmt.Errorf("chatcompletions: unknown event %T", ev)
	}
}

func (e *StreamEncoder) chunk(delta DeltaPayload) *Chunk {
	return &Chunk{
		ID:      e.id,
		Object:  "chat.completion.chunk",
		Created: 0,
		Model:   e.model,
		Choices: []ChoiceDelta{{Index: 0, Delta: delta}},
	}
}

func (e *StreamEncoder) queueToolArguments(block *streamEncodeBlock, arguments string) {
	args := arguments
	function := &FunctionDelta{Arguments: &args}
	call := ToolCallDelta{Index: block.nativeIndex, Function: function}
	if !block.toolStarted {
		id := block.toolID
		kind := "function"
		name := block.toolName
		call.ID = &id
		call.Type = &kind
		function.Name = &name
		block.toolStarted = true
	}
	e.pendingTools = append(e.pendingTools, e.chunk(DeltaPayload{ToolCalls: []ToolCallDelta{call}}))
}

// unwrapIRString validates and unwraps only the outer IR raw JSON string token.
// The returned payload is tool parameter text and is intentionally never parsed
// as JSON itself.
func unwrapIRString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

func isMessageDone(ev ir.Event) bool {
	_, ok := ev.(ir.MessageDone)
	return ok
}

// encodeFinishReason maps an IR stop reason to a finish_reason value; it is
// the inverse of the non-streaming response encode (spec/01 s4.1).
func encodeFinishReason(stop ir.StopReason) (string, *ir.Loss, error) {
	switch stop {
	case ir.StopEndTurn:
		return "stop", nil, nil
	case ir.StopMaxTokens:
		return "length", nil, nil
	case ir.StopRefusal:
		return "content_filter", nil, nil
	case ir.StopToolUse:
		return "tool_calls", nil, nil
	case ir.StopSequence:
		return "stop", &ir.Loss{
			Field:  "stop_sequence",
			Reason: ir.LossUnmappedValue,
			Detail: "Chat Completions finish_reason \"stop\" does not identify the matched stop sequence",
		}, nil
	default:
		return "", nil, fmt.Errorf("chatcompletions: stop reason %q has no Chat Completions equivalent", stop)
	}
}
