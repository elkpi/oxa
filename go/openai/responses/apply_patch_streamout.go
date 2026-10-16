package responses

import (
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
)

func (e *StreamEncoder) startApplyPatchCallBlock(index int, block ir.ApplyPatchToolUseBlock) ([]*StreamEvent, []ir.Loss, error) {
	if block.ID == "" || block.Operation == nil {
		return nil, nil, fmt.Errorf("responses: apply_patch_call requires id and operation")
	}
	var out []*StreamEvent
	if e.activeItem != nil {
		switch e.activeItem.kind {
		case streamMessageOutputItem:
			out = append(out, e.closeMessageItem())
		case streamReasoningOutputItem:
			out = append(out, e.closeReasoningItem())
		default:
			return nil, nil, fmt.Errorf("responses: apply_patch_call block before prior item completed")
		}
	}
	opWire, err := encodeApplyPatchOperation(block.Operation)
	if err != nil {
		return nil, nil, err
	}
	itemID := fmt.Sprintf("apc_%d", e.nextApplyPatchCallItem)
	e.nextApplyPatchCallItem++
	outputIndex := e.nextOutputIndex
	e.nextOutputIndex++
	item := &streamOutputItem{
		kind:        streamApplyPatchCallOutputItem,
		id:          itemID,
		outputIndex: outputIndex,
		callID:      block.ID,
	}
	e.activeItem = item
	bCopy := block
	e.activeBlock = &streamEncodeBlock{
		index:          index,
		kind:           streamApplyPatchCallOutputItem,
		applyPatchCall: &bCopy,
	}
	added := &StreamEvent{
		Type:        EventTypeResponseOutputItemAdded,
		OutputIndex: outputIndex,
		Item: &OutputItem{
			Type:      ItemTypeApplyPatchCall,
			ID:        itemID,
			CallID:    block.ID,
			Status:    StatusInProgress,
			Operation: opWire,
		},
	}
	out = append(out, added)
	return out, nil, nil
}

func (e *StreamEncoder) stopApplyPatchCallBlock() ([]*StreamEvent, []ir.Loss, error) {
	block, item := e.activeBlock, e.activeItem
	if item == nil || item.kind != streamApplyPatchCallOutputItem {
		return nil, nil, fmt.Errorf("responses: apply_patch block without active item")
	}
	opWire, err := encodeApplyPatchOperation(block.applyPatchCall.Operation)
	if err != nil {
		return nil, nil, err
	}
	completed := OutputItem{
		Type:      ItemTypeApplyPatchCall,
		ID:        item.id,
		CallID:    item.callID,
		Status:    string(block.applyPatchCall.Status),
		Operation: opWire,
	}
	done := &StreamEvent{
		Type:        EventTypeResponseOutputItemDone,
		OutputIndex: item.outputIndex,
		Item:        &completed,
	}
	e.completed = append(e.completed, completed)
	e.activeBlock = nil
	e.activeItem = nil
	return []*StreamEvent{done}, nil, nil
}

func (e *StreamEncoder) startApplyPatchResultBlock(index int, block ir.ApplyPatchToolResultBlock) ([]*StreamEvent, []ir.Loss, error) {
	if block.ToolUseID == "" {
		return nil, nil, fmt.Errorf("responses: apply_patch_call_output requires tool_use_id")
	}
	var out []*StreamEvent
	if e.activeItem != nil {
		switch e.activeItem.kind {
		case streamMessageOutputItem:
			out = append(out, e.closeMessageItem())
		case streamReasoningOutputItem:
			out = append(out, e.closeReasoningItem())
		default:
			return nil, nil, fmt.Errorf("responses: apply_patch_call_output block before prior item completed")
		}
	}
	itemID := fmt.Sprintf("apco_%d", e.nextApplyPatchResultItem)
	e.nextApplyPatchResultItem++
	outputIndex := e.nextOutputIndex
	e.nextOutputIndex++
	item := &streamOutputItem{
		kind:        streamApplyPatchResultOutputItem,
		id:          itemID,
		outputIndex: outputIndex,
		callID:      block.ToolUseID,
	}
	e.activeItem = item
	bCopy := block
	e.activeBlock = &streamEncodeBlock{
		index:            index,
		kind:             streamApplyPatchResultOutputItem,
		applyPatchResult: &bCopy,
	}
	addedItem := OutputItem{
		Type:   ItemTypeApplyPatchCallOutput,
		ID:     itemID,
		CallID: block.ToolUseID,
		Status: string(block.Status),
	}
	if block.Output != nil {
		addedItem.Output = block.Output
	}
	added := &StreamEvent{
		Type:        EventTypeResponseOutputItemAdded,
		OutputIndex: outputIndex,
		Item:        &addedItem,
	}
	out = append(out, added)
	return out, nil, nil
}

func (e *StreamEncoder) stopApplyPatchResultBlock() ([]*StreamEvent, []ir.Loss, error) {
	block, item := e.activeBlock, e.activeItem
	if item == nil || item.kind != streamApplyPatchResultOutputItem {
		return nil, nil, fmt.Errorf("responses: apply_patch_result block without active item")
	}
	completed := OutputItem{
		Type:   ItemTypeApplyPatchCallOutput,
		ID:     item.id,
		CallID: item.callID,
		Status: string(block.applyPatchResult.Status),
	}
	if block.applyPatchResult.Output != nil {
		completed.Output = block.applyPatchResult.Output
	}
	done := &StreamEvent{
		Type:        EventTypeResponseOutputItemDone,
		OutputIndex: item.outputIndex,
		Item:        &completed,
	}
	e.completed = append(e.completed, completed)
	e.activeBlock = nil
	e.activeItem = nil
	return []*StreamEvent{done}, nil, nil
}
