// Package vectest is the Go harness that runs the oxa golden vectors
// (vectors/README.md) against a face implementation. It locates the repo
// root, loads the vectors of one face and mode, and compares actual outputs
// against the expected sides using the normative comparison rules.
package vectest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FindRepoRoot walks up from dir looking for a directory containing both
// vectors/ and .git/ (vectors/README.md "How implementations locate
// vectors"). It returns "" when none is found before the filesystem root
// (the module is being consumed as a dependency outside the monorepo).
func FindRepoRoot(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "vectors")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Vector is the subset of the vector file format (spec/schema/
// vector.schema.json) the harness consumes.
type Vector struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Mode           string          `json:"mode"`
	Conversion     string          `json:"conversion"` // to-ir | from-ir
	Input          json.RawMessage `json:"input"`
	ExpectedIR     json.RawMessage `json:"expected_ir"`
	ExpectedOut    json.RawMessage `json:"expected_output"`
	ExpectedLosses []Loss          `json:"expected_losses"`
	Tags           []string        `json:"tags"`
}

// Loss mirrors ir.Loss without importing the ir package; the harness is
// generic over face implementations but always compares losses as unordered
// sets keyed on (path, field, reason).
type Loss struct {
	Path   string `json:"path"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// LoadVectors reads every vector file of one face and mode from
// <root>/vectors/<face>/<mode>/*.json. The returned vectors are sorted by
// file name so failures are deterministic.
func LoadVectors(root, face, mode string) ([]Vector, error) {
	dir := filepath.Join(root, "vectors", face, mode)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Vector
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var v Vector
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		out = append(out, v)
	}
	return out, nil
}

// isRequest reports whether the vector exercises the request direction, based
// on its tags ("request" or "response"; every seed vector carries exactly one
// of them).
func (v Vector) isRequest() bool {
	for _, tag := range v.Tags {
		if tag == "response" {
			return false
		}
	}
	return true
}
