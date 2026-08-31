package vectest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elkpi/oxa/go/ir"
)

type fakeStreamConverter struct {
	face string

	decodeEvents [][]ir.Event
	decodeErr    error
	decodeCalls  []json.RawMessage

	flushEvents []ir.Event
	flushErr    error
	flushCalled bool

	decoderLosses          []ir.Loss
	lossesCalledAfterFlush bool

	applyEvents [][]json.RawMessage
	applyLosses [][]ir.Loss
	applyErr    error
	applied     []ir.Event
}

func (c *fakeStreamConverter) Face() string { return c.face }

func (c *fakeStreamConverter) DecodeNativeEvent(raw json.RawMessage) ([]ir.Event, error) {
	c.decodeCalls = append(c.decodeCalls, raw)
	if c.decodeErr != nil {
		return nil, c.decodeErr
	}
	return c.decodeEvents[len(c.decodeCalls)-1], nil
}

func (c *fakeStreamConverter) FlushDecoder() ([]ir.Event, error) {
	c.flushCalled = true
	return c.flushEvents, c.flushErr
}

func (c *fakeStreamConverter) DecoderLosses() []ir.Loss {
	c.lossesCalledAfterFlush = c.flushCalled
	return c.decoderLosses
}

func (c *fakeStreamConverter) ApplyIREvent(ev ir.Event) ([]json.RawMessage, []ir.Loss, error) {
	c.applied = append(c.applied, ev)
	if c.applyErr != nil {
		return nil, nil, c.applyErr
	}
	i := len(c.applied) - 1
	return c.applyEvents[i], c.applyLosses[i], nil
}

type resettingFakeStreamConverter struct {
	face               string
	resetCount         int
	dirty              bool
	secondStartedClean bool
}

func (c *resettingFakeStreamConverter) Face() string { return c.face }

func (c *resettingFakeStreamConverter) ResetStreamVector() {
	c.resetCount++
	c.dirty = false
}

func (c *resettingFakeStreamConverter) DecodeNativeEvent(raw json.RawMessage) ([]ir.Event, error) {
	var event struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, err
	}
	switch event.Event {
	case "first":
		c.dirty = true
	case "second":
		c.secondStartedClean = !c.dirty
	default:
		return nil, errors.New("unexpected fake event")
	}
	return []ir.Event{ir.MessageStart{ID: event.Event, Model: "model"}}, nil
}

func (c *resettingFakeStreamConverter) FlushDecoder() ([]ir.Event, error) {
	return []ir.Event{
		ir.MessageDelta{StopReason: ir.StopEndTurn},
		ir.MessageDone{},
	}, nil
}

func (*resettingFakeStreamConverter) DecoderLosses() []ir.Loss { return nil }

func (*resettingFakeStreamConverter) ApplyIREvent(ir.Event) ([]json.RawMessage, []ir.Loss, error) {
	return nil, nil, errors.New("unexpected ApplyIREvent")
}

func TestRunStreamToIR(t *testing.T) {
	root := setupStreamVectorRepo(t)
	conv := &fakeStreamConverter{
		face: "fake",
		decodeEvents: [][]ir.Event{
			{ir.MessageStart{ID: "stream-1", Model: "model-1"}},
			{ir.ContentBlockStart{Index: 0, Block: ir.TextBlock{Text: ""}}},
		},
		flushEvents: []ir.Event{
			ir.ContentBlockDelta{Index: 0, Delta: ir.TextDelta{Text: "hello"}},
			ir.ContentBlockStop{Index: 0},
			ir.MessageDelta{StopReason: ir.StopEndTurn, Usage: ir.Usage{InputTokens: 2, OutputTokens: 3}},
			ir.MessageDone{},
		},
		decoderLosses: []ir.Loss{{Path: "native", Field: "ignored", Reason: ir.LossUnmappedField}},
	}
	expectedIR := marshalEventStream(t, append(append([]ir.Event{}, conv.decodeEvents[0]...), append(conv.decodeEvents[1], conv.flushEvents...)...))
	input := marshalNativeEventEnvelope(t, []json.RawMessage{
		json.RawMessage(`{"event":"one"}`),
		json.RawMessage(`{"event":"two"}`),
	})
	writeStreamVector(t, root, conv.face, "to-ir.json", Vector{
		Name:           "fake.stream.to-ir",
		Mode:           "stream",
		Conversion:     "to-ir",
		Input:          input,
		ExpectedIR:     expectedIR,
		ExpectedLosses: []Loss{{Path: "native", Field: "ignored", Reason: "unmapped-field"}},
	})

	RunStream(t, conv)

	if len(conv.decodeCalls) != 2 {
		t.Fatalf("DecodeNativeEvent calls = %d, want 2", len(conv.decodeCalls))
	}
	if !conv.flushCalled {
		t.Error("FlushDecoder was not called")
	}
	if !conv.lossesCalledAfterFlush {
		t.Error("DecoderLosses was not called after FlushDecoder")
	}
}

