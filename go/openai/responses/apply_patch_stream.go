package responses

import (
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
)

type streamApplyPatchCall struct {
	id        string
	callID    string
	status    string
	operation *ApplyPatchOperationWire
}

type streamApplyPatchResult struct {
	id     string
	callID string
	status string
	output *string
}

func (d *StreamDecoder) replayApplyPatchCall(item *OutputItem) ([]ir.Event, error) {
	if item.CallID != d.applyPatchCall.callID {
		return nil, fmt.Errorf("responses: apply_patch_call item.done call_id mismatch")
	}
	status, statusLoss, err := decodeApplyPatchCallStatus(item.Status, fmt.Sprintf("output[%d]", d.outputIndex))
	if err != nil {
		return nil, err
	}
	if statusLoss != nil {
		d.losses = append(d.losses, *statusLoss)
		return nil, nil
	}
	op, opLoss, err := decodeApplyPatchOperation(item.Operation, fmt.Sprintf("output[%d].operation", d.outputIndex))
	if err != nil {
		return nil, err
	}
	if opLoss != nil {
		d.losses = append(d.losses, *opLoss)
		return nil, nil
	}
	d.toolUseSeen = true
	index := d.nextBlockIndex
	d.nextBlockIndex++
	return []ir.Event{
		ir.ContentBlockStart{
			Index: index,
			Block: ir.ApplyPatchToolUseBlock{
				ID:        item.CallID,
				Status:    status,
				Operation: op,
			},
		},
		ir.ContentBlockStop{Index: index},
	}, nil
}

func (d *StreamDecoder) replayApplyPatchResult(item *OutputItem) ([]ir.Event, error) {
	if item.CallID != d.applyPatchResult.callID {
		return nil, fmt.Errorf("responses: apply_patch_call_output item.done call_id mismatch")
	}
	status, statusLoss, err := decodeApplyPatchResultStatus(item.Status, fmt.Sprintf("output[%d]", d.outputIndex))
	if err != nil {
		return nil, err
	}
	if statusLoss != nil {
		d.losses = append(d.losses, *statusLoss)
		return nil, nil
	}
	var outputStr *string
	if item.Output != nil {
		s := *item.Output
		outputStr = &s
	}
	index := d.nextBlockIndex
	d.nextBlockIndex++
	return []ir.Event{
		ir.ContentBlockStart{
			Index: index,
			Block: ir.ApplyPatchToolResultBlock{
				ToolUseID: item.CallID,
				Status:    status,
				Output:    outputStr,
			},
		},
		ir.ContentBlockStop{Index: index},
	}, nil
}
