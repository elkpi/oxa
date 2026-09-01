package vectest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/elkpi/oxa/go/ir"
)

// fakeCrossConverter is a programmable Converter for harness tests.
type fakeCrossConverter struct {
	face         string
	decodedReq   *ir.Request
	encodedReq   []byte
	decodedResp  *ir.Response
	encodedResp  []byte
	decodeLosses []ir.Loss
	encodeLosses []ir.Loss
	reqCalls     int
	respCalls    int
}

func (f *fakeCrossConverter) Face() string { return f.face }

func (f *fakeCrossConverter) DecodeRequestWire(json.RawMessage) (*ir.Request, []ir.Loss, error) {
	f.reqCalls++
	return f.decodedReq, f.decodeLosses, nil
}

func (f *fakeCrossConverter) DecodeResponseWire(json.RawMessage) (*ir.Response, []ir.Loss, error) {
	f.respCalls++
	return f.decodedResp, f.decodeLosses, nil
}

func (f *fakeCrossConverter) EncodeRequestIR(*ir.Request) (json.RawMessage, []ir.Loss, error) {
	return f.encodedReq, f.encodeLosses, nil
}

func (f *fakeCrossConverter) EncodeResponseIR(*ir.Response) (json.RawMessage, []ir.Loss, error) {
	return f.encodedResp, f.encodeLosses, nil
}

func writeCrossVector(t *testing.T, root, fileName string, v Vector) {
	t.Helper()
	dir := filepath.Join(root, "vectors", "cross", "nonstream")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make cross vector directory: %v", err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal vector: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), raw, 0o644); err != nil {
		t.Fatalf("write vector: %v", err)
	}
}

func crossTestVector(name, source, target string, request bool) Vector {
	v := Vector{
		Name:        "cross.nonstream." + name,
		Mode:        "nonstream",
		Conversion:  "protocol-to-protocol",
		Input:       json.RawMessage(`{"kind":"input"}`),
		ExpectedOut: json.RawMessage(`{"kind":"output"}`),
	}
	v.Source.Protocol = source
	v.Target.Protocol = target
	// Matches the fake converters' decode+encode losses so the harness's
	// concatenation and unordered-set comparison are both exercised.
	v.ExpectedLosses = []Loss{
		{Path: "in", Field: "f", Reason: "unmapped-field"},
		{Path: "params", Field: "g", Reason: "unmapped-field"},
	}
	if request {
		v.Tags = []string{"request"}
	} else {
		v.Tags = []string{"response"}
	}
	return v
}

func TestRunCrossComposesMatchedVectors(t *testing.T) {
	root := setupStreamVectorRepo(t)
	alpha := &fakeCrossConverter{
		face:         "alpha",
		decodedReq:   &ir.Request{Model: "m"},
		decodedResp:  &ir.Response{Model: "m"},
		decodeLosses: []ir.Loss{{Path: "in", Field: "f", Reason: ir.LossUnmappedField}},
	}
	beta := &fakeCrossConverter{
		face:         "beta",
		encodedReq:   []byte(`{"kind":"output"}`),
		encodedResp:  []byte(`{"kind":"output"}`),
		encodeLosses: []ir.Loss{{Path: "params", Field: "g", Reason: ir.LossUnmappedField}},
	}
	writeCrossVector(t, root, "alpha-to-beta-request.json", crossTestVector("alpha-to-beta-request", "alpha", "beta", true))
	writeCrossVector(t, root, "alpha-to-beta-response.json", crossTestVector("alpha-to-beta-response", "alpha", "beta", false))
	// Mismatched pair must be skipped by RunCross(t, alpha, beta).
	writeCrossVector(t, root, "beta-to-alpha-request.json", crossTestVector("beta-to-alpha-request", "beta", "alpha", true))

	RunCross(t, alpha, beta)

	if alpha.reqCalls != 1 || alpha.respCalls != 1 {
		t.Fatalf("alpha decode calls = (%d, %d), want (1, 1)", alpha.reqCalls, alpha.respCalls)
	}
}

func TestCrossVectorsForFiltersByPair(t *testing.T) {
	vectors := []Vector{
		crossTestVector("a-to-b-request", "alpha", "beta", true),
		crossTestVector("b-to-a-request", "beta", "alpha", true),
		crossTestVector("a-to-c-request", "alpha", "gamma", true),
	}
	alpha := &fakeCrossConverter{face: "alpha"}
	beta := &fakeCrossConverter{face: "beta"}
	matched := crossVectorsFor(alpha, beta, vectors)
	if len(matched) != 1 || matched[0].Name != "cross.nonstream.a-to-b-request" {
		t.Fatalf("crossVectorsFor() = %+v, want exactly a-to-b-request", matched)
	}
	if got := crossVectorsFor(alpha, &fakeCrossConverter{face: "delta"}, vectors); len(got) != 0 {
		t.Fatalf("crossVectorsFor() with unknown target = %+v, want empty", got)
	}
}
