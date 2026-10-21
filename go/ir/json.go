package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// SpecVersion is the spec version stamped on every IR document on marshal and
// const-checked on unmarshal. It mirrors the specVersion const pinned by
// spec/schema/ir.schema.json; the schema-alignment test in json_test.go
// verifies the agreement.
const SpecVersion = "0.2.0"

// ---- blocks ---------------------------------------------------------------

type wireTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireImageBlock struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type wireToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type wireToolResultBlock struct {
	Type      string            `json:"type"`
	ToolUseID string            `json:"tool_use_id"`
	Content   []json.RawMessage `json:"content"`
	IsError   bool              `json:"is_error,omitempty"`
}

type wireThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

func hasNative(b Block) bool {
	switch v := b.(type) {
	case TextBlock:
		return len(v.Native) > 0
	case ThinkingBlock:
		return len(v.Native) > 0
	case ImageBlock:
		return len(v.Native) > 0
	case ToolUseBlock:
		return len(v.Native) > 0
	case ToolResultBlock:
		if len(v.Native) > 0 {
			return true
		}
		for _, c := range v.Content {
			if hasNative(c) {
				return true
			}
		}
	case CustomToolUseBlock:
		return len(v.Native) > 0
	case CustomToolResultBlock:
		if len(v.Native) > 0 {
			return true
		}
		for _, c := range v.Output.Content {
			if hasNative(c) {
				return true
			}
		}
	case ApplyPatchToolUseBlock:
		return len(v.Native) > 0
	case ApplyPatchToolResultBlock:
		return len(v.Native) > 0
	}
	return false
}

// MarshalBlock renders one block in its canonical type-discriminated form.
func MarshalBlock(b Block) (json.RawMessage, error) {
	if hasNative(b) {
		return nil, fmt.Errorf("ir: native annotations are forbidden in response and event content blocks (spec/15)")
	}
	switch v := b.(type) {
	case ApplyPatchToolUseBlock:
		return marshalApplyPatchUse(v)
	case ApplyPatchToolResultBlock:
		return marshalApplyPatchResult(v)
	case CustomToolUseBlock:
		if v.ID == "" || v.Name == "" {
			return nil, fmt.Errorf("ir: custom call requires id/name")
		}
		if err := validateOpaqueString(v.Input); err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Type string `json:"type"`
			CustomToolUseBlock
		}{BlockTypeCustomToolUse, v})
	case CustomToolResultBlock:
		if v.ToolUseID == "" {
			return nil, fmt.Errorf("ir: custom result requires tool_use_id")
		}
		return json.Marshal(struct {
			Type string `json:"type"`
			CustomToolResultBlock
		}{BlockTypeCustomToolResult, v})
	case TextBlock:
		return json.Marshal(wireTextBlock{Type: BlockTypeText, Text: v.Text})
	case ThinkingBlock:
		return json.Marshal(wireThinkingBlock{
			Type: BlockTypeThinking, Thinking: v.Thinking, Signature: v.Signature,
		})
	case ImageBlock:
		return json.Marshal(wireImageBlock{
			Type: BlockTypeImage, MediaType: v.MediaType, Data: v.Data, URL: v.URL,
		})
	case ToolUseBlock:
		if len(v.Input) == 0 {
			return nil, fmt.Errorf("ir: tool_use input is required (INV-1)")
		}
		return json.Marshal(wireToolUseBlock{
			Type: BlockTypeToolUse, ID: v.ID, Name: v.Name, Input: v.Input,
		})
	case ToolResultBlock:
		content, err := marshalBlocks(v.Content)
		if err != nil {
			return nil, err
		}
		return json.Marshal(wireToolResultBlock{
			Type: BlockTypeToolResult, ToolUseID: v.ToolUseID, Content: content, IsError: v.IsError,
		})
	default:
		return nil, fmt.Errorf("ir: unknown block type %T", b)
	}
}

func marshalBlocks(blocks []Block) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(blocks))
	for i, b := range blocks {
		raw, err := MarshalBlock(b)
		if err != nil {
			return nil, fmt.Errorf("block[%d]: %w", i, err)
		}
		out = append(out, raw)
	}
	return out, nil
}

