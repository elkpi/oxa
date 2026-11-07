package jev

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v3/ir"
)

// DecodeRequest converts a Jev wire request into an IR request (spec/17 §3).
// Errors are structural: missing model, missing state, empty questions, or
// criteria of the wrong shape. Everything else the projection cannot carry
// is a loss.
func DecodeRequest(wire *Request, opts ...Option) (*ir.Request, []ir.Loss, error) {
	o := newOptions(opts...)
	if wire.Model == "" {
		return nil, nil, fmt.Errorf("jev: request model is required")
	}
	if len(bytes.TrimSpace(wire.State)) == 0 || isJSONNull(wire.State) {
		return nil, nil, fmt.Errorf("jev: request state is required")
	}
	if len(wire.Questions) == 0 {
		return nil, nil, fmt.Errorf("jev: request questions must not be empty")
	}

	stateText, degraded, err := stateText(wire.State)
	if err != nil {
		return nil, nil, fmt.Errorf("jev: request state: %w", err)
	}
	var losses []ir.Loss
	if degraded {
		losses = append(losses, ir.Loss{
			Path: "state", Field: "state", Reason: ir.LossDegraded,
			Detail: "N-JEV-1: state serialized to canonical JSON text",
		})
	}
	if len(wire.Assets) > 0 && !isJSONNull(wire.Assets) {
		losses = append(losses, ir.Loss{
			Path: "assets", Field: "assets", Reason: ir.LossUnsupportedSemantic,
			Detail: "N-JEV-4: experimental image assets are out of scope",
		})
	}

	ids := sortedKeys(wire.Questions)
	var lines []string
	props := map[string]any{}
	var required []string
	for _, id := range ids {
		q := wire.Questions[id]
		questionLines, prop, loss, err := renderQuestion(id, q)
		if err != nil {
			return nil, nil, err
		}
		if loss != nil {
			losses = append(losses, *loss)
			continue
		}
		lines = append(lines, questionLines...)
		props[id] = prop
		required = append(required, id)
	}
	if len(props) == 0 {
		return nil, nil, fmt.Errorf("jev: request questions produced no supported questions")
	}

	schema, err := json.Marshal(map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("jev: synthesized schema: %w", err)
	}

	return &ir.Request{
		Model:  o.models.Map(wire.Model),
		System: []ir.SystemBlock{{Text: stateText + "\n\n" + instructionTemplate}},
		Messages: []ir.Message{{
			Role:    ir.RoleUser,
			Content: []ir.Block{ir.TextBlock{Text: strings.Join(lines, "\n")}},
		}},
		Params: ir.Params{
			ResponseFormat: &ir.ResponseFormat{
				Type:   ir.ResponseFormatJSONSchema,
				Name:   ResponseFormatName,
				Schema: json.RawMessage(schema),
			},
		},
	}, losses, nil
}

// stateText renders the state as system-prompt text. A JSON string keeps its
// unquoted value; object/array shapes serialize to canonical compact JSON
// (sorted keys, no whitespace, numbers preserved) and report degraded.
// Other scalar shapes are structural errors (spec/17 §2: state is a string,
// object, or array).
func stateText(raw json.RawMessage) (string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", false, err
		}
		return s, false, nil
	case '{', '[':
		canon, err := canonicalJSON(trimmed)
		if err != nil {
			return "", false, err
		}
		return canon, true, nil
	default:
		return "", false, fmt.Errorf("jev: state must be a string, object, or array")
	}
}

