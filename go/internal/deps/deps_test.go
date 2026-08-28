// Package deps guards the dependency graph of the oxa Go module: the face
// converter packages (openai/chatcompletions, openai/responses once it
// exists, anthropic/messages) must import the ir package, the modelmap
// injection point, and the standard library only, and must never import each
// other (spec/00 hub architecture: every converter talks to the IR, never
// face to face).
package deps

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/elkpi/oxa/go"

// spokePackages lists the face converter packages that exist today. The
// Responses spoke joins when it lands (spec/00).
var spokePackages = []string{
	"./openai/chatcompletions",
	"./openai/responses",
	"./anthropic/messages",
}

// TestSpokesImportOnlyIRAndStdlib runs `go list -deps` over the spoke
// packages and asserts the dependency graph rules.
func TestSpokesImportOnlyIRAndStdlib(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	args := append([]string{"list", "-json"}, spokePackages...)
	cmd := exec.Command("go", args...)
	cmd.Dir = moduleRoot
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go list: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("go list: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	spokes := map[string]bool{}
	for _, p := range spokePackages {
		rel := strings.TrimPrefix(p, "./")
		spokes[modulePath+"/"+rel] = true
	}
	for {
		var pkg struct {
			ImportPath string   `json:"ImportPath"`
			Deps       []string `json:"Deps"`
		}
		if err := dec.Decode(&pkg); err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("decode go list output: %v", err)
		}
		if !spokes[pkg.ImportPath] {
			continue
		}
		for _, dep := range pkg.Deps {
			if dep == pkg.ImportPath {
				continue
			}
			switch {
			case dep == modulePath+"/ir", dep == modulePath+"/modelmap":
				// the hub and the modelmap injection point: allowed
			case strings.HasPrefix(dep, modulePath+"/"):
				// anything else inside the module is a violation: another
				// face, or an internal package the spokes must not touch
				t.Errorf("%s must not depend on %s (only ir, modelmap, and the standard library)",
					pkg.ImportPath, dep)
			case strings.Contains(strings.SplitN(dep, "/", 2)[0], "."):
				t.Errorf("%s must not depend on third-party package %s", pkg.ImportPath, dep)
			}
		}
	}
}

func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