// UnmarshalBlock parses one type-discriminated block. The input of a
// tool_use block and the partial_json of an input_json_delta are kept as the
// exact source JSON string token, so decode/encode round-trips are
// byte-faithful (INV-1).
func UnmarshalBlock(data []byte) (Block, error) {
	var natCheck struct {
		Native []json.RawMessage `json:"native"`
	}
	if err := json.Unmarshal(data, &natCheck); err == nil && len(natCheck.Native) > 0 {
		return nil, fmt.Errorf("ir: native annotations are forbidden in response and event content blocks (spec/15)")
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case BlockTypeApplyPatchToolUse, BlockTypeApplyPatchToolResult:
		return unmarshalApplyPatchBlock(data, head.Type)
	case BlockTypeCustomToolUse:
		var b CustomToolUseBlock
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		if b.ID == "" || b.Name == "" {
			return nil, fmt.Errorf("ir: custom call requires id/name")
		}
		if err := validateOpaqueString(b.Input); err != nil {
			return nil, err
		}
		return b, nil
	case BlockTypeCustomToolResult:
		var w struct {
			ToolUseID string          `json:"tool_use_id"`
			Output    json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if w.ToolUseID == "" {
			return nil, fmt.Errorf("ir: custom result requires tool_use_id")
		}
		var output CustomToolOutput
		if err := output.UnmarshalJSON(w.Output); err != nil {
			return nil, err
		}
		return CustomToolResultBlock{ToolUseID: w.ToolUseID, Output: output}, nil
	case BlockTypeText:
		var w wireTextBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return TextBlock{Text: w.Text}, nil
	case BlockTypeThinking:
		var w wireThinkingBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return ThinkingBlock{Thinking: w.Thinking, Signature: w.Signature}, nil
	case BlockTypeImage:
		var w wireImageBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return ImageBlock{MediaType: w.MediaType, Data: w.Data, URL: w.URL}, nil
	case BlockTypeToolUse:
		var w wireToolUseBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if len(w.Input) == 0 {
			return nil, fmt.Errorf("ir: tool_use input is required (INV-1)")
		}
		return ToolUseBlock{ID: w.ID, Name: w.Name, Input: w.Input}, nil
	case BlockTypeToolResult:
		var w wireToolResultBlock
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		content := make([]Block, 0, len(w.Content))
		for i, raw := range w.Content {
			b, err := UnmarshalBlock(raw)
			if err != nil {
				return nil, fmt.Errorf("content[%d]: %w", i, err)
			}
			content = append(content, b)
		}
		return ToolResultBlock{
			ToolUseID: w.ToolUseID, Content: content, IsError: w.IsError,
		}, nil
	default:
		return nil, fmt.Errorf("ir: unknown block type %q", head.Type)
	}
}

func unmarshalBlocks(raws []json.RawMessage) ([]Block, error) {
	out := make([]Block, 0, len(raws))
	for i, raw := range raws {
		b, err := UnmarshalBlock(raw)
		if err != nil {
			return nil, fmt.Errorf("content[%d]: %w", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

type wireSystemBlock struct {
	Type   string             `json:"type"`
	Text   string             `json:"text"`
	Native []NativeAnnotation `json:"native,omitempty"`
}

func marshalRequestApplyPatchUse(b ApplyPatchToolUseBlock) (json.RawMessage, error) {
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
		Native    []NativeAnnotation   `json:"native,omitempty"`
	}{BlockTypeApplyPatchToolUse, b.ID, b.Status, operation, b.Native})
}

func marshalRequestApplyPatchResult(b ApplyPatchToolResultBlock) (json.RawMessage, error) {
	if b.ToolUseID == "" || (b.Status != ApplyPatchResultCompleted && b.Status != ApplyPatchResultFailed) {
		return nil, fmt.Errorf("ir: apply patch result requires nonempty tool_use_id and valid result status")
	}
	return json.Marshal(struct {
		Type      string                 `json:"type"`
		ToolUseID string                 `json:"tool_use_id"`
		Status    ApplyPatchResultStatus `json:"status"`
		Output    *string                `json:"output,omitempty"`
		Native    []NativeAnnotation     `json:"native,omitempty"`
	}{BlockTypeApplyPatchToolResult, b.ToolUseID, b.Status, b.Output, b.Native})
}

func marshalRequestBlock(b Block) (json.RawMessage, error) {
	switch v := b.(type) {
	case ApplyPatchToolUseBlock:
		return marshalRequestApplyPatchUse(v)
	case ApplyPatchToolResultBlock:
		return marshalRequestApplyPatchResult(v)
	case CustomToolUseBlock:
		if v.ID == "" || v.Name == "" {
			return nil, fmt.Errorf("ir: custom call requires id/name")
		}
		if err := validateOpaqueString(v.Input); err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Type string `json:"type"`
			CustomToolUseBlock
		}{BlockTypeCustomToolUse, v})
	case CustomToolResultBlock:
		if v.ToolUseID == "" {
			return nil, fmt.Errorf("ir: custom result requires tool_use_id")
		}
		var outputRaw json.RawMessage
		if v.Output.Text != nil {
			b, err := json.Marshal(*v.Output.Text)
			if err != nil {
				return nil, err
			}
			outputRaw = b
		} else {
			raws, err := marshalRequestBlocks(v.Output.Content)
			if err != nil {
				return nil, err
			}
			b, err := json.Marshal(raws)
			if err != nil {
				return nil, err
			}
			outputRaw = b
		}
		return json.Marshal(struct {
			Type      string             `json:"type"`
			ToolUseID string             `json:"tool_use_id"`
			Output    json.RawMessage    `json:"output"`
			Native    []NativeAnnotation `json:"native,omitempty"`
		}{BlockTypeCustomToolResult, v.ToolUseID, outputRaw, v.Native})
	case TextBlock:
		return json.Marshal(struct {
			Type   string             `json:"type"`
			Text   string             `json:"text"`
			Native []NativeAnnotation `json:"native,omitempty"`
		}{BlockTypeText, v.Text, v.Native})
	case ThinkingBlock:
		return json.Marshal(struct {
			Type      string             `json:"type"`
			Thinking  string             `json:"thinking"`
			Signature string             `json:"signature,omitempty"`
			Native    []NativeAnnotation `json:"native,omitempty"`
		}{BlockTypeThinking, v.Thinking, v.Signature, v.Native})
	case ImageBlock:
		return json.Marshal(struct {
			Type      string             `json:"type"`
			MediaType string             `json:"media_type,omitempty"`
			Data      string             `json:"data,omitempty"`
			URL       string             `json:"url,omitempty"`
			Native    []NativeAnnotation `json:"native,omitempty"`
		}{BlockTypeImage, v.MediaType, v.Data, v.URL, v.Native})
	case ToolUseBlock:
		if len(v.Input) == 0 {
			return nil, fmt.Errorf("ir: tool_use input is required (INV-1)")
		}
		return json.Marshal(struct {
			Type   string             `json:"type"`
			ID     string             `json:"id"`
			Name   string             `json:"name"`
			Input  json.RawMessage    `json:"input"`
			Native []NativeAnnotation `json:"native,omitempty"`
		}{BlockTypeToolUse, v.ID, v.Name, v.Input, v.Native})
	case ToolResultBlock:
		content, err := marshalRequestBlocks(v.Content)
		if err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Type      string             `json:"type"`
			ToolUseID string             `json:"tool_use_id"`
			Content   []json.RawMessage  `json:"content"`
			IsError   bool               `json:"is_error,omitempty"`
			Native    []NativeAnnotation `json:"native,omitempty"`
		}{BlockTypeToolResult, v.ToolUseID, content, v.IsError, v.Native})
	default:
		return nil, fmt.Errorf("ir: unknown block type %T", b)
	}
}

