// Package jev converts between the SystemOne Jev evaluation protocol and the
// oxa intermediate representation (spec/17). Jev is a client-side structured
// decision protocol: a request evaluates a state against a fixed set of
// questions and returns typed answers. Conversion projects evaluation
// semantics onto the IR conversation model deterministically, reporting
// losses for everything the projection cannot carry.
package jev

import "encoding/json"

// Question types on the Jev wire. box is experimental and out of scope.
const (
	QuestionTypeNoul   = "noul"
	QuestionTypeChoice = "choice"
	QuestionTypeScore  = "score"
	QuestionTypeBox    = "box"
)

// Name of the synthesized response-format schema (spec/17 §3).
const ResponseFormatName = "jev_answers"

// synthesizedID is the envelope identifier stamped onto decoded responses;
// the IR document schema requires a non-empty id and Jev carries none
// (spec/17 §4).
const synthesizedID = "jev"

// instructionTemplate is the fixed output-contract text appended to the
// system state (spec/17 §7.1).
const instructionTemplate = "Answer the structured questions that follow as a single JSON object. Keys MUST be exactly the question IDs given in the user message; values MUST match each question's type: noul answers are numbers in [0,1], choice answers are one of the listed option names, score answers are one of the listed level numbers. Output raw JSON only: no prose, no markdown fences."

// Request is the Jev wire request. State stays as raw JSON so decode can
// distinguish the string form (verbatim text) from object/array forms
// (canonical compact JSON, spec/17 §7.6).
type Request struct {
	Model     string              `json:"model"`
	State     json.RawMessage     `json:"state"`
	Questions map[string]Question `json:"questions"`
	Assets    json.RawMessage     `json:"assets,omitempty"`
}

// Question is one Jev question. Criteria stays raw: its shape depends on the
// question type (noul object, choice map, score array).
type Question struct {
	Type         string          `json:"type"`
	Instructions string          `json:"instructions,omitempty"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
}

// Response is the Jev wire response.
type Response struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   Usage                      `json:"usage"`
}

// Usage is the Jev token accounting.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
