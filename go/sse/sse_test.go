package sse

import (
	"bytes"
	"errors"
	"testing"
	"testing/iotest"
)

func TestDecodeLF(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString("data: hello\n\ndata: world\n\n"))
	for _, want := range []string{"hello", "world"} {
		f, ok, err := d.Next()
		if err != nil || !ok {
			t.Fatalf("Next: ok=%v err=%v want %q", ok, err, want)
		}
		if f.Event != "" || string(f.Data) != want {
			t.Fatalf("frame = %+v want data %q", f, want)
		}
	}
	if _, ok, err := d.Next(); ok || err != nil {
		t.Fatalf("after last frame: ok=%v err=%v", ok, err)
	}
}

func TestDecodeCRLFAndMixed(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString("data: a\r\n\r\ndata: b\n\r\n"))
	for _, want := range []string{"a", "b"} {
		f, ok, err := d.Next()
		if err != nil || !ok || string(f.Data) != want {
			t.Fatalf("frame=%q ok=%v err=%v want %q", f.Data, ok, err, want)
		}
	}
	if _, ok, _ := d.Next(); ok {
		t.Fatal("expected EOF")
	}
}

func TestDecodeMultiLineData(t *testing.T) {
	in := "data: line1\ndata: line2\n\n"
	f, ok, err := NewDecoder(bytes.NewBufferString(in)).Next()
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if want := "line1\nline2"; string(f.Data) != want {
		t.Fatalf("Data=%q want %q", f.Data, want)
	}
}

func TestDecodeEventFieldLastWins(t *testing.T) {
	in := "event: first\nevent: second\ndata: x\n\n"
	f, ok, err := NewDecoder(bytes.NewBufferString(in)).Next()
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if f.Event != "second" || string(f.Data) != "x" {
		t.Fatalf("frame=%+v", f)
	}
}

func TestDecodeCommentsAndUnknownFields(t *testing.T) {
	in := ": keepalive\nid: 42\nretry: 1000\nbogus: v\ndata: x\n\n"
	f, ok, err := NewDecoder(bytes.NewBufferString(in)).Next()
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if f.Event != "" || string(f.Data) != "x" {
		t.Fatalf("frame=%+v", f)
	}
}

func TestDecodeEmptyData(t *testing.T) {
	f, ok, err := NewDecoder(bytes.NewBufferString("data:\n\n")).Next()
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(f.Data) != 0 {
		t.Fatalf("Data=%q want empty", f.Data)
	}
	// "data: " with trailing space is also empty after stripping the space.
	f, ok, err = NewDecoder(bytes.NewBufferString("data: \n\n")).Next()
	if err != nil || !ok || len(f.Data) != 0 {
		t.Fatalf("ok=%v err=%v Data=%q", ok, err, f.Data)
	}
}

func TestDecodeDonePassthrough(t *testing.T) {
	f, ok, err := NewDecoder(bytes.NewBufferString("data: [DONE]\n\n")).Next()
	if err != nil || !ok || string(f.Data) != "[DONE]" {
		t.Fatalf("ok=%v err=%v Data=%q", ok, err, f.Data)
	}
}

func TestDecodeSplitReads(t *testing.T) {
	in := "event: msg\ndata: hello world\n\ndata: second\n\n"
	d := NewDecoder(iotest.OneByteReader(bytes.NewBufferString(in)))
	for i, want := range []string{"hello world", "second"} {
		f, ok, err := d.Next()
		if err != nil || !ok || string(f.Data) != want {
			t.Fatalf("frame %d: ok=%v err=%v Data=%q want %q", i, ok, err, f.Data, want)
		}
		if i == 0 && f.Event != "msg" {
			t.Fatalf("Event=%q want msg", f.Event)
		}
	}
	if _, ok, _ := d.Next(); ok {
		t.Fatal("expected EOF")
	}
}

func TestNextAfterEOFStaysEOF(t *testing.T) {
	d := NewDecoder(bytes.NewBufferString("data: x\n\n"))
	if _, ok, err := d.Next(); !ok || err != nil {
		t.Fatalf("first Next: ok=%v err=%v", ok, err)
	}
	for i := 0; i < 3; i++ {
		if _, ok, err := d.Next(); ok || err != nil {
			t.Fatalf("Next after EOF #%d: ok=%v err=%v", i, ok, err)
		}
	}
}

func TestDecodeReaderError(t *testing.T) {
	want := errors.New("boom")
	d := NewDecoder(iotest.ErrReader(want))
	_, ok, err := d.Next()
	if ok || !errors.Is(err, want) {
		t.Fatalf("ok=%v err=%v want %v", ok, err, want)
	}
}

func TestDecodeTrailingFrameWithoutBlankLine(t *testing.T) {
	f, ok, err := NewDecoder(bytes.NewBufferString("data: tail")).Next()
	if err != nil || !ok || string(f.Data) != "tail" {
		t.Fatalf("ok=%v err=%v Data=%q", ok, err, f.Data)
	}
}

func TestEncode(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, Frame{Event: "e", Data: []byte("a\nb")}); err != nil {
		t.Fatal(err)
	}
	want := "event: e\ndata: a\ndata: b\n\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

func TestEncodeEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, Frame{}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "data: \n\n" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestEncodeWriteError(t *testing.T) {
	want := errors.New("short write")
	var w errWriter
	w.err = want
	if err := Encode(&w, Frame{Data: []byte("x")}); !errors.Is(err, want) {
		t.Fatalf("err=%v want %v", err, want)
	}
}

type errWriter struct{ err error }

func (w *errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRoundTrip(t *testing.T) {
	frames := []Frame{
		{Event: "", Data: []byte("hello")},
		{Event: "delta", Data: []byte("line1\nline2")},
		{Event: "done", Data: []byte("[DONE]")},
		{Event: "empty", Data: nil},
		{Data: []byte("no event")},
	}
	var buf bytes.Buffer
	for _, f := range frames {
		if err := Encode(&buf, f); err != nil {
			t.Fatal(err)
		}
	}
	d := NewDecoder(&buf)
	for i, want := range frames {
		f, ok, err := d.Next()
		if err != nil || !ok {
			t.Fatalf("frame %d: ok=%v err=%v", i, ok, err)
		}
		if f.Event != want.Event || !bytes.Equal(f.Data, want.Data) {
			t.Fatalf("frame %d: got %+v want %+v", i, f, want)
		}
	}
	if _, ok, _ := d.Next(); ok {
		t.Fatal("expected EOF")
	}
}
