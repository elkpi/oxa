package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const schemaRelPath = "spec/schema"

// run executes the requested veccheck mode and returns an error on failure.
func run(root string, writeManifest, checkManifest, schemaOnly bool) error {
	schemas, err := loadSchemas(root)
	if err != nil {
		return err
	}
	fmt.Printf("schemas OK: vector, ir, loss compile against 2020-12 (spec_version const %s)\n", schemas.specVersion)

	if schemaOnly {
		return nil
	}
	if writeManifest && checkManifest {
		return errors.New("-write-manifest and -check-manifest are mutually exclusive")
	}

	vectorsDir := filepath.Join(root, "vectors")
	if _, err := os.Stat(vectorsDir); err != nil {
		return fmt.Errorf("vectors directory not found under root %q: %w", root, err)
	}

	files, err := listVectorFiles(vectorsDir)
	if err != nil {
		return err
	}

	names := map[string]bool{}
	vectors := make([]vector, 0, len(files))
	var checkErrs []error
	checks := 0

	for _, file := range files {
		v, errs := checkVectorFile(schemas, vectorsDir, file, names)
		checks += len(errs) + 1
		if len(errs) > 0 {
			checkErrs = append(checkErrs, errs...)
		}
		if v != nil {
			vectors = append(vectors, *v)
		}
	}

	sort.Slice(vectors, func(i, j int) bool { return vectors[i].Name < vectors[j].Name })

	switch {
	case writeManifest:
		if err := writeManifestFile(vectorsDir, vectors); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%d vectors)\n", filepath.Join(vectorsDir, manifestName), len(vectors))
	case checkManifest:
		if err := checkManifestFile(vectorsDir, vectors); err != nil {
			checkErrs = append(checkErrs, err)
			checks++
		} else {
			checks++
		}
	}

	if len(checkErrs) > 0 {
		for _, e := range checkErrs {
			fmt.Fprintf(os.Stderr, "FAIL: %v\n", e)
		}
		return fmt.Errorf("%d of %d checks failed", len(checkErrs), checks)
	}
	fmt.Printf("%d vectors, %d checks, OK\n", len(vectors), checks)
	return nil
}

// listVectorFiles returns the relative paths of all vector JSON files under
// dir, sorted, excluding manifest.json.
func listVectorFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == manifestName || filepath.Ext(d.Name()) != ".json" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
