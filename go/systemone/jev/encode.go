package jev

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elkpi/oxa/go/v3/ir"
)

// EncodeRequest rebuilds a Jev wire request from an IR request (spec/17
// §3.1): questions reconstruct from the synthesized json_schema response
// format, state is the template-stripped system text, and everything the
// evaluation protocol cannot express reports a loss.
func EncodeRequest(req *ir.Request, opts ...Option) (*Request, []ir.Loss, error) {
	o := newOptions(opts...)
	var losses []ir.Loss

	rf := req.Params.ResponseFormat
	if rf == nil || rf.Type != ir.ResponseFormatJSONSchema || len(rf.Schema) == 0 {
		return nil, nil, fmt.Errorf("jev: EncodeRequest requires a json_schema response format")
	}
	questions, qLosses, err := questionsFromSchema(rf.Schema)
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, qLosses...)

	state, sLosses, err := stateFromIR(req)
	if err != nil {
		return nil, nil, err
	}
	losses = append(losses, sLosses...)
	losses = append(losses, requestLosses(req)...)

	stateRaw, err := json.Marshal(state)
	if err != nil {
		return nil, nil, fmt.Errorf("jev: state: %w", err)
	}
	return &Request{
		Model:     o.models.Map(req.Model),
		State:     json.RawMessage(stateRaw),
		Questions: questions,
	}, losses, nil
}

func requestLosses(req *ir.Request) []ir.Loss {
	var losses []ir.Loss
	p := req.Params
	jev10 := func(path, field string) ir.Loss {
		return ir.Loss{Path: path, Field: field, Reason: ir.LossUnmappedField, Detail: "N-JEV-10: one loss per present field"}
	}
	if p.Temperature != nil {
		losses = append(losses, jev10("params", "temperature"))
	}
	if p.TopP != nil {
		losses = append(losses, jev10("params", "top_p"))
	}
	if p.MaxTokens != nil {
		losses = append(losses, jev10("params", "max_tokens"))
	}
	if len(p.StopSequences) > 0 {
		losses = append(losses, jev10("params", "stop_sequences"))
	}
	if p.ReasoningEffort != "" {
		losses = append(losses, jev10("params", "reasoning_effort"))
	}
	if p.Verbosity != "" {
		losses = append(losses, jev10("params", "verbosity"))
	}
	if len(req.Tools) > 0 {
		losses = append(losses, jev10("tools", "tools"))
	}
	if req.ToolChoice != nil {
		losses = append(losses, jev10("tool_choice", "tool_choice"))
	}
	for _, k := range sortedKeys(req.Metadata) {
		losses = append(losses, ir.Loss{Path: "metadata", Field: k, Reason: ir.LossUnmappedField, Detail: "N-JEV-10: no Jev equivalent"})
	}
	if len(req.Native) > 0 {
		losses = append(losses, jev10("native", "native"))
	}
	return losses
}

// stateFromIR extracts the state: the template-stripped system text. User
// messages carry the derived question rendering (spec/17 §3.1) and are not
// re-carried; any other role reports a loss.
func stateFromIR(req *ir.Request) (string, []ir.Loss, error) {
	var sysParts []string
	for _, b := range req.System {
		if b.Text != "" {
			sysParts = append(sysParts, b.Text)
		}
	}
	if len(sysParts) == 0 {
		return "", nil, fmt.Errorf("jev: EncodeRequest requires system text")
	}
	var losses []ir.Loss
	for i, msg := range req.Messages {
		if msg.Role == ir.RoleUser {
			continue
		}
		losses = append(losses, ir.Loss{
			Path:   fmt.Sprintf("messages[%d]", i),
			Field:  "content",
			Reason: ir.LossUnsupportedSemantic,
			Detail: "N-JEV-10: Jev has no conversation turns",
		})
	}
	return stripTemplate(strings.Join(sysParts, "\n\n")), losses, nil
}

func stripTemplate(text string) string {
	suffix := "\n\n" + instructionTemplate
	if strings.HasSuffix(text, suffix) {
		return strings.TrimSuffix(text, suffix)
	}
	return text
}