// renderQuestion renders one question into prompt lines and its synthesized
// schema property. A non-nil loss means the question is skipped (experimental
// or unsupported type); a nil property with a nil loss cannot happen.
func renderQuestion(id string, q Question) ([]string, map[string]any, *ir.Loss, error) {
	head := fmt.Sprintf("question %s (%s): %s", id, q.Type, q.Instructions)
	var criteriaLines []string
	var prop map[string]any

	switch q.Type {
	case QuestionTypeNoul:
		if hasCriteria(q.Criteria) {
			var crit map[string]json.RawMessage
			if err := json.Unmarshal(q.Criteria, &crit); err != nil {
				return nil, nil, nil, fmt.Errorf("jev: question %s noul criteria must be an object", id)
			}
			criteriaLines = append(criteriaLines, "criteria:")
			for _, name := range []string{"false", "true"} {
				if raw, ok := crit[name]; ok {
					criteriaLines = append(criteriaLines, criteriaLine(name, raw))
				}
			}
		}
		prop = numberProperty(q.Instructions)
		prop["minimum"] = 0
		prop["maximum"] = 1
	case QuestionTypeChoice:
		if !hasCriteria(q.Criteria) {
			return nil, nil, nil, fmt.Errorf("jev: question %s choice criteria is required", id)
		}
		var crit map[string]json.RawMessage
		if err := json.Unmarshal(q.Criteria, &crit); err != nil {
			return nil, nil, nil, fmt.Errorf("jev: question %s choice criteria must be an object", id)
		}
		if len(crit) == 0 {
			return nil, nil, nil, fmt.Errorf("jev: question %s choice criteria must not be empty", id)
		}
		names := sortedKeys(crit)
		criteriaLines = append(criteriaLines, "criteria:")
		for _, name := range names {
			criteriaLines = append(criteriaLines, criteriaLine(name, crit[name]))
		}
		prop = map[string]any{"type": "string", "enum": names}
		setDescription(prop, q.Instructions)
	case QuestionTypeScore:
		if !hasCriteria(q.Criteria) {
			return nil, nil, nil, fmt.Errorf("jev: question %s score criteria is required", id)
		}
		var levels []json.RawMessage
		if err := json.Unmarshal(q.Criteria, &levels); err != nil {
			return nil, nil, nil, fmt.Errorf("jev: question %s score criteria must be an array", id)
		}
		if len(levels) == 0 {
			return nil, nil, nil, fmt.Errorf("jev: question %s score criteria must not be empty", id)
		}
		criteriaLines = append(criteriaLines, "criteria:")
		enum := make([]int, len(levels))
		for i, raw := range levels {
			enum[i] = i + 1
			criteriaLines = append(criteriaLines, criteriaLine(fmt.Sprintf("level %d", i+1), raw))
		}
		prop = map[string]any{"type": "number", "enum": enum}
		setDescription(prop, q.Instructions)
	case QuestionTypeBox:
		return nil, nil, &ir.Loss{
			Path: "questions." + id, Field: "type", Reason: ir.LossUnsupportedSemantic,
			Detail: "N-JEV-5: experimental box questions are out of scope",
		}, nil
	default:
		return nil, nil, &ir.Loss{
			Path: "questions." + id, Field: "type", Reason: ir.LossUnsupportedSemantic,
			Detail: "N-JEV-6: skipped from schema and prompt",
		}, nil
	}

	lines := append([]string{head}, criteriaLines...)
	return lines, prop, nil, nil
}

func numberProperty(instructions string) map[string]any {
	prop := map[string]any{"type": "number"}
	setDescription(prop, instructions)
	return prop
}

func setDescription(prop map[string]any, instructions string) {
	if instructions != "" {
		prop["description"] = instructions
	}
}

// criteriaLine renders "- <name>: <description>"; a null or absent
// description renders as "- <name>".
func criteriaLine(name string, raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return "- " + name
	}
	trimmed := bytes.TrimSpace(raw)
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return "- " + name + ": " + s
		}
	}
	desc, err := canonicalJSON(trimmed)
	if err != nil {
		return "- " + name
	}
	return "- " + name + ": " + desc
}

// DecodeResponse converts a Jev wire response into an IR response
// (spec/17 §4): answers carry verbatim into one canonical text block,
// usage maps directly, and stop_reason is the derived end_turn envelope.
func DecodeResponse(wire *Response, opts ...Option) (*ir.Response, []ir.Loss, error) {
	o := newOptions(opts...)
	if wire.Model == "" {
		return nil, nil, fmt.Errorf("jev: response model is required")
	}
	if len(wire.Answers) == 0 {
		return nil, nil, fmt.Errorf("jev: response answers must not be empty")
	}
	text, err := canonicalAnswers(wire.Answers)
	if err != nil {
		return nil, nil, fmt.Errorf("jev: answers: %w", err)
	}
	var losses []ir.Loss
	for _, id := range sortedKeys(wire.Answers) {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(wire.Answers[id], &probe); err != nil {
			return nil, nil, fmt.Errorf("jev: answer %s must be an object", id)
		}
		if probe.Type == QuestionTypeBox {
			losses = append(losses, ir.Loss{
				Path: "answers." + id, Field: "type", Reason: ir.LossUnsupportedSemantic,
				Detail: "N-JEV-5: experimental box answers are carried verbatim",
			})
		}
	}
	return &ir.Response{
		Model:      o.models.Map(wire.Model),
		ID:         synthesizedID,
		Content:    []ir.Block{ir.TextBlock{Text: text}},
		StopReason: ir.StopEndTurn,
		Usage:      ir.Usage{InputTokens: wire.Usage.InputTokens, OutputTokens: wire.Usage.OutputTokens},
	}, losses, nil
}
