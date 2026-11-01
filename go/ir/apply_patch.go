package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const (
	ToolTypeApplyPatch            = "apply_patch"
	BlockTypeApplyPatchToolUse    = "apply_patch_tool_use"
	BlockTypeApplyPatchToolResult = "apply_patch_tool_result"
	ApplyPatchOperationCreateFile = "create_file"
	ApplyPatchOperationDeleteFile = "delete_file"
	ApplyPatchOperationUpdateFile = "update_file"
)

type ApplyPatchCallStatus string
type ApplyPatchResultStatus string

const (
	ApplyPatchCallInProgress  ApplyPatchCallStatus   = "in_progress"
	ApplyPatchCallCompleted   ApplyPatchCallStatus   = "completed"
	ApplyPatchResultCompleted ApplyPatchResultStatus = "completed"
	ApplyPatchResultFailed    ApplyPatchResultStatus = "failed"
)

// ApplyPatchOperation 的 Path/Diff 是不透明文本，不进行路径清理或补丁解析。
type ApplyPatchOperation interface{ isApplyPatchOperation() }
type ApplyPatchCreateFileOperation struct {
	Path string
	Diff string
}
type ApplyPatchDeleteFileOperation struct{ Path string }
type ApplyPatchUpdateFileOperation struct {
	Path string
	Diff string
}

func (ApplyPatchCreateFileOperation) isApplyPatchOperation() {}
func (ApplyPatchDeleteFileOperation) isApplyPatchOperation() {}
func (ApplyPatchUpdateFileOperation) isApplyPatchOperation() {}

type ApplyPatchToolUseBlock struct {
	ID        string
	Status    ApplyPatchCallStatus
	Operation ApplyPatchOperation
	Native    []NativeAnnotation
}
type ApplyPatchToolResultBlock struct {
	ToolUseID string
	Status    ApplyPatchResultStatus
	Output    *string
	Native    []NativeAnnotation
}

func (ApplyPatchToolUseBlock) isBlock()    {}
func (ApplyPatchToolResultBlock) isBlock() {}

func MarshalApplyPatchOperation(operation ApplyPatchOperation) (json.RawMessage, error) {
	var kind, path string
	var diff *string
	switch op := operation.(type) {
	case ApplyPatchCreateFileOperation:
		kind, path, diff = ApplyPatchOperationCreateFile, op.Path, &op.Diff
	case ApplyPatchDeleteFileOperation:
		kind, path = ApplyPatchOperationDeleteFile, op.Path
	case ApplyPatchUpdateFileOperation:
		kind, path, diff = ApplyPatchOperationUpdateFile, op.Path, &op.Diff
	default:
		return nil, fmt.Errorf("ir: invalid apply patch operation %T", operation)
	}
	return json.Marshal(struct {
		Type string  `json:"type"`
		Path string  `json:"path"`
		Diff *string `json:"diff,omitempty"`
	}{kind, path, diff})
}

func UnmarshalApplyPatchOperation(data []byte) (ApplyPatchOperation, error) {
	fields, err := applyPatchObject(data, "type", "path", "diff")
	if err != nil {
		return nil, err
	}
	kind, err := applyPatchString(fields, "type")
	if err != nil {
		return nil, err
	}
	path, err := applyPatchString(fields, "path")
	if err != nil {
		return nil, err
	}
	if kind == ApplyPatchOperationDeleteFile {
		if _, ok := fields["diff"]; ok {
			return nil, fmt.Errorf("ir: delete_file cannot carry diff")
		}
		return ApplyPatchDeleteFileOperation{Path: path}, nil
	}
	if kind != ApplyPatchOperationCreateFile && kind != ApplyPatchOperationUpdateFile {
		return nil, fmt.Errorf("ir: unknown apply patch operation %q", kind)
	}
	diff, err := applyPatchString(fields, "diff")
	if err != nil {
		return nil, err
	}
	if kind == ApplyPatchOperationCreateFile {
		return ApplyPatchCreateFileOperation{Path: path, Diff: diff}, nil
	}
	return ApplyPatchUpdateFileOperation{Path: path, Diff: diff}, nil
}

