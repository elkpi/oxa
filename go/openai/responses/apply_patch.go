package responses

import (
	"fmt"

	"github.com/elkpi/oxa/go/v2/ir"
)

func decodeApplyPatchOperation(wire *ApplyPatchOperationWire, path string) (ir.ApplyPatchOperation, *ir.Loss, error) {
	if wire == nil {
		return nil, ptrLoss(loss(path, "operation", ir.LossUnsupportedSemantic, "operation is required")), nil
	}
	switch wire.Type {
	case ir.ApplyPatchOperationCreateFile:
		if wire.Diff == nil {
			return nil, nil, fmt.Errorf("responses: %s.diff is required for create_file", path)
		}
		return ir.ApplyPatchCreateFileOperation{Path: wire.Path, Diff: *wire.Diff}, nil, nil
	case ir.ApplyPatchOperationDeleteFile:
		return ir.ApplyPatchDeleteFileOperation{Path: wire.Path}, nil, nil
	case ir.ApplyPatchOperationUpdateFile:
		if wire.Diff == nil {
			return nil, nil, fmt.Errorf("responses: %s.diff is required for update_file", path)
		}
		return ir.ApplyPatchUpdateFileOperation{Path: wire.Path, Diff: *wire.Diff}, nil, nil
	default:
		return nil, ptrLoss(loss(path, "operation", ir.LossUnsupportedSemantic, fmt.Sprintf("unsupported operation type %q", wire.Type))), nil
	}
}

func encodeApplyPatchOperation(op ir.ApplyPatchOperation) (*ApplyPatchOperationWire, error) {
	switch v := op.(type) {
	case ir.ApplyPatchCreateFileOperation:
		diff := v.Diff
		return &ApplyPatchOperationWire{Type: ir.ApplyPatchOperationCreateFile, Path: v.Path, Diff: &diff}, nil
	case ir.ApplyPatchDeleteFileOperation:
		return &ApplyPatchOperationWire{Type: ir.ApplyPatchOperationDeleteFile, Path: v.Path}, nil
	case ir.ApplyPatchUpdateFileOperation:
		diff := v.Diff
		return &ApplyPatchOperationWire{Type: ir.ApplyPatchOperationUpdateFile, Path: v.Path, Diff: &diff}, nil
	default:
		return nil, fmt.Errorf("responses: unknown apply patch operation %T", op)
	}
}

func decodeApplyPatchCallStatus(status string, path string) (ir.ApplyPatchCallStatus, *ir.Loss, error) {
	switch status {
	case string(ir.ApplyPatchCallInProgress), string(ir.ApplyPatchCallCompleted):
		return ir.ApplyPatchCallStatus(status), nil, nil
	default:
		return "", ptrLoss(loss(path+".status", "status", ir.LossUnmappedValue, fmt.Sprintf("unknown apply_patch_call status %q", status))), nil
	}
}

func decodeApplyPatchResultStatus(status string, path string) (ir.ApplyPatchResultStatus, *ir.Loss, error) {
	switch status {
	case string(ir.ApplyPatchResultCompleted), string(ir.ApplyPatchResultFailed):
		return ir.ApplyPatchResultStatus(status), nil, nil
	default:
		return "", ptrLoss(loss(path+".status", "status", ir.LossUnmappedValue, fmt.Sprintf("unknown apply_patch_call_output status %q", status))), nil
	}
}

func decodeApplyPatchResultOutput(output any, path string) (*string, error) {
	if output == nil {
		return nil, nil
	}
	switch v := output.(type) {
	case *string:
		return v, nil
	case string:
		return &v, nil
	default:
		return nil, fmt.Errorf("responses: %s.output must be a string", path)
	}
}
