package vectest

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/elkpi/oxa/go/ir"
)

// StreamConverter is the face-implementation surface that RunStream drives.
// Native stream JSON is intentionally decoded and encoded by face-local test
// adapters, so this generic package never constructs provider wire types.
type StreamConverter interface {
	// Face is the vectors/ directory name of the face.
	Face() string
	DecodeNativeEvent(json.RawMessage) ([]ir.Event, error)
	FlushDecoder() ([]ir.Event, error)
	DecoderLosses() []ir.Loss
	ApplyIREvent(ir.Event) ([]json.RawMessage, []ir.Loss, error)
}

// streamVectorResetter is an optional test-adapter capability. RunStream calls
// it before each vector so that a converter remains isolated even when a prior
// vector ends with a conversion error.
type streamVectorResetter interface {
	ResetStreamVector()
}

// RunStream executes every stream golden vector for conv's face. It skips when
// the vectors repository cannot be located (dependency mode), but requires at
// least one stream vector when running from the monorepo.
func RunStream(t *testing.T, conv StreamConverter) {
	t.Helper()
	root := FindRepoRoot(".")
	if root == "" {
		t.Skip("repo root not found; vector tests skipped (dependency mode)")
	}
	vectors, err := LoadVectors(root, conv.Face(), "stream")
	if err != nil {
		t.Fatalf("load stream vectors for face %s: %v", conv.Face(), err)
	}
	if len(vectors) == 0 {
		t.Fatalf("no stream vectors found for face %s; the harness must execute at least one", conv.Face())
	}
	for _, v := range vectors {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			if resetter, ok := conv.(streamVectorResetter); ok {
				resetter.ResetStreamVector()
			}
			switch v.Conversion {
			case "to-ir":
				actual, losses, err := runStreamToIR(conv, v.Input)
				if err != nil {
					t.Fatalf("decode stream failed: %v", err)
				}
				if err := CompareJSON(v.ExpectedIR, actual); err != nil {
					t.Errorf("expected_ir mismatch: %v\nexpected: %s\nactual:   %s", err, v.ExpectedIR, actual)
				}
				compareLosses(t, v, losses)
			case "from-ir":
				if _, err := unmarshalNativeEvents(v.ExpectedOut); err != nil {
					t.Fatalf("expected_output events envelope: %v", err)
				}
				actual, losses, err := runStreamFromIR(conv, v.Input)
				if err != nil {
					t.Fatalf("encode stream failed: %v", err)
				}
				if err := CompareJSON(v.ExpectedOut, actual); err != nil {
					t.Errorf("expected_output mismatch: %v\nexpected: %s\nactual:   %s", err, v.ExpectedOut, actual)
				}
				compareLosses(t, v, losses)
			default:
				t.Fatalf("unknown conversion %q", v.Conversion)
			}
		})
	}
	t.Logf("vectest: executed %d stream vectors for face %s", len(vectors), conv.Face())
}

func runStreamToIR(conv StreamConverter, input json.RawMessage) (json.RawMessage, []ir.Loss, error) {
	nativeEvents, err := unmarshalNativeEvents(input)
	if err != nil {
		return nil, nil, err
	}

	events := make([]ir.Event, 0, len(nativeEvents))
	for i, raw := range nativeEvents {
		decoded, err := conv.DecodeNativeEvent(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("decode native event %d (%s): %w", i, boundedNativeEvent(raw), err)
		}
		events = append(events, decoded...)
	}
	flushed, err := conv.FlushDecoder()
	if err != nil {
		return nil, nil, fmt.Errorf("flush stream decoder: %w", err)
	}
	events = append(events, flushed...)
	losses := conv.DecoderLosses()

	out, err := ir.MarshalEventStream(&ir.EventStream{Events: events})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal IR event stream: %w", err)
	}
	return out, losses, nil
}

func runStreamFromIR(conv StreamConverter, input json.RawMessage) (json.RawMessage, []ir.Loss, error) {
	stream, err := ir.UnmarshalEventStream(input)
	if err != nil {
		return nil, nil, fmt.Errorf("unmarshal IR event stream: %w", err)
	}

	nativeEvents := make([]json.RawMessage, 0, len(stream.Events))
	var losses []ir.Loss
	for i, ev := range stream.Events {
		encoded, reported, err := conv.ApplyIREvent(ev)
		if err != nil {
			return nil, nil, fmt.Errorf("apply IR event %d: %w", i, err)
		}
		nativeEvents = append(nativeEvents, encoded...)
		losses = append(losses, reported...)
	}
	out, err := json.Marshal(struct {
		Events []json.RawMessage `json:"events"`
	}{Events: nativeEvents})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal native event envelope: %w", err)
	}
	return out, losses, nil
}

const maxNativeEventErrorBytes = 512

func boundedNativeEvent(raw json.RawMessage) string {
	if len(raw) <= maxNativeEventErrorBytes {
		return string(raw)
	}
	return string(raw[:maxNativeEventErrorBytes]) + "…"
}

func unmarshalNativeEvents(raw json.RawMessage) ([]json.RawMessage, error) {
	var envelope struct {
		Events *json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal events envelope: %w", err)
	}
	if envelope.Events == nil {
		return nil, fmt.Errorf("missing events envelope")
	}
	var events []json.RawMessage
	if err := json.Unmarshal(*envelope.Events, &events); err != nil {
		return nil, fmt.Errorf("events is not an array: %w", err)
	}
	if events == nil {
		return nil, fmt.Errorf("events must be an array")
	}
	return events, nil
}
