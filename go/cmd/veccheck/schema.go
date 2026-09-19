package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemas holds the compiled vector and IR schemas plus the allowed spec versions
// declared by the IR schema's specVersion const or enum.
type schemas struct {
	vector       *jsonschema.Schema
	ir           *jsonschema.Schema
	specVersions []string
	specVersion  string // latest declared spec version
}

func (s *schemas) isValidSpecVersion(v string) bool {
	for _, sv := range s.specVersions {
		if sv == v {
			return true
		}
	}
	return false
}

// loadSchemas compiles vector.schema.json, ir.schema.json, and loss.schema.json
// against the 2020-12 meta-schema and reads the specVersion const or enum from
// the IR schema's document definitions. The version is read, never hardcoded.
func loadSchemas(root string) (*schemas, error) {
	dir := filepath.Join(root, schemaRelPath)
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{"vector.schema.json", "ir.schema.json", "loss.schema.json"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("cannot open schema %s: %w", name, err)
		}
		doc, parseErr := jsonschema.UnmarshalJSON(f)
		closeErr := f.Close()
		if parseErr != nil {
			return nil, fmt.Errorf("cannot parse schema %s: %w", name, parseErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("cannot close schema %s: %w", name, closeErr)
		}
		if err := compiler.AddResource(name, doc); err != nil {
			return nil, fmt.Errorf("cannot load schema %s: %w", name, err)
		}
		// Also register the schema under its $id so cross-file $refs like
		// vector.schema.json's "$ref": "loss.schema.json" (an $id URL) resolve
		// locally instead of being fetched over the network.
		if m, ok := doc.(map[string]any); ok {
			if id, ok := m["$id"].(string); ok && id != "" {
				if err := compiler.AddResource(id, doc); err != nil {
					return nil, fmt.Errorf("cannot load schema %s under its $id: %w", name, err)
				}
			}
		}
	}
	vectorSchema, err := compiler.Compile("vector.schema.json")
	if err != nil {
		return nil, fmt.Errorf("vector.schema.json does not compile: %w", err)
	}
	irSchema, err := compiler.Compile("ir.schema.json")
	if err != nil {
		return nil, fmt.Errorf("ir.schema.json does not compile: %w", err)
	}
	if _, err := compiler.Compile("loss.schema.json"); err != nil {
		return nil, fmt.Errorf("loss.schema.json does not compile: %w", err)
	}
	versions, err := readSpecVersions(filepath.Join(dir, "ir.schema.json"))
	if err != nil {
		return nil, err
	}
	latest := versions[len(versions)-1]
	return &schemas{vector: vectorSchema, ir: irSchema, specVersions: versions, specVersion: latest}, nil
}

// readSpecVersions extracts the allowed specVersion values from the IR schema.
// It accepts either a const string or an enum array of strings. All three
// document definitions (request, response, eventStream) must agree.
func readSpecVersions(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Const any   `json:"const"`
				Enum  []any `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("ir.schema.json is not valid JSON: %w", err)
	}
	var versions []string
	for _, defName := range []string{"request", "response", "eventStream"} {
		def, ok := doc.Defs[defName]
		if !ok {
			return nil, fmt.Errorf("ir.schema.json: missing $defs.%s", defName)
		}
		prop, ok := def.Properties["specVersion"]
		if !ok {
			return nil, fmt.Errorf("ir.schema.json: $defs.%s has no specVersion property", defName)
		}
		var cur []string
		if s, ok := prop.Const.(string); ok && s != "" {
			cur = []string{s}
		} else if len(prop.Enum) > 0 {
			for _, item := range prop.Enum {
				if str, ok := item.(string); ok && str != "" {
					cur = append(cur, str)
				}
			}
		}
		if len(cur) == 0 {
			return nil, fmt.Errorf("ir.schema.json: $defs.%s.properties.specVersion has neither const nor non-empty enum", defName)
		}
		if len(versions) == 0 {
			versions = cur
		} else {
			if len(versions) != len(cur) {
				return nil, fmt.Errorf("ir.schema.json: specVersion declarations disagree between defs")
			}
			for i := range versions {
				if versions[i] != cur[i] {
					return nil, fmt.Errorf("ir.schema.json: specVersion declarations disagree between defs")
				}
			}
		}
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("ir.schema.json: no specVersion declarations found")
	}
	return versions, nil
}
