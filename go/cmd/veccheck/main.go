// Command veccheck validates the oxa golden vectors under <root>/vectors
// against spec/schema/vector.schema.json, and the IR sides of each vector
// against spec/schema/ir.schema.json. It also generates and verifies
// vectors/manifest.json.
//
// Flags:
//
//	-root           repo root (default ".")
//	-write-manifest write vectors/manifest.json from the current vector set
//	-check-manifest fail if vectors/manifest.json is missing or stale
//	-schema-only    only verify the schemas compile against 2020-12, then exit
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	root := flag.String("root", ".", "repository root (the directory containing vectors/ and spec/)")
	writeManifest := flag.Bool("write-manifest", false, "write vectors/manifest.json and exit")
	checkManifest := flag.Bool("check-manifest", false, "verify vectors/manifest.json is present and up to date")
	schemaOnly := flag.Bool("schema-only", false, "only validate that the schemas compile against the 2020-12 meta-schema")
	flag.Parse()

	if err := run(*root, *writeManifest, *checkManifest, *schemaOnly); err != nil {
		fmt.Fprintf(os.Stderr, "veccheck: %v\n", err)
		os.Exit(1)
	}
}
