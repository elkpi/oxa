package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVectorCheckerRejectsOrphanCustomResult(t *testing.T) {
	s := testSchemas(t)
	var doc any
	if err := json.Unmarshal([]byte(`{"specVersion":"0.2.0","model":"m","messages":[{"role":"user","content":[{"type":"custom_tool_result","tool_use_id":"orphan","output":""}]}]}`), &doc); err != nil {
		t.Fatal(err)
	}
	vector := map[string]any{"name": "orphan", "description": "custom association", "spec_version": "0.2.0", "mode": "nonstream", "conversion": "to-ir", "source": map[string]string{"protocol": "responses"}, "input": map[string]any{}, "expected_ir": doc, "expected_losses": []any{}, "tags": []string{"custom-tools"}}
	raw, err := json.Marshal(vector)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orphan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, errs := checkVectorFile(s, dir, "orphan.json", map[string]bool{})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "INV-3") {
		t.Fatalf("orphan custom result was not rejected: %v", errs)
	}
}
