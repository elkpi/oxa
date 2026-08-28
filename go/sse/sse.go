// Package sse implements a standalone byte-level Server-Sent Events (SSE)
// frame adapter. It knows nothing about JSON, the oxa IR, or any provider
// face: it decodes and encodes SSE frames as opaque bytes.
//
// Semantics follow the SSE convention: a frame is a sequence of field lines
// terminated by a blank line. Lines may end in LF or CRLF (mixed tolerated).
// Only the data and event fields are surfaced; comments (lines starting with
// :) and any other field name (id:, retry:, unknown) are ignored. The literal
// "[DONE]" is ordinary Data here and is never special-cased.
package sse

import (
	"bufio"
	"bytes"
	"io"
)

// Frame is one decoded SSE frame.
type Frame struct {
	Event string
	Data  []byte
}

// Decoder decodes SSE frames from an io.Reader. It is safe for sequential
// use by one goroutine; it is not safe for concurrent use.
type Decoder struct {
	sc *bufio.Scanner
	// eof records that the underlying stream reached clean EOF, so Next
	// after EOF keeps returning (zero, false, nil).
	eof bool
}

// NewDecoder returns a Decoder reading frames from r. Partial reads are
// handled internally via bufio: one Read call does not need to yield a
// whole line or a whole frame.
func NewDecoder(r io.Reader) *Decoder {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	sc.Split(splitLines)
	return &Decoder{sc: sc}
}

// splitLines is a bufio.SplitFunc that splits on LF, stripping an optional
// trailing CR, so both "\n" and "\r\n" (and mixed) line endings work.
func splitLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		line := data[:i]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		return i + 1, line, nil
	}
	if atEOF {
		if len(data) > 0 {
			// Final line without trailing newline; strip a stray trailing
			// CR just as if a LF had followed it.
			line := data
			if line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			return len(data), line, nil
		}
		return 0, nil, nil
	}
	return 0, nil, nil
}

// Next returns the next frame. It returns (Frame, true, nil) for each
// decoded frame, (zero, false, nil) only on clean EOF (and keeps doing so
// on subsequent calls), and an error if the underlying reader fails.
func (d *Decoder) Next() (Frame, bool, error) {
	if d.eof {
		return Frame{}, false, nil
	}
	var (
		f      Frame
		have   bool // saw at least one field line in the current frame
		dataLn [][]byte
	)
	flush := func() bool {
		// Blank line terminates a frame; a frame with at least one field
		// line (even an empty data:) yields. A stream of blank lines (or
		// only comments/unknown fields between blanks) does not: per the
		// SSE convention an event with no data is discarded, and comment
		// lines produce nothing.
		if have {
			f.Data = bytes.Join(dataLn, []byte("\n"))
			return true
		}
		return false
	}
	for d.sc.Scan() {
		line := d.sc.Bytes()
		if len(line) == 0 {
			if flush() {
				return f, true, nil
			}
			have, dataLn = false, nil
			f = Frame{}
			continue
		}
		if line[0] == ':' {
			continue // comment
		}
		name, value := line, []byte(nil)
		if i := bytes.IndexByte(line, ':'); i >= 0 {
			name, value = line[:i], line[i+1:]
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:] // single optional leading space
			}
		}
		switch string(name) {
		case "data":
			have = true
			dataLn = append(dataLn, value)
		case "event":
			have = true
			f.Event = string(value) // last one wins
		default:
			// id:, retry:, unknown fields: ignored.
		}
	}
	if err := d.sc.Err(); err != nil {
		d.eof = true // no recovery: subsequent calls stay failed-but-eof
		return Frame{}, false, err
	}
	// Stream ended. A trailing frame without a terminating blank line still
	// yields (the SSE convention dispatches it on connection close).
	d.eof = true
	if flush() {
		return f, true, nil
	}
	return Frame{}, false, nil
}

// Encode writes f to w as an SSE frame: an "event: <ev>" line when Event is
// non-empty, one "data: <line>" line per line of Data (Data is split on
// "\n"), then a blank line to terminate the frame.
//
// Round-trip guarantee: Decode(Encode(f)) yields a frame with equal Event
// and bytes.Equal Data, provided f.Data does not end with "\n" and contains
// no "\r" bytes. The decoder joins the per-line data fields with "\n" and
// strips one "\r" before a line-ending "\n", so Data with a trailing newline
// or embedded CR bytes does not round-trip byte-for-byte.
func Encode(w io.Writer, f Frame) error {
	var buf bytes.Buffer
	if f.Event != "" {
		buf.WriteString("event: ")
		buf.WriteString(f.Event)
		buf.WriteByte('\n')
	}
	if len(f.Data) == 0 {
		buf.WriteString("data: \n")
	} else {
		for _, line := range bytes.Split(f.Data, []byte("\n")) {
			buf.WriteString("data: ")
			buf.Write(line)
			buf.WriteByte('\n')
		}
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}
