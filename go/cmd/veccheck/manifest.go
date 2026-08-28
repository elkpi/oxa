package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const manifestName = "manifest.json"

// manifestFile is the deterministic, timestamp-free manifest structure.
// Vectors are sorted by name before serialization, and no timestamp field is
// written, so the file is byte-stable across runs.
type manifestFile struct {
	Vectors []vector `json:"vectors"`
}

// writeManifestFile serializes the vectors to vectors/manifest.json.
func writeManifestFile(vectorsDir string, vectors []vector) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(manifestFile{Vectors: vectors}); err != nil {
		return err
	}
	path := filepath.Join(vectorsDir, manifestName)
	tmp := path + ".partial"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// checkManifestFile recomputes the manifest and compares it byte-for-byte
// with the committed one.
func checkManifestFile(vectorsDir string, vectors []vector) error {
	path := filepath.Join(vectorsDir, manifestName)
	committed, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("vectors/manifest.json missing or unreadable (run veccheck -write-manifest and commit it): %w", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(manifestFile{Vectors: vectors}); err != nil {
		return err
	}
	if !bytes.Equal(committed, buf.Bytes()) {
		return fmt.Errorf("vectors/manifest.json is stale (a vector was added, removed, renamed, or modified); regenerate it with veccheck -write-manifest and commit")
	}
	return nil
}

// sha256Hex hashes data and returns the lowercase hex digest.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
