package jev

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa/go/v3/internal/vectest"
	"github.com/elkpi/oxa/go/v3/ir"
)

// vectorConverter adapts the package-level conversion functions to the
// vectest.Converter surface. Jev has no streaming mode, so only the four
// nonstream directions exist.
type vectorConverter struct{}

func (vectorConverter) Face() string { return "jev" }

func (vectorConverter) DecodeRequestWire(raw json.RawMessage) (*ir.Request, []ir.Loss, error) {
	return DecodeRequestJSON(raw)
}

func (vectorConverter) DecodeResponseWire(raw json.RawMessage) (*ir.Response, []ir.Loss, error) {
	return DecodeResponseJSON(raw)
}

func (vectorConverter) EncodeRequestIR(req *ir.Request) (json.RawMessage, []ir.Loss, error) {
	wire, losses, err := EncodeRequest(req)
	if err != nil {
		return nil, nil, err
	}
	out, err := json.Marshal(wire)
	if err != nil {
		return nil, nil, err
	}
	return out, losses, nil
}

func (vectorConverter) EncodeResponseIR(resp *ir.Response) (json.RawMessage, []ir.Loss, error) {
	wire, losses, err := EncodeResponse(resp)
	if err != nil {
		return nil, nil, err
	}
	out, err := json.Marshal(wire)
	if err != nil {
		return nil, nil, err
	}
	return out, losses, nil
}

func TestVectors(t *testing.T) {
	vectest.Run(t, vectorConverter{})
}
