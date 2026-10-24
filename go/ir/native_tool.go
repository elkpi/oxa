package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func ValidateNativeTool(tool Tool) error {
	if tool.Type != ToolTypeNative {
		if tool.Source != "" || tool.Definition != "" {
			return fmt.Errorf("ir: only native tools may carry source/definition")
		}
		return nil
	}
	if tool.Name == "" || tool.Source == "" || tool.Description != "" || len(tool.InputSchema) != 0 || tool.Format != nil || len(tool.Native) != 0 {
		return fmt.Errorf("ir: native tool requires name/source/definition and cannot carry other tool configuration")
	}
	raw := bytes.TrimSpace([]byte(tool.Definition))
	if len(raw) == 0 || raw[0] != '{' {
		return fmt.Errorf("ir: native tool definition must be a JSON object")
	}
	var header struct {
		Type        string `json:"type"`
		Name        string `json:"name"`
		ServerLabel string `json:"server_label"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return fmt.Errorf("ir: native tool definition: %w", err)
	}
	expectedName := header.Name
	if expectedName == "" {
		expectedName = header.ServerLabel
	}
	if expectedName == "" {
		expectedName = header.Type
	}
	if header.Type == "" || header.Type == ToolTypeFunction || header.Type == ToolTypeCustom || expectedName != tool.Name {
		return fmt.Errorf("ir: native tool definition requires a native type and matching name")
	}
	return nil
}

func ValidateNativeTools(req *Request) error {
	for i, tool := range req.Tools {
		if err := ValidateNativeTool(tool); err != nil {
			return fmt.Errorf("tools[%d]: %w", i, err)
		}
	}
	if req.ToolChoice != nil && req.ToolChoice.ToolType == ToolTypeNative {
		return validateToolChoice(req.ToolChoice)
	}
	return nil
}

func IsNativeToolChoice(req *Request) bool {
	choice := req.ToolChoice
	if choice == nil || choice.Mode != ToolChoiceTool {
		return false
	}
	if choice.ToolType == ToolTypeNative {
		return true
	}
	for _, tool := range req.Tools {
		if tool.Type == ToolTypeNative && tool.Name == choice.Name {
			return true
		}
	}
	return false
}

func NativeToolLoss(tool Tool, path string) Loss {
	return Loss{Path: path, Field: "type", Reason: LossUnsupportedSemantic,
		Detail: fmt.Sprintf("native tool %q from %q cannot be represented by the target protocol", tool.Name, tool.Source)}
}

func NativeToolChoiceLoss(choice *ToolChoice) Loss {
	return Loss{Path: "tool_choice", Field: "tool_choice", Reason: LossUnsupportedSemantic,
		Detail: fmt.Sprintf("native tool choice %q cannot be represented by the target protocol", choice.Name)}
}
