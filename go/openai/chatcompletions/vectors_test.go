package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/elkpi/oxa/go/internal/vectest"
	"github.com/elkpi/oxa/go/ir"
)

// vectorConverter adapts the package-level conversion functions to the
// vectest.Converter surface: wire JSON is decoded into the face's wire types,
// IR results are marshaled through the canonical ir codec.
type vectorConverter struct {
	decoder        *StreamDecoder
	decoderFlushed bool
	encoder        *StreamEncoder
	encoderDone    bool
}

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

func newVectorStreamConverter() *vectorConverter {
	return &vectorConverter{
		decoder: NewStreamDecoder(),
		encoder: NewStreamEncoder(),
	}
}

func (c *vectorConverter) DecodeNativeEvent(raw json.RawMessage) ([]ir.Event, error) {
	if c.decoder == nil || c.decoderFlushed {
		c.decoder = NewStreamDecoder()
		c.decoderFlushed = false
	}
	var chunk Chunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return nil, err
	}
	return c.decoder.Feed(&chunk)
}

func (c *vectorConverter) FlushDecoder() ([]ir.Event, error) {
	events, err := c.decoder.Flush()
	if err == nil {
		c.decoderFlushed = true
	}
	return events, err
}

func (c *vectorConverter) DecoderLosses() []ir.Loss {
	return c.decoder.Losses()
}

func (c *vectorConverter) ApplyIREvent(ev ir.Event) ([]json.RawMessage, []ir.Loss, error) {
	if c.encoder == nil || c.encoderDone {
		c.encoder = NewStreamEncoder()
		c.encoderDone = false
	}
	chunks, losses, err := c.encoder.Apply(ev)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := ev.(ir.MessageDone); ok {
		c.encoderDone = true
	}
	out := make([]json.RawMessage, 0, len(chunks))
	for _, chunk := range chunks {
		raw, err := json.Marshal(chunk)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, raw)
	}
	return out, losses, nil
}

func TestVectors(t *testing.T) {
	vectest.Run(t, vectorConverter{})
	vectest.RunStream(t, newVectorStreamConverter())
}