// questionsFromSchema reads the synthesized schema's properties (the one
// authorized read-only consumption of Params.ResponseFormat.Schema,
// spec/17 §3) and reconstructs the questions map.
func questionsFromSchema(schema json.RawMessage) (map[string]Question, []ir.Loss, error) {
	var obj struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &obj); err != nil {
		return nil, nil, fmt.Errorf("jev: response format schema: %w", err)
	}
	if len(obj.Properties) == 0 {
		return nil, nil, fmt.Errorf("jev: response format schema has no properties")
	}
	questions := map[string]Question{}
	var losses []ir.Loss
	for _, id := range sortedKeys(obj.Properties) {
		var prop map[string]any
		if err := json.Unmarshal(obj.Properties[id], &prop); err != nil {
			return nil, nil, fmt.Errorf("jev: schema property %s: %w", id, err)
		}
		q, loss, ok, err := questionFromProperty(id, prop)
		if err != nil {
			return nil, nil, err
		}
		if loss != nil {
			losses = append(losses, *loss)
		}
		if ok {
			questions[id] = q
		}
	}
	if len(questions) == 0 {
		return nil, nil, fmt.Errorf("jev: response format schema has no reconstructable questions")
	}
	return questions, losses, nil
}

// questionFromProperty reconstructs one question by exact property shape
// (spec/17 §7.4). ok=false with a loss skips the property.
func questionFromProperty(id string, prop map[string]any) (Question, *ir.Loss, bool, error) {
	q := Question{}
	if desc, ok := prop["description"].(string); ok {
		q.Instructions = desc
	}
	propType, _ := prop["type"].(string)
	switch propType {
	case "number":
		min, hasMin := prop["minimum"].(float64)
		max, hasMax := prop["maximum"].(float64)
		if enum, hasEnum := prop["enum"].([]any); hasEnum {
			levels, ok := scoreLevels(enum)
			if !ok {
				return Question{}, &ir.Loss{
					Path: "questions." + id, Field: "type", Reason: ir.LossUnsupportedSemantic,
					Detail: "N-JEV-6: skipped from schema and prompt",
				}, false, nil
			}
			q.Type = QuestionTypeScore
			q.Criteria = marshalCriteria(make([]string, levels))
			return q, &ir.Loss{
				Path: "questions." + id, Field: "criteria", Reason: ir.LossDegraded,
				Detail: "N-JEV-9: criteria descriptions live in the prompt text",
			}, true, nil
		}
		if hasMin && hasMax && min == 0 && max == 1 {
			q.Type = QuestionTypeNoul
			return q, nil, true, nil
		}
	case "string":
		if names, ok := choiceOptions(prop["enum"]); ok {
			q.Type = QuestionTypeChoice
			crit := map[string]string{}
			for _, name := range names {
				crit[name] = name
			}
			q.Criteria = marshalCriteria(crit)
			return q, &ir.Loss{
				Path: "questions." + id, Field: "criteria", Reason: ir.LossDegraded,
				Detail: "N-JEV-9: criteria descriptions live in the prompt text",
			}, true, nil
		}
	}
	return Question{}, &ir.Loss{
		Path: "questions." + id, Field: "type", Reason: ir.LossUnsupportedSemantic,
		Detail: "N-JEV-6: skipped from schema and prompt",
	}, false, nil
}

// scoreLevels validates an enum of ascending 1..N integers and returns N.
func scoreLevels(enum []any) (int, bool) {
	if len(enum) == 0 {
		return 0, false
	}
	for i, v := range enum {
		n, ok := v.(float64)
		if !ok || n != float64(i+1) {
			return 0, false
		}
	}
	return len(enum), true
}

func choiceOptions(v any) ([]string, bool) {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return nil, false
	}
	names := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		names[i] = s
	}
	return names, true
}

func marshalCriteria(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(raw)
}

