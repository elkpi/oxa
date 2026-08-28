package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			var ir any
			if err := json.Unmarshal(irRaw, &ir); err != nil {
				errs = append(errs, fmt.Errorf("%s: %s is not valid JSON: %v", display, irField, err))
			} else if err := s.ir.Validate(ir); err != nil {
				errs = append(errs, fmt.Errorf("%s: %s does not validate against ir.schema.json: %v", display, irField, err))
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
