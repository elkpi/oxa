package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/oxa-protocol/oxa/go/internal/vectest"
	"github.com/oxa-protocol/oxa/go/ir"
)

// vectorConverter adapts the package-level conversion functions to the
// vectest.Converter surface: wire JSON is decoded into the face's wire types,
// IR results are marshaled through the canonical ir codec.
type vectorConverter struct{}

func (vectorConverter) Face() string { return "chatcompletions" }

func (vectorConverter) DecodeRequestWire(raw json.RawMessage) (*ir.Request, []ir.Loss, error) {
	var wire Request
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, nil, err
	}
	return DecodeRequest(&wire)
}

func (vectorConverter) DecodeResponseWire(raw json.RawMessage) (*ir.Response, []ir.Loss, error) {
	var wire Response
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, nil, err
	}
	return DecodeResponse(&wire)
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
