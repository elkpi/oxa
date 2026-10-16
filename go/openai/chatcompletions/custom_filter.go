package chatcompletions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

type customStreamFilter struct {
	phase        int
	nextIndex    int
	outputIndex  int
	open         bool
	custom       bool
	input        string
	fragments    []string
	dropped      bool
	functionSeen bool
	eventNumber  int
}

func (f *customStreamFilter) filter(ev ir.Event) (ir.Event, []ir.Loss, error) {
	eventNumber := f.eventNumber
	f.eventNumber++
	fail := func() (ir.Event, []ir.Loss, error) {
		return nil, nil, fmt.Errorf("custom stream filter: invalid IR event grammar/index")
	}
	switch e := ev.(type) {
	case ir.MessageStart:
		if f.phase != 0 {
			return fail()
		}
		f.phase = 1
		return e, nil, nil
	case ir.ContentBlockStart:
		if f.phase != 1 || f.open || e.Index != f.nextIndex {
			return fail()
		}
		f.open = true
		f.nextIndex++
		f.custom = false
		if b, ok := e.Block.(ir.CustomToolUseBlock); ok {
			if b.ID == "" || b.Name == "" || len(b.Input) == 0 || b.Input[0] != '"' {
				return fail()
			}
			if err := json.Unmarshal(b.Input, &f.input); err != nil {
				return nil, nil, err
			}
			f.custom = true
			f.dropped = true
			f.fragments = nil
			return nil, []ir.Loss{{Path: fmt.Sprintf("events[%d].block", eventNumber), Field: "type", Reason: ir.LossUnsupportedSemantic, Detail: "custom tool calls are not supported by this target profile"}}, nil
		}
		if b, ok := e.Block.(ir.ApplyPatchToolUseBlock); ok {
			if b.ID == "" || b.Status == "" || b.Operation == nil {
				return fail()
			}
			f.custom = true
			f.dropped = true
			f.fragments = nil
			return nil, []ir.Loss{{Path: fmt.Sprintf("events[%d].block", eventNumber), Field: "type", Reason: ir.LossUnsupportedSemantic, Detail: "apply_patch tool calls are not supported by this target profile"}}, nil
		}
		if _, ok := e.Block.(ir.ToolUseBlock); ok {
			f.functionSeen = true
		}
		e.Index = f.outputIndex
		return e, nil, nil
	case ir.ContentBlockDelta:
		if f.phase != 1 || !f.open || e.Index != f.nextIndex-1 {
			return fail()
		}
		if f.custom {
			d, ok := e.Delta.(ir.CustomToolInputDelta)
			if !ok || len(d.Text) == 0 || d.Text[0] != '"' {
				return fail()
			}
			var part string
			if err := json.Unmarshal(d.Text, &part); err != nil {
				return nil, nil, err
			}
			f.fragments = append(f.fragments, part)
			return nil, nil, nil
		}
		e.Index = f.outputIndex
		return e, nil, nil
	case ir.ContentBlockStop:
		if f.phase != 1 || !f.open || e.Index != f.nextIndex-1 {
			return fail()
		}
		f.open = false
		if f.custom {
			if len(f.fragments) > 0 && strings.Join(f.fragments, "") != f.input {
				return nil, nil, fmt.Errorf("custom stream filter: input fragment mismatch")
			}
			f.custom = false
			return nil, nil, nil
		}
		e.Index = f.outputIndex
		f.outputIndex++
		return e, nil, nil
	case ir.MessageDelta:
		if f.phase != 1 || f.open {
			return fail()
		}
		f.phase = 2
		if f.dropped && !f.functionSeen && e.StopReason == ir.StopToolUse {
			e.StopReason = ir.StopEndTurn
			return e, []ir.Loss{{Path: "stop_reason", Field: "stop_reason", Reason: ir.LossDegraded, Detail: "custom calls were dropped; tool_use normalized to end_turn"}}, nil
		}
		return e, nil, nil
	case ir.MessageDone:
		if f.phase != 2 {
			return fail()
		}
		f.phase = 3
		return e, nil, nil
	default:
		return nil, nil, fmt.Errorf("custom stream filter: unsupported event %T", ev)
	}
}

func (e *StreamEncoder) Apply(ev ir.Event) ([]*Chunk, []ir.Loss, error) {
	filtered, losses, err := e.customFilter.filter(ev)
	if err != nil {
		return nil, nil, err
	}
	if filtered == nil {
		return nil, losses, nil
	}
	out, other, err := e.apply(filtered)
	if err != nil {
		return nil, nil, err
	}
	return out, append(losses, other...), nil
}