func marshalApplyPatchUse(b ApplyPatchToolUseBlock) (json.RawMessage, error) {
	if b.ID == "" || (b.Status != ApplyPatchCallInProgress && b.Status != ApplyPatchCallCompleted) {
		return nil, fmt.Errorf("ir: apply patch call requires nonempty id and valid call status")
	}
	operation, err := MarshalApplyPatchOperation(b.Operation)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Type      string               `json:"type"`
		ID        string               `json:"id"`
		Status    ApplyPatchCallStatus `json:"status"`
		Operation json.RawMessage      `json:"operation"`
	}{BlockTypeApplyPatchToolUse, b.ID, b.Status, operation})
}
func marshalApplyPatchResult(b ApplyPatchToolResultBlock) (json.RawMessage, error) {
	if b.ToolUseID == "" || (b.Status != ApplyPatchResultCompleted && b.Status != ApplyPatchResultFailed) {
		return nil, fmt.Errorf("ir: apply patch result requires nonempty tool_use_id and valid result status")
	}
	return json.Marshal(struct {
		Type   string                 `json:"type"`
		ID     string                 `json:"tool_use_id"`
		Status ApplyPatchResultStatus `json:"status"`
		Output *string                `json:"output,omitempty"`
	}{BlockTypeApplyPatchToolResult, b.ToolUseID, b.Status, b.Output})
}
func unmarshalApplyPatchBlock(data []byte, kind string) (Block, error) {
	allowed := []string{"type", "id", "status", "operation"}
	if kind == BlockTypeApplyPatchToolResult {
		allowed = []string{"type", "tool_use_id", "status", "output"}
	}
	fields, err := applyPatchObject(data, allowed...)
	if err != nil {
		return nil, err
	}
	status, err := applyPatchString(fields, "status")
	if err != nil {
		return nil, err
	}
	if kind == BlockTypeApplyPatchToolUse {
		id, err := applyPatchString(fields, "id")
		if err != nil {
			return nil, err
		}
		operation, err := UnmarshalApplyPatchOperation(fields["operation"])
		if err != nil {
			return nil, err
		}
		b := ApplyPatchToolUseBlock{ID: id, Status: ApplyPatchCallStatus(status), Operation: operation}
		_, err = marshalApplyPatchUse(b)
		return b, err
	}
	id, err := applyPatchString(fields, "tool_use_id")
	if err != nil {
		return nil, err
	}
	b := ApplyPatchToolResultBlock{ToolUseID: id, Status: ApplyPatchResultStatus(status)}
	if _, present := fields["output"]; present {
		value, err := applyPatchString(fields, "output")
		if err != nil {
			return nil, err
		}
		b.Output = &value
	}
	_, err = marshalApplyPatchResult(b)
	return b, err
}

func applyPatchObject(data []byte, allowed ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("ir: apply patch value must be an object")
	}
	for field := range fields {
		valid := false
		for _, name := range allowed {
			if field == name {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("ir: unexpected apply patch property %q", field)
		}
	}
	return fields, nil
}
func applyPatchString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '"' {
		return "", fmt.Errorf("ir: apply patch %s must be a present string", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

func (w *wireTool) UnmarshalJSON(data []byte) error {
	var head struct {
		Type   string          `json:"type"`
		Strict json.RawMessage `json:"strict"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(head.Strict), []byte("null")) {
		return fmt.Errorf("ir: tool strict must be a boolean")
	}
	if head.Type == ToolTypeApplyPatch {
		if _, err := applyPatchObject(data, "type"); err != nil {
			return err
		}
	}
	type plain wireTool
	return json.Unmarshal(data, (*plain)(w))
}
func (w *wireToolChoice) UnmarshalJSON(data []byte) error {
	var head struct {
		Type string `json:"tool_type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	if head.Type == ToolTypeApplyPatch {
		if _, err := applyPatchObject(data, "mode", "tool_type", "disable_parallel"); err != nil {
			return err
		}
	}
	type plain wireToolChoice
	return json.Unmarshal(data, (*plain)(w))
}
