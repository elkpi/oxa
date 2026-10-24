package responses

import (
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v2/ir"
)

func (e *StreamEncoder) startCustomCallBlock(index int, block ir.CustomToolUseBlock) ([]*StreamEvent, []ir.Loss, error) {
	if block.ID == "" || block.Name == "" {
		return nil, nil, fmt.Errorf("responses: custom call requires id/name")
	}
	input, err := unwrapStreamIRString(block.Input)
	if err != nil {
		return nil, nil, err
	}
	var out []*StreamEvent
	if e.activeItem != nil {
		switch e.activeItem.kind {
		case streamMessageOutputItem:
			out = append(out, e.closeMessageItem())
		case streamReasoningOutputItem:
			out = append(out, e.closeReasoningItem())
		default:
			return nil, nil, fmt.Errorf("responses: custom block before prior call completed")
		}
	}
	rawName := block.Name
	namespace := ""
	if dotIdx := strings.Index(block.Name, "."); dotIdx > 0 {
		namespace = block.Name[:dotIdx]
		rawName = block.Name[dotIdx+1:]
	}
	item := &streamOutputItem{kind: streamCustomCallOutputItem, id: fmt.Sprintf("ctc_%d", e.nextCustomItem), outputIndex: e.nextOutputIndex, callID: block.ID, name: rawName, namespace: namespace}
	e.nextCustomItem++
	e.nextOutputIndex++
	e.activeItem = item
	e.activeBlock = &streamEncodeBlock{index: index, kind: streamCustomCallOutputItem, toolInput: input}
	empty := ""
	out = append(out, &StreamEvent{Type: EventTypeResponseOutputItemAdded, OutputIndex: item.outputIndex, Item: &OutputItem{Type: ItemTypeCustomToolCall, ID: item.id, CallID: block.ID, Name: rawName, Namespace: namespace, Input: &empty}})
	return out, nil, nil
}

func (e *StreamEncoder) stopCustomCallBlock() ([]*StreamEvent, []ir.Loss, error) {
	block, item := e.activeBlock, e.activeItem
	if item == nil || item.kind != streamCustomCallOutputItem {
		return nil, nil, fmt.Errorf("responses: custom block without active custom item")
	}
	var out []*StreamEvent
	if len(block.fragments) == 0 {
		block.fragments = []string{block.toolInput}
		out = append(out, &StreamEvent{Type: EventTypeResponseCustomToolInputDelta, ItemID: item.id, OutputIndex: item.outputIndex, Delta: block.toolInput})
	}
	input := strings.Join(block.fragments, "")
	if input != block.toolInput {
		return nil, nil, fmt.Errorf("responses: custom input does not equal concatenated fragments")
	}
	completed := OutputItem{Type: ItemTypeCustomToolCall, ID: item.id, CallID: item.callID, Name: item.name, Namespace: item.namespace, Input: &input}
	out = append(out, &StreamEvent{Type: EventTypeResponseCustomToolInputDone, ItemID: item.id, OutputIndex: item.outputIndex, Input: &input}, &StreamEvent{Type: EventTypeResponseOutputItemDone, OutputIndex: item.outputIndex, Item: &completed})
	e.completed = append(e.completed, completed)
	e.activeBlock = nil
	e.activeItem = nil
	return out, nil, nil
}
