package jev

import (
	"encoding/json"
	"fmt"

	"github.com/elkpi/oxa/go/v3/ir"
)

// DecodeRequestJSON parses raw request JSON, audits unknown properties
// (spec/02 §4) in source encounter order, and decodes into the IR.
func DecodeRequestJSON(raw []byte, opts ...Option) (*ir.Request, []ir.Loss, error) {
	root, err := decodeRawObject(raw, "request JSON")
	if err != nil {
		return nil, nil, err
	}
	wire := &Request{}
	var losses []ir.Loss
	for _, k := range orderedTopKeys(raw) {
		v := root[k]
		switch k {
		case "model":
			if err := json.Unmarshal(v, &wire.Model); err != nil {
				return nil, nil, fmt.Errorf("jev: request model: %w", err)
			}
		case "state":
			wire.State = v
		case "questions":
			qs, qLosses, err := parseQuestions(v)
			if err != nil {
				return nil, nil, err
			}
			wire.Questions = qs
			losses = append(losses, qLosses...)
		case "assets":
			wire.Assets = v
		default:
			losses = append(losses, ir.Loss{
				Path: k, Field: k, Reason: ir.LossUnmappedField,
				Detail: "unknown property on the Jev request",
			})
		}
	}
	req, more, err := DecodeRequest(wire, opts...)
	if err != nil {
		return nil, nil, err
	}
	return req, mergeSourceOrder(raw, losses, more), nil
}

func parseQuestions(raw json.RawMessage) (map[string]Question, []ir.Loss, error) {
	m, err := decodeRawObject(raw, "questions")
	if err != nil {
		return nil, nil, err
	}
	out := make(map[string]Question, len(m))
	var losses []ir.Loss
	for _, id := range sortedKeys(m) {
		qm, err := decodeRawObject(m[id], "question "+id)
		if err != nil {
			return nil, nil, err
		}
		var q Question
		for _, k := range sortedKeys(qm) {
			switch k {
			case "type":
				if err := json.Unmarshal(qm[k], &q.Type); err != nil {
					return nil, nil, fmt.Errorf("jev: question %s type: %w", id, err)
				}
			case "instructions":
				if err := json.Unmarshal(qm[k], &q.Instructions); err != nil {
					return nil, nil, fmt.Errorf("jev: question %s instructions: %w", id, err)
				}
			case "criteria":
				q.Criteria = qm[k]
			default:
				losses = append(losses, ir.Loss{
					Path: "questions." + id, Field: k, Reason: ir.LossUnmappedField,
					Detail: "unknown property on the Jev question",
				})
			}
		}
		out[id] = q
	}
	return out, losses, nil
}

// DecodeResponseJSON parses raw response JSON, audits unknown properties in
// source encounter order, and decodes into the IR.
func DecodeResponseJSON(raw []byte, opts ...Option) (*ir.Response, []ir.Loss, error) {
	root, err := decodeRawObject(raw, "response JSON")
	if err != nil {
		return nil, nil, err
	}
	wire := &Response{}
	var losses []ir.Loss
	for _, k := range orderedTopKeys(raw) {
		v := root[k]
		switch k {
		case "model":
			if err := json.Unmarshal(v, &wire.Model); err != nil {
				return nil, nil, fmt.Errorf("jev: response model: %w", err)
			}
		case "answers":
			answers, aLosses, err := parseAnswers(v)
			if err != nil {
				return nil, nil, err
			}
			wire.Answers = answers
			losses = append(losses, aLosses...)
		case "usage":
			usage, uLosses, err := parseUsage(v)
			if err != nil {
				return nil, nil, err
			}
			wire.Usage = usage
			losses = append(losses, uLosses...)
		default:
			losses = append(losses, ir.Loss{
				Path: k, Field: k, Reason: ir.LossUnmappedField,
				Detail: "unknown property on the Jev response",
			})
		}
	}
	resp, more, err := DecodeResponse(wire, opts...)
	if err != nil {
		return nil, nil, err
	}
	return resp, mergeSourceOrder(raw, losses, more), nil
}

func parseAnswers(raw json.RawMessage) (map[string]json.RawMessage, []ir.Loss, error) {
	m, err := decodeRawObject(raw, "answers")
	if err != nil {
		return nil, nil, err
	}
	out := make(map[string]json.RawMessage, len(m))
	var losses []ir.Loss
	for _, id := range sortedKeys(m) {
		am, err := decodeRawObject(m[id], "answer "+id)
		if err != nil {
			return nil, nil, err
		}
		// Recognized fields carry verbatim; unrecognized properties are
		// removed, never passed through (spec/02 §4), and reported.
		filtered := make(map[string]json.RawMessage, len(am))
		for _, k := range sortedKeys(am) {
			switch k {
			case "type", "noul", "choice", "score", "legend", "probabilities", "confidence", "box":
				filtered[k] = am[k]
			default:
				losses = append(losses, ir.Loss{
					Path: "answers." + id, Field: k, Reason: ir.LossUnmappedField,
					Detail: "unknown property on the Jev answer; dropped",
				})
			}
		}
		filteredRaw, err := marshalAnswer(filtered)
		if err != nil {
			return nil, nil, err
		}
		out[id] = filteredRaw
	}
	return out, losses, nil
}

func parseUsage(raw json.RawMessage) (Usage, []ir.Loss, error) {
	m, err := decodeRawObject(raw, "usage")
	if err != nil {
		return Usage{}, nil, err
	}
	var usage Usage
	var losses []ir.Loss
	for _, k := range sortedKeys(m) {
		switch k {
		case "input_tokens":
			if err := json.Unmarshal(m[k], &usage.InputTokens); err != nil {
				return Usage{}, nil, fmt.Errorf("jev: usage input_tokens: %w", err)
			}
		case "output_tokens":
			if err := json.Unmarshal(m[k], &usage.OutputTokens); err != nil {
				return Usage{}, nil, fmt.Errorf("jev: usage output_tokens: %w", err)
			}
		default:
			losses = append(losses, ir.Loss{
				Path: "usage", Field: k, Reason: ir.LossUnmappedField,
				Detail: "unknown property on the Jev usage",
			})
		}
	}
	return usage, losses, nil
}

func decodeRawObject(raw []byte, what string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		if json.Valid(raw) {
			return nil, fmt.Errorf("jev: %s must be an object", what)
		}
		return nil, fmt.Errorf("jev: %s: %w", what, err)
	}
	if m == nil {
		return nil, fmt.Errorf("jev: %s must be an object", what)
	}
	return m, nil
}
