package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

type streamCustomCall struct {
	callID    string
	name      string
	initial   string
	fragments []string
	doneInput *string
}

func (c *streamCustomCall) checkInput(input string) error {
	if len(c.fragments) > 0 {
		if c.initial+strings.Join(c.fragments, "") != input {
			return fmt.Errorf("responses: custom input fragments disagree with complete input")
		}
	} else if c.initial != "" && c.initial != input {
		return fmt.Errorf("responses: custom initial input disagrees with complete input")
	}
	return nil
}

func (d *StreamDecoder) feedCustomInput(ev *StreamEvent) ([]ir.Event, error) {
	if err := d.requireActiveItem(ev, ev.Type); err != nil {
		return nil, err
	}
	if d.skippedItem {
		return nil, nil
	}
	c := d.customCall
	if c == nil {
		return nil, fmt.Errorf("responses: custom input event without custom call")
	}
	if c.doneInput != nil {
		return nil, fmt.Errorf("responses: custom input event after input.done")
	}
	if ev.Type == EventTypeResponseCustomToolInputDelta {
		c.fragments = append(c.fragments, ev.Delta)
		return nil, nil
	}
	if ev.Input == nil {
		return nil, fmt.Errorf("responses: custom input.done requires input")
	}
	if err := c.checkInput(*ev.Input); err != nil {
		return nil, err
	}
	input := *ev.Input
	c.doneInput = &input
	return nil, nil
}

func (d *StreamDecoder) replayCustomCall(item *OutputItem) ([]ir.Event, error) {
	c := d.customCall
	if item.CallID != c.callID || item.Name != c.name || item.Input == nil {
		return nil, fmt.Errorf("responses: custom item.done identity/input mismatch")
	}
	if err := c.checkInput(*item.Input); err != nil {
		return nil, err
	}
	if c.doneInput != nil && *c.doneInput != *item.Input {
		return nil, fmt.Errorf("responses: custom done snapshots disagree")
	}
	input, _ := json.Marshal(*item.Input)
	index := d.nextBlockIndex
	fragments := c.fragments
	if len(fragments) == 0 {
		fragments = []string{*item.Input}
	} else if c.initial != "" {
		fragments = append([]string{c.initial}, fragments...)
	}
	events := []ir.Event{ir.ContentBlockStart{Index: index, Block: ir.CustomToolUseBlock{ID: c.callID, Name: c.name, Input: input}}}
	for _, fragment := range fragments {
		token, _ := json.Marshal(fragment)
		events = append(events, ir.ContentBlockDelta{Index: index, Delta: ir.CustomToolInputDelta{Text: token}})
	}
	events = append(events, ir.ContentBlockStop{Index: index})
	d.nextBlockIndex++
	return events, nil
}