func marshalRequestBlocks(blocks []Block) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(blocks))
	for i, b := range blocks {
		raw, err := marshalRequestBlock(b)
		if err != nil {
			return nil, fmt.Errorf("block[%d]: %w", i, err)
		}
		out = append(out, raw)
	}
	return out, nil
}

func unmarshalRequestBlock(data []byte) (Block, error) {
	var head struct {
		Type   string             `json:"type"`
		Native []NativeAnnotation `json:"native,omitempty"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case BlockTypeApplyPatchToolUse, BlockTypeApplyPatchToolResult:
		b, err := unmarshalApplyPatchBlock(data, head.Type)
		if err != nil {
			return nil, err
		}
		if len(head.Native) > 0 {
			switch v := b.(type) {
			case ApplyPatchToolUseBlock:
				v.Native = head.Native
				return v, nil
			case ApplyPatchToolResultBlock:
				v.Native = head.Native
				return v, nil
			}
		}
		return b, nil
	case BlockTypeCustomToolUse:
		var b CustomToolUseBlock
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		if b.ID == "" || b.Name == "" {
			return nil, fmt.Errorf("ir: custom call requires id/name")
		}
		if err := validateOpaqueString(b.Input); err != nil {
			return nil, err
		}
		b.Native = head.Native
		return b, nil
	case BlockTypeCustomToolResult:
		var w struct {
			ToolUseID string             `json:"tool_use_id"`
			Output    json.RawMessage    `json:"output"`
			Native    []NativeAnnotation `json:"native,omitempty"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if w.ToolUseID == "" {
			return nil, fmt.Errorf("ir: custom result requires tool_use_id")
		}
		var output CustomToolOutput
		trimmed := bytes.TrimSpace(w.Output)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			var text string
			if err := json.Unmarshal(trimmed, &text); err != nil {
				return nil, err
			}
			output.Text = &text
		} else if len(trimmed) > 0 && trimmed[0] == '[' {
			var raws []json.RawMessage
			if err := json.Unmarshal(trimmed, &raws); err != nil {
				return nil, err
			}
			content, err := unmarshalRequestBlocks(raws)
			if err != nil {
				return nil, err
			}
			output.Content = content
		} else {
			return nil, fmt.Errorf("ir: custom output must be a string or content list")
		}
		return CustomToolResultBlock{ToolUseID: w.ToolUseID, Output: output, Native: w.Native}, nil
	case BlockTypeText:
		var w struct {
			Text   string             `json:"text"`
			Native []NativeAnnotation `json:"native,omitempty"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return TextBlock{Text: w.Text, Native: w.Native}, nil
	case BlockTypeThinking:
		var w struct {
			Thinking  string             `json:"thinking"`
			Signature string             `json:"signature,omitempty"`
			Native    []NativeAnnotation `json:"native,omitempty"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return ThinkingBlock{Thinking: w.Thinking, Signature: w.Signature, Native: w.Native}, nil
	case BlockTypeImage:
		var w struct {
			MediaType string             `json:"media_type,omitempty"`
			Data      string             `json:"data,omitempty"`
			URL       string             `json:"url,omitempty"`
			Native    []NativeAnnotation `json:"native,omitempty"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return ImageBlock{MediaType: w.MediaType, Data: w.Data, URL: w.URL, Native: w.Native}, nil
	case BlockTypeToolUse:
		var w struct {
			ID     string             `json:"id"`
			Name   string             `json:"name"`
			Input  json.RawMessage    `json:"input"`
			Native []NativeAnnotation `json:"native,omitempty"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if len(w.Input) == 0 {
			return nil, fmt.Errorf("ir: tool_use input is required (INV-1)")
		}
		return ToolUseBlock{ID: w.ID, Name: w.Name, Input: w.Input, Native: w.Native}, nil
	case BlockTypeToolResult:
		var w struct {
			ToolUseID string             `json:"tool_use_id"`
			Content   []json.RawMessage  `json:"content"`
			IsError   bool               `json:"is_error,omitempty"`
			Native    []NativeAnnotation `json:"native,omitempty"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		content := make([]Block, 0, len(w.Content))
		for i, raw := range w.Content {
			b, err := unmarshalRequestBlock(raw)
			if err != nil {
				return nil, fmt.Errorf("content[%d]: %w", i, err)
			}
			content = append(content, b)
		}
		return ToolResultBlock{
			ToolUseID: w.ToolUseID, Content: content, IsError: w.IsError, Native: w.Native,
		}, nil
	default:
		return nil, fmt.Errorf("ir: unknown block type %q", head.Type)
	}
}

func unmarshalRequestBlocks(raws []json.RawMessage) ([]Block, error) {
	out := make([]Block, 0, len(raws))
	for i, raw := range raws {
		b, err := unmarshalRequestBlock(raw)
		if err != nil {
			return nil, fmt.Errorf("block[%d]: %w", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

// ---- request document ------------------------------------------------------

type wireRequest struct {
	SpecVersion string             `json:"specVersion"`
	Model       string             `json:"model"`
	System      []wireSystemBlock  `json:"system,omitempty"`
	Messages    []wireMessage      `json:"messages"`
	Tools       []wireTool         `json:"tools,omitempty"`
	ToolChoice  *wireToolChoice    `json:"tool_choice,omitempty"`
	Params      *wireParams        `json:"params,omitempty"`
	Metadata    map[string]string  `json:"metadata,omitempty"`
	Native      []NativeAnnotation `json:"native,omitempty"`
}

type wireMessage struct {
	Role    Role              `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type wireTool struct {
	Name        string             `json:"name,omitempty"`
	Description string             `json:"description,omitempty"`
	InputSchema json.RawMessage    `json:"input_schema,omitempty"`
	Type        string             `json:"type,omitempty"`
	Format      *CustomToolFormat  `json:"format,omitempty"`
	Native      []NativeAnnotation `json:"native,omitempty"`
	Source      string             `json:"source,omitempty"`
	Definition  string             `json:"definition,omitempty"`
}

type wireToolChoice struct {
	Mode     string `json:"mode"`
	Name     string `json:"name,omitempty"`
	ToolType string `json:"tool_type,omitempty"`
}

type wireResponseFormat struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type wireParams struct {
	Temperature     *float64            `json:"temperature,omitempty"`
	TopP            *float64            `json:"top_p,omitempty"`
	MaxTokens       *int64              `json:"max_tokens,omitempty"`
	StopSequences   []string            `json:"stop_sequences,omitempty"`
	ReasoningEffort string              `json:"reasoning_effort,omitempty"`
	ResponseFormat  *wireResponseFormat `json:"response_format,omitempty"`
}

func validateWireResponseFormat(rf *wireResponseFormat) error {
	if rf == nil {
		return nil
	}
	switch rf.Type {
	case "text", "json_object":
		return nil
	case "json_schema":
		if rf.Name == "" {
			return fmt.Errorf("params.response_format json_schema requires non-empty name")
		}
		if len(rf.Schema) == 0 {
			return fmt.Errorf("params.response_format json_schema requires schema")
		}
		var schema map[string]json.RawMessage
		if err := json.Unmarshal(rf.Schema, &schema); err != nil || schema == nil {
			return fmt.Errorf("params.response_format json_schema schema must be an object")
		}
		return nil
	default:
		return fmt.Errorf("invalid params.response_format type %q", rf.Type)
	}
}

// MarshalRequest renders a Request as a canonical IR document, stamping
// specVersion.
func MarshalRequest(req *Request) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("ir: nil request")
	}
	if req.Params.ResponseFormat != nil {
		rf := req.Params.ResponseFormat
		if err := validateWireResponseFormat(&wireResponseFormat{
			Type: rf.Type, Name: rf.Name, Description: rf.Description, Schema: rf.Schema, Strict: rf.Strict,
		}); err != nil {
			return nil, err
		}
	}
	system := make([]wireSystemBlock, 0, len(req.System))
	for _, s := range req.System {
		system = append(system, wireSystemBlock{Type: BlockTypeText, Text: s.Text, Native: s.Native})
	}
	messages := make([]wireMessage, 0, len(req.Messages))
	for i, m := range req.Messages {
		content, err := marshalRequestBlocks(m.Content)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		messages = append(messages, wireMessage{Role: m.Role, Content: content})
	}
	tools := make([]wireTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		if err := validateTool(t); err != nil {
			return nil, err
		}
		if t.Type == ToolTypeFunction {
			t.Type = ""
		}
		tools = append(tools, wireTool(t))
	}
	if err := validateToolChoice(req.ToolChoice); err != nil {
		return nil, err
	}
	if err := ValidateCustomConversation(req); err != nil {
		return nil, err
	}
	var tc *wireToolChoice
	if req.ToolChoice != nil {
		tc = &wireToolChoice{Mode: req.ToolChoice.Mode, Name: req.ToolChoice.Name, ToolType: req.ToolChoice.ToolType}
	}
	var params *wireParams
	if req.Params.set() {
		params = &wireParams{
			Temperature:     req.Params.Temperature,
			TopP:            req.Params.TopP,
			MaxTokens:       req.Params.MaxTokens,
			StopSequences:   req.Params.StopSequences,
			ReasoningEffort: req.Params.ReasoningEffort,
		}
		if req.Params.ResponseFormat != nil {
			params.ResponseFormat = &wireResponseFormat{
				Type:        req.Params.ResponseFormat.Type,
				Name:        req.Params.ResponseFormat.Name,
				Description: req.Params.ResponseFormat.Description,
				Schema:      req.Params.ResponseFormat.Schema,
				Strict:      req.Params.ResponseFormat.Strict,
			}
		}
	}
	return json.Marshal(wireRequest{
		SpecVersion: SpecVersion,
		Model:       req.Model,
		System:      system,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  tc,
		Params:      params,
		Metadata:    req.Metadata,
		Native:      req.Native,
	})
}

// UnmarshalRequest parses a canonical IR request document, const-checking
// specVersion.
func UnmarshalRequest(data []byte) (*Request, error) {
	var w wireRequest
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	if err := checkSpecVersion(w.SpecVersion); err != nil {
		return nil, err
	}
	if w.Params != nil {
		if err := validateWireResponseFormat(w.Params.ResponseFormat); err != nil {
			return nil, err
		}
	}
	req := &Request{Model: w.Model, Metadata: w.Metadata, Native: w.Native}
	for _, s := range w.System {
		req.System = append(req.System, SystemBlock{Text: s.Text, Native: s.Native})
	}
	for i, m := range w.Messages {
		content, err := unmarshalRequestBlocks(m.Content)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		req.Messages = append(req.Messages, Message{Role: m.Role, Content: content})
	}
	for _, t := range w.Tools {
		tool := Tool(t)
		if err := validateTool(tool); err != nil {
			return nil, err
		}
		req.Tools = append(req.Tools, tool)
	}
	if w.ToolChoice != nil {
		req.ToolChoice = &ToolChoice{Mode: w.ToolChoice.Mode, Name: w.ToolChoice.Name, ToolType: w.ToolChoice.ToolType}
		if err := validateToolChoice(req.ToolChoice); err != nil {
			return nil, err
		}
	}
	if w.Params != nil {
		req.Params = Params{
			Temperature:     w.Params.Temperature,
			TopP:            w.Params.TopP,
			MaxTokens:       w.Params.MaxTokens,
			StopSequences:   w.Params.StopSequences,
			ReasoningEffort: w.Params.ReasoningEffort,
		}
		if w.Params.ResponseFormat != nil {
			req.Params.ResponseFormat = &ResponseFormat{
				Type:        w.Params.ResponseFormat.Type,
				Name:        w.Params.ResponseFormat.Name,
				Description: w.Params.ResponseFormat.Description,
				Schema:      w.Params.ResponseFormat.Schema,
				Strict:      w.Params.ResponseFormat.Strict,
			}
		}
	}
	if err := ValidateCustomConversation(req); err != nil {
		return nil, err
	}
	return req, nil
}

// ---- response document -----------------------------------------------------

type wireResponse struct {
	SpecVersion  string            `json:"specVersion"`
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	Content      []json.RawMessage `json:"content"`
	StopReason   StopReason        `json:"stop_reason"`
	StopSequence string            `json:"stop_sequence,omitempty"`
	Usage        wireUsage         `json:"usage"`
}

type wireUsage struct {
	InputTokens              int64                    `json:"input_tokens"`
	OutputTokens             int64                    `json:"output_tokens"`
	CacheReadInputTokens     *int64                   `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int64                   `json:"cache_creation_input_tokens,omitempty"`
	InputTokensDetails       *wireInputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokensDetails      *wireOutputTokensDetails `json:"output_tokens_details,omitempty"`
}

type wireInputTokensDetails struct {
	CachedTokens *int64 `json:"cached_tokens,omitempty"`
}

type wireOutputTokensDetails struct {
	ReasoningTokens *int64 `json:"reasoning_tokens,omitempty"`
}

func makeWireUsage(u Usage) wireUsage {
	wu := wireUsage{
		InputTokens:              u.InputTokens,
		OutputTokens:             u.OutputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
	}
	if u.InputTokensDetails != nil {
		wu.InputTokensDetails = &wireInputTokensDetails{CachedTokens: u.InputTokensDetails.CachedTokens}
	}
	if u.OutputTokensDetails != nil {
		wu.OutputTokensDetails = &wireOutputTokensDetails{ReasoningTokens: u.OutputTokensDetails.ReasoningTokens}
	}
	return wu
}

func parseWireUsage(w wireUsage) Usage {
	u := Usage{
		InputTokens:              w.InputTokens,
		OutputTokens:             w.OutputTokens,
		CacheReadInputTokens:     w.CacheReadInputTokens,
		CacheCreationInputTokens: w.CacheCreationInputTokens,
	}
	if w.InputTokensDetails != nil {
		u.InputTokensDetails = &InputTokensDetails{CachedTokens: w.InputTokensDetails.CachedTokens}
	}
	if w.OutputTokensDetails != nil {
		u.OutputTokensDetails = &OutputTokensDetails{ReasoningTokens: w.OutputTokensDetails.ReasoningTokens}
	}
	return u
}

// MarshalResponse renders a Response as a canonical IR document, stamping
// specVersion.
func MarshalResponse(resp *Response) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("ir: nil response")
	}
	content, err := marshalBlocks(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	return json.Marshal(wireResponse{
		SpecVersion:  SpecVersion,
		ID:           resp.ID,
		Model:        resp.Model,
		Content:      content,
		StopReason:   resp.StopReason,
		StopSequence: resp.StopSequence,
		Usage:        makeWireUsage(resp.Usage),
	})
}

// UnmarshalResponse parses a canonical IR response document, const-checking
// specVersion.
func UnmarshalResponse(data []byte) (*Response, error) {
	var w wireResponse
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	if err := checkSpecVersion(w.SpecVersion); err != nil {
		return nil, err
	}
	content, err := unmarshalBlocks(w.Content)
	if err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	return &Response{
		ID:           w.ID,
		Model:        w.Model,
		Content:      content,
		StopReason:   w.StopReason,
		StopSequence: w.StopSequence,
		Usage:        parseWireUsage(w.Usage),
	}, nil
}

// ---- deltas and events -----------------------------------------------------

type wireTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireInputJSONDelta struct {
	Type        string          `json:"type"`
	PartialJSON json.RawMessage `json:"partial_json"`
}

type wireThinkingDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireSignatureDelta struct {
	Type      string `json:"type"`
	Signature string `json:"signature"`
}

// MarshalDelta renders one delta in its canonical type-discriminated form.
func MarshalDelta(d Delta) (json.RawMessage, error) {
	switch v := d.(type) {
	case CustomToolInputDelta:
		if err := validateOpaqueString(v.Text); err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Type string          `json:"type"`
			Text json.RawMessage `json:"text"`
		}{DeltaTypeCustomToolInput, v.Text})
	case TextDelta:
		return json.Marshal(wireTextDelta{Type: DeltaTypeTextDelta, Text: v.Text})
	case InputJSONDelta:
		return json.Marshal(wireInputJSONDelta{
			Type: DeltaTypeInputJSONDelta, PartialJSON: v.PartialJSON,
		})
	case ThinkingDelta:
		return json.Marshal(wireThinkingDelta{Type: DeltaTypeThinking, Text: v.Text})
	case SignatureDelta:
		if v.Signature == "" {
			return nil, fmt.Errorf("ir: signature is required in signature_delta")
		}
		return json.Marshal(wireSignatureDelta{Type: DeltaTypeSignature, Signature: v.Signature})
	default:
		return nil, fmt.Errorf("ir: unknown delta type %T", d)
	}
}

// UnmarshalDelta parses one type-discriminated delta.
func UnmarshalDelta(data []byte) (Delta, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case DeltaTypeCustomToolInput:
		var w CustomToolInputDelta
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if err := validateOpaqueString(w.Text); err != nil {
			return nil, err
		}
		return w, nil
	case DeltaTypeTextDelta:
		var w wireTextDelta
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return TextDelta{Text: w.Text}, nil
	case DeltaTypeInputJSONDelta:
		var w wireInputJSONDelta
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return InputJSONDelta{PartialJSON: w.PartialJSON}, nil
	case DeltaTypeThinking:
		var w wireThinkingDelta
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return ThinkingDelta{Text: w.Text}, nil
	case DeltaTypeSignature:
		var w wireSignatureDelta
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		if w.Signature == "" {
			return nil, fmt.Errorf("ir: signature is required in signature_delta")
		}
		return SignatureDelta{Signature: w.Signature}, nil
	default:
		return nil, fmt.Errorf("ir: unknown delta type %q", head.Type)
	}
}

// MarshalEvent renders one event in its canonical type-discriminated form.
func MarshalEvent(e Event) (json.RawMessage, error) {
	switch v := e.(type) {
	case MessageStart:
		return json.Marshal(struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			Model string `json:"model"`
		}{Type: EventTypeMessageStart, ID: v.ID, Model: v.Model})
	case ContentBlockStart:
		block, err := MarshalBlock(v.Block)
		if err != nil {
			return nil, fmt.Errorf("block: %w", err)
		}
		return json.Marshal(struct {
			Type  string          `json:"type"`
			Index int             `json:"index"`
			Block json.RawMessage `json:"block"`
		}{Type: EventTypeContentBlockStart, Index: v.Index, Block: block})
	case ContentBlockDelta:
		delta, err := MarshalDelta(v.Delta)
		if err != nil {
			return nil, fmt.Errorf("delta: %w", err)
		}
		return json.Marshal(struct {
			Type  string          `json:"type"`
			Index int             `json:"index"`
			Delta json.RawMessage `json:"delta"`
		}{Type: EventTypeContentBlockDelta, Index: v.Index, Delta: delta})
	case ContentBlockStop:
		return json.Marshal(struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}{Type: EventTypeContentBlockStop, Index: v.Index})
	case MessageDelta:
		return json.Marshal(struct {
			Type         string     `json:"type"`
			StopReason   StopReason `json:"stop_reason"`
			StopSequence string     `json:"stop_sequence,omitempty"`
			Usage        wireUsage  `json:"usage"`
		}{Type: EventTypeMessageDelta, StopReason: v.StopReason, StopSequence: v.StopSequence,
			Usage: makeWireUsage(v.Usage)})
	case MessageDone:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{Type: EventTypeMessageDone})
	default:
		return nil, fmt.Errorf("ir: unknown event type %T", e)
	}
}

// UnmarshalEvent parses one type-discriminated event.
func UnmarshalEvent(data []byte) (Event, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case EventTypeMessageStart:
		var w struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return MessageStart{ID: w.ID, Model: w.Model}, nil
	case EventTypeContentBlockStart:
		var w struct {
			Index int             `json:"index"`
			Block json.RawMessage `json:"block"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		block, err := UnmarshalBlock(w.Block)
		if err != nil {
			return nil, fmt.Errorf("block: %w", err)
		}
		return ContentBlockStart{Index: w.Index, Block: block}, nil
	case EventTypeContentBlockDelta:
		var w struct {
			Index int             `json:"index"`
			Delta json.RawMessage `json:"delta"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		delta, err := UnmarshalDelta(w.Delta)
		if err != nil {
			return nil, fmt.Errorf("delta: %w", err)
		}
		return ContentBlockDelta{Index: w.Index, Delta: delta}, nil
	case EventTypeContentBlockStop:
		var w struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return ContentBlockStop{Index: w.Index}, nil
	case EventTypeMessageDelta:
		var w struct {
			StopReason   StopReason `json:"stop_reason"`
			StopSequence string     `json:"stop_sequence"`
			Usage        wireUsage  `json:"usage"`
		}
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, err
		}
		return MessageDelta{
			StopReason:   w.StopReason,
			StopSequence: w.StopSequence,
			Usage:        parseWireUsage(w.Usage),
		}, nil
	case EventTypeMessageDone:
		return MessageDone{}, nil
	default:
		return nil, fmt.Errorf("ir: unknown event type %q", head.Type)
	}
}

// ---- event stream document ---------------------------------------------------

type wireEventStream struct {
	SpecVersion string            `json:"specVersion"`
	Events      []json.RawMessage `json:"events"`
}

// MarshalEventStream renders an EventStream as a canonical IR document,
// stamping specVersion.
func MarshalEventStream(es *EventStream) ([]byte, error) {
	events := make([]json.RawMessage, 0, len(es.Events))
	for i, e := range es.Events {
		raw, err := MarshalEvent(e)
		if err != nil {
			return nil, fmt.Errorf("events[%d]: %w", i, err)
		}
		events = append(events, raw)
	}
	return json.Marshal(wireEventStream{SpecVersion: SpecVersion, Events: events})
}

// UnmarshalEventStream parses a canonical IR event-stream document,
// const-checking specVersion.
func UnmarshalEventStream(data []byte) (*EventStream, error) {
	var w wireEventStream
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	if err := checkSpecVersion(w.SpecVersion); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(w.Events))
	for i, raw := range w.Events {
		e, err := UnmarshalEvent(raw)
		if err != nil {
			return nil, fmt.Errorf("events[%d]: %w", i, err)
		}
		events = append(events, e)
	}
	return &EventStream{Events: events}, nil
}

func checkSpecVersion(v string) error {
	if v != "0.1.0" && v != "0.2.0" {
		return fmt.Errorf("ir: unsupported specVersion %q (want %q or %q)", v, "0.1.0", "0.2.0")
	}
	return nil
}