// EncodeResponse parses an IR response's model output back into typed Jev
// answers (spec/17 §4.1). Answer types recover exactly from WithQuestions
// context; without context they come from the carried value shape, and
// numeric answers report the noul/score ambiguity.
func EncodeResponse(resp *ir.Response, opts ...Option) (*Response, []ir.Loss, error) {
	o := newOptions(opts...)
	var losses []ir.Loss
	var texts []string
	for i, b := range resp.Content {
		if tb, ok := b.(ir.TextBlock); ok {
			texts = append(texts, tb.Text)
			continue
		}
		losses = append(losses, ir.Loss{
			Path:   fmt.Sprintf("content[%d]", i),
			Field:  "type",
			Reason: ir.LossUnsupportedSemantic,
			Detail: "N-JEV-15: non-text blocks are excluded from answers",
		})
	}
	if len(texts) == 0 {
		return nil, nil, fmt.Errorf("jev: response has no text content")
	}
	aggregated := strings.Join(texts, "")
	var answers map[string]json.RawMessage
	if err := json.Unmarshal([]byte(aggregated), &answers); err != nil {
		return nil, nil, fmt.Errorf("jev: aggregated response text is not a JSON object: %w", err)
	}

	out := map[string]json.RawMessage{}
	for _, id := range sortedKeys(answers) {
		answer, loss, err := recoverAnswer(id, answers[id], o.questions)
		if err != nil {
			return nil, nil, err
		}
		if loss != nil {
			losses = append(losses, *loss)
		}
		out[id] = answer
	}
	if resp.StopReason != ir.StopEndTurn && resp.StopReason != ir.StopOther {
		losses = append(losses, ir.Loss{
			Path: "stop_reason", Field: "stop_reason", Reason: ir.LossUnmappedField,
			Detail: "N-JEV-16: Jev responses carry no stop reason",
		})
	}
	losses = append(losses, usageLosses(resp.Usage)...)

	return &Response{
		Model:   o.models.Map(resp.Model),
		Answers: out,
		Usage:   Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens},
	}, losses, nil
}

func usageLosses(u ir.Usage) []ir.Loss {
	var out []ir.Loss
	add := func(field string) ir.Loss {
		return ir.Loss{
			Path: "usage", Field: field, Reason: ir.LossUnmappedField,
			Detail: "N-JEV-17: usage granularity has no Jev equivalent",
		}
	}
	if u.CacheReadInputTokens != nil {
		out = append(out, add("cache_read_input_tokens"))
	}
	if u.CacheCreationInputTokens != nil {
		out = append(out, add("cache_creation_input_tokens"))
	}
	if u.InputTokensDetails != nil {
		out = append(out, add("input_tokens_details"))
	}
	if u.OutputTokensDetails != nil {
		out = append(out, add("output_tokens_details"))
	}
	return out
}

// recoverAnswer normalizes one answer into Jev wire shape (spec/17 §4.1).
func recoverAnswer(id string, raw json.RawMessage, context map[string]Question) (json.RawMessage, *ir.Loss, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, nil, fmt.Errorf("jev: answer %s: %w", id, err)
	}
	switch t := v.(type) {
	case bool:
		n := 0
		if t {
			n = 1
		}
		out, err := marshalAnswer(map[string]any{"type": QuestionTypeNoul, "noul": n})
		return out, nil, err
	case string:
		out, err := marshalAnswer(map[string]any{"type": QuestionTypeChoice, "choice": t})
		return out, nil, err
	case json.Number:
		out, err := marshalAnswer(map[string]any{"type": QuestionTypeScore, "score": t})
		return out, degradedType(id), err
	case map[string]any:
		return recoverObjectAnswer(id, t, raw, context)
	default:
		return nil, nil, fmt.Errorf("jev: answer %s must be an object or scalar value", id)
	}
}

