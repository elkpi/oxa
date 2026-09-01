package vectest

import (
	"testing"

	"github.com/elkpi/oxa/go/ir"
)

// RunCross executes every nonstream cross-protocol vector whose source and
// target endpoints match the two converters' Face() protocol names. Each
// vector composes source decode -> IR -> target encode; the target wire
// output compares structurally and the reported losses are the decode
// losses followed by the encode losses, compared as an unordered set. A
// matched pair with no vectors fails the test.
func RunCross(t *testing.T, source, target Converter) {
	t.Helper()
	root := FindRepoRoot(".")
	if root == "" {
		t.Skip("repo root not found; vector tests skipped (dependency mode)")
	}
	vectors, err := LoadVectors(root, "cross", "nonstream")
	if err != nil {
		t.Fatalf("load cross vectors: %v", err)
	}
	matched := crossVectorsFor(source, target, vectors)
	if len(matched) == 0 {
		t.Fatalf("no cross vectors found for pair %s -> %s", source.Face(), target.Face())
	}
	for _, v := range matched {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			runCrossVector(t, source, target, v)
		})
	}
	t.Logf("vectest: executed %d cross vectors for %s -> %s", len(matched), source.Face(), target.Face())
}

// crossVectorsFor selects the vectors whose endpoints match the pair.
func crossVectorsFor(source, target Converter, vectors []Vector) []Vector {
	var out []Vector
	for _, v := range vectors {
		if v.Source.Protocol == source.Face() && v.Target.Protocol == target.Face() {
			out = append(out, v)
		}
	}
	return out
}

func runCrossVector(t *testing.T, source, target Converter, v Vector) {
	t.Helper()
	if v.Conversion != "protocol-to-protocol" {
		t.Fatalf("cross vector %s has conversion %q, want protocol-to-protocol", v.Name, v.Conversion)
	}
	var out []byte
	var decodeLosses, encodeLosses []ir.Loss
	var err error
	if v.isRequest() {
		var req *ir.Request
		req, decodeLosses, err = source.DecodeRequestWire(v.Input)
		if err == nil {
			out, encodeLosses, err = target.EncodeRequestIR(req)
		}
	} else {
		var resp *ir.Response
		resp, decodeLosses, err = source.DecodeResponseWire(v.Input)
		if err == nil {
			out, encodeLosses, err = target.EncodeResponseIR(resp)
		}
	}
	if err != nil {
		t.Fatalf("cross conversion failed: %v", err)
	}
	if err := CompareJSON(v.ExpectedOut, out); err != nil {
		t.Errorf("expected_output mismatch: %v\nexpected: %s\nactual:   %s", err, v.ExpectedOut, out)
	}
	losses := append(append([]ir.Loss{}, decodeLosses...), encodeLosses...)
	compareLosses(t, v, losses)
}
