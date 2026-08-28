package vectest

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa/go/ir"
)

// Converter is the face-implementation surface the harness drives. The four
// methods correspond exactly to the four conversion directions of a spoke:
//
//   - DecodeRequestWire / DecodeResponseWire take face wire JSON, return an
//     IR value plus losses (face -> IR);
//   - EncodeRequestIR / EncodeResponseIR take an IR value, return rendered
//     face wire JSON plus losses (IR -> face).
//
// The harness marshals through canonical JSON on both sides (IR documents via
// the ir codec, wire documents via the face's own json-tagged types), so all
// comparisons are JSON-sh comparisons; there is no reflect.DeepEqual on Go
// structs, which wire omitempty tags would make fragile.
type Converter interface {
	// Face is the vectors/ directory name of the face ("chatcompletions",
	// "anthropic", later "responses").
	Face() string
	DecodeRequestWire(wire json.RawMessage) (*ir.Request, []ir.Loss, error)
	DecodeResponseWire(wire json.RawMessage) (*ir.Response, []ir.Loss, error)
	EncodeRequestIR(req *ir.Request) (json.RawMessage, []ir.Loss, error)
	EncodeResponseIR(resp *ir.Response) (json.RawMessage, []ir.Loss, error)
}

// Run executes every nonstream golden vector of the converter's face. It
// skips (t.Skip) when the repo root cannot be located; inside the monorepo
// it must always run and must find at least one vector.
func Run(t *testing.T, conv Converter) {
	t.Helper()
	root := FindRepoRoot(".")
	if root == "" {
		t.Skip("repo root not found; vector tests skipped (dependency mode)")
	}
	vectors, err := LoadVectors(root, conv.Face(), "nonstream")
	if err != nil {
		t.Fatalf("load vectors for face %s: %v", conv.Face(), err)
	}
	if len(vectors) == 0 {
		t.Fatalf("no nonstream vectors found for face %s; the harness must execute at least one", conv.Face())
	}
	for _, v := range vectors {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			switch v.Conversion {
			case "to-ir":
				runToIR(t, conv, v)
			case "from-ir":
				runFromIR(t, conv, v)
			default:
				t.Fatalf("unknown conversion %q", v.Conversion)
			}
		})
	}
	t.Logf("vectest: executed %d nonstream vectors for face %s", len(vectors), conv.Face())
}

func runToIR(t *testing.T, conv Converter, v Vector) {
	t.Helper()
	var irDoc []byte
	var losses []ir.Loss
	var err error
	if v.isRequest() {
		var req *ir.Request
		req, losses, err = conv.DecodeRequestWire(v.Input)
		if err == nil {
			irDoc, err = ir.MarshalRequest(req)
		}
	} else {
		var resp *ir.Response
		resp, losses, err = conv.DecodeResponseWire(v.Input)
		if err == nil {
			irDoc, err = ir.MarshalResponse(resp)
		}
	}
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if err := CompareJSON(v.ExpectedIR, irDoc); err != nil {
		t.Errorf("expected_ir mismatch: %v\nexpected: %s\nactual:   %s", err, v.ExpectedIR, irDoc)
	}
	compareLosses(t, v, losses)
}

func runFromIR(t *testing.T, conv Converter, v Vector) {
	t.Helper()
	var out []byte
	var losses []ir.Loss
	var err error
	if v.isRequest() {
		var req *ir.Request
		req, err = ir.UnmarshalRequest(v.Input)
		if err == nil {
			out, losses, err = conv.EncodeRequestIR(req)
		}
	} else {
		var resp *ir.Response
		resp, err = ir.UnmarshalResponse(v.Input)
		if err == nil {
			out, losses, err = conv.EncodeResponseIR(resp)
		}
	}
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	if err := CompareJSON(v.ExpectedOut, out); err != nil {
		t.Errorf("expected_output mismatch: %v\nexpected: %s\nactual:   %s", err, v.ExpectedOut, out)
	}
	compareLosses(t, v, losses)
}

func compareLosses(t *testing.T, v Vector, reported []ir.Loss) {
	t.Helper()
	reportedLosses, err := ConvertLosses(reported)
	if err != nil {
		t.Fatalf("convert reported losses: %v", err)
	}
	if err := CompareLosses(v.ExpectedLosses, reportedLosses); err != nil {
		t.Errorf("losses mismatch: %v\nreported: %+v\nexpected: %+v", err, reported, v.ExpectedLosses)
	}
}