func TestRunStreamResetsOptionalVectorState(t *testing.T) {
	root := setupStreamVectorRepo(t)
	conv := &resettingFakeStreamConverter{face: "resetting-fake"}
	for _, name := range []string{"first", "second"} {
		writeStreamVector(t, root, conv.face, name+".json", Vector{
			Name:       "resetting-fake.stream." + name,
			Mode:       "stream",
			Conversion: "to-ir",
			Input: marshalNativeEventEnvelope(t, []json.RawMessage{
				json.RawMessage(`{"event":"` + name + `"}`),
			}),
			ExpectedIR: marshalEventStream(t, []ir.Event{
				ir.MessageStart{ID: name, Model: "model"},
				ir.MessageDelta{StopReason: ir.StopEndTurn},
				ir.MessageDone{},
			}),
		})
	}

	RunStream(t, conv)

	if conv.resetCount != 2 {
		t.Fatalf("ResetStreamVector calls = %d, want 2", conv.resetCount)
	}
	if !conv.secondStartedClean {
		t.Error("second vector began with dirty state from the first vector")
	}
}

func TestRunStreamFromIR(t *testing.T) {
	root := setupStreamVectorRepo(t)
	conv := &fakeStreamConverter{
		face: "fake",
		applyEvents: [][]json.RawMessage{
			{json.RawMessage(`{"event":"start"}`)},
			{json.RawMessage(`{"event":"delta"}`)},
			{json.RawMessage(`{"event":"done"}`)},
		},
		applyLosses: [][]ir.Loss{
			nil,
			{{Path: "events[1]", Field: "normalization", Reason: ir.LossDegraded}},
			nil,
		},
	}
	input := marshalEventStream(t, []ir.Event{
		ir.MessageStart{ID: "stream-2", Model: "model-2"},
		ir.MessageDelta{StopReason: ir.StopEndTurn},
		ir.MessageDone{},
	})
	expectedOut := marshalNativeEventEnvelope(t, []json.RawMessage{
		json.RawMessage(`{"event":"start"}`),
		json.RawMessage(`{"event":"delta"}`),
		json.RawMessage(`{"event":"done"}`),
	})
	writeStreamVector(t, root, conv.face, "from-ir.json", Vector{
		Name:        "fake.stream.from-ir",
		Mode:        "stream",
		Conversion:  "from-ir",
		Input:       input,
		ExpectedOut: expectedOut,
		ExpectedLosses: []Loss{{
			Path: "events[1]", Field: "normalization", Reason: "degraded",
		}},
	})

	RunStream(t, conv)

	if len(conv.applied) != 3 {
		t.Fatalf("ApplyIREvent calls = %d, want 3", len(conv.applied))
	}
}

func TestRunStreamToIRIncludesNativeEventContext(t *testing.T) {
	conv := &fakeStreamConverter{
		decodeErr: errors.New("native decode failed"),
	}
	_, _, err := runStreamToIR(conv, json.RawMessage(`{"events":[{"type":"broken"}]}`))
	if err == nil {
		t.Fatal("runStreamToIR succeeded, want native conversion error")
	}
	want := `decode native event 0 ({"type":"broken"}): native decode failed`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("runStreamToIR error = %q, want substring %q", err, want)
	}
}

func TestRunStreamToIRTruncatesNativeEventContext(t *testing.T) {
	raw := json.RawMessage(`{"type":"` + strings.Repeat("x", 600) + `"}`)
	conv := &fakeStreamConverter{decodeErr: errors.New("native decode failed")}
	_, _, err := runStreamToIR(conv, marshalNativeEventEnvelope(t, []json.RawMessage{raw}))
	if err == nil {
		t.Fatal("runStreamToIR succeeded, want native conversion error")
	}
	want := string(raw[:512]) + "…"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("runStreamToIR error does not contain bounded raw event: %q", err)
	}
	if strings.Contains(err.Error(), string(raw)) {
		t.Fatalf("runStreamToIR error contains unbounded raw event (%d bytes)", len(raw))
	}
}

func setupStreamVectorRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("make fake git directory: %v", err)
	}
	t.Chdir(root)
	return root
}

func writeStreamVector(t *testing.T, root, face, fileName string, v Vector) {
	t.Helper()
	dir := filepath.Join(root, "vectors", face, "stream")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make vector directory: %v", err)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal vector: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), raw, 0o644); err != nil {
		t.Fatalf("write vector: %v", err)
	}
}

func marshalEventStream(t *testing.T, events []ir.Event) json.RawMessage {
	t.Helper()
	raw, err := ir.MarshalEventStream(&ir.EventStream{Events: events})
	if err != nil {
		t.Fatalf("marshal event stream: %v", err)
	}
	return raw
}

func marshalNativeEventEnvelope(t *testing.T, events []json.RawMessage) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(struct {
		Events []json.RawMessage `json:"events"`
	}{Events: events})
	if err != nil {
		t.Fatalf("marshal native event envelope: %v", err)
	}
	return raw
}
