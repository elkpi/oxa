package sse

import (
	"bytes"
	"testing"
)

// FuzzDecoder asserts that arbitrary input never panics or hangs the
// decoder, and that any decoded frames round-trip through Encode.
func FuzzDecoder(f *testing.F) {
	seeds := []string{
		"data: hello\n\n",
		"data: [DONE]\n\n",
		"event: delta\r\ndata: a\r\ndata: b\r\n\r\n",
		": comment\nid: 1\nretry: 10\ndata: x\n\n",
		"data:\n\n",
		"data: tail-no-blank",
		"\n\n\n",
		"data: \r\r\n\n",
		"event:\ndata:\n\n",
		"garbage without newlines at all",
		"data: a\n\r\n\ndata: b\r\n\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		d := NewDecoder(bytes.NewReader(in))
		var frames []Frame
		for {
			fr, ok, err := d.Next()
			if err != nil {
				break
			}
			if !ok {
				// EOF is terminal and sticky.
				if _, ok2, err2 := d.Next(); ok2 || err2 != nil {
					t.Fatalf("Next after EOF: ok=%v err=%v", ok2, err2)
				}
				break
			}
			frames = append(frames, fr)
		}
		// Round-trip every decoded frame whose Data carries no "\r"
		// bytes (the documented Encode guarantee: decoder output never
		// ends with "\n" since values come from split lines).
		for i, fr := range frames {
			if bytes.IndexByte(fr.Data, '\r') >= 0 || bytes.IndexByte([]byte(fr.Event), '\r') >= 0 {
				continue
			}
			var buf bytes.Buffer
			if err := Encode(&buf, fr); err != nil {
				t.Fatalf("frame %d Encode: %v", i, err)
			}
			got, ok, err := NewDecoder(&buf).Next()
			if err != nil || !ok {
				t.Fatalf("frame %d re-decode: ok=%v err=%v", i, ok, err)
			}
			if got.Event != fr.Event || !bytes.Equal(got.Data, fr.Data) {
				t.Fatalf("frame %d round-trip: %+v -> %+v", i, fr, got)
			}
		}
	})
}