func recoverObjectAnswer(id string, obj map[string]any, raw json.RawMessage, context map[string]Question) (json.RawMessage, *ir.Loss, error) {
	var loss *ir.Loss
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		return nil, nil, fmt.Errorf("jev: answer %s: %w", id, err)
	}
	compactRaw := json.RawMessage(compacted.Bytes())
	typ, _ := obj["type"].(string)
	if typ == QuestionTypeBox {
		return compactRaw, &ir.Loss{
			Path: "answers." + id, Field: "type", Reason: ir.LossUnsupportedSemantic,
			Detail: "N-JEV-5: experimental box answers are carried verbatim",
		}, nil
	}
	if q, ok := context[id]; ok {
		if typ != "" && typ != q.Type {
			// The carried answer contradicts the original question: carry
			// verbatim rather than rewrite (spec/17 §4.1, N-JEV-12).
			return compactRaw, &ir.Loss{
				Path: "answers." + id, Field: "type", Reason: ir.LossUnmappedValue,
				Detail: "N-JEV-12: carried type contradicts the original question",
			}, nil
		}
		if typ == "" {
			obj["type"] = q.Type
		}
		typ = q.Type
	}
	if typ == "" {
		switch {
		case hasKey(obj, "noul"):
			typ = QuestionTypeNoul
		case hasKey(obj, "choice"):
			typ = QuestionTypeChoice
		case hasKey(obj, "score"):
			typ = QuestionTypeScore
		default:
			return nil, nil, fmt.Errorf("jev: answer %s carries no recognized value", id)
		}
		obj["type"] = typ
	}

	switch typ {
	case QuestionTypeNoul:
		n, ok := numValue(obj["noul"])
		if !ok {
			if b, isBool := obj["noul"].(bool); isBool {
				n, ok = 0, true
				if b {
					n = 1
				}
				obj["noul"] = n
			}
		}
		if !ok {
			return nil, nil, fmt.Errorf("jev: answer %s carries no noul value", id)
		}
		if l := rangeLoss(id, n, 0, 1, context); l != nil {
			loss = pickLoss(loss, l)
		}
	case QuestionTypeChoice:
		c, ok := obj["choice"].(string)
		if !ok {
			return nil, nil, fmt.Errorf("jev: answer %s carries no choice value", id)
		}
		if q, hasQ := context[id]; hasQ {
			names, err := criteriaNames(q.Criteria)
			if err != nil {
				return nil, nil, err
			}
			if len(names) > 0 && !contains(names, c) {
				loss = pickLoss(loss, outOfRange(id, "choice"))
			}
		}
	case QuestionTypeScore:
		n, ok := numValue(obj["score"])
		if !ok {
			return nil, nil, fmt.Errorf("jev: answer %s carries no score value", id)
		}
		if q, hasQ := context[id]; hasQ {
			levels, err := criteriaCount(q.Criteria)
			if err != nil {
				return nil, nil, err
			}
			if levels > 0 && (n < 1 || n > float64(levels)) {
				loss = pickLoss(loss, outOfRange(id, "score"))
			}
		}
	}
	out, err := marshalAnswer(obj)
	if err != nil {
		return nil, nil, err
	}
	return out, loss, nil
}

func hasKey(obj map[string]any, key string) bool {
	_, ok := obj[key]
	return ok
}

// numValue extracts a float64 from a JSON number leaf (json.Number or float64).
func numValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func rangeLoss(id string, n, lo, hi float64, context map[string]Question) *ir.Loss {
	if q, ok := context[id]; ok && q.Type == QuestionTypeNoul && (n < lo || n > hi) {
		return outOfRange(id, "noul")
	}
	return nil
}

func outOfRange(id, field string) *ir.Loss {
	return &ir.Loss{
		Path: "answers." + id, Field: field, Reason: ir.LossDegraded,
		Detail: "N-JEV-14: value kept, not rewritten",
	}
}

func degradedType(id string) *ir.Loss {
	return &ir.Loss{
		Path: "answers." + id, Field: "type", Reason: ir.LossDegraded,
		Detail: "N-JEV-13: noul/score are ambiguous without question context",
	}
}

func pickLoss(a, b *ir.Loss) *ir.Loss {
	if a != nil {
		return a
	}
	return b
}

func criteriaNames(raw json.RawMessage) ([]string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil
	}
	return sortedKeys(m), nil
}

func criteriaCount(raw json.RawMessage) (int, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return 0, nil
	}
	return len(arr), nil
}

func contains(names []string, s string) bool {
	for _, n := range names {
		if n == s {
			return true
		}
	}
	return false
}

// marshalAnswer serializes an answer object without HTML escaping and with
// sorted keys (map marshal order).
func marshalAnswer(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(strings.TrimRight(buf.String(), "\n")), nil
}
