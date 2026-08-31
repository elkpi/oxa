package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/elkpi/oxa/go/ir"
)

// vector is one parsed vector file; it doubles as the manifest entry.
type vector struct {
	Name   string   `json:"name"`
	File   string   `json:"file"`
	Tags   []string `json:"tags"`
	SHA256 string   `json:"sha256"`

	// fields used during checking only, not part of the manifest
	specVersion string
	conversion  string
	mode        string
}

// checkVectorFile validates one vector file and returns its parsed summary
// (nil if it could not be read or parsed at all) plus the errors found.
func checkVectorFile(s *schemas, vectorsDir, relFile string, names map[string]bool) (*vector, []error) {
	display := filepath.Join("vectors", relFile)

	raw, err := os.ReadFile(filepath.Join(vectorsDir, relFile))
	if err != nil {
		return nil, []error{fmt.Errorf("%s: cannot read: %w", display, err)}
	}

	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, []error{fmt.Errorf("%s: invalid JSON: %v", display, err)}
	}

	var errs []error
	if err := s.vector.Validate(generic); err != nil {
		errs = append(errs, fmt.Errorf("%s: does not validate against vector.schema.json: %v", display, err))
	}

	var doc struct {
		Name           string          `json:"name"`
		SpecVersion    string          `json:"spec_version"`
		Mode           string          `json:"mode"`
		Conversion     string          `json:"conversion"`
		Tags           []string        `json:"tags"`
		Input          json.RawMessage `json:"input"`
		ExpectedIR     json.RawMessage `json:"expected_ir"`
		ExpectedOutput json.RawMessage `json:"expected_output"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, append(errs, fmt.Errorf("%s: re-decoding failed: %v", display, err))
	}

	// name == dotted relative path
	wantName := dottedName(relFile)
	if doc.Name != wantName {
		errs = append(errs, fmt.Errorf("%s: name %q does not match dotted file path %q", display, doc.Name, wantName))
	}
	// name uniqueness
	if names[doc.Name] {
		errs = append(errs, fmt.Errorf("%s: duplicate vector name %q", display, doc.Name))
	}
	names[doc.Name] = true
	// spec_version matches the IR schema's specVersion const
	if doc.SpecVersion != s.specVersion {
		errs = append(errs, fmt.Errorf("%s: spec_version %q does not match ir.schema.json specVersion const %q", display, doc.SpecVersion, s.specVersion))
	}

	// IR-side validation: expected_ir for to-ir, input for from-ir
	var irField string
	var irRaw json.RawMessage
	switch doc.Conversion {
	case "to-ir":
		irField, irRaw = "expected_ir", doc.ExpectedIR
	case "from-ir":
		irField, irRaw = "input", doc.Input
	}
	if irField != "" {
		if len(irRaw) == 0 {
			errs = append(errs, fmt.Errorf("%s: %s is missing", display, irField))
		} else {
			var irDoc any
			if err := json.Unmarshal(irRaw, &irDoc); err != nil {
				errs = append(errs, fmt.Errorf("%s: %s is not valid JSON: %v", display, irField, err))
			} else {
				if err := s.ir.Validate(irDoc); err != nil {
					errs = append(errs, fmt.Errorf("%s: %s does not validate against ir.schema.json: %v", display, irField, err))
				}
				if doc.Mode == "stream" {
					es, err := ir.UnmarshalEventStream(irRaw)
					if err != nil {
						errs = append(errs, fmt.Errorf("%s: %s cannot decode event stream: %v", display, irField, err))
					} else if err := validateEventStream(es); err != nil {
						errs = append(errs, fmt.Errorf("%s: %s violates stream invariants: %v", display, irField, err))
					}
				}
			}
		}
	}

	v := &vector{
		Name:        doc.Name,
		File:        filepath.ToSlash(relFile),
		Tags:        doc.Tags,
		SHA256:      sha256Hex(raw),
		specVersion: doc.SpecVersion,
		conversion:  doc.Conversion,
		mode:        doc.Mode,
	}
	return v, errs
}

// dottedName converts a relative file path to the vector name convention:
// slashes become dots, the .json suffix is dropped.
func dottedName(rel string) string {
	rel = strings.TrimSuffix(rel, ".json")
	return strings.ReplaceAll(filepath.ToSlash(rel), "/", ".")
}

// validateEventStream checks the relational invariants that JSON Schema cannot
// express for a decoded IR event stream. Tool input and argument fragments are
// decoded only as their outer JSON string tokens; their contents remain opaque.
func validateEventStream(es *ir.EventStream) error {
	if es == nil {
		return fmt.Errorf("events: event stream is nil")
	}
	if len(es.Events) == 0 {
		return fmt.Errorf("events: missing message_start")
	}

	type openBlock struct {
		index int
		kind  string
		input string
		parts []string
	}

	var open *openBlock
	nextIndex := 0
	messageDeltaSeen := false
	messageDoneSeen := false

	for i, event := range es.Events {
		path := fmt.Sprintf("events[%d]", i)
		if messageDoneSeen {
			return fmt.Errorf("%s: event follows message_done", path)
		}
		if i == 0 {
			if _, ok := event.(ir.MessageStart); !ok {
				return fmt.Errorf("%s: first event must be message_start", path)
			}
			continue
		}

		switch e := event.(type) {
		case ir.MessageStart:
			return fmt.Errorf("%s: message_start must occur exactly once and first", path)
		case ir.ContentBlockStart:
			if messageDeltaSeen {
				return fmt.Errorf("%s: content_block_start follows message_delta", path)
			}
			if open != nil {
				return fmt.Errorf("%s: content_block_start while block index %d is open", path, open.index)
			}
			if e.Index != nextIndex {
				return fmt.Errorf("%s.index: want %d, got %d", path, nextIndex, e.Index)
			}

			block := &openBlock{index: e.Index}
			switch b := e.Block.(type) {
			case ir.TextBlock:
				block.kind = "text"
			case ir.ToolUseBlock:
				input, err := decodeEventString(b.Input)
				if err != nil {
					return fmt.Errorf("%s.block.input: %w", path, err)
				}
				block.kind = "tool_use"
				block.input = input
			default:
				return fmt.Errorf("%s.block: unsupported stream block type %T", path, e.Block)
			}
			open = block
		case ir.ContentBlockDelta:
			if messageDeltaSeen {
				return fmt.Errorf("%s: content_block_delta follows message_delta", path)
			}
			if open == nil {
				return fmt.Errorf("%s: content_block_delta requires an open block", path)
			}
			if e.Index != open.index {
				return fmt.Errorf("%s.index: want open block index %d, got %d", path, open.index, e.Index)
			}
			switch d := e.Delta.(type) {
			case ir.TextDelta:
				if open.kind != "text" {
					return fmt.Errorf("%s.delta: text_delta requires a text block, got %s", path, open.kind)
				}
			case ir.InputJSONDelta:
				if open.kind != "tool_use" {
					return fmt.Errorf("%s.delta: input_json_delta requires a tool_use block, got %s", path, open.kind)
				}
				part, err := decodeEventString(d.PartialJSON)
				if err != nil {
					return fmt.Errorf("%s.delta.partial_json: %w", path, err)
				}
				open.parts = append(open.parts, part)
			default:
				return fmt.Errorf("%s.delta: unsupported delta type %T", path, e.Delta)
			}
		case ir.ContentBlockStop:
			if messageDeltaSeen {
				return fmt.Errorf("%s: content_block_stop follows message_delta", path)
			}
			if open == nil {
				return fmt.Errorf("%s: content_block_stop requires an open block", path)
			}
			if e.Index != open.index {
				return fmt.Errorf("%s.index: want open block index %d, got %d", path, open.index, e.Index)
			}
			if open.kind == "tool_use" && len(open.parts) > 0 {
				joined := strings.Join(open.parts, "")
				if joined != open.input {
					return fmt.Errorf("%s.input: joined input fragments %q do not equal tool input %q", path, joined, open.input)
				}
			}
			open = nil
			nextIndex++
		case ir.MessageDelta:
			if open != nil {
				return fmt.Errorf("%s: message_delta requires all blocks to be stopped; block index %d is open", path, open.index)
			}
			if messageDeltaSeen {
				return fmt.Errorf("%s: duplicate message_delta", path)
			}
			messageDeltaSeen = true
		case ir.MessageDone:
			if !messageDeltaSeen {
				return fmt.Errorf("%s: message_done must immediately follow message_delta", path)
			}
			messageDoneSeen = true
		default:
			return fmt.Errorf("%s: unsupported event type %T", path, event)
		}
	}

	if open != nil {
		return fmt.Errorf("events: block index %d is not stopped", open.index)
	}
	if !messageDeltaSeen {
		return fmt.Errorf("events: missing message_delta")
	}
	if !messageDoneSeen {
		return fmt.Errorf("events: missing message_done")
	}
	return nil
}

func decodeEventString(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", fmt.Errorf("must be a JSON string token")
	}
	var value string
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return "", fmt.Errorf("must be a JSON string token: %v", err)
	}
	return value, nil
}
