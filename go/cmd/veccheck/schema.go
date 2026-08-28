package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemas holds the compiled vector and IR schemas plus the spec version
// pinned by the IR schema's specVersion const.
type schemas struct {
	vector      *jsonschema.Schema
	ir          *jsonschema.Schema
	specVersion string
}

// loadSchemas compiles vector.schema.json, ir.schema.json, and loss.schema.json
// against the 2020-12 meta-schema and reads the specVersion const from the IR
// schema's request document definition. The version is read, never hardcoded.
func loadSchemas(root string) (*schemas, error) {
	dir := filepath.Join(root, schemaRelPath)
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{"vector.schema.json", "ir.schema.json", "loss.schema.json"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("cannot open schema %s: %w", name, err)
		}
		doc, err := jsonschema.UnmarshalJSON(f)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("cannot parse schema %s: %w", name, err)
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
	version, err := readSpecVersion(filepath.Join(dir, "ir.schema.json"))
	if err != nil {
		return nil, err
	}
	return &schemas{vector: vectorSchema, ir: irSchema, specVersion: version}, nil
}

// readSpecVersion extracts the specVersion const from the IR schema. It reads
// $defs.request.properties.specVersion.const; the response and eventStream
// consts must agree (they are cross-checked below).
func readSpecVersion(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Const any `json:"const"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("ir.schema.json is not valid JSON: %w", err)
	}
	var version string
	for _, defName := range []string{"request", "response", "eventStream"} {
		def, ok := doc.Defs[defName]
		if !ok {
			return "", fmt.Errorf("ir.schema.json: missing $defs.%s", defName)
		}
		prop, ok := def.Properties["specVersion"]
		if !ok {
			return "", fmt.Errorf("ir.schema.json: $defs.%s has no specVersion property", defName)
		}
		s, ok := prop.Const.(string)
		if !ok || s == "" {
			return "", fmt.Errorf("ir.schema.json: $defs.%s.properties.specVersion.const is not a non-empty string", defName)
		}
		if version == "" {
			version = s
		} else if version != s {
			return "", fmt.Errorf("ir.schema.json: specVersion consts disagree: %q vs %q", version, s)
		}
	}
	if version == "" {
		return "", fmt.Errorf("ir.schema.json: no specVersion const found")
	}
	return version, nil
}
